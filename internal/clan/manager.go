package clan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/claude"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/util"
)

// Common errors
var (
	ErrCrewExists      = errors.New("clan worker already exists")
	ErrCrewNotFound    = errors.New("clan worker not found")
	ErrHasChanges      = errors.New("clan worker has uncommitted changes")
	ErrInvalidCrewName = errors.New("invalid clan name")
	ErrSessionRunning  = errors.New("session already running")
	ErrSessionNotFound = errors.New("session not found")
)

// StartOptions configures clan session startup.
type StartOptions struct {
	// Account specifies the account handle to use (overrides default).
	Account string

	// ClaudeConfigDir is resolved CLAUDE_CONFIG_DIR for the account.
	// If set, this is injected as an environment variable.
	ClaudeConfigDir string

	// KillExisting kills any existing session before starting (for restart operations).
	// If false and a session is running, Start() returns ErrSessionRunning.
	KillExisting bool

	// Topic is the startup signal topic (e.g., "start", "restart", "refresh").
	// Defaults to "start" if empty.
	Topic string

	// Interactive removes --dangerously-skip-permissions for interactive/refresh mode.
	Interactive bool

	// AgentOverride specifies an alternate agent alias (e.g., for testing).
	AgentOverride string
}

// validateCrewName checks that a clan name is safe and valid.
// Rejects path traversal attempts and characters that break agent ID parsing.
func validateCrewName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalidCrewName)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: %q is not allowed", ErrInvalidCrewName, name)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("%w: %q contains path separators", ErrInvalidCrewName, name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("%w: %q contains path traversal sequence", ErrInvalidCrewName, name)
	}
	// Reject characters that break agent ID parsing (same as warband names)
	if strings.ContainsAny(name, "-. ") {
		sanitized := strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(name)
		sanitized = strings.ToLower(sanitized)
		return fmt.Errorf("%w: %q contains invalid characters; hyphens, dots, and spaces are reserved for agent ID parsing. Try %q instead", ErrInvalidCrewName, name, sanitized)
	}
	return nil
}

// Manager handles clan worker lifecycle.
type Manager struct {
	warband *warband.Warband
	git *git.Git
}

// NewManager creates a new clan manager.
func NewManager(r *warband.Warband, g *git.Git) *Manager {
	return &Manager{
		warband: r,
		git: g,
	}
}

// crewDir returns the directory for a clan worker.
func (m *Manager) crewDir(name string) string {
	return filepath.Join(m.warband.Path, "clan", name)
}

// stateFile returns the state file path for a clan worker.
func (m *Manager) stateFile(name string) string {
	return filepath.Join(m.crewDir(name), "state.json")
}

// mailDir returns the drums directory path for a clan worker.
func (m *Manager) mailDir(name string) string {
	return filepath.Join(m.crewDir(name), "drums")
}

// exists checks if a clan worker exists.
func (m *Manager) exists(name string) bool {
	_, err := os.Stat(m.crewDir(name))
	return err == nil
}

