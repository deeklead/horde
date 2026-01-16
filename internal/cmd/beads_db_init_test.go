//go:build integration

// Package cmd contains integration tests for relics db initialization after clone.
//
// Run with: go test -tags=integration ./internal/cmd -run TestRelicsDbInitAfterClone -v
//
// Bug: GitHub Issue #72
// When a repo with tracked .relics/ is added as a warband, relics.db doesn't exist
// (it's gitignored) and rl operations fail because no one runs `rl init`.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// createTrackedRelicsRepoWithIssues creates a git repo with .relics/ tracked that contains existing issues.
// This simulates a clone of a repo that has tracked relics with issues exported to issues.jsonl.
// The relics.db is NOT included (gitignored), so prefix must be detected from issues.jsonl.
func createTrackedRelicsRepoWithIssues(t *testing.T, path, prefix string, numIssues int) {
	t.Helper()

	// Create directory
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	// Initialize git repo with explicit main branch
	cmds := [][]string{
		{"git", "init", "--initial-branch=main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Create initial file and commit (so we have something before relics)
	readmePath := filepath.Join(path, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	commitCmds := [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "Initial commit"},
	}
	for _, args := range commitCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Initialize relics
	relicsDir := filepath.Join(path, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir .relics: %v", err)
	}

	// Run rl init
	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", prefix)
	cmd.Dir = path
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init failed: %v\nOutput: %s", err, output)
	}

	// Create issues
	for i := 1; i <= numIssues; i++ {
		cmd = exec.Command("rl", "--no-daemon", "-q", "create",
			"--type", "task", "--title", fmt.Sprintf("Test issue %d", i))
		cmd.Dir = path
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bd create issue %d failed: %v\nOutput: %s", i, err, output)
		}
	}

	// Add .relics to git (simulating tracked relics)
	cmd = exec.Command("git", "add", ".relics")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add .relics: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "commit", "-m", "Add relics with issues")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit relics: %v\n%s", err, out)
	}

	// Remove relics.db to simulate what a clone would look like
	// (relics.db is gitignored, so cloned repos don't have it)
	dbPath := filepath.Join(relicsDir, "relics.db")
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remove relics.db: %v", err)
	}
}

