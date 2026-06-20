# Beads Analysis — Exploration Index

> Living tracker for the architecture/strengths analysis. Maps what has been
> unfolded (traced to code, with ramifications and remediation) versus what is
> still only named or asserted. Companion to
> [ARCHITECTURE_ANALYSIS.md](ARCHITECTURE_ANALYSIS.md),
> [CORE_ENGINE_ANALYSIS.md](CORE_ENGINE_ANALYSIS.md),
> [AGENT_MEMORY_ANALYSIS.md](AGENT_MEMORY_ANALYSIS.md),
> [EXTENSIBILITY_ANALYSIS.md](EXTENSIBILITY_ANALYSIS.md), and
> [STORAGE_RUNTIME_ANALYSIS.md](STORAGE_RUNTIME_ANALYSIS.md).

**Legend:** ✅ unfolded · 🟡 partial (mechanism seen, ramifications open) ·
⬜ named/asserted only, not opened

**Honest meta-note (updated):** the early fully-unfolded threads were both
*weaknesses*; the brief asked for a guide that centers strengths. That gap is now
closed on the two load-bearing strengths — the hash-ID/graph keystone
(CORE_ENGINE) and the agent-memory layer (AGENT_MEMORY). Verdict on both: the
strengths are *genuine*, but each carries the same ship-then-half-abandon tell
(inert `conditional-blocks`, dead hex ID generator, unwired compaction archive
tables, stubbed-yet-advertised Tier 2). The headline "memory for agents" promise
is real but narrower than sold (flat KV, no ranking/semantic retrieval, no undo).

