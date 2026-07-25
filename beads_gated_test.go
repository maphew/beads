package beads_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads"
	"github.com/steveyegge/beads/internal/workspacegate"
)

// OpenGated must fail fast (never reaching the storage open) while a
// maintenance operation holds the workspace gate exclusively, and must
// not leak a shared gate handle when the underlying storage open fails.
// Both paths are environment-independent: neither needs a working store.
func TestOpenGatedGateSemantics(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.Mkdir(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	gate, err := workspacegate.ForWorkspace(beadsDir)
	if err != nil {
		t.Fatal(err)
	}

	// Exclusive holder (simulating a mode migration) blocks OpenGated,
	// matchable through the exported alias.
	h, err := gate.Acquire(context.Background(), workspacegate.Exclusive,
		workspacegate.Options{Reason: "test migration"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beads.OpenGated(context.Background(), beadsDir, 0); !errors.Is(err, beads.ErrGateBusy) {
		t.Fatalf("OpenGated under exclusive gate: err = %v, want ErrGateBusy", err)
	}
	if err := h.Release(); err != nil {
		t.Fatal(err)
	}

	// Error path: a storage-open failure must release the shared gates
	// taken on the way in. A beadsDir that is a regular file cannot open
	// in any mode or environment.
	badDir := filepath.Join(dir, "bad")
	if err := os.Mkdir(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	badBeads := filepath.Join(badDir, ".beads")
	if err := os.WriteFile(badBeads, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := beads.OpenGated(context.Background(), badBeads, 0); err == nil {
		t.Fatal("OpenGated on a file-as-.beads unexpectedly succeeded")
	}
	badGate, err := workspacegate.ForWorkspace(badBeads)
	if err != nil {
		t.Fatal(err)
	}
	h3, err := badGate.Acquire(context.Background(), workspacegate.Exclusive, workspacegate.Options{})
	if err != nil {
		t.Fatalf("gate still held after failed OpenGated: %v", err)
	}
	_ = h3.Release()
}

// With a working store, OpenGated holds shared gates for the storage
// lifetime and must not amputate extended capabilities (the decorator
// contract on AsIssueClaimer).
func TestOpenGatedLifecycle(t *testing.T) {
	skipIfNoDoltServer(t)

	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.Mkdir(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gate, err := workspacegate.ForWorkspace(beadsDir)
	if err != nil {
		t.Fatal(err)
	}

	st, err := beads.OpenGated(context.Background(), beadsDir, 0)
	if err != nil {
		t.Fatalf("OpenGated: %v", err)
	}

	// Decorator contract: an embedder switching from OpenFromConfig to
	// OpenGated keeps every As*/interface assertion working.
	if _, ok := beads.AsIssueClaimer(st); !ok {
		t.Error("OpenGated store lost IssueClaimer")
	}
	if _, ok := beads.AsEventQuerier(st); !ok {
		t.Error("OpenGated store lost EventQuerier")
	}
	if _, ok := beads.AsBlockedQuerier(st); !ok {
		t.Error("OpenGated store lost BlockedQuerier")
	}
	if _, ok := st.(beads.RemoteStore); !ok {
		t.Error("OpenGated store lost RemoteStore")
	}
	if _, ok := st.(beads.SyncStore); !ok {
		t.Error("OpenGated store lost SyncStore")
	}
	if _, ok := st.(beads.VersionControlReader); !ok {
		t.Error("OpenGated store lost VersionControlReader")
	}

	// The shared gate is held for the storage lifetime: exclusive
	// acquisition fails while open, succeeds after a clean Close.
	if _, err := gate.Acquire(context.Background(), workspacegate.Exclusive, workspacegate.Options{}); !errors.Is(err, beads.ErrGateBusy) {
		t.Fatalf("exclusive while OpenGated storage open: err = %v, want ErrGateBusy", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	h2, err := gate.Acquire(context.Background(), workspacegate.Exclusive, workspacegate.Options{})
	if err != nil {
		t.Fatalf("gate still held after Close: %v", err)
	}
	_ = h2.Release()
}
