package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// Helper function to test adding a dependency with a specific type
func testAddDependencyWithType(t *testing.T, depType types.DependencyType, title1, title2 string) {
	t.Helper()

	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create two issues
	issue1 := &types.Issue{Title: title1, Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: title2, Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	// Add dependency (issue2 depends on issue1)
	dep := &types.Dependency{
		IssueID:     issue2.ID,
		DependsOnID: issue1.ID,
		Type:        depType,
	}

	err := store.AddDependency(ctx, dep, "test-user")
	if err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Verify dependency was added
	deps, err := store.GetDependencies(ctx, issue2.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}

	if len(deps) != 1 {
		t.Fatalf("Expected 1 dependency, got %d", len(deps))
	}

	if deps[0].ID != issue1.ID {
		t.Errorf("Expected dependency on %s, got %s", issue1.ID, deps[0].ID)
	}
}

func TestAddDependency(t *testing.T) {
	testAddDependencyWithType(t, types.DepBlocks, "First", "Second")
}

func TestAddDependencyDiscoveredFrom(t *testing.T) {
	testAddDependencyWithType(t, types.DepDiscoveredFrom, "Parent task", "Bug found during work")
}

func TestParentChildValidation(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an epic (parent) and a task (child)
	epic := &types.Issue{Title: "Epic Feature", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeEpic}
	task := &types.Issue{Title: "Subtask", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, epic, "test-user")
	store.CreateIssue(ctx, task, "test-user")

	// Test 1: Valid direction - Task depends on Epic (child belongs to parent)
	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     task.ID,
		DependsOnID: epic.ID,
		Type:        types.DepParentChild,
	}, "test-user")
	if err != nil {
		t.Fatalf("Valid parent-child dependency failed: %v", err)
	}

	// Verify it was added
	deps, err := store.GetDependencies(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("Expected 1 dependency, got %d", len(deps))
	}

	// Remove the dependency for next test
	err = store.RemoveDependency(ctx, task.ID, epic.ID, "test-user")
	if err != nil {
		t.Fatalf("RemoveDependency failed: %v", err)
	}

	// Test 2: Invalid direction - Epic depends on Task (parent depends on child - backwards!)
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     epic.ID,
		DependsOnID: task.ID,
		Type:        types.DepParentChild,
	}, "test-user")
	if err == nil {
		t.Fatal("Expected error when parent depends on child, but got none")
	}
	if !strings.Contains(err.Error(), "child") || !strings.Contains(err.Error(), "parent") {
		t.Errorf("Expected error message to mention child/parent relationship, got: %v", err)
	}
}

func TestRemoveDependency(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create and link issues
	issue1 := &types.Issue{Title: "First", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Second", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	dep := &types.Dependency{
		IssueID:     issue2.ID,
		DependsOnID: issue1.ID,
		Type:        types.DepBlocks,
	}
	store.AddDependency(ctx, dep, "test-user")

	// Remove the dependency
	err := store.RemoveDependency(ctx, issue2.ID, issue1.ID, "test-user")
	if err != nil {
		t.Fatalf("RemoveDependency failed: %v", err)
	}

	// Verify dependency was removed
	deps, err := store.GetDependencies(ctx, issue2.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}

	if len(deps) != 0 {
		t.Errorf("Expected 0 dependencies after removal, got %d", len(deps))
	}
}

func TestGetDependents(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create issues: bd-2 and bd-3 both depend on bd-1
	issue1 := &types.Issue{Title: "Foundation", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Feature A", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue3 := &types.Issue{Title: "Feature B", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")
	store.CreateIssue(ctx, issue3, "test-user")

	store.AddDependency(ctx, &types.Dependency{IssueID: issue2.ID, DependsOnID: issue1.ID, Type: types.DepBlocks}, "test-user")
	store.AddDependency(ctx, &types.Dependency{IssueID: issue3.ID, DependsOnID: issue1.ID, Type: types.DepBlocks}, "test-user")

	// Get dependents of issue1
	dependents, err := store.GetDependents(ctx, issue1.ID)
	if err != nil {
		t.Fatalf("GetDependents failed: %v", err)
	}

	if len(dependents) != 2 {
		t.Fatalf("Expected 2 dependents, got %d", len(dependents))
	}

	// Verify both dependents are present
	foundIDs := make(map[string]bool)
	for _, dep := range dependents {
		foundIDs[dep.ID] = true
	}

	if !foundIDs[issue2.ID] || !foundIDs[issue3.ID] {
		t.Errorf("Expected dependents %s and %s", issue2.ID, issue3.ID)
	}
}

