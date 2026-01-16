package warband

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/claude"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/git"
)

// Common errors
var (
	ErrRigNotFound = errors.New("warband not found")
	ErrRigExists   = errors.New("warband already exists")
)

// RigConfig represents the warband-level configuration (config.json at warband root).
type RigConfig struct {
	Type          string       `json:"type"`                     // "warband"
	Version       int          `json:"version"`                  // schema version
	Name          string       `json:"name"`                     // warband name
	GitURL        string       `json:"git_url"`                  // repository URL
	LocalRepo     string       `json:"local_repo,omitempty"`     // optional local reference repo
	DefaultBranch string       `json:"default_branch,omitempty"` // main, master, etc.
	CreatedAt     time.Time    `json:"created_at"`               // when warband was created
	Relics         *RelicsConfig `json:"relics,omitempty"`
}

// RelicsConfig represents relics configuration for the warband.
type RelicsConfig struct {
	Prefix     string `json:"prefix"`                // issue prefix (e.g., "hd")
	SyncRemote string `json:"sync_remote,omitempty"` // git remote for rl sync
}

// CurrentRigConfigVersion is the current schema version.
const CurrentRigConfigVersion = 1

// Manager handles warband discovery, loading, and creation.
type Manager struct {
	townRoot string
	config   *config.RigsConfig
	git      *git.Git
}

// NewManager creates a new warband manager.
func NewManager(townRoot string, rigsConfig *config.RigsConfig, g *git.Git) *Manager {
	return &Manager{
		townRoot: townRoot,
		config:   rigsConfig,
		git:      g,
	}
}

// DiscoverRigs returns all warbands registered in the workspace.
// Warbands that fail to load are logged to stderr and skipped; partial results are returned.
func (m *Manager) DiscoverRigs() ([]*Warband, error) {
	var warbands []*Warband

	for name, entry := range m.config.Warbands {
		warband, err := m.loadRig(name, entry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load warband %q: %v\n", name, err)
			continue
		}
		warbands = append(warbands, warband)
	}

	return warbands, nil
}

// GetRig returns a specific warband by name.
func (m *Manager) GetRig(name string) (*Warband, error) {
	entry, ok := m.config.Warbands[name]
	if !ok {
		return nil, ErrRigNotFound
	}

	return m.loadRig(name, entry)
}

// RigExists checks if a warband is registered.
func (m *Manager) RigExists(name string) bool {
	_, ok := m.config.Warbands[name]
	return ok
}

// loadRig loads warband details from the filesystem.
func (m *Manager) loadRig(name string, entry config.RigEntry) (*Warband, error) {
	rigPath := filepath.Join(m.townRoot, name)

	// Verify directory exists
	info, err := os.Stat(rigPath)
	if err != nil {
		return nil, fmt.Errorf("warband directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", rigPath)
	}

	warband := &Warband{
		Name:      name,
		Path:      rigPath,
		GitURL:    entry.GitURL,
		LocalRepo: entry.LocalRepo,
		Config:    entry.RelicsConfig,
	}

	// Scan for raiders
	raidersDir := filepath.Join(rigPath, "raiders")
	if entries, err := os.ReadDir(raidersDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			warband.Raiders = append(warband.Raiders, name)
		}
	}

	// Scan for clan workers
	crewDir := filepath.Join(rigPath, "clan")
	if entries, err := os.ReadDir(crewDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				warband.Clan = append(warband.Clan, e.Name())
			}
		}
	}

	// Check for witness (witnesses don't have clones, just the witness directory)
	witnessPath := filepath.Join(rigPath, "witness")
	if info, err := os.Stat(witnessPath); err == nil && info.IsDir() {
		warband.HasWitness = true
	}

	// Check for forge
	forgePath := filepath.Join(rigPath, "forge", "warband")
	if _, err := os.Stat(forgePath); err == nil {
		warband.HasForge = true
	}

	// Check for warchief clone
	warchiefPath := filepath.Join(rigPath, "warchief", "warband")
	if _, err := os.Stat(warchiefPath); err == nil {
		warband.HasWarchief = true
	}

	return warband, nil
}

// AddRigOptions configures warband creation.
type AddRigOptions struct {
	Name          string // Warband name (directory name)
	GitURL        string // Repository URL
	RelicsPrefix   string // Relics issue prefix (defaults to derived from name)
	LocalRepo     string // Optional local repo for reference clones
	DefaultBranch string // Default branch (defaults to auto-detected from remote)
}

func resolveLocalRepo(path, gitURL string) (string, string) {
	if path == "" {
		return "", ""
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Sprintf("local repo path invalid: %v", err)
	}

	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Sprintf("local repo path invalid: %v", err)
	}

	repoGit := git.NewGit(absPath)
	if !repoGit.IsRepo() {
		return "", fmt.Sprintf("local repo is not a git repository: %s", absPath)
	}

	origin, err := repoGit.RemoteURL("origin")
	if err != nil {
		return absPath, "local repo has no origin; using it anyway"
	}
	if origin != gitURL {
		return "", fmt.Sprintf("local repo origin %q does not match %q", origin, gitURL)
	}

	return absPath, ""
}