// TestRelicsDbInitAfterClone tests that when a tracked relics repo is added as a warband,
// the relics database is properly initialized even though relics.db doesn't exist.
func TestRelicsDbInitAfterClone(t *testing.T) {
	// Skip if rl is not available
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	tmpDir := t.TempDir()
	gtBinary := buildGT(t)

	t.Run("TrackedRepoWithExistingPrefix", func(t *testing.T) {
		// GitHub Issue #72: hd warband add should detect existing prefix from tracked relics
		// https://github.com/OWNER/horde/issues/72
		//
		// This tests that when a tracked relics repo has existing issues in issues.jsonl,
		// hd warband add can detect the prefix from those issues WITHOUT --prefix flag.

		townRoot := filepath.Join(tmpDir, "encampment-prefix-test")
		reposDir := filepath.Join(tmpDir, "repos")
		os.MkdirAll(reposDir, 0755)

		// Create a repo with existing relics prefix "existing-prefix" AND issues
		// This creates issues.jsonl with issues like "existing-prefix-1", etc.
		existingRepo := filepath.Join(reposDir, "existing-repo")
		createTrackedRelicsRepoWithIssues(t, existingRepo, "existing-prefix", 3)

		// Install encampment
		cmd := exec.Command(gtBinary, "install", townRoot, "--name", "prefix-test")
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
		}

		// Add warband WITHOUT specifying --prefix - should detect "existing-prefix" from issues.jsonl
		cmd = exec.Command(gtBinary, "warband", "add", "myrig", existingRepo)
		cmd.Dir = townRoot
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hd warband add failed: %v\nOutput: %s", err, output)
		}

		// Verify routes.jsonl has the prefix
		routesContent, err := os.ReadFile(filepath.Join(townRoot, ".relics", "routes.jsonl"))
		if err != nil {
			t.Fatalf("read routes.jsonl: %v", err)
		}

		if !strings.Contains(string(routesContent), `"prefix":"existing-prefix-"`) {
			t.Errorf("routes.jsonl should contain existing-prefix-, got:\n%s", routesContent)
		}

		// NOW TRY TO USE rl - this is the key test for the bug
		// Without the fix, relics.db doesn't exist and rl operations fail
		rigPath := filepath.Join(townRoot, "myrig", "warchief", "warband")
		cmd = exec.Command("rl", "--no-daemon", "--json", "-q", "create",
			"--type", "task", "--title", "test-from-warband")
		cmd.Dir = rigPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bd create failed (bug!): %v\nOutput: %s\n\nThis is the bug: relics.db doesn't exist after clone because rl init was never run", err, output)
		}

		var result struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("parse output: %v", err)
		}

		if !strings.HasPrefix(result.ID, "existing-prefix-") {
			t.Errorf("expected existing-prefix- prefix, got %s", result.ID)
		}
	})

	t.Run("TrackedRepoWithNoIssuesRequiresPrefix", func(t *testing.T) {
		// Regression test: When a tracked relics repo has NO issues (fresh init),
		// hd warband add must use the --prefix flag since there's nothing to detect from.

		townRoot := filepath.Join(tmpDir, "encampment-no-issues")
		reposDir := filepath.Join(tmpDir, "repos-no-issues")
		os.MkdirAll(reposDir, 0755)

		// Create a tracked relics repo with NO issues (just rl init)
		emptyRepo := filepath.Join(reposDir, "empty-repo")
		createTrackedRelicsRepoWithNoIssues(t, emptyRepo, "empty-prefix")

		// Install encampment
		cmd := exec.Command(gtBinary, "install", townRoot, "--name", "no-issues-test")
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
		}

		// Add warband WITH --prefix since we can't detect from empty issues.jsonl
		cmd = exec.Command(gtBinary, "warband", "add", "emptyrig", emptyRepo, "--prefix", "empty-prefix")
		cmd.Dir = townRoot
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hd warband add with --prefix failed: %v\nOutput: %s", err, output)
		}

		// Verify routes.jsonl has the prefix
		routesContent, err := os.ReadFile(filepath.Join(townRoot, ".relics", "routes.jsonl"))
		if err != nil {
			t.Fatalf("read routes.jsonl: %v", err)
		}

		if !strings.Contains(string(routesContent), `"prefix":"empty-prefix-"`) {
			t.Errorf("routes.jsonl should contain empty-prefix-, got:\n%s", routesContent)
		}

		// Verify rl operations work with the configured prefix
		rigPath := filepath.Join(townRoot, "emptyrig", "warchief", "warband")
		cmd = exec.Command("rl", "--no-daemon", "--json", "-q", "create",
			"--type", "task", "--title", "test-from-empty-repo")
		cmd.Dir = rigPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bd create failed: %v\nOutput: %s", err, output)
		}

		var result struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("parse output: %v", err)
		}

		if !strings.HasPrefix(result.ID, "empty-prefix-") {
			t.Errorf("expected empty-prefix- prefix, got %s", result.ID)
		}
	})

	t.Run("TrackedRepoWithPrefixMismatchErrors", func(t *testing.T) {
		// Test that when --prefix is explicitly provided but doesn't match
		// the prefix detected from existing issues, hd warband add fails with an error.

		townRoot := filepath.Join(tmpDir, "encampment-mismatch")
		reposDir := filepath.Join(tmpDir, "repos-mismatch")
		os.MkdirAll(reposDir, 0755)

		// Create a repo with existing relics prefix "real-prefix" with issues
		mismatchRepo := filepath.Join(reposDir, "mismatch-repo")
		createTrackedRelicsRepoWithIssues(t, mismatchRepo, "real-prefix", 2)

		// Install encampment
		cmd := exec.Command(gtBinary, "install", townRoot, "--name", "mismatch-test")
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
		}

		// Add warband with WRONG --prefix - should fail
		cmd = exec.Command(gtBinary, "warband", "add", "mismatchrig", mismatchRepo, "--prefix", "wrong-prefix")
		cmd.Dir = townRoot
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		output, err := cmd.CombinedOutput()

		// Should fail
		if err == nil {
			t.Fatalf("hd warband add should have failed with prefix mismatch, but succeeded.\nOutput: %s", output)
		}

		// Verify error message mentions the mismatch
		outputStr := string(output)
		if !strings.Contains(outputStr, "prefix mismatch") {
			t.Errorf("expected 'prefix mismatch' in error, got:\n%s", outputStr)
		}
		if !strings.Contains(outputStr, "real-prefix") {
			t.Errorf("expected 'real-prefix' (detected) in error, got:\n%s", outputStr)
		}
		if !strings.Contains(outputStr, "wrong-prefix") {
			t.Errorf("expected 'wrong-prefix' (provided) in error, got:\n%s", outputStr)
		}
	})

	t.Run("TrackedRepoWithNoIssuesFallsBackToDerivedPrefix", func(t *testing.T) {
		// Test the fallback behavior: when a tracked relics repo has NO issues
		// and NO --prefix is provided, hd warband add should derive prefix from warband name.

		townRoot := filepath.Join(tmpDir, "encampment-derived")
		reposDir := filepath.Join(tmpDir, "repos-derived")
		os.MkdirAll(reposDir, 0755)

		// Create a tracked relics repo with NO issues
		derivedRepo := filepath.Join(reposDir, "derived-repo")
		createTrackedRelicsRepoWithNoIssues(t, derivedRepo, "original-prefix")

		// Install encampment
		cmd := exec.Command(gtBinary, "install", townRoot, "--name", "derived-test")
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
		}

		// Add warband WITHOUT --prefix - should derive from warband name "testrig"
		// deriveRelicsPrefix("testrig") should produce some abbreviation
		cmd = exec.Command(gtBinary, "warband", "add", "testrig", derivedRepo)
		cmd.Dir = townRoot
		cmd.Env = append(os.Environ(), "HOME="+tmpDir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("hd warband add (no --prefix) failed: %v\nOutput: %s", err, output)
		}

		// The output should mention "Using prefix" since detection failed
		if !strings.Contains(string(output), "Using prefix") {
			t.Logf("Output: %s", output)
		}

		// Verify rl operations work - the key test is that relics.db was initialized
		rigPath := filepath.Join(townRoot, "testrig", "warchief", "warband")
		cmd = exec.Command("rl", "--no-daemon", "--json", "-q", "create",
			"--type", "task", "--title", "test-derived-prefix")
		cmd.Dir = rigPath
		output, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bd create failed (relics.db not initialized?): %v\nOutput: %s", err, output)
		}

		var result struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("parse output: %v", err)
		}

		// The ID should have SOME prefix (derived from "testrig")
		// We don't care exactly what it is, just that rl works
		if result.ID == "" {
			t.Error("expected non-empty issue ID")
		}
		t.Logf("Created issue with derived prefix: %s", result.ID)
	})
}

// createTrackedRelicsRepoWithNoIssues creates a git repo with .relics/ tracked but NO issues.
// This simulates a fresh rl init that was committed before any issues were created.
func createTrackedRelicsRepoWithNoIssues(t *testing.T, path, prefix string) {
	t.Helper()

	// Create directory
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	// Initialize git repo with explicit main branch
	cmds := [][]string{
		{"git", "init", "--initial-branch=main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Create initial file and commit
	readmePath := filepath.Join(path, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	commitCmds := [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "Initial commit"},
	}
	for _, args := range commitCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Initialize relics
	relicsDir := filepath.Join(path, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir .relics: %v", err)
	}

	// Run rl init (creates relics.db but no issues)
	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", prefix)
	cmd.Dir = path
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init failed: %v\nOutput: %s", err, output)
	}

	// Add .relics to git (simulating tracked relics)
	cmd = exec.Command("git", "add", ".relics")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add .relics: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "commit", "-m", "Add relics (no issues)")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit relics: %v\n%s", err, out)
	}

	// Remove relics.db to simulate what a clone would look like
	dbPath := filepath.Join(relicsDir, "relics.db")
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remove relics.db: %v", err)
	}
}
