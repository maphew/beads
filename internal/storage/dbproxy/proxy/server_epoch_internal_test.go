package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/server"
)

// epochAdvancingServer simulates the F4 race deterministically: `bd dolt
// stop` advancing the stop epoch while the proxy child is inside its (slow)
// backend Start, after the spawn marker has already been cleared under
// proxy.lock.
type epochAdvancingServer struct {
	*server.TestDatabaseServerImpl
	rootDir string
	t       *testing.T
}

func (s *epochAdvancingServer) Start(ctx context.Context) error {
	if err := s.TestDatabaseServerImpl.Start(ctx); err != nil {
		return err
	}
	require.NoError(s.t, advanceStopEpoch(s.rootDir))
	return nil
}

// TestListenAndServe_StopEpochAdvanceDuringStartupAbortsPublish asserts that
// a stop which begins after the child took proxy.lock (and cleared the spawn
// marker) but before proxy.pid is written makes the child abort instead of
// publishing a running proxy after the stop returned.
func TestListenAndServe_StopEpochAdvanceDuringStartupAbortsPublish(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	baseline, err := readStopEpoch(root)
	require.NoError(t, err)

	ts := server.New()
	backend := &epochAdvancingServer{TestDatabaseServerImpl: ts, rootDir: root, t: t}

	p := NewProxyServer(ProxyOpts{
		RootDir:   root,
		Port:      0,
		Server:    backend,
		StopEpoch: baseline,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = p.ListenAndServe(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, errStartInterrupted)

	pf, readErr := pidfile.Read(root, PIDFileName)
	require.NoError(t, readErr)
	assert.Nil(t, pf, "proxy.pid must not be published after the stop epoch advanced")

	counters := ts.Snapshot()
	assert.Equal(t, int64(1), counters.StartCalls)
	assert.Equal(t, int64(1), counters.StopCalls, "aborting the publish must still stop the backend")
}

// TestWaitForServerReady_InterruptAbortsWait asserts the readiness wait
// honors its interrupted seam promptly: a never-ready backend must not pin
// the wait for the full timeout once the seam reports a concurrent stop.
// This is the latency half of the F4 fix — `bd dolt stop` gives up on
// proxy.lock after 5s, so an abort bounded only by serverReadyTimeout (30s)
// fails the stop side even though the child eventually cleans up.
func TestWaitForServerReady_InterruptAbortsWait(t *testing.T) {
	t.Parallel()

	ts := server.New()
	ts.DialErr = errors.New("backend still starting")
	require.NoError(t, ts.Start(context.Background()))

	// Report the interruption only on the third probe so the test exercises
	// the per-iteration re-check, not just a pre-wait fast path.
	var calls atomic.Int64
	interrupted := func() error {
		if calls.Add(1) < 3 {
			return nil
		}
		return fmt.Errorf("%w for test-root: stop epoch advanced during startup", errStartInterrupted)
	}

	start := time.Now()
	err := waitForServerReady(context.Background(), ts, serverReadyTimeout, interrupted)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, errStartInterrupted)
	// Three probes are separated by at most two max-length backoff intervals
	// (2 * readyMaxBackoff = 2s); 10s leaves slack for slow CI while still
	// proving the wait did not run out its 30s budget.
	assert.Less(t, elapsed, 10*time.Second,
		"readiness wait must abort within ~one backoff interval of the interrupt, not run out serverReadyTimeout")
	assert.GreaterOrEqual(t, calls.Load(), int64(3))
}

// epochAdvanceOnDialServer advances the stop epoch from the first readiness
// dial and never becomes dialable, simulating `bd dolt stop` landing while
// the backend is still starting. Deterministic: probe 1 fails after
// advancing the epoch, so probe 2's epoch re-check must abort the wait.
type epochAdvanceOnDialServer struct {
	*server.TestDatabaseServerImpl
	rootDir string
	t       *testing.T
	once    sync.Once
}

func (s *epochAdvanceOnDialServer) Dial(_ context.Context) (net.Conn, error) {
	s.once.Do(func() { require.NoError(s.t, advanceStopEpoch(s.rootDir)) })
	return nil, errors.New("backend still starting")
}

// TestListenAndServe_StopEpochAdvanceDuringReadinessWaitAbortsPromptly is the
// mid-wait sibling of the post-wait test above: the epoch advances while the
// child sits inside waitForServerReady against a backend that never becomes
// ready. The abort must come from inside the retry loop — well before the
// 30s readiness budget — release proxy.lock, and stop the backend without
// ever publishing proxy.pid.
func TestListenAndServe_StopEpochAdvanceDuringReadinessWaitAbortsPromptly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	baseline, err := readStopEpoch(root)
	require.NoError(t, err)

	ts := server.New()
	backend := &epochAdvanceOnDialServer{TestDatabaseServerImpl: ts, rootDir: root, t: t}

	p := NewProxyServer(ProxyOpts{
		RootDir:   root,
		Port:      0,
		Server:    backend,
		StopEpoch: baseline,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	err = p.ListenAndServe(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, errStartInterrupted)
	// Without the in-loop check this scenario burns the full 30s readiness
	// budget and then fails as "database server not ready" instead.
	assert.Less(t, elapsed, 15*time.Second,
		"stop during readiness wait must abort within ~one backoff interval, not exhaust serverReadyTimeout")

	pf, readErr := pidfile.Read(root, PIDFileName)
	require.NoError(t, readErr)
	assert.Nil(t, pf, "proxy.pid must not be published after the stop epoch advanced")

	counters := ts.Snapshot()
	assert.Equal(t, int64(1), counters.StartCalls)
	assert.Equal(t, int64(1), counters.StopCalls, "aborting the readiness wait must still stop the backend")
}
