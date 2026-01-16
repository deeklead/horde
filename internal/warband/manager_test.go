package warband

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/git"
)

func setupTestTown(t *testing.T) (string, *config.RigsConfig) {
	t.Helper()
	root := t.TempDir()

	rigsConfig := &config.RigsConfig{
		Version: 1,
		Warbands:    make(map[string]config.RigEntry),
	}

	return root, rigsConfig
}

func writeFakeBD(t *testing.T, script string) string {
	t.Helper()
	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "rl")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	return binDir
}

func assertRelicsDirLog(t *testing.T, logPath, want string) {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading relics dir log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		t.Fatalf("expected relics dir log entries, got none")
	}
	for _, line := range lines {
		if line != want {
			t.Fatalf("RELICS_DIR = %q, want %q", line, want)
		}
	}
}

func createTestRig(t *testing.T, root, name string) {
	t.Helper()

	rigPath := filepath.Join(root, name)
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatalf("mkdir warband: %v", err)
	}

	// Create agent dirs (witness, forge, warchief)
	for _, dir := range AgentDirs {
		dirPath := filepath.Join(rigPath, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// Create some raiders
	raidersDir := filepath.Join(rigPath, "raiders")
	for _, raider := range []string{"Toast", "Cheedo"} {
		if err := os.MkdirAll(filepath.Join(raidersDir, raider), 0755); err != nil {
			t.Fatalf("mkdir raider: %v", err)
		}
	}
	// Create a shared support dir that should not be treated as a raider worktree.
	if err := os.MkdirAll(filepath.Join(raidersDir, ".claude"), 0755); err != nil {
		t.Fatalf("mkdir raiders/.claude: %v", err)
	}
}

func TestDiscoverRigs(t *testing.T) {
	root, rigsConfig := setupTestTown(t)

	// Create test warband
	createTestRig(t, root, "horde")
	rigsConfig.Warbands["horde"] = config.RigEntry{
		GitURL: "git@github.com:test/horde.git",
	}

	manager := NewManager(root, rigsConfig, git.NewGit(root))

	warbands, err := manager.DiscoverRigs()
	if err != nil {
		t.Fatalf("DiscoverRigs: %v", err)
	}

	if len(warbands) != 1 {
		t.Errorf("warbands count = %d, want 1", len(warbands))
	}

	warband := warbands[0]
	if warband.Name != "horde" {
		t.Errorf("Name = %q, want horde", warband.Name)
	}
	if len(warband.Raiders) != 2 {
		t.Errorf("Raiders count = %d, want 2", len(warband.Raiders))
	}
	if slices.Contains(warband.Raiders, ".claude") {
		t.Errorf("expected raiders/.claude to be ignored, got %v", warband.Raiders)
	}
	if !warband.HasWitness {
		t.Error("expected HasWitness = true")
	}
	if !warband.HasForge {
		t.Error("expected HasForge = true")
	}
}

func TestGetRig(t *testing.T) {
	root, rigsConfig := setupTestTown(t)

	createTestRig(t, root, "test-warband")
	rigsConfig.Warbands["test-warband"] = config.RigEntry{
		GitURL: "git@github.com:test/test-warband.git",
	}

	manager := NewManager(root, rigsConfig, git.NewGit(root))

	warband, err := manager.GetRig("test-warband")
	if err != nil {
		t.Fatalf("GetRig: %v", err)
	}

	if warband.Name != "test-warband" {
		t.Errorf("Name = %q, want test-warband", warband.Name)
	}
}

func TestGetRigNotFound(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))

	_, err := manager.GetRig("nonexistent")
	if err != ErrRigNotFound {
		t.Errorf("GetRig = %v, want ErrRigNotFound", err)
	}
}

func TestRigExists(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	rigsConfig.Warbands["exists"] = config.RigEntry{}

	manager := NewManager(root, rigsConfig, git.NewGit(root))

	if !manager.RigExists("exists") {
		t.Error("expected RigExists = true for existing warband")
	}
	if manager.RigExists("nonexistent") {
		t.Error("expected RigExists = false for nonexistent warband")
	}
}

