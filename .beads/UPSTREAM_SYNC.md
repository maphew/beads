# Upstream Sync Workflow

This document describes how to track and import changes from the main beads project while developing the web-ui branch independently.

## Overview

The web-ui branch is a **downstream fork** of the main beads project. We maintain our own separate issue database (`webui.db`) while periodically syncing with upstream changes from the main project (`issues.jsonl`).

**Key principles:**
- Web-ui issues use the `webui-` prefix (e.g., `webui-1`, `webui-42`)
- Main project issues use the `bd-` prefix (e.g., `bd-105`, `bd-200`)
- Upstream changes are imported as separate issues, not merged into existing ones
- Dependencies link web-ui work to upstream issues when relevant
- The main project is not interested in web-ui features yet, so we don't push changes upstream

## Periodic Upstream Sync

### Step 1: Export Main Project Issues

From the main beads project directory:

```bash
# Export all issues to JSONL format
bd export -o upstream-issues.jsonl

# Or use the git-tracked version
cat .beads/issues.jsonl > upstream-issues.jsonl
```

### Step 2: Review Upstream Changes

Before importing, review what's new:

```bash
# Check what issues are in the upstream export
grep '"id":"bd-' upstream-issues.jsonl | wc -l

# Look for specific issue types or priorities
grep '"priority":0' upstream-issues.jsonl  # Critical issues
grep '"issue_type":"bug"' upstream-issues.jsonl  # Bug fixes
```

### Step 3: Import Upstream Issues

Switch to the web-ui branch and import:

```bash
# Use the --db flag to target the web-ui database
bd import -i upstream-issues.jsonl --db .beads/webui.db --dry-run

# Review the import preview
# Then apply the import
bd import -i upstream-issues.jsonl --db .beads/webui.db
```

**Important**: The `--db` flag ensures issues are imported into `webui.db`, not the default database.

### Step 4: Verify Import

```bash
# Check that upstream issues were imported
bd list --db .beads/webui.db | grep "^bd-"

# Count imported issues
bd list --db .beads/webui.db --json | grep '"id":"bd-' | wc -l

# View specific upstream issue
bd show bd-105 --db .beads/webui.db
```

## Handling Conflicts

### Collision Detection

If the same issue ID exists in both databases with different content:

```bash
# Detect collisions before importing
bd import -i upstream-issues.jsonl --db .beads/webui.db --dry-run

# Output shows:
# === Collision Detection Report ===
# Exact matches (idempotent): 15
# New issues: 5
# COLLISIONS DETECTED: 0
```

### Resolving Collisions

If collisions occur (rare, since upstream uses `bd-` prefix):

```bash
# Option 1: Auto-resolve by remapping incoming issues
bd import -i upstream-issues.jsonl --db .beads/webui.db --resolve-collisions

# Option 2: Manual resolution - edit upstream-issues.jsonl to rename conflicting IDs
# Then import normally
bd import -i upstream-issues.jsonl --db .beads/webui.db
```

## Linking Web-UI Work to Upstream Issues

When web-ui development depends on or relates to upstream work, create dependencies:

### Example: Tracking Upstream Feature

Upstream issue `bd-105` (CWD propagation) is needed for web-ui feature `webui-3`:

```bash
# Create dependency: webui-3 is blocked by bd-105
bd dep add webui-3 bd-105 --type blocks --db .beads/webui.db

# Or when creating the issue:
bd create "Web-ui feature X" -t feature -p 1 \
  --deps blocks:bd-105 \
  --db .beads/webui.db
```

### Example: Tracking Discovered Upstream Issues

While working on web-ui, you discover a bug in the main project:

```bash
# Create a reference to the upstream issue
bd create "Upstream bug: Fix auth in main project" \
  -t bug -p 1 \
  -d "Found during web-ui development. See bd-200 in main project." \
  --db .beads/webui.db

# Link it as discovered-from your current work
bd dep add <new-id> webui-5 --type discovered-from --db .beads/webui.db
```

## Git Workflow for Sync

### Before Pulling Upstream Changes

```bash
# Ensure web-ui database is up-to-date
bd export --db .beads/webui.db -o .beads/webui.jsonl

# Commit web-ui changes
git add .beads/webui.jsonl
git commit -m "Update web-ui issues"
```

### After Pulling Upstream Changes

```bash
# Fetch latest from main project
git fetch origin main

# Export upstream issues
git show origin/main:.beads/issues.jsonl > upstream-issues.jsonl

# Import into web-ui database
bd import -i upstream-issues.jsonl --db .beads/webui.db

# Verify no conflicts
bd list --db .beads/webui.db | head -20
```

### Handling Upstream Merges

