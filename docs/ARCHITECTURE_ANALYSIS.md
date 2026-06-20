# Beads — A Critical Architecture Analysis

> A clear-eyed, honest orientation for maintainers: what beads is, how close it
> is to its aim, what is load-bearing, what is decorative, and what is actively
> knocking the project off balance. Written to direct effort, not to flatter.
>
> Scope note: this is a structural/architectural read of the codebase as it
> stands, not a feature wishlist. Line counts and file references are
> approximate snapshots and will drift.
>
> Companion: [EXTENSIBILITY_ANALYSIS.md](EXTENSIBILITY_ANALYSIS.md) drills into
> the plugin/extension question — why orchestration features landed in core,
> what the real demand for extensibility is (with upstream-tracker numbers), and
> which commands could actually leave core.

---

## 1. What beads is trying to do

Beads (`bd`) is a **dependency-aware issue tracker built as persistent,
structured memory for AI coding agents.** The thesis, stated plainly in the
README and charter: agents lose the plot on long-horizon work when their plan
lives in throwaway markdown TODO lists. Beads replaces that with a real graph —
issues, dependencies, readiness — that survives across sessions and across
agents, and that an agent can query programmatically (`--json` everywhere).

Two design bets define the whole system:

1. **The graph is the product.** Issues + typed dependencies + a "what is ready
   to work on right now" computation. Everything else is in service of that, or
   should be.
