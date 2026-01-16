package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/git"
)

func TestNewSparseCheckoutCheck(t *testing.T) {
	check := NewSparseCheckoutCheck()

	if check.Name() != "sparse-checkout" {
		t.Errorf("expected name 'sparse-checkout', got %q", check.Name())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestSparseCheckoutCheck_NoRigSpecified(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: ""}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError when no warband specified, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "No warband specified") {
		t.Errorf("expected message about no warband, got %q", result.Message)
	}
}

func TestSparseCheckoutCheck_NoGitRepos(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(rigDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	// No git repos found = StatusOK (nothing to check)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no git repos, got %v", result.Status)
	}
}

// initGitRepo creates a minimal git repo with an initial commit.
func initGitRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	// git init
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Configure user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config email failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config name failed: %v\n%s", err, out)
	}

	// Create initial commit
	readmePath := filepath.Join(path, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}
}

func TestSparseCheckoutCheck_WarchiefRigMissingSparseCheckout(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create warchief/warband as a git repo without sparse checkout
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing sparse checkout, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "1 repo(s) missing") {
		t.Errorf("expected message about missing config, got %q", result.Message)
	}
	if len(result.Details) != 1 || !strings.Contains(result.Details[0], "warchief/warband") {
		t.Errorf("expected details to contain warchief/warband, got %v", result.Details)
	}
}

func TestSparseCheckoutCheck_WarchiefRigConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create warchief/warband as a git repo with sparse checkout configured
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)
	if err := git.ConfigureSparseCheckout(warchiefRig); err != nil {
		t.Fatalf("ConfigureSparseCheckout failed: %v", err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when sparse checkout configured, got %v", result.Status)
	}
}

func TestSparseCheckoutCheck_CrewMissingSparseCheckout(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create clan/agent1 as a git repo without sparse checkout
	crewAgent := filepath.Join(rigDir, "clan", "agent1")
	initGitRepo(t, crewAgent)

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing sparse checkout, got %v", result.Status)
	}
	if len(result.Details) != 1 || !strings.Contains(result.Details[0], "clan/agent1") {
		t.Errorf("expected details to contain clan/agent1, got %v", result.Details)
	}
}

func TestSparseCheckoutCheck_RaiderMissingSparseCheckout(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create raiders/pc1 as a git repo without sparse checkout
	raider := filepath.Join(rigDir, "raiders", "pc1")
	initGitRepo(t, raider)

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing sparse checkout, got %v", result.Status)
	}
	if len(result.Details) != 1 || !strings.Contains(result.Details[0], "raiders/pc1") {
		t.Errorf("expected details to contain raiders/pc1, got %v", result.Details)
	}
}

func TestSparseCheckoutCheck_MultipleReposMissing(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create multiple git repos without sparse checkout
	initGitRepo(t, filepath.Join(rigDir, "warchief", "warband"))
	initGitRepo(t, filepath.Join(rigDir, "clan", "agent1"))
	initGitRepo(t, filepath.Join(rigDir, "raiders", "pc1"))

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing sparse checkout, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "3 repo(s) missing") {
		t.Errorf("expected message about 3 missing repos, got %q", result.Message)
	}
	if len(result.Details) != 3 {
		t.Errorf("expected 3 details, got %d", len(result.Details))
	}
}

func TestSparseCheckoutCheck_MixedConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create warchief/warband with sparse checkout configured
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)
	if err := git.ConfigureSparseCheckout(warchiefRig); err != nil {
		t.Fatalf("ConfigureSparseCheckout failed: %v", err)
	}

	// Create clan/agent1 WITHOUT sparse checkout
	crewAgent := filepath.Join(rigDir, "clan", "agent1")
	initGitRepo(t, crewAgent)

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing sparse checkout, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "1 repo(s) missing") {
		t.Errorf("expected message about 1 missing repo, got %q", result.Message)
	}
	if len(result.Details) != 1 || !strings.Contains(result.Details[0], "clan/agent1") {
		t.Errorf("expected details to contain only clan/agent1, got %v", result.Details)
	}
}

func TestSparseCheckoutCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repos without sparse checkout
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)
	crewAgent := filepath.Join(rigDir, "clan", "agent1")
	initGitRepo(t, crewAgent)

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Apply fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify sparse checkout is now configured
	if !git.IsSparseCheckoutConfigured(warchiefRig) {
		t.Error("expected sparse checkout to be configured for warchief/warband")
	}
	if !git.IsSparseCheckoutConfigured(crewAgent) {
		t.Error("expected sparse checkout to be configured for clan/agent1")
	}

	// Verify check now passes
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v", result.Status)
	}
}

func TestSparseCheckoutCheck_FixNoOp(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repo with sparse checkout already configured
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)
	if err := git.ConfigureSparseCheckout(warchiefRig); err != nil {
		t.Fatalf("ConfigureSparseCheckout failed: %v", err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Run check to populate state
	result := check.Run(ctx)
	if result.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %v", result.Status)
	}

	// Fix should be a no-op (no affected repos)
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Still OK
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after no-op fix, got %v", result.Status)
	}
}

func TestSparseCheckoutCheck_NonGitDirSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create non-git directories (should be skipped)
	if err := os.MkdirAll(filepath.Join(rigDir, "warchief", "warband"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigDir, "clan", "agent1"), 0755); err != nil {
		t.Fatal(err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	// Non-git dirs are skipped, so StatusOK
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no git repos, got %v", result.Status)
	}
}

func TestSparseCheckoutCheck_VerifiesAllPatterns(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repo
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)

	// Configure sparse checkout using our function
	if err := git.ConfigureSparseCheckout(warchiefRig); err != nil {
		t.Fatalf("ConfigureSparseCheckout failed: %v", err)
	}

	// Read the sparse-checkout file and verify all patterns are present
	sparseFile := filepath.Join(warchiefRig, ".git", "info", "sparse-checkout")
	content, err := os.ReadFile(sparseFile)
	if err != nil {
		t.Fatalf("Failed to read sparse-checkout file: %v", err)
	}

	contentStr := string(content)

	// Verify all required patterns are present
	requiredPatterns := []string{
		"!/.claude/",        // Settings, rules, agents, commands
		"!/CLAUDE.md",       // Primary context file
		"!/CLAUDE.local.md", // Personal context file
		"!/.mcp.json",       // MCP server configuration
	}

	for _, pattern := range requiredPatterns {
		if !strings.Contains(contentStr, pattern) {
			t.Errorf("sparse-checkout file missing pattern %q, got:\n%s", pattern, contentStr)
		}
	}
}

