# Beads Analysis — Exploration Index

> Living tracker for the architecture/strengths analysis. Maps what has been
> unfolded (traced to code, with ramifications and remediation) versus what is
> still only named or asserted. Companion to
> [ARCHITECTURE_ANALYSIS.md](ARCHITECTURE_ANALYSIS.md),
> [CORE_ENGINE_ANALYSIS.md](CORE_ENGINE_ANALYSIS.md),
> [AGENT_MEMORY_ANALYSIS.md](AGENT_MEMORY_ANALYSIS.md), and
> [EXTENSIBILITY_ANALYSIS.md](EXTENSIBILITY_ANALYSIS.md).

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
| 6.2 | `is_blocked` denormalized cache | 🟡 | ARCH §6.2 | Saw churn; haven't traced invalidation choke points or tested recompute-on-read viability |
| 6.4 | Dolt boundary leak (merge resolvers, FK whitelist) | 🟡 | ARCH §6.4 | Mechanisms known; upstream-vs-beads fix-trend not verified |
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
| 8 | Federation / multi-repo (HOP model, `source_repo` routing, `federation_peers`, `routes`) | ⬜ | Real distributed subsystem; `federation.go` churned 6× recently |
| 11 | Daemon / server mode / dbproxy / circuit breaker | ⬜ | Multi-writer story; seat of severe upstream durability/concurrency bugs |
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
| This index | ✅ (living) | ANALYSIS_INDEX.md |

---

### Suggested next pulls (priority order)
1. ~~#3 content-hash/ID + #5/#4 graph & readiness~~ — ✅ done (CORE_ENGINE_ANALYSIS.md).
2. ~~#9 compaction + #10 agent-context layer~~ — ✅ done (AGENT_MEMORY_ANALYSIS.md): the
   headline "memory for agents" thesis verified — genuine but narrower than sold.
3. Close out **#6.2 / #6.4** — the two partial imbalances (is_blocked invalidation
   choke points; Dolt-boundary upstream-vs-beads fix trend).
4. **#8 federation / multi-repo** — the other genuine distributed subsystem still dark.
5. **#11 daemon / server / dbproxy** — the multi-writer durability story.
