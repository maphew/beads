# Web-UI Development Workflow

This guide explains how to work on web-ui issues, manage task progression, and handle discovered work during development.

## Quick Start

Before starting, ensure you have the web-ui database configured. See [`.beads/WEBUI_SETUP.md`](.beads/WEBUI_SETUP.md) for setup instructions.

## Claiming and Working on Tasks

### 1. Find Ready Work

List all unblocked web-ui tasks:

```bash
bd ready --db webui.db --json
```

This shows all `webui-*` issues with no open blockers.

### 2. Claim a Task

When you start work on an issue, mark it as in progress:

```bash
bd update webui-3 --status in_progress --db webui.db --json
```

This signals to other team members that you're actively working on it.

### 3. Work on the Issue

Implement, test, and document your changes. If you discover related work during development, create discovered issues (see below).

### 4. Complete the Task

When finished, close the issue with a reason:

```bash
bd close webui-3 --reason "Implemented web-ui dashboard component" --db webui.db --json
```

## Creating Discovered Issues

When you find bugs, TODOs, or new work during development, create discovered issues to track them.

### Discovered Issues in Web-UI

Create a discovered issue linked to your current task:

```bash
bd create "Fix styling inconsistency in dashboard" \
  -t bug \
  -p 2 \
  -d "Found misaligned buttons in the dashboard component" \
  --deps discovered-from:webui-3 \
  --db webui.db \
  --json
```

This creates a new issue (e.g., `webui-8`) with a `discovered-from` dependency linking it to `webui-3`.

### Discovered Issues Linked to Upstream

If you discover work that relates to the main beads project, create a discovered issue and link it to the upstream issue:

```bash
# Create discovered issue in web-ui
bd create "Web-UI needs CWD propagation feature" \
  -t feature \
  -p 1 \
  -d "Requires upstream feature from bd-105 (CWD propagation)" \
  --deps discovered-from:webui-2 \
  --db webui.db \
  --json
```

Then link it to the upstream issue:

```bash
# Link to upstream issue (use main database)
bd dep add webui-5 bd-105 --type related --json
```

This creates a `related` dependency showing that `webui-5` is connected to upstream `bd-105`.

## Status Progression

Web-ui issues follow this typical progression:

```
open → in_progress → closed
```

### Status Meanings

- **open** - Ready to work on or waiting for dependencies
- **in_progress** - Currently being worked on
- **closed** - Completed and merged

### Blocking Dependencies

If your web-ui task is blocked by upstream work, create a `blocks` dependency:

```bash
# Create a blocking dependency
bd dep add bd-105 webui-3 --type blocks --db webui.db --json
```

This marks `webui-3` as blocked until `bd-105` is completed. The issue won't appear in `bd ready` until the blocker is resolved.

## Working with Upstream Issues

### Tracking Upstream Changes

When the main project releases a feature you need, import it as a related issue:

```bash
# Create a web-ui issue tracking the upstream feature
bd create "Integrate upstream CWD propagation (bd-105)" \
  -t task \
  -p 1 \
  -d "Integrate the CWD propagation feature from main project" \
  --db webui.db \
  --json
```

Then link it to the upstream issue:

```bash
bd dep add webui-6 bd-105 --type related --json
```

### When Upstream Blocks Web-UI

If web-ui development is blocked waiting for an upstream feature:

```bash
# Create a blocking dependency from upstream to web-ui
bd dep add bd-105 webui-3 --type blocks --db webui.db --json
```

Now `webui-3` won't appear in ready work until `bd-105` is closed.

## Example Workflow

Here's a complete example of working on a web-ui feature:

```bash
# 1. Check ready work
bd ready --db webui.db --json

# 2. Claim webui-3 (Dashboard component)
bd update webui-3 --status in_progress --db webui.db --json

# 3. During development, discover a styling bug
bd create "Fix button alignment in dashboard" \
  -t bug \
  -p 2 \
  --deps discovered-from:webui-3 \
  --db webui.db \
  --json
# Returns: webui-8

# 4. Discover that you need upstream feature bd-105
bd create "Integrate CWD propagation from upstream" \
  -t task \
  -p 1 \
  --deps discovered-from:webui-3 \
  --db webui.db \
  --json
# Returns: webui-9

# 5. Link webui-9 to upstream issue
bd dep add webui-9 bd-105 --type related --json

# 6. Mark webui-3 as blocked by upstream
bd dep add bd-105 webui-3 --type blocks --db webui.db --json

# 7. Complete the styling bug fix
bd close webui-8 --reason "Fixed button alignment" --db webui.db --json

# 8. When upstream feature is ready, update webui-9
bd update webui-9 --status in_progress --db webui.db --json

# 9. Complete webui-9 after integrating upstream feature
bd close webui-9 --reason "Integrated CWD propagation" --db webui.db --json

# 10. Now webui-3 is unblocked, complete it
bd update webui-3 --status in_progress --db webui.db --json
bd close webui-3 --reason "Dashboard component complete" --db webui.db --json
```

## Labels for Organization

Use labels to organize web-ui work by component or priority:

```bash
# Add labels to an issue
bd label add webui-3 dashboard,ui --db webui.db --json

# Filter by label
bd list --label dashboard --db webui.db --json

# View all labels
bd label list-all --db webui.db --json
```

Common web-ui labels:
- `dashboard` - Dashboard component work
- `ui` - User interface improvements
- `integration` - Integration with upstream features
- `bug` - Bug fixes
- `performance` - Performance improvements
- `documentation` - Documentation updates

## Dependency Tree

View the full dependency tree for a web-ui issue:

```bash
bd dep tree webui-3 --db webui.db
```

This shows all blockers, related issues, and discovered work connected to `webui-3`.

## Troubleshooting

### Issue Not Appearing in Ready Work

If an issue isn't showing in `bd ready`, it's likely blocked:

```bash
# Check dependencies
bd show webui-3 --db webui.db --json

# View dependency tree
bd dep tree webui-3 --db webui.db
```

### Linking to Upstream Issues

When linking web-ui issues to upstream issues, use the main database for upstream:

```bash
# Link web-ui issue to upstream (cross-database)
bd dep add webui-5 bd-105 --type related --json
```

The `--db` flag is only needed for web-ui operations. Upstream operations use the default database.

### Syncing with Upstream

See [`.beads/UPSTREAM_SYNC.md`](.beads/UPSTREAM_SYNC.md) for detailed instructions on importing upstream changes and handling conflicts.

## Best Practices

1. **Always use `--db webui.db`** when working on web-ui issues
2. **Link discovered work** with `discovered-from` to maintain context
3. **Use `related` dependencies** to connect web-ui work to upstream issues
4. **Create blocking dependencies** when web-ui is waiting for upstream features
5. **Check `bd ready`** before asking "what should I work on next?"
6. **Close issues promptly** to keep the database clean
7. **Use labels** to organize work by component or priority
8. **Reference upstream issues** in descriptions when relevant (e.g., "Requires bd-105")

## See Also

- [`.beads/WEBUI_SETUP.md`](.beads/WEBUI_SETUP.md) - Setup and configuration
- [`.beads/UPSTREAM_SYNC.md`](.beads/UPSTREAM_SYNC.md) - Upstream tracking workflow
- [`AGENTS.md`](../AGENTS.md) - General beads workflow and commands
