package proxy

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/identity"
	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/server"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
)

const (
	shutdownConfirmDeadline = 5 * time.Second
	shutdownConfirmPoll     = 50 * time.Millisecond
)

// Shutdown stops the verified proxy and backend processes for rootDir.
//
// Advancing the stop epoch first makes every start attempt which began before
// this call terminally abort instead of retrying after its child is stopped.
// The proxy spawn marker covers the smaller release-lock-before-exec window:
// Shutdown waits for that marked attempt to either acquire proxy.lock or fail,
// then verifies and stops whatever it published. All waits are bounded by
// shutdownConfirmDeadline.
func Shutdown(rootDir string) error {
	if _, err := advanceStopEpoch(rootDir); err != nil {
		return fmt.Errorf("proxy.Shutdown: publish stop epoch: %w", err)
	}

	proxyLock, proxyErr := stopAndAcquire(
		rootDir,
		LockFileName,
		PIDFileName,
		pidfile.KindProxy,
		false,
		true,
	)
	if proxyLock != nil {
		defer proxyLock.Unlock()
	}

	backendLock, backendErr := stopAndAcquire(
		rootDir,
		server.LockFileName,
		server.PIDFileName,
		pidfile.KindDoltBackend,
		true,
		false,
	)
	if backendLock != nil {
		backendLock.Unlock()
	}

	switch {
	case proxyErr == nil && backendErr == nil:
		return nil
	case proxyErr == nil:
		return fmt.Errorf("proxy.Shutdown partial: proxy stopped; backend left running: %w", backendErr)
	case backendErr == nil:
		return fmt.Errorf("proxy.Shutdown partial: backend stopped; proxy left running: %w", proxyErr)
	default:
		return fmt.Errorf(
			"proxy.Shutdown failed: proxy left running: %v; backend left running: %w",
			proxyErr,
			backendErr,
		)
	}
}

func ControlFilePaths(rootDir string) []string {
	return []string{
		filepath.Join(rootDir, PIDFileName),
		filepath.Join(rootDir, LockFileName),
		filepath.Join(rootDir, LogFileName),
		filepath.Join(rootDir, spawnMarkerFileName),
		filepath.Join(rootDir, stopEpochFileName),
		filepath.Join(rootDir, server.PIDFileName),
		filepath.Join(rootDir, server.LockFileName),
	}
}

func PurgeControlFiles(rootDir string) []error {
	var errs []error
	for _, path := range ControlFilePaths(rootDir) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errs
}

