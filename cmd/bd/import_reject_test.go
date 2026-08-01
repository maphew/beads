package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// GH#4492: a JSONL import aborts in its entirety on the first record the writer
// refused, and reported nothing useful about which record or why. These tests
// pin the replacement contract: the abort stays the default, but it names the
// offending line and the way past it, and --skip-invalid drops the bad records
// while the good ones survive.

func validImportIssue(id, title string) *types.Issue {
	return &types.Issue{
		ID:        id,
		Title:     title,
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestPartitionImportRecordsKeepsValidRecords(t *testing.T) {
	issues := []*types.Issue{
		validImportIssue("tst-1", "good one"),
		{ID: "tst-2", Title: "bad status", Status: "verify", IssueType: types.TypeTask},
		validImportIssue("tst-3", "good two"),
		{ID: "tst-4", Title: "", Status: types.StatusOpen, IssueType: types.TypeTask},
	}
	sources := []recordSource{
		{Line: 1, Raw: "line-1"},
		{Line: 2, Raw: "line-2"},
		{Line: 3, Raw: "line-3"},
		{Line: 4, Raw: "line-4"},
	}

	valid, rejected := partitionImportRecords(issues, sources, nil, nil)

	if len(valid) != 2 || valid[0].ID != "tst-1" || valid[1].ID != "tst-3" {
		t.Fatalf("valid = %#v, want tst-1 and tst-3", valid)
	}
	if len(rejected) != 2 {
		t.Fatalf("rejected = %#v, want 2", rejected)
	}
	if rejected[0].Line != 2 || rejected[0].ID != "tst-2" {
		t.Errorf("rejected[0] = %+v, want line 2 / tst-2", rejected[0])
	}
	if !strings.Contains(rejected[0].Reason, "invalid status: verify") {
		t.Errorf("rejected[0].Reason = %q, want the validator's own message", rejected[0].Reason)
	}
	if rejected[0].raw != "line-2" {
		t.Errorf("rejected[0].raw = %q, want the source line verbatim", rejected[0].raw)
	}
	if rejected[1].Line != 4 || !strings.Contains(rejected[1].Reason, "title is required") {
		t.Errorf("rejected[1] = %+v, want line 4 / title is required", rejected[1])
	}
	for i, r := range rejected {
		if r.Kind != rejectValidate {
			t.Errorf("rejected[%d].Kind = %q, want %q", i, r.Kind, rejectValidate)
		}
	}
}

// The recovery paths hard-fail on a corrupt file but tolerate a bad row, so
// the two kinds must stay distinguishable after they are merged and sorted.
func TestFirstParseRejectFindsOnlyParseFailures(t *testing.T) {
	validateOnly := []rejectedRecord{
		{Line: 2, Kind: rejectValidate},
		{Line: 7, Kind: rejectValidate},
	}
	if got := firstParseReject(validateOnly); got != nil {
		t.Errorf("firstParseReject(validate-only) = %+v, want nil", got)
	}
	if got := firstParseReject(nil); got != nil {
		t.Errorf("firstParseReject(nil) = %+v, want nil", got)
	}

	mixed := orderRejects([]rejectedRecord{
		{Line: 9, Kind: rejectParse},
		{Line: 2, Kind: rejectValidate},
		{Line: 5, Kind: rejectParse},
	})
	got := firstParseReject(mixed)
	if got == nil || got.Line != 5 {
		t.Errorf("firstParseReject(mixed) = %+v, want the line-5 parse failure", got)
	}
}

// The whole point of the pre-filter is that it agrees with the writer. A custom
// status configured in the database is valid there, so rejecting it here would
// turn a working import into silent data loss — strictly worse than the bug
// being fixed.
func TestPartitionImportRecordsAcceptsCustomVocabulary(t *testing.T) {
	issues := []*types.Issue{
		{ID: "tst-1", Title: "custom status", Status: "verify", IssueType: types.TypeTask, Priority: 2},
		{ID: "tst-2", Title: "custom type", Status: types.StatusOpen, IssueType: "widget", Priority: 2},
	}

	if _, rejected := partitionImportRecords(issues, nil, nil, nil); len(rejected) != 2 {
		t.Fatalf("without custom vocabulary: rejected = %d, want 2 (guards the test below)", len(rejected))
	}

	valid, rejected := partitionImportRecords(issues, nil, []string{"verify"}, []string{"widget"})
	if len(rejected) != 0 {
		t.Fatalf("rejected = %#v, want none: both records use configured custom values", rejected)
	}
	if len(valid) != 2 {
		t.Fatalf("valid = %d, want 2", len(valid))
	}
}

// The probe must not normalize the record that is about to be handed to the
// real writer: PrepareIssueForInsert defaults timestamps and computes a content
// hash, and doing that twice against different vocabularies is how the
// pre-filter and the writer would start disagreeing.
func TestValidateImportRecordDoesNotMutate(t *testing.T) {
	issue := &types.Issue{
		ID:        "tst-1",
		Title:     "no timestamps",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
	}
	if err := validateImportRecord(issue, nil, nil); err != nil {
		t.Fatalf("validateImportRecord: %v", err)
	}
	if !issue.CreatedAt.IsZero() || !issue.UpdatedAt.IsZero() {
		t.Errorf("timestamps were defaulted on the original: created=%v updated=%v", issue.CreatedAt, issue.UpdatedAt)
	}
	if issue.ContentHash != "" {
		t.Errorf("ContentHash = %q, want empty: the writer computes it", issue.ContentHash)
	}
}

// A closed issue with no closed_at is repaired by the writer, not rejected by
// it, so the pre-filter must not reject it either.
func TestValidateImportRecordAcceptsClosedWithoutClosedAt(t *testing.T) {
	issue := &types.Issue{
		ID:        "tst-1",
		Title:     "closed, no closed_at",
		Status:    types.StatusClosed,
		IssueType: types.TypeTask,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := validateImportRecord(issue, nil, nil); err != nil {
		t.Fatalf("validateImportRecord: %v, want accepted (the writer repairs closed_at)", err)
	}
	if issue.ClosedAt != nil {
		t.Errorf("ClosedAt was set on the original record; the repair belongs to the writer")
	}
}

func TestParseJSONLFileCollectsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.jsonl")
	content := strings.Join([]string{
		`{"_schema":"beads-jsonl/1"}`,
		`{"id":"tst-1","title":"good","status":"open","issue_type":"task"}`,
		`{"id":"tst-2","title":"truncated`,
		`{"_type":"memory","key":"k","value":"v"}`,
		`{"id":"tst-3","title":"good two","status":"open","issue_type":"task"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	parsed, err := parseJSONLFile(path)
	if err != nil {
		t.Fatalf("parseJSONLFile: %v — one malformed line must not fail the file", err)
	}
	if len(parsed.Issues) != 2 {
		t.Fatalf("Issues = %d, want 2", len(parsed.Issues))
	}
	if len(parsed.Config) != 1 {
		t.Errorf("Config = %#v, want the one memory record", parsed.Config)
	}
	if len(parsed.Rejected) != 1 {
		t.Fatalf("Rejected = %#v, want 1", parsed.Rejected)
	}
	if parsed.Rejected[0].Line != 3 {
		t.Errorf("Rejected[0].Line = %d, want 3", parsed.Rejected[0].Line)
	}
	if parsed.Rejected[0].raw != `{"id":"tst-2","title":"truncated` {
		t.Errorf("Rejected[0].raw = %q, want the source line verbatim", parsed.Rejected[0].raw)
	}
	if parsed.Rejected[0].Kind != rejectParse {
		t.Errorf("Rejected[0].Kind = %q, want %q", parsed.Rejected[0].Kind, rejectParse)
	}
	// Sources index the surviving records, and must point at the real line
	// numbers — the header and the malformed line both consumed one.
	if len(parsed.Sources) != 2 || parsed.Sources[0].Line != 2 || parsed.Sources[1].Line != 5 {
		t.Errorf("Sources = %#v, want lines 2 and 5", parsed.Sources)
	}
}

func TestOrderRejectsSortsBySourceLine(t *testing.T) {
	got := orderRejects([]rejectedRecord{{Line: 9}, {Line: 2}, {Line: 0}, {Line: 4}})
	want := []int{0, 2, 4, 9}
	for i, r := range got {
		if r.Line != want[i] {
			t.Fatalf("orderRejects lines = %v, want %v", got, want)
		}
	}
}

func TestWriteRejectFileIsVerbatimAndRewrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "rejects.jsonl")

	wrote, err := writeRejectFile(path, []rejectedRecord{
		{Line: 1, raw: `{"a":1}`},
		{Line: 2, raw: ""}, // no captured source — must be skipped, not invented
		{Line: 3, raw: `{"b":2}`},
	})
	if err != nil {
		t.Fatalf("writeRejectFile: %v", err)
	}
	if !wrote {
		t.Fatalf("writeRejectFile wrote = false, want true: two records had captured raw text")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "{\"a\":1}\n{\"b\":2}\n" {
		t.Fatalf("rejects file = %q, want the two captured lines verbatim", string(data))
	}

	// A later import of the same source must describe that import, not append
	// to the previous one.
	if wrote, err := writeRejectFile(path, []rejectedRecord{{Line: 1, raw: `{"c":3}`}}); err != nil {
		t.Fatalf("writeRejectFile (rewrite): %v", err)
	} else if !wrote {
		t.Fatalf("writeRejectFile (rewrite) wrote = false, want true")
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "{\"c\":3}\n" {
		t.Fatalf("rejects file = %q, want only the second run's record", string(data))
	}
}

func TestWriteRejectFileSkipsWhenNothingCaptured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rejects.jsonl")
	wrote, err := writeRejectFile(path, []rejectedRecord{{Line: 1, raw: "  "}})
	if err != nil {
		t.Fatalf("writeRejectFile: %v", err)
	}
	if wrote {
		t.Fatalf("writeRejectFile wrote = true, want false: no record had captured raw text")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no quarantine file when no raw line was captured, stat err = %v", err)
	}
}

// bd review (dual-vendor pass on PR 5202): a rerun that produces zero rejects
// must not leave a stale rejects file from a previous run in place looking
// current.
func TestWriteRejectFileRemovesStaleFileWhenNothingToWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rejects.jsonl")
	if wrote, err := writeRejectFile(path, []rejectedRecord{{Line: 1, raw: `{"a":1}`}}); err != nil || !wrote {
		t.Fatalf("writeRejectFile (seed): wrote=%v err=%v, want true/nil", wrote, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("seed file missing: %v", err)
	}

	// A rerun with nothing to quarantine (empty batch, or every raw text
	// uncaptured) must remove the stale file rather than leave it behind.
	wrote, err := writeRejectFile(path, nil)
	if err != nil {
		t.Fatalf("writeRejectFile (rerun): %v", err)
	}
	if wrote {
		t.Fatalf("writeRejectFile (rerun) wrote = true, want false: nothing to write")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale rejects file still present after a clean rerun, stat err = %v", err)
	}

	// Removing a file that was never there is not an error.
	if wrote, err := writeRejectFile(path, nil); err != nil || wrote {
		t.Fatalf("writeRejectFile (no file, nothing to write): wrote=%v err=%v, want false/nil", wrote, err)
	}
}

func TestRejectPathCollidesWithSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "issues.jsonl")
	if err := os.WriteFile(source, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Direct same path.
	if collide, err := rejectPathCollidesWithSource(source, source); err != nil || !collide {
		t.Fatalf("collide=%v err=%v, want true/nil for the identical path", collide, err)
	}

	// Relative alias of the same file.
	rel := filepath.Join(dir, "..", filepath.Base(dir), "issues.jsonl")
	if collide, err := rejectPathCollidesWithSource(rel, source); err != nil || !collide {
		t.Fatalf("collide=%v err=%v, want true/nil for a relative alias of the same file", collide, err)
	}

	// A different file in the same directory does not collide.
	other := filepath.Join(dir, "issues.rejected.jsonl")
	if collide, err := rejectPathCollidesWithSource(other, source); err != nil || collide {
		t.Fatalf("collide=%v err=%v, want false/nil for a distinct file", collide, err)
	}

	// stdin (empty sourcePath) never collides.
	if collide, err := rejectPathCollidesWithSource(source, ""); err != nil || collide {
		t.Fatalf("collide=%v err=%v, want false/nil when importing from stdin", collide, err)
	}

	// A symlinked reject path that resolves to the source path collides too.
	symlinkPath := filepath.Join(dir, "alias.jsonl")
	if err := os.Symlink(source, symlinkPath); err != nil {
		t.Skipf("symlinks unsupported in this environment: %v", err)
	}
	if collide, err := rejectPathCollidesWithSource(symlinkPath, source); err != nil || !collide {
		t.Fatalf("collide=%v err=%v, want true/nil for a symlink to the source", collide, err)
	}
}

func TestReportRejectedRecordsCapsPerRecordWarnings(t *testing.T) {
	var rejected []rejectedRecord
	for i := 1; i <= maxRejectWarnings+5; i++ {
		rejected = append(rejected, rejectedRecord{Line: i, Reason: "bad"})
	}
	var buf bytes.Buffer
	reportRejectedRecords(&buf, "in.jsonl", rejected, "in.jsonl.rejected.jsonl")

	out := buf.String()
	if got := strings.Count(out, "skipped invalid record on line"); got != maxRejectWarnings {
		t.Errorf("per-record warnings = %d, want %d", got, maxRejectWarnings)
	}
	for _, want := range []string{
		"and 5 more invalid record(s)",
		"15 invalid record(s) skipped from in.jsonl",
		"written to in.jsonl.rejected.jsonl",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\ngot:\n%s", want, out)
		}
	}
}

// Every caller of this report has already decided to skip — the CLI only
// reaches it under --skip-invalid, and the recovery paths have no choice. So
// there is nothing here to advertise, and a hint would be telling the user
// about a mode they are already in.
func TestReportRejectedRecordsAdvertisesNoAlternativeMode(t *testing.T) {
	var buf bytes.Buffer
	reportRejectedRecords(&buf, "in.jsonl", []rejectedRecord{{Line: 1, Reason: "bad"}}, "")
	for _, unwanted := range []string{"--strict", "--skip-invalid"} {
		if strings.Contains(buf.String(), unwanted) {
			t.Errorf("report advertises %s to a caller that is already skipping:\n%s", unwanted, buf.String())
		}
	}
	if strings.Contains(buf.String(), "written to") {
		t.Errorf("report claims a quarantine file that was not written:\n%s", buf.String())
	}
}

func TestFirstRejectErrorNamesFirstFaultAndCountsRest(t *testing.T) {
	err := firstRejectError([]rejectedRecord{
		{Line: 2, ID: "tst-2", Reason: "invalid status: verify"},
		{Line: 4, Reason: "failed to parse JSONL line"},
	})
	if err == nil {
		t.Fatal("firstRejectError = nil, want an error")
	}
	msg := err.Error()
	for _, want := range []string{"line 2", "tst-2", "invalid status: verify", "and 1 more"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
	if firstRejectError(nil) != nil {
		t.Error("firstRejectError(nil) should be nil")
	}
}

// Strict-by-default keeps the reporter of GH#4492 in exactly the position they
// started in: plain `bd import` on a file with one bad row still imports
// nothing. The only thing that changes their outcome is the failure telling
// them the escape hatch exists, so that hint is load-bearing, not decoration.
func TestFirstRejectErrorNamesTheEscapeHatch(t *testing.T) {
	err := firstRejectError([]rejectedRecord{{Line: 2, Reason: "invalid status: verify"}})
	if err == nil {
		t.Fatal("firstRejectError = nil, want an error")
	}
	msg := err.Error()
	for _, want := range []string{"--skip-invalid", "--rejects"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not tell the user about %s; without it the default leaves\n"+
				"the reporter of GH#4492 with the same empty import they filed about:\n%s", want, msg)
		}
	}
	// Nothing partial landed, and the message has to say so: a user who thinks
	// some rows imported will fix the file and re-import, double-applying it.
	if !strings.Contains(msg, "nothing was imported") {
		t.Errorf("error does not say the import was a no-op:\n%s", msg)
	}
}

