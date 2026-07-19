# Architecture boundary questions: discussion agenda

> **Status: open questions, not decisions.** Companion to
> [ARCHITECTURE_MAP.md](ARCHITECTURE_MAP.md) (the descriptive survey). Each
> thread below is a contestable direction question the survey surfaced,
> stated with its supporting evidence, the existing document or workstream it
> collides with, and who needs to be in the room. The intent is to work
> these into a common roadmap; a resolved thread should graduate into an ADR
> under [adr/](adr/) or a decision record under [decisions/](decisions/),
> and the colliding documents updated to match.
>
> Ranking is by estimated win-to-risk ratio, but thread 0 gates several
> others and should resolve first.

## How to read a thread

- **Question**: the decision to make.
- **For**: evidence from the survey supporting a change.
- **Against / collides with**: existing docs, decisions, or active
  workstreams that claim this territory differently. These are not
  strawmen; per [PROJECT_CHARTER.md](PROJECT_CHARTER.md) review posture,
  the default is to absorb and transform, not to bounce.
- **Needs**: whose input is required before deciding.

---

## Thread 0: storage backend direction (gates threads 1, 7, 8)

**Question:** Is beads dolt-only with alternative backends as a temporary
experiment, or genuinely multi-backend with dolt as the flagship?

**History (all 2026):** Beads Classic SQLite backend removed 03-02
(87493ce91). Multi-backend sqlkit surface (Postgres/MySQL/SQLite +
conformance suite) landed 07-09 (1fc38ba77). Removal PR #4847 ("Simplify the
supported backends") merged 07-16 at 22:44 and was reverted by its own
author 51 minutes later (#4857), with no rationale recorded on either PR.

**For (contraction):** the three Dolt execution modes are already the
codebase's biggest complexity multiplier; backend plurality multiplies the
test and support matrix again; `docs/architecture/index.md` still describes
a dolt-only world.

**For (plurality):** `docs/architecture/storage-backends.md` documents a
conformance-tested, declaratively-extensible backend surface;
`PROPOSAL-pluggable-storage-backends.md` is owner-directed with a stated
success criterion ("beads runs on a Postgres backend") and survived
adversarial review; the 07-16 removal did not stick.

**Collides with:** itself, twice in one evening. This is the clearest case
of an undocumented decision in the project.

**Needs:** the #4847/#4857 author's rationale for the same-day revert
(problem found? sequencing? change of mind?), and the proposal owner. Until
recorded, no other doc should assert either direction as settled.

## Thread 1: tracker bridges: extract, contain, or capability-gate

**Question:** Do the six vendor tracker integrations (~16.4k lines) stay
compiled into every `bd` binary?

**For (moving them):** each is a vendor API client with credentials, rate
limits, and field mappings that churn on vendor schedules, not beads
schedules; every user ships all six while using zero or one; they already
share `internal/tracker` (~2k lines of interface).

**Against / collides with:**

- [PROJECT_CHARTER.md](PROJECT_CHARTER.md) lists "integrations that
  translate external tracker data into beads concepts" *inside* core scope.
- [INTEGRATION_CHARTER.md](INTEGRATION_CHARTER.md) directs new trackers into
  the existing in-tree pattern and treats per-tracker scope limits, not
  extraction, as the answer to scope creep (decision log 2026-03-24).
- `PROPOSAL-pluggable-storage-backends.md` states "single binary, no dynamic
  plugins"; addons are capability-gated stubs plus build tags. An
  out-of-process `bd-tracker-<vendor>` subprocess design contradicts that
  stance; a capability-gated in-binary design extends it.
- The shared interface is not vendor-clean: the tracker engine keys a Dolt
  fast path on raw DB access (red-team finding recorded in the proposal),
  so "extraction is easy because the interface exists" overstates.

**Needs:** thread 0 resolved (the extraction mechanism should match the
storage proposal's architecture); integration charter owner; a decision on
whether GitHub stays blessed in-tree for the dogfooding/projection workflow.

## Thread 2: Gastown knowledge: genericize into an orchestrator adapter surface

**Question:** Should `bd` compile in knowledge of one specific orchestrator
(Gastown), or expose a generic adapter surface any orchestrator can fill?

**For:** 26 non-test files reference Gastown concepts; `mayor/town.json`
(a vendor filesystem layout) is hardcoded in `doctor_gastown_guard.go`;
"rig" vocabulary appears in `merge_slot.go`. The charter's orchestration
boundary explicitly says beads should not encode orchestrator concepts in
core. The `mail.delegate` config pattern already shows the generic shape.

**Against / collides with:** `docs/architecture/dolt.md` documents
`gt dolt start` and the town-root rig layout as the normal supported
server-mode workflow, and its troubleshooting section has no non-Gastown
fix. The coupling is currently load-bearing product surface for the main
consumer, not dead code. Removing proper nouns without an adapter in place
breaks the primary deployment.

**Note:** this is a charter-vs-docs contradiction inside the project today;
either the charter's boundary needs a carve-out or the docs are describing
scar tissue as design.

**Needs:** Gastown maintainer(s); agreement on a minimal adapter contract
(delegation commands, workspace-root detection hook, slot naming) that gt
implements first.

## Thread 3: chemistry (formula/mol): first-class schema or registered layer

**Question:** Should the kernel know `MolType`/`WispType`/`BondRef`/gate
natively, or should chemistry register itself through metadata and public
verbs?

**For (demotion):** the formula engine (~5.2k lines) plus the mol subcommand
family is self-contained, uses the kernel only to create issues and deps,
and its vocabulary raises the learning curve for everyone who does not use
it. The charter's own metadata-before-schema policy argues for metadata
here. Wisp-awareness is sprinkled through storage (`count_include_wisps`,
ephemeral routing, purge).

**Against / collides with:** `docs/architecture/index.md` documents
molecule/gate as first-class issue types and the mol/wisp/gate/lease field
groups as stable core schema; demotion is a schema migration with real
cost, and the charter also says schema changes need pressing justification,
which cuts both ways. Chemistry may be strategically core to Gastown-style
orchestration (thread 2's consumer).

**Needs:** chemistry/formula workstream owner; a cost estimate for
metadata-izing the existing fields vs hardening the internal boundary
("only talks to kernel via public verbs") without moving data.

## Thread 4: AI-powered `compact`: companion tool or in-kernel

**Question:** Should an offline-capable CLI kernel carry a Claude API client
(`internal/compact`, AI-summarize old closed issues)?

**For (moving it):** it is an agent-powered maintenance app, category-wise a
sibling of the MCP server, not of `gc`; it drags a network dependency and
API-key handling into the core binary.

**Against / collides with:** no doc defends the current placement; the cost
is mostly packaging and user migration. Lowest-controversy thread here.

**Needs:** a decision on where it lands (companion binary, MCP-side tool, or
capability-gated stub per the proposal's addon pattern; note thread 0/1
mechanism alignment).

## Thread 5: deletion-verb consolidation

**Question:** Fold `cleanup` / `prune` / `purge` / `gc` / `compact` /
`flatten` into one policy-driven `gc`?

**For:** six verbs whose distinctions (closed vs old-closed vs
closed-ephemeral) are policy parameters wearing command costumes; the
aggregate contributes to a ~117-command discoverability problem.

**Against / collides with:** `docs/architecture/dolt.md` documents the
prune/purge split as deliberate, differentiated design; reference-aware
protection applies to prune while purge is exempt because ephemeral
references are themselves transient. Consolidation must preserve those
semantics and the muscle memory of existing agents/scripts (deprecation
aliases, not removal).

**Needs:** maintainer consensus on UX; an inventory of which verbs appear in
generated agent instructions in the wild.

## Thread 6: vendor session hooks out-of-process

**Question:** Should `codex-hook` and `cursor-hook` move to the same
out-of-process pattern as the MCP server before more runtimes accrete?

**For:** they are per-runtime adapters, the same species as the MCP server,
which already lives outside the binary; at n=6 this recapitulates the
tracker situation.

**Against / collides with:** nothing doctrinal; at n=2 the carrying cost is
small, so this is about stopping a pattern early rather than fixing a
problem. Cheap to decide, low urgency.

**Needs:** agreement on the pattern for the *next* runtime that shows up,
even if the existing two stay put.

## Thread 7: descend `cmd/bd` logic into packages

**Question:** How do we stop `cmd/bd` (~110k non-test lines, nearly 2x
storage) from being the application?

**For:** anything two commands share (routing resolution, sync
orchestration, guard logic) should live in `internal/`; this is the
prerequisite for any future where something other than the CLI (MCP server,
gt, a daemon) drives beads without shelling out.

**Against / collides with:** no doc opposes it; in fact
`PROPOSAL-pluggable-storage-backends.md` already plans part of it (command
registry, commit protocol out of CLI globals). The collision risk is
*sequencing*: bulk mechanical descent would conflict with the active uow
migration (thread 8) and the proposal's own refactors. Also
`engdocs/CLAUDE.md`'s "thin Cobra command layer" description should be
corrected regardless of pace.

**Needs:** sequencing agreement with the uow and storage-proposal
workstreams; a rule for new code ("no new business logic in cmd/bd") is
available immediately even if the backlog moves slowly.

## Thread 8: the proxied-server dual surface

**Question:** What is the endgame for the 27 hand-written
`*_proxied_server.go` command duals?

**Correction to the survey's first draft:** this is not unowned drift. The
duals are the visible stage of the active uow migration workstream (one
command per PR since 2026-05-10), which has roughly doubled coverage since
the storage proposal's 2026-07-02 census (13 to 27 files, now including
`purge` and `version`, which the proposal's text says have no dual; the
proposal's D1 gap analysis needs re-measuring). The proposal sketches a
reconciliation (a uowStore adapter collapsing the duals) and notes the
decision "requires the uow workstream owner in the room."

**For (acting):** every dual is a divergence risk; dual-path drift has
already produced real test flakiness.

**Needs:** the uow workstream owner, full stop. Until then the only safe
statement is: do not add a third path, and re-measure before deciding.

---

## Appendix: doc fixes that need no discussion

These can proceed as ordinary PRs regardless of how the threads resolve
(thread-0-dependent items noted):

1. `engdocs/CLAUDE.md`: replace the nonexistent `internal/storage/db/` and
   `internal/storage/doltserver/` paths (actual: `internal/storage/dbproxy/`
   and current runtime packages); soften the "thin command layer" claim.
2. `docs/architecture/index.md`: "sole storage backend" contradicts its own
   related-docs entry and `storage-backends.md` (wording depends on
   thread 0, but the self-contradiction can be flagged now).
3. `docs/architecture/index.md` "When NOT to use Beads": the large-team
   caveat cites the retired git-JSONL sync model; rewrite against Dolt
   remotes + server mode.
4. `docs/architecture/dolt.md` troubleshooting: add standalone
   (`bd dolt start`) alternatives beside the `gt dolt start` instructions.
5. `engdocs/DOC_INVENTORY.md`: re-key the disposition table to post-Mintlify
   paths; drop the deleted `deploy-docs.yml` reference.
6. `engdocs/REPO_CONTEXT.md`: the `GitCmd` example stages
   `.beads/issues.jsonl`, a git-JSONL-era workflow now discouraged; pick a
   current example.
7. `engdocs/INTERNALS.md`: flush-path wording still describes JSONL-era
   write behavior in places ("JSONL write time"); align with the Dolt-commit
   flush path.