**Update 2 (storage runtime):** the two partial imbalances are now closed
(STORAGE_RUNTIME_ANALYSIS) and the dark storage-runtime subsystem (#11) opened
alongside them — they are one story: *why storage is the bug epicenter.* §6.2
verified — `is_blocked` has **no invalidation choke point** (24 hand-placed
recompute sites) plus a separate, repeatedly-patched merge path; recompute-on-read
is a recursive CTE, measurable with the existing benchmarks. §6.4 verified — three
hand-written conflict resolvers + a hand-maintained FK-cascade whitelist (two of
whose seven entries are the *dead* snapshot tables from AGENT_MEMORY §2c), trend
**accreting** (no upstream offload, no provisional annotations). #11: the default
standalone mode is single-writer (embedded flock); concurrency requires a server
mode and a whole resilience layer (circuit breaker, manifest recovery,
port/PID/shadow-DB management) — database-operations code the charter forbids.

**Update 3 (course-correction — read this before the next pull):** the maintainer
observed that sessions 2-3 produced *correct, exhaustive* findings but fewer "now
I understand" moments than session 1 — coverage crowded out insight. Diagnosis:
this index, created at the same commit that began the "verify the strengths" work
(`51b1a27`), silently redefined the goal from *understand beads* to *turn ⬜ into
✅*. Coverage-thinking rewards breadth and closure and punishes the slow circling
that produces a reframe. Correction adopted: pick a **question that would change
how you'd act**, organize around **one generative tension**, **hunt the
inversion** (where the obvious reading is wrong), and deliver a **transferable
principle** — not an inventory. First product of the new mode:
[IS_BEADS_BECOMING_GASTOWN.md](IS_BEADS_BECOMING_GASTOWN.md) (the authorship
tension: the charter is a self-imposed boundary its own author structurally
cannot enforce; the wisp universe is that denial made physical). **Do not let
this index pick the next pull by "what's still ⬜." Let the next question pick
it.** Second product of the new mode:
[IS_THE_GRAPH_THE_PRODUCT.md](IS_THE_GRAPH_THE_PRODUCT.md) — verdict: the product
is a *verb* (readiness dispatch over the dependency graph), not a noun; "memory"
is both the metaphor for the graph and the name of an unrelated 310-line KV
feature. It **interlocks** with the Gastown essay rather than sitting beside it:
the core verb is *dispatch*, dispatch is the first half of orchestration, so the
readiness engine is the structural gravity well that pulls Gastown into core —
the boundary was undefendable by construction, not merely undefended. Two essays,
one finding seen from two sides.

---

## A. Skeleton / strengths — the actual product

| # | Thread | Status | Documented in | Notes |
|---|--------|--------|---------------|-------|
| 3 | **Content-hash + hash-ID scheme** (distributed-correctness keystone) | ✅ | CORE_ENGINE §1 | Verified: 3 mechanisms / 2 philosophies (entropy-for-issues, deterministic-for-edges). Found dead `types.GenerateHashID`; "zero conflict" broken by counter mode + child IDs |
| 4 | **Readiness engine** (`GetReadyWork`/union, sort policies, parent-child propagation, gates, cycles) | ✅ | CORE_ENGINE §2 | Clean model; entirely `is_blocked`-driven (inherits §6.2); pays §6.3 wisp tax per query |
| 5 | **Dependency graph engine** (19 dep types, `AffectsReadyWork`/`IsBlockingEdge`, cycle detection) | ✅ | CORE_ENGINE §2 | Sound cycle detection. **Confirmed gap: `conditional-blocks` is inert** (`IsFailureClose` is dead code) |
| — | Storage interface / `DoltStorage` 13-sub-interface composition | ⬜ | — | Listed, not assessed for clean vs leaky decomposition |

## B. Imbalances — the things to fix

| # | Thread | Status | Documented in | Notes |
|---|--------|--------|---------------|-------|
| 6.1 | Orchestration-in-core (charter/code split) | ✅ | ARCH §6.1, EXT | Plus full plugin/extensibility thread |
| 6.3 | Wisp duplicate-table universe | ✅ | ARCH §6.3 | Fleshed as the central concrete finding |
| 6.2 | `is_blocked` denormalized cache | ✅ | ARCH §6.2, STORAGE §3 | **No choke point: 19 recompute call sites across 13 files + a separate diff-driven merge path** (`blocked_merge.go`), patched at 0046→0047→bd-6dnrw.3→.39. Recompute-on-read = recursive CTE, priceable via `BenchmarkGetReadyWork_*`. Code's own comment names the defect |
| 6.4 | Dolt boundary leak (merge resolvers, FK whitelist) | ✅ | ARCH §6.4, STORAGE §4 | Verified: 3 hand-written "benign conflict" resolvers + a 7-table FK-cascade whitelist (**2 entries are dead snapshot tables**). Trend **accreting** — no `remove once dolt#N` anchors, version bump not paired with workaround removal |
| 6.5 | Feature-accretion velocity vs "stay small" | 🟡 | ARCH §6.5 | Asserted from commit/changelog metrics |
| 6.6 | `init.go` first-run blast radius (2303 lines) | ⬜ | ARCH §6.6 | Asserted from size/churn; internals not read |

## A2. Headline thesis — "memory for agents" (the pitch, now verified)

| # | Thread | Status | Documented in | Notes |
|---|--------|--------|---------------|-------|
| 10 | **Agent-context / KV-memory layer** (`prime`, `remember`/`recall`/`forget`/`memories`) | ✅ | AGENT_MEMORY §1 | Real durable memory (`kv.memory.*` rows in `config`, auto-injected at prime). Asterisks: flat (no scoping/ranking/semantic/FTS), 64 KB ceiling, MCP exposes no memory tool, `prime` injects all memories unranked and **computes no ready-work** (only static text) |
| 9 | **Compaction / "memory decay"** | ✅ | AGENT_MEMORY §2 | Real LLM (Anthropic Haiku) summarization + agent-native `--analyze`/`--apply`. **Confirmed: archive tables (`issue_snapshots`/`compaction_snapshots`, migrations 0009/0010) are dead — never written**; Tier 2 stubbed yet advertised; destructive in-place, `restore` is display-only → effectively irreversible; no automation; `bd compact` vs `bd admin compact` name collision |

## C. Subsystems still dark

| # | Thread | Status | Notes |
|---|--------|--------|-------|
| 11 | Daemon / server mode / dbproxy / circuit breaker | ✅ | STORAGE §1-2: four-mode matrix (embedded single-writer flock / owned / external / proxied-server), client-side resilience layer (circuit breaker, manifest recovery, port/PID/shadow-DB mgmt). Cooperative non-ACID exclusive lock. Daemon *internals* (logrotate, servermode lifecycle edge cases) still light |
| 8 | Federation / multi-repo (HOP model, `source_repo` routing, `federation_peers`, `routes`) | ⬜ | Real distributed subsystem; `federation.go` churned 6× recently |
| 12 | Orchestration internals (`internal/formula` ~9.7k lines, molecules, swarm, cook, gate) | ⬜ | Discussed abstractly; mechanics never examined |
| 13 | Testing architecture (test 230k > prod 173k; CGO matrix; 132 `t.Skip`) | ⬜ | Understood only as a number |
| 14 | Config three-way (`metadata.json` / `config.yaml` / DB `config`) | ⬜ | Known user-confusion source |

## D. Done — standalone deliverables

| Thread | Status | Doc |
|--------|--------|-----|
| Whole-project architecture analysis | ✅ | ARCHITECTURE_ANALYSIS.md |
| Extensibility / plugin demand analysis | ✅ | EXTENSIBILITY_ANALYSIS.md |
| Core engine (identity + graph/readiness) | ✅ | CORE_ENGINE_ANALYSIS.md |
| Agent-memory layer (prime/memory + compaction) | ✅ | AGENT_MEMORY_ANALYSIS.md |
| Storage runtime (concurrency/durability + is_blocked + merge-repair) | ✅ | STORAGE_RUNTIME_ANALYSIS.md |
| Authorship tension — "is beads becoming Gastown?" (essay, not survey) | ✅ | IS_BEADS_BECOMING_GASTOWN.md |
| Product identity — "is the graph the product?" (essay; interlocks w/ above) | ✅ | IS_THE_GRAPH_THE_PRODUCT.md |
| This index | ✅ (living) | ANALYSIS_INDEX.md |

---

### Suggested next pulls (priority order)
1. ~~#3 content-hash/ID + #5/#4 graph & readiness~~ — ✅ done (CORE_ENGINE_ANALYSIS.md).
2. ~~#9 compaction + #10 agent-context layer~~ — ✅ done (AGENT_MEMORY_ANALYSIS.md): the
   headline "memory for agents" thesis verified — genuine but narrower than sold.
3. ~~Close out **#6.2 / #6.4** + open **#11**~~ — ✅ done (STORAGE_RUNTIME_ANALYSIS.md):
   one story — *why storage is the bug epicenter.* is_blocked has no choke point; the
   merge-repair burden is accreting; default concurrency is single-writer.
4. **#8 federation / multi-repo** — the other genuine distributed subsystem still dark.
   Now the natural next pull: it sits on the same Dolt sync/merge substrate STORAGE just
   anatomized (`federation.go` already shows up in the is_blocked-after-pull recompute).
5. **#12 orchestration internals** (`internal/formula` ~9.7k lines) — the load-bearing
   mass behind the §6.1 charter/code split; never examined up close.
6. **#13 testing architecture** (test 230k > prod 173k) — the "battle-tested" claim the
   other docs lean on, still understood only as a number (132 `t.Skip`, CGO matrix).