type fakeVocabStore struct {
	storage.DoltStorage
	statuses    []string
	types       []string
	statusesErr error
	typesErr    error
}

func (f *fakeVocabStore) GetCustomStatuses(context.Context) ([]string, error) {
	return f.statuses, f.statusesErr
}

func (f *fakeVocabStore) GetCustomTypes(context.Context) ([]string, error) {
	return f.types, f.typesErr
}

func TestImportVocabularyFailsClosed(t *testing.T) {
	ctx := context.Background()

	statuses, issueTypes, err := importVocabulary(ctx, &fakeVocabStore{statuses: []string{"verify"}, types: []string{"widget"}})
	if err != nil {
		t.Fatalf("importVocabulary: %v", err)
	}
	if len(statuses) != 1 || statuses[0] != "verify" || len(issueTypes) != 1 || issueTypes[0] != "widget" {
		t.Fatalf("vocabulary = %v / %v, want [verify] / [widget]", statuses, issueTypes)
	}

	// A partial read must be an error, not an empty vocabulary: pre-filtering
	// against half the custom statuses would reject valid records.
	if _, _, err := importVocabulary(ctx, &fakeVocabStore{statusesErr: os.ErrPermission}); err == nil {
		t.Error("importVocabulary with a failing status read = nil error, want error")
	}
	if _, _, err := importVocabulary(ctx, &fakeVocabStore{typesErr: os.ErrPermission}); err == nil {
		t.Error("importVocabulary with a failing type read = nil error, want error")
	}
	if _, _, err := importVocabulary(ctx, nil); err == nil {
		t.Error("importVocabulary(nil store) = nil error, want error")
	}
}

