package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/worktreeremove"
)

func TestWorktreeRemovalCommandRecordsApprovedOperations(t *testing.T) {
	observer := &recordingWorktreeRemovalObserver{
		prepare: worktreeremove.PrepareFacts{
			Registration:       worktreeremove.Present,
			Target:             worktreeremove.RegisteredTarget,
			RegisteredPath:     "/exact/registry/path",
			TargetDir:          worktreeremove.Present,
			GitAdminDir:        worktreeremove.Present,
			GitMarker:          worktreeremove.Present,
			CommonDir:          worktreeremove.Matched,
			Head:               worktreeremove.Present,
			Status:             worktreeremove.Clean,
			Comparator:         worktreeremove.ComparatorNotRequired,
			Containment:        worktreeremove.ContainmentNotRequired,
			ManagedIgnore:      worktreeremove.IgnoreManaged,
			ManagedIgnoreEntry: "nested/lane",
		},
		revalidation: []worktreeremove.RevalidationFacts{worktreeRemovalRevalidationFacts(worktreeremove.Force)},
	}
	mutator := &recordingWorktreeRemovalMutator{}
	presenter := &recordingWorktreeRemovalPresenter{}

	err := runWorktreeRemovalOrchestration(
		context.Background(),
		worktreeRemovalRequest{mode: worktreeremove.Force},
		observer,
		mutator,
		presenter,
	)
	if err != nil {
		t.Fatalf("runWorktreeRemovalOrchestration() error = %v", err)
	}

	if want := []string{"prepare", "revalidate"}; !reflect.DeepEqual(observer.operations, want) {
		t.Fatalf("observer operations = %#v, want %#v", observer.operations, want)
	}
	if want := []worktreeremove.Mutation{{TargetPath: "/exact/registry/path", Force: true}}; !reflect.DeepEqual(mutator.removals, want) {
		t.Fatalf("removals = %#v, want %#v", mutator.removals, want)
	}
	if want := []worktreeremove.Cleanup{{Entry: "nested/lane"}}; !reflect.DeepEqual(mutator.cleanups, want) {
		t.Fatalf("cleanups = %#v, want %#v", mutator.cleanups, want)
	}
	if want := []string{"removed"}; !reflect.DeepEqual(presenter.events, want) {
		t.Fatalf("presentation = %#v, want %#v", presenter.events, want)
	}
}

func TestWorktreeRemovalCommandRefusesBeforeMutation(t *testing.T) {
	observer := &recordingWorktreeRemovalObserver{
		prepare: worktreeremove.PrepareFacts{
			Registration:   worktreeremove.Present,
			Target:         worktreeremove.PrimaryWorktree,
			RegisteredPath: "/exact/registry/path",
			TargetDir:      worktreeremove.Present,
			GitAdminDir:    worktreeremove.Present,
			GitMarker:      worktreeremove.Present,
			CommonDir:      worktreeremove.Matched,
			Head:           worktreeremove.Present,
			Status:         worktreeremove.Clean,
			Comparator:     worktreeremove.ComparatorAvailable,
			Containment:    worktreeremove.Contained,
			ManagedIgnore:  worktreeremove.IgnoreAbsent,
		},
	}
	mutator := &recordingWorktreeRemovalMutator{}
	presenter := &recordingWorktreeRemovalPresenter{}

	err := runWorktreeRemovalOrchestration(
		context.Background(),
		worktreeRemovalRequest{mode: worktreeremove.Normal},
		observer,
		mutator,
		presenter,
	)
	if err == nil || err.Error() != "cannot prepare worktree removal: cannot remove the primary worktree" {
		t.Fatalf("runWorktreeRemovalOrchestration() error = %v", err)
	}
	if want := []string{"prepare"}; !reflect.DeepEqual(observer.operations, want) {
		t.Fatalf("observer operations = %#v, want %#v", observer.operations, want)
	}
	if len(mutator.removals) != 0 || len(mutator.cleanups) != 0 {
		t.Fatalf("mutations = %#v / %#v, want none", mutator.removals, mutator.cleanups)
	}
	if len(presenter.events) != 0 {
		t.Fatalf("presentation = %#v, want none", presenter.events)
	}
}