func TestSparseCheckoutCheck_LegacyPatternNotSufficient(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repo
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)

	// Manually configure sparse checkout with only legacy .claude/ pattern (missing CLAUDE.md)
	cmd := exec.Command("git", "config", "core.sparseCheckout", "true")
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config failed: %v\n%s", err, out)
	}

	sparseFile := filepath.Join(warchiefRig, ".git", "info", "sparse-checkout")
	if err := os.MkdirAll(filepath.Dir(sparseFile), 0755); err != nil {
		t.Fatal(err)
	}
	// Only include legacy pattern, missing CLAUDE.md
	if err := os.WriteFile(sparseFile, []byte("/*\n!.claude/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	// Should fail because CLAUDE.md pattern is missing
	if result.Status != StatusError {
		t.Errorf("expected StatusError for legacy-only pattern, got %v", result.Status)
	}
}

func TestSparseCheckoutCheck_FixUpgradesLegacyPatterns(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repo with legacy sparse checkout (only .claude/)
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)

	cmd := exec.Command("git", "config", "core.sparseCheckout", "true")
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config failed: %v\n%s", err, out)
	}

	sparseFile := filepath.Join(warchiefRig, ".git", "info", "sparse-checkout")
	if err := os.MkdirAll(filepath.Dir(sparseFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sparseFile, []byte("/*\n!.claude/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Apply fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify all patterns are now present
	content, err := os.ReadFile(sparseFile)
	if err != nil {
		t.Fatalf("Failed to read sparse-checkout file: %v", err)
	}

	contentStr := string(content)
	requiredPatterns := []string{"!/.claude/", "!/CLAUDE.md", "!/CLAUDE.local.md", "!/.mcp.json"}
	for _, pattern := range requiredPatterns {
		if !strings.Contains(contentStr, pattern) {
			t.Errorf("after fix, sparse-checkout file missing pattern %q", pattern)
		}
	}

	// Verify check now passes
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v", result.Status)
	}
}

func TestSparseCheckoutCheck_FixFailsWithUntrackedCLAUDEMD(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repo without sparse checkout
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)

	// Create untracked CLAUDE.md (not added to git)
	claudeFile := filepath.Join(warchiefRig, "CLAUDE.md")
	if err := os.WriteFile(claudeFile, []byte("# Untracked context\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Fix should fail because CLAUDE.md is untracked and won't be removed
	err := check.Fix(ctx)
	if err == nil {
		t.Fatal("expected Fix to return error for untracked CLAUDE.md, but it succeeded")
	}

	// Verify error message is helpful
	if !strings.Contains(err.Error(), "CLAUDE.md") {
		t.Errorf("expected error to mention CLAUDE.md, got: %v", err)
	}
	if !strings.Contains(err.Error(), "untracked or modified") {
		t.Errorf("expected error to explain files are untracked/modified, got: %v", err)
	}
	if !strings.Contains(err.Error(), "manually remove") {
		t.Errorf("expected error to mention manual removal, got: %v", err)
	}
}

func TestSparseCheckoutCheck_FixFailsWithUntrackedClaudeDir(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repo without sparse checkout
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)

	// Create untracked .claude/ directory (not added to git)
	claudeDir := filepath.Join(warchiefRig, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Fix should fail because .claude/ is untracked and won't be removed
	err := check.Fix(ctx)
	if err == nil {
		t.Fatal("expected Fix to return error for untracked .claude/, but it succeeded")
	}

	// Verify error message mentions .claude
	if !strings.Contains(err.Error(), ".claude") {
		t.Errorf("expected error to mention .claude, got: %v", err)
	}
}

func TestSparseCheckoutCheck_FixFailsWithModifiedCLAUDEMD(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repo without sparse checkout
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)

	// Add and commit CLAUDE.md to the repo
	claudeFile := filepath.Join(warchiefRig, "CLAUDE.md")
	if err := os.WriteFile(claudeFile, []byte("# Original context\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "CLAUDE.md")
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "Add CLAUDE.md")
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}

	// Now modify CLAUDE.md without committing (making it "dirty")
	if err := os.WriteFile(claudeFile, []byte("# Modified context - local changes\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Fix should fail because CLAUDE.md is modified and git won't remove it
	err := check.Fix(ctx)
	if err == nil {
		t.Fatal("expected Fix to return error for modified CLAUDE.md, but it succeeded")
	}

	// Verify error message is helpful
	if !strings.Contains(err.Error(), "CLAUDE.md") {
		t.Errorf("expected error to mention CLAUDE.md, got: %v", err)
	}
}

func TestSparseCheckoutCheck_FixFailsWithMultipleProblems(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create git repo without sparse checkout
	warchiefRig := filepath.Join(rigDir, "warchief", "warband")
	initGitRepo(t, warchiefRig)

	// Create multiple untracked context files
	if err := os.WriteFile(filepath.Join(warchiefRig, "CLAUDE.md"), []byte("# Context\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(warchiefRig, ".mcp.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewSparseCheckoutCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Fix should fail and list multiple files
	err := check.Fix(ctx)
	if err == nil {
		t.Fatal("expected Fix to return error for multiple untracked files, but it succeeded")
	}

	// Verify error mentions both files
	errStr := err.Error()
	if !strings.Contains(errStr, "CLAUDE.md") {
		t.Errorf("expected error to mention CLAUDE.md, got: %v", err)
	}
	if !strings.Contains(errStr, ".mcp.json") {
		t.Errorf("expected error to mention .mcp.json, got: %v", err)
	}
}