func TestRemoveRig(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	rigsConfig.Warbands["to-remove"] = config.RigEntry{}

	manager := NewManager(root, rigsConfig, git.NewGit(root))

	if err := manager.RemoveRig("to-remove"); err != nil {
		t.Fatalf("RemoveRig: %v", err)
	}

	if manager.RigExists("to-remove") {
		t.Error("warband should not exist after removal")
	}
}

func TestRemoveRigNotFound(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))

	err := manager.RemoveRig("nonexistent")
	if err != ErrRigNotFound {
		t.Errorf("RemoveRig = %v, want ErrRigNotFound", err)
	}
}

func TestAddRig_RejectsInvalidNames(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))

	tests := []struct {
		name      string
		wantError string
	}{
		{"op-baby", `warband name "op-baby" contains invalid characters`},
		{"my.warband", `warband name "my.warband" contains invalid characters`},
		{"my warband", `warband name "my warband" contains invalid characters`},
		{"op-baby-test", `warband name "op-baby-test" contains invalid characters`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.AddRig(AddRigOptions{
				Name:   tt.name,
				GitURL: "git@github.com:test/test.git",
			})
			if err == nil {
				t.Errorf("AddRig(%q) succeeded, want error containing %q", tt.name, tt.wantError)
				return
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("AddRig(%q) error = %q, want error containing %q", tt.name, err.Error(), tt.wantError)
			}
		})
	}
}

func TestListRigNames(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	rigsConfig.Warbands["rig1"] = config.RigEntry{}
	rigsConfig.Warbands["rig2"] = config.RigEntry{}

	manager := NewManager(root, rigsConfig, git.NewGit(root))

	names := manager.ListRigNames()
	if len(names) != 2 {
		t.Errorf("names count = %d, want 2", len(names))
	}
}

func TestRigSummary(t *testing.T) {
	warband := &Warband{
		Name:        "test",
		Raiders:    []string{"a", "b", "c"},
		HasWitness:  true,
		HasForge: false,
	}

	summary := warband.Summary()

	if summary.Name != "test" {
		t.Errorf("Name = %q, want test", summary.Name)
	}
	if summary.RaiderCount != 3 {
		t.Errorf("RaiderCount = %d, want 3", summary.RaiderCount)
	}
	if !summary.HasWitness {
		t.Error("expected HasWitness = true")
	}
	if summary.HasForge {
		t.Error("expected HasForge = false")
	}
}

func TestEnsureGitignoreEntry_AddsEntry(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))

	gitignorePath := filepath.Join(root, ".gitignore")

	if err := manager.ensureGitignoreEntry(gitignorePath, ".test-entry/"); err != nil {
		t.Fatalf("ensureGitignoreEntry: %v", err)
	}

	content, _ := os.ReadFile(gitignorePath)
	if string(content) != ".test-entry/\n" {
		t.Errorf("content = %q, want .test-entry/", string(content))
	}
}

func TestEnsureGitignoreEntry_DoesNotDuplicate(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))

	gitignorePath := filepath.Join(root, ".gitignore")

	// Pre-populate with the entry
	if err := os.WriteFile(gitignorePath, []byte(".test-entry/\n"), 0644); err != nil {
		t.Fatalf("writing .gitignore: %v", err)
	}

	if err := manager.ensureGitignoreEntry(gitignorePath, ".test-entry/"); err != nil {
		t.Fatalf("ensureGitignoreEntry: %v", err)
	}

	content, _ := os.ReadFile(gitignorePath)
	if string(content) != ".test-entry/\n" {
		t.Errorf("content = %q, want single .test-entry/", string(content))
	}
}

