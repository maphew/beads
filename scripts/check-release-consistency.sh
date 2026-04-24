#!/usr/bin/env bash
# check-release-consistency.sh - release-blocking docs and package drift checks.
#
# This complements check-versions.sh and check-doc-flags.sh with checks for
# cross-package release metadata, canonical repository URLs, and install snippets.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

CANONICAL_REPO="gastownhall/beads"
CANONICAL_GITHUB="https://github.com/${CANONICAL_REPO}"
GO_MODULE="github.com/steveyegge/beads"

fail=0

section() {
    echo ""
    echo "=== $1 ==="
}

pass() {
    echo "PASS: $1"
}

error() {
    echo "FAIL: $1" >&2
    fail=1
}

require_file_contains() {
    local file=$1
    local pattern=$2
    local description=$3

    if grep -Eq "$pattern" "$file"; then
        pass "$description"
    else
        error "$description"
        echo "  Missing pattern in $file: $pattern" >&2
    fi
}

require_json_value() {
    local file=$1
    local filter=$2
    local expected=$3
    local description=$4
    local actual

    if ! command -v jq >/dev/null 2>&1; then
        error "jq is required for JSON metadata checks"
        return
    fi

    actual="$(jq -r "$filter // empty" "$file")"
    if [[ "$actual" == "$expected" ]]; then
        pass "$description"
    else
        error "$description"
        echo "  $file $filter: '$actual' (expected '$expected')" >&2
    fi
}

section "Version consistency"
"$SCRIPT_DIR/check-versions.sh"

section "Canonical repository references"
mapfile -t public_files < <(
    git ls-files \
        README.md \
        CONTRIBUTING.md \
        RELEASING.md \
        NEWSLETTER.md \
        .goreleaser.yml \
        'docs/**' \
        'website/**' \
        'examples/**' \
        'integrations/**' \
        'npm-package/**' \
        'scripts/**' \
        'winget/**' \
    | grep -v '^scripts/check-release-consistency\.sh$'
)

old_repo_refs="$(
    git grep -n -E \
        'github\.com/steveyegge/beads|raw\.githubusercontent\.com/steveyegge/beads|github:steveyegge/beads|steveyegge/beads' \
        -- "${public_files[@]}" 2>/dev/null \
    | grep -Ev \
        'go install|go list|go get github\.com/steveyegge/beads|go:github\.com/steveyegge/beads|github\.com/steveyegge/beads/(cmd|pkg|internal|examples)|\.(go|md):[0-9]+:.*"github\.com/steveyegge/beads"|go\.mod:[0-9]+:.*(require|replace) github\.com/steveyegge/beads|/plugin marketplace|module path|module import|module ID|bd-example-extension-go|scripts/repro-dolt-hang|CHANGELOG|changelog|historical' \
    || true
)"
if [[ -n "$old_repo_refs" ]]; then
    error "public docs/package metadata contain non-canonical steveyegge/beads repository references"
    echo "$old_repo_refs" >&2
else
    pass "public repository URLs use ${CANONICAL_REPO}"
fi

bad_module_snippets="$(
    git grep -n -E \
        'go install github\.com/gastownhall/beads|go list .*github\.com/gastownhall/beads|go get github\.com/gastownhall/beads|go:github\.com/gastownhall/beads|github\.com/gastownhall/beads/(cmd|pkg|internal|examples)|"github\.com/gastownhall/beads"' \
        -- "${public_files[@]}" 2>/dev/null || true
)"
if [[ -n "$bad_module_snippets" ]]; then
    error "Go module install snippets were rewritten to the repository owner"
    echo "$bad_module_snippets" >&2
else
    pass "Go module install snippets keep ${GO_MODULE}"
fi

section "Install snippets and release endpoints"
require_file_contains README.md \
    'https://raw\.githubusercontent\.com/gastownhall/beads/main/scripts/install\.sh' \
    "README quick install script points at ${CANONICAL_REPO}"
require_file_contains docs/INSTALLING.md \
    'mise install github:gastownhall/beads' \
    "mise GitHub install points at ${CANONICAL_REPO}"
require_file_contains docs/INSTALLING.md \
    'go install github\.com/steveyegge/beads/cmd/bd@latest' \
    "go install docs keep the published module path"
require_file_contains scripts/install.sh \
    'https://api\.github\.com/repos/gastownhall/beads/releases/latest' \
    "install.sh queries canonical GitHub releases"
require_file_contains scripts/update-winget.sh \
    'https://github\.com/gastownhall/beads/releases/download/v\$VERSION/checksums\.txt' \
    "winget updater fetches canonical release checksums"
require_file_contains scripts/upgrade-smoke-test.sh \
    'https://github\.com/gastownhall/beads/releases/download/\$\{PREV_VERSION\}/\$\{asset\}' \
    "upgrade smoke test downloads from canonical releases"

section "Package metadata"
require_json_value npm-package/package.json '.repository.url' \
    "${CANONICAL_GITHUB}.git" \
    "npm repository URL is canonical"
require_json_value npm-package/package.json '.bugs.url' \
    "${CANONICAL_GITHUB}/issues" \
    "npm bugs URL is canonical"
require_json_value npm-package/package.json '.homepage' \
    "${CANONICAL_GITHUB}#readme" \
    "npm homepage is canonical"

require_file_contains integrations/beads-mcp/pyproject.toml \
    '^Homepage = "https://github\.com/gastownhall/beads"$' \
    "PyPI homepage is canonical"
require_file_contains integrations/beads-mcp/pyproject.toml \
    '^Repository = "https://github\.com/gastownhall/beads"$' \
    "PyPI repository URL is canonical"
require_file_contains integrations/beads-mcp/pyproject.toml \
    '^Issues = "https://github\.com/gastownhall/beads/issues"$' \
    "PyPI issues URL is canonical"

require_file_contains .goreleaser.yml \
    'owner: gastownhall' \
    "GoReleaser Homebrew tap owner is canonical"
require_file_contains .goreleaser.yml \
    'homepage: "https://github\.com/gastownhall/beads"' \
    "GoReleaser homepage is canonical"

require_file_contains winget/SteveYegge.beads.installer.yaml \
    'InstallerUrl: https://github\.com/gastownhall/beads/releases/download/v[0-9]+\.[0-9]+\.[0-9]+/beads_[0-9]+\.[0-9]+\.[0-9]+_windows_amd64\.zip' \
    "winget installer URL is canonical"
require_file_contains winget/SteveYegge.beads.locale.en-US.yaml \
    'PackageUrl: https://github\.com/gastownhall/beads' \
    "winget package URL is canonical"
require_file_contains winget/SteveYegge.beads.locale.en-US.yaml \
    'PublisherSupportUrl: https://github\.com/gastownhall/beads/issues' \
    "winget support URL is canonical"

echo ""
if [[ "$fail" -ne 0 ]]; then
    echo "FAILED: release consistency checks found drift" >&2
    exit 1
fi

echo "PASSED: release consistency checks found no drift"
