package doltinspect

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

func TestStatusEmbeddedJSONContract(t *testing.T) {
	beadsDir := t.TempDir()
	dataDir := filepath.Join(beadsDir, "embeddeddolt")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}

	result, err := NewService().Status(context.Background(), StatusRequest{
		BeadsDir: beadsDir,
		Embedded: true,
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if result.Mode != StatusModeEmbedded {
		t.Fatalf("Mode = %q, want %q", result.Mode, StatusModeEmbedded)
	}
	if result.Embedded.DataDir != dataDir {
		t.Fatalf("DataDir = %q, want %q", result.Embedded.DataDir, dataDir)
	}

	data, err := json.Marshal(result.Embedded)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["running"]; ok {
		t.Fatalf("embedded status JSON should omit generic running field: %s", data)
	}
	if raw["server_running"] != false {
		t.Fatalf("server_running = %v, want false", raw["server_running"])
	}
}

func TestShowConfigUsesTypedConnectionCheck(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_PORT", "4407")

	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendDolt
	cfg.DoltMode = configfile.DoltModeServer
	cfg.DoltServerHost = "dolt.example.com"
	cfg.DoltServerUser = "root"
	cfg.DoltDatabase = "beads_ext"

	var checkedHost string
	var checkedPort int
	result := NewService().ShowConfig(ShowConfigRequest{
		BeadsDir:       t.TempDir(),
		Config:         cfg,
		TestConnection: true,
		ConnectionCheck: func(host string, port int) bool {
			checkedHost = host
			checkedPort = port
			return true
		},
	})

	if !result.ExternalServer {
		t.Fatal("ExternalServer should be true for non-local Dolt host")
	}
	if !result.ConnectionChecked || !result.ConnectionOK {
		t.Fatalf("connection result = checked:%v ok:%v, want checked:true ok:true", result.ConnectionChecked, result.ConnectionOK)
	}
	if checkedHost != "dolt.example.com" || checkedPort != 4407 {
		t.Fatalf("connection check target = %s:%d, want dolt.example.com:4407", checkedHost, checkedPort)
	}
}
