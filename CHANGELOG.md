# Changelog

All notable changes to the beads project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Batch Deletion**: Enhanced `bd delete` command with batch operations (bd-127)
  - Delete multiple issues at once: `bd delete bd-1 bd-2 bd-3 --force`
  - Read from file: `bd delete --from-file deletions.txt --force`
  - Dry-run mode: `--dry-run` to preview deletions before execution
  - Cascade mode: `--cascade` to recursively delete all dependents
  - Force mode: `--force` to orphan dependents instead of failing
  - Atomic transactions: all deletions succeed or none do
  - Comprehensive statistics: tracks deleted issues, dependencies, labels, and events

### Fixed
- **Critical**: `bd list --status all` showing 0 issues (bd-148)
  - Status filter now treats "all" as special value meaning "show all statuses"
  - Previously treated "all" as literal status value, matching no issues

## [0.9.9] - 2025-10-17

### Added
- **Daemon RPC Architecture**: Production-ready RPC protocol for client-daemon communication (bd-110, bd-111, bd-112, bd-114, bd-117)
  - Unix socket-based RPC enables faster command execution via long-lived daemon process
  - Automatic client detection with graceful fallback to direct mode
  - Serializes SQLite writes and batches git operations to prevent concurrent access issues
  - Resolves database corruption, git lock contention, and ID counter conflicts with multiple agents
  - Comprehensive integration tests and stress testing with 4+ concurrent agents
- **Issue Deletion**: `bd delete` command for removing issues with comprehensive cleanup
  - Safely removes issues from database and JSONL export
  - Cleans up dependencies and references to deleted issues
  - Works correctly with git-based workflows
- **Issue Restoration**: `bd restore` command for recovering compacted/deleted issues
  - Restores issues from git history when needed
  - Preserves references and dependency relationships
- **Prefix Renaming**: `bd rename-prefix` command for batch ID prefix changes
  - Updates all issue IDs and text references throughout the database
  - Useful for project rebranding or namespace changes
