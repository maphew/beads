package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
)

type fakeAutoPushTarget struct {
	commit string
	push   func(context.Context) error
}

func (f *fakeAutoPushTarget) GetCurrentCommit(context.Context) (string, error) {
	return f.commit, nil
}

func (f *fakeAutoPushTarget) Push(ctx context.Context) error {
	return f.push(ctx)
}

type autoPushFakeStore struct {
	storage.DoltStorage

	currentCommit string
	pushErr       error
	pushWait      bool
	pushCalls     atomic.Int32
}

func (s *autoPushFakeStore) HasRemote(context.Context, string) (bool, error) {
	return true, nil
}

func (s *autoPushFakeStore) GetCurrentCommit(context.Context) (string, error) {
	if s.currentCommit == "" {
		return "fake-commit", nil
	}
	return s.currentCommit, nil
}

func (s *autoPushFakeStore) Push(ctx context.Context) error {
	s.pushCalls.Add(1)
	if s.pushWait {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.pushErr
}

func (s *autoPushFakeStore) IsClosed() bool {
	return false
}

func (s *autoPushFakeStore) Close() error {
	return nil
}

func setupAutoPushTest(t *testing.T, st storage.DoltStorage) string {
	t.Helper()

	originalStore := store
	originalTestModeUseGlobals := testModeUseGlobals
	originalQuiet := quietFlag
	originalJSON := jsonOutput
	originalSandbox := sandboxMode
	t.Cleanup(func() {
		setStore(originalStore)
		testModeUseGlobals = originalTestModeUseGlobals
		quietFlag = originalQuiet
		jsonOutput = originalJSON
		sandboxMode = originalSandbox
		config.ResetForTesting()
	})

	enableTestModeGlobals()
	setStore(st)
	quietFlag = false
	jsonOutput = false
	sandboxMode = false

	tmp := t.TempDir()
	beadsDir := filepath.Join(tmp, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DIR", beadsDir)
	t.Setenv("BD_DOLT_AUTO_PUSH", "true")
	t.Setenv("BD_DOLT_AUTO_PUSH_INTERVAL", "0")

	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}

	return beadsDir
}

func captureStderrForAutoPush(t *testing.T, fn func()) string {
	t.Helper()

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		_ = r.Close()
	}()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestIsDoltAutoPushEnabled_ExplicitConfig(t *testing.T) {
	// Cannot be parallel: modifies global env vars and config.

	tests := []struct {
		name       string
		envVal     string // "true"/"false" = explicit config via env
		wantResult bool
	}{
		{
			name:       "explicit true → enabled",
			envVal:     "true",
			wantResult: true,
		},
		{
			name:       "explicit false → disabled",
			envVal:     "false",
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BD_DOLT_AUTO_PUSH", tt.envVal)

			config.ResetForTesting()
			t.Cleanup(func() { config.ResetForTesting() })
			if err := config.Initialize(); err != nil {
				t.Fatalf("config.Initialize: %v", err)
			}

			// With explicit config, store check is bypassed
			// (store is nil in this test, which would return false for auto-detection)
			got := isDoltAutoPushEnabled(context.Background())
			if got != tt.wantResult {
				t.Errorf("isDoltAutoPushEnabled() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestIsDoltAutoPushEnabled_DefaultOff(t *testing.T) {
	// Default (no explicit config) must return false — auto-push is opt-in only.
	os.Unsetenv("BD_DOLT_AUTO_PUSH")
	t.Cleanup(func() { os.Unsetenv("BD_DOLT_AUTO_PUSH") })

	config.ResetForTesting()
	t.Cleanup(func() { config.ResetForTesting() })
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}

	got := isDoltAutoPushEnabled(context.Background())
	if got != false {
		t.Errorf("isDoltAutoPushEnabled() default = %v, want false", got)
	}
}

func TestMaybeAutoPush_NilStore(t *testing.T) {
	// maybeAutoPush should be a no-op when store is nil (no panic).
	os.Unsetenv("BD_DOLT_AUTO_PUSH")
	t.Cleanup(func() { os.Unsetenv("BD_DOLT_AUTO_PUSH") })

	config.ResetForTesting()
	t.Cleanup(func() { config.ResetForTesting() })
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}

	// Should not panic with nil store
	maybeAutoPush(context.Background())
}

func TestAutoPush_SkippedForReadOnlyCommands(t *testing.T) {
	// Read-only commands should not trigger auto-push (GH#2191).
	readOnly := []string{"list", "ready", "show", "status", "stats", "blocked", "search", "graph"}
	for _, cmd := range readOnly {
		if !isReadOnlyCommand(cmd) {
			t.Errorf("isReadOnlyCommand(%q) = false, want true", cmd)
		}
	}

	writeCmds := []string{"create", "update", "close", "import"}
	for _, cmd := range writeCmds {
		if isReadOnlyCommand(cmd) {
			t.Errorf("isReadOnlyCommand(%q) = true, want false", cmd)
		}
	}
}

func TestShouldAutoPushAfterCommand(t *testing.T) {
	originalReadonlyMode := readonlyMode
	t.Cleanup(func() { readonlyMode = originalReadonlyMode })
	readonlyMode = false

	root := &cobra.Command{Use: "bd"}
	list := &cobra.Command{Use: "list"}
	create := &cobra.Command{Use: "create"}
	dolt := &cobra.Command{Use: "dolt"}
	doltShow := &cobra.Command{Use: "show"}
	doltSet := &cobra.Command{Use: "set"}
	backup := &cobra.Command{Use: "backup"}
	backupStatus := &cobra.Command{Use: "status"}
	backupRestore := &cobra.Command{Use: "restore"}
	root.AddCommand(list, create, dolt, backup)
	dolt.AddCommand(doltShow, doltSet)
	backup.AddCommand(backupStatus, backupRestore)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{name: "top-level read-only command", cmd: list, want: false},
		{name: "top-level write command", cmd: create, want: true},
		{name: "dolt diagnostic subcommand", cmd: doltShow, want: false},
		{name: "dolt write subcommand", cmd: doltSet, want: true},
		{name: "backup status subcommand", cmd: backupStatus, want: false},
		{name: "backup restore subcommand", cmd: backupRestore, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAutoPushAfterCommand(tt.cmd); got != tt.want {
				t.Fatalf("shouldAutoPushAfterCommand(%q) = %v, want %v", tt.cmd.CommandPath(), got, tt.want)
			}
		})
	}
}

func TestShouldAutoPushAfterCommand_ReadonlyMode(t *testing.T) {
	originalReadonlyMode := readonlyMode
	t.Cleanup(func() { readonlyMode = originalReadonlyMode })
	readonlyMode = true

	cmd := &cobra.Command{Use: "create"}
	if shouldAutoPushAfterCommand(cmd) {
		t.Fatal("shouldAutoPushAfterCommand should be false in readonly mode")
	}
}

func TestAutoPushTimeoutConstants(t *testing.T) {
	// Verify timeout defaults are reasonable (GH#3370).
	if autoPushTimeout < 10*time.Second || autoPushTimeout > 120*time.Second {
		t.Errorf("autoPushTimeout = %s, want 10s-120s range", autoPushTimeout)
	}
}

func TestPushWithContextReturnsPushResult(t *testing.T) {
	target := &fakeAutoPushTarget{
		push: func(context.Context) error {
			return nil
		},
	}

	if err := pushWithContext(context.Background(), target); err != nil {
		t.Fatalf("pushWithContext() = %v, want nil", err)
	}
}

func TestPushWithContextReturnsPushError(t *testing.T) {
	wantErr := errors.New("push failed")
	target := &fakeAutoPushTarget{
		push: func(context.Context) error {
			return wantErr
		},
	}

	err := pushWithContext(context.Background(), target)
	if !errors.Is(err, wantErr) {
		t.Fatalf("pushWithContext() = %v, want %v", err, wantErr)
	}
}

func TestPushWithContextBoundsIgnoredContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	target := &fakeAutoPushTarget{
		push: func(context.Context) error {
			close(started)
			<-release
			return nil
		},
	}
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := pushWithContext(ctx, target)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pushWithContext() = %v, want deadline exceeded", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("pushWithContext returned before Push started")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("pushWithContext took %s, want under 500ms", elapsed)
	}
}

