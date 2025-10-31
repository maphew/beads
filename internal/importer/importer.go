package importer

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

// Options contains import configuration
type Options struct {
	DryRun               bool // Preview changes without applying them
	SkipUpdate           bool // Skip updating existing issues (create-only mode)
	Strict               bool // Fail on any error (dependencies, labels, etc.)
	RenameOnImport       bool // Rename imported issues to match database prefix
	SkipPrefixValidation bool // Skip prefix validation (for auto-import)
}

// Result contains statistics about the import operation
type Result struct {
	Created          int               // New issues created
	Updated          int               // Existing issues updated
	Unchanged        int               // Existing issues that matched exactly (idempotent)
	Skipped          int               // Issues skipped (duplicates, errors)
	Collisions       int               // Collisions detected
	IDMapping        map[string]string // Mapping of remapped IDs (old -> new)
	CollisionIDs     []string          // IDs that collided
	PrefixMismatch   bool              // Prefix mismatch detected
	ExpectedPrefix   string            // Database configured prefix
	MismatchPrefixes map[string]int    // Map of mismatched prefixes to count
}

// ImportIssues handles the core import logic used by both manual and auto-import.
// This function:
// - Works with existing storage or opens direct SQLite connection if needed
// - Detects and handles collisions
// - Imports issues, dependencies, labels, and comments
// - Returns detailed results
//
// The caller is responsible for:
// - Reading and parsing JSONL into issues slice
// - Displaying results to the user
// - Setting metadata (e.g., last_import_hash)
//
// Parameters:
// - ctx: Context for cancellation
// - dbPath: Path to SQLite database file
// - store: Existing storage instance (can be nil for direct mode)
// - issues: Parsed issues from JSONL
// - opts: Import options
func ImportIssues(ctx context.Context, dbPath string, store storage.Storage, issues []*types.Issue, opts Options) (*Result, error) {
	result := &Result{
		IDMapping:        make(map[string]string),
		MismatchPrefixes: make(map[string]int),
	}

	// Compute content hashes for all incoming issues (bd-95)
	for _, issue := range issues {
		if issue.ContentHash == "" {
			issue.ContentHash = issue.ComputeContentHash()
		}
	}

	// Get or create SQLite store
	sqliteStore, needCloseStore, err := getOrCreateStore(ctx, dbPath, store)
	if err != nil {
		return nil, err
	}
	if needCloseStore {
		defer func() { _ = sqliteStore.Close() }()
	}
	
	// Clear export_hashes before import to prevent staleness (bd-160)
	// Import operations may add/update issues, so export_hashes entries become invalid
	if !opts.DryRun {
		if err := sqliteStore.ClearAllExportHashes(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clear export_hashes before import: %v\n", err)
		}
	}

	// Check and handle prefix mismatches
	if err := handlePrefixMismatch(ctx, sqliteStore, issues, opts, result); err != nil {
		return result, err
	}

	// Detect and resolve collisions
	issues, err = handleCollisions(ctx, sqliteStore, issues, opts, result)
	if err != nil {
		return result, err
	}
	if opts.DryRun && result.Collisions == 0 {
		return result, nil
	}

	// Upsert issues (create new or update existing)
	if err := upsertIssues(ctx, sqliteStore, issues, opts, result); err != nil {
		return nil, err
	}

	// Import dependencies
	if err := importDependencies(ctx, sqliteStore, issues, opts); err != nil {
		return nil, err
	}

	// Import labels
	if err := importLabels(ctx, sqliteStore, issues, opts); err != nil {
		return nil, err
	}

	// Import comments
	if err := importComments(ctx, sqliteStore, issues, opts); err != nil {
		return nil, err
	}

	// Checkpoint WAL to ensure data persistence and reduce WAL file size
	if err := sqliteStore.CheckpointWAL(ctx); err != nil {
		// Non-fatal - just log warning
		fmt.Fprintf(os.Stderr, "Warning: failed to checkpoint WAL: %v\n", err)
	}

	return result, nil
}

