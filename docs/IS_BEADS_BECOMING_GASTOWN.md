# Is Beads Becoming Gastown?

> The authorship tension — and why the charter cannot hold.
>
> This is an essay, not a survey. The other documents in this set map subsystems;
> this one chases a single question that reframes most of them: beads ships with a
> disciplined charter that forbids orchestration, storage-engine, and schema
> growth, and the code violates all three. The easy reading is "discipline
> slipped." The argument here is that discipline is not the variable — **authorship
> is** — and that this changes what the fix even is. Companion to
> [ARCHITECTURE_ANALYSIS.md](ARCHITECTURE_ANALYSIS.md) (§6.1) and
> [EXTENSIBILITY_ANALYSIS.md](EXTENSIBILITY_ANALYSIS.md) (§3); tracked in
> [ANALYSIS_INDEX.md](ANALYSIS_INDEX.md).

---

## 1. The weak answer, and why it misleads

Ask "is beads becoming Gastown?" and the obvious answer is *yes — orchestration
leaked into core.* That is what ARCHITECTURE §6.1 found: `mol`, `gate`, `swarm`,
`cook`, and the wisp agent/rig/role types live in the schema and on the `Issue`
struct, despite the charter assigning them to the layer above. True, and well
evidenced.

But that framing quietly smuggles in a diagnosis: if features *leaked*, then the
leak is a *discipline* failure, and the fix is *governance* — "make scope-fence
violations a blocking review criterion with teeth," as §6.5 recommends. That
recommendation has been available the whole time. It has not worked. When a
proposed fix has been obvious and available and still hasn't taken, the fix is
usually aimed at the wrong variable.

The wrong variable is discipline. The right one is who holds the pen.

## 2. The charter is a photographic negative

Read the Storage Boundary as if you'd never seen the code
(`PROJECT_CHARTER.md:50-53`):

> *"Avoid beads-side flocks, engine introspection, storage-specific retry loops,
> crash-recovery workarounds, or schema poking that belongs in Dolt or the Dolt
> driver."*