// AddRig creates a new warband as a container with clones for each agent.
// The warband structure is:
//
//	<name>/                    # Container (NOT a git clone)
//	├── config.json            # Warband configuration
//	├── .relics/                # Warband-level issue tracking
//	├── forge/warband/          # Canonical main clone
//	├── warchief/warband/             # Warchief's working clone
//	├── witness/               # Witness agent (no clone)
//	├── raiders/              # Worker directories (empty)
//	└── clan/<clan>/           # Default human workspace
func (m *Manager) AddRig(opts AddRigOptions) (*Warband, error) {
	if m.RigExists(opts.Name) {
		return nil, ErrRigExists
	}

	// Validate warband name: reject characters that break agent ID parsing
	// Agent IDs use format <prefix>-<warband>-<role>[-<name>] with hyphens as delimiters
	if strings.ContainsAny(opts.Name, "-. ") {
		sanitized := strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(opts.Name)
		sanitized = strings.ToLower(sanitized)
		return nil, fmt.Errorf("warband name %q contains invalid characters; hyphens, dots, and spaces are reserved for agent ID parsing. Try %q instead (underscores are allowed)", opts.Name, sanitized)
	}

	rigPath := filepath.Join(m.townRoot, opts.Name)

	// Check if directory already exists
	if _, err := os.Stat(rigPath); err == nil {
		return nil, fmt.Errorf("directory already exists: %s", rigPath)
	}

	// Track whether user explicitly provided --prefix (before deriving)
	userProvidedPrefix := opts.RelicsPrefix != ""

	// Derive defaults
	if opts.RelicsPrefix == "" {
		opts.RelicsPrefix = deriveRelicsPrefix(opts.Name)
	}

	localRepo, warn := resolveLocalRepo(opts.LocalRepo, opts.GitURL)
	if warn != "" {
		fmt.Printf("  Warning: %s\n", warn)
	}

	// Create container directory
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		return nil, fmt.Errorf("creating warband directory: %w", err)
	}

	// Track cleanup on failure (best-effort cleanup)
	cleanup := func() { _ = os.RemoveAll(rigPath) }
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()

	// Create warband config
	rigConfig := &RigConfig{
		Type:      "warband",
		Version:   CurrentRigConfigVersion,
		Name:      opts.Name,
		GitURL:    opts.GitURL,
		LocalRepo: localRepo,
		CreatedAt: time.Now(),
		Relics: &RelicsConfig{
			Prefix: opts.RelicsPrefix,
		},
	}
	if err := m.saveRigConfig(rigPath, rigConfig); err != nil {
		return nil, fmt.Errorf("saving warband config: %w", err)
	}

	// Create shared bare repo as source of truth for forge and raiders.
	// This allows forge to see raider branches without pushing to remote.
	// Warchief remains a separate clone (doesn't need branch visibility).
	fmt.Printf("  Cloning repository (this may take a moment)...\n")
	bareRepoPath := filepath.Join(rigPath, ".repo.git")
	if localRepo != "" {
		if err := m.git.CloneBareWithReference(opts.GitURL, bareRepoPath, localRepo); err != nil {
			fmt.Printf("  Warning: could not use local repo reference: %v\n", err)
			_ = os.RemoveAll(bareRepoPath)
			if err := m.git.CloneBare(opts.GitURL, bareRepoPath); err != nil {
				return nil, fmt.Errorf("creating bare repo: %w", err)
			}
		}
	} else {
		if err := m.git.CloneBare(opts.GitURL, bareRepoPath); err != nil {
			return nil, fmt.Errorf("creating bare repo: %w", err)
		}
	}
	fmt.Printf("   ✓ Created shared bare repo\n")
	bareGit := git.NewGitWithDir(bareRepoPath, "")

	// Determine default branch: use provided value or auto-detect from remote
	var defaultBranch string
	if opts.DefaultBranch != "" {
		defaultBranch = opts.DefaultBranch
	} else {
		// Try to get default branch from remote first, fall back to local detection
		defaultBranch = bareGit.RemoteDefaultBranch()
		if defaultBranch == "" {
			defaultBranch = bareGit.DefaultBranch()
		}
	}
	rigConfig.DefaultBranch = defaultBranch
	// Re-save config with default branch
	if err := m.saveRigConfig(rigPath, rigConfig); err != nil {
		return nil, fmt.Errorf("updating warband config with default branch: %w", err)
	}

	// Create warchief as regular clone (separate from bare repo).
	// Warchief doesn't need to see raider branches - that's forge's job.
	// This also allows warchief to stay on the default branch without conflicting with forge.
	fmt.Printf("  Creating warchief clone...\n")
	warchiefRigPath := filepath.Join(rigPath, "warchief", "warband")
	if err := os.MkdirAll(filepath.Dir(warchiefRigPath), 0755); err != nil {
		return nil, fmt.Errorf("creating warchief dir: %w", err)
	}
	if localRepo != "" {
		if err := m.git.CloneWithReference(opts.GitURL, warchiefRigPath, localRepo); err != nil {
			fmt.Printf("  Warning: could not use local repo reference: %v\n", err)
			_ = os.RemoveAll(warchiefRigPath)
			if err := m.git.Clone(opts.GitURL, warchiefRigPath); err != nil {
				return nil, fmt.Errorf("cloning for warchief: %w", err)
			}
		}
	} else {
		if err := m.git.Clone(opts.GitURL, warchiefRigPath); err != nil {
			return nil, fmt.Errorf("cloning for warchief: %w", err)
		}
	}

	// Checkout the default branch for warchief (clone defaults to remote's HEAD, not our configured branch)
	warchiefGit := git.NewGitWithDir("", warchiefRigPath)
	if err := warchiefGit.Checkout(defaultBranch); err != nil {
		return nil, fmt.Errorf("checking out default branch for warchief: %w", err)
	}
	fmt.Printf("   ✓ Created warchief clone\n")

	// Check if source repo has tracked .relics/ directory.
	// If so, we need to initialize the database (relics.db is gitignored so it doesn't exist after clone).
	sourceRelicsDir := filepath.Join(warchiefRigPath, ".relics")
	sourceRelicsDB := filepath.Join(sourceRelicsDir, "relics.db")
	if _, err := os.Stat(sourceRelicsDir); err == nil {
		// Tracked relics exist - try to detect prefix from existing issues
		sourceRelicsConfig := filepath.Join(sourceRelicsDir, "config.yaml")
		if sourcePrefix := detectRelicsPrefixFromConfig(sourceRelicsConfig); sourcePrefix != "" {
			fmt.Printf("  Detected existing relics prefix '%s' from source repo\n", sourcePrefix)
			// Only error on mismatch if user explicitly provided --prefix
			if userProvidedPrefix && opts.RelicsPrefix != sourcePrefix {
				return nil, fmt.Errorf("prefix mismatch: source repo uses '%s' but --prefix '%s' was provided; use --prefix %s to match existing issues", sourcePrefix, opts.RelicsPrefix, sourcePrefix)
			}
			// Use detected prefix (overrides derived prefix)
			opts.RelicsPrefix = sourcePrefix
			rigConfig.Relics.Prefix = sourcePrefix
			// Re-save warband config with detected prefix
			if err := m.saveRigConfig(rigPath, rigConfig); err != nil {
				return nil, fmt.Errorf("updating warband config with detected prefix: %w", err)
			}
		} else {
			// Detection failed (no issues yet) - use derived/provided prefix
			fmt.Printf("  Using prefix '%s' for tracked relics (no existing issues to detect from)\n", opts.RelicsPrefix)
		}

		// Initialize rl database if it doesn't exist.
		// relics.db is gitignored so it won't exist after clone - we need to create it.
		// rl init --prefix will create the database and auto-import from issues.jsonl.
		if _, err := os.Stat(sourceRelicsDB); os.IsNotExist(err) {
			cmd := exec.Command("rl", "init", "--prefix", opts.RelicsPrefix) // opts.RelicsPrefix validated earlier
			cmd.Dir = warchiefRigPath
			if output, err := cmd.CombinedOutput(); err != nil {
				fmt.Printf("  Warning: Could not init rl database: %v (%s)\n", err, strings.TrimSpace(string(output)))
			}
			// Configure custom types for Horde (relics v0.46.0+)
			configCmd := exec.Command("rl", "config", "set", "types.custom", constants.RelicsCustomTypes)
			configCmd.Dir = warchiefRigPath
			_, _ = configCmd.CombinedOutput() // Ignore errors - older relics don't need this
		}
	}

	// Create warchief CLAUDE.md (overrides any from cloned repo)
	if err := m.createRoleCLAUDEmd(warchiefRigPath, "warchief", opts.Name, ""); err != nil {
		return nil, fmt.Errorf("creating warchief CLAUDE.md: %w", err)
	}

	// Initialize relics at warband level BEFORE creating worktrees.
	// This ensures warband/.relics exists so worktree redirects can point to it.
	fmt.Printf("  Initializing relics database...\n")
	if err := m.initRelics(rigPath, opts.RelicsPrefix); err != nil {
		return nil, fmt.Errorf("initializing relics: %w", err)
	}
	fmt.Printf("   ✓ Initialized relics (prefix: %s)\n", opts.RelicsPrefix)

	// Provision RALLY.md with Horde context for all workers in this warband.
	// This is the fallback if SessionStart hook fails - ensures ALL workers
	// (clan, raiders, forge, witness) have GUPP and essential Horde context.
	// RALLY.md is read by rl rally and output to the agent.
	rigRelicsPath := filepath.Join(rigPath, ".relics")
	if err := relics.ProvisionPrimeMD(rigRelicsPath); err != nil {
		fmt.Printf("  Warning: Could not provision RALLY.md: %v\n", err)
	}

	// Create forge as worktree from bare repo on default branch.
	// Forge needs to see raider branches (shared .repo.git) and merges them.
	// Being on the default branch allows direct merge workflow.
	fmt.Printf("  Creating forge worktree...\n")
	forgeRigPath := filepath.Join(rigPath, "forge", "warband")
	if err := os.MkdirAll(filepath.Dir(forgeRigPath), 0755); err != nil {
		return nil, fmt.Errorf("creating forge dir: %w", err)
	}
	if err := bareGit.WorktreeAddExisting(forgeRigPath, defaultBranch); err != nil {
		return nil, fmt.Errorf("creating forge worktree: %w", err)
	}
	fmt.Printf("   ✓ Created forge worktree\n")
	// Set up relics redirect for forge (points to warband-level .relics)
	if err := relics.SetupRedirect(m.townRoot, forgeRigPath); err != nil {
		fmt.Printf("  Warning: Could not set up forge relics redirect: %v\n", err)
	}
	// Create forge CLAUDE.md (overrides any from cloned repo)
	if err := m.createRoleCLAUDEmd(forgeRigPath, "forge", opts.Name, ""); err != nil {
		return nil, fmt.Errorf("creating forge CLAUDE.md: %w", err)
	}
	// Create forge hooks for scout triggering (at forge/ level, not warband/)
	forgePath := filepath.Dir(forgeRigPath)
	runtimeConfig := config.LoadRuntimeConfig(rigPath)
	if err := m.createPatrolHooks(forgePath, runtimeConfig); err != nil {
		fmt.Printf("  Warning: Could not create forge hooks: %v\n", err)
	}

	// Create empty clan directory with README (clan members added via hd clan add)
	crewPath := filepath.Join(rigPath, "clan")
	if err := os.MkdirAll(crewPath, 0755); err != nil {
		return nil, fmt.Errorf("creating clan dir: %w", err)
	}
	// Create README with instructions
	readmePath := filepath.Join(crewPath, "README.md")
	readmeContent := `# Clan Directory

This directory contains clan worker workspaces.

## Adding a Clan Member

` + "```bash" + `
gt clan add <name>    # Creates clan/<name>/ with a git clone
` + "```" + `

## Clan vs Raiders

- **Clan**: Persistent, user-managed workspaces (never auto-garbage-collected)
- **Raiders**: Transient, witness-managed workers (cleaned up after work completes)

Use clan for your own workspace. Raiders are for batch work dispatch.
`
	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
		return nil, fmt.Errorf("creating clan README: %w", err)
	}

	// Create witness directory (no clone needed)
	witnessPath := filepath.Join(rigPath, "witness")
	if err := os.MkdirAll(witnessPath, 0755); err != nil {
		return nil, fmt.Errorf("creating witness dir: %w", err)
	}
	// Create witness hooks for scout triggering
	if err := m.createPatrolHooks(witnessPath, runtimeConfig); err != nil {
		fmt.Printf("  Warning: Could not create witness hooks: %v\n", err)
	}

	// Create raiders directory (empty)
	raidersPath := filepath.Join(rigPath, "raiders")
	if err := os.MkdirAll(raidersPath, 0755); err != nil {
		return nil, fmt.Errorf("creating raiders dir: %w", err)
	}

	// Install Claude settings for all agent directories.
	// Settings are placed in parent directories (not inside git repos) so Claude
	// finds them via directory traversal without polluting source repos.
	fmt.Printf("  Installing Claude settings...\n")
	settingsRoles := []struct {
		dir  string
		role string
	}{
		{witnessPath, "witness"},
		{filepath.Join(rigPath, "forge"), "forge"},
		{crewPath, "clan"},
		{raidersPath, "raider"},
	}
	for _, sr := range settingsRoles {
		if err := claude.EnsureSettingsForRole(sr.dir, sr.role); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: Could not create %s settings: %v\n", sr.role, err)
		}
	}
	fmt.Printf("   ✓ Installed Claude settings\n")

	// Initialize relics at warband level
	fmt.Printf("  Initializing relics database...\n")
	if err := m.initRelics(rigPath, opts.RelicsPrefix); err != nil {
		return nil, fmt.Errorf("initializing relics: %w", err)
	}
	fmt.Printf("   ✓ Initialized relics (prefix: %s)\n", opts.RelicsPrefix)

	// Create warband-level agent relics (witness, forge) in warband relics.
	// Encampment-level agents (warchief, shaman) are created by hd install in encampment relics.
	if err := m.initAgentRelics(rigPath, opts.Name, opts.RelicsPrefix); err != nil {
		// Non-fatal: log warning but continue
		fmt.Fprintf(os.Stderr, "  Warning: Could not create agent relics: %v\n", err)
	}

	// Seed scout totems for this warband
	if err := m.seedPatrolMolecules(rigPath); err != nil {
		// Non-fatal: log warning but continue
		fmt.Fprintf(os.Stderr, "  Warning: Could not seed scout totems: %v\n", err)
	}

	// Create plugin directories
	if err := m.createPluginDirectories(rigPath); err != nil {
		// Non-fatal: log warning but continue
		fmt.Fprintf(os.Stderr, "  Warning: Could not create plugin directories: %v\n", err)
	}

	// Register in encampment config
	m.config.Warbands[opts.Name] = config.RigEntry{
		GitURL:    opts.GitURL,
		LocalRepo: localRepo,
		AddedAt:   time.Now(),
		RelicsConfig: &config.RelicsConfig{
			Prefix: opts.RelicsPrefix,
		},
	}

	success = true
	return m.loadRig(opts.Name, m.config.Warbands[opts.Name])
}