// getOrCreateStore returns an existing storage or creates a new one
func getOrCreateStore(ctx context.Context, dbPath string, store storage.Storage) (*sqlite.SQLiteStorage, bool, error) {
	if store != nil {
		sqliteStore, ok := store.(*sqlite.SQLiteStorage)
		if !ok {
			return nil, false, fmt.Errorf("import requires SQLite storage backend")
		}
		return sqliteStore, false, nil
	}

	// Open direct connection for daemon mode
	if dbPath == "" {
		return nil, false, fmt.Errorf("database path not set")
	}
	sqliteStore, err := sqlite.New(dbPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open database: %w", err)
	}

	return sqliteStore, true, nil
}

// handlePrefixMismatch checks and handles prefix mismatches
func handlePrefixMismatch(ctx context.Context, sqliteStore *sqlite.SQLiteStorage, issues []*types.Issue, opts Options, result *Result) error {
	configuredPrefix, err := sqliteStore.GetConfig(ctx, "issue_prefix")
	if err != nil {
		return fmt.Errorf("failed to get configured prefix: %w", err)
	}

	// Only validate prefixes if a prefix is configured
	if strings.TrimSpace(configuredPrefix) == "" {
		if opts.RenameOnImport {
			return fmt.Errorf("cannot rename: issue_prefix not configured in database")
		}
		return nil
	}

	result.ExpectedPrefix = configuredPrefix

	// Analyze prefixes in imported issues
	for _, issue := range issues {
		prefix := utils.ExtractIssuePrefix(issue.ID)
		if prefix != configuredPrefix {
			result.PrefixMismatch = true
			result.MismatchPrefixes[prefix]++
		}
	}

	// If prefix mismatch detected and not handling it, return error or warning
	if result.PrefixMismatch && !opts.RenameOnImport && !opts.DryRun && !opts.SkipPrefixValidation {
		return fmt.Errorf("prefix mismatch detected: database uses '%s-' but found issues with prefixes: %v (use --rename-on-import to automatically fix)", configuredPrefix, GetPrefixList(result.MismatchPrefixes))
	}

	// Handle rename-on-import if requested
	if result.PrefixMismatch && opts.RenameOnImport && !opts.DryRun {
		if err := RenameImportedIssuePrefixes(issues, configuredPrefix); err != nil {
			return fmt.Errorf("failed to rename prefixes: %w", err)
		}
		// After renaming, clear the mismatch flags since we fixed them
		result.PrefixMismatch = false
		result.MismatchPrefixes = make(map[string]int)
	}

	return nil
}

// handleCollisions detects and resolves ID collisions
func handleCollisions(ctx context.Context, sqliteStore *sqlite.SQLiteStorage, issues []*types.Issue, opts Options, result *Result) ([]*types.Issue, error) {
	// Phase 1: Detect (read-only)
	collisionResult, err := sqlite.DetectCollisions(ctx, sqliteStore, issues)
	if err != nil {
		return nil, fmt.Errorf("collision detection failed: %w", err)
	}

	result.Collisions = len(collisionResult.Collisions)
	for _, collision := range collisionResult.Collisions {
		result.CollisionIDs = append(result.CollisionIDs, collision.ID)
	}

	// With hash IDs, "collisions" (same ID, different content) are actually UPDATES
	// Hash IDs are based on creation content and remain stable across updates
	// So same ID + different fields = normal update operation, not a collision
	// The collisionResult.Collisions list represents issues that will be updated
	if len(collisionResult.Collisions) > 0 {
		result.Updated = len(collisionResult.Collisions)
	}

	// Phase 4: Renames removed - obsolete with hash IDs (bd-8e05)
	// Hash-based IDs are content-addressed, so renames don't occur

	if opts.DryRun {
		result.Created = len(collisionResult.NewIssues) + len(collisionResult.Renames)
		result.Unchanged = len(collisionResult.ExactMatches)
	}

	return issues, nil
}

// buildHashMap creates a map of content hash → issue for O(1) lookup
func buildHashMap(issues []*types.Issue) map[string]*types.Issue {
	result := make(map[string]*types.Issue)
	for _, issue := range issues {
		if issue.ContentHash != "" {
			result[issue.ContentHash] = issue
		}
	}
	return result
}

