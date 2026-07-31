package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// JSONL import used to be all-or-nothing: one record with a bad field aborted
// the whole file inside the transaction, so a 5,000-issue export with a single
// "status":"verify" row imported nothing at all (GH#4492). The records are
// independent, so a per-record fault has no business discarding its neighbors.
//
// The reject path below runs the same validation the writer runs, one record at
// a time, *before* the batch is handed to storage. Every rejection is reported
// with its source line and reason, the surviving records still commit in a
// single transaction, and the rejected lines are written out verbatim so
// nothing is lost. `bd import --strict` restores the old abort-on-first-fault
// behavior for callers that want it.

// recordSource locates a parsed record in its JSONL source so a rejection can
// name a line number and the raw text can be quarantined verbatim.
type recordSource struct {
	Line int
	Raw  string
}

// rejectKind separates the two ways a record can be unusable. The distinction
// matters because the recovery paths treat them differently: a record the
// writer would refuse is one bad row in an otherwise good file, but a line that
// is not JSON at all suggests the file itself is truncated or corrupt — and
// "restored everything except the damaged part" is not a restore.
type rejectKind string

const (
	rejectParse    rejectKind = "parse"
	rejectValidate rejectKind = "validate"
)

// rejectedRecord is one JSONL record that did not survive record-level
// validation. Reason is the validator's message, not a summary of it.
type rejectedRecord struct {
	Line   int        `json:"line"`
	ID     string     `json:"id,omitempty"`
	Reason string     `json:"reason"`
	Kind   rejectKind `json:"kind"`

	// raw is the source line, preserved verbatim for the quarantine file. It
	// is unexported so it never leaks into --json output, which would double
	// the payload for no benefit.
	raw string
}

// maxRejectWarnings bounds the per-record warnings printed to stderr. A file
// that is wholly malformed should not scroll the real error off the terminal;
// the summary line and the quarantine file carry the rest.
const maxRejectWarnings = 10

// importVocabulary reads the custom statuses and issue types that the writer
// will validate against. Returning an error means the pre-filter must not run:
// validating against an incomplete vocabulary would reject records the writer
// would have accepted, which is a worse failure than the one being fixed.
func importVocabulary(ctx context.Context, store storage.DoltStorage) (customStatuses, customTypes []string, err error) {
	if store == nil {
		return nil, nil, fmt.Errorf("no store")
	}
	customStatuses, err = store.GetCustomStatuses(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reading custom statuses: %w", err)
	}
	customTypes, err = store.GetCustomTypes(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reading custom types: %w", err)
	}
	return customStatuses, customTypes, nil
}

// validateImportRecord reports whether the writer would accept this record.
//
// It runs PrepareIssueForInsert — the exact function the create path calls
// first, before it issues any SQL for the row — against a copy, so the
// normalization it performs (timestamp defaulting, closed_at repair, content
// hashing) does not leak onto a record that is about to be handed to the real
// writer. Validating the original here and the copy there would let the two
// disagree about, say, a closed issue missing closed_at, which the writer
// repairs rather than rejects.
func validateImportRecord(issue *types.Issue, customStatuses, customTypes []string) error {
	if issue == nil {
		return fmt.Errorf("nil record")
	}
	probe := *issue
	return issueops.PrepareIssueForInsert(&probe, customStatuses, customTypes)
}

// partitionImportRecords splits parsed records into those the writer will
// accept and those it will not. sources must be index-aligned with issues; it
// may be nil or shorter, in which case the rejection simply carries no line
// number and no raw text to quarantine.
func partitionImportRecords(
	issues []*types.Issue,
	sources []recordSource,
	customStatuses, customTypes []string,
) (valid []*types.Issue, rejected []rejectedRecord) {
	valid = make([]*types.Issue, 0, len(issues))

	for i, issue := range issues {
		var src recordSource
		if i < len(sources) {
			src = sources[i]
		}
		if err := validateImportRecord(issue, customStatuses, customTypes); err != nil {
			id := ""
			if issue != nil {
				id = issue.ID
			}
			rejected = append(rejected, rejectedRecord{
				Line:   src.Line,
				ID:     id,
				Reason: err.Error(),
				Kind:   rejectValidate,
				raw:    src.Raw,
			})
			continue
		}
		valid = append(valid, issue)
	}
	return valid, rejected
}

