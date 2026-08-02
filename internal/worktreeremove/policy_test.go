package worktreeremove

import (
	"strings"
	"testing"
)

func TestPrepareRefusesUnsafeTargetsInExistingOrder(t *testing.T) {
	base := prepareFacts()
	for _, tt := range []struct {
		name string
		set  func(*PrepareFacts)
		want string
	}{
		{"primary", func(f *PrepareFacts) { f.Target = PrimaryWorktree }, "cannot remove the primary worktree"},
		{"current", func(f *PrepareFacts) { f.Target = CurrentWorktree }, "cannot remove the worktree containing the running command"},
		{"common directory", func(f *PrepareFacts) { f.CommonDir = Unmatched }, "target common git directory"},
		{"missing registered path", func(f *PrepareFacts) { f.RegisteredPath = "" }, "registered worktree target"},
		{"missing cleanup identity", func(f *PrepareFacts) { f.ManagedIgnore, f.ManagedIgnoreEntry = IgnoreManaged, "" }, "managed ignore cleanup identity"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			facts := base
			tt.set(&facts)
			_, err := Prepare(Request{Mode: Normal}, facts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Prepare() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPrepareCleanlinessAndContainmentRespectForce(t *testing.T) {
	for _, tt := range []struct {
		name string
		req  Request
		set  func(*PrepareFacts)
		want string
	}{
		{"dirty refused", Request{Mode: Normal}, func(f *PrepareFacts) { f.Status = Dirty }, "modified, untracked, or ignored"},
		{"dirty forced", Request{Mode: Force}, func(f *PrepareFacts) {
			f.Status, f.Comparator, f.Containment = Dirty, ComparatorNotRequired, ContainmentNotRequired
		}, ""},
		{"comparator required", Request{Mode: Normal}, func(f *PrepareFacts) { f.Comparator = ComparatorMissing }, "cannot verify unpushed commits"},
		{"not contained", Request{Mode: Normal}, func(f *PrepareFacts) { f.Containment = NotContained }, "not contained"},
		{"force marks comparison not required", Request{Mode: Force}, func(f *PrepareFacts) { f.Comparator, f.Containment = ComparatorNotRequired, ContainmentNotRequired }, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			facts := prepareFacts()
			tt.set(&facts)
			_, err := Prepare(tt.req, facts)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Prepare() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Prepare() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPrepareRefusesZeroValueOrIncompleteFacts(t *testing.T) {
	if _, err := Prepare(Request{}, PrepareFacts{}); err == nil {
		t.Fatal("Prepare() accepted zero-value request and facts")
	}
	for _, tt := range []struct {
		name string
		set  func(*PrepareFacts)
	}{
		{"registration", func(f *PrepareFacts) { f.Registration = PresenceUnknown }},
		{"target", func(f *PrepareFacts) { f.Target = TargetUnknown }},
		{"registered path", func(f *PrepareFacts) { f.RegisteredPath = "" }},
		{"target directory", func(f *PrepareFacts) { f.TargetDir = PresenceUnknown }},
		{"git admin directory", func(f *PrepareFacts) { f.GitAdminDir = PresenceUnknown }},
		{"git marker", func(f *PrepareFacts) { f.GitMarker = PresenceUnknown }},
		{"common dir", func(f *PrepareFacts) { f.CommonDir = MatchUnknown }},
		{"head", func(f *PrepareFacts) { f.Head = PresenceUnknown }},
		{"status", func(f *PrepareFacts) { f.Status = StatusUnknown }},
		{"managed ignore", func(f *PrepareFacts) { f.ManagedIgnore = IgnoreUnknown }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			facts := prepareFacts()
			tt.set(&facts)
			if _, err := Prepare(Request{Mode: Force}, facts); err == nil {
				t.Fatal("Prepare() accepted incomplete facts in force mode")
			}
		})
	}
}

func TestRevalidateRefusesZeroUnknownAndChangedInvariant(t *testing.T) {
	plan, err := Prepare(Request{Mode: Normal}, prepareFacts())
	if err != nil {
		t.Fatal(err)
	}
	if err := Revalidate(Plan{}, revalidationFacts(Normal)); err == nil {
		t.Fatal("Revalidate() accepted zero plan")
	}
	for _, tt := range []struct {
		name string
		set  func(*RevalidationFacts, InvariantState)
	}{
		{"registration", func(f *RevalidationFacts, s InvariantState) { f.Registration = s }},
		{"lock prune", func(f *RevalidationFacts, s InvariantState) { f.LockPrune = s }},
		{"target path", func(f *RevalidationFacts, s InvariantState) { f.TargetPath = s }},
		{"target directory", func(f *RevalidationFacts, s InvariantState) { f.TargetDirectory = s }},
		{"git directory", func(f *RevalidationFacts, s InvariantState) { f.GitAdminDirectory = s }},
		{"git directory bytes", func(f *RevalidationFacts, s InvariantState) { f.GitAdminDirectoryBytes = s }},
		{"git marker", func(f *RevalidationFacts, s InvariantState) { f.GitMarker = s }},
		{"git marker bytes", func(f *RevalidationFacts, s InvariantState) { f.GitMarkerBytes = s }},
		{"common directory", func(f *RevalidationFacts, s InvariantState) { f.CommonDirectory = s }},
		{"head", func(f *RevalidationFacts, s InvariantState) { f.Head = s }},
		{"cleanliness", func(f *RevalidationFacts, s InvariantState) { f.Cleanliness = s }},
		{"status bytes", func(f *RevalidationFacts, s InvariantState) { f.StatusBytes = s }},
		{"dirty fingerprint", func(f *RevalidationFacts, s InvariantState) { f.DirtyFileFingerprint = s }},
		{"comparator", func(f *RevalidationFacts, s InvariantState) { f.Comparator = s }},
		{"containment", func(f *RevalidationFacts, s InvariantState) { f.Containment = s }},
		{"managed ignore", func(f *RevalidationFacts, s InvariantState) { f.ManagedIgnore = s }},
	} {
		for _, state := range []InvariantState{InvariantUnknown, InvariantChanged} {
			t.Run(tt.name+"/"+invariantName(state), func(t *testing.T) {
				facts := revalidationFacts(Normal)
				tt.set(&facts, state)
				if err := Revalidate(plan, facts); err == nil {
					t.Fatal("Revalidate() accepted incomplete or changed invariant")
				}
			})
		}
	}
	facts := revalidationFacts(Force)
	facts.Head = InvariantNotRequired
	if err := Revalidate(mustPrepare(t, Request{Mode: Force}, forcePrepareFacts()), facts); err == nil {
		t.Fatal("Revalidate() accepted an unobserved required force invariant")
	}
}

func TestClassifyFailureUsesRawPostFailureEvidence(t *testing.T) {
	plan, err := Prepare(Request{Mode: Normal}, prepareFacts())
	if err != nil {
		t.Fatal(err)
	}
	removeErr := assertErr{}
	stable := FailureFacts{Revalidation: revalidationFacts(Normal), RevalidationResult: RevalidationPassed, Registration: Present, TargetPath: Present}
	if got, err := ClassifyFailure(plan, stable, removeErr); err != nil || got.Kind != UnchangedFailure {
		t.Fatalf("stable failure = (%#v, %v), want unchanged", got, err)
	}
	for _, facts := range []FailureFacts{
		{Revalidation: revalidationFacts(Normal), Registration: Missing, TargetPath: Present},
		{Revalidation: revalidationFacts(Normal), Registration: Present, TargetPath: Missing},
		{Revalidation: revalidationFacts(Normal), RevalidationResult: RevalidationFailed, Registration: Present, TargetPath: Present},
		func() FailureFacts { f := stable; f.Revalidation.Head = InvariantUnknown; return f }(),
	} {
		if got, err := ClassifyFailure(plan, facts, removeErr); err != nil || got.Kind != PartialFailure {
			t.Fatalf("indeterminate failure = (%#v, %v), want partial", got, err)
		}
	}
	if _, err := ClassifyFailure(Plan{}, stable, removeErr); err == nil {
		t.Fatal("ClassifyFailure() accepted zero plan")
	}
}

func prepareFacts() PrepareFacts {
	return PrepareFacts{Registration: Present, Target: RegisteredTarget, RegisteredPath: "/exact/registry/path", TargetDir: Present, GitAdminDir: Present, GitMarker: Present, CommonDir: Matched, Head: Present, Status: Clean, Comparator: ComparatorAvailable, Containment: Contained, ManagedIgnore: IgnoreAbsent}
}

func forcePrepareFacts() PrepareFacts {
	facts := prepareFacts()
	facts.Comparator, facts.Containment = ComparatorNotRequired, ContainmentNotRequired
	return facts
}

func mustPrepare(t *testing.T, request Request, facts PrepareFacts) Plan {
	t.Helper()
	plan, err := Prepare(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func revalidationFacts(mode Mode) RevalidationFacts {
	facts := RevalidationFacts{Registration: InvariantStable, LockPrune: InvariantStable, TargetPath: InvariantStable, TargetDirectory: InvariantStable, GitAdminDirectory: InvariantStable, GitAdminDirectoryBytes: InvariantStable, GitMarker: InvariantStable, GitMarkerBytes: InvariantStable, CommonDirectory: InvariantStable, Head: InvariantStable, Cleanliness: InvariantStable, StatusBytes: InvariantStable, DirtyFileFingerprint: InvariantStable, Comparator: InvariantStable, Containment: InvariantStable, ManagedIgnore: InvariantStable}
	if mode == Force {
		facts.Comparator, facts.Containment = InvariantNotRequired, InvariantNotRequired
	}
	return facts
}

func invariantName(state InvariantState) string {
	if state == InvariantUnknown {
		return "unknown"
	}
	return "changed"
}

type assertErr struct{}

func (assertErr) Error() string { return "remove failed" }
