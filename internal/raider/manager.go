// Liftoff test: 2026-01-09T14:30:00

package raider

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

// Common errors
var (
	ErrRaiderExists     = errors.New("raider already exists")
	ErrRaiderNotFound   = errors.New("raider not found")
	ErrHasChanges        = errors.New("raider has uncommitted changes")
	ErrHasUncommittedWork = errors.New("raider has uncommitted work")
)

// UncommittedWorkError provides details about uncommitted work.
type UncommittedWorkError struct {
	RaiderName string
	Status      *git.UncommittedWorkStatus
}

func (e *UncommittedWorkError) Error() string {
	return fmt.Sprintf("raider %s has uncommitted work: %s", e.RaiderName, e.Status.String())
}

func (e *UncommittedWorkError) Unwrap() error {
	return ErrHasUncommittedWork
}

// Manager handles raider lifecycle.
type Manager struct {
	warband      *warband.Warband
	git      *git.Git
	relics    *relics.Relics
	namePool *NamePool
	tmux     *tmux.Tmux
}

// NewManager creates a new raider manager.
func NewManager(r *warband.Warband, g *git.Git, t *tmux.Tmux) *Manager {
	// Use the resolved relics directory to find where rl commands should run.
	// For tracked relics: warband/.relics/redirect -> warchief/warband/.relics, so use warchief/warband
	// For local relics: warband/.relics is the database, so use warband root
	resolvedRelics := relics.ResolveRelicsDir(r.Path)
	relicsPath := filepath.Dir(resolvedRelics) // Get the directory containing .relics

	// Try to load warband settings for namepool config
	settingsPath := filepath.Join(r.Path, "settings", "config.json")
	var pool *NamePool

	settings, err := config.LoadRigSettings(settingsPath)
	if err == nil && settings.Namepool != nil {
		// Use configured namepool settings
		pool = NewNamePoolWithConfig(
			r.Path,
			r.Name,
			settings.Namepool.Style,
			settings.Namepool.Names,
			settings.Namepool.MaxBeforeNumbering,
		)
	} else {
		// Use defaults
		pool = NewNamePool(r.Path, r.Name)
	}
	_ = pool.Load() // non-fatal: state file may not exist for new warbands

	return &Manager{
		warband:      r,
		git:      g,
		relics:    relics.NewWithRelicsDir(relicsPath, resolvedRelics),
		namePool: pool,
		tmux:     t,
	}
}

// assigneeID returns the relics assignee identifier for a raider.
// Format: "warband/raiderName" (e.g., "horde/Toast")
func (m *Manager) assigneeID(name string) string {
	return fmt.Sprintf("%s/%s", m.warband.Name, name)
}

// agentBeadID returns the agent bead ID for a raider.
// Format: "<prefix>-<warband>-raider-<name>" (e.g., "hd-horde-raider-Toast", "bd-relics-raider-obsidian")
// The prefix is looked up from routes.jsonl to support warbands with custom prefixes.
func (m *Manager) agentBeadID(name string) string {
	// Find encampment root to lookup prefix from routes.jsonl
	townRoot, err := workspace.Find(m.warband.Path)
	if err != nil || townRoot == "" {
		// Fall back to default prefix
		return relics.RaiderBeadID(m.warband.Name, name)
	}
	prefix := relics.GetPrefixForRig(townRoot, m.warband.Name)
	return relics.RaiderBeadIDWithPrefix(prefix, m.warband.Name, name)
}

// getCleanupStatusFromBead reads the cleanup_status from the raider's agent bead.
// Returns CleanupUnknown if the bead doesn't exist or has no cleanup_status.
// ZFC #10: This is the ZFC-compliant way to check if removal is safe.
func (m *Manager) getCleanupStatusFromBead(name string) CleanupStatus {
	agentID := m.agentBeadID(name)
	_, fields, err := m.relics.GetAgentBead(agentID)
	if err != nil || fields == nil {
		return CleanupUnknown
	}
	if fields.CleanupStatus == "" {
		return CleanupUnknown
	}
	return CleanupStatus(fields.CleanupStatus)
}

// checkCleanupStatus validates the cleanup status against removal safety rules.
// Returns an error if removal should be blocked based on the status.
// force=true: allow has_uncommitted, block has_stash and has_unpushed
// force=false: block all non-clean statuses
func (m *Manager) checkCleanupStatus(name string, status CleanupStatus, force bool) error {
	// Clean status is always safe
	if status.IsSafe() {
		return nil
	}

	// With force, uncommitted changes can be bypassed
	if force && status.CanForceRemove() {
		return nil
	}

	// Map status to appropriate error
	switch status {
	case CleanupUncommitted:
		return &UncommittedWorkError{
			RaiderName: name,
			Status:      &git.UncommittedWorkStatus{HasUncommittedChanges: true},
		}
	case CleanupStash:
		return &UncommittedWorkError{
			RaiderName: name,
			Status:      &git.UncommittedWorkStatus{StashCount: 1},
		}
	case CleanupUnpushed:
		return &UncommittedWorkError{
			RaiderName: name,
			Status:      &git.UncommittedWorkStatus{UnpushedCommits: 1},
		}
	default:
		// Unknown status - be conservative and block
		return &UncommittedWorkError{
			RaiderName: name,
			Status:      &git.UncommittedWorkStatus{HasUncommittedChanges: true},
		}
	}
}

