package issueops

// Build-time completeness guard for the row_lock optimistic-concurrency token,
// absorbed from gastownhall/beads#4682's revision guard and retargeted at the
// row_lock column current main actually ships (see the freshRowLock invariant
// in lease.go and types.Issue.RowVersion). The PR's substring stamp check
// (strings.Contains(body, "revision")) was false-negative-prone — any comment
// or identifier mentioning the column name counted as a stamp — so this port
// tightens it to the actual mint forms: a freshRowLock()/FreshRowLock()/
// RowLockClause() call in the enclosing function.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// issueTableWriteRe matches the start of an UPDATE or INSERT to an issue-bearing
// table: literal `issues`/`wisps`, or a `%s`-templated table (the routed
// issues/wisps funnels build the table name with WispTableRouting/pickIssueTable
// and interpolate it as %s). Auxiliary tables reached through the same %s
// templating are filtered out by their distinguishing columns (auxOrExemptMarkers).
var issueTableWriteRe = regexp.MustCompile(`(?i)\b(?:UPDATE|INSERT\s+INTO)\s+(?:issues|wisps|%s)\b`)

// rowLockStampRe matches the forms that actually mint a fresh token: a direct
// freshRowLock()/FreshRowLock() call (close, reopen, unclaim, reclaim, insert)
// or RowLockClause() (claim and the proxied domain/db paths, which interpolate
// the returned "row_lock = ?" fragment into their SET clause).
var rowLockStampRe = regexp.MustCompile(`\b(?:freshRowLock|FreshRowLock|RowLockClause)\s*\(`)

// auxOrExemptMarkers identify a matched write that legitimately does NOT stamp
// row_lock. The exemption set is main's freshRowLock invariant (lease.go):
// row_lock guards status/assignee/started_at against the reclaim/close races,
// and paths touching only orthogonal cells are safe to merge with a reclaim.
//
//   - is_blocked: the denormalized is_blocked recompute deliberately preserves
//     updated_at (blocked_state.go x4, dependencies.go, domain/db/dependency.go)
//     and must not remint row_lock either — an aux-marker refresh that bumped
//     the token would clobber a concurrent whole-row CAS (ExpectedVersion) for
//     no content change. Analysis absorbed from gastownhall/beads#4682.
//     (The PR's other exemption, the lease heartbeat, is moot on main: since
//     bd-lrgn1 heartbeats live entirely in the ephemeral leases table and never
//     touch the issues row.)
//   - "compaction_level = ": compaction apply/restore rewrites bookkeeping and
//     compacted body text only — cells the reclaim/close races don't care
//     about, exempted by name in the freshRowLock invariant. The assignment
//     form (not the bare column name) keeps the create INSERT, whose column
//     list also names compaction_level, checkable.
//   - "SET id = ?": rename (updateIssueIDInTx/updateWispIDInTx) is the only
//     writer of the primary key, exempted by name in the freshRowLock
//     invariant.
//   - "SELECT %s FROM": the persistence move's fully-templated aux-table copy
//     (INSERT INTO %s (%s) SELECT %s FROM %s ...) — it copies snapshot/event
//     side tables verbatim; the issues/wisps row itself is re-inserted through
//     InsertIssueStrictInTx, which stamps.
//   - the remainder are columns unique to AUXILIARY tables reached through the
//     same %s templating (events / dependencies / child_counters / snapshots /
//     comments). Issue rows key on `id` and never carry these, so their
//     presence proves a non-issue table.
//
// If a future write trips this guard: stamp row_lock with freshRowLock() (or
// RowLockClause()) when it is a real issues/wisps content write; add the
// distinguishing column/marker here — with the WHY — when it is a new
// auxiliary-table or deliberately-orthogonal write.
var auxOrExemptMarkers = []string{
	"is_blocked",          // is_blocked recompute (exempt by design)
	"compaction_level = ", // compaction bookkeeping/restore (exempt by design)
	"SET id = ?",          // rename: primary-key rewrite (exempt by design)
	"SELECT %s FROM",      // persistence move's verbatim aux-table copy
	"issue_id",            // events / dependencies / comments / snapshots FK
	"depends_on",          // dependency edge + retarget writes
	"parent_id",           // child_counters
	"last_child",          // child_counters
	"event_type",          // events / wisp_events
}

// TestAllIssueRowWritesStampRowLock is the load-bearing completeness guard for
// the freshRowLock invariant: it proves, at build time, that EVERY issues/wisps
// row write across both whole-row write stacks (issueops and the proxied
// internal/storage/domain/db) either stamps a fresh row_lock or matches a
// documented exemption. A forgotten write path is a test failure, not a silent
// reintroduction of the zombie-merge bug or a stale-CAS hole in RowVersion.
func TestAllIssueRowWritesStampRowLock(t *testing.T) {
	dirs := []string{".", filepath.Join("..", "domain", "db")}
	checked := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			n, violations := scanIssueWriteRowLockStamps(t, path, src)
			checked += n
			for _, v := range violations {
				t.Error(v)
			}
		}
	}
	if checked == 0 {
		t.Fatal("guard verified zero issue-table writes — the scan regex or directory set is wrong")
	}
	t.Logf("verified %d issues/wisps row writes across both stacks stamp row_lock", checked)
}