- **Comprehensive Testing**: Added scripttest-based integration tests (#59)
  - End-to-end coverage for CLI workflows
  - Tests for init command edge cases (bd-70)

### Fixed
- **Critical**: Metadata errors causing crashes on first import (bd-663)
  - Auto-import now treats missing metadata as first import instead of failing
  - Eliminates initialization errors in fresh repositories
- **Critical**: N+1 query pattern in auto-import (bd-666)
  - Replaced per-issue queries with batch fetching
  - Dramatically improves performance for large imports
- **Critical**: Duplicate issue imports (bd-421)
  - Added deduplication logic to prevent importing same issue multiple times
  - Maintains data integrity during repeated imports
- **Bug**: Auto-flush missing after renumber/rename-prefix (bd-346)
  - Commands now properly export to JSONL after completion
  - Ensures git sees latest changes immediately
- **Bug**: Renumber ID collision with UUID temp IDs (bd-345)
  - Uses proper UUID-based temporary IDs to prevent conflicts during renumbering
  - ID counter now correctly syncs after renumbering operations
- **Bug**: Collision resolution dependency handling (bd-437)
  - Uses unchecked dependency addition during collision remapping
  - Prevents spurious cycle detection errors
- **Bug**: macOS crashes documented (closes #3, bd-87)
  - Added CGO_ENABLED=1 workaround documentation for macOS builds

### Changed
- CLI commands now prefer RPC when daemon is running
  - Improved error reporting and diagnostics for RPC failures
  - More consistent exit codes and status messages
- Internal command architecture refactored for RPC client/server sharing
  - Reduced code duplication between direct and daemon modes
  - Improved reliability of background operations
- Ready work sort order flipped to show oldest issues first
  - Helps prioritize long-standing work items

### Performance
- Faster command execution through RPC-backed daemon (up to 10x improvement)
- N+1 query elimination in list/show operations
- Reduced write amplification from improved auto-flush behavior
- Cycle detection performance benchmarks added (bd-311)

### Testing
- Integration tests for daemon RPC request/response flows
- End-to-end coverage for delete/restore lifecycles  
- Regression tests for metadata handling, auto-flush, ID counter sync
- Comprehensive tests for collision detection in auto-import (bd-401)

### Documentation
- Release process documentation added (RELEASING.md)
- Multiple workstreams warning banner for development coordination

## [0.9.8] - 2025-10-16

### Added
- **Background Daemon Mode**: `bd daemon` command for continuous auto-sync (#bd-386)
  - Watches for changes and automatically exports to JSONL
  - Monitors git repository for incoming changes and auto-imports
  - Production-ready with graceful shutdown, PID file management, and signal handling
  - Eliminates manual export/import in active development workflows
- **Git Synchronization**: `bd sync` command for automated git workflows (#bd-378)
  - One-command sync: stage, commit, pull, push JSONL changes
  - Automatic merge conflict resolution with collision remapping
  - Status reporting shows sync progress and any issues
  - Ideal for distributed teams and CI/CD integration
- **Issue Compaction**: `bd compact` command to summarize old closed issues (bd-254-264)
  - AI-powered summarization using Claude Haiku
  - Reduces database size while preserving essential information
  - Configurable thresholds for age, dependencies, and references
  - Compaction status visible in `bd show` output
- **Label and Title Filtering**: Enhanced `bd list` command (#45, bd-269)
  - Filter by labels: `bd list --label bug,critical`
  - Filter by title: `bd list --title "auth"`
  - Combine with status/priority filters
- **List Output Formats**: `bd list --format` flag for custom output (PR #46)
  - Format options: `default`, `compact`, `detailed`, `json`
  - Better integration with scripts and automation tools
- **MCP Reopen Support**: Reopen closed issues via MCP server
  - Claude Desktop plugin can now reopen issues
  - Useful for revisiting completed work
- **Cross-Type Cycle Prevention**: Dependency cycles detected across all types (bd-312)
  - Prevents A→B→A cycles even when mixing `blocks`, `related`, etc.
  - Semantic validation for parent-child direction
  - Diagnostic warnings when cycles detected

### Fixed
- **Critical**: Auto-import collision skipping bug (bd-393, bd-228)
  - Import would silently skip collisions instead of remapping
  - Could cause data loss when merging branches
  - Now correctly applies collision resolution with remapping
- **Critical**: Transaction state corruption (bd-221)
  - Nested transactions could corrupt database state
  - Fixed with proper transaction boundary handling
- **Critical**: Concurrent temp file collisions (bd-306, bd-373)
  - Multiple `bd` processes would collide on shared `.tmp` filename
  - Now uses PID suffix for temp files: `.beads/issues.jsonl.tmp.12345`
- **Critical**: Circular dependency detection gaps (bd-307)
  - Some cycle patterns were missed by detection algorithm
  - Enhanced with comprehensive cycle prevention
- **Bug**: False positive merge conflict detection (bd-313, bd-270)
  - Auto-import would detect conflicts when none existed
  - Fixed with improved Git conflict marker detection
- **Bug**: Import timeout with large issue sets (bd-199)
  - 200+ issue imports would timeout
  - Optimized import performance
- **Bug**: Collision resolver missing ID counter sync (bd-331)
  - After remapping, ID counters weren't updated
  - Could cause duplicate IDs in subsequent creates
- **Bug**: NULL handling in statistics for empty databases (PR #37)
  - `bd stats` would crash on newly initialized databases
  - Fixed NULL value handling in GetStatistics

### Changed
- Compaction removes snapshot/restore (simplified to permanent decay)
- Export file writing refactored to avoid Windows Defender false positives (PR #31)
- Error handling improved in auto-import and fallback paths (PR #47)
- Reduced cyclomatic complexity in main.go (PR #48)
- MCP integration tests fixed and linting cleaned up (PR #40)

### Performance
- Cycle detection benchmarks added (bd-311)
- Import optimization for large issue sets
- Export uses PID-based temp files to avoid lock contention

### Community
- Merged PR #31: Windows Defender mitigation for export
- Merged PR #37: Fix NULL handling in statistics
- Merged PR #38: Nix flake for declarative builds
- Merged PR #40: MCP integration test fixes
- Merged PR #45: Label and title filtering for bd list
- Merged PR #46: Add --format flag to bd list
- Merged PR #47: Error handling consistency
- Merged PR #48: Cyclomatic complexity reduction

## [0.9.2] - 2025-10-14

### Added
- **One-Command Dependency Creation**: `--deps` flag for `bd create` (#18)
  - Create issues with dependencies in a single command
  - Format: `--deps type:id` or just `--deps id` (defaults to blocks)
  - Multiple dependencies: `--deps discovered-from:bd-20,blocks:bd-15`
  - Whitespace-tolerant parsing
  - Particularly useful for AI agents creating discovered-from issues
- **External Reference Tracking**: `external_ref` field for linking to external trackers
  - Link bd issues to GitHub, Jira, Linear, etc.
  - Example: `bd create "Issue" --external-ref gh-42`
  - `bd update` supports updating external references
  - Tracked in JSONL for git portability
- **Metadata Storage**: Internal metadata table for system state
  - Stores import hash for idempotent auto-import
  - Enables future extensibility for system preferences
  - Auto-migrates existing databases
- **Windows Support**: Complete Windows 11 build instructions (#10)
  - Tested with mingw-w64
  - Full CGo support documented
  - PATH setup instructions
- **Go Extension Example**: Complete working example of database extensions (#15)
  - Demonstrates custom table creation
  - Shows cross-layer queries joining with issues
  - Includes test suite and documentation
- **Issue Type Display**: `bd list` now shows issue type in output (#17)
  - Better visibility: `bd-1 [P1] [bug] open`
  - Helps distinguish bugs from features at a glance

### Fixed
- **Critical**: Dependency tree deduplication for diamond dependencies (bd-85, #1)
  - Fixed infinite recursion in complex dependency graphs
  - Prevents duplicate nodes at same level
  - Handles multiple blockers correctly
- **Critical**: Hash-based auto-import replaces mtime comparison (bd-84)
  - Git pull updates mtime but may not change content
  - Now uses SHA256 hash to detect actual changes
  - Prevents unnecessary imports after git operations
- **Critical**: Parallel issue creation race condition (PR #8, bd-66)
  - Multiple processes could generate same ID
  - Replaced in-memory counter with atomic database counter
  - Syncs counters after import to prevent collisions
  - Comprehensive test coverage

### Changed
- Auto-import now uses content hash instead of modification time
- Dependency tree visualization improved for complex graphs
- Better error messages for dependency operations

### Community
- Merged PR #8: Parallel issue creation fix
- Merged PR #10: Windows build instructions
- Merged PR #12: Fix quickstart EXTENDING.md link
- Merged PR #14: Better enable Go extensions
- Merged PR #15: Complete Go extension example
- Merged PR #17: Show issue type in list output

## [0.9.1] - 2025-10-14

### Added
- **Incremental JSONL Export**: Major performance optimization
  - Dirty issue tracking system to only export changed issues
  - Auto-flush with 5-second debounce after CRUD operations
  - Automatic import when JSONL is newer than database
  - `--no-auto-flush` and `--no-auto-import` flags for manual control
  - Comprehensive test coverage for auto-flush/import
- **ID Space Partitioning**: Explicit ID assignment for parallel workers
  - `bd create --id worker1-100` for controlling ID allocation
  - Enables multiple agents to work without conflicts
  - Documented in CLAUDE.md for agent workflows
- **Auto-Migration System**: Seamless database schema upgrades
  - Automatically adds dirty_issues table to existing databases
  - Silent migration on first access after upgrade
  - No manual intervention required

### Fixed
- **Critical**: Race condition in dirty tracking (TOCTOU bug)
  - Could cause data loss during concurrent operations
  - Fixed by tracking specific exported IDs instead of clearing all
- **Critical**: Export with filters cleared all dirty issues
  - Status/priority filters would incorrectly mark non-matching issues as clean
  - Now only clears issues that were actually exported
- **Bug**: Malformed ID detection never worked
  - SQLite CAST returns 0 for invalid strings, not NULL
  - Now correctly detects non-numeric ID suffixes like "bd-abc"
  - No false positives on legitimate zero-prefixed IDs
- **Bug**: Inconsistent dependency dirty marking
  - Duplicated 20+ lines of code in AddDependency/RemoveDependency
  - Refactored to use shared markIssuesDirtyTx() helper
- Fixed unchecked error in import.go when unmarshaling JSON
- Fixed unchecked error returns in test cleanup code
- Removed duplicate test code in dependencies_test.go
- Fixed Go version in go.mod (was incorrectly set to 1.25.2)

### Changed
- Export now tracks which specific issues were exported
- ClearDirtyIssuesByID() added (ClearDirtyIssues() deprecated with race warning)
- Dependency operations use shared dirty-marking helper (DRY)

### Performance
- Incremental export: Only writes changed issues (vs full export)
- Regex caching in ID replacement: 1.9x performance improvement
- Automatic debounced flush prevents excessive I/O

## [0.9.0] - 2025-10-12

### Added
- **Collision Resolution System**: Automatic ID remapping for import collisions
  - Reference scoring algorithm to minimize updates during remapping
  - Word-boundary regex matching to prevent false replacements
  - Automatic updating of text references and dependencies
  - `--resolve-collisions` flag for safe branch merging
  - `--dry-run` flag to preview collision detection
- **Export/Import with JSONL**: Git-friendly text format
  - Dependencies embedded in JSONL for complete portability
  - Idempotent import (exact matches detected)
  - Collision detection (same ID, different content)
- **Ready Work Algorithm**: Find issues with no open blockers
  - `bd ready` command shows unblocked work
  - `bd blocked` command shows what's waiting
- **Dependency Management**: Four dependency types
  - `blocks`: Hard blocker (affects ready work)
  - `related`: Soft relationship
  - `parent-child`: Epic/subtask hierarchy
  - `discovered-from`: Track issues discovered during work
- **Database Discovery**: Auto-find database in project hierarchy
  - Walks up directory tree like git
  - Supports `$BEADS_DB` environment variable
  - Falls back to `~/.beads/default.db`
- **Comprehensive Documentation**:
  - README.md with 900+ lines of examples and FAQs
  - CLAUDE.md for AI agent integration patterns
  - SECURITY.md with security policy and best practices
  - TEXT_FORMATS.md analyzing JSONL approach
  - EXTENDING.md for database extension patterns
  - GIT_WORKFLOW.md for git integration
- **Examples**: Real-world integration patterns
  - Python agent implementation
  - Bash agent script
  - Git hooks for automatic export/import
  - Branch merge workflow with collision resolution
  - Claude Desktop MCP integration (coming soon)

### Changed
- Switched to JSONL as source of truth (from binary SQLite)
- SQLite database now acts as ephemeral cache
- Issue IDs generated with numerical max (not alphabetical)
- Export sorts issues by ID for consistent git diffs

### Security
- SQL injection protection via allowlisted field names
- Input validation for all issue fields
- File path validation for database operations
- Warnings about not storing secrets in issues

## [0.1.0] - Initial Development

### Added
- Core issue tracking (create, update, list, show, close)
- SQLite storage backend
- Dependency tracking with cycle detection
- Label support
- Event audit trail
- Full-text search
- Statistics and reporting
- `bd init` for project initialization
- `bd quickstart` interactive tutorial

---

## Version History

- **0.9.8** (2025-10-16): Daemon mode, git sync, compaction, critical bug fixes
- **0.9.2** (2025-10-14): Community PRs, critical bug fixes, and --deps flag
- **0.9.1** (2025-10-14): Performance optimization and critical bug fixes
- **0.9.0** (2025-10-12): Pre-release polish and collision resolution
- **0.1.0**: Initial development version

## Upgrade Guide

### Upgrading to 0.9.8

No breaking changes. All changes are backward compatible:
- **bd daemon**: New optional background service for auto-sync workflows
- **bd sync**: New optional git integration command
- **bd compact**: New optional command for issue summarization (requires Anthropic API key)
- **--format flag**: Optional new feature for `bd list`
- **Label/title filters**: Optional new filters for `bd list`
- **Bug fixes**: All critical fixes are transparent to users

Simply pull the latest version and rebuild:
```bash
go install github.com/steveyegge/beads/cmd/bd@latest
# or
git pull && go build -o bd ./cmd/bd
```

**Note**: The `bd compact` command requires an Anthropic API key in `$ANTHROPIC_API_KEY` environment variable. All other features work without any additional setup.

### Upgrading to 0.9.2

No breaking changes. All changes are backward compatible:
- **--deps flag**: Optional new feature for `bd create`
- **external_ref**: Optional field, existing issues unaffected
- **Metadata table**: Auto-migrates on first use
- **Bug fixes**: All critical fixes are transparent to users

Simply pull the latest version and rebuild:
```bash
go install github.com/steveyegge/beads/cmd/bd@latest
# or
git pull && go build -o bd ./cmd/bd
```

### Upgrading to 0.9.1

No breaking changes. All changes are backward compatible:
- **Auto-migration**: The dirty_issues table is automatically added to existing databases
- **Auto-flush/import**: Enabled by default, improves workflow (can disable with flags if needed)
- **ID partitioning**: Optional feature, use `--id` flag only if needed for parallel workers

If you're upgrading from 0.9.0, simply pull the latest version. Your existing database will be automatically migrated on first use.

### Upgrading to 0.9.0

No breaking changes. The JSONL export format is backward compatible.

If you have issues in your database:
1. Run `bd export -o .beads/issues.jsonl` to create the text file
2. Commit `.beads/issues.jsonl` to git
3. Add `.beads/*.db` to `.gitignore`

New collaborators can clone the repo and run:
```bash
bd import -i .beads/issues.jsonl
```

The SQLite database will be automatically populated from the JSONL file.

## Future Releases

See open issues tagged with milestone markers for planned features in upcoming releases.

For version 1.0, see: `bd dep tree bd-8` (the 1.0 milestone epic)
