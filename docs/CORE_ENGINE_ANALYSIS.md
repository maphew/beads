# Beads — Core Engine Analysis (Identity + Graph/Readiness)

> Unfolds the two load-bearing *strengths* of the skeleton: the distributed
> identity scheme (hash-IDs + content hash + deterministic edge IDs) and the
> dependency-graph / ready-work engine. Companion to
> [ARCHITECTURE_ANALYSIS.md](ARCHITECTURE_ANALYSIS.md); tracked in
> [ANALYSIS_INDEX.md](ANALYSIS_INDEX.md) (#3/#4/#5).
>
> Why this matters: the prior deep-dives (plugins, wisp tables) were both
> *weaknesses*. This document verifies the parts that are supposed to be
> *strengths* — and they largely are, with two specific, confirmed exceptions.

---

## Part 1 — Distributed identity (the keystone)

The headline claim ("hash-based IDs prevent merge collisions") is real but
under-described. There are actually **three distinct identity mechanisms running
two opposite philosophies**, and the asymmetry is principled.

### 1a. Issue IDs — entropy-based (probabilistic uniqueness)
Live path: `idgen.GenerateHashID` (`internal/idgen/hash.go`), called from
`issueops.GenerateIssueIDInTable` (`helpers.go:148`).

- `SHA256(title | description | creator | timestamp.UnixNano() | nonce)` →
  base36, with byte-width scaling.
- **Seeded on nanosecond timestamp + nonce → effectively random per creation, NOT
  content-deterministic.** Two agents creating the same title get different IDs.
  This is *collision-avoidance by entropy*, not content-addressing.
- **Adaptive length** (`GetAdaptiveIDLengthTx` → `ComputeAdaptiveLength`):
  default config `MaxCollisionProbability 0.25, Min 3, Max 8`. The ID grows
  (3→8 chars) as the table fills, keeping collision probability bounded.
- **Collision loop:** for `length` in `base..8`, for `nonce` in `0..9`, generate
  and `SELECT COUNT(*) ... WHERE id = ?`. The check is **local to the clone only.**
  Cross-clone uniqueness is purely the birthday bound, mitigated by adaptive
  growth.

**Why random, not content-addressed?** Because issue *content is not unique* —
two issues can legitimately share a title/description. There is no natural key to
content-address, so entropy is the correct choice.

### 1b. Dependency edge IDs — content-addressed (deterministic, merge-safe)
`internal/storage/depid/depid.go`: `uuid.NewSHA1(Namespace, issueID + "\x1f" +
target)` — a deterministic UUIDv5 over the edge's natural key `(issue_id,
target)`, with a fixed namespace and a Unit-Separator delimiter that cannot occur
in IDs.

- **The same edge yields the same primary key on every clone → merges cleanly
  by construction.** This is the fix for the #4259 saga (random `DEFAULT (UUID())`
  PKs diverged across clones and broke `bd dolt pull` unrecoverably).
- Elegantly done: type deliberately excluded from identity (it is not in any
  unique key); namespace frozen forever (changing it re-keys every edge).

**The principled asymmetry:** content-address where a natural key exists (edges),
use entropy where it does not (issues). The #4259 pain was *learning* that edges
must be deterministic. This is the strongest, most hard-won piece of the design.

### 1c. `content_hash` — a third, separate thing
`Issue.ComputeContentHash` (`types/types.go`) is a deterministic fingerprint over
substantive fields, stored in the `content_hash` column and used by the **merge
skip/update decision** ("same ID + same content = skip; same ID + different
content = update", in `dolt/store.go`). It is **not** the ID. The README/
ARCHITECTURE docs blur "hash-based IDs" and "content hashing for dedup" — they are
different mechanisms with different jobs (identity vs. change-detection). Worth
disambiguating for any maintainer touching either.

### 1d. Confirmed findings / risks

1. **Dead, divergent ID code.** `types.GenerateHashID` (hex + progressive
   `hash[:6]→[:7]→[:8]` truncation + `workspaceID`) has **no live callers** — the
   real path is `idgen.GenerateHashID` (base36 + nonce). The *more thoroughly
   documented* generator (`id_generator.go`, with collision-probability tables) is
   the *dead* one. Anyone reading the docs to understand IDs learns the wrong
   algorithm. Delete or reconcile.
2. **"Zero Conflict" has unstated asterisks.** Two opt-in/structural modes
   reintroduce exactly the cross-clone collision the hash scheme prevents:
   - **Counter mode** (`issue_id_mode=counter`): sequential `bd-1, bd-2` via the
     `issue_counter` table — human-friendly, **collision-unsafe across clones.**
   - **Hierarchical child IDs** (`parent.N`): assigned from a `child_counters`
     table (`GetNextChildIDTx`), per-parent sequential. Two clones both minting
     `parent.1` for different content collide → merge conflict.
   The README's "Hash-based IDs prevent merge collisions" should caveat that
   counter mode and child IDs do not. Issue-table conflicts are **not** in the
   merge auto-resolve safe classes (see ARCH §6.4), so they surface to the
   operator — acceptable, but undocumented.

**Net:** the identity design is a genuine strength — `depid` especially is
elegant and the entropy/adaptive scheme is sound — but the docs describe a dead
algorithm and oversell "zero conflict," and issue uniqueness is probabilistic
with a clone-local check.

---

## Part 2 — The dependency-graph / ready-work engine (the core behavior)

"What is ready to work on right now" is the product's central behavior. The
semantic model is clean; the implementation is entirely denormalization-driven
and pays the structural taxes documented elsewhere.

### 2a. The readiness predicate
`GetReadyWork → getReadyWorkUnion` (`domain/db/ready_work*.go`) and the embedded
twin (`issueops/ready_work.go`). The core WHERE reduces to:

```
status IN ('open','in_progress')  AND  (pinned = 0 OR pinned IS NULL)
AND is_blocked = 0
```

plus deferral exclusion (issues with future `defer_until` **and their transitive
children**). **The entire engine hinges on the denormalized `is_blocked`
column** (indexed `idx_issues_is_blocked(is_blocked, status)`) — O(1) at query
time, but see ARCH §6.2 for the maintenance fragility it inherits.

### 2b. The blocking model — 4 active types
`AffectsReadyWork()` = `blocks | parent-child | conditional-blocks | waits-for`.
`is_blocked` is recomputed by **iterative mark/unmark passes until convergence**
(`RecomputeIsBlockedInTx`) on the write path:

- **blocks** — hard blocker while the target is non-closed/non-pinned.
- **parent-child** — a blocked parent propagates `is_blocked=1` to children
  (recursive, via the convergence loop).
- **waits-for** — fanout gate: blocked while spawned children are pending; the
  `any-children` variant unblocks on first closed child. Implemented as nested
  correlated `EXISTS` + `JSON_EXTRACT(metadata,'$.gate')`, with **no metadata
  validation** (parse failure silently defaults to `all-children`).
- **conditional-blocks** — see the confirmed gap below.

### 2c. Cycle detection
Pre-insert recursive CTE (`CheckDependencyCycleInTx`), only for
`blocks`/`conditional-blocks` (the cyclic-capable types), `UNION` for
termination, cross-table (dependencies + wisp_dependencies). Returns yes/no (no
cycle path), skippable via `--no-cycle-check` / `SkipCycleCheck` for bulk wiring.
Sound and appropriately scoped.

### 2d. Sort policies
`hybrid` (default), `priority`, `oldest`. Hybrid: recent issues (< 48h, a
**hardcoded, config-less** cutoff) sorted by priority; older issues FIFO by
creation — so recent P0s don't starve while the backlog still drains. Reasonable;
the magic 48h constant is the only smell.

### 2e. Confirmed correctness gap: `conditional-blocks` is inert
`conditional-blocks` is documented as *"B runs only if A **fails**"* — failure
defined by `IsFailureClose` against `FailureCloseKeywords` (`failed`, `rejected`,
`timeout`, …). **Both `IsFailureClose` and `FailureCloseKeywords` have zero
callers**, and the `is_blocked`/ready SQL **never references `close_reason`.** The
query treats `conditional-blocks` *identically to `blocks`*:

```sql
(d.type = 'blocks' OR d.type = 'conditional-blocks')
AND t.status <> 'closed' AND t.status <> 'pinned'
```

So a `conditional-blocks` edge simply blocks until the target closes, **regardless
of whether the target succeeded or failed.** The distinguishing semantic — the
entire point of the type — is not implemented in the engine. It is a shipped,
documented feature backed by dead code. Either wire `IsFailureClose` into the
readiness computation (it needs `close_reason` in the SQL, which the denormalized
`is_blocked` model makes awkward) or remove the type and its dead helpers and stop
documenting a behavior that does not exist.

### 2f. The duplicate-universe tax lands here
Every graph traversal — ready, blocked, descendants, cycle check — must `UNION`
or loop across `issues`+`wisps` and `dependencies`+`wisp_dependencies` (the typed
`depends_on_issue_id`/`depends_on_wisp_id` split). The engine is where ARCH §6.3
is paid in full, on the hottest path. This is the concrete, per-query cost of the
wisp shadow schema.

---

## Verdict

**The keystones are real strengths — with two asterisks.**

- **Identity:** the random-for-issues / deterministic-for-edges asymmetry is
  principled, and `depid` is the most elegant, hard-won code in the system. This
  is the part of beads that earns its distributed reputation. Asterisks: a dead
  documented ID algorithm, and a "zero conflict" promise that counter mode and
  child IDs quietly break.
- **Graph/readiness:** the semantic model (4 blocking types, parent propagation,
  gates) is clean and the cycle handling is sound. Asterisks: it sits *entirely*
  on the `is_blocked` denormalization (so it inherits §6.2's merge-staleness) and
  pays the §6.3 wisp-UNION tax on every traversal; and `conditional-blocks` is
  inert.

**The throughline to the rest of the analysis:** the strengths are genuine, but
they rest on the two structural weaknesses already identified — the readiness
engine is only as correct as `is_blocked` (§6.2), and every graph query carries
the wisp duplication (§6.3). Fixing those two does not just remove friction; it
hardens the engine that *is* the product. And the two inert/dead features
(`conditional-blocks`, the hex ID generator) are small but telling: features
shipped, then half-abandoned, with documentation left describing the intent
rather than the reality — the same accretion pattern ARCH §6.5 flags, seen up
close.
