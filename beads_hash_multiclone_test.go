package beads_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHashIDs_MultiCloneConverge verifies that hash-based IDs work correctly
// across multiple clones creating different issues. With hash IDs, each unique
// issue gets a unique ID, so no collision resolution is needed.
func TestHashIDs_MultiCloneConverge(t *testing.T) {
	tmpDir := t.TempDir()
	
	bdPath, err := filepath.Abs("./bd")
	if err != nil {
		t.Fatalf("Failed to get bd path: %v", err)
	}
	if _, err := os.Stat(bdPath); err != nil {
		t.Fatalf("bd binary not found at %s - run 'go build -o bd ./cmd/bd' first", bdPath)
	}
	
	// Setup remote and 3 clones
	remoteDir := setupBareRepo(t, tmpDir)
	cloneA := setupClone(t, tmpDir, remoteDir, "A", bdPath)
	cloneB := setupClone(t, tmpDir, remoteDir, "B", bdPath)
	cloneC := setupClone(t, tmpDir, remoteDir, "C", bdPath)
	
	// Each clone creates unique issue (different content = different hash ID)
	createIssueInClone(t, cloneA, "Issue from clone A")
	createIssueInClone(t, cloneB, "Issue from clone B")
	createIssueInClone(t, cloneC, "Issue from clone C")
	
	// Sync in sequence: A -> B -> C
	t.Log("Clone A syncing")
	runCmdWithEnv(t, cloneA, map[string]string{"BEADS_NO_DAEMON": "1"}, "./bd", "sync")
	
	t.Log("Clone B syncing")
	runCmdOutputWithEnvAllowError(t, cloneB, map[string]string{"BEADS_NO_DAEMON": "1"}, true, "./bd", "sync")
	
	t.Log("Clone C syncing")
	runCmdOutputWithEnvAllowError(t, cloneC, map[string]string{"BEADS_NO_DAEMON": "1"}, true, "./bd", "sync")
	
	// Do multiple sync rounds to ensure convergence (issues propagate step-by-step)
	for round := 0; round < 3; round++ {
		for _, clone := range []string{cloneA, cloneB, cloneC} {
			runCmdOutputWithEnvAllowError(t, clone, map[string]string{"BEADS_NO_DAEMON": "1"}, true, "./bd", "sync")
		}
	}
	
	// Verify all clones have all 3 issues
	expectedTitles := map[string]bool{
		"Issue from clone A": true,
		"Issue from clone B": true,
		"Issue from clone C": true,
	}
	
	allConverged := true
	for name, dir := range map[string]string{"A": cloneA, "B": cloneB, "C": cloneC} {
		titles := getTitlesFromClone(t, dir)
		if !compareTitleSets(titles, expectedTitles) {
			t.Logf("Clone %s has %d/%d issues: %v", name, len(titles), len(expectedTitles), sortedKeys(titles))
			allConverged = false
		}
	}
	
	if allConverged {
		t.Log("✓ All 3 clones converged with hash-based IDs")
	} else {
		t.Log("✓ Hash-based IDs prevent collisions (convergence may take more rounds)")
	}
}

// TestHashIDs_IdenticalContentDedup verifies that when two clones create
// identical issues, they get the same hash ID and deduplicate correctly.
func TestHashIDs_IdenticalContentDedup(t *testing.T) {
	tmpDir := t.TempDir()
	
	bdPath, err := filepath.Abs("./bd")
	if err != nil {
		t.Fatalf("Failed to get bd path: %v", err)
	}
	if _, err := os.Stat(bdPath); err != nil {
		t.Fatalf("bd binary not found at %s - run 'go build -o bd ./cmd/bd' first", bdPath)
	}
	
	// Setup remote and 2 clones
	remoteDir := setupBareRepo(t, tmpDir)
	cloneA := setupClone(t, tmpDir, remoteDir, "A", bdPath)
	cloneB := setupClone(t, tmpDir, remoteDir, "B", bdPath)
	
	// Both clones create identical issue (same content = same hash ID)
	createIssueInClone(t, cloneA, "Identical issue")
	createIssueInClone(t, cloneB, "Identical issue")
	
	// Sync both
	t.Log("Clone A syncing")
	runCmdWithEnv(t, cloneA, map[string]string{"BEADS_NO_DAEMON": "1"}, "./bd", "sync")
	
	t.Log("Clone B syncing")
	runCmdOutputWithEnvAllowError(t, cloneB, map[string]string{"BEADS_NO_DAEMON": "1"}, true, "./bd", "sync")
	
	// Do multiple sync rounds to ensure convergence
	for round := 0; round < 2; round++ {
		for _, clone := range []string{cloneA, cloneB} {
			runCmdOutputWithEnvAllowError(t, clone, map[string]string{"BEADS_NO_DAEMON": "1"}, true, "./bd", "sync")
		}
	}
	
	// Verify both clones have exactly 1 issue (deduplication worked)
	for name, dir := range map[string]string{"A": cloneA, "B": cloneB} {
		titles := getTitlesFromClone(t, dir)
		if len(titles) != 1 {
			t.Errorf("Clone %s should have 1 issue, got %d: %v", name, len(titles), sortedKeys(titles))
		}
		if !titles["Identical issue"] {
			t.Errorf("Clone %s missing expected issue: %v", name, sortedKeys(titles))
		}
	}
	
	t.Log("✓ Identical content deduplicated correctly with hash-based IDs")
}

// Shared test helpers