// orderRejects returns the rejections in source order. Unparseable lines are
// collected during the scan and validation failures afterwards, so without this
// the warnings and the quarantine file come out interleaved by failure kind
// rather than by line — which reads as noise next to the source file.
func orderRejects(rejected []rejectedRecord) []rejectedRecord {
	sort.SliceStable(rejected, func(i, j int) bool { return rejected[i].Line < rejected[j].Line })
	return rejected
}

// reportRejectedRecords writes the per-record warnings and the summary count.
// Output goes to stderr so it survives `bd import ... | jq` and never
// contaminates --json stdout.
func reportRejectedRecords(w io.Writer, source string, rejected []rejectedRecord, quarantinePath string, strictHint bool) {
	if len(rejected) == 0 {
		return
	}
	shown := len(rejected)
	if shown > maxRejectWarnings {
		shown = maxRejectWarnings
	}
	for _, r := range rejected[:shown] {
		fmt.Fprintf(w, "warning: skipped invalid record%s%s: %s\n",
			formatRejectLine(r.Line), formatRejectID(r.ID), r.Reason)
	}
	if len(rejected) > shown {
		fmt.Fprintf(w, "warning: ... and %d more invalid record(s)\n", len(rejected)-shown)
	}

	label := source
	if label == "" {
		label = "input"
	}
	hint := ""
	if strictHint {
		hint = "; run with --strict to fail instead"
	}
	fmt.Fprintf(w, "warning: %d invalid record(s) skipped from %s%s\n", len(rejected), label, hint)
	if quarantinePath != "" {
		fmt.Fprintf(w, "warning: skipped records written to %s\n", quarantinePath)
	}
}

func formatRejectLine(line int) string {
	if line <= 0 {
		return ""
	}
	return fmt.Sprintf(" on line %d", line)
}

func formatRejectID(id string) string {
	if id == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", id)
}

// firstRejectError renders the rejected batch as the error --strict returns.
// It names the first fault in full and counts the rest, so the strict message
// stays as specific as today's while admitting there may be more.
func firstRejectError(rejected []rejectedRecord) error {
	if len(rejected) == 0 {
		return nil
	}
	first := rejected[0]
	msg := fmt.Sprintf("invalid record%s%s: %s",
		formatRejectLine(first.Line), formatRejectID(first.ID), first.Reason)
	if len(rejected) > 1 {
		msg += fmt.Sprintf(" (and %d more invalid record(s))", len(rejected)-1)
	}
	return fmt.Errorf("%s", msg)
}

// firstParseReject returns the first unparseable-line rejection, or nil if the
// batch only contains records the writer refused. The recovery paths use this
// to keep hard-failing on a corrupt file while still tolerating a bad row.
func firstParseReject(rejected []rejectedRecord) *rejectedRecord {
	for i := range rejected {
		if rejected[i].Kind == rejectParse {
			return &rejected[i]
		}
	}
	return nil
}

// rejectFilePath is the quarantine path for a JSONL file that was imported by
// path: alongside the source, suffixed. Callers that import from stdin have no
// natural sibling and pass an explicit path instead.
func rejectFilePath(sourcePath string) string {
	if sourcePath == "" {
		return ""
	}
	return sourcePath + ".rejected.jsonl"
}

// writeRejectFile quarantines the rejected source lines verbatim so the data is
// recoverable: fix the file and re-import just it. A record whose raw text was
// not captured is skipped rather than reconstructed — a re-serialized record is
// not the user's line, and silently substituting one would make the quarantine
// file untrustworthy as a re-import source.
//
// The file is rewritten (not appended) so it always describes the most recent
// import of that source rather than accumulating across runs.
func writeRejectFile(path string, rejected []rejectedRecord) error {
	if path == "" || len(rejected) == 0 {
		return nil
	}
	var b strings.Builder
	n := 0
	for _, r := range rejected {
		if strings.TrimSpace(r.raw) == "" {
			continue
		}
		b.WriteString(r.raw)
		b.WriteString("\n")
		n++
	}
	if n == 0 {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", path, err)
		}
	}
	// 0600: the quarantine file is a verbatim copy of issue records, so it
	// carries whatever the source JSONL carried.
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