If the main project merges changes that affect web-ui:

```bash
# 1. Export current web-ui state
bd export --db .beads/webui.db -o .beads/webui.jsonl

# 2. Merge main branch
git merge origin/main

# 3. Handle any conflicts in .beads/issues.jsonl (main project issues)
# 4. Re-import upstream issues
git show HEAD:.beads/issues.jsonl > upstream-issues.jsonl
bd import -i upstream-issues.jsonl --db .beads/webui.db --dry-run

# 5. If no conflicts, apply import
bd import -i upstream-issues.jsonl --db .beads/webui.db

# 6. Commit merged state
git add .beads/
git commit -m "Merge upstream changes and sync web-ui issues"
```

## Dependency Patterns

### Pattern 1: Web-UI Blocked by Upstream

Web-ui feature cannot proceed until upstream issue is resolved:

```bash
# webui-3 is blocked by bd-105
bd dep add webui-3 bd-105 --type blocks --db .beads/webui.db

# webui-3 will not appear in ready work until bd-105 is closed
bd ready --db .beads/webui.db
```

### Pattern 2: Web-UI Related to Upstream

Web-ui work is related to but not blocked by upstream issue:

```bash
# webui-4 is related to bd-200
bd dep add webui-4 bd-200 --type related --db .beads/webui.db

# Both issues appear in ready work, but webui-4 is not blocked
bd ready --db .beads/webui.db
```

### Pattern 3: Discovered Upstream Issue

During web-ui work, you discover an upstream bug:

```bash
# Create reference to upstream issue
bd create "Upstream: Fix bug in main project" \
  -t bug -p 1 \
  -d "Discovered while working on webui-5" \
  --db .beads/webui.db

# Link as discovered-from
bd dep add <new-id> webui-5 --type discovered-from --db .beads/webui.db

# View discovery chain
bd dep tree webui-5 --db .beads/webui.db
```

## Sync Schedule

**Recommended sync frequency:**
- **Weekly**: Check for critical upstream issues (`priority: 0`)
- **Bi-weekly**: Full sync of all upstream changes
- **Before major releases**: Comprehensive sync to catch all upstream improvements

### Automated Sync (Optional)

Create a script to automate periodic syncs:

```bash
#!/bin/bash
# sync-upstream.sh

set -e

UPSTREAM_DIR="../beads"  # Path to main beads project
WEBUI_DB=".beads/webui.db"

echo "Exporting upstream issues..."
cd "$UPSTREAM_DIR"
bd export -o issues-export.jsonl

echo "Importing into web-ui database..."
cd -
bd import -i "$UPSTREAM_DIR/issues-export.jsonl" --db "$WEBUI_DB" --dry-run

read -p "Apply import? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
  bd import -i "$UPSTREAM_DIR/issues-export.jsonl" --db "$WEBUI_DB"
  echo "Sync complete!"
fi
```

Run with: `bash sync-upstream.sh`

## Troubleshooting

### Issue: "database is locked"

```bash
# The daemon may be holding a lock
bd daemon stop

# Try import again
bd import -i upstream-issues.jsonl --db .beads/webui.db

# Restart daemon
bd daemon --global
```

### Issue: Upstream issues not appearing

```bash
# Verify import succeeded
bd list --db .beads/webui.db --json | grep '"id":"bd-' | head -5

# Check for import errors
bd import -i upstream-issues.jsonl --db .beads/webui.db --dry-run

# Verify file format
head -1 upstream-issues.jsonl  # Should be valid JSON
```

### Issue: Dependency links broken after sync

```bash
# Verify upstream issue exists
bd show bd-105 --db .beads/webui.db

# Check dependency
bd dep tree webui-3 --db .beads/webui.db

# If broken, recreate dependency
bd dep add webui-3 bd-105 --type blocks --db .beads/webui.db
```

## Best Practices

1. **Always use `--db .beads/webui.db`** when working with web-ui issues
2. **Export before merging** to preserve web-ui state
3. **Review upstream changes** before importing to understand impact
4. **Link dependencies** when web-ui work relates to upstream issues
5. **Keep JSONL files in git** for audit trail and collaboration
6. **Document discovered issues** with clear references to upstream
7. **Test imports with `--dry-run`** before applying changes

## See Also

- [`WEBUI_SETUP.md`](.beads/WEBUI_SETUP.md) - Configuration and database setup
- [`WEBUI_WORKFLOW.md`](.beads/WEBUI_WORKFLOW.md) - Development workflow for web-ui
- [`AGENTS.md`](../AGENTS.md) - General beads workflow and patterns
- Main project [`AGENTS.md`](../AGENTS.md) - Reference for upstream patterns
