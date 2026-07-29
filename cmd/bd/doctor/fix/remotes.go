package fix

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/storage/doltutil"
)

func openFixDB(beadsDir string, cfg *configfile.Config) (*sql.DB, error) {
	host := cfg.GetDoltServerHost()
	user := cfg.GetDoltServerUser()
	database := cfg.GetDoltDatabase()
	password := cfg.GetDoltServerPassword()
	port := doltserver.DefaultConfig(beadsDir).Port

	connStr := doltutil.ServerDSN{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		Database: database,
		TLS:      cfg.GetDoltServerTLS(),
	}.String()
	return sql.Open("mysql", connStr)
}

// verifyFixTargetIdentity guards every destructive doctor --fix path against
// a stale or wrong dolt-server.port file (or the shared-mode default port)
// silently redirecting DELETE/UPDATE statements at a different project's
// database than the one doctor diagnosed (mybd-2qegi).
//
// openFixDB dials fresh on every fix call, re-resolving the port from
// doltserver.DefaultConfig(beadsDir) independently of whatever connection
// doctor's read-only checks used. Two different projects commonly share the
// same database name ("beads" by default), so a stale port pointing at an
// unrelated project's server would otherwise connect successfully and
// destructively mutate the wrong data with no error.
//
// This compares the freshly-dialed connection's `_project_id` (from the
// dolt-synced metadata table, the same key internal/storage/dolt/store.go's
// verifyProjectIdentity checks) against the local metadata.json project_id
// for beadsDir. Unlike verifyProjectIdentity — which treats a missing id on
// either side as an old-style setup and skips verification — a destructive
// fix must never proceed on an unverifiable target, so a missing id on
// either side is also treated as a hard abort here.
func verifyFixTargetIdentity(db *sql.DB, beadsDir string) error {
	metaCfg, err := configfile.Load(beadsDir)
	if err != nil || metaCfg == nil {
		return fmt.Errorf("refusing destructive fix: cannot load local metadata.json to verify target database identity: %w", err)
	}
	localID := metaCfg.ProjectID
	if localID == "" {
		return fmt.Errorf("refusing destructive fix: local metadata.json has no project_id, cannot verify the dolt server we resolved is the diagnosed database")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var dbID string
	err = db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE `key` = '_project_id'").Scan(&dbID)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("refusing destructive fix: failed to read database project_id to verify target identity: %w", err)
	}
	if dbID == "" {
		return fmt.Errorf("refusing destructive fix: connected database has no project_id in its metadata table, cannot verify it is the diagnosed database (possible stale dolt-server.port or wrong server)")
	}

	if localID != dbID {
		return fmt.Errorf(
			"refusing destructive fix — PROJECT IDENTITY MISMATCH\n\n"+
				"  Local project ID (metadata.json):  %s\n"+
				"  Connected database project ID:     %s\n\n"+
				"The resolved dolt server port is serving a DIFFERENT project's database\n"+
				"than the one doctor diagnosed. This can happen with a stale\n"+
				"dolt-server.port file or the shared-mode default port pointing at\n"+
				"another project's server.\n\n"+
				"To diagnose: bd dolt status",
			localID, dbID)
	}
	return nil
}