Five named prohibitions. The previous session found the code does **all five**:
a beads-side flock (`acquireEmbeddedLock`), storage-specific retry loops (port
reclamation, the circuit breaker), a crash-recovery workaround
(`manifest_recovery.go`, for Dolt's *"root hash doesn't exist"*), and schema
poking (the hand-written conflict resolvers and FK-cascade repair). This is not a
boundary that was set and then drifted past. It is a list that names, with
forensic precision, the exact things the code does.

That precision is the tell. A genuine forward-looking constraint is *general* —
"beads should stay small enough to remain reliable" (`PROJECT_CHARTER.md:8-9`).
You only write a sentence as specific as *"avoid beads-side flocks"* when you are
looking directly at a flock you have already written. Whether the words were
typed before or after the violation, the charter **functions** as a negative
image: each boundary is a precise outline of the breach the code now fills.

Walk the other two and the pattern repeats. The Orchestration Boundary names
"Gastown, Gas City, schedulers, swarms, release coordinators"
(`PROJECT_CHARTER.md:31-34`) — and `swarm`, gates, and release-flow concepts are
in core. The Schema Boundary says "the schema is considered stable... use
metadata first" (`:59-73`) — over fifty migrations and a forty-plus-field `Issue`
later. Three boundaries, three negatives. The charter is not the project's
conscience guiding the work; it is the project's conscience **narrating** the
work it cannot stop.

## 3. Why a negative and not a fence: one hand on both sides

A fence is only as real as the independence of the parties it separates. The
charter's boundary sits between *beads-core* and *the orchestrator above it* — and
the same person owns both. The author writes the rule, is the party the rule
restrains, and is the reviewer who would enforce it. Collapse those three roles
into one and the charter cannot be a gate; it can only be a diary of intentions.

You can watch the mechanism fire, in the code's own words
(`internal/types/types.go:544-551`):

> *"Most orchestrator types (convoy, merge-request, slot, agent, role, rig) were
> removed from beads core. They are now purely custom types... molecule, gate, and
> event were **re-promoted to built-in because bd commands rely on them.**"*

Read that as a transaction. The boundary was *enforced* — those types were
evicted to "custom types." Then it was *reversed*, and the stated reason is that
the substrate ("bd commands") "rely on" the orchestrator's concepts. The consumer
reached down into the substrate, the substrate said yes, and the schema was
changed to make the yes legal. There was no independent owner of beads-core to
say "no — design a seam." There could not be: the owner of beads-core is the
person who wanted the feature shipped today.

This is why "add teeth to the review gate" was never going to work. **You cannot
review-gate yourself.** Teeth require a jaw that is not your own.

## 4. The fault line proves it is structural, not lazy

If this were mere indiscipline, the violations would be scattered — wherever
attention lapsed. They are not scattered. They fall along a clean line.

What left core and *stayed* gone: `convoy`, `merge-request`, `slot`, `agent`,
`role`, `rig` — concepts that are **pure vocabulary**. A `role` is a label; the
readiness engine never consults it. What came back: `molecule`, `gate`, `event`,
`message` — every one of which the engine or the CLI must *operate on*. `gate`
feeds `waits-for` and thus `bd ready`; `event` is the audit spine; `molecule` is
a structural grouping `bd` traverses; `message` is inter-agent comms (GH#1347).

So the boundary held *exactly* where the orchestration concept could sit inertly
in metadata, and failed *exactly* where it needed to reach the hot path. That is
not a lapse of attention; it is a load-bearing fault line. The author can hold
the line for anything decorative and cannot hold it for anything that touches the
engine — because for engine-touching concepts there is no seam to express them
from outside (EXTENSIBILITY §4 names this: no readiness-predicate extension
point), and no independent core-owner to demand one be built. Discipline is real
and visible in the half that left. It simply cannot win the half that matters.

## 5. The wisp universe is the denial made physical

Here the authorship tension and the previous analyses' sharpest concrete finding
turn out to be the same thing seen from two sides.

The orchestrator's vocabulary that *did* stay in core — `rig` (in 21 migration
sites), `hook_bead`, `role_bead`, `agent_state`, `role_type`, `await_type` — does
not sit in the `issues` table. It concentrates in the **`wisps`** tables, the
ones migration 0019 excludes from version control. ARCHITECTURE §6.3 read the
wisp duplicate-universe as an artifact of a Dolt limitation (table-scoped
`dolt_ignore` forcing a row-level concept into a separate table). That is true at
the mechanical level. But step back to the authorship level and a second reading
appears, and it is the deeper one:

**Wisps are where beads quarantines the Gastown concepts it cannot admit are in
core.** The whole point of the shadow schema is to keep agent/rig/role/message
churn *out of the versioned issue graph* — so that the issue graph can still be
described, truthfully on its own terms, as "just an issue tracker." The wisp
universe is the architecture of the sentence *"beads should not encode their
concepts in core."* It is the denial, given a 51-column table and a shadow
migration stream. The cost session 1 measured — double-entry schema, the #4138
backfill bug, the per-query UNION tax — is the **running cost of maintaining the
fiction.** You are paying, on every `bd ready`, to keep the orchestration concepts
in a room the charter can pretend isn't part of the house.

## 6. The same force is the strength — this is not a hit piece

It would be a misreading to treat the author as careless. Single-author vertical
integration is *why beads is good.* It is why the core loop is so coherent, why
the hash-ID/`depid` design is so considered (CORE_ENGINE §1), why the tool
dogfoods itself convincingly, why it ships at 110 releases. The same hand on both
sides of the fence is exactly what lets the substrate and its consumer evolve in
lockstep without the friction of a negotiated interface. Vertical integration is
a superpower for coherence.

It is simply also kryptonite for boundaries. The charter violations and the
product's unusual coherence are **not two facts; they are one phenomenon viewed
from two sides.** The thing to internalize is not "the author lacks discipline" —
he plainly has more than most, or there would be no charter at all and no
half-successful extraction to point to. It is that *discipline is the wrong tool
for this job*, and reaching for more of it is reaching for a longer lever on a
fixed pivot.

## 7. What this changes about the fix

ARCHITECTURE §6.1 already framed the real fork — "amend the charter to embrace
orchestration, or extract it. Pick one." The authorship reframe sharpens *why
those are the only two*, and strikes a third option off the table:

- **(a) Stop denying.** Fold the Orchestration Boundary. State plainly that beads
  *is* Gastown's substrate, and then **design** the durable/ephemeral split as a
  first-class concern instead of paying the wisp-shadow tax to pretend it isn't
  one. This is a documentation change plus the refactor session 1 already
  specified — and it is *cheaper after the admission*, because once orchestration
  is openly in scope, the ephemeral store can be designed rather than smuggled.
- **(b) Change the authorship structure.** Give beads-core to an owner whose job
  is to defend it and who can tell the orchestrator "no — build the seam." This
  is the only thing that makes the fence real, and it is a **people** change, not
  a code change. It is also the more expensive one, and may simply not be what
  the author wants — which is a legitimate answer, but then (a) is forced.
- **(c) ~~Enforce the existing charter harder.~~** Struck out. There is no
  independent enforcer; a self-imposed gate is documentation. Every hour spent
  trying to make the current charter bite is spent sharpening a knife with no
  handle.

The limbo ARCHITECTURE §6.1 called "the worst of both" is not a transitional
state waiting for better governance. It is the **stable equilibrium** of a
boundary with one owner on both sides. It will persist until either the boundary
is dropped (a) or its ownership is split (b). Nothing between those two moves it.

---

## The principle to carry out of this

> **A boundary written by the party it constrains is documentation, not
> enforcement. The more precisely such a boundary names its own violations, the
> more certainly those violations have already happened — specificity in a
> self-imposed rule is a confession, not a constraint. A constraint becomes
> load-bearing only when the cost of breaking it falls on someone other than the
> person who benefits from breaking it.**

The corollary is the practical test, and it generalizes far past beads — to any
monorepo's module boundaries, any "we keep the API layer pure" rule, any
microservice split drawn by one team: **an interface between two layers owned by
the same hand is decorative. It will be crossed exactly when crossing is
convenient — which is exactly when it matters.** If you want a boundary to hold,
do not write it a stronger rule. Give the other side of it to someone who is
allowed to say no.

So: *is beads becoming Gastown?* No — that undersells it. Beads **is** Gastown's
substrate already; the charter is the precise, honest, and structurally
unenforceable record of the author knowing it shouldn't be. The most expensive
thing in the project is not the orchestration in core or the storage logic in
core. It is the gap between the two, held open by a fence that, by construction,
its own author cannot climb down from alone.