// repoBase returns the git directory and Git object to use for worktree operations.
// Prefers the shared bare repo (.repo.git) if it exists, otherwise falls back to warchief/warband.
// The bare repo architecture allows all worktrees (forge, raiders) to share branch visibility.
func (m *Manager) repoBase() (*git.Git, error) {
	// First check for shared bare repo (new architecture)
	bareRepoPath := filepath.Join(m.warband.Path, ".repo.git")
	if info, err := os.Stat(bareRepoPath); err == nil && info.IsDir() {
		// Bare repo exists - use it
		return git.NewGitWithDir(bareRepoPath, ""), nil
	}

	// Fall back to warchief/warband (legacy architecture)
	warchiefPath := filepath.Join(m.warband.Path, "warchief", "warband")
	if _, err := os.Stat(warchiefPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no repo base found (neither .repo.git nor warchief/warband exists)")
	}
	return git.NewGit(warchiefPath), nil
}

// raiderDir returns the parent directory for a raider.
// This is raiders/<name>/ - the raider's home directory.
func (m *Manager) raiderDir(name string) string {
	return filepath.Join(m.warband.Path, "raiders", name)
}

// clonePath returns the path where the git worktree lives.
// New structure: raiders/<name>/<rigname>/ - gives LLMs recognizable repo context.
// Falls back to old structure: raiders/<name>/ for backward compatibility.
func (m *Manager) clonePath(name string) string {
	// New structure: raiders/<name>/<rigname>/
	newPath := filepath.Join(m.warband.Path, "raiders", name, m.warband.Name)
	if info, err := os.Stat(newPath); err == nil && info.IsDir() {
		return newPath
	}

	// Old structure: raiders/<name>/ (backward compat)
	oldPath := filepath.Join(m.warband.Path, "raiders", name)
	if info, err := os.Stat(oldPath); err == nil && info.IsDir() {
		// Check if this is actually a git worktree (has .git file or dir)
		gitPath := filepath.Join(oldPath, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return oldPath
		}
	}

	// Default to new structure for new raiders
	return newPath
}

// exists checks if a raider exists.
func (m *Manager) exists(name string) bool {
	_, err := os.Stat(m.raiderDir(name))
	return err == nil
}

// AddOptions configures raider creation.
type AddOptions struct {
	BannerBead string // Bead ID to set as banner_bead at muster time (atomic assignment)
}

// Add creates a new raider as a git worktree from the repo base.
// Uses the shared bare repo (.repo.git) if available, otherwise warchief/warband.
// This is much faster than a full clone and shares objects with all worktrees.
// Raider state is derived from relics assignee field, not state.json.
//
// Branch naming: Each raider run gets a unique branch (raider/<name>-<timestamp>).
// This prevents drift issues from stale branches and ensures a clean starting state.
// Old branches are ephemeral and never pushed to origin.
func (m *Manager) Add(name string) (*Raider, error) {
	return m.AddWithOptions(name, AddOptions{})
}