// buildIDMap creates a map of ID → issue for O(1) lookup
func buildIDMap(issues []*types.Issue) map[string]*types.Issue {
	result := make(map[string]*types.Issue)
	for _, issue := range issues {
		result[issue.ID] = issue
	}
	return result
}

// handleRename handles content match with different IDs (rename detected)
// Returns the old ID that was deleted (if any), or empty string if no deletion occurred
func handleRename(ctx context.Context, s *sqlite.SQLiteStorage, existing *types.Issue, incoming *types.Issue) (string, error) {
	// Check if target ID already exists with the same content (race condition)
	// This can happen when multiple clones import the same rename simultaneously
	targetIssue, err := s.GetIssue(ctx, incoming.ID)
	if err == nil && targetIssue != nil {
		// Target ID exists - check if it has the same content
		if targetIssue.ComputeContentHash() == incoming.ComputeContentHash() {
			// Same content - check if old ID still exists and delete it
			deletedID := ""
			existingCheck, checkErr := s.GetIssue(ctx, existing.ID)
			if checkErr == nil && existingCheck != nil {
				if err := s.DeleteIssue(ctx, existing.ID); err != nil {
					return "", fmt.Errorf("failed to delete old ID %s: %w", existing.ID, err)
				}
				deletedID = existing.ID
			}
			// The rename is already complete in the database
			return deletedID, nil
		}
		// REMOVED (bd-8e05): Sequential ID collision handling during rename
		// With hash-based IDs, rename collisions should not occur
		return "", fmt.Errorf("rename collision handling removed - should not occur with hash IDs")
		
		/* OLD CODE REMOVED (bd-8e05)
		// Different content - this is a collision during rename
		// Allocate a new ID for the incoming issue instead of using the desired ID
		prefix, err := s.GetConfig(ctx, "issue_prefix")
		if err != nil || prefix == "" {
			prefix = "bd"
		}
		
		oldID := existing.ID
		
		// Retry up to 3 times to handle concurrent ID allocation
		const maxRetries = 3
		for attempt := 0; attempt < maxRetries; attempt++ {
			newID, err := s.AllocateNextID(ctx, prefix)
			if err != nil {
				return "", fmt.Errorf("failed to generate new ID for rename collision: %w", err)
			}
			
			// Update incoming issue to use the new ID
			incoming.ID = newID
			
			// Delete old ID (only on first attempt)
			if attempt == 0 {
				if err := s.DeleteIssue(ctx, oldID); err != nil {
					return "", fmt.Errorf("failed to delete old ID %s: %w", oldID, err)
				}
			}
			
			// Create with new ID
			err = s.CreateIssue(ctx, incoming, "import-rename-collision")
			if err == nil {
				// Success!
				return oldID, nil
			}
			
			// Check if it's a UNIQUE constraint error
			if !sqlite.IsUniqueConstraintError(err) {
				// Not a UNIQUE constraint error, fail immediately
				return "", fmt.Errorf("failed to create renamed issue with collision resolution %s: %w", newID, err)
			}
			
			// UNIQUE constraint error - retry with new ID
			if attempt == maxRetries-1 {
				// Last attempt failed
				return "", fmt.Errorf("failed to create renamed issue with collision resolution after %d retries: %w", maxRetries, err)
			}
		}
		
		// Note: We don't update text references here because it would be too expensive
		// to scan all issues during every import. Text references to the old ID will
		// eventually be cleaned up by manual reference updates or remain as stale.
		// This is acceptable because the old ID no longer exists in the system.
		
		return oldID, nil
		*/
	}

	// Check if old ID still exists (it might have been deleted by another clone)
	existingCheck, checkErr := s.GetIssue(ctx, existing.ID)
	if checkErr != nil || existingCheck == nil {
		// Old ID doesn't exist - the rename must have been completed by another clone
		// Verify that target exists with correct content
		targetCheck, targetErr := s.GetIssue(ctx, incoming.ID)
		if targetErr == nil && targetCheck != nil && targetCheck.ComputeContentHash() == incoming.ComputeContentHash() {
			return "", nil
		}
		return "", fmt.Errorf("old ID %s doesn't exist and target ID %s is not as expected", existing.ID, incoming.ID)
	}

	// Delete old ID
	oldID := existing.ID
	if err := s.DeleteIssue(ctx, oldID); err != nil {
		return "", fmt.Errorf("failed to delete old ID %s: %w", oldID, err)
	}

	// Create with new ID
	if err := s.CreateIssue(ctx, incoming, "import-rename"); err != nil {
		// If UNIQUE constraint error, it's likely another clone created it concurrently
		if sqlite.IsUniqueConstraintError(err) {
			// Check if target exists with same content
			targetIssue, getErr := s.GetIssue(ctx, incoming.ID)
			if getErr == nil && targetIssue != nil && targetIssue.ComputeContentHash() == incoming.ComputeContentHash() {
				// Same content - rename already complete, this is OK
				return oldID, nil
			}
		}
		return "", fmt.Errorf("failed to create renamed issue %s: %w", incoming.ID, err)
	}

	// Reference updates removed - obsolete with hash IDs (bd-8e05)
	// Hash-based IDs are deterministic, so no reference rewriting needed

	return oldID, nil
}