// scanIssueWriteRowLockStamps parses one Go source file and returns the number
// of issue-table writes it verified plus a violation message for each such write
// whose enclosing function does not mint a fresh row_lock. Split out so the
// guard's teeth can be tested against synthetic sources (TestRowLockGuardHasTeeth).
func scanIssueWriteRowLockStamps(t *testing.T, path string, src []byte) (checked int, violations []string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		start := fset.Position(fn.Body.Pos()).Offset
		end := fset.Position(fn.Body.End()).Offset
		body := string(src[start:end])
		// The routed funnels (update.go, domain/db) assemble the SET clause
		// separately from the UPDATE literal, so verify the mint against the
		// whole function body, not just the write literal.
		stampsRowLock := rowLockStampRe.MatchString(body)

		for _, loc := range issueTableWriteRe.FindAllStringIndex(body, -1) {
			// Classify by the tight enclosing SQL literal so an adjacent
			// statement's columns can't bleed into this write's markers.
			stmt := enclosingSQLLiteral(body, loc[0])
			if hasAnyMarker(stmt, auxOrExemptMarkers) {
				continue // auxiliary table or documented exemption: no stamp
			}
			checked++
			if !stampsRowLock {
				violations = append(violations, path+": "+funcDisplayName(fn)+
					" performs an issues/wisps row write that does not stamp row_lock:\n\t"+
					firstSQLLine(stmt)+
					"\nEvery issues/wisps content write must mint a fresh token via freshRowLock()/RowLockClause() (see the freshRowLock invariant in lease.go), or carry a documented exemption marker.")
			}
		}
		return true
	})
	return checked, violations
}

// TestRowLockGuardHasTeeth proves the guard actually flags an unstamped
// issues/wisps write and passes a correctly-stamped one — so a green
// TestAllIssueRowWritesStampRowLock means something.
func TestRowLockGuardHasTeeth(t *testing.T) {
	stamped := []byte(`package p
func w(tx T, id, s string) {
	tx.ExecContext(ctx, "UPDATE issues SET status = ?, row_lock = ? WHERE id = ?", s, freshRowLock(), id)
}`)
	if n, v := scanIssueWriteRowLockStamps(t, "stamped.go", stamped); len(v) != 0 || n != 1 {
		t.Errorf("stamped write: got checked=%d violations=%v; want checked=1 violations=none", n, v)
	}

	unstamped := []byte(`package p
func w(tx T, id, s string) {
	tx.ExecContext(ctx, "UPDATE issues SET status = ? WHERE id = ?", s, id)
}`)
	if n, v := scanIssueWriteRowLockStamps(t, "unstamped.go", unstamped); n != 1 || len(v) != 1 {
		t.Errorf("unstamped write: got checked=%d violations=%d; want checked=1 violations=1", n, len(v))
	}

	// The tightened matcher: a body that merely MENTIONS row_lock — here a
	// token-preserving self-assignment, but a comment would do — without
	// minting a fresh one must still be flagged. The PR's substring check
	// (strings.Contains(body, "revision")) passed exactly this shape.
	mentionOnly := []byte(`package p
func w(tx T, id, s string) {
	// row_lock intentionally not reminted (WRONG: status writes must mint).
	tx.ExecContext(ctx, "UPDATE issues SET status = ?, row_lock = row_lock WHERE id = ?", s, id)
}`)
	if n, v := scanIssueWriteRowLockStamps(t, "mention.go", mentionOnly); n != 1 || len(v) != 1 {
		t.Errorf("mention-only write: got checked=%d violations=%d; want checked=1 violations=1 (substring match must not count as a stamp)", n, len(v))
	}

	// An auxiliary-table write (has issue_id) must be ignored, stamped or not.
	aux := []byte(`package p
func w(tx T, id, s string) {
	tx.ExecContext(ctx, "INSERT INTO %s (id, issue_id, event_type) VALUES (?, ?, ?)", a, b, c)
}`)
	if n, v := scanIssueWriteRowLockStamps(t, "aux.go", aux); n != 0 || len(v) != 0 {
		t.Errorf("aux write: got checked=%d violations=%v; want checked=0 violations=none", n, v)
	}
}

func hasAnyMarker(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

func funcDisplayName(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		if t, ok := receiverTypeName(fn.Recv.List[0].Type); ok {
			return "(" + t + ")." + fn.Name.Name
		}
	}
	return fn.Name.Name
}

func receiverTypeName(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name, true
	}
	return "", false
}

func firstSQLLine(window string) string {
	for _, line := range strings.Split(window, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(window)
}

// enclosingSQLLiteral returns the Go string literal (backtick-raw or
// double-quoted) that contains the byte at matchStart, without its delimiters.
// The SQL write statements here never span more than one literal, and neither
// delimiter appears inside these SQL bodies (which use single quotes), so a
// nearest-delimiter scan bounds the statement exactly — no bleed into adjacent
// statements. Falls back to a fixed window if no delimiter is found.
func enclosingSQLLiteral(body string, matchStart int) string {
	open, delim := -1, byte(0)
	for i := matchStart; i >= 0; i-- {
		if body[i] == '`' || body[i] == '"' {
			open, delim = i, body[i]
			break
		}
	}
	if open < 0 {
		end := matchStart + 400
		if end > len(body) {
			end = len(body)
		}
		return body[matchStart:end]
	}
	for i := open + 1; i < len(body); i++ {
		if body[i] == delim && (delim == '`' || body[i-1] != '\\') {
			return body[open+1 : i]
		}
	}
	return body[open+1:]
}
