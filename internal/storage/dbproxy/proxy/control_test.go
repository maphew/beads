package proxy

import (
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/dbproxy/identity"
	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newControlServer(t *testing.T) (*controlServer, string, identity.IdentReply) {
	t.Helper()
	root := t.TempDir()
	secret, err := identity.WriteSecret(root)
	require.NoError(t, err)
	want := identity.IdentReply{
		Schema:     pidfile.SchemaV2,
		Role:       pidfile.KindProxy,
		RootID:     "workspace-root",
		UpstreamID: "dolt-server",
		PID:        123,
		Birth:      "linux-v1:boot:1",
		DataPort:   3306,
	}
	control, err := startControl(root, func() identity.IdentReply { return want })
	require.NoError(t, err)
	want.ControlPort = control.Port()
	t.Cleanup(func() { _ = control.Close() })
	return control, secret, want
}

func TestControl_Identify(t *testing.T) {
	control, secret, want := newControlServer(t)
	got, err := identity.Identify("127.0.0.1", control.Port(), secret, time.Second)
	require.NoError(t, err)
	assert.Len(t, got.MAC, 64)
	got.MAC = ""
	assert.Equal(t, &want, got)
}

func TestControl_RejectsWrongSecret(t *testing.T) {
	control, _, _ := newControlServer(t)
	_, err := identity.Identify("127.0.0.1", control.Port(), "not-the-secret", time.Second)
	require.ErrorIs(t, err, identity.ErrIdentRefused)
}

func TestControl_RejectsOversizedAndGarbageRequests(t *testing.T) {
	control, _, _ := newControlServer(t)
	cases := []struct {
		name    string
		request string
	}{
		{name: "oversized", request: "IDENT " + strings.Repeat("x", maxIdentRequestBytes) + "\n"},
		{name: "garbage", request: "WHOAMI\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(control.Port())), time.Second)
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(time.Second)))
			_, err = io.WriteString(conn, tc.request)
			require.NoError(t, err)
			buf := make([]byte, 1)
			_, err = conn.Read(buf)
			assert.Error(t, err)
		})
	}
}

func TestControl_ConcurrentIdentify(t *testing.T) {
	control, secret, want := newControlServer(t)
	const calls = maxConcurrentIdentRequests
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for range calls {
		wg.Go(func() {
			got, err := identity.Identify("127.0.0.1", control.Port(), secret, time.Second)
			if err == nil {
				got.MAC = ""
				if *got != want {
					err = errors.New("identity reply mismatch")
				}
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}
}

func TestControl_CapsConcurrentHandshakes(t *testing.T) {
	control, _, _ := newControlServer(t)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(control.Port()))
	conns := make([]net.Conn, 0, maxConcurrentIdentRequests)
	t.Cleanup(func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	for range maxConcurrentIdentRequests {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		require.NoError(t, err)
		conns = append(conns, conn)
	}
	require.Eventually(t, func() bool {
		return len(control.slots) == maxConcurrentIdentRequests
	}, time.Second, 10*time.Millisecond)

	extra, err := net.DialTimeout("tcp", addr, time.Second)
	require.NoError(t, err)
	defer extra.Close()
	require.NoError(t, extra.SetDeadline(time.Now().Add(time.Second)))
	_, err = io.WriteString(extra, "IDENT blocked blocked\n")
	require.NoError(t, err)
	buf := make([]byte, 1)
	_, err = extra.Read(buf)
	require.Error(t, err)
}

func TestControl_AcceptLoopStopsAfterPersistentErrors(t *testing.T) {
	listener := &failingListener{err: errors.New("persistent accept failure")}
	control := &controlServer{
		listener: listener,
		done:     make(chan struct{}),
		errs:     make(chan error, 1),
		slots:    make(chan struct{}, maxConcurrentIdentRequests),
	}
	go control.acceptLoop()

	select {
	case err := <-control.Errors():
		require.ErrorContains(t, err, "control accept failed")
	case <-time.After(time.Second):
		t.Fatal("control accept loop did not report persistent failure")
	}
	select {
	case <-control.done:
	case <-time.After(time.Second):
		t.Fatal("control accept loop did not stop")
	}
	assert.Equal(t, maxControlAcceptErrors, listener.accepts)
	assert.True(t, listener.closed)
}

type failingListener struct {
	err     error
	accepts int
	closed  bool
}

func (l *failingListener) Accept() (net.Conn, error) {
	l.accepts++
	return nil, l.err
}

func (l *failingListener) Close() error {
	l.closed = true
	return nil
}

func (l *failingListener) Addr() net.Addr {
	return &net.TCPAddr{}
}