func TestMaybeAutoPush_CancelledContext(t *testing.T) {
	// maybeAutoPush should handle cancelled context gracefully (GH#3370).
	t.Setenv("BD_DOLT_AUTO_PUSH", "true")

	config.ResetForTesting()
	t.Cleanup(func() { config.ResetForTesting() })
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	maybeAutoPush(ctx)
}

func TestMaybeAutoPush_DisabledByConfig(t *testing.T) {
	// When explicitly disabled, maybeAutoPush should be a no-op.
	t.Setenv("BD_DOLT_AUTO_PUSH", "false")

	config.ResetForTesting()
	t.Cleanup(func() { config.ResetForTesting() })
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}

	// Should not panic or attempt push
	maybeAutoPush(context.Background())
}

func TestMaybeAutoPush_UnreachableRemoteTimesOutAndThrottlesRetry(t *testing.T) {
	t.Setenv("BD_DOLT_AUTO_PUSH_TIMEOUT", "1ms")
	fake := &autoPushFakeStore{currentCommit: "commit-a", pushWait: true}
	setupAutoPushTest(t, fake)

	stderr := captureStderrForAutoPush(t, func() {
		maybeAutoPush(context.Background())
	})

	if got := fake.pushCalls.Load(); got != 1 {
		t.Fatalf("pushCalls = %d, want 1", got)
	}
	if !strings.Contains(stderr, "dolt auto-push timed out") {
		t.Fatalf("expected timeout warning, got:\n%s", stderr)
	}

	ps, err := loadPushState()
	if err != nil {
		t.Fatalf("loadPushState: %v", err)
	}
	if ps == nil || ps.LastPush == "" {
		t.Fatalf("push state should record failed attempt timestamp, got %+v", ps)
	}
	if ps.LastCommit != "" {
		t.Fatalf("failed push must not record LastCommit, got %+v", ps)
	}

	maybeAutoPush(context.Background())
	if got := fake.pushCalls.Load(); got != 1 {
		t.Fatalf("pushCalls after throttled retry = %d, want 1", got)
	}
}