func TestGetDependencyTree(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create a chain: bd-3 → bd-2 → bd-1
	issue1 := &types.Issue{Title: "Level 0", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Level 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue3 := &types.Issue{Title: "Level 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")
	store.CreateIssue(ctx, issue3, "test-user")

	store.AddDependency(ctx, &types.Dependency{IssueID: issue2.ID, DependsOnID: issue1.ID, Type: types.DepBlocks}, "test-user")
	store.AddDependency(ctx, &types.Dependency{IssueID: issue3.ID, DependsOnID: issue2.ID, Type: types.DepBlocks}, "test-user")

	// Get tree starting from issue3
	tree, err := store.GetDependencyTree(ctx, issue3.ID, 10, false)
	if err != nil {
		t.Fatalf("GetDependencyTree failed: %v", err)
	}

	if len(tree) != 3 {
		t.Fatalf("Expected 3 nodes in tree, got %d", len(tree))
	}

	// Verify depths
	depthMap := make(map[string]int)
	for _, node := range tree {
		depthMap[node.ID] = node.Depth
	}

	if depthMap[issue3.ID] != 0 {
		t.Errorf("Expected depth 0 for %s, got %d", issue3.ID, depthMap[issue3.ID])
	}

	if depthMap[issue2.ID] != 1 {
		t.Errorf("Expected depth 1 for %s, got %d", issue2.ID, depthMap[issue2.ID])
	}

	if depthMap[issue1.ID] != 2 {
		t.Errorf("Expected depth 2 for %s, got %d", issue1.ID, depthMap[issue1.ID])
	}
}

func TestDetectCycles(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Try to create a cycle: bd-1 → bd-2 → bd-3 → bd-1
	// This should be prevented by AddDependency
	issue1 := &types.Issue{Title: "First", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Second", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue3 := &types.Issue{Title: "Third", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")
	store.CreateIssue(ctx, issue3, "test-user")

	// Add first two dependencies successfully
	err := store.AddDependency(ctx, &types.Dependency{IssueID: issue1.ID, DependsOnID: issue2.ID, Type: types.DepBlocks}, "test-user")
	if err != nil {
		t.Fatalf("First dependency failed: %v", err)
	}

	err = store.AddDependency(ctx, &types.Dependency{IssueID: issue2.ID, DependsOnID: issue3.ID, Type: types.DepBlocks}, "test-user")
	if err != nil {
		t.Fatalf("Second dependency failed: %v", err)
	}

	// The third dependency should fail because it would create a cycle
	err = store.AddDependency(ctx, &types.Dependency{IssueID: issue3.ID, DependsOnID: issue1.ID, Type: types.DepBlocks}, "test-user")
	if err == nil {
		t.Fatal("Expected error when creating cycle, but got none")
	}

	// Verify no cycles exist
	cycles, err := store.DetectCycles(ctx)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}

	if len(cycles) != 0 {
		t.Errorf("Expected no cycles after prevention, but found %d", len(cycles))
	}
}

func TestNoCyclesDetected(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create a valid chain with no cycles
	issue1 := &types.Issue{Title: "First", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Second", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	store.AddDependency(ctx, &types.Dependency{IssueID: issue2.ID, DependsOnID: issue1.ID, Type: types.DepBlocks}, "test-user")

	cycles, err := store.DetectCycles(ctx)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}

	if len(cycles) != 0 {
		t.Errorf("Expected no cycles, but found %d", len(cycles))
	}
}

func TestCrossTypeCyclePrevention(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create issues for cross-type cycle test
	issue1 := &types.Issue{Title: "Task A", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Task B", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	// Add: issue1 blocks issue2
	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue1.ID,
		DependsOnID: issue2.ID,
		Type:        types.DepBlocks,
	}, "test-user")
	if err != nil {
		t.Fatalf("First dependency (blocks) failed: %v", err)
	}

	// Try to add: issue2 parent-child issue1 (this would create a cross-type cycle)
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue2.ID,
		DependsOnID: issue1.ID,
		Type:        types.DepParentChild,
	}, "test-user")
	if err == nil {
		t.Fatal("Expected error when creating cross-type cycle, but got none")
	}

	// Verify no cycles exist
	cycles, err := store.DetectCycles(ctx)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}

	if len(cycles) != 0 {
		t.Errorf("Expected no cycles after prevention, but found %d", len(cycles))
	}
}