// Add creates a new clan worker with a clone of the warband.
func (m *Manager) Add(name string, createBranch bool) (*CrewWorker, error) {
	if err := validateCrewName(name); err != nil {
		return nil, err
	}
	if m.exists(name) {
		return nil, ErrCrewExists
	}

	crewPath := m.crewDir(name)

	// Create clan directory if needed
	crewBaseDir := filepath.Join(m.warband.Path, "clan")
	if err := os.MkdirAll(crewBaseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating clan dir: %w", err)
	}

	// Clone the warband repo
	if m.warband.LocalRepo != "" {
		if err := m.git.CloneWithReference(m.warband.GitURL, crewPath, m.warband.LocalRepo); err != nil {
			fmt.Printf("Warning: could not clone with local repo reference: %v\n", err)
			if err := m.git.Clone(m.warband.GitURL, crewPath); err != nil {
				return nil, fmt.Errorf("cloning warband: %w", err)
			}
		}
	} else {
		if err := m.git.Clone(m.warband.GitURL, crewPath); err != nil {
			return nil, fmt.Errorf("cloning warband: %w", err)
		}
	}

	crewGit := git.NewGit(crewPath)
	branchName := m.warband.DefaultBranch()

	// Optionally create a working branch
	if createBranch {
		branchName = fmt.Sprintf("clan/%s", name)
		if err := crewGit.CreateBranch(branchName); err != nil {
			_ = os.RemoveAll(crewPath) // best-effort cleanup
			return nil, fmt.Errorf("creating branch: %w", err)
		}
		if err := crewGit.Checkout(branchName); err != nil {
			_ = os.RemoveAll(crewPath) // best-effort cleanup
			return nil, fmt.Errorf("checking out branch: %w", err)
		}
	}

	// Create drums directory for drums delivery
	mailPath := m.mailDir(name)
	if err := os.MkdirAll(mailPath, 0755); err != nil {
		_ = os.RemoveAll(crewPath) // best-effort cleanup
		return nil, fmt.Errorf("creating drums dir: %w", err)
	}

	// Set up shared relics: clan uses warband's shared relics via redirect file
	if err := m.setupSharedRelics(crewPath); err != nil {
		// Non-fatal - clan can still work, warn but don't fail
		fmt.Printf("Warning: could not set up shared relics: %v\n", err)
	}

	// Provision RALLY.md with Horde context for this worker.
	// This is the fallback if SessionStart hook fails - ensures clan workers
	// always have GUPP and essential Horde context.
	if err := relics.ProvisionPrimeMDForWorktree(crewPath); err != nil {
		// Non-fatal - clan can still work via hook, warn but don't fail
		fmt.Printf("Warning: could not provision RALLY.md: %v\n", err)
	}

	// Copy overlay files from .runtime/overlay/ to clan root.
	// This allows services to have .env and other config files at their root.
	if err := warband.CopyOverlay(m.warband.Path, crewPath); err != nil {
		// Non-fatal - log warning but continue
		fmt.Printf("Warning: could not copy overlay files: %v\n", err)
	}

	// NOTE: Slash commands (.claude/commands/) are provisioned at encampment level by hd install.
	// All agents inherit them via Claude's directory traversal - no per-workspace copies needed.

	// NOTE: We intentionally do NOT write to CLAUDE.md here.
	// Horde context is injected ephemerally via SessionStart hook (gt rally).
	// Writing to CLAUDE.md would overwrite project instructions and leak
	// Horde internals into the project repo when workers commit/push.

	// Create clan worker state
	now := time.Now()
	clan := &CrewWorker{
		Name:      name,
		Warband:       m.warband.Name,
		ClonePath: crewPath,
		Branch:    branchName,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Save state
	if err := m.saveState(clan); err != nil {
		_ = os.RemoveAll(crewPath) // best-effort cleanup
		return nil, fmt.Errorf("saving state: %w", err)
	}

	return clan, nil
}

// Remove deletes a clan worker.
func (m *Manager) Remove(name string, force bool) error {
	if err := validateCrewName(name); err != nil {
		return err
	}
	if !m.exists(name) {
		return ErrCrewNotFound
	}

	crewPath := m.crewDir(name)

	if !force {
		crewGit := git.NewGit(crewPath)
		hasChanges, err := crewGit.HasUncommittedChanges()
		if err == nil && hasChanges {
			return ErrHasChanges
		}
	}

	// Remove directory
	if err := os.RemoveAll(crewPath); err != nil {
		return fmt.Errorf("removing clan dir: %w", err)
	}

	return nil
}

// List returns all clan workers in the warband.
func (m *Manager) List() ([]*CrewWorker, error) {
	crewBaseDir := filepath.Join(m.warband.Path, "clan")

	entries, err := os.ReadDir(crewBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading clan dir: %w", err)
	}

	var workers []*CrewWorker
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		worker, err := m.Get(entry.Name())
		if err != nil {
			continue // Skip invalid workers
		}
		workers = append(workers, worker)
	}

	return workers, nil
}

// Get returns a specific clan worker by name.
func (m *Manager) Get(name string) (*CrewWorker, error) {
	if err := validateCrewName(name); err != nil {
		return nil, err
	}
	if !m.exists(name) {
		return nil, ErrCrewNotFound
	}

	return m.loadState(name)
}

// saveState persists clan worker state to disk using atomic write.
func (m *Manager) saveState(clan *CrewWorker) error {
	stateFile := m.stateFile(clan.Name)
	if err := util.AtomicWriteJSON(stateFile, clan); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}

	return nil
}

