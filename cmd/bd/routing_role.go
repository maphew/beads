package main

import (
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/routing"
)

func activeRepoPathForRouting() string {
	rc, err := beads.GetRepoContext()
	if err == nil && rc != nil && rc.RepoRoot != "" {
		// Two ways BeadsDir can resolve outside the CWD's repo, with opposite
		// answers for role detection:
		//   - BEADS_DIR set (bd -C plumbs the target through it, or the user
		//     exported it): the user explicitly selected that beads project,
		//     so its repo root is the active repo. This is the GH#4242 fix.
		//   - a .beads/redirect followed from the CWD workspace: the redirect
		//     relocates STORAGE, not the project — beads.role belongs to the
		//     workspace repo the user is operating in, not to wherever its
		//     data happens to live (which may not even be a git repo).
		if os.Getenv("BEADS_DIR") != "" || !rc.IsRedirected {
			return rc.RepoRoot
		}
		if rc.CWDRepoRoot != "" {
			return rc.CWDRepoRoot
		}
		return "."
	}
	if beadsDir := beads.FindBeadsDir(); beadsDir != "" {
		return filepath.Dir(beadsDir)
	}
	return "."
}

func detectUserRoleForActiveRepo() (routing.UserRole, error) {
	return routing.DetectUserRole(activeRepoPathForRouting())
}
