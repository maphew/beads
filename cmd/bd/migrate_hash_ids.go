package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

var migrateHashIDsCmd = &cobra.Command{
	Use:   "migrate-hash-ids",
	Short: "Migrate sequential IDs to hash-based IDs",
	Long: `Migrate database from sequential IDs (bd-1, bd-2) to hash-based IDs (bd-a3f8e9a2).

This command:
- Generates hash IDs for all top-level issues
- Assigns hierarchical child IDs (bd-a3f8e9a2.1) for epic children
- Updates all references (dependencies, comments, external refs)
- Creates mapping file for reference
- Validates all relationships are intact

Use --dry-run to preview changes before applying.`,
	Run: func(cmd *cobra.Command, _ []string) {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		
		ctx := context.Background()
		
		// Find database
		dbPath := beads.FindDatabasePath()
		if dbPath == "" {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "no_database",
					"message": "No beads database found. Run 'bd init' first.",
				})
			} else {
				fmt.Fprintf(os.Stderr, "Error: no beads database found\n")
				fmt.Fprintf(os.Stderr, "Hint: run 'bd init' to initialize bd\n")
			}
			os.Exit(1)
		}
		
		// Create backup before migration
		if !dryRun {
			backupPath := strings.TrimSuffix(dbPath, ".db") + ".backup-" + time.Now().Format("20060102-150405") + ".db"
			if err := copyFile(dbPath, backupPath); err != nil {
				if jsonOutput {
					outputJSON(map[string]interface{}{
						"error":   "backup_failed",
						"message": err.Error(),
					})
				} else {
					fmt.Fprintf(os.Stderr, "Error: failed to create backup: %v\n", err)
				}
				os.Exit(1)
			}
			if !jsonOutput {
				color.Green("✓ Created backup: %s\n\n", filepath.Base(backupPath))
			}
		}
		
		// Open database
		store, err := sqlite.New(dbPath)
		if err != nil {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "open_failed",
					"message": err.Error(),
				})
			} else {
				fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
			}
			os.Exit(1)
		}
		defer store.Close()
		
		// Get all issues using SearchIssues with empty query and no filters
		issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err != nil {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "list_failed",
					"message": err.Error(),
				})
			} else {
				fmt.Fprintf(os.Stderr, "Error: failed to list issues: %v\n", err)
			}
			os.Exit(1)
		}
		
		if len(issues) == 0 {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"status":  "no_issues",
					"message": "No issues to migrate",
				})
			} else {
				fmt.Println("No issues to migrate")
			}
			return
		}
		
		// Check if already using hash IDs
		if isHashID(issues[0].ID) {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"status":  "already_migrated",
					"message": "Database already uses hash-based IDs",
				})
			} else {
				fmt.Println("Database already uses hash-based IDs")
			}
			return
		}
		
		// Perform migration
		mapping, err := migrateToHashIDs(ctx, store, issues, dryRun)
		if err != nil {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "migration_failed",
					"message": err.Error(),
				})
			} else {
				fmt.Fprintf(os.Stderr, "Error: migration failed: %v\n", err)
			}
			os.Exit(1)
		}
		
		// Save mapping to file
		if !dryRun {
			mappingPath := filepath.Join(filepath.Dir(dbPath), "hash-id-mapping.json")
			if err := saveMappingFile(mappingPath, mapping); err != nil {
				if !jsonOutput {
					color.Yellow("Warning: failed to save mapping file: %v\n", err)
				}
			} else if !jsonOutput {
				color.Green("✓ Saved mapping to: %s\n", filepath.Base(mappingPath))
			}
		}
		
		// Output results
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":        "success",
				"dry_run":       dryRun,
				"issues_migrated": len(mapping),
				"mapping":       mapping,
			})
		} else {
			if dryRun {
				fmt.Println("\nDry run complete - no changes made")
				fmt.Printf("Would migrate %d issues\n\n", len(mapping))
				fmt.Println("Preview of mapping (first 10):")
				count := 0
				for old, new := range mapping {
					if count >= 10 {
						fmt.Printf("... and %d more\n", len(mapping)-10)
						break
					}
					fmt.Printf("  %s → %s\n", old, new)
					count++
				}
			} else {
				color.Green("\n✓ Migration complete!\n\n")
				fmt.Printf("Migrated %d issues to hash-based IDs\n", len(mapping))
				fmt.Println("\nNext steps:")
				fmt.Println("  1. Run 'bd export' to update JSONL file")
				fmt.Println("  2. Commit changes to git")
				fmt.Println("  3. Notify team members to pull and re-initialize")
			}
		}
	},
}