func TestCrossTypeCyclePreventionDiscoveredFrom(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create issues
	issue1 := &types.Issue{Title: "Parent Task", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Bug Found", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeBug}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	// Add: issue2 discovered-from issue1
	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue2.ID,
		DependsOnID: issue1.ID,
		Type:        types.DepDiscoveredFrom,
	}, "test-user")
	if err != nil {
		t.Fatalf("First dependency (discovered-from) failed: %v", err)
	}

	// Try to add: issue1 blocks issue2 (this would create a cross-type cycle)
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue1.ID,
		DependsOnID: issue2.ID,
		Type:        types.DepBlocks,
	}, "test-user")
	if err == nil {
		t.Fatal("Expected error when creating cross-type cycle with discovered-from, but got none")
	}
}

func TestSelfDependencyPrevention(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	issue := &types.Issue{Title: "Task", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	store.CreateIssue(ctx, issue, "test-user")

	// Try to create self-dependency (issue depends on itself)
	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue.ID,
		DependsOnID: issue.ID,
		Type:        types.DepBlocks,
	}, "test-user")

	if err == nil {
		t.Fatal("Expected error when creating self-dependency, but got none")
	}

	if !strings.Contains(err.Error(), "cannot depend on itself") {
		t.Errorf("Expected self-dependency error message, got: %v", err)
	}
}

func TestRelatedTypeCyclePrevention(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	issue1 := &types.Issue{Title: "Task A", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Task B", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	// Add: issue1 related issue2
	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue1.ID,
		DependsOnID: issue2.ID,
		Type:        types.DepRelated,
	}, "test-user")
	if err != nil {
		t.Fatalf("First dependency (related) failed: %v", err)
	}

	// Try to add: issue2 related issue1 (this creates a 2-node cycle with related type)
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue2.ID,
		DependsOnID: issue1.ID,
		Type:        types.DepRelated,
	}, "test-user")
	if err == nil {
		t.Fatal("Expected error when creating related-type cycle, but got none")
	}
}

func TestMixedTypeRelatedCyclePrevention(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	issue1 := &types.Issue{Title: "Task A", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Task B", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")

	// Add: issue1 blocks issue2
	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue1.ID,
		DependsOnID: issue2.ID,
		Type:        types.DepBlocks,
	}, "test-user")
	if err != nil {
		t.Fatalf("First dependency (blocks) failed: %v", err)
	}

	// Try to add: issue2 related issue1 (this creates a cross-type cycle)
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue2.ID,
		DependsOnID: issue1.ID,
		Type:        types.DepRelated,
	}, "test-user")
	if err == nil {
		t.Fatal("Expected error when creating blocks+related cycle, but got none")
	}
}

func TestCrossTypeCyclePreventionThreeIssues(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create issues for 3-node cross-type cycle test
	issue1 := &types.Issue{Title: "Task A", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue2 := &types.Issue{Title: "Task B", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}
	issue3 := &types.Issue{Title: "Task C", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask}

	store.CreateIssue(ctx, issue1, "test-user")
	store.CreateIssue(ctx, issue2, "test-user")
	store.CreateIssue(ctx, issue3, "test-user")

	// Add: issue1 blocks issue2
	err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue1.ID,
		DependsOnID: issue2.ID,
		Type:        types.DepBlocks,
	}, "test-user")
	if err != nil {
		t.Fatalf("First dependency failed: %v", err)
	}

	// Add: issue2 parent-child issue3
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue2.ID,
		DependsOnID: issue3.ID,
		Type:        types.DepParentChild,
	}, "test-user")
	if err != nil {
		t.Fatalf("Second dependency failed: %v", err)
	}

	// Try to add: issue3 discovered-from issue1 (this would create a 3-node cross-type cycle)
	err = store.AddDependency(ctx, &types.Dependency{
		IssueID:     issue3.ID,
		DependsOnID: issue1.ID,
		Type:        types.DepDiscoveredFrom,
	}, "test-user")
	if err == nil {
		t.Fatal("Expected error when creating 3-node cross-type cycle, but got none")
	}

	// Verify no cycles exist
	cycles, err := store.DetectCycles(ctx)
	if err != nil {
		t.Fatalf("DetectCycles failed: %v", err)
	}

	if len(cycles) != 0 {
		t.Errorf("Expected no cycles after prevention, but found %d", len(cycles))
	}
}