// upsertIssues creates new issues or updates existing ones using content-first matching
func upsertIssues(ctx context.Context, sqliteStore *sqlite.SQLiteStorage, issues []*types.Issue, opts Options, result *Result) error {
	// Get all DB issues once
	dbIssues, err := sqliteStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return fmt.Errorf("failed to get DB issues: %w", err)
	}
	
	dbByHash := buildHashMap(dbIssues)
	dbByID := buildIDMap(dbIssues)

	// Track what we need to create
	var newIssues []*types.Issue
	seenHashes := make(map[string]bool)

	for _, incoming := range issues {
		hash := incoming.ContentHash
		if hash == "" {
			// Shouldn't happen (computed earlier), but be defensive
			hash = incoming.ComputeContentHash()
			incoming.ContentHash = hash
		}

		// Skip duplicates within incoming batch
		if seenHashes[hash] {
			result.Skipped++
			continue
		}
		seenHashes[hash] = true

		// Phase 1: Match by content hash first
		if existing, found := dbByHash[hash]; found {
			// Same content exists
			if existing.ID == incoming.ID {
				// Exact match (same content, same ID) - idempotent case
				result.Unchanged++
			} else {
				// Same content, different ID - rename detected
				if !opts.SkipUpdate {
					deletedID, err := handleRename(ctx, sqliteStore, existing, incoming)
					if err != nil {
						return fmt.Errorf("failed to handle rename %s -> %s: %w", existing.ID, incoming.ID, err)
					}
					// Remove the deleted ID from the map to prevent stale references
					if deletedID != "" {
						delete(dbByID, deletedID)
					}
					result.Updated++
				} else {
					result.Skipped++
				}
			}
			continue
		}

		// Phase 2: New content - check for ID collision
		if existingWithID, found := dbByID[incoming.ID]; found {
			// ID exists but different content - this is a collision
			// The collision should have been handled earlier by handleCollisions
			// If we reach here, it means collision wasn't resolved - treat as update
			if !opts.SkipUpdate {
				// Build updates map
				updates := make(map[string]interface{})
				updates["title"] = incoming.Title
				updates["description"] = incoming.Description
				updates["status"] = incoming.Status
				updates["priority"] = incoming.Priority
				updates["issue_type"] = incoming.IssueType
				updates["design"] = incoming.Design
				updates["acceptance_criteria"] = incoming.AcceptanceCriteria
				updates["notes"] = incoming.Notes

				if incoming.Assignee != "" {
					updates["assignee"] = incoming.Assignee
				} else {
					updates["assignee"] = nil
				}

				if incoming.ExternalRef != nil && *incoming.ExternalRef != "" {
					updates["external_ref"] = *incoming.ExternalRef
				} else {
					updates["external_ref"] = nil
				}

				// Only update if data actually changed
				if IssueDataChanged(existingWithID, updates) {
					if err := sqliteStore.UpdateIssue(ctx, incoming.ID, updates, "import"); err != nil {
						return fmt.Errorf("error updating issue %s: %w", incoming.ID, err)
					}
					result.Updated++
				} else {
					result.Unchanged++
				}
			} else {
				result.Skipped++
			}
		} else {
			// Truly new issue
			newIssues = append(newIssues, incoming)
		}
	}

	// Batch create all new issues
	if len(newIssues) > 0 {
		if err := sqliteStore.CreateIssues(ctx, newIssues, "import"); err != nil {
			return fmt.Errorf("error creating issues: %w", err)
		}
		result.Created += len(newIssues)
	}

	// REMOVED (bd-c7af): Counter sync after import - no longer needed with hash IDs

	return nil
}