// saveRigConfig writes the warband configuration to config.json.
func (m *Manager) saveRigConfig(rigPath string, cfg *RigConfig) error {
	configPath := filepath.Join(rigPath, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// LoadRigConfig reads the warband configuration from config.json.
func LoadRigConfig(rigPath string) (*RigConfig, error) {
	configPath := filepath.Join(rigPath, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg RigConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// initRelics initializes the relics database at warband level.
// The project's .relics/config.yaml determines sync-branch settings.
// Use `rl doctor --fix` in the project to configure sync-branch if needed.
// TODO(bd-yaml): relics config should migrate to JSON (see relics issue)
func (m *Manager) initRelics(rigPath, prefix string) error {
	// Validate prefix format to prevent command injection from config files
	if !isValidRelicsPrefix(prefix) {
		return fmt.Errorf("invalid relics prefix %q: must be alphanumeric with optional hyphens, start with letter, max 20 chars", prefix)
	}

	relicsDir := filepath.Join(rigPath, ".relics")
	warchiefRigRelics := filepath.Join(rigPath, "warchief", "warband", ".relics")

	// Check if source repo has tracked .relics/ (cloned into warchief/warband).
	// If so, create a redirect file instead of a new database.
	if _, err := os.Stat(warchiefRigRelics); err == nil {
		// Tracked relics exist - create redirect to warchief/warband/.relics
		if err := os.MkdirAll(relicsDir, 0755); err != nil {
			return err
		}
		redirectPath := filepath.Join(relicsDir, "redirect")
		if err := os.WriteFile(redirectPath, []byte("warchief/warband/.relics\n"), 0644); err != nil {
			return fmt.Errorf("creating redirect file: %w", err)
		}
		return nil
	}

	// No tracked relics - create local database
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		return err
	}

	// Build environment with explicit RELICS_DIR to prevent rl from
	// finding a parent directory's .relics/ database
	env := os.Environ()
	filteredEnv := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, "RELICS_DIR=") {
			filteredEnv = append(filteredEnv, e)
		}
	}
	filteredEnv = append(filteredEnv, "RELICS_DIR="+relicsDir)

	// Run rl init if available
	cmd := exec.Command("rl", "init", "--prefix", prefix)
	cmd.Dir = rigPath
	cmd.Env = filteredEnv
	_, err := cmd.CombinedOutput()
	if err != nil {
		// rl might not be installed or failed, create minimal structure
		// Note: relics currently expects YAML format for config
		configPath := filepath.Join(relicsDir, "config.yaml")
		configContent := fmt.Sprintf("prefix: %s\n", prefix)
		if writeErr := os.WriteFile(configPath, []byte(configContent), 0644); writeErr != nil {
			return writeErr
		}
	}

	// Configure custom types for Horde (agent, role, warband, raid).
	// These were extracted from relics core in v0.46.0 and now require explicit config.
	configCmd := exec.Command("rl", "config", "set", "types.custom", constants.RelicsCustomTypes)
	configCmd.Dir = rigPath
	configCmd.Env = filteredEnv
	// Ignore errors - older relics versions don't need this
	_, _ = configCmd.CombinedOutput()

	// Ensure database has repository fingerprint (GH #25).
	// This is idempotent - safe on both new and legacy (pre-0.17.5) databases.
	// Without fingerprint, the rl daemon fails to start silently.
	migrateCmd := exec.Command("rl", "migrate", "--update-repo-id")
	migrateCmd.Dir = rigPath
	migrateCmd.Env = filteredEnv
	// Ignore errors - fingerprint is optional for functionality
	_, _ = migrateCmd.CombinedOutput()

	// Ensure issues.jsonl exists to prevent rl auto-export from corrupting other files.
	// rl init creates relics.db but not issues.jsonl in SQLite mode.
	// Without issues.jsonl, bd's auto-export might write issues to other .jsonl files.
	issuesJSONL := filepath.Join(relicsDir, "issues.jsonl")
	if _, err := os.Stat(issuesJSONL); os.IsNotExist(err) {
		if err := os.WriteFile(issuesJSONL, []byte{}, 0644); err != nil {
			// Non-fatal but log it
			fmt.Printf("   ⚠ Could not create issues.jsonl: %v\n", err)
		}
	}

	// NOTE: We intentionally do NOT create routes.jsonl in warband relics.
	// bd's routing walks up to find encampment root (via warchief/encampment.json) and uses
	// encampment-level routes.jsonl for prefix-based routing. Warband-level routes.jsonl
	// would prevent this walk-up and break cross-warband routing.

	return nil
}

// initAgentRelics creates warband-level agent relics for Witness and Forge.
// These agents use the warband's relics prefix and are stored in warband relics.
//
// Encampment-level agents (Warchief, Shaman) are created by hd install in encampment relics.
// Role relics are also created by hd install with hq- prefix.
//
// Warband-level agents (Witness, Forge) are created here in warband relics with warband prefix.
// Format: <prefix>-<warband>-<role> (e.g., pi-pixelforge-witness)
//
// Agent relics track lifecycle state for ZFC compliance (gt-h3hak, gt-pinkq).
func (m *Manager) initAgentRelics(rigPath, rigName, prefix string) error {
	// Warband-level agents go in warband relics with warband prefix (per docs/architecture.md).
	// Encampment-level agents (Warchief, Shaman) are created by hd install in encampment relics.
	// Use ResolveRelicsDir to follow redirect files for tracked relics.
	rigRelicsDir := relics.ResolveRelicsDir(rigPath)
	bd := relics.NewWithRelicsDir(rigPath, rigRelicsDir)

	// Define warband-level agents to create
	type agentDef struct {
		id       string
		roleType string
		warband      string
		desc     string
	}

	// Create warband-specific agents using warband prefix in warband relics.
	// Format: <prefix>-<warband>-<role> (e.g., pi-pixelforge-witness)
	agents := []agentDef{
		{
			id:       relics.WitnessBeadIDWithPrefix(prefix, rigName),
			roleType: "witness",
			warband:      rigName,
			desc:     fmt.Sprintf("Witness for %s - monitors raider health and progress.", rigName),
		},
		{
			id:       relics.ForgeBeadIDWithPrefix(prefix, rigName),
			roleType: "forge",
			warband:      rigName,
			desc:     fmt.Sprintf("Forge for %s - processes merge queue.", rigName),
		},
	}

	// Note: Warchief and Shaman are now created by hd install in encampment relics.

	for _, agent := range agents {
		// Check if already exists
		if _, err := bd.Show(agent.id); err == nil {
			continue // Already exists
		}

		// RoleBead points to the shared role definition bead for this agent type.
		// Role relics are in encampment relics with hq- prefix (e.g., hq-witness-role).
		fields := &relics.AgentFields{
			RoleType:   agent.roleType,
			Warband:        agent.warband,
			AgentState: "idle",
			BannerBead:   "",
			RoleBead:   relics.RoleBeadIDTown(agent.roleType),
		}

		if _, err := bd.CreateAgentBead(agent.id, agent.desc, fields); err != nil {
			return fmt.Errorf("creating %s: %w", agent.id, err)
		}
		fmt.Printf("   ✓ Created agent bead: %s\n", agent.id)
	}

	return nil
}

// ensureGitignoreEntry adds an entry to .gitignore if it doesn't already exist.
func (m *Manager) ensureGitignoreEntry(gitignorePath, entry string) error {
	// Read existing content
	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Check if entry already exists
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == entry {
			return nil // Already present
		}
	}

	// Append entry
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gosec // G302: .gitignore should be readable by git tools
	if err != nil {
		return err
	}
	defer f.Close()

	// Add newline before if file doesn't end with one
	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(entry + "\n")
	return err
}

