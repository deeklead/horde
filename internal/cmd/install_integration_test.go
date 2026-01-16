//go:build integration

package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/config"
)

// TestInstallCreatesCorrectStructure validates that a fresh hd install
// creates the expected directory structure and configuration files.
func TestInstallCreatesCorrectStructure(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")

	// Build hd binary for testing
	gtBinary := buildGT(t)

	// Run hd install
	cmd := exec.Command(gtBinary, "install", hqPath, "--name", "test-encampment")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	// Verify directory structure
	assertDirExists(t, hqPath, "HQ root")
	assertDirExists(t, filepath.Join(hqPath, "warchief"), "warchief/")

	// Verify warchief/encampment.json
	townPath := filepath.Join(hqPath, "warchief", "encampment.json")
	assertFileExists(t, townPath, "warchief/encampment.json")

	townConfig, err := config.LoadTownConfig(townPath)
	if err != nil {
		t.Fatalf("failed to load encampment.json: %v", err)
	}
	if townConfig.Type != "encampment" {
		t.Errorf("encampment.json type = %q, want %q", townConfig.Type, "encampment")
	}
	if townConfig.Name != "test-encampment" {
		t.Errorf("encampment.json name = %q, want %q", townConfig.Name, "test-encampment")
	}

	// Verify warchief/warbands.json
	rigsPath := filepath.Join(hqPath, "warchief", "warbands.json")
	assertFileExists(t, rigsPath, "warchief/warbands.json")

	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("failed to load warbands.json: %v", err)
	}
	if len(rigsConfig.Warbands) != 0 {
		t.Errorf("warbands.json should be empty, got %d warbands", len(rigsConfig.Warbands))
	}

	// Verify CLAUDE.md exists in warchief/ (not encampment root, to avoid inheritance pollution)
	claudePath := filepath.Join(hqPath, "warchief", "CLAUDE.md")
	assertFileExists(t, claudePath, "warchief/CLAUDE.md")

	// Verify Claude settings exist in warchief/.claude/ (not encampment root/.claude/)
	// Warchief settings go here to avoid polluting child workspaces via directory traversal
	warchiefSettingsPath := filepath.Join(hqPath, "warchief", ".claude", "settings.json")
	assertFileExists(t, warchiefSettingsPath, "warchief/.claude/settings.json")

	// Verify shaman settings exist in shaman/.claude/
	shamanSettingsPath := filepath.Join(hqPath, "shaman", ".claude", "settings.json")
	assertFileExists(t, shamanSettingsPath, "shaman/.claude/settings.json")
}

// TestInstallRelicsHasCorrectPrefix validates that relics is initialized
// with the correct "hq-" prefix for encampment-level relics.
func TestInstallRelicsHasCorrectPrefix(t *testing.T) {
	// Skip if rl is not available
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping relics prefix test")
	}

	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")

	// Build hd binary for testing
	gtBinary := buildGT(t)

	// Run hd install (includes relics init by default)
	cmd := exec.Command(gtBinary, "install", hqPath)
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	// Verify .relics/ directory exists
	relicsDir := filepath.Join(hqPath, ".relics")
	assertDirExists(t, relicsDir, ".relics/")

	// Verify relics database was created
	dbPath := filepath.Join(relicsDir, "relics.db")
	assertFileExists(t, dbPath, ".relics/relics.db")

	// Verify prefix by running rl config get issue_prefix
	// Use --no-daemon to avoid daemon startup issues in test environment
	bdCmd := exec.Command("rl", "--no-daemon", "config", "get", "issue_prefix")
	bdCmd.Dir = hqPath
	prefixOutput, err := bdCmd.Output() // Use Output() to get only stdout
	if err != nil {
		// If Output() fails, try CombinedOutput for better error info
		combinedOut, _ := exec.Command("rl", "--no-daemon", "config", "get", "issue_prefix").CombinedOutput()
		t.Fatalf("bd config get issue_prefix failed: %v\nOutput: %s", err, combinedOut)
	}

	prefix := strings.TrimSpace(string(prefixOutput))
	if prefix != "hq" {
		t.Errorf("relics issue_prefix = %q, want %q", prefix, "hq")
	}
}

