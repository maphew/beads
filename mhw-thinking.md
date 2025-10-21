09:30 2025-10-18

## prompt
@/AGENTS.md - initial context. Our current state: we're downstream of the main project. The web-ui branch is our current endeavour. Main project is not interested in this feature at this time. So we need to develop independently, but keep tracking main closely.

@/.beads/issues.jsonl is upstream issue tracking an project management. It's their source of truth. We need to follow this pattern for our own issues and management.

Does this make sense to you?

## result

work happened, commit 6111a4a66172e55fc6e2e8f5be702169696488d7 to b3231601d25acf5f1081fb0841c167de4fd08fb1

-----
11:35 2025-10-18

Regular usage:

```
# Work on web-ui issues
export BEADS_DB=.beads/webui.db
bd ready
bd update webui-2 --status in_progress
# ...start working on issue #2 
```

## prompt:

if I ask you to "come up to speed on project, and start issue 2" what will you do?
(i want to understand your default actions, so i only ask for things that are extra. it just wastes time and context if i ask for things you already do as a matter of course)

...snip...

## prompt

come up to speed on project, and start issue 2.
webui docs are @.beads/WEBUI* and @/.beads/UPSTREAM_SYNC.md . 
upstream or main docs are all other .md in repo. (mostly root).

...snip...

## prompt

take another pass at issue #9 - Remove duplication in webui docs (only describe what's different from upstream (main)). the webui docs still seem overly long to me

...snip...

proved that asking Grok to do the same job twice can yield a better result!


----
22:36 2025-10-18

**Amp code**

prompt:

@.beads/issues.jsonl contents from Main are repeatedly being mass deleted and replaced with Webui issues. I think this means webui misunderstands multiple db setup and how to use.

To see:
```
git diff --unified=20 .beads/issues.jsonl | head -100
```

...snip...


**Amp:** For future sessions, you can simply say: "If you discover any significant issues or incidents, create a beads issue documenting the problem, root cause, and resolution - just like we did with the database contamination issue (bd-156, 2025-10-18)."

-----
13:30 2025-10-19

**prompt:**
we need split web-ui into an independent program from bd. Upstream wants bd to stay focussed on CLI and agents as primary users.
currently the prototype is a branch from the main repo (web-ui).
package the prototype into it's own isolated program, that we can use as initial checkin to a new repo.

**enhanced:**
We need to split the web-ui into an independent program separate from bd. Upstream requirements mandate that bd remains focused on CLI and agents as primary users. Currently, the web-ui prototype exists as a branch from the main repository. Your task is to package this prototype into a completely isolated standalone program that can serve as the initial commit for a new dedicated repository, ensuring all dependencies, configurations, and build processes are self-contained and properly decoupled from the original bd codebase.

**enhanced 2x**
We need to split the web-ui into an independent program separate from bd. Upstream requirements mandate that bd remains focused on CLI and agents as primary users. Currently, the web-ui prototype exists as a branch from the main repository. Your task is to package this prototype into a completely isolated standalone program that can serve as the initial commit for a new dedicated repository, ensuring all dependencies, configurations, and build processes are self-contained and properly decoupled from the original bd codebase.

Verify that the new repository can be cloned, built, and run independently without requiring access to the original bd repository or its dependencies. Ensure all documentation is updated to reflect the standalone nature of the application, including setup instructions, development guidelines, and contribution workflows. The final deliverable should be a clean repository structure ready for immediate publication with a complete commit history starting from this initial standalone version.

context:
@/.beads/WEBUI_SETUP.md 
@/.beads/WEBUI_AGENTS.md 
@/.beads/WEBUI_WORKFLOW.md 
@/.beads/UPSTREAM_SYNC.md

.../Orchestrator
.../Model Qwen3 Coder 480B A35B
...work happens...

-----
06:50 2025-10-21

Qwen finished making stand-alone webui ($3.30), but created it as git repo inside a git repo. I'm not sure if that's the right thing to do or not. I was thinking I'd continue with webui project as long lived fork, but am now questioning the wisdom of that. Maybe better to make a complete break, and only use the public beads api. Yeah, let's do that.