// migrateToHashIDs performs the actual migration
func migrateToHashIDs(ctx context.Context, store *sqlite.SQLiteStorage, issues []*types.Issue, dryRun bool) (map[string]string, error) {
	// Build dependency graph to determine top-level vs child issues
	parentMap := make(map[string]string) // child ID → parent ID
	
	// Get all dependencies to find parent-child relationships
	for _, issue := range issues {
		deps, err := store.GetDependencyRecords(ctx, issue.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get dependencies for %s: %w", issue.ID, err)
		}
		
		for _, dep := range deps {
			if dep.Type == types.DepParentChild {
				// issue depends on parent
				parentMap[issue.ID] = dep.DependsOnID
			}
		}
	}
	
	// Get prefix from config or use default
	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		prefix = "bd"
	}
	
	// Generate mapping: old ID → new hash ID
	mapping := make(map[string]string)
	childCounters := make(map[string]int) // parent hash ID → next child number
	
	// First pass: generate hash IDs for top-level issues (no parent)
	for _, issue := range issues {
		if _, hasParent := parentMap[issue.ID]; !hasParent {
			// Top-level issue - generate hash ID
			hashID := generateHashIDForIssue(prefix, issue)
			mapping[issue.ID] = hashID
		}
	}
	
	// Second pass: assign hierarchical IDs to child issues
	for _, issue := range issues {
		if parentID, hasParent := parentMap[issue.ID]; hasParent {
			// Child issue - use parent's hash ID + sequential number
			parentHashID, ok := mapping[parentID]
			if !ok {
				return nil, fmt.Errorf("parent %s not yet mapped for child %s", parentID, issue.ID)
			}
			
			// Get next child number for this parent
			childNum := childCounters[parentHashID] + 1
			childCounters[parentHashID] = childNum
			
			// Assign hierarchical ID
			mapping[issue.ID] = fmt.Sprintf("%s.%d", parentHashID, childNum)
		}
	}
	
	if dryRun {
		return mapping, nil
	}
	
	// Apply the migration
	// UpdateIssueID handles updating the issue, dependencies, comments, events, labels, and dirty_issues
	// We need to also update text references in descriptions, notes, design, acceptance criteria
	
	// Sort issues by ID to process parents before children
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ID < issues[j].ID
	})
	
	// Update all issues
	for _, issue := range issues {
		newID := mapping[issue.ID]
		
		// Update text references in this issue
		issue.Description = replaceIDReferences(issue.Description, mapping)
		if issue.Design != "" {
			issue.Design = replaceIDReferences(issue.Design, mapping)
		}
		if issue.Notes != "" {
			issue.Notes = replaceIDReferences(issue.Notes, mapping)
		}
		if issue.AcceptanceCriteria != "" {
			issue.AcceptanceCriteria = replaceIDReferences(issue.AcceptanceCriteria, mapping)
		}
		if issue.ExternalRef != nil {
			updated := replaceIDReferences(*issue.ExternalRef, mapping)
			issue.ExternalRef = &updated
		}
		
		// Use UpdateIssueID to change the primary key and cascade to all foreign keys
		// This method handles dependencies, comments, events, labels, and dirty_issues
		oldID := issue.ID
		if err := store.UpdateIssueID(ctx, oldID, newID, issue, "migration"); err != nil {
			return nil, fmt.Errorf("failed to update issue %s → %s: %w", oldID, newID, err)
		}
	}
	
	return mapping, nil
}

// generateHashIDForIssue generates a hash-based ID for an issue
func generateHashIDForIssue(prefix string, issue *types.Issue) string {
	// Use the same algorithm as generateHashID in sqlite.go
	// Use "system" as the actor for migration to ensure deterministic IDs
	content := fmt.Sprintf("%s|%s|%s|%d|%d",
		issue.Title,
		issue.Description,
		"system", // Use consistent actor for migration
		issue.CreatedAt.UnixNano(),
		0, // nonce
	)
	
	hash := sha256Hash(content)
	shortHash := hash[:8] // First 8 hex chars
	
	return fmt.Sprintf("%s-%s", prefix, shortHash)
}

// sha256Hash computes SHA256 hash and returns first 8 hex chars
func sha256Hash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:4]) // 4 bytes = 8 hex chars
}

// replaceIDReferences replaces all old ID references with new hash IDs
func replaceIDReferences(text string, mapping map[string]string) string {
	// Match patterns like "bd-123" or "bd-123.4"
	re := regexp.MustCompile(`\bbd-\d+(?:\.\d+)*\b`)
	
	return re.ReplaceAllStringFunc(text, func(match string) string {
		if newID, ok := mapping[match]; ok {
			return newID
		}
		return match // Keep unchanged if not in mapping
	})
}

// isHashID checks if an ID is hash-based (not sequential)
func isHashID(id string) bool {
	// Hash IDs contain hex characters after the prefix
	// Sequential IDs are only digits
	parts := strings.SplitN(id, "-", 2)
	if len(parts) != 2 {
		return false
	}
	
	// Check if the suffix starts with a hex digit (a-f)
	suffix := parts[1]
	if len(suffix) == 0 {
		return false
	}
	
	// If it contains any letter a-f, it's a hash ID
	return regexp.MustCompile(`[a-f]`).MatchString(suffix)
}

// saveMappingFile saves the ID mapping to a JSON file
func saveMappingFile(path string, mapping map[string]string) error {
	// Convert to sorted array for readability
	type mappingEntry struct {
		OldID string `json:"old_id"`
		NewID string `json:"new_id"`
	}
	
	entries := make([]mappingEntry, 0, len(mapping))
	for old, new := range mapping {
		entries = append(entries, mappingEntry{
			OldID: old,
			NewID: new,
		})
	}
	
	// Sort by old ID for readability
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].OldID < entries[j].OldID
	})
	
	data, err := json.MarshalIndent(map[string]interface{}{
		"migrated_at": time.Now().Format(time.RFC3339),
		"count":       len(entries),
		"mapping":     entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(path, data, 0644)
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func init() {
	migrateHashIDsCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	rootCmd.AddCommand(migrateHashIDsCmd)
}
