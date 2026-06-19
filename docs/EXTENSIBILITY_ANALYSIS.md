# Beads — Extensibility & Plugin Analysis

> Companion to [ARCHITECTURE_ANALYSIS.md](ARCHITECTURE_ANALYSIS.md). Where that
> document reads the architecture, this one answers a narrower question that
> kept surfacing: **can beads be extended without recompiling, what is the real
> demand for that, and which commands could actually move out of core?**
>
> Demand figures are a point-in-time snapshot of the upstream tracker
> (`gastownhall/beads`, ~269 open issues at time of writing), bucketed by
> title-keyword search. Labels are barely used upstream (`enhancement`=1,
> `bug`=8), so keyword buckets are the signal — they undercount (body-only
> mentions missed), overlap, and are directional, not exact. Conclusions lean on
> the *sampled content* of each bucket, not raw counts.

---

## 1. The starting question: is there a plugin system?

**No — not for extending beads core.** Every command is registered through
Cobra `AddCommand` at compile time (`rootCmd` in `cmd/bd/main.go`). There is no
git-style external dispatch (no `exec.LookPath("bd-foo")`), no manifest, no
runtime command registration, no Go `plugin.Open`. **To add `bd new-thing`
today, it must be compiled into the binary.**

The `plugins/` and `.claude-plugin/` directories are the *opposite* of what the
name implies: they package **beads as a plugin for AI harnesses** (Claude /
Codex / Copilot skills, agents, hooks), not a way to plug into beads.

## 2. The extension seams that *do* exist

Real today, in order of how much they can absorb without new architecture:

1. **Issue metadata (`Issue.Metadata` JSON) + metadata slots
   (`SlotSet/Get/Clear`).** The charter's blessed extension point. Arbitrary
   per-issue JSON, queryable (`MetadataFields`, `HasMetadataKey`). Already used
   in practice by orchestration: the `execution_*` hints and `gt` delegation
   slots ride on it. Data extension, not behavior.
2. **`.beads/hooks/` executable scripts (`internal/hooks`).** The only
   out-of-process, language-agnostic runtime seam — `on_create`/`on_update`/
   `on_close` run via `exec` with a 10s timeout. Narrow (3 events) but real.
3. **Custom types / custom statuses (config-driven).** Extend the
   type/status/label vocabulary with no code.
4. **Tracker registry (`internal/tracker/registry.go`).** `tracker.Register()`
   with `init()`-time registration — a clean compile-time plugin pattern, but
   Go-only, in-binary, and scoped strictly to tracker integrations.
5. **`--json` everywhere + the MCP server (`integrations/beads-mcp`).** The
   "build on top from outside" surface. This is the charter's actual
   composability story and where orchestration layers (`gt`/Gastown) live.

## 3. Why the heavy features went into core anyway

Molecules, swarms, gates, wisps, cooking, bonding live *in core* — in the schema
and the `Issue` struct — despite the charter assigning them to the orchestration
layer above beads. This was **not** a discoverability or guidance failure: the
charter exists, and an extraction was actually attempted. `internal/types/
types.go` records it:

> *"Most orchestrator types (convoy, merge-request, slot, agent, role, rig) were
> removed from beads core. They are now purely custom types... molecule, gate,
> and event were **re-promoted to built-in because bd commands rely on them**."*

Convoy/rig/agent/slot/role got pushed out successfully; molecule/gate/event/
message came back. The forces that pulled them back, by strength:

1. **Command-first gravity.** Features shipped as first-class CLI verbs
   (`bd mol`, `bd gate`, `bd cook`). A first-class command needs its concept to
   be a real validated, content-hashed, indexed type. Metadata can hold a
   molecule's *data* but cannot make `bd mol` a first-class verb. The command
   came first; the type had to follow.
2. **Readiness-engine coupling (the deep cause).** The most important
   orchestration features must *influence* `bd ready` — `waits-for`,
   `conditional-blocks`, and gate fields all feed `AffectsReadyWork()`. No
   existing seam reaches the readiness SQL. Metadata/hooks/MCP cannot express
   "this gate blocks readiness." For that class of feature, core was the only
   place that fit.
3. **Single-author vertical integration.** The same author builds beads *and*
   its orchestration layer (Gastown / Gas City, named in the charter). When your
   own orchestrator needs a primitive today, adding it to core beats designing a
   stable seam — and dogfooding rewards the immediate capability.
4. **Velocity vs. an aspirational gate.** Charter compliance is a doc gate, not
   a CI gate. Under fast cadence, "add a field/type" beats "design an extension
   point." The partial extraction shows the discipline is real but only bites
   when a feature is peripheral enough to leave.

## 4. Architectures that could change "must be compiled in"

Two distinct needs want different architectures:

- **Case A — a new verb that orchestrates existing bd ops** (`bd standup`,
  `bd burndown`, team workflows): **git-style external subcommand dispatch.**
  When `bd foo` isn't built in, exec `bd-foo` on `PATH`, forwarding args + env
  (`BEADS_DIR`). Any language; reads via `--json`, writes via `bd`. ~One
  command-not-found handler. Upgrade later to a **manifest registry**
  (`bd plugin install`, à la `gh extension` / `krew`) for discoverability. This
  is the CLI expression of what the charter already preaches.
