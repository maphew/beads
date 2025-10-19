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