func TestEnsureGitignoreEntry_AppendsToExisting(t *testing.T) {
	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))

	gitignorePath := filepath.Join(root, ".gitignore")

	// Pre-populate with existing entries
	if err := os.WriteFile(gitignorePath, []byte("node_modules/\n*.log\n"), 0644); err != nil {
		t.Fatalf("writing .gitignore: %v", err)
	}

	if err := manager.ensureGitignoreEntry(gitignorePath, ".test-entry/"); err != nil {
		t.Fatalf("ensureGitignoreEntry: %v", err)
	}

	content, _ := os.ReadFile(gitignorePath)
	expected := "node_modules/\n*.log\n.test-entry/\n"
	if string(content) != expected {
		t.Errorf("content = %q, want %q", string(content), expected)
	}
}

func TestInitRelics_TrackedRelics_CreatesRedirect(t *testing.T) {
	t.Parallel()
	// When the cloned repo has tracked relics (warchief/warband/.relics exists),
	// initRelics should create a redirect file at <warband>/.relics/redirect
	// pointing to warchief/warband/.relics instead of creating a local database.
	rigPath := t.TempDir()

	// Simulate tracked relics in the cloned repo
	warchiefRelicsDir := filepath.Join(rigPath, "warchief", "warband", ".relics")
	if err := os.MkdirAll(warchiefRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir warchief relics: %v", err)
	}
	// Create a config file to simulate a real relics directory
	if err := os.WriteFile(filepath.Join(warchiefRelicsDir, "config.yaml"), []byte("prefix: gt\n"), 0644); err != nil {
		t.Fatalf("write warchief config: %v", err)
	}

	manager := &Manager{}
	if err := manager.initRelics(rigPath, "hd"); err != nil {
		t.Fatalf("initRelics: %v", err)
	}

	// Verify redirect file was created
	redirectPath := filepath.Join(rigPath, ".relics", "redirect")
	content, err := os.ReadFile(redirectPath)
	if err != nil {
		t.Fatalf("reading redirect file: %v", err)
	}

	expected := "warchief/warband/.relics\n"
	if string(content) != expected {
		t.Errorf("redirect content = %q, want %q", string(content), expected)
	}

	// Verify no local database was created (no config.yaml at warband level)
	rigConfigPath := filepath.Join(rigPath, ".relics", "config.yaml")
	if _, err := os.Stat(rigConfigPath); !os.IsNotExist(err) {
		t.Errorf("expected no config.yaml at warband level when using redirect, but it exists")
	}
}

func TestInitRelics_LocalRelics_CreatesDatabase(t *testing.T) {
	// Cannot use t.Parallel() due to t.Setenv
	// When the cloned repo does NOT have tracked relics (no warchief/warband/.relics),
	// initRelics should create a local database at <warband>/.relics/
	rigPath := t.TempDir()

	// Create warchief/warband directory but WITHOUT .relics (no tracked relics)
	warchiefRigDir := filepath.Join(rigPath, "warchief", "warband")
	if err := os.MkdirAll(warchiefRigDir, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband: %v", err)
	}

	// Use fake rl that succeeds
	script := `#!/usr/bin/env bash
set -e
if [[ "$1" == "init" ]]; then
  # Simulate successful rl init
  exit 0
fi
exit 0
`
	binDir := writeFakeBD(t, script)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := &Manager{}
	if err := manager.initRelics(rigPath, "hd"); err != nil {
		t.Fatalf("initRelics: %v", err)
	}

	// Verify NO redirect file was created
	redirectPath := filepath.Join(rigPath, ".relics", "redirect")
	if _, err := os.Stat(redirectPath); !os.IsNotExist(err) {
		t.Errorf("expected no redirect file for local relics, but it exists")
	}

	// Verify .relics directory was created
	relicsDir := filepath.Join(rigPath, ".relics")
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		t.Errorf("expected .relics directory to be created")
	}
}

