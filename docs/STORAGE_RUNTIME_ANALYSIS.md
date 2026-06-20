# Beads — Storage Runtime Analysis (concurrency, durability, and the merge-repair burden)

> Answers the question every other document in this set raises but none closes:
> **why is storage the bug epicenter?** EXTENSIBILITY §5 found that storage /
> sync / dolt / merge is ~38 % of upstream demand and the *most severe* bucket;
> ARCHITECTURE §6.2/§6.4 named the two mechanisms (the `is_blocked` cache, the
> leaking Dolt boundary) but left them partly traced. This document finishes
> both — and opens the dark subsystem behind them (#11: the daemon / server /
> proxy / circuit-breaker runtime). Companion to
> [ARCHITECTURE_ANALYSIS.md](ARCHITECTURE_ANALYSIS.md),
> [CORE_ENGINE_ANALYSIS.md](CORE_ENGINE_ANALYSIS.md), and
> [AGENT_MEMORY_ANALYSIS.md](AGENT_MEMORY_ANALYSIS.md); tracked in
> [ANALYSIS_INDEX.md](ANALYSIS_INDEX.md) (#6.2 / #6.4 / #11).
>
> The structural thesis, stated up front: **the storage layer is large and
> busy not because storage is the product, but because the correctness of the
> Dolt bet *under concurrency and under merge* is being paid down inside beads,
> by hand.** Line counts and refs are point-in-time and will drift.

---

## 1. The concurrency model — what actually serializes a write

The README pitch is "multi-agent, multi-branch collaboration for free." That is
true for one kind of concurrency and not another, and the distinction is the
whole story.

### 1a. Two collaboration modes, only one of them free

- **Async / offline (genuinely cheap, and the good part).** Two clones each
  write locally, commit to Dolt history, and reconcile later via
  `bd dolt push`/`pull`. No shared server, no live lock. This is the Dolt bet
  paying off — and §4 is the bill for it.
- **Concurrent / live (not free).** Two writers mutating *the same database at
  the same time* need a running Dolt **server**. There is no lock-free
  multi-writer embedded path; choosing concurrency means taking on the entire
  server runtime of §2.

### 1b. The mode matrix (two axes, stated with conflicting defaults)

Beads has **two independent "mode" concepts at two layers**, which is itself a
source of confusion (and feeds the config three-way, #14):

**Axis A — connection mode** (`internal/configfile`, persisted as `dolt_mode`):
`embedded` / `server` / `proxied-server` (`configfile.go:214-216`), plus
shared-server via env. This decides *how a `bd` process reaches the data.*

**Axis B — server lifecycle** (`internal/doltserver/servermode.go`):
`ResolveServerMode` → `Owned` (beads auto-starts/stops the sql-server) /
`External` (user manages it) / `Embedded`. This decides *who owns the server
process.*

The two axes disagree on what the default even is:

- `GetDoltMode()` returns `embedded` for an unset config
  (`configfile.go:269-274`) — **but its own doc-comment says "defaulting to
  server."** The comment contradicts the code. (A small, exact instance of the
  docs-describe-what-the-code-doesn't pattern the other analyses keep finding.)
- `ResolveServerMode` calls `Owned` "the default for standalone users"
  (`servermode.go:15-17`).
- In practice a flagless `bd init` lands in **embedded**: server mode is
  selected only by `--server`, `BEADS_DOLT_SERVER_MODE=1`, `--shared-server`,
  or `config.yaml dolt.mode: server` (`init.go:248-320`); `--proxied-server`
  selects the proxy. So the default standalone experience is the single-writer
  one (next).

### 1c. Embedded = exactly one writer, enforced by a file lock

In embedded mode the serialization mechanism is a non-blocking exclusive
**flock** on `.beads/embeddeddolt/.lock` (`acquireEmbeddedLock`,
`store_factory.go:64-79`). A second writer does not queue — it is rejected:

> *"the embedded backend supports only one writer at a time — use the dolt
> server backend for concurrent access"* (`store_factory.go:72-74`).

In server mode the same function returns a `NoopLock` (`store_factory.go:65-67`):
beads does no write serialization at all and delegates entirely to the Dolt
server (MVCC + a global commit-graph lock that serializes `DOLT_COMMIT`; see
`docs/design/dolt-concurrency.md`). The server path also has to *pin a single
connection* across the SQL transaction + `DOLT_COMMIT`, because each pool
connection has an independent working set in Dolt server mode (GH#2455) — a
non-obvious footgun that, if missed, commits the wrong working set.

### 1d. `proxied-server` — the "multi-writer without server ops" answer (real, recent, with refactor residue)

There is a third connection mode that the casual reader misses: a long-lived,
per-root-directory TCP **proxy** (`db-proxy-child`, a hidden fork+exec'd
command, `cmd/bd/db_proxy_child.go`) that fronts a `DatabaseServer` so multiple
local `bd` invocations share one engine without the user running a server. It
is **not a stub**: `internal/storage/dbproxy/` is ~1.75 k production LOC + ~2.1 k
test LOC, wired through `newProxiedServerUOWProvider` (`cmd/bd/uow_factory.go`)
and `init_proxied_server.go`.

It does, however, wear the project's signature tell. The *older* store-based
construction path was superseded by the UoW-provider path and left behind as
dead, commented-out code with TODOs:

```go
if cfg.ProxiedServer {
    // TODO: this should not be a store
    // it should be a uow provider
    return nil, fmt.Errorf("proxy server store should be uow provider")
}
```
(`store_factory.go:49-53`, repeated at `:89-97`). Two construction paths, one
live, one commented — the migration was never garbage-collected.

### 1e. The "exclusive lock" is cooperative and explicitly *not* a lock

`docs/EXCLUSIVE_LOCK.md` documents a `.beads/.exclusive-lock` JSON file that
external tools (CI, VibeCoder) write to claim a database. It is easy to mistake
for write serialization. It is not. The doc is admirably blunt
(`EXCLUSIVE_LOCK.md:142-151`): it does **not** provide mutual exclusion between
tools, transaction isolation, or ACID guarantees. It is a *cooperative signal
to the sync server to skip a database*, validated by PID + hostname liveness,
fail-safe on a malformed file. Useful for its stated job; a hazard if anyone
reads "lock" and assumes the guarantees the word implies.

*(Provenance aside: the doc footer files issues at `gastownhall/beads` while the
code imports `github.com/steveyegge/beads` — the repo has changed hands at least
twice. Harmless, but it surfaces in user-facing docs.)*

---

## 2. The client-side resilience layer (the size tell of the owned-server default)

Because the non-embedded default is **Owned** — beads launches and babysits a
Dolt sql-server — a large, mostly-invisible body of code exists purely to
survive that server's failure modes. None of it is issue-tracking; all of it is
operating a database engine, which the charter says beads must not do.

- **Circuit breaker** (`internal/storage/dolt/circuit.go`). A file-based,
  cross-process breaker (state in `/tmp`, shared between `bd` processes, plus an
  in-process mutex) that fails fast when the server is unreachable instead of
  letting every command hang. Per-database granularity so one degraded project
  doesn't trip the breaker for every worktree on a shared server (GH#3140).
  Thresholds: **5** consecutive failures within a **60 s** window trips it; a
  **5 s** cooldown before a half-open TCP probe; a **5 min** stale-TTL so an old
  breaker file can't poison a fresh init (`circuit.go:22-41`).
- **Manifest recovery** (`internal/doltserver/manifest_recovery.go`). After an
  unclean shutdown Dolt's manifest can reference a root hash that was never
  flushed — the server then refuses to start with *"root hash doesn't exist"*
  (`manifest_recovery.go:13-17`, GH#3290). Beads detects this signature in the
  server log and **auto-reinitializes the store — but only when it can prove no
  data is lost** (empty journal, no data chunks); a journal with data is punted
  to a manual `dolt fsck` runbook rather than risked. Careful, correct, and a
  textbook example of beads absorbing storage-engine crash recovery.
- **Plus**: stale-PID / orphan-server cleanup, ephemeral-port allocation with
  retry and explicit-port reclamation, shadow-database prevention (refuse to
  auto-start a *different* server when one is externally managed), and a
  one-time hyphenated-database-name migration (GH#2142). Each is a reasonable
  fix; collectively they are an operations layer for a database the project
  declared it would not operate.

**Read this layer as a health metric.** It is the running cost of choosing
"beads owns the server" as the concurrency answer. It is also why `doctor`
(1273 lines) is large: a fragile runtime needs a big self-repair surface
(ARCH §5/§6.4).

---

## 3. Closing §6.2 — the `is_blocked` cache has no choke point, and a second harder one for merges

ARCHITECTURE §6.2 flagged `is_blocked` as an active bug source but left the
invalidation structure untraced. Traced, it is worse than "a stale cache": it is
a cache with **no single invalidation point**, maintained by every write path
independently, plus an entirely separate mechanism for merges.

### 3a. 24 hand-placed recompute sites, each re-deriving its own blast radius

The recompute entrypoints (`RecomputeIsBlockedInTx`,
`internal/storage/issueops/blocked_state.go:49`, and the merge variant in §3b)
are invoked from **19 non-test call sites across 13 files** — `create`, `close`,
`update`, `delete` (×2), `promote`, `bulk_ops`, `dependencies` (×2), `wisps`
(×3), `ephemeral_routing`, and the two merge paths — i.e. essentially every
distinct mutation path maintains the cache by hand. There is no "after any
write, reconcile readiness" hook; each mutation must:

1. *recognize* that it could change readiness, and
2. compute the **transitive affected set** itself —
   `AffectedByStatusChangeInTx` / `AffectedByDepChangeInTx` /
   `AffectedByDeletionInTx` (`blocked_state.go:366-550`) walk blockers, waiters,
   and parent-child descendants — then
3. run a **fixpoint mark/unmark loop until convergence** (`blocked_state.go:53-72`),
   because a parent's `is_blocked` is read from its *cached* value, so
   propagation needs iteration to settle.

Miss step 1 or under-compute step 2 at *any one* of those sites and a row's
`is_blocked` goes silently stale — and `bd ready`, which is just
`WHERE is_blocked = 0` (CORE_ENGINE §2a), trusts it. The denormalization buys an
O(1) read and pays with a 24-way invalidation obligation that no type or test
can enforce centrally.

### 3b. Merges bypass all 24 — so there's a second, diff-driven recompute

The sharpest part is documented in the code's own words
(`blocked_merge.go:33-37`):

> *"is_blocked is maintained only by the local write paths … so a merge that
> brings in another clone's writes bypasses every recompute hook: clone A closes
> blocker X while clone B adds an edge W→X, and the merged result silently
> carries W.is_blocked=1 with a closed blocker — `bd ready` then trusts the
> stale column. This is the missing merge-side hook."*

The fix is a *parallel, harder* invalidation path,
`RecomputeIsBlockedAfterMergeInTx` (`blocked_merge.go:52`): it `dolt_diff`s the
pre-pull commit against `WORKING`, seeds the *same* affected-set expansion from
the changed issues/edges, and recomputes only that closure — falling back to a
whole-graph pass when the diff fails (a merge that also reshaped the schema) or
when more than **1000** rows changed (`mergeRecomputeSeedCap`,
`blocked_merge.go:22`), because the per-row BFS is unbounded while the full pass
is bounded by table size. It even has to paper over engine quirks: `DOLT_HASHOF`
with a `HASHOF` fallback for embedded/older engines (`blocked_merge.go:62-66`),
and a `WORKING`-vs-`HEAD` subtlety where an auto-resolved merge lands in the
working set *without advancing HEAD*, so "HEAD didn't move" does not mean
"nothing merged" (`blocked_merge.go:68-80`).

That last subtlety is not hypothetical — it is the most recent fix on this
column. The timeline of one denormalization:

- migration **0046** adds `is_blocked`; migration **0047** immediately has to
  recompute it ("mixed" backfill);
- **bd-6dnrw.3** adds the runtime merge-side hook ("recompute is_blocked after
  dolt pull/merge");
- **bd-6dnrw.39** fixes that hook for working-set merges where HEAD never
  advanced.

A steady drip of corrections to a single derived column — exactly the
"recurring correctness hazard, not a one-off" §6.2 asserted, now with the
mechanism named.

### 3c. The recompute-on-read alternative — measurable, not obvious

§6.2 offered "drop the column, recompute on read" as the other fork. Closing the
question honestly: it is **not** a flat `WHERE`. Readiness includes parent-child
propagation, which is a fixpoint (a child is blocked if its parent is), so
on-read recomputation means a **recursive CTE**, not a stateless predicate. The
denormalization's whole justification is that the recursive form was too slow at
scale — `blocked_merge.go:43` records that "a full-graph pass timed out on large
databases when migration 0047 tried it." So the real tradeoff is:

- **keep the column:** O(1) reads, perpetual 24-site + merge-path invalidation
  fragility (a shipped-bug stream);
- **recompute on read:** zero staleness by construction, a recursive CTE on the
  hot path whose cost is *exactly what `BenchmarkGetReadyWork_Large/XLarge`
  already measures.*

The benchmarks exist to price this. The recommendation is unchanged but now
sharper: **measure the recursive read against the 10–20 k datasets before
assuming the cache is necessary** — the cache's maintenance cost is no longer
hypothetical, it's in the commit log.

---

## 4. Closing §6.4 — the merge-repair burden is real, hand-maintained, and accreting

§6.4 asserted that beads is absorbing storage-engine merge logic and that the
trend should be watched. Both are now verified.

### 4a. Three hand-written "benign conflict" resolvers

Post-pull, `tryAutoResolveMergeConflicts` (`dolt/store.go`) inspects
`dolt_conflicts` and auto-resolves three table-conflict classes it has decided
are safe, each backed by a bespoke verifier and each encoding intimate knowledge
of Dolt's row-merge behavior:

| Table | Resolved how | Dolt shortcoming patched | Verifier |
|---|---|---|---|
| `metadata` | `--theirs` | per-clone-local rows (`dolt_auto_push_*`) diverge with no semantic conflict (GH#2466) | — |
| `dependencies` | `--theirs` when **audit-columns-only** differ | deterministic edge IDs (#4259 / migration 0050) make a same-PK conflict provably the same edge | `dependencyConflictsAreAuditOnly()` |
| `schema_migrations` | keep the row that recorded a hash | mixed binary vintages record `(version, NULL)` vs `(version, sha256)` for the same migration (bd-6dnrw.29 / #4270) | `schemaMigrationsConflictsAreVintageOnly()` |

Each verifier exists to make sure beads only auto-resolves a conflict it can
*prove* is cosmetic; two genuinely different non-empty hashes, for instance, are
left for the operator. The discipline is good. The fact that it has to live in
beads at all is the leak.

### 4b. The FK-cascade repair and its hand-maintained whitelist

Dolt merges row-wise and **never re-executes cascades**
(`fk_violation_repair.go:11-22`), so "clone A deletes issue X" merged with
"clone B inserts a child of X" yields a dangling FK row that rolls the whole
merge back and never converges on retry. `tryRepairFKCascadeViolations` fixes
this by *manually applying the cascade* — deleting the dangling rows — against a
hand-maintained map of child tables, `fkCascadeRepairDeletes`
(`fk_violation_repair.go:23-33`):

```
dependencies, labels, comments, events,
issue_snapshots, compaction_snapshots, child_counters
```

Two failure modes:

1. **The whitelist is a tripwire.** A violating table not in the map aborts the
   repair (`fk_violation_repair.go:60-63`) and the merge fails as
   "constraint violations bd cannot auto-repair." Add a new issue-child table and
   forget this map, and you ship an unrecoverable-merge bug. The cascade lives in
   migrations 0041/0042; this Go map must be kept in lockstep with the schema by
   hand.
2. **Two of the seven entries are dead tables.** `issue_snapshots` and
   `compaction_snapshots` are the archive tables AGENT_MEMORY §2c proved are
   **never written**. The repair DELETEs against them are harmless (they match
   nothing), but they are phantom maintenance: a maintainer extending this list
   must reason about — and a future schema change must carry — cascade repair for
   tables that hold no rows. The dead feature still taxes the merge layer. (Wire
   the snapshots, per AGENT_MEMORY §2c, or drop them from the list; today they
   are neither.)

### 4c. The trend is *accreting*, not draining upstream

The recent commit window is a run of beads-side patches — `bd-6dnrw.1`
(ancestor-PK merge refusals), `.2` (rekey per-clone-random history PKs), `.3`
(post-merge is_blocked), `.4` (FK cascade repair), `.29` (schema_migrations
vintage), `.39` (working-set is_blocked) — and crucially **none are annotated as
provisional**: no `TODO: remove once dolt#NNNN lands`, no link to a dolthub/dolt
issue in the resolver code, and a recent Dolt version bump was *not* paired with
deleting a workaround. The repair code is written and tested as permanent
infrastructure. That is the precise signal §6.4 said to watch for, and it is
pointing the wrong way: **the storage boundary is failing in practice, and the
charter's "do not become a storage engine" is, for the merge layer, currently
fiction.**

---

## Verdict

The Dolt bet is strategically sound and its *async* collaboration story is
genuinely good. But the bet's cost is concentrated, structural, and currently
growing, and it explains the demand data the other documents only counted:

- **Concurrency is mode-shaped and the free lunch is async-only.** The default
  standalone path is single-writer (embedded flock). Live concurrent writers
  require a server, which drags in a whole resilience layer (circuit breaker,
  manifest recovery, port/PID/shadow-DB management) that is database-operations
  code the charter forbids. The proxied-server mode is the real
  "multi-writer-without-ops" answer — substantial, recent, and carrying its own
  half-migrated residue.
- **`is_blocked` is the single most fragile invariant in the system.** No choke
  point (24 sites), a separate and repeatedly-patched merge path, and a derived
  value that `bd ready` trusts unconditionally. It is the seam where the §6.3
  wisp duplication, the §6.4 merge gaps, and the readiness engine all meet.
- **The merge-repair layer is the leak made concrete.** Three hand-written
  conflict resolvers, a hand-maintained FK-cascade whitelist (two of whose
  entries are dead tables), and a commit trend that is accreting beads-side with
  no upstream offload.

**Ranked remediation (highest leverage first):**

1. **Resolve the `is_blocked` fork — and measure it.** Either (a) funnel *every*
   mutation and merge path through one invalidation choke point (so a new write
   path cannot forget), or (b) drop the column and recompute on read via the
   recursive CTE, *pricing it first with the existing
   `BenchmarkGetReadyWork_Large/XLarge`.* The status quo — 24 sites plus a
   distinct, brittle merge path — is the most expensive option on the board.
2. **Generate the FK-cascade whitelist and the cascade migrations from one
   schema source**, so a new child table cannot be forgotten; and reconcile the
   two dead snapshot tables (wire or remove). This caps the same class of
   ship-an-unrecoverable-merge bug as the #4138 wisp-backfill (ARCH §6.3).
3. **Track merge/repair LOC as a published health metric, and make the
   workarounds provisional.** Open the upstream dolthub/dolt issues, annotate
   each resolver with the issue it patches and the condition for its removal.
   Permanent-looking workaround code is how a "temporary" boundary leak becomes
   the architecture.
4. **Document the mode matrix honestly.** Fix the `GetDoltMode` comment/code
   contradiction, write down the two-axis (connection × lifecycle) model, and
   decide whether proxied-server is *the* blessed concurrency answer — then
   finish or delete its dual construction path.

**The throughline.** Every other document found genuine strength resting on the
same two structural weaknesses; this one shows where those weaknesses *come
from*. The graph engine is only as correct as `is_blocked` (§3), and the
distributed promise is only as durable as a merge layer beads is hand-patching
(§4) on a runtime it half-operates (§1–2). Storage is the bug epicenter not by
accident but by architecture: beads bet its correctness on Dolt, and the
unpriced part of that bet — concurrency, invalidation, and merge — is being
paid down one `fix(dolt)` at a time. Defend the core by making this layer
*boring*: one invalidation path, one schema source of truth, and an explicit
plan to push the merge fixes back where the charter says they belong.
