# Web-UI Branch Agent Instructions

This document provides guidance for agents working on the web-ui branch of beads. The web-ui branch is a **downstream fork** of the main beads project, maintaining its own issue database while tracking upstream changes.

## Quick Start

The web-ui branch uses a separate beads database (`webui.db`) with the `webui-` prefix. All commands must specify the database:

```bash
# View ready web-ui work
bd ready --db .beads/webui.db --json

# Create a web-ui issue
bd create "Add dark mode toggle" -t feature -p 2 --db .beads/webui.db --json

# Update web-ui issue status
bd update webui-5 --status in_progress --db .beads/webui.db --json
```

**Tip**: Set `BEADS_DB=.beads/webui.db` in your environment to avoid typing `--db` every time:
```bash
export BEADS_DB=.beads/webui.db
bd ready --json  # Now uses webui.db automatically
```

## Database Configuration

### Location and Prefix

- **Database file**: `.beads/webui.db`
- **Issue prefix**: `webui-` (e.g., `webui-1`, `webui-42`)
- **Main project database**: `.beads/issues.db` (prefix: `bd-`)

### Why Separate Databases?

The web-ui branch maintains independent issue tracking because:
1. **Downstream fork pattern** - Web-ui is not yet merged into main
2. **Independent development** - Web-ui has its own roadmap and priorities
3. **Upstream tracking** - We import main project issues as needed without polluting our namespace
4. **Clean separation** - Prevents ID collisions and makes it clear which issues are web-ui specific

### Environment Variable Setup

For convenience, set the database in your shell profile:

```bash
# Add to ~/.bashrc, ~/.zshrc, or equivalent
export BEADS_DB=.beads/webui.db
```

Then all `bd` commands automatically use the web-ui database:

```bash
bd ready --json          # Uses webui.db
bd create "New task" -p 1 --json
bd update webui-3 --status closed --json
```

To temporarily use the main database:
```bash
bd ready --db .beads/issues.db --json
```

## Common Commands with --db Flag

### View and Filter

```bash
# List all web-ui issues
bd list --db .beads/webui.db --json

# Show ready work (no blockers)
bd ready --db .beads/webui.db --json

# Get issue details
bd show webui-5 --db .beads/webui.db --json

# Filter by label
bd list --label ui,frontend --db .beads/webui.db --json

# Show dependency tree
bd dep tree webui-1 --db .beads/webui.db
```

### Create and Update

```bash
# Create new web-ui issue
bd create "Implement responsive layout" -t feature -p 1 \
  -d "Make web-ui responsive on mobile devices" \
  --db .beads/webui.db --json

# Create with labels
bd create "Fix button styling" -t bug -p 2 \
  -l ui,css --db .beads/webui.db --json

# Update status
bd update webui-3 --status in_progress --db .beads/webui.db --json

# Close issue
bd close webui-7 --reason "Implemented and tested" --db .beads/webui.db --json
```

### Dependency Management

```bash
# Add dependency
bd dep add webui-5 webui-3 --type blocks --db .beads/webui.db --json

# Link to upstream issue (discovered-from)
bd dep add webui-8 bd-105 --type related --db .beads/webui.db --json

# Show dependency tree
bd dep tree webui-1 --db .beads/webui.db
```

### Labels

```bash
# Add label
bd label add webui-5 critical --db .beads/webui.db --json

# Remove label
bd label remove webui-5 wip --db .beads/webui.db --json

# List all labels
bd label list-all --db .beads/webui.db --json
```

## Working on Web-UI Issues

### Claiming a Task

1. Check ready work:
   ```bash
   bd ready --db .beads/webui.db --json
   ```

2. Claim the issue:
   ```bash
   bd update webui-3 --status in_progress --db .beads/webui.db --json
   ```

3. Work on implementation

### Discovering New Work

If you find bugs or TODOs while working on web-ui:

```bash
# Create discovered issue and link it
bd create "Fix button hover state" -t bug -p 2 \
  --deps discovered-from:webui-3 \
  --db .beads/webui.db --json
```

