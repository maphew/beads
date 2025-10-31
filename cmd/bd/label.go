// Package main implements the bd CLI label management commands.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

var labelCmd = &cobra.Command{
	Use:   "label",
	Short: "Manage issue labels",
}

// Helper function to process label operations for multiple issues
func processBatchLabelOperation(issueIDs []string, label string, operation string, 
	daemonFunc func(string, string) error, storeFunc func(context.Context, string, string, string) error) {
	ctx := context.Background()
	results := []map[string]interface{}{}

	for _, issueID := range issueIDs {
		var err error
		if daemonClient != nil {
			err = daemonFunc(issueID, label)
		} else {
			err = storeFunc(ctx, issueID, label, actor)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error %s label %s %s: %v\n", operation, operation, issueID, err)
			continue
		}

		if jsonOutput {
			results = append(results, map[string]interface{}{
				"status":   operation,
				"issue_id": issueID,
				"label":    label,
			})
		} else {
			green := color.New(color.FgGreen).SprintFunc()
			verb := "Added"
			prep := "to"
			if operation == "removed" {
				verb = "Removed"
				prep = "from"
			}
			fmt.Printf("%s %s label '%s' %s %s\n", green("✓"), verb, label, prep, issueID)
		}
	}

	if len(issueIDs) > 0 && daemonClient == nil {
		markDirtyAndScheduleFlush()
	}

	if jsonOutput && len(results) > 0 {
		outputJSON(results)
	}
}

func parseLabelArgs(args []string) (issueIDs []string, label string) {
	label = args[len(args)-1]
	issueIDs = args[:len(args)-1]
	return
}

//nolint:dupl // labelAddCmd and labelRemoveCmd are similar but serve different operations
var labelAddCmd = &cobra.Command{
	Use:   "add [issue-id...] [label]",
	Short: "Add a label to one or more issues",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		issueIDs, label := parseLabelArgs(args)
		
		// Resolve partial IDs if in direct mode
		if daemonClient == nil {
			ctx := context.Background()
			resolvedIDs := make([]string, 0, len(issueIDs))
			for _, id := range issueIDs {
				fullID, err := utils.ResolvePartialID(ctx, store, id)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", id, err)
					continue
				}
				resolvedIDs = append(resolvedIDs, fullID)
			}
			issueIDs = resolvedIDs
		}
		
		processBatchLabelOperation(issueIDs, label, "added",
			func(issueID, lbl string) error {
				_, err := daemonClient.AddLabel(&rpc.LabelAddArgs{ID: issueID, Label: lbl})
				return err
			},
			func(ctx context.Context, issueID, lbl, act string) error {
				return store.AddLabel(ctx, issueID, lbl, act)
			})
	},
}

//nolint:dupl // labelRemoveCmd and labelAddCmd are similar but serve different operations
var labelRemoveCmd = &cobra.Command{
	Use:   "remove [issue-id...] [label]",
	Short: "Remove a label from one or more issues",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		issueIDs, label := parseLabelArgs(args)
		
		// Resolve partial IDs if in direct mode
		if daemonClient == nil {
			ctx := context.Background()
			resolvedIDs := make([]string, 0, len(issueIDs))
			for _, id := range issueIDs {
				fullID, err := utils.ResolvePartialID(ctx, store, id)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", id, err)
					continue
				}
				resolvedIDs = append(resolvedIDs, fullID)
			}
			issueIDs = resolvedIDs
		}
		
		processBatchLabelOperation(issueIDs, label, "removed",
			func(issueID, lbl string) error {
				_, err := daemonClient.RemoveLabel(&rpc.LabelRemoveArgs{ID: issueID, Label: lbl})
				return err
			},
			func(ctx context.Context, issueID, lbl, act string) error {
				return store.RemoveLabel(ctx, issueID, lbl, act)
			})
	},
}

var labelListCmd = &cobra.Command{
	Use:   "list [issue-id]",
	Short: "List labels for an issue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		issueID := args[0]

		ctx := context.Background()
		var labels []string
		
		// Resolve partial ID if in direct mode
		if daemonClient == nil {
			fullID, err := utils.ResolvePartialID(ctx, store, issueID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", issueID, err)
				os.Exit(1)
			}
			issueID = fullID
		}

		// Use daemon if available
		if daemonClient != nil {
			resp, err := daemonClient.Show(&rpc.ShowArgs{ID: issueID})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			var issue types.Issue
			if err := json.Unmarshal(resp.Data, &issue); err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
				os.Exit(1)
			}
			labels = issue.Labels
		} else {
			// Direct mode
			var err error
			labels, err = store.GetLabels(ctx, issueID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}

		if jsonOutput {
			// Always output array, even if empty
			if labels == nil {
				labels = []string{}
			}
			outputJSON(labels)
			return
		}

		if len(labels) == 0 {
			fmt.Printf("\n%s has no labels\n", issueID)
			return
		}

		cyan := color.New(color.FgCyan).SprintFunc()
		fmt.Printf("\n%s Labels for %s:\n", cyan("🏷"), issueID)
		for _, label := range labels {
			fmt.Printf("  - %s\n", label)
		}
		fmt.Println()
	},
}

var labelListAllCmd = &cobra.Command{
	Use:   "list-all",
	Short: "List all unique labels in the database",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		var issues []*types.Issue
		var err error

		// Use daemon if available
		if daemonClient != nil {
			resp, err := daemonClient.List(&rpc.ListArgs{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if err := json.Unmarshal(resp.Data, &issues); err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Direct mode
			issues, err = store.SearchIssues(ctx, "", types.IssueFilter{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}

		// Collect unique labels with counts
		labelCounts := make(map[string]int)
		for _, issue := range issues {
			if daemonClient != nil {
				// Labels are already in the issue from daemon
				for _, label := range issue.Labels {
					labelCounts[label]++
				}
			} else {
				// Direct mode - need to fetch labels
				labels, err := store.GetLabels(ctx, issue.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error getting labels for %s: %v\n", issue.ID, err)
					os.Exit(1)
				}
				for _, label := range labels {
					labelCounts[label]++
				}
			}
		}

		if len(labelCounts) == 0 {
			if jsonOutput {
				outputJSON([]string{})
			} else {
				fmt.Println("\nNo labels found in database")
			}
			return
		}

		// Sort labels alphabetically
		labels := make([]string, 0, len(labelCounts))
		for label := range labelCounts {
			labels = append(labels, label)
		}
		sort.Strings(labels)

		if jsonOutput {
			// Output as array of {label, count} objects
			type labelInfo struct {
				Label string `json:"label"`
				Count int    `json:"count"`
			}
			result := make([]labelInfo, 0, len(labels))
			for _, label := range labels {
				result = append(result, labelInfo{
					Label: label,
					Count: labelCounts[label],
				})
			}
			outputJSON(result)
			return
		}

		cyan := color.New(color.FgCyan).SprintFunc()
		fmt.Printf("\n%s All labels (%d unique):\n", cyan("🏷"), len(labels))

		// Find longest label for alignment
		maxLen := 0
		for _, label := range labels {
			if len(label) > maxLen {
				maxLen = len(label)
			}
		}

		for _, label := range labels {
			padding := strings.Repeat(" ", maxLen-len(label))
			fmt.Printf("  %s%s  (%d issues)\n", label, padding, labelCounts[label])
		}
		fmt.Println()
	},
}

func init() {
	labelCmd.AddCommand(labelAddCmd)
	labelCmd.AddCommand(labelRemoveCmd)
	labelCmd.AddCommand(labelListCmd)
	labelCmd.AddCommand(labelListAllCmd)
	rootCmd.AddCommand(labelCmd)
}