// stopAndAcquire returns with lockName held after the recorded process is
// absent. Holding proxy.lock across backend cleanup prevents a fresh start
// from slipping between the two shutdown phases.
func stopAndAcquire(
	rootDir string,
	lockName string,
	pidName string,
	wantKind string,
	verifyRoot bool,
	checkSpawnMarker bool,
) (*util.Lock, error) {
	lockPath := filepath.Join(rootDir, lockName)
	recordPath := pidfile.Path(rootDir, pidName)
	deadline := time.Now().Add(shutdownConfirmDeadline)
	var stopped *pidfile.PidFile

	for {
		lock, err := util.TryLock(lockPath)
		switch {
		case err == nil:
			if checkSpawnMarker {
				active, markerErr := inspectSpawnMarkerLocked(rootDir)
				if markerErr != nil {
					lock.Unlock()
					return nil, markerErr
				}
				if active {
					lock.Unlock()
					if time.Now().After(deadline) {
						return nil, fmt.Errorf(
							"timeout (%s) waiting for spawn marker %s; wait for the in-progress start to finish, then retry",
							shutdownConfirmDeadline,
							filepath.Join(rootDir, spawnMarkerFileName),
						)
					}
					time.Sleep(shutdownConfirmPoll)
					continue
				}
			}

			pf, readErr := pidfile.Read(rootDir, pidName)
			if readErr != nil {
				lock.Unlock()
				return nil, fmt.Errorf("read %s: %w", recordPath, readErr)
			}
			if pf == nil {
				return lock, nil
			}
			if stopped != nil && *pf == *stopped {
				if removeErr := pidfile.Remove(rootDir, pidName); removeErr != nil {
					lock.Unlock()
					return nil, fmt.Errorf("remove stopped process record %s: %w", recordPath, removeErr)
				}
				return lock, nil
			}
			stopped = nil

			if validateErr := validateKillRecord(rootDir, recordPath, pf, wantKind, verifyRoot); validateErr != nil {
				lock.Unlock()
				return nil, validateErr
			}
			handle, dead, openErr := openRecordedProcess(pf)
			if openErr != nil {
				lock.Unlock()
				return nil, unverifiableProcessError("shutdown", recordPath, pf.Pid, openErr)
			}
			if dead {
				if _, quarantineErr := quarantineRecord(rootDir, pidName, time.Now()); quarantineErr != nil {
					lock.Unlock()
					return nil, fmt.Errorf("quarantine dead process record %s: %w", recordPath, quarantineErr)
				}
				return lock, nil
			}
			if killErr := handle.Kill(); killErr != nil {
				_ = handle.Close()
				lock.Unlock()
				return nil, fmt.Errorf("kill verified pid %d from %s: %w", pf.Pid, recordPath, killErr)
			}
			if closeErr := handle.Close(); closeErr != nil {
				lock.Unlock()
				return nil, fmt.Errorf("close verified process handle for pid %d: %w", pf.Pid, closeErr)
			}
			if waitErr := waitForRecordedProcessExit(pf, time.Until(deadline)); waitErr != nil {
				lock.Unlock()
				return nil, fmt.Errorf("confirm verified pid %d stopped: %w", pf.Pid, waitErr)
			}
			if removeErr := pidfile.Remove(rootDir, pidName); removeErr != nil {
				lock.Unlock()
				return nil, fmt.Errorf("remove stopped process record %s: %w", recordPath, removeErr)
			}
			return lock, nil

		case !lockfile.IsLocked(err):
			return nil, fmt.Errorf("probe %s: %w", lockPath, err)
		}

		pf, readErr := pidfile.Read(rootDir, pidName)
		if readErr != nil {
			return nil, fmt.Errorf("read held-lock record %s: %w", recordPath, readErr)
		}
		if pf != nil {
			if validateErr := validateKillRecord(rootDir, recordPath, pf, wantKind, verifyRoot); validateErr != nil {
				return nil, validateErr
			}
			handle, dead, openErr := openRecordedProcess(pf)
			if openErr != nil {
				return nil, unverifiableProcessError("shutdown", recordPath, pf.Pid, openErr)
			}
			if !dead {
				if killErr := handle.Kill(); killErr != nil {
					_ = handle.Close()
					return nil, fmt.Errorf("kill verified pid %d from %s: %w", pf.Pid, recordPath, killErr)
				}
				if closeErr := handle.Close(); closeErr != nil {
					return nil, fmt.Errorf("close verified process handle for pid %d: %w", pf.Pid, closeErr)
				}
				stopped = pf
			}
		}

		if time.Now().After(deadline) {
			pid := 0
			if pf != nil {
				pid = pf.Pid
			}
			return nil, fmt.Errorf(
				"timeout (%s) acquiring %s after inspecting pid %d at %s; stop the lock owner with the binary that started it, then retry",
				shutdownConfirmDeadline,
				lockPath,
				pid,
				recordPath,
			)
		}
		time.Sleep(shutdownConfirmPoll)
	}
}

func validateKillRecord(
	rootDir string,
	recordPath string,
	pf *pidfile.PidFile,
	wantKind string,
	verifyRoot bool,
) error {
	if err := pf.ValidateV2(wantKind); err != nil {
		return unverifiableProcessError("shutdown", recordPath, pf.Pid, err)
	}
	if !verifyRoot {
		return nil
	}
	rootID, err := identity.RootID(rootDir)
	if err != nil {
		return fmt.Errorf("resolve workspace identity for %s: %w", recordPath, err)
	}
	if pf.RootID != rootID {
		return unverifiableProcessError(
			"shutdown",
			recordPath,
			pf.Pid,
			fmt.Errorf("root identity mismatch (record has %q, workspace has %q)", pf.RootID, rootID),
		)
	}
	return nil
}