This creates a new issue and automatically links it as discovered during work on `webui-3`.

### Linking to Upstream Issues

When web-ui work relates to upstream issues, use `related` dependencies:

```bash
# Link web-ui issue to upstream issue
bd dep add webui-8 bd-105 --type related --db .beads/webui.db --json
```

This documents that `webui-8` is related to upstream `bd-105` (e.g., CWD propagation feature).

### Completing Work

When finished:

```bash
bd close webui-5 --reason "Implemented and tested" --db .beads/webui.db --json
```

## Tracking Upstream Changes

See [`.beads/UPSTREAM_SYNC.md`](.beads/UPSTREAM_SYNC.md) for detailed instructions on:
- Importing upstream issues
- Handling conflicts between upstream and web-ui work
- Keeping the branch aligned with main project

Quick reference:
```bash
# Import upstream issues (see UPSTREAM_SYNC.md for details)
bd import -i upstream-issues.jsonl --db .beads/webui.db --json
```

## Issue Types and Priorities

### Issue Types

- `bug` - Something broken in web-ui
- `feature` - New web-ui functionality
- `task` - Work item (tests, docs, refactoring)
- `epic` - Large feature composed of multiple web-ui issues
- `chore` - Maintenance work (dependencies, tooling)

### Priorities

- `0` - Critical (security, data loss, broken builds)
- `1` - High (major features, important bugs)
- `2` - Medium (nice-to-have features, minor bugs)
- `3` - Low (polish, optimization)
- `4` - Backlog (future ideas)

## Troubleshooting

### "Database not found" error

**Problem**: Command fails with "database not found"

**Solution**: Ensure the database exists:
```bash
# Initialize web-ui database if needed
bd init --db .beads/webui.db --prefix webui
```

### Wrong database being used

**Problem**: Commands are using the main database instead of web-ui

**Solution**: Check your environment:
```bash
# Verify BEADS_DB is set
echo $BEADS_DB

# Or explicitly specify --db flag
bd ready --db .beads/webui.db --json
```

### Issue IDs don't match expected prefix

**Problem**: Issues are created with wrong prefix (e.g., `bd-1` instead of `webui-1`)

**Solution**: Verify you're using the correct database:
```bash
# Check which database is active
bd show webui-1 --db .beads/webui.db --json

# If creating new issues, ensure --db flag is set
bd create "New task" -p 1 --db .beads/webui.db --json
```

### Collision detection during import

**Problem**: Import shows collisions when syncing upstream

**Solution**: See [`.beads/UPSTREAM_SYNC.md`](.beads/UPSTREAM_SYNC.md) for collision handling strategies.

## Pro Tips

- **Always use `--json`** for programmatic use and agent workflows
- **Set `BEADS_DB` environment variable** to avoid typing `--db` repeatedly
- **Use `discovered-from` dependencies** to track issues found during web-ui work
- **Link to upstream with `related` dependencies** when web-ui work relates to main project
- **Check `bd ready`** before asking "what should I work on?"
- **Use `bd dep tree`** to understand complex dependencies between web-ui issues
- **Priority 0-1 issues** are usually more important than 2-4

## Related Documentation

- [`.beads/WEBUI_SETUP.md`](.beads/WEBUI_SETUP.md) - Setup and configuration guide
- [`.beads/UPSTREAM_SYNC.md`](.beads/UPSTREAM_SYNC.md) - Upstream tracking workflow
- [`.beads/WEBUI_WORKFLOW.md`](.beads/WEBUI_WORKFLOW.md) - Development workflow patterns
- [`AGENTS.md`](../AGENTS.md) - Main beads agent instructions (upstream project)

## Questions?

- Check existing web-ui issues: `bd list --db .beads/webui.db --json`
- Review upstream issues: `bd list --db .beads/issues.db --json`
- Read the setup guide: [`.beads/WEBUI_SETUP.md`](.beads/WEBUI_SETUP.md)
- Create an issue if unsure: `bd create "Question: ..." -t task -p 2 --db .beads/webui.db --json`
