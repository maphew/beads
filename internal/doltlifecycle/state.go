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
	Initialized       bool
	Mode              Mode
	ServerReachable   bool
	MigrationRequired bool
	MigrationFailed   bool
	LockHeld          bool
	LockContended     bool
	RemoteConfigured  bool
	RemoteDiverged    bool
	RecoveryRequired  bool
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
			states = append(states, StateInitializedEmbedded)
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
	case StateRecoveryRequired, StateMigrationFailed, StateLockContended, StateServerUnavailable:
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
