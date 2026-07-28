package main

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func parseDepSpecs(deps []string) ([]domain.DependencySpec, error) {
	var out []domain.DependencySpec
	for _, raw := range deps {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		spec, err := parseDepSpec(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, spec)
	}
	return out, nil
}

func parseDepSpec(raw string) (domain.DependencySpec, error) {
	if !strings.Contains(raw, ":") {
		return domain.DependencySpec{
			Type:     types.DepBlocks,
			TargetID: raw,
		}, nil
	}

	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return domain.DependencySpec{}, fmt.Errorf("invalid dependency format %q, expected 'type:id' or 'id'", raw)
	}
	rawType := types.DependencyType(strings.TrimSpace(parts[0]))
	target := strings.TrimSpace(parts[1])

	spec := domain.DependencySpec{TargetID: target, Type: canonicalDependencyType(rawType)}
	if rawType == types.DepBlocks {
		// Explicit "blocks:" (as opposed to the "depends-on"/"blocked-by"
		// aliases, which keep direction) reverses direction: the target
		// depends on the issue being created, not the other way around.
		spec.SwapDirection = true
	}

	if err := validateDependencyType(spec.Type); err != nil {
		return domain.DependencySpec{}, err
	}
	return spec, nil
}

// canonicalDependencyType maps the documented alias spellings "depends-on"
// and "blocked-by" to the canonical "blocks" storage type that ready/blocked
// gating checks (types.DependencyType.AffectsReadyWork). Both `bd create
// --deps` and `bd dep add --type` share this mapping so an alias always
// gates the same way as the canonical type — see GH#5069, where `bd dep add
// --type blocked-by` stored the literal string "blocked-by" and silently
// never gated `bd ready`/`bd blocked`, even though `--help` itself documents
// "blocked-by" as the flag name. Any other value (including well-known types
// like "parent-child" and custom types) passes through unchanged.
func canonicalDependencyType(t types.DependencyType) types.DependencyType {
	switch t {
	case "depends-on", "blocked-by":
		return types.DepBlocks
	default:
		return t
	}
}

// validateDependencyType enforces that a (post-alias-normalization)
// dependency type is both structurally valid and one of the well-known
// built-in types. `bd create --deps` and `bd dep add --type` intentionally
// reject custom/unknown dependency types (see the comment on
// WellKnownDependencyTypes) — this is the single shared check so both
// commands stay in lockstep.
func validateDependencyType(t types.DependencyType) error {
	if !t.IsValid() {
		return fmt.Errorf("invalid dependency type %q (must be non-empty, max 50 chars); valid types: %s",
			t, createDepsAcceptedTypeList())
	}
	if !t.IsWellKnown() {
		return fmt.Errorf("unknown dependency type %q; valid types: %s",
			t, createDepsAcceptedTypeList())
	}
	return nil
}

// buildWaitsFor validates and constructs a WaitsForSpec from the --waits-for
// and --waits-for-gate flag values. gateExplicit must be true when the caller
// explicitly passed --waits-for-gate (not relying on its default); in that case
// a missing spawnerID is rejected rather than silently ignored.
func buildWaitsFor(spawnerID, gate string, gateExplicit bool) (*domain.WaitsForSpec, error) {
	spawnerID = strings.TrimSpace(spawnerID)
	if spawnerID == "" {
		if gateExplicit {
			return nil, fmt.Errorf("--waits-for-gate requires --waits-for (no spawner ID specified)")
		}
		return nil, nil
	}
	if gate == "" {
		gate = types.WaitsForAllChildren
	}
	if !types.IsValidWaitsForGate(gate) {
		return nil, fmt.Errorf("invalid --waits-for-gate value %q (valid: all-children, any-children)", gate)
	}
	return &domain.WaitsForSpec{SpawnerID: spawnerID, Gate: gate}, nil
}

func discoveredFromParent(deps []string) string {
	for _, raw := range deps {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.Contains(raw, ":") {
			continue
		}
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			continue
		}
		depType := types.DependencyType(strings.TrimSpace(parts[0]))
		target := strings.TrimSpace(parts[1])
		if depType == types.DepDiscoveredFrom && target != "" {
			return target
		}
	}
	return ""
}

func overlayYAMLPrefix(dbPrefix string) string {
	if v := strings.TrimSpace(config.GetString("issue-prefix")); v != "" {
		return v
	}
	return dbPrefix
}