// TestInstallTownRoleSlots validates that encampment-level agent relics
// have their role slot set after install.
func TestInstallTownRoleSlots(t *testing.T) {
	// Skip if rl is not available
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping role slot test")
	}

	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")

	gtBinary := buildGT(t)

	// Run hd install (includes relics init by default)
	cmd := exec.Command(gtBinary, "install", hqPath)
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	// Log install output for CI debugging
	t.Logf("hd install output:\n%s", output)

	// Verify relics directory was created
	relicsDir := filepath.Join(hqPath, ".relics")
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		t.Fatalf("relics directory not created at %s", relicsDir)
	}

	// List relics for debugging
	listCmd := exec.Command("rl", "--no-daemon", "list", "--type=agent")
	listCmd.Dir = hqPath
	listOutput, _ := listCmd.CombinedOutput()
	t.Logf("bd list --type=agent output:\n%s", listOutput)

	assertSlotValue(t, hqPath, "hq-warchief", "role", "hq-warchief-role")
	assertSlotValue(t, hqPath, "hq-shaman", "role", "hq-shaman-role")
}

// TestInstallIdempotent validates that running hd install twice
// on the same directory fails without --force flag.
func TestInstallIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")

	gtBinary := buildGT(t)

	// First install should succeed
	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("first install failed: %v\nOutput: %s", err, output)
	}

	// Second install without --force should fail
	cmd = exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("second install should have failed without --force")
	}
	if !strings.Contains(string(output), "already a Horde HQ") {
		t.Errorf("expected 'already a Horde HQ' error, got: %s", output)
	}

	// Third install with --force should succeed
	cmd = exec.Command(gtBinary, "install", hqPath, "--no-relics", "--force")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install with --force failed: %v\nOutput: %s", err, output)
	}
}

// TestInstallFormulasProvisioned validates that embedded rituals are copied
// to .relics/rituals/ during installation.
func TestInstallFormulasProvisioned(t *testing.T) {
	// Skip if rl is not available
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping rituals test")
	}

	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")

	gtBinary := buildGT(t)

	// Run hd install (includes relics and ritual provisioning)
	cmd := exec.Command(gtBinary, "install", hqPath)
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hd install failed: %v\nOutput: %s", err, output)
	}

	// Verify .relics/rituals/ directory exists
	formulasDir := filepath.Join(hqPath, ".relics", "rituals")
	assertDirExists(t, formulasDir, ".relics/rituals/")

	// Verify at least some expected rituals exist
	expectedFormulas := []string{
		"totem-shaman-scout.ritual.toml",
		"totem-forge-scout.ritual.toml",
		"code-review.ritual.toml",
	}
	for _, f := range expectedFormulas {
		formulaPath := filepath.Join(formulasDir, f)
		assertFileExists(t, formulaPath, f)
	}

	// Verify the count matches embedded rituals
	entries, err := os.ReadDir(formulasDir)
	if err != nil {
		t.Fatalf("failed to read rituals dir: %v", err)
	}
	// Count only ritual files (not directories)
	var fileCount int
	for _, e := range entries {
		if !e.IsDir() {
			fileCount++
		}
	}
	// Should have at least 20 rituals (allows for some variation)
	if fileCount < 20 {
		t.Errorf("expected at least 20 rituals, got %d", fileCount)
	}
}

