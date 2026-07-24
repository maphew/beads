package proxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/identity"
	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
)

// ForceStopOptions configures ForceStopUnverified.
type ForceStopOptions struct {
	Timeout time.Duration
}

// ForceStopReport truthfully describes which irreversible force-stop actions
// completed, including partial progress returned alongside an error.
type ForceStopReport struct {
	RecordPath      string
	PID             int
	Executable      string
	RecordFound     bool
	LockWasHeld     bool
	ProcessWasGone  bool
	SignalSent      bool
	QuarantinedPath string
}

// ForceStopUnverified is the narrow recovery primitive intended for future
// wiring to `bd dolt stop --force`. It is not exposed by a CLI in this stage.
//
// When proxy.lock is free, the function holds it while inspecting, signaling,
// and quarantining the unchanged record. When proxy.lock is held (the usual
// pre-upgrade-proxy case), it first verifies the live PID's executable basename,
// signals it, waits for the lock to become free, then quarantines only if the
// record is unchanged. Both flows accept an already-gone recorded process.
// An unverified live PID is never signaled unless its executable basename is
// exactly bd or dolt (with an optional .exe suffix).
func ForceStopUnverified(rootDir string, opts ...ForceStopOptions) (ForceStopReport, error) {
	report := ForceStopReport{RecordPath: pidfile.Path(rootDir, PIDFileName)}
	if len(opts) > 1 {
		return report, errors.New("proxy.ForceStopUnverified: at most one options value is allowed")
	}
	timeout := shutdownConfirmDeadline
	if len(opts) == 1 && opts[0].Timeout != 0 {
		timeout = opts[0].Timeout
	}
	if timeout <= 0 {
		return report, fmt.Errorf("proxy.ForceStopUnverified: timeout must be positive, got %s", timeout)
	}
	if err := advanceStopEpoch(rootDir); err != nil {
		return report, fmt.Errorf("proxy.ForceStopUnverified: publish stop epoch: %w", err)
	}

	deadline := time.Now().Add(timeout)
	lockPath := filepath.Join(rootDir, LockFileName)
	lock, err := util.TryLock(lockPath)
	if err == nil {
		defer lock.Unlock()
		return forceStopUnverifiedLocked(rootDir, deadline, report)
	}
	if !lockfile.IsLocked(err) {
		return report, fmt.Errorf("proxy.ForceStopUnverified: probe %s: %w", lockPath, err)
	}
	report.LockWasHeld = true

	record, err := readForceStopRecord(rootDir, &report)
	if err != nil || record == nil {
		return report, err
	}
	if err := requireUnverifiableProxyRecord(rootDir, record); err != nil {
		return report, err
	}
	if err := inspectAndStopUnverifiedPID(record.Pid, deadline, &report); err != nil {
		return report, err
	}

	lock, err = acquireForceStopLock(lockPath, deadline)
	if err != nil {
		return report, err
	}
	defer lock.Unlock()
	if err := quarantineForceStopRecord(rootDir, record, &report); err != nil {
		return report, err
	}
	return report, nil
}

func forceStopUnverifiedLocked(
	rootDir string,
	deadline time.Time,
	report ForceStopReport,
) (ForceStopReport, error) {
	record, err := readForceStopRecord(rootDir, &report)
	if err != nil || record == nil {
		return report, err
	}
	if err := requireUnverifiableProxyRecord(rootDir, record); err != nil {
		return report, err
	}
	if err := inspectAndStopUnverifiedPID(record.Pid, deadline, &report); err != nil {
		return report, err
	}
	if err := quarantineForceStopRecord(rootDir, record, &report); err != nil {
		return report, err
	}
	return report, nil
}

func readForceStopRecord(rootDir string, report *ForceStopReport) (*pidfile.PidFile, error) {
	record, err := pidfile.Read(rootDir, PIDFileName)
	if err != nil {
		if isMalformedPIDFileError(err) {
			return nil, unverifiableProcessError(
				"force-stop",
				report.RecordPath,
				0,
				err,
				unverifiableProcessChecks{},
			)
		}
		return nil, fmt.Errorf("proxy.ForceStopUnverified: read %s: %w", report.RecordPath, err)
	}
	if record == nil {
		return nil, nil
	}
	report.RecordFound = true
	report.PID = record.Pid
	return record, nil
}

