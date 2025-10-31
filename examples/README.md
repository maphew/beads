# Beads Examples

This directory contains examples of how to integrate bd with AI agents and workflows.

## Examples

- **[python-agent/](python-agent/)** - Simple Python agent that discovers ready work and completes tasks
- **[bash-agent/](bash-agent/)** - Bash script showing the full agent workflow
- **[markdown-to-jsonl/](markdown-to-jsonl/)** - Convert markdown planning docs to bd issues
- **[github-import/](github-import/)** - Import issues from GitHub repositories
- **[git-hooks/](git-hooks/)** - Pre-configured git hooks for automatic export/import
<!-- REMOVED (bd-4c74): branch-merge example - collision resolution no longer needed with hash IDs -->
- **[claude-desktop-mcp/](claude-desktop-mcp/)** - MCP server for Claude Desktop integration
- **[claude-code-skill/](claude-code-skill/)** - Claude Code skill for effective beads usage patterns

## Quick Start

```bash
# Try the Python agent example
cd python-agent
python agent.py

# Try the bash agent example
cd bash-agent
./agent.sh

# Install git hooks
cd git-hooks
./install.sh

# REMOVED (bd-4c74): branch-merge demo - hash IDs eliminate collision resolution
```

## Creating Your Own Agent

The basic agent workflow:

1. **Find ready work**: `bd ready --json --limit 1`
2. **Claim the task**: `bd update <id> --status in_progress --json`
3. **Do the work**: Execute the task
4. **Discover new issues**: `bd create "Found bug" --json`
5. **Link discoveries**: `bd dep add <new-id> <parent-id> --type discovered-from`
6. **Complete the task**: `bd close <id> --reason "Done" --json`

All commands support `--json` for easy parsing.
