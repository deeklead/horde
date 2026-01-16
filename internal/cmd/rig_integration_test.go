//go:build integration

// Package cmd contains integration tests for the warband command.
//
// Run with: go test -tags=integration ./internal/cmd -run TestRigAdd -v
package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/warband"
)

// createTestGitRepo creates a minimal git repository for testing.
// Returns the path to the bare repo URL (suitable for cloning).
func createTestGitRepo(t *testing.T, name string) string {
	t.Helper()

	// Create a regular repo with initial commit
	repoDir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	// Initialize git repo with explicit main branch
	// (system default may vary, causing checkout failures)
	cmds := [][]string{
		{"git", "init", "--initial-branch=main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Create initial file and commit
	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	commitCmds := [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "Initial commit"},
	}
	for _, args := range commitCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Return the path as a file:// URL
	return repoDir
}

// setupTestTown creates a minimal Horde workspace for testing.
// Returns townRoot and a cleanup function.
func setupTestTown(t *testing.T) string {
	t.Helper()

	townRoot := t.TempDir()

	// Create warchief directory (required for warbands.json)
	warchiefDir := filepath.Join(townRoot, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir warchief: %v", err)
	}

	// Create empty warbands.json
	rigsPath := filepath.Join(warchiefDir, "warbands.json")
	rigsConfig := &config.RigsConfig{
		Version: 1,
		Warbands:    make(map[string]config.RigEntry),
	}
	if err := config.SaveRigsConfig(rigsPath, rigsConfig); err != nil {
		t.Fatalf("save warbands.json: %v", err)
	}

	// Create .relics directory for routes
	relicsDir := filepath.Join(townRoot, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir .relics: %v", err)
	}

	return townRoot
}

// mockBdCommand creates a fake rl binary that simulates rl behavior.
// This avoids needing rl installed for tests.
func mockBdCommand(t *testing.T) {
	t.Helper()

	binDir := t.TempDir()
	bdPath := filepath.Join(binDir, "rl")

	// Create a script that simulates rl init and other commands
	script := `#!/bin/sh
# Mock rl for testing

case "$1" in
  init)
    # Create .relics directory and config.yaml
    mkdir -p .relics
    prefix="hd"
    for arg in "$@"; do
      case "$arg" in
        --prefix=*) prefix="${arg#--prefix=}" ;;
        --prefix)
          # Next arg is the prefix
          shift
          if [ -n "$1" ] && [ "$1" != "--"* ]; then
            prefix="$1"
          fi
          ;;
      esac
      shift
    done
    # Handle positional --prefix VALUE
    shift  # skip 'init'
    while [ $# -gt 0 ]; do
      case "$1" in
        --prefix)
          shift
          prefix="$1"
          ;;
      esac
      shift
    done
    echo "prefix: $prefix" > .relics/config.yaml
    exit 0
    ;;
  migrate)
    exit 0
    ;;
  show)
    echo '{"error":"not found"}' >&2
    exit 1
    ;;
  create)
    # Return minimal JSON for agent bead creation
    echo '{}'
    exit 0
    ;;
  mol|list)
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(bdPath, []byte(script), 0755); err != nil {
		t.Fatalf("write mock bd: %v", err)
	}

	// Prepend to PATH
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRigAddCreatesCorrectStructure verifies that hd warband add creates
// the expected directory structure.
func TestRigAddCreatesCorrectStructure(t *testing.T) {
	mockBdCommand(t)
	townRoot := setupTestTown(t)
	gitURL := createTestGitRepo(t, "testproject")

	// Load warbands config
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}

	// Create warband manager and add warband
	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	_, err = mgr.AddRig(warband.AddRigOptions{
		Name:        "testrig",
		GitURL:      gitURL,
		RelicsPrefix: "tr",
	})
	if err != nil {
		t.Fatalf("AddRig: %v", err)
	}

	rigPath := filepath.Join(townRoot, "testrig")

	// Verify directory structure
	expectedDirs := []string{
		"",                // warband root
		"warchief",           // warchief container
		"warchief/warband",       // warchief clone
		"forge",        // forge container
		"forge/warband",    // forge worktree
		"witness",         // witness dir
		"raiders",        // raiders dir
		"clan",            // clan dir
		".relics",          // relics dir
		"plugins",         // plugins dir
	}

	for _, dir := range expectedDirs {
		path := filepath.Join(rigPath, dir)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %s to exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", dir)
		}
	}

	// Verify config.json exists
	configPath := filepath.Join(rigPath, "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.json not found: %v", err)
	}

	// Verify .repo.git (bare repo) exists
	bareRepoPath := filepath.Join(rigPath, ".repo.git")
	if _, err := os.Stat(bareRepoPath); err != nil {
		t.Errorf(".repo.git not found: %v", err)
	}

	// Verify warchief/warband is a git repo
	warchiefRigPath := filepath.Join(rigPath, "warchief", "warband")
	gitDirPath := filepath.Join(warchiefRigPath, ".git")
	if _, err := os.Stat(gitDirPath); err != nil {
		t.Errorf("warchief/warband/.git not found: %v", err)
	}

	// Verify forge/warband is a git worktree (has .git file pointing to bare repo)
	forgeRigPath := filepath.Join(rigPath, "forge", "warband")
	forgeGitPath := filepath.Join(forgeRigPath, ".git")
	info, err := os.Stat(forgeGitPath)
	if err != nil {
		t.Errorf("forge/warband/.git not found: %v", err)
	} else if info.IsDir() {
		t.Errorf("forge/warband/.git should be a file (worktree), not a directory")
	}

	// Verify Claude settings are created in correct locations (outside git repos).
	// Settings in parent directories are inherited by agents via directory traversal,
	// without polluting the source repos.
	expectedSettings := []struct {
		path string
		desc string
	}{
		{filepath.Join(rigPath, "witness", ".claude", "settings.json"), "witness/.claude/settings.json"},
		{filepath.Join(rigPath, "forge", ".claude", "settings.json"), "forge/.claude/settings.json"},
		{filepath.Join(rigPath, "clan", ".claude", "settings.json"), "clan/.claude/settings.json"},
		{filepath.Join(rigPath, "raiders", ".claude", "settings.json"), "raiders/.claude/settings.json"},
	}

	for _, s := range expectedSettings {
		if _, err := os.Stat(s.path); err != nil {
			t.Errorf("%s not found: %v", s.desc, err)
		}
	}

	// Verify settings are NOT created inside source repos (these would be wrong)
	wrongLocations := []struct {
		path string
		desc string
	}{
		{filepath.Join(rigPath, "witness", "warband", ".claude", "settings.json"), "witness/warband/.claude (inside source repo)"},
		{filepath.Join(rigPath, "forge", "warband", ".claude", "settings.json"), "forge/warband/.claude (inside source repo)"},
	}

	for _, w := range wrongLocations {
		if _, err := os.Stat(w.path); err == nil {
			t.Errorf("%s should NOT exist (settings would pollute source repo)", w.desc)
		}
	}
}

// TestRigAddInitializesRelics verifies that relics is initialized with
// the correct prefix.
func TestRigAddInitializesRelics(t *testing.T) {
	mockBdCommand(t)
	townRoot := setupTestTown(t)
	gitURL := createTestGitRepo(t, "relicstest")

	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}

	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	newRig, err := mgr.AddRig(warband.AddRigOptions{
		Name:        "relicstest",
		GitURL:      gitURL,
		RelicsPrefix: "bt",
	})
	if err != nil {
		t.Fatalf("AddRig: %v", err)
	}

	// Verify warband config has correct prefix
	if newRig.Config == nil {
		t.Fatal("warband.Config is nil")
	}
	if newRig.Config.Prefix != "bt" {
		t.Errorf("warband.Config.Prefix = %q, want %q", newRig.Config.Prefix, "bt")
	}

	// Verify .relics directory was created
	relicsDir := filepath.Join(townRoot, "relicstest", ".relics")
	if _, err := os.Stat(relicsDir); err != nil {
		t.Errorf(".relics directory not found: %v", err)
	}

	// Verify config.yaml exists with correct prefix
	configPath := filepath.Join(relicsDir, "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf(".relics/config.yaml not found: %v", err)
	} else {
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Errorf("reading config.yaml: %v", err)
		} else if !strings.Contains(string(content), "prefix: bt") && !strings.Contains(string(content), "prefix:bt") {
			t.Errorf("config.yaml doesn't contain expected prefix, got: %s", string(content))
		}
	}

	// =========================================================================
	// IMPORTANT: Verify routes.jsonl does NOT exist in the warband's .relics directory
	// =========================================================================
	//
	// WHY WE DON'T CREATE routes.jsonl IN WARBAND DIRECTORIES:
	//
	// 1. BD'S WALK-UP ROUTING MECHANISM:
	//    When rl needs to find routing configuration, it walks up the directory
	//    tree looking for a .relics directory with routes.jsonl. It stops at the
	//    first routes.jsonl it finds. If a warband has its own routes.jsonl, rl will
	//    use that and NEVER reach the encampment-level routes.jsonl, breaking cross-warband
	//    routing entirely.
	//
	// 2. ENCAMPMENT-LEVEL ROUTING IS THE SOURCE OF TRUTH:
	//    All routing configuration belongs in the encampment's .relics/routes.jsonl.
	//    This single file contains prefix->path mappings for ALL warbands, enabling
	//    rl to route issue IDs like "tr-123" to the correct warband directory.
	//
	// 3. HISTORICAL BUG - BD AUTO-EXPORT CORRUPTION:
	//    There was a bug where bd's auto-export feature would write issue data
	//    to routes.jsonl if issues.jsonl didn't exist. This corrupted routing
	//    config with issue JSON objects. We now create empty issues.jsonl files
	//    proactively to prevent this, but we also verify routes.jsonl doesn't
	//    exist as a defense-in-depth measure.
	//
	// 4. DOCTOR CHECK EXISTS:
	//    The "warband-routes-jsonl" doctor check detects and can fix (delete) any
	//    routes.jsonl files that appear in warband .relics directories.
	//
	// If you're modifying warband creation and thinking about adding routes.jsonl
	// to the warband's .relics directory - DON'T. It will break cross-warband routing.
	// =========================================================================
	rigRoutesPath := filepath.Join(relicsDir, "routes.jsonl")
	if _, err := os.Stat(rigRoutesPath); err == nil {
		t.Errorf("routes.jsonl should NOT exist in warband .relics directory (breaks rl walk-up routing)")
	}

	// Verify issues.jsonl DOES exist (prevents rl auto-export corruption)
	rigIssuesPath := filepath.Join(relicsDir, "issues.jsonl")
	if _, err := os.Stat(rigIssuesPath); err != nil {
		t.Errorf("issues.jsonl should exist in warband .relics directory (prevents auto-export corruption): %v", err)
	}
}

// TestRigAddUpdatesRoutes verifies that routes.jsonl is updated
// with the new warband's route.
func TestRigAddUpdatesRoutes(t *testing.T) {
	mockBdCommand(t)
	townRoot := setupTestTown(t)
	gitURL := createTestGitRepo(t, "routetest")

	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}

	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	newRig, err := mgr.AddRig(warband.AddRigOptions{
		Name:        "routetest",
		GitURL:      gitURL,
		RelicsPrefix: "rt",
	})
	if err != nil {
		t.Fatalf("AddRig: %v", err)
	}

	// Append route to routes.jsonl (this is done by the CLI command, not AddRig)
	// The CLI command in runRigAdd calls relics.AppendRoute after AddRig succeeds
	if newRig.Config != nil && newRig.Config.Prefix != "" {
		route := relics.Route{
			Prefix: newRig.Config.Prefix + "-",
			Path:   "routetest",
		}
		if err := relics.AppendRoute(townRoot, route); err != nil {
			t.Fatalf("AppendRoute: %v", err)
		}
	}

	// Save warbands config (normally done by the command)
	if err := config.SaveRigsConfig(rigsPath, rigsConfig); err != nil {
		t.Fatalf("save warbands.json: %v", err)
	}

	// Load routes and verify the new route exists
	townRelicsDir := filepath.Join(townRoot, ".relics")
	routes, err := relics.LoadRoutes(townRelicsDir)
	if err != nil {
		t.Fatalf("LoadRoutes: %v", err)
	}

	// Find route for our warband
	var foundRoute *relics.Route
	for _, r := range routes {
		if r.Prefix == "rt-" {
			foundRoute = &r
			break
		}
	}

	if foundRoute == nil {
		t.Error("route with prefix 'rt-' not found in routes.jsonl")
		t.Logf("routes: %+v", routes)
	} else {
		// The path should point to the warband (or warchief/warband if .relics is tracked in source)
		if !strings.HasPrefix(foundRoute.Path, "routetest") {
			t.Errorf("route path = %q, want prefix 'routetest'", foundRoute.Path)
		}
	}
}

// TestRigAddUpdatesRigsJson verifies that warbands.json is updated
// with the new warband entry.
func TestRigAddUpdatesRigsJson(t *testing.T) {
	mockBdCommand(t)
	townRoot := setupTestTown(t)
	gitURL := createTestGitRepo(t, "jsontest")

	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}

	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	_, err = mgr.AddRig(warband.AddRigOptions{
		Name:        "jsontest",
		GitURL:      gitURL,
		RelicsPrefix: "jt",
	})
	if err != nil {
		t.Fatalf("AddRig: %v", err)
	}

	// Save warbands config (normally done by the command)
	if err := config.SaveRigsConfig(rigsPath, rigsConfig); err != nil {
		t.Fatalf("save warbands.json: %v", err)
	}

	// Reload and verify
	rigsConfig2, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("reload warbands.json: %v", err)
	}

	entry, ok := rigsConfig2.Warbands["jsontest"]
	if !ok {
		t.Error("warband 'jsontest' not found in warbands.json")
		t.Logf("warbands: %+v", rigsConfig2.Warbands)
	} else {
		if entry.GitURL != gitURL {
			t.Errorf("GitURL = %q, want %q", entry.GitURL, gitURL)
		}
		if entry.RelicsConfig == nil {
			t.Error("RelicsConfig is nil")
		} else if entry.RelicsConfig.Prefix != "jt" {
			t.Errorf("RelicsConfig.Prefix = %q, want %q", entry.RelicsConfig.Prefix, "jt")
		}
	}
}

// TestRigAddDerivesPrefix verifies that when no prefix is specified,
// one is derived from the warband name.
func TestRigAddDerivesPrefix(t *testing.T) {
	mockBdCommand(t)
	townRoot := setupTestTown(t)
	gitURL := createTestGitRepo(t, "myproject")

	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}

	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	newRig, err := mgr.AddRig(warband.AddRigOptions{
		Name:   "myproject",
		GitURL: gitURL,
		// No RelicsPrefix - should be derived
	})
	if err != nil {
		t.Fatalf("AddRig: %v", err)
	}

	// For a single-word name like "myproject", the prefix should be first 2 chars
	if newRig.Config.Prefix != "my" {
		t.Errorf("derived prefix = %q, want %q", newRig.Config.Prefix, "my")
	}
}

// TestRigAddCreatesRigConfig verifies that config.json contains
// the correct warband configuration.
func TestRigAddCreatesRigConfig(t *testing.T) {
	mockBdCommand(t)
	townRoot := setupTestTown(t)
	gitURL := createTestGitRepo(t, "configtest")

	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}

	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	_, err = mgr.AddRig(warband.AddRigOptions{
		Name:        "configtest",
		GitURL:      gitURL,
		RelicsPrefix: "ct",
	})
	if err != nil {
		t.Fatalf("AddRig: %v", err)
	}

	// Read and verify config.json
	configPath := filepath.Join(townRoot, "configtest", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config.json: %v", err)
	}

	var rigCfg warband.RigConfig
	if err := json.Unmarshal(data, &rigCfg); err != nil {
		t.Fatalf("parsing config.json: %v", err)
	}

	if rigCfg.Type != "warband" {
		t.Errorf("Type = %q, want 'warband'", rigCfg.Type)
	}
	if rigCfg.Name != "configtest" {
		t.Errorf("Name = %q, want 'configtest'", rigCfg.Name)
	}
	if rigCfg.GitURL != gitURL {
		t.Errorf("GitURL = %q, want %q", rigCfg.GitURL, gitURL)
	}
	if rigCfg.Relics == nil {
		t.Error("Relics config is nil")
	} else if rigCfg.Relics.Prefix != "ct" {
		t.Errorf("Relics.Prefix = %q, want 'ct'", rigCfg.Relics.Prefix)
	}
	if rigCfg.DefaultBranch == "" {
		t.Error("DefaultBranch is empty")
	}
}

// TestRigAddCreatesAgentDirs verifies that agent state files are created.
func TestRigAddCreatesAgentDirs(t *testing.T) {
	mockBdCommand(t)
	townRoot := setupTestTown(t)
	gitURL := createTestGitRepo(t, "agenttest")

	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}

	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	_, err = mgr.AddRig(warband.AddRigOptions{
		Name:        "agenttest",
		GitURL:      gitURL,
		RelicsPrefix: "at",
	})
	if err != nil {
		t.Fatalf("AddRig: %v", err)
	}

	rigPath := filepath.Join(townRoot, "agenttest")

	// Verify agent directories exist (state.json files are no longer created)
	expectedDirs := []string{
		"witness",
		"forge",
		"warchief",
	}

	for _, dir := range expectedDirs {
		path := filepath.Join(rigPath, dir)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %s to exist: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("expected %s to be a directory", dir)
		}
	}
}

// TestRigAddRejectsInvalidNames verifies that warband names with invalid
// characters are rejected.
func TestRigAddRejectsInvalidNames(t *testing.T) {
	mockBdCommand(t)
	townRoot := setupTestTown(t)
	gitURL := createTestGitRepo(t, "validname")

	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}

	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	// Characters that break agent ID parsing (hyphens, dots, spaces)
	// Note: underscores are allowed
	invalidNames := []string{
		"my-warband",       // hyphens break agent ID parsing
		"my.warband",       // dots break parsing
		"my warband",       // spaces are invalid
		"my-multi-warband", // multiple hyphens
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			_, err := mgr.AddRig(warband.AddRigOptions{
				Name:   name,
				GitURL: gitURL,
			})
			if err == nil {
				t.Errorf("AddRig(%q) should have failed", name)
			} else if !strings.Contains(err.Error(), "invalid characters") {
				t.Errorf("AddRig(%q) error = %v, want 'invalid characters'", name, err)
			}
		})
	}
}