func TestWorktreeRemovalOrchestrationPresentsPartialRemoveFailure(t *testing.T) {
	removeErr := errors.New("remove failed")
	observer := &recordingWorktreeRemovalObserver{
		prepare: worktreeremove.PrepareFacts{
			Registration:   worktreeremove.Present,
			Target:         worktreeremove.RegisteredTarget,
			RegisteredPath: "/exact/registry/path",
			TargetDir:      worktreeremove.Present,
			GitAdminDir:    worktreeremove.Present,
			GitMarker:      worktreeremove.Present,
			CommonDir:      worktreeremove.Matched,
			Head:           worktreeremove.Present,
			Status:         worktreeremove.Clean,
			Comparator:     worktreeremove.ComparatorAvailable,
			Containment:    worktreeremove.Contained,
			ManagedIgnore:  worktreeremove.IgnoreAbsent,
		},
		revalidation: []worktreeremove.RevalidationFacts{worktreeRemovalRevalidationFacts(worktreeremove.Normal)},
		failure: []worktreeremove.FailureFacts{{
			Revalidation: worktreeRemovalRevalidationFacts(worktreeremove.Normal),
			Registration: worktreeremove.Missing,
			TargetPath:   worktreeremove.Missing,
		}},
	}
	mutator := &recordingWorktreeRemovalMutator{removeErr: removeErr}
	presenter := &recordingWorktreeRemovalPresenter{}

	err := runWorktreeRemovalOrchestration(
		context.Background(),
		worktreeRemovalRequest{mode: worktreeremove.Normal},
		observer,
		mutator,
		presenter,
	)
	if !errors.Is(err, removeErr) {
		t.Fatalf("runWorktreeRemovalOrchestration() error = %v, want wrapped %v", err, removeErr)
	}
	if want := []string{"prepare", "revalidate", "failure"}; !reflect.DeepEqual(observer.operations, want) {
		t.Fatalf("observer operations = %#v, want %#v", observer.operations, want)
	}
	if want := []worktreeremove.Mutation{{TargetPath: "/exact/registry/path", Force: false}}; !reflect.DeepEqual(mutator.removals, want) {
		t.Fatalf("removals = %#v, want %#v", mutator.removals, want)
	}
	if want := []string{"partial failure"}; !reflect.DeepEqual(presenter.events, want) {
		t.Fatalf("presentation = %#v, want %#v", presenter.events, want)
	}
}

func TestWorktreeRemovalCommandUsesPolicyPinnedMutation(t *testing.T) {
	observer := &recordingWorktreeRemovalObserver{
		prepare:      worktreeRemovalFacts(worktreeremove.IgnoreAbsent),
		revalidation: []worktreeremove.RevalidationFacts{worktreeRemovalRevalidationFacts(worktreeremove.Normal)},
	}
	mutator := &recordingWorktreeRemovalMutator{}

	err := runWorktreeRemovalOrchestration(
		context.Background(),
		worktreeRemovalRequest{mode: worktreeremove.Normal},
		observer,
		mutator,
		&recordingWorktreeRemovalPresenter{},
	)
	if err != nil {
		t.Fatalf("runWorktreeRemovalOrchestration() error = %v", err)
	}
	if want := []worktreeremove.Mutation{{TargetPath: "/exact/registry/path", Force: false}}; !reflect.DeepEqual(mutator.removals, want) {
		t.Fatalf("removals = %#v, want %#v", mutator.removals, want)
	}
}