// importDependencies imports dependency relationships
func importDependencies(ctx context.Context, sqliteStore *sqlite.SQLiteStorage, issues []*types.Issue, opts Options) error {
	for _, issue := range issues {
		if len(issue.Dependencies) == 0 {
			continue
		}

		// Fetch existing dependencies once per issue
		existingDeps, err := sqliteStore.GetDependencyRecords(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("error checking dependencies for %s: %w", issue.ID, err)
		}

		// Build set of existing dependencies for O(1) lookup
		existingSet := make(map[string]bool)
		for _, existing := range existingDeps {
			key := fmt.Sprintf("%s|%s", existing.DependsOnID, existing.Type)
			existingSet[key] = true
		}

		for _, dep := range issue.Dependencies {
			// Check for duplicate using set
			key := fmt.Sprintf("%s|%s", dep.DependsOnID, dep.Type)
			if existingSet[key] {
				continue
			}

			// Add dependency
			if err := sqliteStore.AddDependency(ctx, dep, "import"); err != nil {
				if opts.Strict {
					return fmt.Errorf("error adding dependency %s → %s: %w", dep.IssueID, dep.DependsOnID, err)
				}
				continue
			}
		}
	}

	return nil
}

// importLabels imports labels for issues
func importLabels(ctx context.Context, sqliteStore *sqlite.SQLiteStorage, issues []*types.Issue, opts Options) error {
	for _, issue := range issues {
		if len(issue.Labels) == 0 {
			continue
		}

		// Get current labels
		currentLabels, err := sqliteStore.GetLabels(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("error getting labels for %s: %w", issue.ID, err)
		}

		currentLabelSet := make(map[string]bool)
		for _, label := range currentLabels {
			currentLabelSet[label] = true
		}

		// Add missing labels
		for _, label := range issue.Labels {
			if !currentLabelSet[label] {
				if err := sqliteStore.AddLabel(ctx, issue.ID, label, "import"); err != nil {
					if opts.Strict {
						return fmt.Errorf("error adding label %s to %s: %w", label, issue.ID, err)
					}
					continue
				}
			}
		}
	}

	return nil
}

// importComments imports comments for issues
func importComments(ctx context.Context, sqliteStore *sqlite.SQLiteStorage, issues []*types.Issue, opts Options) error {
	for _, issue := range issues {
		if len(issue.Comments) == 0 {
			continue
		}

		// Get current comments to avoid duplicates
		currentComments, err := sqliteStore.GetIssueComments(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("error getting comments for %s: %w", issue.ID, err)
		}

		// Build a set of existing comments (by author+normalized text)
		existingComments := make(map[string]bool)
		for _, c := range currentComments {
			key := fmt.Sprintf("%s:%s", c.Author, strings.TrimSpace(c.Text))
			existingComments[key] = true
		}

		// Add missing comments
		for _, comment := range issue.Comments {
			key := fmt.Sprintf("%s:%s", comment.Author, strings.TrimSpace(comment.Text))
			if !existingComments[key] {
				if _, err := sqliteStore.AddIssueComment(ctx, issue.ID, comment.Author, comment.Text); err != nil {
					if opts.Strict {
						return fmt.Errorf("error adding comment to %s: %w", issue.ID, err)
					}
					continue
				}
			}
		}
	}

	return nil
}

// Helper functions

func GetPrefixList(prefixes map[string]int) []string {
	var result []string
	keys := make([]string, 0, len(prefixes))
	for k := range prefixes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, prefix := range keys {
		count := prefixes[prefix]
		result = append(result, fmt.Sprintf("%s- (%d issues)", prefix, count))
	}
	return result
}
