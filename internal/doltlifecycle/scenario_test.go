package doltlifecycle

import "testing"

func TestLifecycleScenarios(t *testing.T) {
	tests := []struct {
		name         string
		obs          Observation
		wantPrimary  State
		wantSeverity Severity
	}{
		{
			name:         "bootstrap unavailable before init",
			obs:          Observation{},
			wantPrimary:  StateUninitialized,
			wantSeverity: SeverityWarning,
		},
		{
			name: "stale server state",
			obs: Observation{
				Initialized:     true,
				Mode:            ModeServer,
				ServerReachable: false,
			},
			wantPrimary:  StateServerUnavailable,
			wantSeverity: SeverityError,
		},
		{
			name: "embedded lock contention",
			obs: Observation{
				Initialized:        true,
				Mode:               ModeEmbedded,
				EmbeddedAccessible: true,
				LockContended:      true,
			},
			wantPrimary:  StateLockContended,
			wantSeverity: SeverityError,
		},
		{
			name: "remote divergence",
			obs: Observation{
				Initialized:      true,
				Mode:             ModeServer,
				ServerReachable:  true,
				RemoteConfigured: true,
				RemoteDiverged:   true,
			},
			wantPrimary:  StateRemoteDiverged,
			wantSeverity: SeverityWarning,
		},
		{
			name: "migration retry required",
			obs: Observation{
				Initialized:        true,
				Mode:               ModeEmbedded,
				EmbeddedAccessible: true,
				MigrationRequired:  true,
			},
			wantPrimary:  StateMigrationRequired,
			wantSeverity: SeverityWarning,
		},
		{
			name: "migration failure recovery",
			obs: Observation{
				Initialized:        true,
				Mode:               ModeEmbedded,
				EmbeddedAccessible: true,
				MigrationFailed:    true,
			},
			wantPrimary:  StateMigrationFailed,
			wantSeverity: SeverityError,
		},
		{
			name: "recovery command availability",
			obs: Observation{
				Initialized:      true,
				Mode:             ModeServer,
				ServerReachable:  false,
				RecoveryRequired: true,
			},
			wantPrimary:  StateRecoveryRequired,
			wantSeverity: SeverityError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.obs)
			if got.Primary != tt.wantPrimary {
				t.Fatalf("Primary = %q, want %q; states=%v", got.Primary, tt.wantPrimary, got.States)
			}
			if got.Severity != tt.wantSeverity {
				t.Fatalf("Severity = %q, want %q", got.Severity, tt.wantSeverity)
			}
			if StateGuidance(got.Primary) == "" {
				t.Fatalf("StateGuidance(%q) is empty", got.Primary)
			}
		})
	}
}
