# Architecture map: where the mass is, derived from the code

> **Status: descriptive survey, not policy.** This document records what the
> codebase *is*, category by category, with line counts and coupling notes.
> Product-boundary policy lives in [PROJECT_CHARTER.md](PROJECT_CHARTER.md);
> the contested "should this move?" questions raised by this survey live in
> [ARCHITECTURE_MAP_DISCUSSION.md](ARCHITECTURE_MAP_DISCUSSION.md).
>
> **Snapshot:** verified against `main` at 7e9d60b29 (2026-07-17). Line and
> file counts are non-test Go (`find <dir> -name '*.go' ! -name '*_test.go'`).
> Counts drift; re-measure before load-bearing use.
>
> **Provenance:** initial survey produced by a Claude (claude.ai) research
> session commissioned by @maphew against a ~2026-07-08 clone; fact-checked
> and corrected against current HEAD by Claude Code agents before inclusion.
> The original snapshot predated the 2026-07-09 multi-backend landing, and
> its storage claims have been corrected accordingly (see Category 2).

Method: enumerate every registered cobra command (~117 top-level at HEAD),
every `internal/` package with line counts, and read enough of each subsystem
to classify it. Where docs and ground disagree, this reflects the ground.

## Gravity map (where the mass actually is)

| Area | Non-test lines | Notes |
|---|---|---|
| `cmd/bd` | ~109,800 (343 files) | The command layer is nearly 2x the size of storage. A lot of business logic lives here, not in packages. |
| `internal/storage` | ~59,500 | Dolt backend, embedded dolt, dbproxy, sqlkit (Postgres/MySQL/SQLite), schema, uow, issueops. The real kernel. |
| Tracker bridges (linear+ado+gitlab+notion+github+jira+`tracker`) | ~16,400 | Six vendor integrations, in-tree. |
| `internal/formula` | ~5,200 | The chemistry/workflow-templating engine. |
| `internal/doltserver` + dbproxy | ~2,500+ | Server lifecycle management. |
| config + configfile | ~3,500 | Config sprawl is its own subsystem. |
| Everything else | - | ui, telemetry, git, hooks, molecules, routing, etc.; each under 2k. |

