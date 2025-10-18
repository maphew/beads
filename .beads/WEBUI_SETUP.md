# Web-UI Branch Setup and Configuration

This guide explains how to set up and configure the web-ui branch for independent development while tracking upstream changes from the main beads project.

## Overview

The web-ui branch is a **downstream fork** of the main beads project. It maintains its own issue database (`webui.db`) with the `webui-` prefix, separate from the main project's `bd-` prefixed issues. This allows:

- **Independent development** - Web-ui work tracked separately from main project
- **Upstream tracking** - Periodically import and monitor main project changes
- **Clean separation** - No conflicts between web-ui and main project issues
- **Flexible workflow** - Work on web-ui features while staying aware of upstream progress

## Database Configuration

### File Locations

```
.beads/
├── issues.jsonl          # Main project issues (bd- prefix)
├── webui.db              # Web-ui branch database (webui- prefix)
└── webui.jsonl           # Web-ui branch export (auto-synced)
```

### Database Prefix

- **Main project**: `bd-` prefix (e.g., `bd-105`, `bd-42`)
- **Web-ui branch**: `webui-` prefix (e.g., `webui-1`, `webui-7`)

The prefix is set during initialization and determines the ID format for all issues created in that database.

## Using the --db Flag

The `--db` flag allows you to specify which database to use for a command. This is essential when working with the web-ui branch.

### Syntax

```bash
bd <command> --db <database-path> [options]
```

### Examples

```bash
# List web-ui issues
bd list --db .beads/webui.db

# Create a web-ui task
bd create "Implement dashboard component" -t feature -p 1 --db .beads/webui.db

# Check ready web-ui work
bd ready --db .beads/webui.db

# Update web-ui issue status
bd update webui-5 --status in_progress --db .beads/webui.db

# Show web-ui issue details
bd show webui-3 --db .beads/webui.db --json

# Close a web-ui task
bd close webui-2 --reason "Implemented" --db .beads/webui.db
```

## Environment Variable Setup

For convenience, you can set the `BEADS_DB` environment variable to avoid typing `--db` repeatedly.

### Bash/Zsh Setup

Add to your `.bashrc`, `.zshrc`, or shell profile:

```bash
# For web-ui branch work
export BEADS_DB=.beads/webui.db

# Or for main project work
export BEADS_DB=.beads/issues.db
```

### Using Environment Variables

Once set, commands automatically use the specified database:

```bash
# With BEADS_DB=.beads/webui.db
bd ready                    # Uses web-ui database
bd create "New task" -p 1   # Creates in web-ui database
bd list                     # Lists web-ui issues
```

### Switching Databases

```bash
# Switch to web-ui database
export BEADS_DB=.beads/webui.db
bd ready

# Switch back to main project
export BEADS_DB=.beads/issues.db
bd ready

# Or use --db flag to override
bd ready --db .beads/issues.db
```

## Initialization

If the web-ui database doesn't exist, initialize it:

```bash
# Initialize web-ui database with webui- prefix
bd init --db .beads/webui.db --prefix webui

# Verify initialization
bd stats --db .beads/webui.db
```

## Troubleshooting

### Issue: "database not found" error

**Solution**: Ensure the database file exists or initialize it:
```bash
bd init --db .beads/webui.db --prefix webui
```

### Issue: Issues appearing in wrong database

**Solution**: Always verify you're using the correct `--db` flag or `BEADS_DB` environment variable:
```bash
# Check current database setting
echo $BEADS_DB

# Explicitly specify database
bd list --db .beads/webui.db
```

### Issue: Can't find web-ui issue after creating it

**Solution**: Verify the issue was created in the correct database:
```bash
# List all web-ui issues
bd list --db .beads/webui.db --json

# Search for specific issue
bd show webui-5 --db .beads/webui.db --json
```

### Issue: Prefix mismatch (e.g., bd- instead of webui-)

**Solution**: Ensure you're using the correct database when creating issues:
```bash
# Wrong - creates bd- prefix issue
bd create "Task" -p 1

# Correct - creates webui- prefix issue
bd create "Task" -p 1 --db .beads/webui.db
```

## Best Practices

1. **Always specify --db flag** when working with web-ui issues to avoid confusion
2. **Use environment variables** for your primary database to reduce typing
3. **Keep databases separate** - don't mix web-ui and main project issues
4. **Export regularly** - web-ui issues auto-sync to `.beads/webui.jsonl`
5. **Verify database** before running commands - check `echo $BEADS_DB` or use `--db` explicitly
6. **Use labels** to categorize web-ui work (e.g., `ui`, `backend`, `integration`)
7. **Link to upstream** when web-ui work relates to main project issues using `related` dependencies

## Next Steps

- Read [`UPSTREAM_SYNC.md`](.beads/UPSTREAM_SYNC.md) to learn how to track and import main project changes
- Read [`WEBUI_WORKFLOW.md`](.beads/WEBUI_WORKFLOW.md) for development workflow patterns
- Check [`AGENTS.md`](../AGENTS.md) for general beads workflow and commands