- **Case B — behavior that must reach the engine** (gates that block readiness,
  custom merge resolvers): external dispatch can't do it. Needs **hashicorp/
  go-plugin (out-of-process RPC)** against a narrow, frozen subset of `Storage`
  *plus* a new **readiness-predicate extension point** the ready query consults.
  Native Go plugins (`plugin.Open`) are a non-starter here (toolchain pinning,
  no Windows, CGO/Dolt conflicts).

That readiness-predicate hook is the single highest-leverage architectural
investment *if* the goal is to let gates/molecules leave core. Without it they
cannot leave, no matter the will.

## 5. The real demand (this is the load-bearing finding)

Bucketing ~269 open upstream issues:

| Bucket | count | ~share | Plugin/seam-addressable? |
|---|---|---|---|
| **storage / sync / dolt / merge / migration / schema** | **102** | ~38% | **No** — engine/substrate. The dominant and most severe demand (fleet-wide data corruption, crash durability, `is_blocked` backfill returning blocked work). |
| core-command bugs (ready/dep/create/close…) | 42 | ~16% | No (and contaminated with storage bugs) |
| report / view / export / output | 31 | ~12% | Partly |
| custom status/type/label/priority | 25 | ~9% | Mostly integration/display behavior, not "add a custom status" |
| mcp / json / api | 16 | ~6% | **The de-facto extension API — and it's broken** (#4399: `bd update --json` emits unparseable concatenated JSON) |
| feat: (conventional requests) | 14 | ~5% | Partly (e.g. `bd setup cursor` is a clean dispatch fit) |
| hooks / webhook / trigger / automation | 12 | ~4% | Demand is "ship/fix it" (#3924 codex-hook missing from release, 8 👍) |
| config | 11 | ~4% | Mostly config/diagnostics bugs |
| tracker integrations (linear/jira/gitlab/notion/ado) | 10 | ~4% | Yes — via the existing registry seam |
| metadata / field / attribute | 8 | ~3% | ~0 genuine extension demand (top hit is a storage data-loss race) |

**Two conclusions the numbers force:**

1. **Demand is dominated by storage correctness (~2–3× anything else) and it is
   the most severe.** No plugin or seam touches it.
2. **The existing seams are under-hardened, not under-built.** Their buckets are
   dominated by *bugs in the seam* (broken `--json`, hook not shipped, tracker
   label fidelity, registry allowlist drift), not by gaps the seam can't
   express. Genuine *unmet* extension demand across all seams is small
   (likely <20 issues). A new plugin system would absorb only ~10–20% of
   incoming requests, mostly low-priority polish.

The most important single data point: the **JSON/CLI seam is the real extension
platform** (every external tool and any future plugin depends on it), and its
flagship issue is that the contract is broken. You cannot build a plugin
ecosystem on a `--json` that breaks `json.load`.

## 6. Which commands are actually candidates to leave core

Of ~108 top-level commands, three groups (verified against the code):

**Group 1 — sugar already expressible via `update`/`dep`/metadata/config
(no new architecture).** ~15 thin wrappers, all small (~2k lines total):
`defer`/`undefer` (= `update` status+defer_until), `note`/`assign`/`priority`/
`tag`/`promote` (= `update --field`), `link`/`supersede`/`duplicate`
(= `dep add --type`), `kv` (= metadata seam), `statuses`/`types` (= config vocab
seam), `setState`/`state`, `quick`/`todo` (= `create` shortcuts).
*Candidate ≠ should-delete:* they exist as discoverable verbs for agents/humans;
removing them is a UX call with low reward and zero demand signal (nobody asks to
delete `bd defer`).

**Group 2 — read-only views externalizable onto the JSON/MCP seam.**
`graph`, `diff`, `history`, `lint`, `query`, `sql`. Candidates *in principle*,
but **gated on hardening `--json` first** (see #4399).

**Group 3 — NOT candidates for any existing seam.** The ~18–20 orchestration
commands (`mol*`, `gate`, `cook`, `swarm`, `wisp`, `formula`, `pour`) — the
actual source of core bloat (`swarm.go` 1188, `cook.go` 1071, `mol_*` ~3–4k).
They couple to the readiness engine and need the Case-B hook (new architecture).
The 6 tracker commands are already on the registry seam.

**The uncomfortable shape:** ease-of-re-homing is *inversely correlated* with
weight in core. The commands that re-home trivially are tiny sugar nobody wants
removed; the commands that bloat core can't use any existing seam. There is no
existing seam that absorbs the weight.

## 7. Bottom line for maintainers

The whole investigation converges on one investment order, now backed by demand
data:

1. **Storage / Dolt correctness first.** ~38% of demand, the most severe, and
   nothing else helps it. (See ARCHITECTURE_ANALYSIS §6.2/§6.4.)
2. **Harden the JSON/CLI contract.** It is the real extension platform; fixing it
   (a) clears its own bucket, (b) unblocks externalizing Group-2 views, and
   (c) is the precondition for *any* future dispatch-plugin ecosystem.
3. **Tidy the tracker registry** (derive config prefixes from `tracker.Register()`
   instead of a hand-maintained allowlist; kills the #4427 drift class).
4. **Only then** consider new plugin architecture — and even then, start with
   the two places where demand and seam align (trackers, `bd setup *`
   generators), and treat the readiness-predicate hook as the gating prerequisite
   for ever moving orchestration out of core.

A general-purpose plugin system is a *forward-looking governance* investment (a
destination so core stops accreting), **not** a backlog drain. The data says it
is subordinate to storage stabilization and JSON-contract hardening.
