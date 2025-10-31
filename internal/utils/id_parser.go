// Package utils provides utility functions for issue ID parsing and resolution.
package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// ParseIssueID ensures an issue ID has the configured prefix.
// If the input already has the prefix (e.g., "bd-a3f8e9"), returns it as-is.
// If the input lacks the prefix (e.g., "a3f8e9"), adds the configured prefix.
// Works with hierarchical IDs too: "a3f8e9.1.2" → "bd-a3f8e9.1.2"
func ParseIssueID(input string, prefix string) string {
	if prefix == "" {
		prefix = "bd-"
	}
	
	if strings.HasPrefix(input, prefix) {
		return input
	}
	
	return prefix + input
}

// ResolvePartialID resolves a potentially partial issue ID to a full ID.
// Supports:
// - Full IDs: "bd-a3f8e9" or "a3f8e9" → "bd-a3f8e9"
// - Without hyphen: "bda3f8e9" or "wya3f8e9" → "bd-a3f8e9"
// - Partial IDs: "a3f8" → "bd-a3f8e9" (if unique match)
// - Hierarchical: "a3f8e9.1" → "bd-a3f8e9.1"
//
// Returns an error if:
// - No issue found matching the ID
// - Multiple issues match (ambiguous prefix)
func ResolvePartialID(ctx context.Context, store storage.Storage, input string) (string, error) {
	// Get the configured prefix
	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		prefix = "bd"
	}
	
	// Ensure prefix has hyphen for ID format
	prefixWithHyphen := prefix
	if !strings.HasSuffix(prefix, "-") {
		prefixWithHyphen = prefix + "-"
	}
	
	// Normalize input:
	// 1. If it has the full prefix with hyphen (bd-a3f8e9), use as-is
	// 2. Otherwise, add prefix with hyphen (handles both bare hashes and prefix-without-hyphen cases)
	
	var normalizedID string
	
	if strings.HasPrefix(input, prefixWithHyphen) {
		// Already has prefix with hyphen: "bd-a3f8e9"
		normalizedID = input
	} else {
		// Bare hash or prefix without hyphen: "a3f8e9", "07b8c8", "bda3f8e9" → all get prefix with hyphen added
		normalizedID = prefixWithHyphen + input
	}
	
	// First try exact match
	issue, err := store.GetIssue(ctx, normalizedID)
	if err == nil && issue != nil {
		return normalizedID, nil
	}
	
	// If exact match failed, try substring search
	filter := types.IssueFilter{}
	
	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		return "", fmt.Errorf("failed to search issues: %w", err)
	}
	
	// Extract the hash part for substring matching
	hashPart := strings.TrimPrefix(normalizedID, prefix)
	
	var matches []string
	for _, issue := range issues {
		issueHash := strings.TrimPrefix(issue.ID, prefix)
		// Check if the issue hash contains the input hash as substring
		if strings.Contains(issueHash, hashPart) {
			matches = append(matches, issue.ID)
		}
	}
	
	if len(matches) == 0 {
		return "", fmt.Errorf("no issue found matching %q", input)
	}
	
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous ID %q matches %d issues: %v\nUse more characters to disambiguate", input, len(matches), matches)
	}
	
	return matches[0], nil
}

// ResolvePartialIDs resolves multiple potentially partial issue IDs.
// Returns the resolved IDs and any errors encountered.
func ResolvePartialIDs(ctx context.Context, store storage.Storage, inputs []string) ([]string, error) {
	var resolved []string
	for _, input := range inputs {
		fullID, err := ResolvePartialID(ctx, store, input)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, fullID)
	}
	return resolved, nil
}