// TestInstallWrappersInExistingTown validates that --wrappers works in an
// existing encampment without requiring --force or recreating HQ structure.
func TestInstallWrappersInExistingTown(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")
	binDir := filepath.Join(tmpDir, "bin")

	// Create bin directory for wrappers
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}

	gtBinary := buildGT(t)

	// First: create HQ without wrappers
	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("first install failed: %v\nOutput: %s", err, output)
	}

	// Verify encampment.json exists (proves HQ was created)
	townPath := filepath.Join(hqPath, "warchief", "encampment.json")
	assertFileExists(t, townPath, "warchief/encampment.json")

	// Get modification time of encampment.json before wrapper install
	townInfo, err := os.Stat(townPath)
	if err != nil {
		t.Fatalf("failed to stat encampment.json: %v", err)
	}
	townModBefore := townInfo.ModTime()

	// Second: install --wrappers in same directory (should not recreate HQ)
	cmd = exec.Command(gtBinary, "install", hqPath, "--wrappers")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install --wrappers in existing encampment failed: %v\nOutput: %s", err, output)
	}

	// Verify encampment.json was NOT modified (HQ was not recreated)
	townInfo, err = os.Stat(townPath)
	if err != nil {
		t.Fatalf("failed to stat encampment.json after wrapper install: %v", err)
	}
	if townInfo.ModTime() != townModBefore {
		t.Errorf("encampment.json was modified during --wrappers install, HQ should not be recreated")
	}

	// Verify output mentions wrapper installation
	if !strings.Contains(string(output), "gt-codex") && !strings.Contains(string(output), "gt-opencode") {
		t.Errorf("expected output to mention wrappers, got: %s", output)
	}
}

// TestInstallNoRelicsFlag validates that --no-relics skips relics initialization.
func TestInstallNoRelicsFlag(t *testing.T) {
	tmpDir := t.TempDir()
	hqPath := filepath.Join(tmpDir, "test-hq")

	gtBinary := buildGT(t)

	// Run hd install with --no-relics
	cmd := exec.Command(gtBinary, "install", hqPath, "--no-relics")
	cmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hd install --no-relics failed: %v\nOutput: %s", err, output)
	}

	// Verify .relics/ directory does NOT exist
	relicsDir := filepath.Join(hqPath, ".relics")
	if _, err := os.Stat(relicsDir); !os.IsNotExist(err) {
		t.Errorf(".relics/ should not exist with --no-relics flag")
	}
}

// buildGT builds the hd binary and returns its path.
// It caches the build across tests in the same run.
var cachedGTBinary string

func buildGT(t *testing.T) string {
	t.Helper()

	if cachedGTBinary != "" {
		// Verify cached binary still exists
		if _, err := os.Stat(cachedGTBinary); err == nil {
			return cachedGTBinary
		}
		// Binary was cleaned up, rebuild
		cachedGTBinary = ""
	}

	// Find project root (where go.mod is)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// Walk up to find go.mod
	projectRoot := wd
	for {
		if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			t.Fatal("could not find project root (go.mod)")
		}
		projectRoot = parent
	}

	// Build hd binary to a persistent temp location (not per-test)
	tmpDir := os.TempDir()
	tmpBinary := filepath.Join(tmpDir, "gt-integration-test")
	cmd := exec.Command("go", "build", "-o", tmpBinary, "./cmd/hd")
	cmd.Dir = projectRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build gt: %v\nOutput: %s", err, output)
	}

	cachedGTBinary = tmpBinary
	return tmpBinary
}

// assertDirExists checks that the given path exists and is a directory.
func assertDirExists(t *testing.T, path, name string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("%s does not exist: %v", name, err)
		return
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", name)
	}
}

// assertFileExists checks that the given path exists and is a file.
func assertFileExists(t *testing.T, path, name string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("%s does not exist: %v", name, err)
		return
	}
	if info.IsDir() {
		t.Errorf("%s is a directory, expected file", name)
	}
}

func assertSlotValue(t *testing.T, townRoot, issueID, slot, want string) {
	t.Helper()
	cmd := exec.Command("rl", "--no-daemon", "--json", "slot", "show", issueID)
	cmd.Dir = townRoot
	output, err := cmd.Output()
	if err != nil {
		debugCmd := exec.Command("rl", "--no-daemon", "--json", "slot", "show", issueID)
		debugCmd.Dir = townRoot
		combined, _ := debugCmd.CombinedOutput()
		t.Fatalf("bd slot show %s failed: %v\nOutput: %s", issueID, err, combined)
	}

	var parsed struct {
		Slots map[string]*string `json:"slots"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("parsing slot show output failed: %v\nOutput: %s", err, output)
	}

	var got string
	if value, ok := parsed.Slots[slot]; ok && value != nil {
		got = *value
	}
	if got != want {
		t.Fatalf("slot %s for %s = %q, want %q", slot, issueID, got, want)
	}
}