Two structural facts jump out before any categorization: (1) the CLI layer is
the largest single body of code and holds real logic (routing resolution, sync
orchestration, guards), and (2) **27 `*_proxied_server.go` files** mean many
mutating commands have *two implementations*, an embedded-dolt path and a
shared-server path. Note this dual surface is not unowned drift: it is the
visible stage of the active uow migration workstream (see
[Discussion thread 8](ARCHITECTURE_MAP_DISCUSSION.md#thread-8)), which has
roughly doubled its coverage since early July (13 to 27 files).

---

## Category 1 - Kernel (this *is* beads; remove it and the product is gone)

The invariant core, the part that matches the README's one-line pitch
("dependency-aware graph replacing markdown plans"):

- **Data model** (`internal/types`): Issue, Dependency (+DependencyType),
  Comment, Label, Event, Status/IssueType enums with invariants
  (closed implies closed_at, etc.). Also already colonized by upper layers:
  `MolType`, `WispType`, and `BondRef` are type definitions here, and `gate`
  is a first-class `IssueType` constant (`TypeGate`).
- **Graph semantics**: dependency edges, cycle detection, blocked-state
  computation (`recompute_blocked`, `issueops/blocked*`), `ready` (the single
  most important query in the product: "what can be worked on now").
- **CRUD + query verbs**: `create`, `update`, `close`/`reopen`, `show`,
  `list`, `dep`/`link`/`relate`, `search`, `query` (mini query language),
  `count`.
- **Identity**: `idgen`, hash-based IDs, partial-ID resolution,
  `rename-prefix`.
- **Storage contract**: `storage.Storage` interface + transactions (`uow`),
  schema, migrations.
- **Config + init**: `bd init` (and its many variants), config get/set with
  side-effects and drift detection.

Verdict: coherent, correctly central. The one smell is upper-layer concepts
(mol/wisp/gate/bond) leaking into `internal/types`: the kernel knows about the
chemistry system rather than the chemistry system extending the kernel. Note
that `docs/architecture/index.md` documents these fields as intentional core
schema, so this is a framing disagreement, not just debt; see
[Discussion thread 3](ARCHITECTURE_MAP_DISCUSSION.md#thread-3).

## Category 2 - Substrate infrastructure (invisible machinery the kernel stands on)

- **Dolt lifecycle**: `internal/doltserver` (auto-start `dolt sql-server`),
  `internal/storage/dbproxy` (proxy child process, pidfiles, flocks),
  `db-proxy-child` internal command, embedded-vs-server-vs-proxied mode
  selection (`direct_mode.go`, `store_factory*.go`, cgo/nocgo build splits).
- **Alternative SQL backends**: `internal/storage/{postgres,mysql,sqlite}`
  plus shared `sqlkit` and dialect packages, wired through
  `store_factory.go` and covered by a conformance suite
  (`docs/architecture/storage-backends.md`). **This surface is contested
  ground**: multi-backend support landed 2026-07-09, a removal PR (#4847)
  merged 2026-07-16 and was reverted by its own author 51 minutes later
  (#4857) with no recorded rationale. Treat backend plurality as an open
  direction question
  ([Discussion thread 0](ARCHITECTURE_MAP_DISCUSSION.md#thread-0)), not as
  settled in either direction. (The Beads Classic SQLite backend removal of
  2026-03-02 is a separate, completed event.)
- **Durability plumbing**: `atomicfile`, `lockfile`, `remotecache`, backup
  machinery (`backup*`, auto-backup).
- **Guards**: a whole genus: `init_guard`, `config_guard`,
  `dolt_remote_guard`, `remote_migrate_gate`, `update_description_guard`,
  `bootstrap_backend_guard`, schema-skew detection, pollution detection.
  These exist because agents wield the tool destructively; they are scar
  tissue from real incidents, and they are *load-bearing* scar tissue.

Verdict: core-supporting, but the three Dolt execution modes (embedded /
external server / proxied server) are the biggest hidden complexity
multiplier in the codebase, now multiplied again by backend plurality.
`PROPOSAL-pluggable-storage-backends.md` proposes containing all three modes
as topologies inside the dolt backend; whether all three must survive is a
question for that workstream, not a greenfield one.

## Category 3 - Distribution & sync (the "distributed" in the name)

- **Native**: dolt remotes: `bd dolt push/pull`, `dolt_autopush`,
  `dolt_autocommit`, remote guard, `doltremote` pkg.
- **Federation** (`federation.go`, `internal/storage/federation.go`):
  peer-to-peer workspace sync, add-peer/list-peers, sovereignty config.
  Ambitious, dolt-native, relatively self-contained.
- **JSONL layer**: `export`/`import` (+ `export.auto`, `import.auto`), the
  git-artifact model. **Ground truth (verified at HEAD): there is no
  top-level `bd sync`.** "Sync" exists only as per-tracker subcommands
  (`bd github sync`, `bd ado sync`, ...) and dolt push/pull. Older docs
  describing `bd sync` as the git-JSONL heartbeat describe a previous era;
  JSONL survives as portability/backup/interchange format, not the primary
  sync bus. The current-generation docs (`docs/core-concepts/sync-concepts.md`,
  `docs/architecture/index.md`) already say this correctly.
- **Git integration**: hooks install (`init_git_hooks`, `hooks`,
  `migrate_hooks*`), `sync_git.go` helpers, `internal/git`.
- **Multi-repo routing** (`internal/routing`, `routed.go`,
  `routing_read.go`, `repo.go`): resolve an issue ID across multiple repos
  via `routes.jsonl`.
- **Cross-project deps**: `ship` (publish a capability another project can
  depend on).

Verdict: dolt-remote sync + federation are legitimately core to the thesis.
The JSONL path is a compatibility/portability layer and is already named as
such in current docs. Routing sits on the boundary; it is really an
orchestrator-scale feature (see Category 7).

## Category 4 - Agent coordination (the product thesis; second kernel)

This is what distinguishes beads from "a CLI issue tracker":

- **Work claiming with leases**: `update --claim`, `unclaim`, `reclaim`
  (lease expiry recovery), `heartbeat` (lease refresh), claim pools,
  `last_touched`. A real distributed-lease system (moved to an ephemeral
  dolt_ignored leases table in #4863, 2026-07-17).
- **`ready`** with explanations (`ReadyExplanation`): the agent's work queue.
- **`prime`**: AI-optimized context injection, MCP-aware output sizing,
  agent.profile policy wording, stealth-mode awareness. The load-bearing
  bridge between the DB and the model's context window.
- **Gates** (`gate`, `TypeGate`): async coordination primitives;
  wait-for-external-event as a first-class node.
- **Merge slots** (`merge_slot`): serialized conflict-resolution locks so N
  agents don't fight over merges.
- **Swarm** (`swarm`): validate/manage epics structured for parallel
  multi-agent execution.
- **Agent memory**: `remember`/`recall`/`forget`/`memories`: persistent
  memories injected at prime time. Plus generic `kv`.
- **Audit** (`audit`): append-only JSONL of agent interactions.
- **Session hooks**: `agent_hook`, `codex-hook`, `cursor-hook`: lifecycle
  hooks for specific agent runtimes.

Verdict: coherent as a layer, and it is the moat. But note it is *stratified*
by generality: claims/leases/ready/gates are substrate-grade; memory/kv/audit
are "apps built on the substrate" that happen to ship in the binary;
codex/cursor hooks are vendor-specific adapters (plugin-shaped, see
Category 8).

## Category 5 - Chemistry / workflow templating (a separable product inside the product)

`formula` (5.2k lines: parser, conditions, control flow, expansion,
schema-gen) + `mol` family (`cook`, `pour`, `wisp`, `distill`, `squash`,
`burn`, `bond`, `seed`, `progress`, `stale`, `current`, `ready-gated`) +
`internal/molecules` (template catalogs in `molecules.jsonl`) + `promote`
(wisp to bead).

This is a full template-to-proto-to-instance pipeline with its own state
taxonomy (solid/liquid/vapor), its own vocabulary, its own file format, and
its own subcommand tree. It *uses* the kernel (molecules are issues; wisps
are ephemeral issues) but nothing in the kernel needs it, except the type
presence noted in Category 1 and wisp-awareness sprinkled through storage
(`count_include_wisps`, ephemeral routing, purge).

Verdict: extension-shaped, currently welded in; the strongest candidate for
"would be better as a layer." But `docs/architecture/index.md` documents the
mol/wisp/gate field groups as stable core schema, so demoting them is a
design decision with migration cost, not a cleanup
([Discussion thread 3](ARCHITECTURE_MAP_DISCUSSION.md#thread-3)).
Counterargument: chemistry may be strategically core to Gastown-style
orchestration, in which case it belongs in the orchestration distribution,
not in every `bd` binary.

## Category 6 - Tracker bridges (plugin-shaped, in-tree)

`github`, `gitlab`, `jira`, `linear`, `notion`, `ado`: each an
`internal/<vendor>` client pkg + a `cmd/bd/<vendor>.go` with
`sync`/`push`/`pull`/`status` subcommands, unified by `internal/tracker`
(~2k lines of shared interface). ~16.4k lines total. `sync_push_pull.go`
mechanically stamps out push/pull for the API-based trackers.

Verdict: the most plugin-shaped subsystem, but two facts complicate the
"textbook extraction target" reading: (1) the shared interface is
Dolt-tinged; the tracker engine keys a fast path on raw DB access
(documented as a red-team finding in
`PROPOSAL-pluggable-storage-backends.md`), so extraction is not free; and
(2) [INTEGRATION_CHARTER.md](INTEGRATION_CHARTER.md) currently directs new
trackers *into* the in-tree pattern, and
[PROJECT_CHARTER.md](PROJECT_CHARTER.md) lists tracker integrations inside
core scope. Whether to extract, contain, or capability-gate them is
[Discussion thread 1](ARCHITECTURE_MAP_DISCUSSION.md#thread-1).

## Category 7 - Orchestrator coupling (Gastown bleed-through)

26 non-test files reference Gastown concepts. Concretely:

- `mail.go`: `bd mail` is a pure delegation shim to `gt mail` ("agents often
  type bd mail... this bridges that gap"). Configured via `mail.delegate`.
- `doctor_gastown_guard.go`: hardcodes knowledge of `mayor/town.json`,
  Gastown's workspace layout, to refuse `doctor --fix` at orchestrator roots.
- `merge_slot.go`: creates slots "for the current rig" (Gastown vocabulary).
- Routing (`routes.jsonl`) exists substantially to serve multi-project
  orchestrator workspaces.
- Scattered awareness in `bootstrap`, `init`, `prune`, `mol_current`, etc.

Verdict: this is the inverse of plugin-shaped; the host has grown organs for
one specific guest. The `mail` delegate is a good pattern (generic delegation
point, config-driven); the `mayor/town.json` path check is not (vendor
filesystem layout compiled into core). Note the tension is *inside the
project's own documents*: PROJECT_CHARTER's orchestration boundary says beads
should not encode orchestrator concepts in core, while
`docs/architecture/dolt.md` documents `gt dolt start` and the town-root rig
layout as the normal supported server-mode workflow. Resolving that is
[Discussion thread 2](ARCHITECTURE_MAP_DISCUSSION.md#thread-2).

## Category 8 - Editor/agent integration surface (correctly out-of-process)

- `integrations/beads-mcp`: Python MCP server (separate PyPI package).
  Right shape.
- `plugins/beads`: the Claude Code skill: SKILL.md + command docs +
  resources. Right shape (data, not code).
- `integrations/claude-code`, `integrations/junie`: instruction files.
- `setup` command + `internal/recipes` + `internal/templates`: bd writes
  workflow instructions into CLAUDE.md/AGENTS.md/etc. per-tool. In-binary,
  but it is templating, cheap.
- `onboard` (snippet for agent instruction files), `completions`.

Verdict: this boundary is already drawn well; the MCP server and skill live
outside the Go binary. The vendor session hooks (`codex-hook`, `cursor-hook`)
inside `cmd/bd` are the inconsistency: same species as the MCP server
(per-runtime adapters) but compiled in. Fine at n=2; a pattern to stop before
n=6 recapitulates the tracker situation.

## Category 9 - Human sugar

`todo` (self-described "convenience wrapper for task issues"), `epic`,
`quick` (create, print only ID), `note` (append comment), `human` (curated
command list), `tips`, `thanks`, `feedback`, `graph` visualizations
(`graph_visual`), `export_obsidian`, markdown rendering (`uimd`), Ayu-themed
terminal styling (`internal/ui`), `quickstart`, `where`, `info`.

Verdict: mostly cheap and harmless; sugar is fine when it is a thin wrapper
over kernel verbs (todo, quick, note all are). `export_obsidian` is a
format-specific exporter that belongs with the bridges/exporters family, not
core. The risk with sugar is not any one command, it is the aggregate: ~117
top-level commands means discoverability is already a problem the `human`
command exists to apologize for.

## Category 10 - Ops & hygiene (large, and internally redundant)

- **Doctor**: `doctor` + eight sibling files (health, fix, pollution,
  conventions, artifacts, agent, validate, gastown-guard) + a `doctor/`
  subdir. A diagnostics subsystem grown by accretion; every incident adds a
  check.
- **Space/history management, six overlapping verbs**: `cleanup` (delete
  closed), `prune` (delete old closed), `purge` (delete closed ephemerals),
  `gc` (decay + dolt gc + compact commits), `compact` (AI-summarize old
  closed; **calls Claude Haiku from inside the CLI**, `internal/compact`),
  `flatten` (squash dolt history). Plus `closed_delete_candidates`, `stale`,
  `orphans`, `duplicates`/`find_duplicates`/`duplicate`/`supersede`. Note
  `docs/architecture/dolt.md` documents the prune/purge split as deliberate
  design (reference-aware protection applies to prune only), so
  consolidation is a proposal, not a cleanup
  ([Discussion thread 5](ARCHITECTURE_MAP_DISCUSSION.md#thread-5)).
- **Recovery/migration**: `backup`/`restore`, `reset`, `migrate` family
  (dolt-mode, hooks, issues, personal), `upgrade`, `bootstrap`, `preflight`,
  repair-ish guards.
- **Quality**: `lint` (template sections), `rules` (audit/compact Claude
  rules files), `detect_pollution`.

Verdict: doctor earns its size (agents break things in creative ways).
`compact`'s AI dependency is a category error inside an offline-capable CLI
kernel: it is an *agent-powered maintenance app* and would sit more honestly
beside the MCP server than beside `gc`.

## Category 11 - Observability & meta

`internal/telemetry` (OTel, opt-in via env), `internal/metrics` +
`send_metrics` (command-event metrics), `version_tracking`,
`telemetry_redact`, `ping`, `version`, `help_all`, `docsmint` (tools/, doc
generation), BENCHMARKS.md machinery.

Verdict: appropriately thin and opt-in. Internal utility; nothing
controversial.

## Category 12 - Extension escape hatches (the *actual* current plugin API)

- `bd sql`: raw SQL against the dolt db. Explicitly blessed: the public-API
  doc says "most extensions should use direct SQL."
- Root `beads.go`: minimal public Go API (Storage, Transaction,
  RemoteStore, ...) for embedders; `examples/bd-example-extension-go`.
- `format/`: public rendering pkg "used by gt and other consumers."
- JSONL as interchange; `--json` on everything; `batch`.

Verdict: the honest answer to "what is the plugin system today" is
**"SQL + JSONL + a small Go API + shelling out to bd."** That is workable and
is how Gastown consumes beads, but note `format/` and the Go API exist
*because* gt needed them; the extension API is being carved by one consumer's
silhouette. `PROPOSAL-pluggable-storage-backends.md` (owner-directed, past
adversarial review) is the active plan for the storage half of this surface
and explicitly rejects dynamic plugins in favor of capability-gated stubs in
a single binary; any extension mechanism discussion must reconcile with it.

## Category 13 - Packaging & distribution

`npm-package/`, `winget/`, nix files (`flake.nix`, `overlay.nix`,
`packages.nix`, `default.nix`), `install.ps1`, `mint.sh`, `scripts/`,
Makefile, renovate. Internal/build only.

---

## What is core and should be defended as such

The kernel (Cat 1) + substrate (Cat 2) + dolt-native sync/federation (Cat 3)
+ the agent coordination primitives (claims/leases/ready/gates/prime, Cat 4).
That set is small, coherent, and matches the README's actual claim.
Everything else is periphery that should have to justify its seat in the
binary. The ranked "what should move, and where" questions derived from this
survey, together with which existing document or workstream each one
collides with, are maintained separately in
[ARCHITECTURE_MAP_DISCUSSION.md](ARCHITECTURE_MAP_DISCUSSION.md).

## Doc-vs-ground deltas noticed in passing

Verified at HEAD 7e9d60b29 unless noted:

- No top-level `bd sync`; sync is per-tracker + dolt push/pull. Current-era
  docs are correct; any older doc describing `bd sync` misleads.
- `issues.jsonl` at repo root contains a single issue; the JSONL dogfood
  surface is vestigial in the main repo. Real dogfooding runs through dolt.
- `docs/architecture/index.md` still says "Dolt as its sole storage backend"
  while its own related-docs list and `storage-backends.md` describe the
  multi-backend surface; one of them is wrong depending on how
  Discussion thread 0 resolves.
- `engdocs/CLAUDE.md` cites `internal/storage/db/` and
  `internal/storage/doltserver/`, which do not exist at HEAD (the real
  package is `internal/storage/dbproxy/`), and presents `cmd/bd` as a thin
  command layer, which the gravity map contradicts.
- `docs/architecture/dolt.md` troubleshooting offers only `gt dolt start`
  fixes, which assumes a Gastown install (see Category 7).
- `engdocs/DOC_INVENTORY.md` still keys several entries to pre-Mintlify
  paths and to the deleted `deploy-docs.yml`.
