package doltinspect

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/storage/doltutil"
)

type Service struct{}

func NewService() Service {
	return Service{}
}

type StatusRequest struct {
	BeadsDir string
	Config   *configfile.Config
	Embedded bool
}

type StatusMode string

const (
	StatusModeEmbedded StatusMode = "embedded"
	StatusModeExternal StatusMode = "external"
	StatusModeManaged  StatusMode = "managed"
)

type StatusResult struct {
	Mode         StatusMode
	Embedded     EmbeddedStatusResult
	External     ExternalStatusResult
	Managed      *doltserver.State
	ExpectedPort int
	LogPath      string
	SharedServer bool
}

type ExternalStatusResult struct {
	Mode     string `json:"mode"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Database string `json:"database"`
	TLS      bool   `json:"tls"`
	Running  bool   `json:"running"`
	Version  string `json:"version,omitempty"`
	Error    string `json:"error,omitempty"`
}

type EmbeddedStatusResult struct {
	Mode          string `json:"mode"`
	ServerRunning bool   `json:"server_running"`
	DataDir       string `json:"data_dir"`
	DataDirExists bool   `json:"data_dir_exists"`
}

func (Service) Status(ctx context.Context, req StatusRequest) (StatusResult, error) {
	if req.Embedded {
		return StatusResult{
			Mode:     StatusModeEmbedded,
			Embedded: InspectEmbeddedStatus(req.BeadsDir),
		}, nil
	}

	if req.Config != nil && ShouldUseExternalEndpoint(req.BeadsDir, req.Config) {
		return StatusResult{
			Mode:     StatusModeExternal,
			External: InspectExternalStatus(ctx, req.BeadsDir, req.Config),
		}, nil
	}

	serverDir := doltserver.ResolveServerDir(req.BeadsDir)
	state, err := doltserver.IsRunning(serverDir)
	if err != nil {
		return StatusResult{}, err
	}
	return StatusResult{
		Mode:         StatusModeManaged,
		Managed:      state,
		ExpectedPort: doltserver.DefaultConfig(serverDir).Port,
		LogPath:      doltserver.LogPath(serverDir),
		SharedServer: doltserver.IsSharedServerMode(),
	}, nil
}

func InspectExternalStatus(ctx context.Context, beadsDir string, cfg *configfile.Config) ExternalStatusResult {
	host := cfg.GetDoltServerHost()
	port := doltserver.DefaultConfig(beadsDir).Port
	user := cfg.GetDoltServerUser()
	database := cfg.GetDoltDatabase()
	tls := cfg.GetDoltServerTLS()
	password := cfg.GetDoltServerPasswordForPort(port)

	dsn := doltutil.ServerDSN{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		TLS:      tls,
		Timeout:  5 * time.Second,
	}.String()

	result := ExternalStatusResult{
		Mode:     "external",
		Host:     host,
		Port:     port,
		User:     user,
		Database: database,
		TLS:      tls,
	}

	db, openErr := sql.Open("mysql", dsn)
	if openErr != nil {
		result.Error = openErr.Error()
		return result
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if pingErr := db.PingContext(ctx); pingErr != nil {
		result.Error = pingErr.Error()
		return result
	}

	result.Running = true
	// Best-effort version lookup; don't treat errors as fatal.
	_ = db.QueryRowContext(ctx, "SELECT @@version").Scan(&result.Version)
	return result
}

func InspectEmbeddedStatus(beadsDir string) EmbeddedStatusResult {
	dataDir := filepath.Join(beadsDir, "embeddeddolt")
	dataDirExists := false
	if info, err := os.Stat(dataDir); err == nil && info.IsDir() {
		dataDirExists = true
	}

	return EmbeddedStatusResult{
		Mode: "embedded",
		// Embedded mode has an active in-process engine, but no separate
		// server process. Use a server-specific field so clients do not read
		// running=false as "Dolt is unavailable".
		ServerRunning: false,
		DataDir:       dataDir,
		DataDirExists: dataDirExists,
	}
}

type ShowConfigRequest struct {
	BeadsDir        string
	Config          *configfile.Config
	Embedded        bool
	TestConnection  bool
	ConnectionCheck func(host string, port int) bool
}

type ShowConfigResult struct {
	Backend           string
	Database          string
	Embedded          bool
	DataDir           string
	Host              string
	Port              int
	User              string
	SharedServer      bool
	ExternalServer    bool
	ConnectionChecked bool
	ConnectionOK      bool
}

func (Service) ShowConfig(req ShowConfigRequest) ShowConfigResult {
	cfg := req.Config
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	result := ShowConfigResult{
		Backend: cfg.GetBackend(),
	}
	if result.Backend != configfile.BackendDolt {
		return result
	}

	result.Database = cfg.GetDoltDatabase()
	result.Embedded = req.Embedded
	if result.Embedded {
		result.DataDir = filepath.Join(req.BeadsDir, "embeddeddolt")
		return result
	}

	result.Host = cfg.GetDoltServerHost()
	result.Port = doltserver.DefaultConfig(req.BeadsDir).Port
	result.User = cfg.GetDoltServerUser()
	result.SharedServer = doltserver.IsSharedServerMode()
	result.ExternalServer = ShouldUseExternalEndpoint(req.BeadsDir, cfg)
	if req.TestConnection {
		result.ConnectionChecked = true
		if req.ConnectionCheck != nil {
			result.ConnectionOK = req.ConnectionCheck(result.Host, result.Port)
		}
	}
	return result
}

func IsLocalHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return true // empty defaults to local
	}
	switch h {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	return false
}

func ShouldUseExternalEndpoint(beadsDir string, cfg *configfile.Config) bool {
	if cfg == nil || !cfg.IsDoltServerMode() {
		return false
	}
	if doltserver.DefaultConfig(beadsDir).Mode == doltserver.ServerModeExternal {
		return true
	}
	return !IsLocalHost(cfg.GetDoltServerHost())
}