// bd review (dual-vendor pass on PR 5202, P1): --rejects is user-supplied and
// writeRejectFile truncates and rewrites it. If it names the import source —
// directly or via a relative alias — the source must be refused, not
// destroyed. This exercises the actual runImportFromReader wiring, not just
// the rejectPathCollidesWithSource helper.
func TestRunImportFromReaderRefusesRejectPathCollidingWithSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "issues.jsonl")
	original := `{"id":"tst-1","title":"bad status","status":"verify","issue_type":"task"}` + "\n"
	if err := os.WriteFile(source, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	origStore, origRejects, origSkip, origJSON := store, importRejects, importSkipInvalid, jsonOutput
	t.Cleanup(func() {
		store, importRejects, importSkipInvalid, jsonOutput = origStore, origRejects, origSkip, origJSON
	})
	store = &fakeVocabStore{}
	importSkipInvalid = true
	jsonOutput = false

	cases := []struct {
		name       string
		rejectPath string
	}{
		{"direct same path", source},
		{"relative alias", filepath.Join(dir, "..", filepath.Base(dir), "issues.jsonl")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			importRejects = tc.rejectPath

			f, err := os.Open(source)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close()

			err = runImportFromReader(context.Background(), f, source, source)
			if err == nil {
				t.Fatal("runImportFromReader = nil error, want a collision refusal")
			}
			if !strings.Contains(err.Error(), "same file as the import source") {
				t.Errorf("error = %q, want it to name the collision", err.Error())
			}

			data, rerr := os.ReadFile(source)
			if rerr != nil {
				t.Fatalf("ReadFile: %v", rerr)
			}
			if string(data) != original {
				t.Fatalf("source file was modified by the refused import: got %q, want unchanged %q", data, original)
			}
		})
	}
}