func TestMaybeAutoPush_DivergedRemotePrintsRecoveryGuidance(t *testing.T) {
	fake := &autoPushFakeStore{
		currentCommit: "commit-b",
		pushErr:       errors.New("push failed: can't find common ancestor"),
	}
	setupAutoPushTest(t, fake)

	stderr := captureStderrForAutoPush(t, func() {
		maybeAutoPush(context.Background())
	})

	if got := fake.pushCalls.Load(); got != 1 {
		t.Fatalf("pushCalls = %d, want 1", got)
	}
	for _, want := range []string{
		"dolt auto-push failed",
		"Local and remote Dolt histories have diverged",
		"bd bootstrap",
		"bd dolt push --force",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("expected stderr to contain %q, got:\n%s", want, stderr)
		}
	}

	ps, err := loadPushState()
	if err != nil {
		t.Fatalf("loadPushState: %v", err)
	}
	if ps == nil || ps.LastPush == "" {
		t.Fatalf("push state should record diverged attempt timestamp, got %+v", ps)
	}
	if ps.LastCommit != "" {
		t.Fatalf("diverged push must not record LastCommit, got %+v", ps)
	}
}

func TestLoadSavePushState(t *testing.T) {

	// Create a temp .beads dir with metadata.json so FindBeadsDir works
	tmp := t.TempDir()
	beadsDir := filepath.Join(tmp, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DIR", beadsDir)

	// No file yet → nil, nil
	ps, err := loadPushState()
	if err != nil {
		t.Fatalf("loadPushState (no file): %v", err)
	}
	if ps != nil {
		t.Fatalf("loadPushState (no file): got %+v, want nil", ps)
	}

	// Save and reload
	want := &pushState{LastPush: "2026-03-09T12:00:00Z", LastCommit: "abc123"}
	if err := savePushState(want); err != nil {
		t.Fatalf("savePushState: %v", err)
	}
	got, err := loadPushState()
	if err != nil {
		t.Fatalf("loadPushState: %v", err)
	}
	if got == nil || got.LastPush != want.LastPush || got.LastCommit != want.LastCommit {
		t.Errorf("loadPushState = %+v, want %+v", got, want)
	}
}

func TestLoadPushState_CorruptJSON(t *testing.T) {

	tmp := t.TempDir()
	beadsDir := filepath.Join(tmp, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DIR", beadsDir)

	// Write garbage
	if err := os.WriteFile(filepath.Join(beadsDir, "push-state.json"), []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPushState()
	if err == nil {
		t.Error("loadPushState with corrupt JSON: expected error, got nil")
	}
}
