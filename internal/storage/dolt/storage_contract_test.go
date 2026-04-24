//go:build cgo

package dolt

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/types"
)

func TestStorageContracts(t *testing.T) {
	t.Run("server", func(t *testing.T) {
		store, cleanup := setupTestStore(t)
		t.Cleanup(cleanup)
		runCoreStorageContract(t, store)
	})

	t.Run("embedded", func(t *testing.T) {
		if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
			t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded Dolt contract tests")
		}
		ctx := t.Context()
		beadsDir := filepath.Join(t.TempDir(), ".beads")
		store, err := embeddeddolt.New(ctx, beadsDir, "ctr", "main")
		if err != nil {
			t.Fatalf("embeddeddolt.New: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if err := store.SetConfig(ctx, "issue_prefix", "ctr"); err != nil {
			t.Fatalf("SetConfig(issue_prefix): %v", err)
		}
		if err := store.Commit(ctx, "contract init"); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		runCoreStorageContract(t, store)
	})
}

func runCoreStorageContract(t *testing.T, store storage.Storage) {
	t.Helper()
	ctx := t.Context()
	requireNoError(t, store.SetConfig(ctx, "issue_prefix", "ctr"), "SetConfig(issue_prefix)")
	requireNoError(t, store.SetConfig(ctx, "contract.mode", "active"), "SetConfig(contract.mode)")
	assertConfigValue(t, ctx, store, "contract.mode", "active")

	for _, issue := range []*types.Issue{
		contractIssue("ctr-epic", "Contract Epic", types.TypeEpic, types.StatusOpen, 1),
		contractIssue("ctr-ready", "Contract Ready", types.TypeTask, types.StatusOpen, 2),
		contractIssue("ctr-blocker", "Contract Blocker", types.TypeTask, types.StatusOpen, 1),
		contractIssue("ctr-blocked", "Contract Blocked", types.TypeTask, types.StatusOpen, 2),
		contractIssue("ctr-close", "Contract Close", types.TypeTask, types.StatusOpen, 3),
	} {
		requireNoError(t, store.CreateIssue(ctx, issue, "contract"), "CreateIssue("+issue.ID+")")
	}

	requireNoError(t, store.UpdateIssue(ctx, "ctr-ready", map[string]interface{}{
		"title":    "Contract Ready Updated",
		"priority": 1,
		"status":   string(types.StatusInProgress),
	}, "contract"), "UpdateIssue(ctr-ready)")
	assertIssue(t, ctx, store, "ctr-ready", func(issue *types.Issue) {
		if issue.Title != "Contract Ready Updated" {
			t.Fatalf("updated title = %q, want %q", issue.Title, "Contract Ready Updated")
		}
		if issue.Priority != 1 {
			t.Fatalf("updated priority = %d, want 1", issue.Priority)
		}
		if issue.Status != types.StatusInProgress {
			t.Fatalf("updated status = %q, want %q", issue.Status, types.StatusInProgress)
		}
	})

	requireNoError(t, store.AddDependency(ctx, &types.Dependency{
		IssueID:     "ctr-ready",
		DependsOnID: "ctr-epic",
		Type:        types.DepParentChild,
	}, "contract"), "AddDependency(parent-child)")
	requireNoError(t, store.AddDependency(ctx, &types.Dependency{
		IssueID:     "ctr-blocked",
		DependsOnID: "ctr-blocker",
		Type:        types.DepBlocks,
	}, "contract"), "AddDependency(blocks)")
	assertDependencyIDs(t, ctx, store.GetDependencies, "ctr-blocked", []string{"ctr-blocker"})
	assertDependencyIDs(t, ctx, store.GetDependents, "ctr-blocker", []string{"ctr-blocked"})

	requireNoError(t, store.AddLabel(ctx, "ctr-ready", "area/storage", "contract"), "AddLabel")
	labels, err := store.GetLabels(ctx, "ctr-ready")
	requireNoError(t, err, "GetLabels")
	if !slices.Contains(labels, "area/storage") {
		t.Fatalf("labels = %v, want area/storage", labels)
	}
	labeled, err := store.GetIssuesByLabel(ctx, "area/storage")
	requireNoError(t, err, "GetIssuesByLabel")
	assertIssueIDs(t, labeled, []string{"ctr-ready"})

	comment, err := store.AddIssueComment(ctx, "ctr-ready", "contract", "contract comment")
	requireNoError(t, err, "AddIssueComment")
	if comment.IssueID != "ctr-ready" || comment.Text != "contract comment" {
		t.Fatalf("comment = %#v, want issue ctr-ready with contract text", comment)
	}
	comments, err := store.GetIssueComments(ctx, "ctr-ready")
	requireNoError(t, err, "GetIssueComments")
	if len(comments) != 1 || comments[0].Text != "contract comment" {
		t.Fatalf("comments = %#v, want one contract comment", comments)
	}

	results, err := store.SearchIssues(ctx, "Contract", types.IssueFilter{})
	requireNoError(t, err, "SearchIssues")
	assertIssueIDs(t, results, []string{"ctr-blocked", "ctr-blocker", "ctr-close", "ctr-epic", "ctr-ready"})

	ready, err := store.GetReadyWork(ctx, types.WorkFilter{})
	requireNoError(t, err, "GetReadyWork")
	assertContainsIssueIDs(t, ready, []string{"ctr-blocker", "ctr-ready"})
	assertNotContainsIssueID(t, ready, "ctr-blocked")

	requireNoError(t, store.CloseIssue(ctx, "ctr-close", "completed", "contract", ""), "CloseIssue")
	assertIssue(t, ctx, store, "ctr-close", func(issue *types.Issue) {
		if issue.Status != types.StatusClosed {
			t.Fatalf("closed status = %q, want %q", issue.Status, types.StatusClosed)
		}
		if issue.ClosedAt == nil {
			t.Fatal("ClosedAt should be set")
		}
	})

	allConfig, err := store.GetAllConfig(ctx)
	requireNoError(t, err, "GetAllConfig")
	if allConfig["contract.mode"] != "active" {
		t.Fatalf("GetAllConfig contract.mode = %q, want active", allConfig["contract.mode"])
	}
}

