# Beads — Agent-Memory Layer Analysis (the headline promise, verified)

> Unfolds the two subsystems that *are* the product's headline — "a memory
> upgrade for your coding agent." Beads sells itself as persistent, structured
> memory for AI agents; this document checks whether that promise is actually
> delivered, in code. Companion to
> [ARCHITECTURE_ANALYSIS.md](ARCHITECTURE_ANALYSIS.md) and
> [CORE_ENGINE_ANALYSIS.md](CORE_ENGINE_ANALYSIS.md); tracked in
> [ANALYSIS_INDEX.md](ANALYSIS_INDEX.md) (#9 / #10).
>
> Why this matters: the graph engine (CORE_ENGINE) is the product's *spine*, but
> "memory for agents" is its *pitch*. If the pitch is vaporware the project is
> mis-sold; if it is real it is the most defensible differentiator. The answer,
> verified below, is: **both delivery mechanisms are real and working — and both
> are shipped-then-half-abandoned in exactly the accretion pattern ARCH §6.5
> flags.** The promise is genuine; the follow-through is partial.

There are two distinct mechanisms by which beads delivers "memory":

1. **The context-injection / KV-memory layer** (#10) — what the agent *reads at
   the start of every session* (`bd prime`) and the durable notes it can stash
   and retrieve (`remember`/`recall`/`forget`/`memories`).
2. **Compaction / "memory decay"** (#9) — how old work is *summarized away* so it
   stops costing context, the on-thesis answer to "memory must forget to stay
   useful."

---

## Part 1 — The context-injection & KV-memory layer (#10)

### 1a. `bd prime` — the first surface (`cmd/bd/prime.go`, 713 lines)

`prime` is the single highest-leverage output in the product: it is what an agent
reads first, injected at session start via host `SessionStart` hooks. Its design
is sound in shape:

- **User override short-circuit.** If `.beads/PRIME.md` (or a redirected /
  `~/.config/beads/PRIME.md`) exists, prime dumps it verbatim and returns
  (`prime.go:149-173`). Projects fully control their own agent preamble — a clean,
  charter-aligned extension seam.
- **Host-aware envelopes.** `--hook-json` wraps output in the correct
  `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":…}}`
  shape for Claude Code / Gemini / Codex (`prime.go:206-220`); MCP vs CLI vs
  `--memories-only` modes branch at `prime.go:316-336`. Good integration
  ergonomics.
- **Memories are genuinely injected.** `formatMemoriesForPrime`
  (`prime.go:340-400`) lazily opens the store under a 10 s timeout
  (`BEADS_PRIME_TIMEOUT`, `prime.go:34-58`), reads config, filters `kv.memory.*`,
  and emits a `## Persistent Memories (N)` section, with a graceful
  storage-locked fallback message (`prime.go:402-411`). This is the real
  cross-session carry — it works.

**Confirmed gap — prime does not compute ready work; it only *tells the agent to
run it*.** The headline framing is "memory **+ ready work**," but `prime.go`
issues no graph query. Every occurrence of "ready" in the file
(`prime.go:498, 505, 627, 634, 639, 694`) is static instructional text — *"check
`bd ready`," "Start: Check `ready` tool for available work."* Verified:
`grep -E 'GetReadyWork|IterReadyWork|ListIssues|\.List\(' cmd/bd/prime.go` is
empty. The body of `outputCLIContext` (`prime.go:513-712`) is a ~200-line
hardcoded Go string-literal command cheat-sheet. So prime is **~95 % static
boilerplate**; the only dynamically computed pieces are a few branch booleans
(`isEphemeralBranch`, `primeHasGitRemote`, `prime.go:514-516`) selecting one of
four canned close-protocol blocks, plus the injected memories. The "live" part of
the first surface is small.

**Confirmed gap — memories are injected unconditionally, unranked, untruncated**
(full mode). `formatMemoriesForPrime` sorts all memory keys alphabetically and
emits every one (`prime.go:379, 393-397`). No relevance, recency, or token
budget. Context grows linearly with memory count; there is no cap and no "most
relevant N." For a *memory* product, the absence of any retrieval policy at the
exact injection point is the notable miss.

### 1b. `remember` / `recall` / `forget` / `memories` (`cmd/bd/memory.go`, 310 lines)

**Storage (verified): flat key-value rows in the `config` table.** All four
commands wrap `store.{Set,Get,Delete}Config` / `GetAllConfig` with the composite
prefix `kv.` + `memory.` (`memory.go:14, 80, 132, 213, 264`; `kv.go:13`), backed
by `REPLACE INTO config` / `SELECT value FROM config WHERE key=?`
(`internal/storage/issueops/config_metadata.go:12-54`). The `config` table is
`(key VARCHAR(255) PK, value TEXT NOT NULL)` (migration 0006).

- `remember` (`memory.go:45-106`): key = `--key` or `slugify(insight)` (first ~8
  words, 60-char cap, `memory.go:21-42`); updates in place if the key exists.
- `recall <key>` (`memory.go:248-290`): **exact-key lookup only** — no search, no
  fuzzy match.
- `forget <key>` (`memory.go:193-245`): exact-key delete.
- `memories [search]` (`memory.go:109-190`): loads the **entire** config table,
  filters `kv.memory.*` in Go, then optional **case-insensitive substring** over
  key OR value (`strings.Contains`, `memory.go:148-153`).

**Strength (real, and the load-bearing one):** this is genuine durable memory.
It survives sessions and credential/account rotation, is auto-surfaced by prime,
travels with the Dolt DB on `push`/`pull`, and has clean, parseable `--json`
(`{"key","value","action"}` etc., `memory.go:97-101, 273-281`). For the core use
case — "let me stash a durable note my future self / another agent will see" — it
delivers.

**Confirmed weaknesses (with proof):**

- **It is persistent but not *structured*.** Flat namespace, one `config` table,
  **no scoping** — no global-vs-repo-vs-session distinction anywhere; memories sit
  in the same Dolt-versioned DB as issues and sync with them. (Verified: only the
  single `kv.memory.*` prefix exists.)
- **Retrieval is exact-key or dumb substring — no ranking, FTS, or semantic
  search.** `grep -E 'embedding|vector|cosine|similarity|MATCH AGAINST'` over the
  memory path returns **zero hits**. There is no `MATCH AGAINST`, no scoring, no
  vectors. An agent that does not already know the slug cannot retrieve via
  `recall`; `memories <kw>` and `recall <key>` even have inconsistent semantics
  (substring vs exact-key).
- **`memories` is O(all config rows)** — full table load + client-side filter
  (`memory.go:126-153`), no SQL prefix predicate.
- **64 KB value ceiling.** Migration 0049 widened `issues`/`wisps`/`comments` to
  LONGTEXT but **deliberately does not touch `config`** (verified: 0049 lists only
  those three tables) — so memory values stay capped at `TEXT` (65,535 bytes) and
  large memories risk a silent MySQL error 1105.
- **Slug collisions overwrite silently.** Two insights whose first ~8 words
  slugify identically clobber each other with no guard (`memory.go:72-78`).
- **MCP agents get no memory *tool*.** The MCP server (`integrations/beads-mcp/`)
  calls itself a "Memory System" in its docstring but its tool list (`tools.py`)
  is issue-graph only — no `remember`/`recall`/`forget`. MCP-mode agents receive
  memories *only* as injected prime text, never as a callable retrieval surface.
  For the "memory for agents" pitch, the agent-facing protocol omits memory.

---

## Part 2 — Compaction / "memory decay" (#9)

This is the distinctive, on-thesis idea: memory that does not forget will drown
the agent in stale context, so beads summarizes old closed work to shrink its
context cost. **It is a real, LLM-backed, working feature — and also the clearest
single instance of the ship-then-half-abandon pattern in the codebase.**

### 2a. Naming collision (a real footgun)

"Compact" names **two unrelated subsystems sharing zero code:**

- **`bd admin compact`** — the on-thesis one: LLM-summarizes old closed *issue
  content* (`cmd/bd/compact.go`, registered at `admin.go:40`; package
  `internal/compact/`).
- **`bd compact`** (top-level) — squashes old *Dolt commit history* to shrink the
  versioned DB (`cmd/bd/compact_dolt.go:208`,
  `internal/storage/versioncontrolops/compact.go`).

The two are in latent tension (see 2e); the help text already has to
cross-reference them (`compact_dolt.go:27-28`). Renaming one is cheap insurance.

### 2b. The algorithm (`internal/compact/compactor.go:87-157`)

`CompactTier1`:
1. `CheckEligibility` — closed + has `closed_at` + age ≥ threshold + not already
   compacted (`internal/storage/issueops/compaction.go:39-86`).
2. `summarizer.SummarizeTier1` — **a real Anthropic Claude (Haiku) API call**
   (`compactor.go:117`; impl `internal/compact/haiku.go:77-201`).
3. **Size guard** — if the summary is not actually shorter, abort and leave the
   issue untouched (`compactor.go:124`). Principled.
4. **Destructive in-place overwrite** — set `description = summary` and **clear
   `design`, `notes`, `acceptance_criteria` to empty strings**
   (`compactor.go:133-138`). The originals are not copied anywhere first.
5. Record `compaction_level` / `compacted_at` / `compacted_at_commit` /
   `original_size` (`compaction.go:88-98`; fields at `types/types.go:62-65`) and
   add an audit comment.

**Strength — the summarization is genuinely intelligent, not mechanical.**
`haiku.go` imports `github.com/anthropics/anthropic-sdk-go` (verified at
`haiku.go:15`), makes a real `client.Messages.New` call with retry/backoff and a
purpose-built prompt asking for *Summary / Key Decisions / Resolution* under a
hard "must be shorter" instruction (`haiku.go:127-201, 264-291`), with OTel token
metrics and optional prompt/response audit logging. Model from
`config.DefaultAIModel()`; key from `ANTHROPIC_API_KEY` > `ai.api_key`
(`haiku.go:47-69`).

**Strength — the agent-native `--analyze` / `--apply` split is the right design.**
Three mutually-exclusive modes (`compact.go:107-126`): `--analyze`
(`compact.go:523`) exports candidates + full content as JSON for *the driving
agent itself* to summarize (no API key, no second model); `--apply`
(`compact.go:643`) ingests the agent-written summary and applies the same guarded
overwrite; `--auto` (`compact.go:159`) is the legacy direct-to-Claude path — and
the help text itself labels it "legacy" (`compact.go:49`). Letting the agent that
already has the context do the compression is exactly the on-thesis move.

**Strength — readiness graph is provably untouched.** Compaction is gated to
`status = closed` (`compaction.go:54`), which is already out of the ready set; no
dependency rows are altered. `DependentCount` is computed but informational
(`compaction.go:106-149`). It cannot corrupt `is_blocked` or ready-work.

**Strength — real test coverage**, not skipped: ~45 tests across
`compactor_test.go` / `compactor_unit_test.go` (22) / `haiku_test.go` (12) /
`dolt/compact_test.go` (21), driven by an injected `stubSummarizer` double so the
guard/field-clearing/metadata logic is exercised without an API key
(`compactor_unit_test.go:57-71`). Only the 2 live-API tests skip absent a key.

### 2c. Confirmed weakness — the "save the original first" archive is dead code

This is the sharpest finding. Migrations **0009 (`issue_snapshots`, with
`original_content`/`archived_events`)** and **0010 (`compaction_snapshots`, with
`snapshot_json`)** create exactly the tables you would use to preserve the
pre-compaction text before destroying it. **They are never written.** Verified:
`grep -E 'INSERT INTO (issue_snapshots|compaction_snapshots)'` over non-test code
is empty; the only references are FK-cleanup `DELETE`s
(`dolt/fk_violation_repair.go:30-31`) and table-clear loops
(`dolt/issues.go:348,425`). The `compactedSize` argument to `ApplyCompaction` is
even discarded — its parameter is named `_` (`dolt/compact.go:50`). The intended
safety net was scaffolded in the schema and then never connected.

### 2d. Confirmed weakness — Tier 2 is dead but still advertised

`bd admin compact --tier 2` hard-errors *"Tier 2 compaction not yet
implemented"* and exits 1 (`compact.go:273-275`). Yet `--stats` advertises
"Tier 2 (90+ days closed)… estimated savings 95 %" (`compact.go:515-519`), the
`compact_tier2_days` config (default 90) is read, and `GetTier2Candidates`
exists. The entire Tier-2 surface is advisory/dead. (The headline reduction
figures — Tier1 70 %, Tier2 95 % — are hardcoded display constants at
`compact.go:512-519`, not measured outcomes.)

### 2e. Confirmed weakness — compaction is effectively irreversible

Because the originals are overwritten in place (2b) and never archived (2c), the
*only* recovery is Dolt row-history via `bd restore` — which is **display-only**:
*"This is read-only and does not modify the database"* (`restore.go:25`). It
queries `store.History()` and heuristically prints the history entry with the
**largest content** as "pre-compaction" (`restore.go:62-83`); it does not write
it back. And the recovery path is itself fragile: the *other* `compact` (Dolt
history-squash, 2a) plus `dolt gc` can erase the very history rows `restore`
depends on. So a "memory decay" feature destroys structured fields with no
first-class undo.

### 2f. Confirmed weakness — no automation

Despite the "graceful decay" framing, there is **no on-close hook and no
automatic threshold trigger.** `CompactTier1` is only ever called from manual
`bd admin compact` invocation (`compact.go:272`). Decay only happens when a human
or agent remembers to run it — which, for a memory-hygiene feature, is the thing
most likely to be forgotten.

---

## Verdict

**The headline promise is real — and it is the honest core of the pitch.** Across
both mechanisms there is genuine, working, differentiated capability:

- Durable cross-session/cross-agent memory that auto-surfaces at session start
  (`prime` + `kv.memory.*`), with clean JSON and host-hook envelopes.
- A real LLM-backed compaction feature with a principled eligibility guard and an
  agent-native `--analyze`/`--apply` design that fits the thesis precisely.

Nobody should call "memory for agents" vaporware. But the marketing word
**"structured"** is where it strains, and **both** delivery paths carry the same
tell:

- Memory is **persistent but flat** — no scoping, no ranking, no semantic/FTS
  retrieval, a 64 KB ceiling, and (for MCP agents) no callable memory tool at all.
  `prime` injects *every* memory unranked and computes no ready-work.
- Compaction is **real but unfinished** — destructive in place, with its archive
  tables built-but-unwired (dead code), Tier 2 stubbed-yet-advertised, no
  automation, and no true undo.

**The throughline to the rest of the analysis:** this is the exact
ship-then-half-abandon pattern CORE_ENGINE saw in `conditional-blocks` and the
dead hex ID generator, and that ARCH §6.5 named as the accretion risk — now seen
on the *headline* feature, not a peripheral one. The strengths are genuine; the
last 20 % of follow-through (retrieval policy, archive wiring, undo, MCP memory
tool) is consistently the part that did not ship.

**Highest-leverage, low-blast-radius fixes (ranked):**

1. **Wire the existing `issue_snapshots` / `compaction_snapshots` tables before
   the destructive overwrite, and make `bd restore` actually restore.** The
   schema already exists (migrations 0009/0010); connecting it turns compaction
   from irreversible-by-omission into safe. Highest payoff per line. (§2c/§2e)
2. **Either implement Tier 2 or stop advertising it** — remove the `--stats`
   Tier-2 savings line and the `--tier 2` path until it does something. Don't ship
   a documented capability that hard-errors. (§2d)
3. **Give `prime` a memory *retrieval policy*** — at minimum a recency/count cap
   and a most-relevant-N, instead of dumping all memories alphabetically; and
   consider injecting a computed top-N ready slice rather than only telling the
   agent to run `bd ready`. (§1a)
4. **Expose memory as an MCP tool**, so protocol-driven agents can `recall`
   on demand rather than only receiving the prime dump. (§1b)
5. **Rename one of the two `compact` commands** to kill the footgun. (§2a)
6. Lift the `config` value ceiling to LONGTEXT (extend migration 0049's treatment
   to `config`) so memories cannot silently fail at 64 KB. (§1b)

None of these are architectural rewrites — they are finishing the feature the
project already built and already sells. That is the cheapest credibility the
roadmap has available.
