//go:build linux

package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/procid"
	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceStopUnverifiedFreeLock(t *testing.T) {
	root := t.TempDir()
	helper := startNamedForceStopHelper(t, "bd")
	writeLegacyProxyRecord(t, root, helper)

	report, err := ForceStopUnverified(root)
	require.NoError(t, err)
	assert.True(t, report.RecordFound)
	assert.False(t, report.LockWasHeld)
	assert.Equal(t, helper.cmd.Process.Pid, report.PID)
	assert.Equal(t, "bd", report.Executable)
	assert.True(t, report.SignalSent)
	assert.NotEmpty(t, report.QuarantinedPath)
	assertHelperExited(t, helper)
	assert.NoFileExists(t, pidfile.Path(root, PIDFileName))
	assert.FileExists(t, report.QuarantinedPath)
}

func TestForceStopUnverifiedHeldLock(t *testing.T) {
	root := t.TempDir()
	helper := startNamedForceStopHelper(t, "dolt")
	writeLegacyProxyRecord(t, root, helper)
	held, err := util.TryLock(filepath.Join(root, LockFileName))
	require.NoError(t, err)
	released := make(chan struct{})
	go func() {
		<-helper.done
		held.Unlock()
		close(released)
	}()

	report, err := ForceStopUnverified(root, ForceStopOptions{Timeout: 5 * time.Second})
	require.NoError(t, err)
	<-released
	assert.True(t, report.LockWasHeld)
	assert.Equal(t, "dolt", report.Executable)
	assert.True(t, report.SignalSent)
	assert.NotEmpty(t, report.QuarantinedPath)
	assert.NoFileExists(t, pidfile.Path(root, PIDFileName))
}

func TestForceStopUnverifiedGoneProcess(t *testing.T) {
	root := t.TempDir()
	helper := startNamedForceStopHelper(t, "bd")
	writeLegacyProxyRecord(t, root, helper)
	handle, err := procid.Open(helper.cmd.Process.Pid, helper.token)
	require.NoError(t, err)
	require.NoError(t, handle.Kill())
	require.NoError(t, handle.Close())
	assertHelperExited(t, helper)

	report, err := ForceStopUnverified(root)
	require.NoError(t, err)
	assert.True(t, report.ProcessWasGone)
	assert.False(t, report.SignalSent)
	assert.NotEmpty(t, report.QuarantinedPath)
}

func TestForceStopUnverifiedRejectsForeignExecutable(t *testing.T) {
	root := t.TempDir()
	helper := startHelperProcess(t)
	writeLegacyProxyRecord(t, root, helper)

	report, err := ForceStopUnverified(root)
	require.ErrorContains(t, err, "want bd or dolt")
	assert.False(t, report.SignalSent)
	assert.Empty(t, report.QuarantinedPath)
	assertHelperAlive(t, helper)
	assert.FileExists(t, pidfile.Path(root, PIDFileName))
}

func TestForceStopUnverifiedRejectsVerifiableV2Record(t *testing.T) {
	root := t.TempDir()
	helper := startNamedForceStopHelper(t, "bd")
	record := v2Record(t, root, pidfile.KindProxy, helper)
	require.NoError(t, pidfile.Write(root, PIDFileName, record))

	report, err := ForceStopUnverified(root)
	require.ErrorContains(t, err, "use proxy.Shutdown")
	assert.False(t, report.SignalSent)
	assert.Empty(t, report.QuarantinedPath)
	assertHelperAlive(t, helper)
	assert.FileExists(t, pidfile.Path(root, PIDFileName))
}

func startNamedForceStopHelper(t *testing.T, name string) helperProcess {
	t.Helper()
	sleepPath, err := exec.LookPath("sleep")
	require.NoError(t, err)
	sleepPath, err = filepath.EvalSymlinks(sleepPath)
	require.NoError(t, err)
	data, err := os.ReadFile(sleepPath)
	require.NoError(t, err)
	executable := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(executable, data, 0o700))

	cmd := exec.Command(executable, "30")
	require.NoError(t, cmd.Start())
	token, err := procid.Capture(cmd.Process.Pid)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	helper := helperProcess{cmd: cmd, token: token, done: done}
	t.Cleanup(func() {
		matched, verifyErr := procid.Verify(cmd.Process.Pid, token)
		if verifyErr == nil && matched {
			handle, openErr := procid.Open(cmd.Process.Pid, token)
			if openErr == nil {
				_ = handle.Kill()
				_ = handle.Close()
			}
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("named helper pid %d did not exit", cmd.Process.Pid)
		}
	})
	return helper
}

func writeLegacyProxyRecord(t *testing.T, root string, helper helperProcess) {
	t.Helper()
	require.NoError(t, pidfile.Write(root, PIDFileName, pidfile.PidFile{
		Pid:  helper.cmd.Process.Pid,
		Port: 3307,
	}))
}
