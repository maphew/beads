package dolt

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/doltserver"
)

// These tests cover the auto-start port-provenance fail-closed behavior
// (GH#4052): when the configured Dolt server port is unreachable and
// auto-start ends up on a different port, bd must fail closed instead of
// silently retargeting when the configured port came from an authoritative
// source (env var, project/global config.yaml, metadata.json). Retargeting
// stays unchanged when the source is bd's own port-file bookkeeping, or when
// no port was ever configured (ServerPort == 0).
//
// stubEnsureRunningDetailed replaces the package-level ensureRunningDetailed
// var so these tests never spawn a real dolt sql-server process.
func stubEnsureRunningDetailed(t *testing.T, port int, startedByUs bool, err error) {
	t.Helper()
	orig := ensureRunningDetailed
	t.Cleanup(func() { ensureRunningDetailed = orig })
	ensureRunningDetailed = func(beadsDir string) (int, bool, error) {
		return port, startedByUs, err
	}
}

// baseAutoStartCfg returns a Config that will reach the auto-start branch of
// newServerMode: the initial dial to ServerPort fails (nothing listens on
// port 1), auto-start is permitted, and BEADS_TEST_MODE disables the real
// circuit breaker so no state files are touched.
func baseAutoStartCfg(t *testing.T, database string, serverPort int, source doltserver.PortSource) *Config {
	t.Helper()
	t.Setenv("BEADS_TEST_MODE", "1")
	return &Config{
		Database:         database,
		Path:             t.TempDir(),
		ServerHost:       "127.0.0.1",
		ServerPort:       serverPort,
		ServerPortSource: source,
		AutoStart:        true,
		DisableAutoStart: false,
	}
}

// TestNewServerMode_AuthoritativePortSource_RetargetedPort_FailsClosed is the
// regression test: an authoritative port source (env var here) plus a
// changed auto-start port must fail closed with an explanatory error, not
// silently retarget. Verified to genuinely discriminate: reverting the
// store.go fail-closed check makes this test fail (see session report).
func TestNewServerMode_AuthoritativePortSource_RetargetedPort_FailsClosed(t *testing.T) {
	const configuredPort = 1 // unreachable: nothing listens on port 1
	const newPort = 54321
	cfg := baseAutoStartCfg(t, "test_port_provenance_env", configuredPort, doltserver.PortSourceEnv)
	stubEnsureRunningDetailed(t, newPort, false, nil)

	_, err := newServerMode(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected fail-closed error when an authoritative port source disagrees with auto-start's port")
	}
	msg := err.Error()
	for _, want := range []string{
		strconv.Itoa(configuredPort),
		strconv.Itoa(newPort),
		"will not silently use",
		"bd dolt start",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
	if cfg.ServerPort != configuredPort {
		t.Errorf("cfg.ServerPort mutated to %d on fail-closed path, want unchanged %d", cfg.ServerPort, configuredPort)
	}
}

// TestNewServerMode_AllAuthoritativeSources_FailClosed exercises each
// authoritative PortSource, not just env, to guard against a future source
// being added to the authoritative set without updating IsAuthoritative (or
// vice versa).
func TestNewServerMode_AllAuthoritativeSources_FailClosed(t *testing.T) {
	sources := []doltserver.PortSource{
		doltserver.PortSourceEnv,
		doltserver.PortSourceDoltConfigYaml,
		doltserver.PortSourceConfigYaml,
		doltserver.PortSourceMetadataJSON,
	}
	for _, src := range sources {
		src := src
		t.Run(string(src), func(t *testing.T) {
			cfg := baseAutoStartCfg(t, "test_port_provenance_"+string(src), 1, src)
			stubEnsureRunningDetailed(t, 54322, false, nil)

			_, err := newServerMode(context.Background(), cfg)
			if err == nil {
				t.Fatalf("expected fail-closed error for authoritative source %q", src)
			}
			if !strings.Contains(err.Error(), "will not silently use") {
				t.Errorf("source %q: expected fail-closed message, got: %v", src, err)
			}
		})
	}
}

// TestNewServerMode_PortFileSource_RetargetedPort_StillRetargets guards
// against the UX regression: bd's own port-file bookkeeping must keep
// today's silent-retarget-with-warning behavior. Since the retargeted port
// (54323) is not a real server either, the open still ultimately fails, but
// via the old "auto-started but still unreachable" path, not the new
// fail-closed error — proving cfg.ServerPort was updated and a connection
// was attempted on the new port.
func TestNewServerMode_PortFileSource_RetargetedPort_StillRetargets(t *testing.T) {
	const configuredPort = 1
	const newPort = 54323
	cfg := baseAutoStartCfg(t, "test_port_provenance_portfile", configuredPort, doltserver.PortSourcePortFile)
	stubEnsureRunningDetailed(t, newPort, false, nil)

	_, err := newServerMode(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a connection error (no real server listening on the retargeted port)")
	}
	if strings.Contains(err.Error(), "will not silently use") {
		t.Fatalf("port-file source must not trigger the fail-closed path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "auto-started but still unreachable") {
		t.Fatalf("expected the existing auto-start-unreachable error, got: %v", err)
	}
	if cfg.ServerPort != newPort {
		t.Errorf("cfg.ServerPort = %d, want retargeted to %d", cfg.ServerPort, newPort)
	}
}

// TestNewServerMode_UnsetPortSource_RetargetedPort_StillRetargets guards the
// same UX-preservation case for a Config built without ServerPortSource set
// at all (zero value == PortSourceUnset), matching any caller that
// constructs Config directly rather than through applyConfigDefaults.
func TestNewServerMode_UnsetPortSource_RetargetedPort_StillRetargets(t *testing.T) {
	const configuredPort = 1
	const newPort = 54324
	cfg := baseAutoStartCfg(t, "test_port_provenance_unset", configuredPort, doltserver.PortSourceUnset)
	stubEnsureRunningDetailed(t, newPort, false, nil)

	_, err := newServerMode(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a connection error (no real server listening on the retargeted port)")
	}
	if strings.Contains(err.Error(), "will not silently use") {
		t.Fatalf("unset port source must not trigger the fail-closed path, got: %v", err)
	}
	if cfg.ServerPort != newPort {
		t.Errorf("cfg.ServerPort = %d, want retargeted to %d", cfg.ServerPort, newPort)
	}
}

// TestNewServerMode_ZeroServerPort_Unaffected verifies cfg.ServerPort == 0
// (never resolved) is unaffected by the provenance check regardless of
// source: auto-start always adopts the ephemeral port it allocated.
func TestNewServerMode_ZeroServerPort_Unaffected(t *testing.T) {
	const newPort = 54325
	cfg := baseAutoStartCfg(t, "test_port_provenance_zero", 0, doltserver.PortSourceEnv)
	stubEnsureRunningDetailed(t, newPort, false, nil)

	_, err := newServerMode(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a connection error (no real server listening on the allocated port)")
	}
	if strings.Contains(err.Error(), "will not silently use") {
		t.Fatalf("cfg.ServerPort == 0 must never trigger the fail-closed path, got: %v", err)
	}
	if cfg.ServerPort != newPort {
		t.Errorf("cfg.ServerPort = %d, want adopted ephemeral port %d", cfg.ServerPort, newPort)
	}
}