2. **Dolt is the substrate.** Rather than build storage, versioning, sync, and
   merge, beads delegates all of it to [Dolt](https://github.com/dolthub/dolt),
   a version-controlled SQL database. The pitch: cell-level merge and native
   push/pull give multi-agent, multi-branch collaboration "for free," with
   hash-based IDs (`bd-a1b2`) so concurrent creators never collide.

The charter (`docs/PROJECT_CHARTER.md`) is unusually disciplined about scope and
is the most important governance document in the repo. It draws four fences:
beads owns **issue primitives**; it must **not** encode orchestration policy,
must **not** become a storage engine, must **not** casually grow the schema
(use metadata first), and treats tracker integrations as **adoption bridges,
not a second product**.

**Read the charter as the project's conscience. Most of the friction in this
analysis is the gap between the charter and the code.**

---

## 2. How near, how far

**Near — the core works and is genuinely good.** The irreducible loop —
`create → ready → show → update --claim → close`, with `dep` wiring the graph
and `prime`/`remember` feeding agent context — is coherent, fast, JSON-first,
and clearly battle-tested (test code outweighs production code ~230k:173k
lines). The hash-ID collision-avoidance scheme is a real, correct answer to a
real distributed problem. The Dolt bet pays off for its core promise: offline
work, audit history, and merge without a central server. This is a working,
shipping tool (110 releases, v1.0.x) that dogfoods itself.

**Far — in three specific directions:**

- **Far from "small enough to remain reliable and understandable"** (the
  charter's own stated goal). The command surface is ~109 top-level commands /
  ~150+ entry points across ~294 non-test Go files in `cmd/bd/` alone. The
  `Issue` struct carries 40+ fields. This is not a small tool anymore.
- **Far from the orchestration fence.** A large, first-class feature layer —
  molecules, swarms, gates, wisps, bonding, formula "cooking," work types, mol
  types — lives *in core*, in the schema and the `Issue` type, despite the
  charter explicitly assigning these to the orchestration layer above beads
  (Gastown/Gas City). The fence is drawn; the code is on the wrong side of it.
- **Far from a settled storage story.** The Dolt bet, while strategically
  sound, is the source of nearly all current firefighting (see §6). The most
  recent commit history is dominated by `fix(schema)`, `fix(dolt)`, and
  `fix(sync)` — merge-PK refusals, FK cascade repair, stale denormalized
  caches. The substrate is not yet quiet.

So: **the heart is healthy; the project is overweight around it, and the
plumbing it bet on is still leaking.**

---

## 3. The skeleton — without these, there is nothing

These are the load-bearing structures. Remove any one and beads is not beads.

### Data model (`internal/types/types.go`)
- **`Issue`** — the work item. Its *core* is small: ID, content hash, title,
  description, status, priority, type, assignee, timestamps. Everything past
  that is accretion (see §5/§6).
- **`Dependency`** — typed edges. The four that actually matter to the engine
  are the ones where `AffectsReadyWork()` is true: `blocks`, `parent-child`,
  `conditional-blocks`, `waits-for`. The other ~15 dependency types are graph
  decoration of varying value.
- **`ContentHash`** (`ComputeContentHash`) — deterministic SHA-256 over
  substantive fields. This is the keystone of distributed operation: it is how
  merges decide "same ID + same content = skip, same ID + different content =
  update." Treat it as a stable contract; changing what it hashes is a
  cross-clone breaking change.

### The readiness computation
The single most important *behavior* in the product is "what is ready to work
on" — issues with no open blockers, with blocking propagating through
parent-child edges. This is `GetReadyWork` / `IterReadyWork`, implemented as a
recursive-CTE union over the `issues` (+`wisps`) tables. This query *is* the
value proposition. (Its denormalized cache, `is_blocked`, is a liability — §6.)

### Storage interface (`internal/storage/storage.go`)
- **`Storage`** + the composite **`DoltStorage`** (which stacks 13 capability
  sub-interfaces: VersionControl, HistoryViewer, RemoteStore, SyncStore,
  FederationStore, …). The interface boundary itself is good design — consumers
  depend on the interface, not the concrete Dolt store. Its sheer width,
  however, is a measure of how much surface has accreted.

### The schema (`internal/storage/schema/migrations/`, 50 migrations)
Core tables: `issues`, `dependencies`, `labels`, `comments`, `events`,
`config`, `metadata`. These seven are the spine. Everything queries them.

### The CLI dispatch and DB-open path
`cmd/bd/main.go` (root + global init) and the embedded/server open logic
(`beads_cgo.go` / `beads_nocgo.go` → `internal/storage/dolt/`,
`internal/storage/embeddeddolt/`). Without a working "open the right backend"
path, no command runs.

---

## 4. Hot paths

Where cycles and correctness actually concentrate:

1. **Write path: every mutation auto-commits to Dolt.** `bd create/update/close`
   → SQL write → automatic Dolt commit. This is on the critical path of *every*
   write and is where the embedded-Dolt cost is paid. `internal/storage/dolt/store.go`
   is the busiest file in the repo by churn (≈10 touches in the recent window).
2. **Read path: `bd ready` / `bd list` / `bd show`.** Direct SQL against local
   Dolt. The ready query (recursive CTE + `is_blocked` cache) is the hot read
   and the explicit benchmark target (`BenchmarkGetReadyWork_Large/XLarge`,
   10–20k issue datasets). Performance escape hatches in `IssueFilter`
   (`SkipLabels`, `SkipWisps`, `NoIDShrink`) exist specifically to keep this
   path fast on large "hub" databases — a sign the team has already fought this
   battle.
3. **Sync path: `bd dolt push` / `bd dolt pull`.** Native Dolt sync over
   `refs/dolt/data`, plus working-set merge and `is_blocked` recomputation. This
   is the *correctness* hot path and the current bug epicenter (§6).
4. **Agent context path: `bd prime`** (712 lines) — injects workflow context +
   `remember`ed memories at the start of every agent session. Low compute, high
   leverage: this is the surface the agent reads first, so its output quality
   disproportionately shapes how well agents use the tool.

---

## 5. Supporting roles, minor features, accidents of implementation

Useful or harmless, but not the skeleton. Maintainers should hold these to a
lower standard of investment and a higher bar for further growth.

- **Tracker integrations** (`internal/{linear,ado,gitlab,jira,notion,github}`,
  ~37k lines combined; `cmd/bd/{linear,ado,...}.go`). The charter is explicit:
  these are *adoption bridges, not a product surface*. They are the single
  largest non-core code mass. They earn their keep only as on-ramps; resist any
  pull toward tracker-feature parity, webhook gateways, or credential vaults.
- **`doctor` / `info` / `bootstrap`** (1273 / 1422 / ~977 lines) — diagnostics
  and self-repair. Valuable *because* the storage layer is fragile; their size
  is a symptom of §6, not an independent goal. A quieter storage layer should
  let `doctor` shrink, not grow.
- **Compaction** (`internal/compact`, `bd compact`) — "memory decay" that
  summarizes old closed issues to save agent context. A genuinely on-thesis
  idea (agent memory) but secondary to the graph.
- **Memory commands** (`remember`/`recall`/`forget`/`memories`) — small, cheap,
  high-thesis-alignment. Keep.
- **`hooks.go` / git-hook installation** (1693 lines) — setup ergonomics. Large
  for what it does; an accident of supporting many agent harnesses (Claude,
  Codex, Cursor, Factory, mux…).
- **`init.go`** (2303 lines, the largest file) — does far too much. Repo
  discovery, Dolt bootstrap, agent-file generation, hook install, remote wiring.
  A prime candidate for decomposition; its size makes the most important
  first-run experience the hardest code to reason about.
- **Vestigial / niche**: `mail`, `admin` (hidden), `ship`. Candidates for
  deprecation review.

---

## 6. What runs counter — friction, imbalance, the things to fix

This is the section maintainers should act on. Ordered by how much it
destabilizes the project — but note that **§6.3 (the `wisp_*` duplicate table
universe) is the single most significant concrete finding**, precisely because
it is the visible cash-out of §6.1 and §6.4 colliding. §6.1 and §6.4 are the
root *causes*; §6.3 is where they have already produced shipped data-correctness
bugs and a permanent hot-path tax.

### 6.1 The charter/code split on orchestration (the central tension)
The charter forbids orchestration concepts in core. The code is saturated with
them: `MolType`, `WorkType`, `WispType`, gate fields (`AwaitType`, `AwaitID`,
`Waiters`), bonding (`BondedFrom`/`BondRef`), and formula source-tracing fields
all live on the core `Issue` struct; ~25 `cmd/bd` command files implement
`mol*`, `swarm`, `gate`, `cook`, `pour`, `wisp`. The type system even documents
its own retreat: molecule/gate/event types were *"re-promoted to built-in
because bd commands rely on them."* That sentence is the imbalance in one line —
the fence was crossed, then the schema was changed to legalize the crossing.

**Why it matters:** every one of these fields is in `ComputeContentHash`, in
validation, in the schema, in merge logic. They tax the hot paths and the
storage layer they were supposed to stay out of. **This is the highest-leverage
thing to confront:** either (a) formally amend the charter to admit that beads
*is* the orchestration substrate and stop pretending otherwise, or (b) commit to
extracting molecule/swarm/gate/cook into a plugin or a `gt` companion that rides
on metadata. The current limbo — forbidden in doctrine, load-bearing in code —
is the worst of both.

### 6.2 The `is_blocked` denormalized cache (active bug source)
Readiness is fundamentally a graph computation, but it is cached into an
`issues.is_blocked` column (migration 0046) for query speed. Denormalization +
Dolt's branch/merge model = the cache is routinely stale after a pull or
working-set merge. The recent commit log is a parade of fixes for exactly this:
*"recompute is_blocked for working-set merges," "recompute is_blocked after
dolt pull/merge," "recompute mixed is_blocked."* This is a recurring correctness
hazard, not a one-off. **Decide deliberately:** either own the cache with a
single, well-tested invalidation choke point that *every* merge/pull path must
pass through, or drop the column and recompute on read (the benchmarks exist to
tell you whether that is actually too slow — measure before assuming).

### 6.3 The `wisp_*` duplicate table universe (the sharpest, most concrete imbalance)
This is the clearest single example in the codebase of the storage substrate
leaking into the data model, and it sits at the **intersection of §6.1 and §6.4**
— it is downstream of both. If you read only one subsection, read this one.

**What it is.** `wisps` is a **51-column table that is a near-exact clone of the
51-column `issues` table** (same column names, same types), plus a full shadow
aux family: `wisp_dependencies`, `wisp_labels`, `wisp_comments`, `wisp_events`.
Five core tables, five shadow tables. And there is a **second, parallel migration
stream** — `internal/storage/schema/migrations/ignored/` (9 migrations) shadowing
the tracked stream (50).

**Why it exists (legitimate).** Migration 0019 is the whole story:

```sql
REPLACE INTO dolt_ignore VALUES ('wisps', true);
REPLACE INTO dolt_ignore VALUES ('wisp_%', true);
```

Wisps are excluded from Dolt version control entirely. The motivation is sound
and charter-aligned: ephemeral, high-churn agent infrastructure — heartbeats,
pings, agent/role/rig state, messages (0035 migrated exactly the `agent`, `rig`,
`role`, `message` types into wisps) — must not pollute the synced issue history
or inflate every `dolt push`/`pull`. Keeping that churn out of the versioned
graph is the right instinct.

**Why it became a *duplicate universe* (the forced move).** `dolt_ignore` is
**table-scoped, not row-scoped.** Dolt cannot ignore rows, only tables. The design
wanted a row-level property ("these issues are throwaway") from a mechanism that
only offers table-level ignore — so the only way to make some issues
non-versioned was to put them in a *different table*, and splitting the table
forces cloning the entire relational family that hangs off it. **The duplication
is not gratuitous; it is the mechanical consequence of expressing a row-level
concept through a table-level tool.** That is the leak: a Dolt limitation
propagated into a doubling of the data model.

**What it costs (concrete, not abstract):**

1. **Double-entry schema maintenance.** Every issue field must be added to both
   tables; the streams pair up — `0041↔ignored/0003` (split deps target),
   `0046↔ignored/0006` (`is_blocked`), `0047↔ignored/0007` (recompute),
   `0049↔ignored/0008` (widen LONGTEXT). Two migration streams in lockstep.
2. **It has already caused a silent correctness bug.** Upstream #4138: the
   `is_blocked` backfill ran on `issues` but the wisp companion added the column
   *without* backfilling — so `bd ready` returned blocked wisp work as ready (766
   mis-flagged wisps in a real store). The duplication tax cashing out as data
   corruption, compounding the §6.2 fragility by cloning it.
3. **It complicated the *core* dependencies table.** Dolt forbids FKs from a
   tracked table (`dependencies`) to an ignored table (`wisps`). To let
   dependencies span both worlds, the target was split into three typed columns
   (`depends_on_issue_id`/`depends_on_wisp_id`/`depends_on_external`) under a
   CHECK constraint, plus hand-written helpers (`DeleteWispFromDependenciesInTx`,
   `UpdateWispIDInDependenciesInTx`) that manually maintain integrity FKs would
   give for free. The shadow schema reached back and complicated the primary.
4. **Every cross-cutting read pays a UNION.** ~10 files implement
   `SearchAcrossIssuesAndWisps`/union logic (`ready_work_union.go`,
   `issue_search.go`, `blocked.go`, `events.go`, `issue_descendants.go`, …);
   **126 non-test files reference wisps.** The `SkipWisps` escape hatch (9 sites)
   exists because the UNION is costly enough that hot paths opt out — a permanent
   tax on the hot read path, the path that *is* the product.
5. **The clone is already drifting.** `wisps` still carries orchestration columns
   (`hook_bead`, `role_bead`, `agent_state`, `rig`, `role_type`) that were
   *dropped from `issues`* (0038 `drop_hop_columns`). It is no longer a faithful
   mirror — it is a diverging shadow accumulating its own legacy cruft.

**Aligned vs. off-balance.** The *goal* (keep ephemeral agent churn out of the
versioned, synced graph) is correct. The *mechanism* (a full shadow schema +
shadow migration stream) is accidental complexity forced by serving a row-level
need with a table-level tool. And note *why* there is ephemeral agent churn to
isolate at all: heartbeats, agents, roles, rigs, messages — the orchestration
concepts §6.1 says do not belong in core. **The duplicate universe is downstream
of both big imbalances:** orchestration-in-core created the need, the Dolt
table-ignore limitation dictated the shape. Fix either upstream cause and this
largely dissolves.

**Ways out (ranked):**

1. **Best leverage — collapse the aux shadow tables, keep only `wisps`.**
   Ephemeral data rarely needs relational queryability or merge. Store wisp
   labels/deps/comments/events as JSON columns *on the wisp row* instead of four
   shadow tables. Kills 4 of 5 shadow tables and most of the `ignored/` stream,
   removes the wisp-side typed-target FK gymnastics, and preserves the legitimate
   `dolt_ignore` benefit. High payoff, contained blast radius.
2. **Structural but expensive — single table + `ephemeral` flag + partial index +
   TTL/GC.** Conceptually cleanest (one data model) but fights Dolt head-on:
   row-level ignore does not exist, so ephemeral rows re-enter Dolt history and
   sync between GC runs. Trades duplication for history-churn — probably not worth
   it given the original motivation.
3. **Cleanest separation — a distinct ephemeral store** (local SQLite, or a
   separate never-pushed Dolt DB). Removes the leak entirely but adds a second
   storage path to own.
4. **Cheapest interim — generate both migration streams from one source.** If the
   duplication stays, at least make the tracked/ignored pairing mechanical (one
   schema definition, emit both) so you cannot ship another #4138. Caps the
   recurring correctness risk without reducing the model.

**The root lesson to record:** do not model a row-level lifecycle property as a
separate table just because the storage engine's ignore is table-scoped. Push
ephemerality up into the orchestration layer or down into a purpose-built store —
not sideways into a schema clone.

### 6.4 The Dolt substrate is heavy, and the storage boundary is leaking
Embedded mode requires CGO and links the entire go-mysql-server + Dolt engine
into the binary, dragging ICU concerns that forced a project-wide
`-tags=gms_pure_go` policy (`.buildflags`, `docs/ICU-POLICY.md`). Non-CGO builds
can only do server mode.

The deeper problem is not weight but doctrine. The charter says beads must
*"not become a storage engine"* and must avoid *"beads-side flocks, engine
introspection, storage-specific retry loops, crash-recovery workarounds, or
schema poking that belongs in Dolt or the Dolt driver."* The merge layer in
`internal/storage/dolt/store.go` is now exactly that, in concrete form:

- **Three hand-maintained "safe-class" conflict resolvers.** Post-pull,
  `tryAutoResolveMergeConflicts()` inspects `dolt_conflicts` and auto-resolves
  three specific table conflicts it knows are benign — `metadata` (machine-local
  rows), `dependencies` (audit-column-only diffs, made tractable only by the
  deterministic-ID migration 0050), and `schema_migrations` (mixed binary
  vintages with NULL vs sha256 content hashes). Each is bespoke knowledge of
  Dolt's row-merge behavior encoded in beads.
- **An FK-cascade-repair whitelist.** `tryRepairFKCascadeViolations()` hand-
  applies the `CASCADE` semantics Dolt's merge never re-runs (delete-vs-insert
  across clones), against a *manually maintained list of child tables.* Add a
  new child table and forget to update that list, and merges silently fail to
  commit on unrepaired violations.
- **Six-way push/pull routing** (CLI subprocess vs SQL `DOLT_PULL`) chosen by
  remote protocol, credentials, and cloud auth — each path with its own env-var
  and timeout handling. A wrong route loses credentials or times out mid-
  transfer.

Recent commit subjects name the bleeding directly: *"Dolt ancestor-PK merge
refusals," "per-clone-random history-table PKs," "cascade-repair FK constraint
violations on pull merges."* **The Dolt bet is probably right, but beads is
paying for it by absorbing storage-engine logic the charter explicitly assigns
to Dolt/the driver.** Drive these fixes upstream; treat the volume of
`internal/storage/dolt` merge/repair code as a health metric — if it keeps
rising, the storage boundary has failed in practice and the charter is fiction.
Note the same dynamic powers `doctor` (1273 lines of self-repair): it is large
*because* this layer is fragile, not for its own sake.

### 6.5 Feature accretion velocity vs. the "stay small" goal
69 commits in 90 days, 110 releases, a 325KB CHANGELOG, 50 migrations, a
40+-field `Issue`. The project is shipping fast — a strength — but almost
entirely in the direction the charter warns against (schema growth,
orchestration features, integration breadth). There is a `PROJECT_CHARTER.md`
and an `AGENTS.md` "read the charter before adding surface area" gate, which is
exactly the right instinct; the evidence suggests the gate is not actually
holding. **Recommendation:** make scope-fence violations a blocking review
criterion with teeth (a checklist item that asks "does this add a core `Issue`
field or schema table? justify against §Schema Boundary"), and periodically
*remove* surface, not just add it.

### 6.6 `init.go` and the first-run blast radius
The 2303-line `init.go` concentrates the riskiest, most-modified, hardest-to-
reason-about logic at the exact moment a new user forms their first impression.
Its size and 5 recent touches make it a reliability liability out of proportion
to its conceptual difficulty. Decompose it.

---

## 7. A maintainer's directive (what to do with all this)

**Protect and invest:**
- The core graph loop and the readiness query. This is the product. Keep it
  fast, keep it correct, keep it boring.
- `ComputeContentHash` and the hash-ID scheme — the distributed-correctness
  keystone. Change with extreme care and cross-clone migration plans.
- The charter itself. It is the best tool you have; the problem is enforcement,
  not the document.

**Decide deliberately (these are forks in the road, not bugs):**
- Orchestration: amend the charter to embrace it, or extract it. Pick one. (§6.1)
- `is_blocked`: single invalidation choke point, or recompute-on-read. (§6.2)
- **`wisps` (highest-impact concrete refactor): collapse the four aux shadow
  tables to JSON-on-wisp, keeping only the `wisps` table — preserves the
  legitimate `dolt_ignore` benefit while removing most of the duplicate universe,
  the shadow migration stream, and the wisp-side FK gymnastics. If you ship one
  structural fix from this document, ship this. (§6.3)** At minimum, generate the
  tracked/ignored migration pair from one source so no future schema change can
  repeat the #4138 backfill bug.

**Push outward / push back:**
- Drive Dolt merge/FK/PK fixes upstream rather than accreting beads-side repair
  (§6.4). Watch the workaround-code ratio as a health metric.
- Hold integrations and orchestration features to the charter's "bridge, not
  product" / "metadata before schema" bars (§5, §6.5).

**Refactor for reliability:**
- **Collapse the `wisp_*` aux shadow tables to JSON-on-wisp (§6.3) — the
  highest-impact concrete refactor in this document.** It is where §6.1 and §6.4
  have already produced a shipped correctness bug and a permanent hot-path tax.
- Decompose `init.go` (§6.6). Let a quieter storage layer shrink `doctor`
  rather than grow it (§5).

**The one-sentence version:** beads has an excellent core idea, a correct
distributed-ID design, and a disciplined charter — and it is being pulled off
balance by orchestration features it swore to keep out and a storage substrate
whose merge semantics it is still fighting, a collision whose sharpest
cash-out is the `wisp_*` duplicate table universe (a full schema clone, a shadow
migration stream, and a shipped `bd ready` correctness bug) that taxes every
query. Defend the core; collapse the wisp shadow tables; resolve the three
forks; enforce the fence you already wrote.