func TestWorktreeRemovalCommandRefusesStaleApprovalAndPrepareDiagnostic(t *testing.T) {
	for _, tt := range []struct {
		name          string
		prepareErr    error
		revalidateErr error
		change        func(*worktreeremove.RevalidationFacts)
		want          string
	}{
		{
			name:   "stale target",
			change: func(f *worktreeremove.RevalidationFacts) { f.TargetPath = worktreeremove.InvariantChanged },
			want:   "worktree changed before removal",
		},
		{
			name:       "prepare diagnostic",
			prepareErr: errors.New("adapter preparation diagnostic"),
			want:       "adapter preparation diagnostic",
		},
		{
			name:          "stable facts with diagnostic",
			revalidateErr: errors.New("adapter revalidation diagnostic"),
			want:          "adapter revalidation diagnostic",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			facts := worktreeRemovalRevalidationFacts(worktreeremove.Normal)
			if tt.change != nil {
				tt.change(&facts)
			}
			observer := &recordingWorktreeRemovalObserver{
				prepare:       worktreeRemovalFacts(worktreeremove.IgnoreAbsent),
				prepareErr:    tt.prepareErr,
				revalidateErr: tt.revalidateErr,
				revalidation:  []worktreeremove.RevalidationFacts{facts},
			}
			mutator := &recordingWorktreeRemovalMutator{}
			err := runWorktreeRemovalOrchestration(context.Background(), worktreeRemovalRequest{mode: worktreeremove.Normal}, observer, mutator, &recordingWorktreeRemovalPresenter{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("runWorktreeRemovalOrchestration() error = %v, want %q", err, tt.want)
			}
			if len(mutator.removals) != 0 {
				t.Fatalf("removals = %#v, want none", mutator.removals)
			}
		})
	}
}

func TestWorktreeRemovalGitAdapterUsesExactApprovedArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the recording git executable is a POSIX shell script")
	}
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	gitPath := filepath.Join(dir, "git")
	if err := os.WriteFile(gitPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WORKTREE_REMOVE_ARGS_FILE\"\n"), 0755); err != nil {
		t.Fatalf("write recording git: %v", err)
	}
	adapter := &gitWorktreeRemovalAdapter{plan: &worktreeRemovalPlan{
		git:           &worktreeRemovalGit{executable: gitPath, env: []string{"WORKTREE_REMOVE_ARGS_FILE=" + argsPath}},
		executionRoot: dir,
		target:        pinnedWorktreeTarget{path: "/exact/registered path"},
		force:         true,
	}}
	mutation := worktreeremove.Mutation{TargetPath: "/exact/registered path", Force: true}
	if err := adapter.Remove(context.Background(), mutation); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	got, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read recorded git arguments: %v", err)
	}
	if want := "-c\ncore.hooksPath=\n-c\ncore.fsmonitor=false\n-c\ncore.ignorecase=false\nworktree\nremove\n--force\n--\n/exact/registered path\n"; string(got) != want {
		t.Fatalf("git arguments = %q, want %q", got, want)
	}

	if err := adapter.Remove(context.Background(), worktreeremove.Mutation{TargetPath: "/forged/path", Force: true}); err == nil {
		t.Fatal("Remove() accepted a forged target path")
	}
	adapter.plan.gitignoreCleanup = &gitignoreCleanupPlan{entry: "nested/lane"}
	if err := adapter.Cleanup(context.Background(), worktreeremove.Cleanup{Entry: "other/lane"}); err == nil {
		t.Fatal("Cleanup() accepted a forged managed-ignore entry")
	}
}

type recordingWorktreeRemovalObserver struct {
	prepare       worktreeremove.PrepareFacts
	prepareErr    error
	revalidateErr error
	revalidation  []worktreeremove.RevalidationFacts
	failure       []worktreeremove.FailureFacts
	operations    []string
}

func (r *recordingWorktreeRemovalObserver) Prepare(_ context.Context, request worktreeRemovalRequest) (worktreeRemovalApproval, error) {
	r.operations = append(r.operations, "prepare")
	return worktreeRemovalApproval{
		facts:      r.prepare,
		prepareErr: r.prepareErr,
	}, nil
}

