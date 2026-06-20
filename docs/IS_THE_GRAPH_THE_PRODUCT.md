# Is the Graph the Product?

> The product is a verb, not a noun.
>
> An essay, companion to [IS_BEADS_BECOMING_GASTOWN.md](IS_BEADS_BECOMING_GASTOWN.md).
> The question sounds like it has a yes/no answer about a data structure. It
> doesn't. Answering it honestly separates three organs the project calls by two
> names, finds where the engineering mass actually sits, and — the payoff — turns
> out to *explain the Gastown finding* rather than sit beside it. Tracked in
> [ANALYSIS_INDEX.md](ANALYSIS_INDEX.md).

---

## 1. The question is mis-axed

"Is the graph the product, or is memory the product?" assumes two organs. There
are three, and the confusion in the question comes from one word doing two jobs.

- **Memory-as-recall** — the `bd remember` / `bd recall` / `bd memories` family.
  A ~310-line key-value store over Dolt's config table, with substring lookup. No
  ranking, no embeddings, no summarization, no context assembly. Nothing else in
  the system reads from it.
- **The dependency graph** — issues plus typed edges (blocks, parent-child,
  related, discovered-from), with `dependency_count`/`dependent_count`
  denormalized onto every issue. First-class, and the README's headline noun.
- **Readiness dispatch** — `bd ready`: the computation that walks the graph and
  answers *"which issues are unblocked and claimable right now?"* ~754 lines of
  query engine, ~748 of blocking-state propagation, ~1,028 of dependency logic,
  backed by **9,359 lines of tests** (per the code survey). Four-plus subsystems
  consume it.

These are not three points on one axis. They are a **skeleton** (the graph), a
**function** (dispatch), and a **sales pitch** (memory). The question "is the
graph the product?" conflates the first two and the marketing conflates the
third with the first. Untangle them and the answer falls out.

## 2. The README names the container and never the computation

Read the front matter cold, the way the charter essay read the charter cold.

The **headline** (line 3) is *"Distributed graph issue tracker for AI agents."*
Graph leads. Good — that's the skeleton, named.

The **first prose sentence** (line 15) is the tell: *"Beads provides a persistent,
structured memory for coding agents. It replaces messy markdown plans with a
dependency-aware graph."* Read what that sentence actually does: it introduces
"memory" and then, in its very next clause, **defines memory as the graph.**
"Memory" here is not a rival organ hiding the graph. It is the *value-word for*
the graph — the benefit ("your agent won't lose the plan") attached to the
mechanism ("a dependency-aware graph"). My earlier hint — "a dependency engine
wearing a memory costume" — was wrong in a specific way: it isn't a costume over
a different organ. The costume is labelled with the organ's own function on the
next line.

Now count the vocabulary across the whole README: `memory` 5, `graph` 4, `ready`
4, `depend` 3, `block` 2 — and `schedule` **0**, `dispatch` **0**. The README
names the *container* in every register it can find (a graph, a memory, a tracker)
and **never once names the computation the container exists to perform.** The
thing the code is — a readiness dispatcher — is the one word-class entirely
absent from how the product describes itself. They are selling the noun and
withholding the verb.

## 3. The name collision is the whole source of the confusion

Here is why "is *memory* the product?" feels like a live question at all, when the
code so plainly says no. "Memory" is a **homonym** in beads:

- **Memory₁** — the system metaphor (line 15): the durable issue graph *is* your
  agent's memory. Load-bearing. True. = the graph.
- **Memory₂** — the literal feature (lines 46, 69): `bd remember "insight"`, a
  ~310-line sticky-note table. Trivial. Reads from nothing, feeds nothing but
  `bd prime`'s footer.

Memory₂ squats on Memory₁'s name. A reader (or an analyst) sees `bd remember`,
sees "structured memory for coding agents," and reasonably wonders whether the
product is the recall feature. It isn't — Memory₂ is a convenience the size of a
label system, and the falsifier is dead on inspection: there is no recall
machinery to be the product *of*. But the homonym means the smallest feature in
the system borrows the gravitas of the largest metaphor. **That is not a costume
hiding the organ; it is a homonym confusing the diagnosis.** It is also a fixable
documentation bug: rename the feature (`bd note`/`bd pin`) or rename the metaphor,
but don't let a 310-line KV store and the entire product share a word.

## 4. The product is the verb, and the verb is dispatch

Strip the marketing and ask the only question that decides a product: *where did
the engineering actually go, and what does everything else bend toward?* Two
measures, both pointing the same way.

**Investment.** Readiness + dependencies: ~5,700 lines of core logic against
9,359 lines of tests. Memory-recall: ~310 + ~350. The ratio is past **20:1**. You
do not write 9,000 lines of tests for a feature; you write them for the thing the
product *is*, the thing whose edge cases keep drawing blood.

**Fan-in.** `bd ready` is not a leaf. Gates resume through it (`--gated` →
`findGateReadyMolecules`), molecule dispatch consumes it, patrol/swarm find their
next unit through it, `bd prime` points agents at it as the primary verb, batch
claim filters on it. Memory-recall's fan-in is zero. In any system, the node with
the highest *investment × fan-in* is the product; everything else is a feature
hanging off it. Here that node is unambiguous, and it is a verb: **compute what is
workable now and hand it to an agent.**