func TestInitRelicsWritesConfigOnFailure(t *testing.T) {
	rigPath := t.TempDir()
	relicsDir := filepath.Join(rigPath, ".relics")

	script := `#!/usr/bin/env bash
set -e
if [[ -n "$RELICS_DIR_LOG" ]]; then
  echo "${RELICS_DIR:-<unset>}" >> "$RELICS_DIR_LOG"
fi
cmd="$1"
shift
if [[ "$cmd" == "init" ]]; then
  echo "bd init failed" >&2
  exit 1
fi
echo "unexpected command: $cmd" >&2
exit 1
`

	binDir := writeFakeBD(t, script)
	relicsDirLog := filepath.Join(t.TempDir(), "relics-dir.log")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RELICS_DIR_LOG", relicsDirLog)

	manager := &Manager{}
	if err := manager.initRelics(rigPath, "hd"); err != nil {
		t.Fatalf("initRelics: %v", err)
	}

	configPath := filepath.Join(relicsDir, "config.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config.yaml: %v", err)
	}
	if string(config) != "prefix: gt\n" {
		t.Fatalf("config.yaml = %q, want %q", string(config), "prefix: gt\n")
	}
	assertRelicsDirLog(t, relicsDirLog, relicsDir)
}

func TestInitAgentRelicsUsesRigRelicsDir(t *testing.T) {
	// Warband-level agent relics (witness, forge) are stored in warband relics.
	// Encampment-level agents (warchief, shaman) are created by hd install in encampment relics.
	// This test verifies that warband agent relics are created in the warband directory,
	// using the resolved warband relics directory for RELICS_DIR.
	townRoot := t.TempDir()
	rigPath := filepath.Join(townRoot, "testrip")
	rigRelicsDir := filepath.Join(rigPath, ".relics")

	if err := os.MkdirAll(rigRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir warband relics dir: %v", err)
	}

	// Track which agent IDs were created
	var createdAgents []string

	script := `#!/usr/bin/env bash
set -e
if [[ -n "$RELICS_DIR_LOG" ]]; then
  echo "${RELICS_DIR:-<unset>}" >> "$RELICS_DIR_LOG"
fi
if [[ "$1" == "--no-daemon" ]]; then
  shift
fi
if [[ "$1" == "--allow-stale" ]]; then
  shift
fi
cmd="$1"
shift
case "$cmd" in
  show)
    # Return empty to indicate agent doesn't exist yet
    echo "[]"
    ;;
  create)
    id=""
    title=""
    for arg in "$@"; do
      case "$arg" in
        --id=*) id="${arg#--id=}" ;;
        --title=*) title="${arg#--title=}" ;;
      esac
    done
    # Log the created agent ID for verification
    echo "$id" >> "$AGENT_LOG"
    printf '{"id":"%s","title":"%s","description":"","issue_type":"agent"}' "$id" "$title"
    ;;
  slot)
    # Accept slot commands
    ;;
  *)
    echo "unexpected command: $cmd" >&2
    exit 1
    ;;
esac
`

	binDir := writeFakeBD(t, script)
	agentLog := filepath.Join(t.TempDir(), "agents.log")
	relicsDirLog := filepath.Join(t.TempDir(), "relics-dir.log")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGENT_LOG", agentLog)
	t.Setenv("RELICS_DIR_LOG", relicsDirLog)
	t.Setenv("RELICS_DIR", "") // Clear any existing RELICS_DIR

	manager := &Manager{townRoot: townRoot}
	if err := manager.initAgentRelics(rigPath, "demo", "hd"); err != nil {
		t.Fatalf("initAgentRelics: %v", err)
	}

	// Verify the expected warband-level agents were created
	data, err := os.ReadFile(agentLog)
	if err != nil {
		t.Fatalf("reading agent log: %v", err)
	}
	createdAgents = strings.Split(strings.TrimSpace(string(data)), "\n")

	// Should create witness and forge for the warband
	expectedAgents := map[string]bool{
		"gt-demo-witness":  false,
		"gt-demo-forge": false,
	}

	for _, id := range createdAgents {
		if _, ok := expectedAgents[id]; ok {
			expectedAgents[id] = true
		}
	}

	for id, found := range expectedAgents {
		if !found {
			t.Errorf("expected agent %s was not created", id)
		}
	}
	assertRelicsDirLog(t, relicsDirLog, rigRelicsDir)
}