func requireUnverifiableProxyRecord(rootDir string, record *pidfile.PidFile) error {
	if err := record.ValidateV2(pidfile.KindProxy); err != nil {
		return nil
	}
	rootID, err := identity.RootID(rootDir)
	if err == nil && record.RootID == rootID {
		return errors.New(
			"proxy.ForceStopUnverified: record has a verifiable v2 workspace identity; use proxy.Shutdown",
		)
	}
	return nil
}

func inspectAndStopUnverifiedPID(pid int, deadline time.Time, report *ForceStopReport) error {
	if pid <= 0 {
		return fmt.Errorf("proxy.ForceStopUnverified: record %s has invalid pid %d", report.RecordPath, pid)
	}
	executable, gone, err := processExecutableBasename(pid)
	if err != nil {
		return fmt.Errorf("proxy.ForceStopUnverified: inspect executable for pid %d: %w", pid, err)
	}
	if gone {
		report.ProcessWasGone = true
		return nil
	}
	executable = normalizeForceStopExecutable(executable)
	report.Executable = executable
	if executable != "bd" && executable != "dolt" {
		return fmt.Errorf(
			"proxy.ForceStopUnverified: refusing to signal pid %d from %s: executable basename is %q, want bd or dolt",
			pid,
			report.RecordPath,
			executable,
		)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("proxy.ForceStopUnverified: find pid %d: %w", pid, err)
	}
	if err := process.Kill(); err != nil {
		current, nowGone, inspectErr := processExecutableBasename(pid)
		if inspectErr == nil && (nowGone || normalizeForceStopExecutable(current) != executable) {
			report.ProcessWasGone = true
			return nil
		}
		return fmt.Errorf("proxy.ForceStopUnverified: signal pid %d: %w", pid, err)
	}
	report.SignalSent = true

	for {
		current, nowGone, inspectErr := processExecutableBasename(pid)
		if inspectErr != nil {
			return fmt.Errorf("proxy.ForceStopUnverified: confirm pid %d exit: %w", pid, inspectErr)
		}
		if nowGone || normalizeForceStopExecutable(current) != executable {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("proxy.ForceStopUnverified: timeout waiting for pid %d to exit", pid)
		}
		time.Sleep(shutdownConfirmPoll)
	}
}

func normalizeForceStopExecutable(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	name = strings.TrimSuffix(name, " (deleted)")
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	return name
}

func acquireForceStopLock(lockPath string, deadline time.Time) (*util.Lock, error) {
	for {
		lock, err := util.TryLock(lockPath)
		if err == nil {
			return lock, nil
		}
		if !lockfile.IsLocked(err) {
			return nil, fmt.Errorf("proxy.ForceStopUnverified: acquire %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("proxy.ForceStopUnverified: timeout acquiring %s after signaling", lockPath)
		}
		time.Sleep(shutdownConfirmPoll)
	}
}

func quarantineForceStopRecord(
	rootDir string,
	record *pidfile.PidFile,
	report *ForceStopReport,
) error {
	current, err := pidfile.Read(rootDir, PIDFileName)
	if err != nil {
		return fmt.Errorf("proxy.ForceStopUnverified: re-read %s: %w", report.RecordPath, err)
	}
	if current == nil {
		return nil
	}
	if *current != *record {
		return fmt.Errorf(
			"proxy.ForceStopUnverified: record %s changed after pid %d was stopped; refusing to quarantine the replacement",
			report.RecordPath,
			record.Pid,
		)
	}
	target, err := quarantineRecord(rootDir, PIDFileName, time.Now())
	if err != nil {
		return fmt.Errorf("proxy.ForceStopUnverified: quarantine %s: %w", report.RecordPath, err)
	}
	report.QuarantinedPath = target
	return nil
}
