package doltlifecycle

type Mode string

const (
	ModeUnknown  Mode = "unknown"
	ModeEmbedded Mode = "embedded"
	ModeServer   Mode = "server"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type State string

const (
	StateUninitialized       State = "uninitialized"
	StateInitializedEmbedded State = "initialized_embedded"
	StateEmbeddedUnavailable State = "embedded_unavailable"
	StateInitializedServer   State = "initialized_server"
	StateServerUnavailable   State = "server_unavailable"
	StateMigrationRequired   State = "migration_required"
	StateMigrationFailed     State = "migration_failed"
	StateLockHeld            State = "lock_held"
	StateLockContended       State = "lock_contended"
	StateRemoteConfigured    State = "remote_configured"
	StateRemoteDiverged      State = "remote_diverged"
	StateRecoveryRequired    State = "recovery_required"
)

type Observation struct {
	Initialized        bool
	Mode               Mode
	EmbeddedAccessible bool
	ServerReachable    bool
	MigrationRequired  bool
	MigrationFailed    bool
	LockHeld           bool
	LockContended      bool
	RemoteConfigured   bool
	RemoteDiverged     bool
	RecoveryRequired   bool
}

type Snapshot struct {
	Primary  State
	States   []State
	Severity Severity
}

func Evaluate(obs Observation) Snapshot {
	states := make([]State, 0, 4)

	if !obs.Initialized {
		states = append(states, StateUninitialized)
	} else {
		switch obs.Mode {
		case ModeEmbedded:
			if obs.EmbeddedAccessible {
				states = append(states, StateInitializedEmbedded)
			} else {
				states = append(states, StateEmbeddedUnavailable)
			}
		case ModeServer:
			if obs.ServerReachable {
				states = append(states, StateInitializedServer)
			} else {
				states = append(states, StateServerUnavailable)
			}
		default:
			states = append(states, StateUninitialized)
		}
	}

	if obs.MigrationRequired {
		states = append(states, StateMigrationRequired)
	}
	if obs.MigrationFailed {
		states = append(states, StateMigrationFailed)
	}
	if obs.LockHeld {
		states = append(states, StateLockHeld)
	}
	if obs.LockContended {
		states = append(states, StateLockContended)
	}
	if obs.RemoteConfigured {
		states = append(states, StateRemoteConfigured)
	}
	if obs.RemoteDiverged {
		states = append(states, StateRemoteDiverged)
	}
	if obs.RecoveryRequired {
		states = append(states, StateRecoveryRequired)
	}

	primary := primaryState(states)
	return Snapshot{
		Primary:  primary,
		States:   states,
		Severity: StateSeverity(primary),
	}
}

func primaryState(states []State) State {
	for _, candidate := range []State{
		StateRecoveryRequired,
		StateMigrationFailed,
		StateMigrationRequired,
		StateLockContended,
		StateEmbeddedUnavailable,
		StateServerUnavailable,
		StateRemoteDiverged,
		StateLockHeld,
		StateUninitialized,
		StateInitializedEmbedded,
		StateInitializedServer,
		StateRemoteConfigured,
	} {
		for _, state := range states {
			if state == candidate {
				return candidate
			}
		}
	}
	return StateUninitialized
}

func StateSeverity(state State) Severity {
	switch state {
	case StateRecoveryRequired, StateMigrationFailed, StateLockContended, StateEmbeddedUnavailable, StateServerUnavailable:
		return SeverityError
	case StateMigrationRequired, StateRemoteDiverged, StateLockHeld, StateUninitialized:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

func StateDescription(state State) string {
	switch state {
	case StateUninitialized:
		return "workspace has no initialized Beads/Dolt data"
	case StateInitializedEmbedded:
		return "workspace is initialized for embedded Dolt"
	case StateEmbeddedUnavailable:
		return "workspace is configured for embedded Dolt but the embedded data directory is unavailable"
	case StateInitializedServer:
		return "workspace is initialized for server-backed Dolt and the server is reachable"
	case StateServerUnavailable:
		return "workspace is configured for server-backed Dolt but the server is unavailable"
	case StateMigrationRequired:
		return "database schema or runtime files require migration"
	case StateMigrationFailed:
		return "a previous migration failed and needs repair"
	case StateLockHeld:
		return "current process holds the embedded Dolt lock"
	case StateLockContended:
		return "another process holds the embedded Dolt lock"
	case StateRemoteConfigured:
		return "a Dolt remote is configured"
	case StateRemoteDiverged:
		return "local and remote Dolt histories have diverged"
	case StateRecoveryRequired:
		return "runtime state requires recovery before normal operation"
	default:
		return "unknown Dolt lifecycle state"
	}
}

func StateGuidance(state State) string {
	switch state {
	case StateUninitialized:
		return "Run bd bootstrap for existing-project recovery, or bd init only for a brand-new project."
	case StateInitializedEmbedded:
		return "Embedded Dolt is initialized; no server process is expected."
	case StateEmbeddedUnavailable:
		return "Check .beads/embeddeddolt permissions and contents, then restore from backup or run bd bootstrap if recovery is needed."
	case StateInitializedServer:
		return "Dolt server is reachable; inspect dolt_status or doctor output for remaining issues."
	case StateServerUnavailable:
		return "Run bd dolt status to inspect the configured server, then bd dolt start if this project owns the server."
	case StateMigrationRequired:
		return "Run bd doctor --fix to apply required migrations."
	case StateMigrationFailed:
		return "Run bd doctor --fix after reviewing the migration error; restore from backup if repair fails."
	case StateLockHeld:
		return "Current process owns the embedded Dolt lock."
	case StateLockContended:
		return "Wait for the other bd process to finish, or stop it if it is stale."
	case StateRemoteConfigured:
		return "A Dolt remote is configured; use bd bootstrap --dry-run to confirm recovery or sync plans."
	case StateRemoteDiverged:
		return "Run bd dolt pull or bd doctor for divergence details before pushing."
	case StateRecoveryRequired:
		return "Run bd bootstrap first; use bd backup restore only if bootstrap cannot recover automatically."
	default:
		return "Run bd doctor for diagnostics."
	}
}