And the *specificity* of that verb is itself evidence it is real and used — the
same argument the charter essay turned on. Decorative engines stay generic.
Beads' blocking logic knows that a child of a gate stays blocked until the gate
closes *and* an any-child metadata condition is met; that children of an issue
with a future `defer_until` are excluded; that cycles must be detected across both
the `dependencies` and `wisp_dependencies` tables. You only discover rules that
specific by living inside a real dependency graph under real load. The specificity
is a fingerprint of use.

So: **is the graph the product? Yes — but "graph" is the skeleton noun.** The
product is the *readiness dispatch that runs on* the graph. The graph is what the
product is made of; dispatch is what the product is *for*; memory is what the
product is *sold as*.

## 5. Do the makers actually use it? (an honest, partial test)

Code investment shows what was *built*, not what is *used*. The clean test is
beads' own dogfooded graph — but it isn't in this repository. The live data lives
in Dolt and syncs through a Dolt remote; `.dolt/` and `.beads/*.db` are
git-ignored by design. Which is itself a small, real irony worth stating: **the
"persistent structured memory for coding agents" is, for beads' own project, not
in version control.** You cannot learn what beads remembers about beads by reading
the beads repository. The memory lives only in the remote.

What *is* visible is the commit log, and it carries a faint but legible imprint of
the graph in use. Of the last 2,000 commits, 23 cite a `bd-` id; **17 of those 23
are hierarchical** (`bd-6dnrw.28`, `.30`, `.31`, `.33`, `.34`, `.39`…), and a
single epic — `bd-6dnrw`, a "remote-migrate gate" effort — is decomposed into ten-
plus numbered children worked across many commits. Seventy-six of the 2,000 carry
gate / blocked / dependency / ready language. The honest reading of those numbers,
both halves:

- **For "the graph is used":** when these authors structure real work, they
  structure it as a *parent-child, gated decomposition* — exactly the graph the
  engine is built for. The bd-6dnrw epic is the readiness/gate machinery being
  dogfooded in the open. Not decorative.
- **Against over-claiming:** only ~1% of commits link to an issue at all. The
  beads work-graph and the git history are **parallel memories, loosely tethered.**
  The graph is used for *structured/epic* work, not stamped onto every chore — which
  is, to be fair, exactly how dependency graphs get used in every real tracker.

Verdict on the empirical test: the graph is genuinely load-bearing for the
project's own non-trivial work, and the recall feature is absent from it
entirely. Falsifier ("the graph is decorative even to its makers") does not hold;
over-claim ("the makers live in a dense graph for everything") is not supported
either. The truth is the boring-correct middle, and it still puts dispatch, not
memory, at the center.

## 6. The payoff: this *is* the Gastown finding

Name the product as a verb — *dispatch ready work to an agent* — and the previous
essay's conclusion stops being a separate observation and becomes a corollary.

Dispatch is the **first half of orchestration.** "Decide what is workable and who
may take it" is precisely what an orchestrator needs before it can "actually run
the agent." Beads' core already does the first half. Gastown is the second half.
A system whose core function is dispatch is not *near* an orchestrator — it is a
proto-orchestrator with the run-step missing.

This re-explains everything the charter essay attributed to authorship, from a
deeper cause. Orchestration falls into core not only because one hand owns both
sides of the fence (true), but because **the core's own verb is the gravity
well.** `bd ready` is the thing gates, molecules, and wisps all reach for, because
each of them is an attempt to make the dispatch a little more capable — a little
more like running the agent. Even a perfectly independent core-owner, with veto
power and no incentive to ship Gastown features, would feel this pull, because it
is structural to what `bd ready` *is*. The charter forbade orchestration in a
product whose core function is the front half of orchestration. The boundary was
not undefended; it was **undefendable by construction.**

So the two questions — "is the graph the product?" and "is beads becoming
Gastown?" — have one answer. The product is a work-dispatcher. A work-dispatcher
is an orchestrator with one step removed. Beads is becoming Gastown because beads,
correctly understood, was *already* the inner loop of Gastown the day `bd ready`
became the spine.

---

## The principle to carry out of this

> **A product is a verb, not a noun. To find it, ignore the README's nouns and
> locate the single computation with the highest investment × fan-in — the thing
> everything else is built to feed or consume. That is what you have made,
> whatever you call it. And when a system markets its container ("a graph," "a
> memory") but never its computation ("it dispatches"), it will systematically
> misdescribe — and under-sell — the engine it actually built.**

Two corollaries earned here, both transferable past beads:

- **Homonyms at the center of a product story cannibalize meaning.** When a
  system metaphor and a minor feature share a word ("memory" the soul, "memory"
  the sticky-note), no observer can tell which one you mean — including your own
  analysts. Name the small thing something small.
- **If your product's verb is "dispatch," you have built a proto-orchestrator,
  and no boundary document will keep orchestration out** — because orchestration
  is just your verb with a second step bolted on. The pull is structural, not a
  discipline failure, and the only honest responses are to own the verb or to
  cleave it deliberately. Forbidding it is forbidding the thing you are.

So — *is the graph the product?* The graph is the skeleton; the product is the
verb that runs on it; "memory" is the costume **and** an unrelated 310-line
feature wearing the costume's name. Beads is a readiness dispatcher for agent
work that calls itself a memory because "memory for your coding agent" is the
easier sentence — and because the dispatcher it actually is points straight at
the orchestration layer it has promised, in writing, never to become.