// deriveRelicsPrefix generates a relics prefix from a warband name.
// Examples: "horde" -> "hd", "my-project" -> "mp", "foo" -> "foo"
func deriveRelicsPrefix(name string) string {
	// Remove common suffixes
	name = strings.TrimSuffix(name, "-py")
	name = strings.TrimSuffix(name, "-go")

	// Split on hyphens/underscores
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_'
	})

	// If single part, try to detect compound words (e.g., "horde" -> "gas" + "encampment")
	if len(parts) == 1 {
		parts = splitCompoundWord(parts[0])
	}

	if len(parts) >= 2 {
		// Take first letter of each part: "horde" -> "hd"
		prefix := ""
		for _, p := range parts {
			if len(p) > 0 {
				prefix += string(p[0])
			}
		}
		return strings.ToLower(prefix)
	}

	// Single word: use first 2-3 chars
	if len(name) <= 3 {
		return strings.ToLower(name)
	}
	return strings.ToLower(name[:2])
}

// splitCompoundWord attempts to split a compound word into its components.
// Common suffixes like "encampment", "ville", "port" are detected to split
// compound names (e.g., "horde" -> ["gas", "encampment"]).
func splitCompoundWord(word string) []string {
	word = strings.ToLower(word)

	// Common suffixes for compound place names
	suffixes := []string{"encampment", "ville", "port", "place", "land", "field", "wood", "ford"}

	for _, suffix := range suffixes {
		if strings.HasSuffix(word, suffix) && len(word) > len(suffix) {
			prefix := word[:len(word)-len(suffix)]
			if len(prefix) > 0 {
				return []string{prefix, suffix}
			}
		}
	}

	return []string{word}
}

