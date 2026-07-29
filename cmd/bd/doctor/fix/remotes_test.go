//go:build cgo

package fix

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/testutil"
	"github.com/steveyegge/beads/internal/types"
)

// newIdentityTestStore is a variant of newFixTestStore that additionally
// sets CreateIfMissing on the dolt.Config (newFixTestStore's connect-only
// config depends on the test database already existing, which this Dolt
// server build does not do implicitly) and seeds a project_id shared by
// metadata.json and the database, matching what a real `bd init` produces.
func newIdentityTestStore(t *testing.T, dir, prefix string) (store *dolt.DoltStore, beadsDir, projectID string) {
	t.Helper()
	testutil.RequireDoltBinary(t)
	ctx := context.Background()

	port := fixTestServerPort()

	beadsDir = filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("create .beads: %v", err)
	}

	dbName := "fixident_" + strings.ToLower(t.Name())
	dbName = strings.NewReplacer("/", "_", " ", "_").Replace(dbName)

	projectID = configfile.GenerateProjectID()
	cfg := &configfile.Config{
		Database:       "dolt",
		DoltMode:       configfile.DoltModeServer,
		DoltServerHost: "127.0.0.1",
		DoltServerPort: port,
		DoltDatabase:   dbName,
		ProjectID:      projectID,
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	store, err := dolt.New(ctx, &dolt.Config{
		Path:            filepath.Join(beadsDir, "beads.db"),
		ServerHost:      "127.0.0.1",
		ServerPort:      port,
		Database:        dbName,
		CreateIfMissing: true,
		MaxOpenConns:    1,
	})
	if err != nil {
		t.Skipf("skipping: Dolt server not available: %v", err)
	}

	if err := store.SetConfig(ctx, "issue_prefix", prefix); err != nil {
		store.Close()
		t.Fatalf("SetConfig(issue_prefix): %v", err)
	}
	if err := store.SetMetadata(ctx, "_project_id", projectID); err != nil {
		store.Close()
		t.Fatalf("SetMetadata(_project_id): %v", err)
	}

	t.Cleanup(func() {
		store.Close()
		dropFixTestDatabase(dbName, port)
	})
	return store, beadsDir, projectID
}

// TestDestructiveFix_AbortsOnProjectIdentityMismatch is the mybd-2qegi
// regression test: destructive doctor --fix paths (remotes.go's openFixDB)
// dial a fresh connection on every call, re-resolving the dolt server port
// independently of whatever connection doctor's read-only checks used. A
// stale .beads/dolt-server.port file (or the shared-mode default port) can
// aim that fresh connection at a different project's database — one that
// commonly shares the same default database name ("beads").
//
// This models the observable effect a stale/wrong port resolution would
// have: the connection openDoltDB opens belongs to a database whose stored
// _project_id does not match the local metadata.json project_id it was
// diagnosed against. verifyFixTargetIdentity must catch that and abort
// before any DELETE runs, regardless of *how* the mismatched connection was
// reached.
func TestDestructiveFix_AbortsOnProjectIdentityMismatch(t *testing.T) {
	dir := t.TempDir()
	store, _, _ := newIdentityTestStore(t, dir, "tst")
	ctx := context.Background()

	// Simulate the stale-port scenario: the connection openDoltDB resolves
	// lands on a database belonging to a *different* project than the local
	// metadata.json describes.
	if err := store.SetMetadata(ctx, "_project_id", "wrong-project-"+configfile.GenerateProjectID()); err != nil {
		t.Fatalf("failed to force a project_id mismatch: %v", err)
	}

	// Plant a mis-keyed dependency row that a real DependencyKeys fix would
	// re-key/delete if it were allowed to proceed against this connection.
	for _, id := range []string{"tst-1", "tst-2"} {
		issue := &types.Issue{
			ID:        id,
			Title:     "mismatch guard test " + id,
			Priority:  2,
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("CreateIssue(%s): %v", id, err)
		}
	}
	const randomID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	db := store.UnderlyingDB()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO dependencies (id, issue_id, depends_on_issue_id, type, created_at, created_by)
		VALUES (?, 'tst-1', 'tst-2', 'blocks', NOW(), 'test')`, randomID); err != nil {
		t.Fatalf("insert randomly-keyed dependency: %v", err)
	}

	// The destructive fix must refuse to touch this connection.
	err := DependencyKeys(dir, false)
	if err == nil {
		t.Fatal("DependencyKeys should have aborted on project identity mismatch, got nil error")
	}
	if !strings.Contains(err.Error(), "PROJECT IDENTITY MISMATCH") {
		t.Errorf("expected a PROJECT IDENTITY MISMATCH error, got: %v", err)
	}

	// The anomalous row must be untouched — no re-key, no delete.
	var count int
	if scanErr := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM dependencies WHERE id = ?`, randomID).Scan(&count); scanErr != nil {
		t.Fatalf("count dependency row: %v", scanErr)
	}
	if count != 1 {
		t.Errorf("expected the mis-keyed row to survive the aborted fix, found %d matching rows", count)
	}

	// Same guard must apply to the other destructive fix entrypoints that
	// share openDoltDB/openFixDB.
	for name, fn := range map[string]func(string) error{
		"CrossTableDuplicates":    func(p string) error { return CrossTableDuplicates(p, false) },
		"OrphanedDependencies":    func(p string) error { return OrphanedDependencies(p, false) },
		"ChildParentDependencies": func(p string) error { return ChildParentDependencies(p, false) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := fn(dir); err == nil {
				t.Fatalf("%s should have aborted on project identity mismatch, got nil error", name)
			} else if !strings.Contains(err.Error(), "PROJECT IDENTITY MISMATCH") {
				t.Errorf("%s: expected a PROJECT IDENTITY MISMATCH error, got: %v", name, err)
			}
		})
	}
}