func worktreeRemovalFacts(ignore worktreeremove.IgnoreKind) worktreeremove.PrepareFacts {
	return worktreeremove.PrepareFacts{
		Registration: worktreeremove.Present, Target: worktreeremove.RegisteredTarget, RegisteredPath: "/exact/registry/path",
		TargetDir: worktreeremove.Present, GitAdminDir: worktreeremove.Present,
		GitMarker: worktreeremove.Present, CommonDir: worktreeremove.Matched,
		Head: worktreeremove.Present, Status: worktreeremove.Clean,
		Comparator: worktreeremove.ComparatorAvailable, Containment: worktreeremove.Contained,
		ManagedIgnore: ignore,
	}
}

func worktreeRemovalRevalidationFacts(mode worktreeremove.Mode) worktreeremove.RevalidationFacts {
	facts := worktreeremove.RevalidationFacts{Registration: worktreeremove.InvariantStable, LockPrune: worktreeremove.InvariantStable, TargetPath: worktreeremove.InvariantStable, TargetDirectory: worktreeremove.InvariantStable, GitAdminDirectory: worktreeremove.InvariantStable, GitAdminDirectoryBytes: worktreeremove.InvariantStable, GitMarker: worktreeremove.InvariantStable, GitMarkerBytes: worktreeremove.InvariantStable, CommonDirectory: worktreeremove.InvariantStable, Head: worktreeremove.InvariantStable, Cleanliness: worktreeremove.InvariantStable, StatusBytes: worktreeremove.InvariantStable, DirtyFileFingerprint: worktreeremove.InvariantStable, Comparator: worktreeremove.InvariantStable, Containment: worktreeremove.InvariantStable, ManagedIgnore: worktreeremove.InvariantStable}
	if mode == worktreeremove.Force {
		facts.Comparator, facts.Containment = worktreeremove.InvariantNotRequired, worktreeremove.InvariantNotRequired
	}
	return facts
}

func (r *recordingWorktreeRemovalObserver) Revalidate(context.Context, worktreeRemovalApproval) (worktreeRemovalRevalidation, error) {
	r.operations = append(r.operations, "revalidate")
	if len(r.revalidation) == 0 {
		return worktreeRemovalRevalidation{}, errors.New("missing revalidation fact")
	}
	fact := r.revalidation[0]
	r.revalidation = r.revalidation[1:]
	return worktreeRemovalRevalidation{facts: fact, err: r.revalidateErr}, nil
}

func (r *recordingWorktreeRemovalObserver) Failure(context.Context, worktreeRemovalApproval, error) (worktreeRemovalFailure, error) {
	r.operations = append(r.operations, "failure")
	if len(r.failure) == 0 {
		return worktreeRemovalFailure{}, errors.New("missing failure fact")
	}
	fact := r.failure[0]
	r.failure = r.failure[1:]
	return worktreeRemovalFailure{facts: fact}, nil
}

type recordingWorktreeRemovalMutator struct {
	removeErr error
	removals  []worktreeremove.Mutation
	cleanups  []worktreeremove.Cleanup
}

func (r *recordingWorktreeRemovalMutator) Remove(_ context.Context, mutation worktreeremove.Mutation) error {
	r.removals = append(r.removals, mutation)
	return r.removeErr
}

func (r *recordingWorktreeRemovalMutator) Cleanup(_ context.Context, cleanup worktreeremove.Cleanup) error {
	r.cleanups = append(r.cleanups, cleanup)
	return nil
}

type recordingWorktreeRemovalPresenter struct{ events []string }

func (r *recordingWorktreeRemovalPresenter) Removed(worktreeremove.Mutation) error {
	r.events = append(r.events, "removed")
	return nil
}

func (r *recordingWorktreeRemovalPresenter) Failure(kind worktreeremove.FailureKind, _ error) {
	if kind == worktreeremove.PartialFailure {
		r.events = append(r.events, "partial failure")
		return
	}
	r.events = append(r.events, "unchanged failure")
}