// detectRelicsPrefixFromConfig reads the issue prefix from a relics config.yaml file.
// Returns empty string if the file doesn't exist or doesn't contain a prefix.
// Falls back to detecting prefix from existing issues in issues.jsonl.
//
// relicsPrefixRegexp validates relics prefix format: alphanumeric, may contain hyphens,
// must start with letter, max 20 chars. Prevents shell injection via config files.
var relicsPrefixRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,19}$`)

// isValidRelicsPrefix checks if a prefix is safe for use in shell commands.
// Prefixes must be alphanumeric (with optional hyphens), start with a letter,
// and be at most 20 characters. This prevents command injection from
// malicious config files.
func isValidRelicsPrefix(prefix string) bool {
	return relicsPrefixRegexp.MatchString(prefix)
}

// When adding a warband from a source repo that has .relics/ tracked in git (like a project
// that already uses relics for issue tracking), we need to use that project's existing
// prefix instead of generating a new one. Otherwise, the warband would have a mismatched
// prefix and routing would fail to find the existing issues.
func detectRelicsPrefixFromConfig(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	// Parse YAML-style config (simple line-by-line parsing)
	// Looking for "issue-prefix: <value>" or "prefix: <value>"
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Check for issue-prefix or prefix key
		for _, key := range []string{"issue-prefix:", "prefix:"} {
			if strings.HasPrefix(line, key) {
				value := strings.TrimSpace(strings.TrimPrefix(line, key))
				// Remove quotes if present
				value = strings.Trim(value, `"'`)
				if value != "" && isValidRelicsPrefix(value) {
					return value
				}
			}
		}
	}

	// Fallback: try to detect prefix from existing issues in issues.jsonl
	// Look for the first issue ID pattern like "gt-abc123"
	relicsDir := filepath.Dir(configPath)
	issuesPath := filepath.Join(relicsDir, "issues.jsonl")
	if issuesData, err := os.ReadFile(issuesPath); err == nil {
		issuesLines := strings.Split(string(issuesData), "\n")
		for _, line := range issuesLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Look for "id":"<prefix>-<hash>" pattern
			if idx := strings.Index(line, `"id":"`); idx != -1 {
				start := idx + 6 // len(`"id":"`)
				if end := strings.Index(line[start:], `"`); end != -1 {
					issueID := line[start : start+end]
					// Extract prefix (everything before the last hyphen-hash part)
					if dashIdx := strings.LastIndex(issueID, "-"); dashIdx > 0 {
						prefix := issueID[:dashIdx]
						// Handle prefixes like "hd" (from "gt-abc") - return without trailing hyphen
						if isValidRelicsPrefix(prefix) {
							return prefix
						}
					}
				}
			}
			break // Only check first issue
		}
	}

	return ""
}