// TestVerifyFixTargetIdentity covers verifyFixTargetIdentity directly:
// match passes, mismatch and unverifiable targets (missing project_id on
// either side) abort.
func TestVerifyFixTargetIdentity(t *testing.T) {
	dir := t.TempDir()
	store, beadsDir, _ := newIdentityTestStore(t, dir, "vfi")
	ctx := context.Background()
	db := store.UnderlyingDB()

	t.Run("matching project ids pass", func(t *testing.T) {
		if err := verifyFixTargetIdentity(db, beadsDir); err != nil {
			t.Errorf("expected no error for matching project ids, got: %v", err)
		}
	})

	t.Run("mismatched project ids abort", func(t *testing.T) {
		if err := store.SetMetadata(ctx, "_project_id", "some-other-project"); err != nil {
			t.Fatalf("SetMetadata: %v", err)
		}
		err := verifyFixTargetIdentity(db, beadsDir)
		if err == nil {
			t.Fatal("expected mismatch error, got nil")
		}
		if !strings.Contains(err.Error(), "PROJECT IDENTITY MISMATCH") {
			t.Errorf("expected PROJECT IDENTITY MISMATCH, got: %v", err)
		}
	})

	t.Run("missing database project_id is unverifiable and aborts", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, "DELETE FROM metadata WHERE `key` = '_project_id'"); err != nil {
			t.Fatalf("clear _project_id: %v", err)
		}
		err := verifyFixTargetIdentity(db, beadsDir)
		if err == nil {
			t.Fatal("expected unverifiable-target error, got nil")
		}
		if !strings.Contains(err.Error(), "no project_id") {
			t.Errorf("expected an unverifiable-target error, got: %v", err)
		}
	})

	t.Run("missing local project_id is unverifiable and aborts", func(t *testing.T) {
		cfg, err := configfile.Load(beadsDir)
		if err != nil || cfg == nil {
			t.Fatalf("load config: %v", err)
		}
		cfg.ProjectID = ""
		if err := cfg.Save(beadsDir); err != nil {
			t.Fatalf("save config: %v", err)
		}
		if err := verifyFixTargetIdentity(db, beadsDir); err == nil {
			t.Fatal("expected unverifiable-target error for missing local project_id, got nil")
		}
	})
}