func TestIsValidRelicsPrefix(t *testing.T) {
	tests := []struct {
		prefix string
		want   bool
	}{
		// Valid prefixes
		{"hd", true},
		{"rl", true},
		{"hq", true},
		{"horde", true},
		{"myProject", true},
		{"my-project", true},
		{"a", true},
		{"A", true},
		{"test123", true},
		{"a1b2c3", true},
		{"a-b-c", true},

		// Invalid prefixes
		{"", false},                      // empty
		{"1abc", false},                  // starts with number
		{"-abc", false},                  // starts with hyphen
		{"abc def", false},               // contains space
		{"abc;ls", false},                // shell injection attempt
		{"$(whoami)", false},             // command substitution
		{"`id`", false},                  // backtick command
		{"abc|cat", false},               // pipe
		{"../etc/passwd", false},         // path traversal
		{"aaaaaaaaaaaaaaaaaaaaa", false}, // too long (21 chars, >20 limit)
		{"valid-but-with-$var", false},   // variable reference
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			got := isValidRelicsPrefix(tt.prefix)
			if got != tt.want {
				t.Errorf("isValidRelicsPrefix(%q) = %v, want %v", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestInitRelicsRejectsInvalidPrefix(t *testing.T) {
	rigPath := t.TempDir()
	manager := &Manager{}

	tests := []string{
		"",
		"$(whoami)",
		"abc;rm -rf /",
		"../etc",
		"123",
	}

	for _, prefix := range tests {
		t.Run(prefix, func(t *testing.T) {
			err := manager.initRelics(rigPath, prefix)
			if err == nil {
				t.Errorf("initRelics(%q) should have failed", prefix)
			}
			if !strings.Contains(err.Error(), "invalid relics prefix") {
				t.Errorf("initRelics(%q) error = %q, want error containing 'invalid relics prefix'", prefix, err.Error())
			}
		})
	}
}

func TestDeriveRelicsPrefix(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		// Compound words with common suffixes should split
		{"horde", "hd"},       // gas + encampment
		{"nashville", "nv"},     // nash + ville
		{"bridgeport", "bp"},    // bridge + port
		{"someplace", "sp"},     // some + place
		{"greenland", "gl"},     // green + land
		{"springfield", "sf"},   // spring + field
		{"hollywood", "hw"},     // holly + wood
		{"oxford", "of"},        // ox + ford

		// Hyphenated names
		{"my-project", "mp"},
		{"horde", "hd"},
		{"some-long-name", "sln"},

		// Underscored names
		{"my_project", "mp"},

		// Short single words (use the whole name)
		{"foo", "foo"},
		{"bar", "bar"},
		{"ab", "ab"},

		// Longer single words without known suffixes (first 2 chars)
		{"myrig", "my"},
		{"awesome", "aw"},
		{"coolrig", "co"},

		// With language suffixes stripped
		{"myproject-py", "my"},
		{"myproject-go", "my"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveRelicsPrefix(tt.name)
			if got != tt.want {
				t.Errorf("deriveRelicsPrefix(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestSplitCompoundWord(t *testing.T) {
	tests := []struct {
		word string
		want []string
	}{
		// Known suffixes
		{"horde", []string{"gas", "encampment"}},
		{"nashville", []string{"nash", "ville"}},
		{"bridgeport", []string{"bridge", "port"}},
		{"someplace", []string{"some", "place"}},
		{"greenland", []string{"green", "land"}},
		{"springfield", []string{"spring", "field"}},
		{"hollywood", []string{"holly", "wood"}},
		{"oxford", []string{"ox", "ford"}},

		// Just the suffix (should not split)
		{"encampment", []string{"encampment"}},
		{"ville", []string{"ville"}},

		// No known suffix
		{"myrig", []string{"myrig"}},
		{"awesome", []string{"awesome"}},

		// Empty prefix would result (should not split)
		// Note: "encampment" itself shouldn't split to ["", "encampment"]
	}

	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			got := splitCompoundWord(tt.word)
			if len(got) != len(tt.want) {
				t.Errorf("splitCompoundWord(%q) = %v, want %v", tt.word, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitCompoundWord(%q)[%d] = %q, want %q", tt.word, i, got[i], tt.want[i])
				}
			}
		})
	}
}