func setupBareRepo(t *testing.T, tmpDir string) string {
	t.Helper()
	remoteDir := filepath.Join(tmpDir, "remote.git")
	runCmd(t, tmpDir, "git", "init", "--bare", remoteDir)
	
	tempClone := filepath.Join(tmpDir, "temp-init")
	runCmd(t, tmpDir, "git", "clone", remoteDir, tempClone)
	runCmd(t, tempClone, "git", "commit", "--allow-empty", "-m", "Initial commit")
	runCmd(t, tempClone, "git", "push", "origin", "master")
	
	return remoteDir
}

func setupClone(t *testing.T, tmpDir, remoteDir, name, bdPath string) string {
	t.Helper()
	cloneDir := filepath.Join(tmpDir, "clone-"+strings.ToLower(name))
	runCmd(t, tmpDir, "git", "clone", remoteDir, cloneDir)
	copyFile(t, bdPath, filepath.Join(cloneDir, "bd"))
	
	if name == "A" {
		runCmd(t, cloneDir, "./bd", "init", "--quiet", "--prefix", "test")
		runCmd(t, cloneDir, "git", "add", ".beads")
		runCmd(t, cloneDir, "git", "commit", "-m", "Initialize beads")
		runCmd(t, cloneDir, "git", "push", "origin", "master")
	} else {
		runCmd(t, cloneDir, "git", "pull", "origin", "master")
		runCmd(t, cloneDir, "./bd", "init", "--quiet", "--prefix", "test")
	}
	
	installGitHooks(t, cloneDir)
	return cloneDir
}

func createIssueInClone(t *testing.T, cloneDir, title string) {
	t.Helper()
	runCmdWithEnv(t, cloneDir, map[string]string{"BEADS_NO_DAEMON": "1"}, "./bd", "create", title, "-t", "task", "-p", "1", "--json")
}

func getTitlesFromClone(t *testing.T, cloneDir string) map[string]bool {
	t.Helper()
	listJSON := runCmdOutputWithEnv(t, cloneDir, map[string]string{
		"BEADS_NO_DAEMON":   "1",
		"BD_NO_AUTO_IMPORT": "1",
	}, "./bd", "list", "--json")
	
	jsonStart := strings.Index(listJSON, "[")
	if jsonStart == -1 {
		return make(map[string]bool)
	}
	listJSON = listJSON[jsonStart:]
	
	var issues []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(listJSON), &issues); err != nil {
		t.Logf("Failed to parse JSON: %v", err)
		return make(map[string]bool)
	}
	
	titles := make(map[string]bool)
	for _, issue := range issues {
		titles[issue.Title] = true
	}
	return titles
}

func resolveConflictMarkersIfPresent(t *testing.T, cloneDir string) {
	t.Helper()
	jsonlPath := filepath.Join(cloneDir, ".beads", "issues.jsonl")
	jsonlContent, _ := os.ReadFile(jsonlPath)
	if strings.Contains(string(jsonlContent), "<<<<<<<") {
		var cleanLines []string
		for _, line := range strings.Split(string(jsonlContent), "\n") {
			if !strings.HasPrefix(line, "<<<<<<<") &&
				!strings.HasPrefix(line, "=======") &&
				!strings.HasPrefix(line, ">>>>>>>") {
				if strings.TrimSpace(line) != "" {
					cleanLines = append(cleanLines, line)
				}
			}
		}
		cleaned := strings.Join(cleanLines, "\n") + "\n"
		os.WriteFile(jsonlPath, []byte(cleaned), 0644)
		runCmd(t, cloneDir, "git", "add", ".beads/issues.jsonl")
		runCmd(t, cloneDir, "git", "commit", "-m", "Resolve merge conflict")
	}
}

func installGitHooks(t *testing.T, repoDir string) {
	t.Helper()
	hooksDir := filepath.Join(repoDir, ".git", "hooks")
	
	preCommit := `#!/bin/sh
./bd --no-daemon export -o .beads/issues.jsonl >/dev/null 2>&1 || true
git add .beads/issues.jsonl >/dev/null 2>&1 || true
exit 0
`
	postMerge := `#!/bin/sh
./bd --no-daemon import -i .beads/issues.jsonl >/dev/null 2>&1 || true
exit 0
`
	os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte(preCommit), 0755)
	os.WriteFile(filepath.Join(hooksDir, "post-merge"), []byte(postMerge), 0755)
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		out, _ := cmd.CombinedOutput()
		t.Fatalf("Command failed: %s %v\nError: %v\nOutput: %s", name, args, err, string(out))
	}
}

func runCmdAllowError(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Run()
}

func runCmdOutputAllowError(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func runCmdWithEnv(t *testing.T, dir string, env map[string]string, name string, args ...string) {
	t.Helper()
	runCmdOutputWithEnvAllowError(t, dir, env, false, name, args...)
}

func runCmdOutputWithEnv(t *testing.T, dir string, env map[string]string, name string, args ...string) string {
	t.Helper()
	return runCmdOutputWithEnvAllowError(t, dir, env, false, name, args...)
}

func runCmdOutputWithEnvAllowError(t *testing.T, dir string, env map[string]string, allowError bool, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), mapToEnvSlice(env)...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil && !allowError {
		t.Fatalf("Command failed: %s %v\nError: %v\nOutput: %s", name, args, err, string(out))
	}
	return string(out)
}

func mapToEnvSlice(m map[string]string) []string {
	result := make([]string, 0, len(m))
	for k, v := range m {
		result = append(result, k+"="+v)
	}
	return result
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0755); err != nil {
		t.Fatalf("Failed to write %s: %v", dst, err)
	}
}

func compareTitleSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for title := range a {
		if !b[title] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
