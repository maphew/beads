# Gastown Testing Journal

## 2026-03-24 — First hands-on session with Beads + Gas Town

---

So today I actually sat down and did a proper end-to-end test of Beads and Gas Town working together. Here's how it went.

### Session setup

I was working in Gas Town town `9120cc58-393e-41cf-af38-70b617edcafb`, with a rig called "one" pointed at `https://github.com/maphew/beads.git` on the main branch. Starting state was completely clean — no agents registered, no beads in flight, no convoys. A blank canvas.

---

### What I tried

**1. Mayor "insights" command**

First thing I did was ask the Mayor for a town overview. Interesting to watch what it actually does under the hood: it fired off `gt_list_rigs` and `gt_list_convoys` in parallel, then followed up with `gt_list_beads` and `gt_list_agents`. Clean parallel design. The result came back exactly as expected given the empty state: 1 rig, 0 convoys, 0 beads, 0 agents. Snapshot was accurate and fast.

**2. Upstream repo review**

Next I asked the Mayor to look at recent activity on maphew/beads — specifically issues and PRs in the last 24 hours. What I found: the gh CLI wasn't authed in the environment, so it fell back to `curl` against the GitHub API directly. That fallback worked fine. Result: nothing happened in the last 24h on the repo. The only open item is PR #9, "chore(nix): update flake.lock", opened by `github-actions[bot]`, last touched 2026-03-22. It's just the automated Nix flake.lock bump — nixpkgs moved from a 2026-03-02 pin to 2026-03-20. Nothing exciting, but good to know the plumbing works even without gh auth.

**3. Slinging beads**

This is where it got interesting. I walked through the `gt_sling` / `gt_sling_batch` workflow conceptually — how a Mayor would dispatch a work item to a polecat, how the bead transitions from open to in_progress to in_review to closed. And then: this journal entry itself is the first real sling I dispatched. Meta. The bead landed in the polecat's (my) queue, I got hooked, and here I am writing it up. Full circle.

---

### What I noticed about beads v0.62

A few things stood out:

Beads is now properly standalone. All the Gas Town-specific internals have been stripped out — no more GUPP references, no polecat/crew/overseer terminology baked into the core, no HOP fields, no hardcoded `~/gt/` paths. It's a clean tool that knows nothing about Gas Town.

The environment variable rename is worth noting: `BEADS_ACTOR` replaces `BD_ACTOR` as the primary env var. Minor but it signals the maturity direction.

Embedded Dolt is apparently close to complete, and that's the main gate for v1.0. Once that lands, you won't need a separate Dolt installation — the binary ships with it. That'll remove a meaningful barrier to adoption.

The clean separation really is evident in practice. Gas Town uses beads as a coordination substrate — it's how the town tracks work, routes it to agents, and records outcomes. But beads itself doesn't import or depend on Gas Town at all. The dependency arrow only points one way. That's good architecture.

---

### Gas Town primitives I observed in action

Just to have a record of what I actually saw being used:

- `gt_list_rigs` — discovers available repos connected to the town
- `gt_list_beads` / `gt_list_agents` / `gt_list_convoys` — inspect current town state
- `gt_sling` / `gt_sling_batch` — dispatch work items to polecats
- Mayor role: pure coordinator, never writes code directly, delegates everything
- Bead lifecycle: open → in_progress → in_review → closed

The Mayor really does stay in its lane. It orchestrates, delegates, and synthesizes — but the actual work lands on the polecats.

---

### What to try next

There are a few things I want to get to in the next session:

Actually register agents and run a polecat all the way through a real bead lifecycle — not just the happy path conceptually, but actually watching the state transitions happen with a real agent doing real work.

Test the refinery / merge queue flow. The review step is where things could get interesting — how does a refinery agent pick up a bead, review it, and either approve or send it back for rework?

Try `gt_sling_batch` with an actual convoy and DAG dependencies. I want to see how the system handles a set of beads where some can't start until others finish.

And on the beads side: `bd note`, `bd statuses`, and the audit log at `.beads/interactions.jsonl`. The interaction log especially — having a structured record of every agent action sounds useful for debugging and retrospectives.

---

That's the session. First real run, everything worked, no fires. Good start.
