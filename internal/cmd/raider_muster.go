// Package cmd provides raider spawning utilities for hd charge.
package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/constants"
	"github.com/OWNER/horde/internal/events"
	"github.com/OWNER/horde/internal/git"
	"github.com/OWNER/horde/internal/raider"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/tmux"
	"github.com/OWNER/horde/internal/workspace"
)

// SpawnedRaiderInfo contains info about a spawned raider session.
type SpawnedRaiderInfo struct {
	RigName     string // Warband name (e.g., "horde")
	RaiderName string // Raider name (e.g., "Toast")
	ClonePath   string // Path to raider's git worktree
	SessionName string // Tmux session name (e.g., "gt-horde-p-Toast")
	Pane        string // Tmux pane ID
}

// AgentID returns the agent identifier (e.g., "horde/raiders/Toast")
func (s *SpawnedRaiderInfo) AgentID() string {
	return fmt.Sprintf("%s/raiders/%s", s.RigName, s.RaiderName)
}

// SlingSpawnOptions contains options for spawning a raider via charge.
type SlingSpawnOptions struct {
	Force    bool   // Force muster even if raider has uncommitted work
	Account  string // Claude Code account handle to use
	Create   bool   // Create raider if it doesn't exist (currently always true for charge)
	BannerBead string // Bead ID to set as banner_bead at muster time (atomic assignment)
	Agent    string // Agent override for this muster (e.g., "gemini", "codex", "claude-haiku")
}

// SpawnRaiderForSling creates a fresh raider and optionally starts its session.
// This is used by hd charge when the target is a warband name.
// The caller (charge) handles hook attachment and nudging.
func SpawnRaiderForSling(rigName string, opts SlingSpawnOptions) (*SpawnedRaiderInfo, error) {
	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return nil, fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warband config
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(rigName)
	if err != nil {
		return nil, fmt.Errorf("warband '%s' not found", rigName)
	}

	// Get raider manager (with tmux for session-aware allocation)
	raiderGit := git.NewGit(r.Path)
	t := tmux.NewTmux()
	raiderMgr := raider.NewManager(r, raiderGit, t)

	// Allocate a new raider name
	raiderName, err := raiderMgr.AllocateName()
	if err != nil {
		return nil, fmt.Errorf("allocating raider name: %w", err)
	}
	fmt.Printf("Allocated raider: %s\n", raiderName)

	// Check if raider already exists (shouldn't happen - indicates stale state needing repair)
	existingRaider, err := raiderMgr.Get(raiderName)

	// Build add options with banner_bead set atomically at muster time
	addOpts := raider.AddOptions{
		BannerBead: opts.BannerBead,
	}

	if err == nil {
		// Stale state: raider exists despite fresh name allocation - repair it
		// Check for uncommitted work first
		if !opts.Force {
			pGit := git.NewGit(existingRaider.ClonePath)
			workStatus, checkErr := pGit.CheckUncommittedWork()
			if checkErr == nil && !workStatus.Clean() {
				return nil, fmt.Errorf("raider '%s' has uncommitted work: %s\nUse --force to proceed anyway",
					raiderName, workStatus.String())
			}
		}
		fmt.Printf("Repairing stale raider %s with fresh worktree...\n", raiderName)
		if _, err = raiderMgr.RepairWorktreeWithOptions(raiderName, opts.Force, addOpts); err != nil {
			return nil, fmt.Errorf("repairing stale raider: %w", err)
		}
	} else if err == raider.ErrRaiderNotFound {
		// Create new raider
		fmt.Printf("Creating raider %s...\n", raiderName)
		if _, err = raiderMgr.AddWithOptions(raiderName, addOpts); err != nil {
			return nil, fmt.Errorf("creating raider: %w", err)
		}
	} else {
		return nil, fmt.Errorf("getting raider: %w", err)
	}

	// Get raider object for path info
	raiderObj, err := raiderMgr.Get(raiderName)
	if err != nil {
		return nil, fmt.Errorf("getting raider after creation: %w", err)
	}

	// Resolve account for runtime config
	accountsPath := constants.WarchiefAccountsPath(townRoot)
	claudeConfigDir, accountHandle, err := config.ResolveAccountConfigDir(accountsPath, opts.Account)
	if err != nil {
		return nil, fmt.Errorf("resolving account: %w", err)
	}
	if accountHandle != "" {
		fmt.Printf("Using account: %s\n", accountHandle)
	}

	// Start session (reuse tmux from manager)
	raiderSessMgr := raider.NewSessionManager(t, r)

	// Check if already running
	running, _ := raiderSessMgr.IsRunning(raiderName)
	if !running {
		fmt.Printf("Starting session for %s/%s...\n", rigName, raiderName)
		startOpts := raider.SessionStartOptions{
			RuntimeConfigDir: claudeConfigDir,
		}
		if opts.Agent != "" {
			cmd, err := config.BuildRaiderStartupCommandWithAgentOverride(rigName, raiderName, r.Path, "", opts.Agent)
			if err != nil {
				return nil, err
			}
			startOpts.Command = cmd
		}
		if err := raiderSessMgr.Start(raiderName, startOpts); err != nil {
			return nil, fmt.Errorf("starting session: %w", err)
		}
	}

	// Get session name and pane
	sessionName := raiderSessMgr.SessionName(raiderName)
	pane, err := getSessionPane(sessionName)
	if err != nil {
		return nil, fmt.Errorf("getting pane for %s: %w", sessionName, err)
	}

	fmt.Printf("%s Raider %s spawned\n", style.Bold.Render("✓"), raiderName)

	// Log muster event to activity feed
	_ = events.LogFeed(events.TypeSpawn, "hd", events.SpawnPayload(rigName, raiderName))

	return &SpawnedRaiderInfo{
		RigName:     rigName,
		RaiderName: raiderName,
		ClonePath:   raiderObj.ClonePath,
		SessionName: sessionName,
		Pane:        pane,
	}, nil
}

// IsRigName checks if a target string is a warband name (not a role or path).
// Returns the warband name and true if it's a valid warband.
func IsRigName(target string) (string, bool) {
	// If it contains a slash, it's a path format (warband/role or warband/clan/name)
	if strings.Contains(target, "/") {
		return "", false
	}

	// Check known non-warband role names
	switch strings.ToLower(target) {
	case "warchief", "may", "shaman", "dea", "clan", "witness", "wit", "forge", "ref":
		return "", false
	}

	// Try to load as a warband
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return "", false
	}

	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		return "", false
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	_, err = rigMgr.GetRig(target)
	if err != nil {
		return "", false
	}

	return target, true
}
