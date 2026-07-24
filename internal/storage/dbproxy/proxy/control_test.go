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
	control.reply = func() identity.IdentReply { return want }
	t.Cleanup(func() { _ = control.Close() })
	return control, secret, want
}

func TestControl_Identify(t *testing.T) {
	control, secret, want := newControlServer(t)
	got, err := identity.Identify("127.0.0.1", control.Port(), secret, time.Second)
	require.NoError(t, err)
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
	const calls = 16
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for range calls {
		wg.Go(func() {
			got, err := identity.Identify("127.0.0.1", control.Port(), secret, time.Second)
			if err == nil && *got != want {
				err = errors.New("identity reply mismatch")
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