// RemoveRig unregisters a warband (does not delete files).
func (m *Manager) RemoveRig(name string) error {
	if !m.RigExists(name) {
		return ErrRigNotFound
	}

	delete(m.config.Warbands, name)
	return nil
}

// ListRigNames returns the names of all registered warbands.
func (m *Manager) ListRigNames() []string {
	names := make([]string, 0, len(m.config.Warbands))
	for name := range m.config.Warbands {
		names = append(names, name)
	}
	return names
}

// createRoleCLAUDEmd creates a minimal bootstrap pointer CLAUDE.md file.
// Full context is injected ephemerally by `hd rally` at session start.
// This keeps on-disk files small (<30 lines) per the priming architecture.
func (m *Manager) createRoleCLAUDEmd(workspacePath string, role string, rigName string, workerName string) error {
	// Create role-specific bootstrap pointer
	var bootstrap string
	switch role {
	case "warchief":
		bootstrap = `# Warchief Context (` + rigName + `)

> **Recovery**: Run ` + "`hd rally`" + ` after compaction, clear, or new session

Full context is injected by ` + "`hd rally`" + ` at session start.
`
	case "forge":
		bootstrap = `# Forge Context (` + rigName + `)

> **Recovery**: Run ` + "`hd rally`" + ` after compaction, clear, or new session

Full context is injected by ` + "`hd rally`" + ` at session start.

## Quick Reference

- Check MQ: ` + "`hd mq list`" + `
- Process next: ` + "`hd mq process`" + `
`
	case "clan":
		name := workerName
		if name == "" {
			name = "worker"
		}
		bootstrap = `# Clan Context (` + rigName + `/` + name + `)

> **Recovery**: Run ` + "`hd rally`" + ` after compaction, clear, or new session

Full context is injected by ` + "`hd rally`" + ` at session start.

## Quick Reference

- Check hook: ` + "`hd hook`" + `
- Check drums: ` + "`hd drums inbox`" + `
`
	case "raider":
		name := workerName
		if name == "" {
			name = "worker"
		}
		bootstrap = `# Raider Context (` + rigName + `/` + name + `)

> **Recovery**: Run ` + "`hd rally`" + ` after compaction, clear, or new session

Full context is injected by ` + "`hd rally`" + ` at session start.

## Quick Reference

- Check hook: ` + "`hd hook`" + `
- Report done: ` + "`hd done`" + `
`
	default:
		bootstrap = `# Agent Context

> **Recovery**: Run ` + "`hd rally`" + ` after compaction, clear, or new session

Full context is injected by ` + "`hd rally`" + ` at session start.
`
	}

	claudePath := filepath.Join(workspacePath, "CLAUDE.md")
	return os.WriteFile(claudePath, []byte(bootstrap), 0644)
}

