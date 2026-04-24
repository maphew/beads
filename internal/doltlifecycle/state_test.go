package doltlifecycle

import (
	"slices"
	"testing"
)

func TestEvaluateBaseStates(t *testing.T) {
	tests := []struct {
		name string
		obs  Observation
		want State
	}{
		{
			name: "uninitialized",
			obs:  Observation{},
			want: StateUninitialized,
		},
		{
			name: "initialized embedded",
			obs:  Observation{Initialized: true, Mode: ModeEmbedded},
			want: StateInitializedEmbedded,
		},
		{
			name: "initialized server reachable",
			obs:  Observation{Initialized: true, Mode: ModeServer, ServerReachable: true},
			want: StateInitializedServer,
		},
		{
			name: "server unavailable",
			obs:  Observation{Initialized: true, Mode: ModeServer},
			want: StateServerUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.obs)
			if got.Primary != tt.want {
				t.Fatalf("Primary = %q, want %q; states=%v", got.Primary, tt.want, got.States)
			}
			if !slices.Contains(got.States, tt.want) {
				t.Fatalf("states = %v, want to contain %q", got.States, tt.want)
			}
		})
	}
}

func TestEvaluateKeepsSecondaryStates(t *testing.T) {
	snapshot := Evaluate(Observation{
		Initialized:      true,
		Mode:             ModeServer,
		ServerReachable:  true,
		RemoteConfigured: true,
		RemoteDiverged:   true,
	})

	if snapshot.Primary != StateRemoteDiverged {
		t.Fatalf("Primary = %q, want %q", snapshot.Primary, StateRemoteDiverged)
	}
	for _, want := range []State{StateInitializedServer, StateRemoteConfigured, StateRemoteDiverged} {
		if !slices.Contains(snapshot.States, want) {
			t.Fatalf("states = %v, want to contain %q", snapshot.States, want)
		}
	}
}

func TestEvaluatePrimaryPrecedence(t *testing.T) {
	snapshot := Evaluate(Observation{
		Initialized:       true,
		Mode:              ModeServer,
		ServerReachable:   false,
		MigrationRequired: true,
		MigrationFailed:   true,
		LockContended:     true,
		RemoteDiverged:    true,
		RecoveryRequired:  true,
	})

	if snapshot.Primary != StateRecoveryRequired {
		t.Fatalf("Primary = %q, want %q", snapshot.Primary, StateRecoveryRequired)
	}
	if snapshot.Severity != SeverityError {
		t.Fatalf("Severity = %q, want %q", snapshot.Severity, SeverityError)
	}
}

func TestStateMetadataCoversAllStates(t *testing.T) {
	for _, state := range []State{
		StateUninitialized,
		StateInitializedEmbedded,
		StateInitializedServer,
		StateServerUnavailable,
		StateMigrationRequired,
		StateMigrationFailed,
		StateLockHeld,
		StateLockContended,
		StateRemoteConfigured,
		StateRemoteDiverged,
		StateRecoveryRequired,
	} {
		if StateDescription(state) == "" {
			t.Fatalf("StateDescription(%q) is empty", state)
		}
		if StateSeverity(state) == "" {
			t.Fatalf("StateSeverity(%q) is empty", state)
		}
	}
}
