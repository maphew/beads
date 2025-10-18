# Web-UI Branch Agent Instructions

This document covers **only the differences** from the main beads project ([`AGENTS.md`](../AGENTS.md)). The web-ui branch is a downstream fork maintaining its own issue database while tracking upstream changes.

## Key Differences from Upstream

### Database & Prefix
- **Database**: `.beads/webui.db` (vs `.beads/issues.db` upstream)
- **Issue prefix**: `webui-` (vs `bd-` upstream)
- **Environment variable**: Set `BEADS_DB=.beads/webui.db` to avoid `--db` flags

### All Commands Require Database Specification
```bash
# Always specify --db or set BEADS_DB=.beads/webui.db
bd ready --db .beads/webui.db --json
bd create "New task" -p 1 --db .beads/webui.db --json
bd update webui-3 --status in_progress --db .beads/webui.db --json
```

### Upstream Issue Integration
- Import upstream issues with `bd import -i upstream-issues.jsonl --db .beads/webui.db`
- Link web-ui issues to upstream with `related` dependencies: `bd dep add webui-X bd-Y --type related --db .beads/webui.db`
- Handle collisions during import with `--resolve-collisions`

### Workflow Differences
- **Separate ready queue**: `bd ready --db .beads/webui.db` shows only web-ui work
- **Cross-database dependencies**: Use `related` type to link web-ui issues to upstream `bd-*` issues
- **Independent development**: Web-ui maintains its own roadmap and priorities

## Quick Reference

### Environment Setup
```bash
export BEADS_DB=.beads/webui.db  # Add to ~/.bashrc or ~/.zshrc
```

### Common Commands
```bash
# View ready work
bd ready --json

# Create issue (with BEADS_DB set)
bd create "Implement component" -t feature -p 1 --json

# Update status
bd update webui-5 --status in_progress --json

# Link to upstream issue
bd dep add webui-8 bd-105 --type related --json

# Import upstream changes
bd import -i upstream-issues.jsonl --resolve-collisions --json
```

## Troubleshooting

- **Wrong prefix**: Ensure `BEADS_DB=.beads/webui.db` or use `--db .beads/webui.db`
- **Database not found**: Run `bd init --db .beads/webui.db --prefix webui`
- **Import collisions**: Use `--resolve-collisions` flag

## Related Documentation

- [`AGENTS.md`](../AGENTS.md) - Full beads workflow (upstream project)
- [`.beads/UPSTREAM_SYNC.md`](.beads/UPSTREAM_SYNC.md) - Upstream tracking
- [`.beads/WEBUI_SETUP.md`](.beads/WEBUI_SETUP.md) - Setup guide
- [`.beads/WEBUI_WORKFLOW.md`](.beads/WEBUI_WORKFLOW.md) - Development patterns