func contractIssue(id, title string, issueType types.IssueType, status types.Status, priority int) *types.Issue {
	return &types.Issue{
		ID:        id,
		Title:     title,
		Status:    status,
		Priority:  priority,
		IssueType: issueType,
	}
}

func assertConfigValue(t *testing.T, ctx context.Context, store storage.Storage, key, want string) {
	t.Helper()
	got, err := store.GetConfig(ctx, key)
	requireNoError(t, err, "GetConfig("+key+")")
	if got != want {
		t.Fatalf("GetConfig(%q) = %q, want %q", key, got, want)
	}
}

func assertIssue(t *testing.T, ctx context.Context, store storage.Storage, id string, check func(*types.Issue)) {
	t.Helper()
	issue, err := store.GetIssue(ctx, id)
	requireNoError(t, err, "GetIssue("+id+")")
	check(issue)
}

func assertDependencyIDs(
	t *testing.T,
	ctx context.Context,
	get func(context.Context, string) ([]*types.Issue, error),
	id string,
	want []string,
) {
	t.Helper()
	issues, err := get(ctx, id)
	requireNoError(t, err, "dependency query("+id+")")
	assertIssueIDs(t, issues, want)
}

func assertIssueIDs(t *testing.T, issues []*types.Issue, want []string) {
	t.Helper()
	got := make([]string, 0, len(issues))
	for _, issue := range issues {
		got = append(got, issue.ID)
	}
	slices.Sort(got)
	sortedWant := slices.Clone(want)
	slices.Sort(sortedWant)
	if !slices.Equal(got, sortedWant) {
		t.Fatalf("issue IDs = %v, want %v", got, sortedWant)
	}
}

func assertContainsIssueIDs(t *testing.T, issues []*types.Issue, want []string) {
	t.Helper()
	got := make(map[string]bool, len(issues))
	for _, issue := range issues {
		got[issue.ID] = true
	}
	for _, id := range want {
		if !got[id] {
			t.Fatalf("issue IDs missing %s; got %v", id, mapKeys(got))
		}
	}
}

func assertNotContainsIssueID(t *testing.T, issues []*types.Issue, id string) {
	t.Helper()
	for _, issue := range issues {
		if issue.ID == id {
			t.Fatalf("issue IDs unexpectedly contain %s", id)
		}
	}
}

func mapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func requireNoError(t *testing.T, err error, op string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
}
