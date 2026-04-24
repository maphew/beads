package doltinspect

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltlifecycle"
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
	Lifecycle    doltlifecycle.Snapshot
}

type ExternalStatusResult struct {
	Mode              string                 `json:"mode"`
	Host              string                 `json:"host"`
	Port              int                    `json:"port"`
	User              string                 `json:"user"`
	Database          string                 `json:"database"`
	TLS               bool                   `json:"tls"`
	Running           bool                   `json:"running"`
	Version           string                 `json:"version,omitempty"`
	Error             string                 `json:"error,omitempty"`
	LifecycleState    doltlifecycle.State    `json:"lifecycle_state"`
	LifecycleStates   []doltlifecycle.State  `json:"lifecycle_states"`
	LifecycleSeverity doltlifecycle.Severity `json:"lifecycle_severity"`
	LifecycleGuidance string                 `json:"lifecycle_guidance"`
}

type EmbeddedStatusResult struct {
	Mode              string                 `json:"mode"`
	ServerRunning     bool                   `json:"server_running"`
	DataDir           string                 `json:"data_dir"`
	DataDirExists     bool                   `json:"data_dir_exists"`
	LifecycleState    doltlifecycle.State    `json:"lifecycle_state"`
	LifecycleStates   []doltlifecycle.State  `json:"lifecycle_states"`
	LifecycleSeverity doltlifecycle.Severity `json:"lifecycle_severity"`
	LifecycleGuidance string                 `json:"lifecycle_guidance"`
}

func (Service) Status(ctx context.Context, req StatusRequest) (StatusResult, error) {
	if req.Embedded {
		embedded := InspectEmbeddedStatus(req.BeadsDir)
		return StatusResult{
			Mode:     StatusModeEmbedded,
			Embedded: embedded,
			Lifecycle: doltlifecycle.Snapshot{
				Primary:  embedded.LifecycleState,
				States:   embedded.LifecycleStates,
				Severity: embedded.LifecycleSeverity,
			},
		}, nil
	}

	if req.Config != nil && ShouldUseExternalEndpoint(req.BeadsDir, req.Config) {
		external := InspectExternalStatus(ctx, req.BeadsDir, req.Config)
		return StatusResult{
			Mode:     StatusModeExternal,
			External: external,
			Lifecycle: doltlifecycle.Snapshot{
				Primary:  external.LifecycleState,
				States:   external.LifecycleStates,
				Severity: external.LifecycleSeverity,
			},
		}, nil
	}

	serverDir := doltserver.ResolveServerDir(req.BeadsDir)
	state, err := doltserver.IsRunning(serverDir)
	if err != nil {
		return StatusResult{}, err
	}
	lifecycle := doltlifecycle.Evaluate(doltlifecycle.Observation{
		Initialized:     true,
		Mode:            doltlifecycle.ModeServer,
		ServerReachable: state != nil && state.Running,
	})
	return StatusResult{
		Mode:         StatusModeManaged,
		Managed:      state,
		ExpectedPort: doltserver.DefaultConfig(serverDir).Port,
		LogPath:      doltserver.LogPath(serverDir),
		SharedServer: doltserver.IsSharedServerMode(),
		Lifecycle:    lifecycle,
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
		setExternalLifecycle(&result)
		return result
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if pingErr := db.PingContext(ctx); pingErr != nil {
		result.Error = pingErr.Error()
		setExternalLifecycle(&result)
		return result
	}

	result.Running = true
	// Best-effort version lookup; don't treat errors as fatal.
	_ = db.QueryRowContext(ctx, "SELECT @@version").Scan(&result.Version)
	setExternalLifecycle(&result)
	return result
}

func setExternalLifecycle(result *ExternalStatusResult) {
	lifecycle := doltlifecycle.Evaluate(doltlifecycle.Observation{
		Initialized:     true,
		Mode:            doltlifecycle.ModeServer,
		ServerReachable: result.Running,
	})
	result.LifecycleState = lifecycle.Primary
	result.LifecycleStates = lifecycle.States
	result.LifecycleSeverity = lifecycle.Severity
	result.LifecycleGuidance = doltlifecycle.StateGuidance(lifecycle.Primary)
}

func InspectEmbeddedStatus(beadsDir string) EmbeddedStatusResult {
	dataDir := filepath.Join(beadsDir, "embeddeddolt")
	dataDirExists := false
	if info, err := os.Stat(dataDir); err == nil && info.IsDir() {
		dataDirExists = true
	}

	lifecycle := doltlifecycle.Evaluate(doltlifecycle.Observation{
		Initialized: true,
		Mode:        doltlifecycle.ModeEmbedded,
	})
	return EmbeddedStatusResult{
		Mode: "embedded",
		// Embedded mode has an active in-process engine, but no separate
		// server process. Use a server-specific field so clients do not read
		// running=false as "Dolt is unavailable".
		ServerRunning:     false,
		DataDir:           dataDir,
		DataDirExists:     dataDirExists,
		LifecycleState:    lifecycle.Primary,
		LifecycleStates:   lifecycle.States,
		LifecycleSeverity: lifecycle.Severity,
		LifecycleGuidance: doltlifecycle.StateGuidance(lifecycle.Primary),
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
	Lifecycle         doltlifecycle.Snapshot
	LifecycleGuidance string
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
		result.Lifecycle = doltlifecycle.Evaluate(doltlifecycle.Observation{
			Initialized: true,
			Mode:        doltlifecycle.ModeEmbedded,
		})
		result.LifecycleGuidance = doltlifecycle.StateGuidance(result.Lifecycle.Primary)
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
	result.Lifecycle = doltlifecycle.Evaluate(doltlifecycle.Observation{
		Initialized:      true,
		Mode:             doltlifecycle.ModeServer,
		ServerReachable:  !result.ConnectionChecked || result.ConnectionOK,
		RemoteConfigured: result.ExternalServer,
	})
	result.LifecycleGuidance = doltlifecycle.StateGuidance(result.Lifecycle.Primary)
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
