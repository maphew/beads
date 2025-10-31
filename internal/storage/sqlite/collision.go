package sqlite

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// CollisionResult categorizes incoming issues by their relationship to existing DB state
type CollisionResult struct {
	ExactMatches []string           // IDs that match exactly (idempotent import)
	Collisions   []*CollisionDetail // Issues with same ID but different content
	NewIssues    []string           // IDs that don't exist in DB yet
	Renames      []*RenameDetail    // Issues with same content but different ID (renames)
}

// RenameDetail captures a rename/remap detected during collision detection
type RenameDetail struct {
	OldID string        // ID in database (to be deleted)
	NewID string        // ID in incoming (to be created)
	Issue *types.Issue  // The issue with new ID
}

// CollisionDetail provides detailed information about a collision
type CollisionDetail struct {
	ID                string        // The issue ID that collided
	IncomingIssue     *types.Issue  // The issue from the import file
	ExistingIssue     *types.Issue  // The issue currently in the database
	ConflictingFields []string      // List of field names that differ
	RemapIncoming     bool          // If true, remap incoming; if false, remap existing
}

// DetectCollisions compares incoming JSONL issues against DB state
// It distinguishes between:
//  1. Exact match (idempotent) - ID and content are identical
//  2. ID match but different content (collision) - same ID, different fields
//  3. New issue - ID doesn't exist in DB
//  4. Rename detected - Different ID but same content (from prior remap)
//
// Returns a CollisionResult categorizing all incoming issues.
func DetectCollisions(ctx context.Context, s *SQLiteStorage, incomingIssues []*types.Issue) (*CollisionResult, error) {
	result := &CollisionResult{
		ExactMatches: make([]string, 0),
		Collisions:   make([]*CollisionDetail, 0),
		NewIssues:    make([]string, 0),
	}

	// Build content hash map for rename detection
	contentToID := make(map[string]string)
	for _, incoming := range incomingIssues {
		hash := hashIssueContent(incoming)
		contentToID[hash] = incoming.ID
	}

	// Check each incoming issue
	for _, incoming := range incomingIssues {
		existing, err := s.GetIssue(ctx, incoming.ID)
		if err != nil || existing == nil {
			// Issue doesn't exist in DB - it's new
			result.NewIssues = append(result.NewIssues, incoming.ID)
			continue
		}

		// Issue ID exists - check if content matches
		conflictingFields := compareIssues(existing, incoming)
		if len(conflictingFields) == 0 {
			// Exact match - idempotent import
			result.ExactMatches = append(result.ExactMatches, incoming.ID)
		} else {
			// Same ID, different content - collision
			// With hash IDs, this shouldn't happen unless manually edited
			result.Collisions = append(result.Collisions, &CollisionDetail{
				ID:                incoming.ID,
				IncomingIssue:     incoming,
				ExistingIssue:     existing,
				ConflictingFields: conflictingFields,
			})
		}
	}

	return result, nil
}

// compareIssues returns list of field names that differ between two issues
func compareIssues(existing, incoming *types.Issue) []string {
	conflicts := []string{}

	if existing.Title != incoming.Title {
		conflicts = append(conflicts, "title")
	}
	if existing.Description != incoming.Description {
		conflicts = append(conflicts, "description")
	}
	if existing.Status != incoming.Status {
		conflicts = append(conflicts, "status")
	}
	if existing.Priority != incoming.Priority {
		conflicts = append(conflicts, "priority")
	}
	if existing.IssueType != incoming.IssueType {
		conflicts = append(conflicts, "issue_type")
	}
	if existing.Assignee != incoming.Assignee {
		conflicts = append(conflicts, "assignee")
	}
	if existing.Design != incoming.Design {
		conflicts = append(conflicts, "design")
	}
	if existing.AcceptanceCriteria != incoming.AcceptanceCriteria {
		conflicts = append(conflicts, "acceptance_criteria")
	}
	if existing.Notes != incoming.Notes {
		conflicts = append(conflicts, "notes")
	}
	if (existing.ExternalRef == nil && incoming.ExternalRef != nil) ||
		(existing.ExternalRef != nil && incoming.ExternalRef == nil) ||
		(existing.ExternalRef != nil && incoming.ExternalRef != nil && *existing.ExternalRef != *incoming.ExternalRef) {
		conflicts = append(conflicts, "external_ref")
	}

	return conflicts
}

// hashIssueContent creates a deterministic hash of issue content (excluding ID and timestamps)
func hashIssueContent(issue *types.Issue) string {
	h := sha256.New()
	fmt.Fprintf(h, "title:%s\n", issue.Title)
	fmt.Fprintf(h, "description:%s\n", issue.Description)
	fmt.Fprintf(h, "status:%s\n", issue.Status)
	fmt.Fprintf(h, "priority:%d\n", issue.Priority)
	fmt.Fprintf(h, "type:%s\n", issue.IssueType)
	fmt.Fprintf(h, "assignee:%s\n", issue.Assignee)
	fmt.Fprintf(h, "design:%s\n", issue.Design)
	fmt.Fprintf(h, "acceptance:%s\n", issue.AcceptanceCriteria)
	fmt.Fprintf(h, "notes:%s\n", issue.Notes)
	if issue.ExternalRef != nil {
		fmt.Fprintf(h, "external_ref:%s\n", *issue.ExternalRef)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
