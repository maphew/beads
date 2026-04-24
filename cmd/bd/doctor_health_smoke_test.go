package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

// TestRunCheckHealth_UnreachableServer exercises the DSN-building branch in
// runCheckHealth (bd-h5k7). With metadata.json pointing at an unreachable
// port, the code should resolve Password via GetDoltServerPasswordForPort
// without panicking, fail the ping silently, and return.
func TestRunCheckHealth_UnreachableServer(t *testing.T) {
	// Force the resolved port to 1 (guaranteed unreachable) so we don't
	// depend on any real server.
	t.Setenv("BEADS_DOLT_SERVER_PORT", "1")

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{
  "database": "beads.db",
  "dolt_mode": "server",
  "dolt_server_host": "127.0.0.1",
  "dolt_server_user": "root",
  "dolt_database": "beads"
}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should not panic. Silent exit on ping failure is the expected path.
	runCheckHealth(tmpDir)
}

func TestDoctorServerRunsForEmbeddedTarget(t *testing.T) {
	repoDir := t.TempDir()
	beadsDir := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMetadataConfig(t, beadsDir, configfile.DoltModeEmbedded, "doctor_embedded")

	origJSONOutput := jsonOutput
	origDoctorServer := doctorServer
	origServerMode := serverMode
	t.Cleanup(func() {
		jsonOutput = origJSONOutput
		doctorServer = origDoctorServer
		serverMode = origServerMode
	})
	jsonOutput = true
	doctorServer = true
	serverMode = false

	out := captureStdout(t, func() error {
		doctorCmd.Run(doctorCmd, []string{repoDir})
		return nil
	})

	var result struct {
		OverallOK bool `json:"overall_ok"`
		Checks    []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse doctor --server JSON: %v\n%s", err, out)
	}
	if !result.OverallOK {
		t.Fatalf("embedded server health should be informational, got %#v", result)
	}
	if len(result.Checks) != 1 {
		t.Fatalf("expected one server config check, got %#v", result.Checks)
	}
	check := result.Checks[0]
	if check.Name != "Server Config" || check.Status != statusOK || !strings.Contains(check.Message, "embedded") {
		t.Fatalf("unexpected embedded server check: %#v", check)
	}
}
