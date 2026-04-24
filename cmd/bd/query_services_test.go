//go:build cgo

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestQueryReadyWorkTruncation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t, filepath.Join(t.TempDir(), ".beads", "beads.db"))

	for _, issue := range []*types.Issue{
		{ID: "svc-ready-1", Title: "Ready 1", Status: types.StatusOpen, Priority: 0, IssueType: types.TypeTask, CreatedAt: time.Now()},
		{ID: "svc-ready-2", Title: "Ready 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask, CreatedAt: time.Now()},
		{ID: "svc-ready-3", Title: "Ready 3", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now()},
	} {
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatal(err)
		}
	}

	result, err := queryReadyWork(ctx, s, readyQueryRequest{Filter: types.WorkFilter{Status: types.StatusOpen, Limit: 1}})
	if err != nil {
		t.Fatalf("queryReadyWork: %v", err)
	}
	if !result.Truncated {
		t.Fatal("expected ready result to report truncation")
	}
	if result.TotalReady != 3 {
		t.Fatalf("TotalReady = %d, want 3", result.TotalReady)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("len(Issues) = %d, want 1", len(result.Issues))
	}
}

func TestQueryListIssuesSortsAndTruncates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t, filepath.Join(t.TempDir(), ".beads", "beads.db"))

	for _, issue := range []*types.Issue{
		{ID: "svc-list-a", Title: "Alpha", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now()},
		{ID: "svc-list-c", Title: "Charlie", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now()},
		{ID: "svc-list-b", Title: "Bravo", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, CreatedAt: time.Now()},
	} {
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatal(err)
		}
	}

	result, err := queryListIssues(ctx, s, listQueryRequest{
		Filter:         types.IssueFilter{},
		SortBy:         "title",
		Reverse:        true,
		EffectiveLimit: 2,
	})
	if err != nil {
		t.Fatalf("queryListIssues: %v", err)
	}
	if !result.Truncated {
		t.Fatal("expected list result to report truncation")
	}
	if got := []string{result.Issues[0].Title, result.Issues[1].Title}; got[0] != "Charlie" || got[1] != "Bravo" {
		t.Fatalf("sorted titles = %v, want [Charlie Bravo]", got)
	}
}

func TestShowIssueDetailsServiceComputesParentAndEpicProgress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t, filepath.Join(t.TempDir(), ".beads", "beads.db"))

	epic := &types.Issue{ID: "svc-show-epic", Title: "Epic", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeEpic, CreatedAt: time.Now()}
	child := &types.Issue{ID: "svc-show-child", Title: "Child", Status: types.StatusClosed, Priority: 1, IssueType: types.TypeTask, CreatedAt: time.Now(), ClosedAt: ptrTime(time.Now())}
	for _, issue := range []*types.Issue{epic, child} {
		if err := s.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddDependency(ctx, &types.Dependency{IssueID: child.ID, DependsOnID: epic.ID, Type: types.DepParentChild, CreatedAt: time.Now()}, "test"); err != nil {
		t.Fatal(err)
	}

	childDetails := buildShowIssueDetails(ctx, s, child)
	if childDetails.Parent == nil || *childDetails.Parent != epic.ID {
		t.Fatalf("child parent = %v, want %s", childDetails.Parent, epic.ID)
	}

	epicDetails := buildShowIssueDetails(ctx, s, epic)
	if epicDetails.EpicTotalChildren == nil || *epicDetails.EpicTotalChildren != 1 {
		t.Fatalf("EpicTotalChildren = %v, want 1", epicDetails.EpicTotalChildren)
	}
	if epicDetails.EpicCloseable == nil || !*epicDetails.EpicCloseable {
		t.Fatalf("EpicCloseable = %v, want true", epicDetails.EpicCloseable)
	}
}