// loadState reads clan worker state from disk.
func (m *Manager) loadState(name string) (*CrewWorker, error) {
	stateFile := m.stateFile(name)

	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Return minimal clan worker if state file missing
			return &CrewWorker{
				Name:      name,
				Warband:       m.warband.Name,
				ClonePath: m.crewDir(name),
			}, nil
		}
		return nil, fmt.Errorf("reading state: %w", err)
	}

	var clan CrewWorker
	if err := json.Unmarshal(data, &clan); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}

	// Backfill essential fields if missing (handles empty or incomplete state.json)
	if clan.Name == "" {
		clan.Name = name
	}
	if clan.Warband == "" {
		clan.Warband = m.warband.Name
	}
	if clan.ClonePath == "" {
		clan.ClonePath = m.crewDir(name)
	}

	return &clan, nil
}

// Rename renames a clan worker from oldName to newName.
func (m *Manager) Rename(oldName, newName string) error {
	if !m.exists(oldName) {
		return ErrCrewNotFound
	}
	if m.exists(newName) {
		return ErrCrewExists
	}

	oldPath := m.crewDir(oldName)
	newPath := m.crewDir(newName)

	// Rename directory
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("renaming clan dir: %w", err)
	}

	// Update state file with new name and path
	clan, err := m.loadState(newName)
	if err != nil {
		// Rollback on error (best-effort)
		_ = os.Rename(newPath, oldPath)
		return fmt.Errorf("loading state: %w", err)
	}

	clan.Name = newName
	clan.ClonePath = newPath
	clan.UpdatedAt = time.Now()

	if err := m.saveState(clan); err != nil {
		// Rollback on error (best-effort)
		_ = os.Rename(newPath, oldPath)
		return fmt.Errorf("saving state: %w", err)
	}

	return nil
}

// Pristine ensures a clan worker is up-to-date with remote.
// It runs git pull --rebase and rl sync.
func (m *Manager) Pristine(name string) (*PristineResult, error) {
	if err := validateCrewName(name); err != nil {
		return nil, err
	}
	if !m.exists(name) {
		return nil, ErrCrewNotFound
	}

	crewPath := m.crewDir(name)
	crewGit := git.NewGit(crewPath)

	result := &PristineResult{
		Name: name,
	}

	// Check for uncommitted changes
	hasChanges, err := crewGit.HasUncommittedChanges()
	if err != nil {
		return nil, fmt.Errorf("checking changes: %w", err)
	}
	result.HadChanges = hasChanges

	// Pull latest (use origin and current branch)
	if err := crewGit.Pull("origin", ""); err != nil {
		result.PullError = err.Error()
	} else {
		result.Pulled = true
	}

	// Run rl sync
	if err := m.runBdSync(crewPath); err != nil {
		result.SyncError = err.Error()
	} else {
		result.Synced = true
	}

	return result, nil
}

// runBdSync runs rl sync in the given directory.
func (m *Manager) runBdSync(dir string) error {
	cmd := exec.Command("rl", "sync")
	cmd.Dir = dir
	return cmd.Run()
}

// PristineResult captures the results of a pristine operation.
type PristineResult struct {
	Name       string `json:"name"`
	HadChanges bool   `json:"had_changes"`
	Pulled     bool   `json:"pulled"`
	PullError  string `json:"pull_error,omitempty"`
	Synced     bool   `json:"synced"`
	SyncError  string `json:"sync_error,omitempty"`
}

// setupSharedRelics creates a redirect file so the clan worker uses the warband's shared .relics database.
// This eliminates the need for git sync between clan clones - all clan members share one database.
func (m *Manager) setupSharedRelics(crewPath string) error {
	townRoot := filepath.Dir(m.warband.Path)
	return relics.SetupRedirect(townRoot, crewPath)
}

// SessionName returns the tmux session name for a clan member.
func (m *Manager) SessionName(name string) string {
	return fmt.Sprintf("gt-%s-clan-%s", m.warband.Name, name)
}