// AddWithOptions creates a new raider with the specified options.
// This allows setting banner_bead atomically at creation time, avoiding
// cross-relics routing issues when charging work to new raiders.
func (m *Manager) AddWithOptions(name string, opts AddOptions) (*Raider, error) {
	if m.exists(name) {
		return nil, ErrRaiderExists
	}

	// New structure: raiders/<name>/<rigname>/ for LLM ergonomics
	// The raider's home dir is raiders/<name>/, worktree is raiders/<name>/<rigname>/
	raiderDir := m.raiderDir(name)
	clonePath := filepath.Join(raiderDir, m.warband.Name)

	// Unique branch per run - prevents drift from stale branches
	// Use base36 encoding for shorter branch names (8 chars vs 13 digits)
	branchName := fmt.Sprintf("raider/%s-%s", name, strconv.FormatInt(time.Now().UnixMilli(), 36))

	// Create raider directory (raiders/<name>/)
	if err := os.MkdirAll(raiderDir, 0755); err != nil {
		return nil, fmt.Errorf("creating raider dir: %w", err)
	}

	// Get the repo base (bare repo or warchief/warband)
	repoGit, err := m.repoBase()
	if err != nil {
		return nil, fmt.Errorf("finding repo base: %w", err)
	}

	// Fetch latest from origin to ensure worktree starts from up-to-date code
	if err := repoGit.Fetch("origin"); err != nil {
		// Non-fatal - proceed with potentially stale code
		fmt.Printf("Warning: could not fetch origin: %v\n", err)
	}

	// Determine the start point for the new worktree
	// Use origin/<default-branch> to ensure we start from the warband's configured branch
	defaultBranch := "main"
	if rigCfg, err := warband.LoadRigConfig(m.warband.Path); err == nil && rigCfg.DefaultBranch != "" {
		defaultBranch = rigCfg.DefaultBranch
	}
	startPoint := fmt.Sprintf("origin/%s", defaultBranch)

	// Always create fresh branch - unique name guarantees no collision
	// git worktree add -b raider/<name>-<timestamp> <path> <startpoint>
	// Worktree goes in raiders/<name>/<rigname>/ for LLM ergonomics
	if err := repoGit.WorktreeAddFromRef(clonePath, branchName, startPoint); err != nil {
		return nil, fmt.Errorf("creating worktree from %s: %w", startPoint, err)
	}

	// Ensure AGENTS.md exists - critical for raiders to "land the plane"
	// Fall back to copy from warchief/warband if not in git (e.g., stale fetch, local-only file)
	agentsMDPath := filepath.Join(clonePath, "AGENTS.md")
	if _, err := os.Stat(agentsMDPath); os.IsNotExist(err) {
		srcPath := filepath.Join(m.warband.Path, "warchief", "warband", "AGENTS.md")
		if srcData, readErr := os.ReadFile(srcPath); readErr == nil {
			if writeErr := os.WriteFile(agentsMDPath, srcData, 0644); writeErr != nil {
				fmt.Printf("Warning: could not copy AGENTS.md: %v\n", writeErr)
			}
		}
	}

	// NOTE: We intentionally do NOT write to CLAUDE.md here.
	// Horde context is injected ephemerally via SessionStart hook (gt rally).
	// Writing to CLAUDE.md would overwrite project instructions and could leak
	// Horde internals into the project repo if merged.

	// Set up shared relics: raider uses warband's .relics via redirect file.
	// This eliminates git sync overhead - all raiders share one database.
	if err := m.setupSharedRelics(clonePath); err != nil {
		// Non-fatal - raider can still work with local relics
		// Log warning but don't fail the muster
		fmt.Printf("Warning: could not set up shared relics: %v\n", err)
	}

	// Provision RALLY.md with Horde context for this worker.
	// This is the fallback if SessionStart hook fails - ensures raiders
	// always have GUPP and essential Horde context.
	if err := relics.ProvisionPrimeMDForWorktree(clonePath); err != nil {
		// Non-fatal - raider can still work via hook, warn but don't fail
		fmt.Printf("Warning: could not provision RALLY.md: %v\n", err)
	}

	// Copy overlay files from .runtime/overlay/ to raider root.
	// This allows services to have .env and other config files at their root.
	if err := warband.CopyOverlay(m.warband.Path, clonePath); err != nil {
		// Non-fatal - log warning but continue
		fmt.Printf("Warning: could not copy overlay files: %v\n", err)
	}

	// Run setup hooks from .runtime/setup-hooks/.
	// These hooks can inject local git config, copy secrets, or perform other setup tasks.
	if err := warband.RunSetupHooks(m.warband.Path, clonePath); err != nil {
		// Non-fatal - log warning but continue
		fmt.Printf("Warning: could not run setup hooks: %v\n", err)
	}

	// NOTE: Slash commands (.claude/commands/) are provisioned at encampment level by hd install.
	// All agents inherit them via Claude's directory traversal - no per-workspace copies needed.

	// Create or reopen agent bead for ZFC compliance (self-report state).
	// State starts as "spawning" - will be updated to "working" when Claude starts.
	// BannerBead is set atomically at creation time if provided (avoids cross-relics routing issues).
	// Uses CreateOrReopenAgentBead to handle re-spawning with same name (GH #332).
	agentID := m.agentBeadID(name)
	_, err = m.relics.CreateOrReopenAgentBead(agentID, agentID, &relics.AgentFields{
		RoleType:   "raider",
		Warband:        m.warband.Name,
		AgentState: "spawning",
		RoleBead:   relics.RoleBeadIDTown("raider"),
		BannerBead:   opts.BannerBead, // Set atomically at muster time
	})
	if err != nil {
		// Non-fatal - log warning but continue
		fmt.Printf("Warning: could not create agent bead: %v\n", err)
	}

	// Return raider with working state (transient model: raiders are spawned with work)
	// State is derived from relics, not stored in state.json
	now := time.Now()
	raider := &Raider{
		Name:      name,
		Warband:       m.warband.Name,
		State:     StateWorking, // Transient model: raider spawns with work
		ClonePath: clonePath,
		Branch:    branchName,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return raider, nil
}

// Remove deletes a raider worktree.
// If force is true, removes even with uncommitted changes (but not stashes/unpushed).
// Use nuclear=true to bypass ALL safety checks.
func (m *Manager) Remove(name string, force bool) error {
	return m.RemoveWithOptions(name, force, false)
}

// RemoveWithOptions deletes a raider worktree with explicit control over safety checks.
// force=true: bypass uncommitted changes check (legacy behavior)
// nuclear=true: bypass ALL safety checks including stashes and unpushed commits
//
// ZFC #10: Uses cleanup_status from agent bead if available (raider self-report),
// falls back to git check for backward compatibility.
func (m *Manager) RemoveWithOptions(name string, force, nuclear bool) error {
	if !m.exists(name) {
		return ErrRaiderNotFound
	}

	// Clone path is where the git worktree lives (new or old structure)
	clonePath := m.clonePath(name)
	// Raider dir is the parent directory (raiders/<name>/)
	raiderDir := m.raiderDir(name)

	// Check for uncommitted work unless bypassed
	if !nuclear {
		// ZFC #10: First try to read cleanup_status from agent bead
		// This is the ZFC-compliant path - trust what the raider reported
		cleanupStatus := m.getCleanupStatusFromBead(name)

		if cleanupStatus != CleanupUnknown {
			// ZFC path: Use raider's self-reported status
			if err := m.checkCleanupStatus(name, cleanupStatus, force); err != nil {
				return err
			}
		} else {
			// Fallback path: Check git directly (for raiders that haven't reported yet)
			raiderGit := git.NewGit(clonePath)
			status, err := raiderGit.CheckUncommittedWork()
			if err == nil && !status.Clean() {
				// For backward compatibility: force only bypasses uncommitted changes, not stashes/unpushed
				if force {
					// Force mode: allow uncommitted changes but still block on stashes/unpushed
					if status.StashCount > 0 || status.UnpushedCommits > 0 {
						return &UncommittedWorkError{RaiderName: name, Status: status}
					}
				} else {
					return &UncommittedWorkError{RaiderName: name, Status: status}
				}
			}
		}
	}

	// Get repo base to remove the worktree properly
	repoGit, err := m.repoBase()
	if err != nil {
		// Fall back to direct removal if repo base not found
		return os.RemoveAll(raiderDir)
	}

	// Try to remove as a worktree first (use force flag for worktree removal too)
	if err := repoGit.WorktreeRemove(clonePath, force); err != nil {
		// Fall back to direct removal if worktree removal fails
		// (e.g., if this is an old-style clone, not a worktree)
		if removeErr := os.RemoveAll(clonePath); removeErr != nil {
			return fmt.Errorf("removing clone path: %w", removeErr)
		}
	}

	// Also remove the parent raider directory if it's now empty
	// (for new structure: raiders/<name>/ contains only raiders/<name>/<rigname>/)
	if raiderDir != clonePath {
		_ = os.Remove(raiderDir) // Non-fatal: only removes if empty
	}

	// Prune any stale worktree entries (non-fatal: cleanup only)
	_ = repoGit.WorktreePrune()

	// Release name back to pool if it's a pooled name (non-fatal: state file update)
	m.namePool.Release(name)
	_ = m.namePool.Save()

	// Close agent bead (non-fatal: may not exist or relics may not be available)
	// NOTE: We use CloseAndClearAgentBead instead of DeleteAgentBead because rl delete --hard
	// creates tombstones that cannot be reopened.
	agentID := m.agentBeadID(name)
	if err := m.relics.CloseAndClearAgentBead(agentID, "raider removed"); err != nil {
		// Only log if not "not found" - it's ok if it doesn't exist
		if !errors.Is(err, relics.ErrNotFound) {
			fmt.Printf("Warning: could not close agent bead %s: %v\n", agentID, err)
		}
	}

	return nil
}

// AllocateName allocates a name from the name pool.
// Returns a pooled name (raider-01 through raider-50) if available,
// otherwise returns an overflow name (rigname-N).
func (m *Manager) AllocateName() (string, error) {
	// First reconcile pool with existing raiders to handle stale state
	m.ReconcilePool()

	name, err := m.namePool.Allocate()
	if err != nil {
		return "", err
	}

	if err := m.namePool.Save(); err != nil {
		return "", fmt.Errorf("saving pool state: %w", err)
	}

	return name, nil
}

// ReleaseName releases a name back to the pool.
// This is called when a raider is removed.
func (m *Manager) ReleaseName(name string) {
	m.namePool.Release(name)
	_ = m.namePool.Save() // non-fatal: state file update
}

// RepairWorktree repairs a stale raider by removing it and creating a fresh worktree.
// This is NOT for normal operation - it handles reconciliation when AllocateName
// returns a name that unexpectedly already exists (stale state recovery).
//
// The raider starts with the latest code from origin/<default-branch>.
// The name is preserved (not released to pool) since we're repairing immediately.
// force controls whether to bypass uncommitted changes check.
//
// Branch naming: Each repair gets a unique branch (raider/<name>-<timestamp>).
// Old branches are left for garbage collection - they're never pushed to origin.
func (m *Manager) RepairWorktree(name string, force bool) (*Raider, error) {
	return m.RepairWorktreeWithOptions(name, force, AddOptions{})
}

// RepairWorktreeWithOptions repairs a stale raider and creates a fresh worktree with options.
// This is NOT for normal operation - see RepairWorktree for context.
// Allows setting banner_bead atomically at repair time.
// After repair, uses new structure: raiders/<name>/<rigname>/
func (m *Manager) RepairWorktreeWithOptions(name string, force bool, opts AddOptions) (*Raider, error) {
	if !m.exists(name) {
		return nil, ErrRaiderNotFound
	}

	// Get the old clone path (may be old or new structure)
	oldClonePath := m.clonePath(name)
	raiderGit := git.NewGit(oldClonePath)

	// New clone path uses new structure
	raiderDir := m.raiderDir(name)
	newClonePath := filepath.Join(raiderDir, m.warband.Name)

	// Get the repo base (bare repo or warchief/warband)
	repoGit, err := m.repoBase()
	if err != nil {
		return nil, fmt.Errorf("finding repo base: %w", err)
	}

	// Check for uncommitted work unless forced
	if !force {
		status, err := raiderGit.CheckUncommittedWork()
		if err == nil && !status.Clean() {
			return nil, &UncommittedWorkError{RaiderName: name, Status: status}
		}
	}

	// Close old agent bead before recreation (non-fatal)
	// NOTE: We use CloseAndClearAgentBead instead of DeleteAgentBead because rl delete --hard
	// creates tombstones that cannot be reopened.
	agentID := m.agentBeadID(name)
	if err := m.relics.CloseAndClearAgentBead(agentID, "raider repair"); err != nil {
		if !errors.Is(err, relics.ErrNotFound) {
			fmt.Printf("Warning: could not close old agent bead %s: %v\n", agentID, err)
		}
	}

	// Remove the old worktree (use force for git worktree removal)
	if err := repoGit.WorktreeRemove(oldClonePath, true); err != nil {
		// Fall back to direct removal
		if removeErr := os.RemoveAll(oldClonePath); removeErr != nil {
			return nil, fmt.Errorf("removing old clone path: %w", removeErr)
		}
	}

	// Prune stale worktree entries (non-fatal: cleanup only)
	_ = repoGit.WorktreePrune()

	// Fetch latest from origin to ensure we have fresh commits (non-fatal: may be offline)
	_ = repoGit.Fetch("origin")

	// Ensure raider directory exists for new structure
	if err := os.MkdirAll(raiderDir, 0755); err != nil {
		return nil, fmt.Errorf("creating raider dir: %w", err)
	}

	// Determine the start point for the new worktree
	// Use origin/<default-branch> to ensure we start from latest fetched commits
	defaultBranch := "main"
	if rigCfg, err := warband.LoadRigConfig(m.warband.Path); err == nil && rigCfg.DefaultBranch != "" {
		defaultBranch = rigCfg.DefaultBranch
	}
	startPoint := fmt.Sprintf("origin/%s", defaultBranch)

	// Create fresh worktree with unique branch name, starting from origin's default branch
	// Old branches are left behind - they're ephemeral (never pushed to origin)
	// and will be cleaned up by garbage collection
	// Use base36 encoding for shorter branch names (8 chars vs 13 digits)
	branchName := fmt.Sprintf("raider/%s-%s", name, strconv.FormatInt(time.Now().UnixMilli(), 36))
	if err := repoGit.WorktreeAddFromRef(newClonePath, branchName, startPoint); err != nil {
		return nil, fmt.Errorf("creating fresh worktree from %s: %w", startPoint, err)
	}

	// Ensure AGENTS.md exists - critical for raiders to "land the plane"
	// Fall back to copy from warchief/warband if not in git (e.g., stale fetch, local-only file)
	agentsMDPath := filepath.Join(newClonePath, "AGENTS.md")
	if _, err := os.Stat(agentsMDPath); os.IsNotExist(err) {
		srcPath := filepath.Join(m.warband.Path, "warchief", "warband", "AGENTS.md")
		if srcData, readErr := os.ReadFile(srcPath); readErr == nil {
			if writeErr := os.WriteFile(agentsMDPath, srcData, 0644); writeErr != nil {
				fmt.Printf("Warning: could not copy AGENTS.md: %v\n", writeErr)
			}
		}
	}

	// NOTE: We intentionally do NOT write to CLAUDE.md here.
	// Horde context is injected ephemerally via SessionStart hook (gt rally).

	// Set up shared relics
	if err := m.setupSharedRelics(newClonePath); err != nil {
		fmt.Printf("Warning: could not set up shared relics: %v\n", err)
	}

	// Copy overlay files from .runtime/overlay/ to raider root.
	if err := warband.CopyOverlay(m.warband.Path, newClonePath); err != nil {
		fmt.Printf("Warning: could not copy overlay files: %v\n", err)
	}

	// NOTE: Slash commands inherited from encampment level - no per-workspace copies needed.

	// Create or reopen agent bead for ZFC compliance
	// BannerBead is set atomically at recreation time if provided.
	// Uses CreateOrReopenAgentBead to handle re-spawning with same name (GH #332).
	_, err = m.relics.CreateOrReopenAgentBead(agentID, agentID, &relics.AgentFields{
		RoleType:   "raider",
		Warband:        m.warband.Name,
		AgentState: "spawning",
		RoleBead:   relics.RoleBeadIDTown("raider"),
		BannerBead:   opts.BannerBead, // Set atomically at muster time
	})
	if err != nil {
		fmt.Printf("Warning: could not create agent bead: %v\n", err)
	}

	// Return fresh raider in working state (transient model: raiders are spawned with work)
	now := time.Now()
	return &Raider{
		Name:      name,
		Warband:       m.warband.Name,
		State:     StateWorking,
		ClonePath: newClonePath,
		Branch:    branchName,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ReconcilePool derives pool InUse state from existing raider directories and active sessions.
// This implements ZFC: InUse is discovered from filesystem and tmux, not tracked separately.
// Called before each allocation to ensure InUse reflects reality.
//
// In addition to directory checks, this also:
// - Kills orphaned tmux sessions (sessions without directories are broken)
func (m *Manager) ReconcilePool() {
	// Get raiders with existing directories
	raiders, err := m.List()
	if err != nil {
		return
	}

	var namesWithDirs []string
	for _, p := range raiders {
		namesWithDirs = append(namesWithDirs, p.Name)
	}

	// Get names with tmux sessions
	var namesWithSessions []string
	if m.tmux != nil {
		poolNames := m.namePool.getNames()
		for _, name := range poolNames {
			sessionName := fmt.Sprintf("hd-%s-%s", m.warband.Name, name)
			hasSession, _ := m.tmux.HasSession(sessionName)
			if hasSession {
				namesWithSessions = append(namesWithSessions, name)
			}
		}
	}

	m.ReconcilePoolWith(namesWithDirs, namesWithSessions)

	// Prune any stale git worktree entries (handles manually deleted directories)
	if repoGit, err := m.repoBase(); err == nil {
		_ = repoGit.WorktreePrune()
	}
}

// ReconcilePoolWith reconciles the name pool given lists of names from different sources.
// This is the testable core of ReconcilePool.
//
// - namesWithDirs: names that have existing worktree directories (in use)
// - namesWithSessions: names that have tmux sessions
//
// Names with sessions but no directories are orphans and their sessions are killed.
// Only namesWithDirs are marked as in-use for allocation.
func (m *Manager) ReconcilePoolWith(namesWithDirs, namesWithSessions []string) {
	dirSet := make(map[string]bool)
	for _, name := range namesWithDirs {
		dirSet[name] = true
	}

	// Kill orphaned sessions (session exists but no directory)
	if m.tmux != nil {
		for _, name := range namesWithSessions {
			if !dirSet[name] {
				sessionName := fmt.Sprintf("hd-%s-%s", m.warband.Name, name)
				_ = m.tmux.KillSession(sessionName)
			}
		}
	}

	m.namePool.Reconcile(namesWithDirs)
	// Note: No Save() needed - InUse is transient state, only OverflowNext is persisted
}

// PoolStatus returns information about the name pool.
func (m *Manager) PoolStatus() (active int, names []string) {
	return m.namePool.ActiveCount(), m.namePool.ActiveNames()
}

// List returns all raiders in the warband.
func (m *Manager) List() ([]*Raider, error) {
	raidersDir := filepath.Join(m.warband.Path, "raiders")

	entries, err := os.ReadDir(raidersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading raiders dir: %w", err)
	}

	var raiders []*Raider
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		raider, err := m.Get(entry.Name())
		if err != nil {
			continue // Skip invalid raiders
		}
		raiders = append(raiders, raider)
	}

	return raiders, nil
}

// Get returns a specific raider by name.
// State is derived from relics assignee field:
// - If an issue is assigned to this raider: StateWorking
// - If no issue assigned: StateDone (ready for cleanup - transient raiders should have work)
func (m *Manager) Get(name string) (*Raider, error) {
	if !m.exists(name) {
		return nil, ErrRaiderNotFound
	}

	return m.loadFromRelics(name)
}

// SetState updates a raider's state.
// In the relics model, state is derived from issue status:
// - StateWorking/StateActive: issue status set to in_progress
// - StateDone: assignee cleared from issue (raider ready for cleanup)
// - StateStuck: issue status set to blocked (if supported)
// If relics is not available, this is a no-op.
func (m *Manager) SetState(name string, state State) error {
	if !m.exists(name) {
		return ErrRaiderNotFound
	}

	// Find the issue assigned to this raider
	assignee := m.assigneeID(name)
	issue, err := m.relics.GetAssignedIssue(assignee)
	if err != nil {
		// If relics is not available, treat as no-op (state can't be changed)
		return nil
	}

	switch state {
	case StateWorking, StateActive:
		// Set issue to in_progress if there is one
		if issue != nil {
			status := "in_progress"
			if err := m.relics.Update(issue.ID, relics.UpdateOptions{Status: &status}); err != nil {
				return fmt.Errorf("setting issue status: %w", err)
			}
		}
	case StateDone:
		// Clear assignment when done (raider ready for cleanup)
		if issue != nil {
			empty := ""
			if err := m.relics.Update(issue.ID, relics.UpdateOptions{Assignee: &empty}); err != nil {
				return fmt.Errorf("clearing assignee: %w", err)
			}
		}
	case StateStuck:
		// Mark issue as blocked if supported, otherwise just note in issue
		if issue != nil {
			// For now, just keep the assignment - the issue's blocked_by would indicate stuck
			// We could add a status="blocked" here if relics supports it
		}
	}

	return nil
}

// AssignIssue assigns an issue to a raider by setting the issue's assignee in relics.
func (m *Manager) AssignIssue(name, issue string) error {
	if !m.exists(name) {
		return ErrRaiderNotFound
	}

	// Set the issue's assignee to this raider
	assignee := m.assigneeID(name)
	status := "in_progress"
	if err := m.relics.Update(issue, relics.UpdateOptions{
		Assignee: &assignee,
		Status:   &status,
	}); err != nil {
		return fmt.Errorf("setting issue assignee: %w", err)
	}

	return nil
}

// ClearIssue removes the issue assignment from a raider.
// In the transient model, this transitions to Done state for cleanup.
// This clears the assignee from the currently assigned issue in relics.
// If relics is not available, this is a no-op.
func (m *Manager) ClearIssue(name string) error {
	if !m.exists(name) {
		return ErrRaiderNotFound
	}

	// Find the issue assigned to this raider
	assignee := m.assigneeID(name)
	issue, err := m.relics.GetAssignedIssue(assignee)
	if err != nil {
		// If relics is not available, treat as no-op
		return nil
	}

	if issue == nil {
		// No issue assigned, nothing to clear
		return nil
	}

	// Clear the assignee from the issue
	empty := ""
	if err := m.relics.Update(issue.ID, relics.UpdateOptions{
		Assignee: &empty,
	}); err != nil {
		return fmt.Errorf("clearing issue assignee: %w", err)
	}

	return nil
}

// loadFromRelics gets raider info from relics assignee field.
// State is simple: issue assigned → working, no issue → done (ready for cleanup).
// Transient raiders should always have work; no work means ready for Witness cleanup.
// We don't interpret issue status (ZFC: Go is transport, not decision-maker).
func (m *Manager) loadFromRelics(name string) (*Raider, error) {
	// Use clonePath which handles both new (raiders/<name>/<rigname>/)
	// and old (raiders/<name>/) structures
	clonePath := m.clonePath(name)

	// Get actual branch from worktree (branches are now timestamped)
	raiderGit := git.NewGit(clonePath)
	branchName, err := raiderGit.CurrentBranch()
	if err != nil {
		// Fall back to old format if we can't read the branch
		branchName = fmt.Sprintf("raider/%s", name)
	}

	// Query relics for assigned issue
	assignee := m.assigneeID(name)
	issue, relicsErr := m.relics.GetAssignedIssue(assignee)
	if relicsErr != nil {
		// If relics query fails, return basic raider info as working
		// (assume raider is doing something if it exists)
		return &Raider{
			Name:      name,
			Warband:       m.warband.Name,
			State:     StateWorking,
			ClonePath: clonePath,
			Branch:    branchName,
		}, nil
	}

	// Transient model: has issue = working, no issue = done (ready for cleanup)
	// Raiders without work should be nuked by the Witness
	state := StateDone
	issueID := ""
	if issue != nil {
		issueID = issue.ID
		state = StateWorking
	}

	return &Raider{
		Name:      name,
		Warband:       m.warband.Name,
		State:     state,
		ClonePath: clonePath,
		Branch:    branchName,
		Issue:     issueID,
	}, nil
}

// setupSharedRelics creates a redirect file so the raider uses the warband's shared .relics database.
// This eliminates the need for git sync between raider clones - all raiders share one database.
func (m *Manager) setupSharedRelics(clonePath string) error {
	townRoot := filepath.Dir(m.warband.Path)
	return relics.SetupRedirect(townRoot, clonePath)
}

// CleanupStaleBranches removes orphaned raider branches that are no longer in use.
// This includes:
// - Branches for raiders that no longer exist
// - Old timestamped branches (keeps only the most recent per raider name)
// Returns the number of branches deleted.
func (m *Manager) CleanupStaleBranches() (int, error) {
	repoGit, err := m.repoBase()
	if err != nil {
		return 0, fmt.Errorf("finding repo base: %w", err)
	}

	// List all raider branches
	branches, err := repoGit.ListBranches("raider/*")
	if err != nil {
		return 0, fmt.Errorf("listing branches: %w", err)
	}

	if len(branches) == 0 {
		return 0, nil
	}

	// Get list of existing raiders
	raiders, err := m.List()
	if err != nil {
		return 0, fmt.Errorf("listing raiders: %w", err)
	}

	// Build set of current raider branches (from actual raider objects)
	currentBranches := make(map[string]bool)
	for _, p := range raiders {
		currentBranches[p.Branch] = true
	}

	// Delete branches not in current set
	deleted := 0
	for _, branch := range branches {
		if currentBranches[branch] {
			continue // This branch is in use
		}
		// Delete orphaned branch
		if err := repoGit.DeleteBranch(branch, true); err != nil {
			// Log but continue - non-fatal
			fmt.Printf("Warning: could not delete branch %s: %v\n", branch, err)
			continue
		}
		deleted++
	}

	return deleted, nil
}

// StalenessInfo contains details about a raider's staleness.
type StalenessInfo struct {
	Name            string
	CommitsBehind   int  // How many commits behind origin/main
	HasActiveSession bool // Whether tmux session is running
	HasUncommittedWork bool // Whether there's uncommitted or unpushed work
	AgentState      string // From agent bead (empty if no bead)
	IsStale         bool   // Overall assessment: safe to clean up
	Reason          string // Why it's considered stale (or not)
}

// DetectStaleRaiders identifies raiders that are candidates for cleanup.
// A raider is considered stale if:
// - No active tmux session AND
// - Either: way behind main (>threshold commits) OR no agent bead/activity
// - Has no uncommitted work that could be lost
//
// threshold: minimum commits behind main to consider "way behind" (e.g., 20)
func (m *Manager) DetectStaleRaiders(threshold int) ([]*StalenessInfo, error) {
	raiders, err := m.List()
	if err != nil {
		return nil, fmt.Errorf("listing raiders: %w", err)
	}

	if len(raiders) == 0 {
		return nil, nil
	}

	// Get default branch from warband config
	defaultBranch := "main"
	if rigCfg, err := warband.LoadRigConfig(m.warband.Path); err == nil && rigCfg.DefaultBranch != "" {
		defaultBranch = rigCfg.DefaultBranch
	}

	var results []*StalenessInfo
	for _, p := range raiders {
		info := &StalenessInfo{
			Name: p.Name,
		}

		// Check for active tmux session
		// Session name follows pattern: gt-<warband>-<raider>
		sessionName := fmt.Sprintf("hd-%s-%s", m.warband.Name, p.Name)
		info.HasActiveSession = checkTmuxSession(sessionName)

		// Check how far behind main
		raiderGit := git.NewGit(p.ClonePath)
		info.CommitsBehind = countCommitsBehind(raiderGit, defaultBranch)

		// Check for uncommitted work
		status, err := raiderGit.CheckUncommittedWork()
		if err == nil && !status.Clean() {
			info.HasUncommittedWork = true
		}

		// Check agent bead state
		agentID := m.agentBeadID(p.Name)
		_, fields, err := m.relics.GetAgentBead(agentID)
		if err == nil && fields != nil {
			info.AgentState = fields.AgentState
		}

		// Determine staleness
		info.IsStale, info.Reason = assessStaleness(info, threshold)
		results = append(results, info)
	}

	return results, nil
}

// checkTmuxSession checks if a tmux session exists.
func checkTmuxSession(sessionName string) bool {
	// Use has-session command which returns 0 if session exists
	cmd := exec.Command("tmux", "has-session", "-t", sessionName) //nolint:gosec // G204: sessionName is constructed internally
	return cmd.Run() == nil
}

// countCommitsBehind counts how many commits a worktree is behind origin/<defaultBranch>.
func countCommitsBehind(g *git.Git, defaultBranch string) int {
	// Use rev-list to count commits: origin/main..HEAD shows commits ahead,
	// HEAD..origin/main shows commits behind
	remoteBranch := "origin/" + defaultBranch
	count, err := g.CountCommitsBehind(remoteBranch)
	if err != nil {
		return 0 // Can't determine, assume not behind
	}
	return count
}

// assessStaleness determines if a raider should be cleaned up.
// Per gt-zecmc: uses tmux state (HasActiveSession) rather than agent_state
// since observable states (running, done, idle) are no longer recorded in relics.
func assessStaleness(info *StalenessInfo, threshold int) (bool, string) {
	// Never clean up if there's uncommitted work
	if info.HasUncommittedWork {
		return false, "has uncommitted work"
	}

	// If session is active, not stale (tmux is source of truth for liveness)
	if info.HasActiveSession {
		return false, "session active"
	}

	// No active session - this raider is a cleanup candidate
	// Check for reasons to keep it:

	// Check for non-observable states that indicate intentional pause
	// (stuck, awaiting-gate are still stored in relics per gt-zecmc)
	if info.AgentState == "stuck" || info.AgentState == "awaiting-gate" {
		return false, fmt.Sprintf("agent_state=%s (intentional pause)", info.AgentState)
	}

	// No session and way behind main = stale
	if info.CommitsBehind >= threshold {
		return true, fmt.Sprintf("%d commits behind main, no active session", info.CommitsBehind)
	}

	// No session and no agent bead = abandoned, clean up
	if info.AgentState == "" {
		return true, "no agent bead, no active session"
	}

	// No session but has agent bead without special state = clean up
	// (The session is the source of truth for liveness)
	return true, "no active session"
}