// createPatrolHooks creates .claude/settings.json with hooks for scout roles.
// These hooks trigger hd rally on session start and inject drums, enabling
// autonomous scout execution for Witness and Forge roles.
func (m *Manager) createPatrolHooks(workspacePath string, runtimeConfig *config.RuntimeConfig) error {
	if runtimeConfig == nil || runtimeConfig.Hooks == nil || runtimeConfig.Hooks.Provider != "claude" {
		return nil
	}
	if runtimeConfig.Hooks.Dir == "" || runtimeConfig.Hooks.SettingsFile == "" {
		return nil
	}

	settingsDir := filepath.Join(workspacePath, runtimeConfig.Hooks.Dir)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		return fmt.Errorf("creating settings dir: %w", err)
	}

	// Standard scout hooks - same as shaman
	hooksJSON := `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "hd rally && hd drums check --inject"
          }
        ]
      }
    ],
    "PreCompact": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "hd rally"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "hd drums check --inject"
          }
        ]
      }
    ]
  }
}
`
	settingsPath := filepath.Join(settingsDir, runtimeConfig.Hooks.SettingsFile)
	return os.WriteFile(settingsPath, []byte(hooksJSON), 0600)
}

// seedPatrolMolecules creates scout totem prototypes in the warband's relics database.
// These totems define the work loops for Shaman, Witness, and Forge roles.
func (m *Manager) seedPatrolMolecules(rigPath string) error {
	// Use rl command to seed totems (more reliable than internal API)
	cmd := exec.Command("rl", "mol", "seed", "--scout")
	cmd.Dir = rigPath
	if err := cmd.Run(); err != nil {
		// Fallback: rl mol seed might not support --scout yet
		// Try creating them individually via rl create
		return m.seedPatrolMoleculesManually(rigPath)
	}
	return nil
}