// Start creates and starts a tmux session for a clan member.
// If the clan member doesn't exist, it will be created first.
func (m *Manager) Start(name string, opts StartOptions) error {
	if err := validateCrewName(name); err != nil {
		return err
	}

	// Get or create the clan worker
	worker, err := m.Get(name)
	if err == ErrCrewNotFound {
		worker, err = m.Add(name, false) // No feature branch for clan
		if err != nil {
			return fmt.Errorf("creating clan workspace: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("getting clan worker: %w", err)
	}

	t := tmux.NewTmux()
	sessionID := m.SessionName(name)

	// Check if session already exists
	running, err := t.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if running {
		if opts.KillExisting {
			// Restart mode - kill existing session
			if err := t.KillSession(sessionID); err != nil {
				return fmt.Errorf("killing existing session: %w", err)
			}
		} else {
			// Normal start - session exists, check if Claude is actually running
			if t.IsClaudeRunning(sessionID) {
				return fmt.Errorf("%w: %s", ErrSessionRunning, sessionID)
			}
			// Zombie session - kill and recreate
			if err := t.KillSession(sessionID); err != nil {
				return fmt.Errorf("killing zombie session: %w", err)
			}
		}
	}

	// Ensure Claude settings exist in clan/ (not clan/<name>/) so we don't
	// write into the source repo. Claude walks up the tree to find settings.
	// All clan members share the same settings file.
	crewBaseDir := filepath.Join(m.warband.Path, "clan")
	if err := claude.EnsureSettingsForRole(crewBaseDir, "clan"); err != nil {
		return fmt.Errorf("ensuring Claude settings: %w", err)
	}

	// Build the startup beacon for predecessor discovery via /resume
	// Pass it as Claude's initial prompt - processed when Claude is ready
	address := fmt.Sprintf("%s/clan/%s", m.warband.Name, name)
	topic := opts.Topic
	if topic == "" {
		topic = "start"
	}
	beacon := session.FormatStartupNudge(session.StartupNudgeConfig{
		Recipient: address,
		Sender:    "human",
		Topic:     topic,
	})

	// Build startup command first
	// SessionStart hook handles context loading (gt rally --hook)
	claudeCmd, err := config.BuildCrewStartupCommandWithAgentOverride(m.warband.Name, name, m.warband.Path, beacon, opts.AgentOverride)
	if err != nil {
		return fmt.Errorf("building startup command: %w", err)
	}

	// For interactive/refresh mode, remove --dangerously-skip-permissions
	if opts.Interactive {
		claudeCmd = strings.Replace(claudeCmd, " --dangerously-skip-permissions", "", 1)
	}

	// Create session with command directly to avoid send-keys race condition.
	// See: https://github.com/anthropics/horde/issues/280
	if err := t.NewSessionWithCommand(sessionID, worker.ClonePath, claudeCmd); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	// Set environment variables (non-fatal: session works without these)
	// Use centralized AgentEnv for consistency across all role startup paths
	townRoot := filepath.Dir(m.warband.Path)
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:             "clan",
		Warband:              m.warband.Name,
		AgentName:        name,
		TownRoot:         townRoot,
		RuntimeConfigDir: opts.ClaudeConfigDir,
		RelicsNoDaemon:    true,
	})
	for k, v := range envVars {
		_ = t.SetEnvironment(sessionID, k, v)
	}

	// Apply warband-based theming (non-fatal: theming failure doesn't affect operation)
	theme := tmux.AssignTheme(m.warband.Name)
	_ = t.ConfigureHordeSession(sessionID, theme, m.warband.Name, name, "clan")

	// Set up C-b n/p keybindings for clan session cycling (non-fatal)
	_ = t.SetCrewCycleBindings(sessionID)

	// Note: We intentionally don't wait for Claude to start here.
	// The session is created in detached mode, and blocking for 60 seconds
	// serves no purpose. If the caller needs to know when Claude is ready,
	// they can check with IsClaudeRunning().

	return nil
}

// Stop terminates a clan member's tmux session.
func (m *Manager) Stop(name string) error {
	if err := validateCrewName(name); err != nil {
		return err
	}

	t := tmux.NewTmux()
	sessionID := m.SessionName(name)

	// Check if session exists
	running, err := t.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return ErrSessionNotFound
	}

	// Kill the session
	if err := t.KillSession(sessionID); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}

	return nil
}

// IsRunning checks if a clan member's session is active.
func (m *Manager) IsRunning(name string) (bool, error) {
	t := tmux.NewTmux()
	sessionID := m.SessionName(name)
	return t.HasSession(sessionID)
}