// seedPatrolMoleculesManually creates scout totems using rl create commands.
func (m *Manager) seedPatrolMoleculesManually(rigPath string) error {
	// Scout totem definitions for seeding
	patrolMols := []struct {
		title string
		desc  string
	}{
		{
			title: "Shaman Scout",
			desc:  "Warchief's daemon scout loop for handling callbacks, health checks, and cleanup.",
		},
		{
			title: "Witness Scout",
			desc:  "Per-warband worker monitor scout loop with progressive nudging.",
		},
		{
			title: "Forge Scout",
			desc:  "Merge queue processor scout loop with verification gates.",
		},
	}

	for _, mol := range patrolMols {
		// Check if already exists by title
		checkCmd := exec.Command("rl", "list", "--type=totem", "--format=json")
		checkCmd.Dir = rigPath
		output, _ := checkCmd.Output()
		if strings.Contains(string(output), mol.title) {
			continue // Already exists
		}

		// Create the totem
		cmd := exec.Command("rl", "create", //nolint:gosec // G204: rl is a trusted internal tool
			"--type=totem",
			"--title="+mol.title,
			"--description="+mol.desc,
			"--priority=2",
		)
		cmd.Dir = rigPath
		if err := cmd.Run(); err != nil {
			// Non-fatal, continue with others
			continue
		}
	}
	return nil
}

// createPluginDirectories creates plugin directories at encampment and warband levels.
// - ~/horde/plugins/ (encampment-level, shared across all warbands)
// - <warband>/plugins/ (warband-level, warband-specific plugins)
func (m *Manager) createPluginDirectories(rigPath string) error {
	// Encampment-level plugins directory
	townPluginsDir := filepath.Join(m.townRoot, "plugins")
	if err := os.MkdirAll(townPluginsDir, 0755); err != nil {
		return fmt.Errorf("creating encampment plugins directory: %w", err)
	}

	// Create a README in encampment plugins if it doesn't exist
	townReadme := filepath.Join(townPluginsDir, "README.md")
	if _, err := os.Stat(townReadme); os.IsNotExist(err) {
		content := `# Horde Plugins

This directory contains encampment-level plugins that run during Shaman scout cycles.

## Plugin Structure

Each plugin is a directory containing:
- plugin.md - Plugin definition with TOML frontmatter

## Gate Types

- cooldown: Time since last run (e.g., 24h)
- cron: Schedule-based (e.g., "0 9 * * *")
- condition: Metric threshold
- event: Trigger-based (startup, heartbeat)

See docs/shaman-plugins.md for full documentation.
`
		if writeErr := os.WriteFile(townReadme, []byte(content), 0644); writeErr != nil {
			// Non-fatal
			return nil
		}
	}

	// Warband-level plugins directory
	rigPluginsDir := filepath.Join(rigPath, "plugins")
	if err := os.MkdirAll(rigPluginsDir, 0755); err != nil {
		return fmt.Errorf("creating warband plugins directory: %w", err)
	}

	// Add plugins/ and .repo.git/ to warband .gitignore
	gitignorePath := filepath.Join(rigPath, ".gitignore")
	if err := m.ensureGitignoreEntry(gitignorePath, "plugins/"); err != nil {
		return err
	}
	return m.ensureGitignoreEntry(gitignorePath, ".repo.git/")
}
