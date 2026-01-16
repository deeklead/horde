package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/clan"
	"github.com/deeklead/horde/internal/drums"
	"github.com/deeklead/horde/internal/runtime"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/encampmentlog"
	"github.com/deeklead/horde/internal/workspace"
)

func runCrewRemove(cmd *cobra.Command, args []string) error {
	var lastErr error

	// --purge implies --force
	forceRemove := crewForce || crewPurge

	for _, arg := range args {
		name := arg
		rigOverride := crewRig

		// Parse warband/name format (e.g., "relics/emma" -> warband=relics, name=emma)
		if warband, crewName, ok := parseRigSlashName(name); ok {
			if rigOverride == "" {
				rigOverride = warband
			}
			name = crewName
		}

		crewMgr, r, err := getCrewManager(rigOverride)
		if err != nil {
			fmt.Printf("Error removing %s: %v\n", arg, err)
			lastErr = err
			continue
		}

		// Check for running session (unless forced)
		if !forceRemove {
			t := tmux.NewTmux()
			sessionID := crewSessionName(r.Name, name)
			hasSession, _ := t.HasSession(sessionID)
			if hasSession {
				fmt.Printf("Error removing %s: session '%s' is running (use --force to kill and remove)\n", arg, sessionID)
				lastErr = fmt.Errorf("session running")
				continue
			}
		}

		// Kill session if it exists
		t := tmux.NewTmux()
		sessionID := crewSessionName(r.Name, name)
		if hasSession, _ := t.HasSession(sessionID); hasSession {
			if err := t.KillSession(sessionID); err != nil {
				fmt.Printf("Error killing session for %s: %v\n", arg, err)
				lastErr = err
				continue
			}
			fmt.Printf("Killed session %s\n", sessionID)
		}

		// Determine workspace path
		crewPath := filepath.Join(r.Path, "clan", name)

		// Check if this is a worktree (has .git file) vs regular clone (has .git directory)
		isWorktree := false
		gitPath := filepath.Join(crewPath, ".git")
		if info, err := os.Stat(gitPath); err == nil && !info.IsDir() {
			isWorktree = true
		}

		// Remove the workspace
		if isWorktree {
			// For worktrees, use git worktree remove
			warchiefRigPath := constants.RigWarchiefPath(r.Path)
			removeArgs := []string{"worktree", "remove", crewPath}
			if forceRemove {
				removeArgs = []string{"worktree", "remove", "--force", crewPath}
			}
			removeCmd := exec.Command("git", removeArgs...)
			removeCmd.Dir = warchiefRigPath
			if output, err := removeCmd.CombinedOutput(); err != nil {
				fmt.Printf("Error removing worktree %s: %v\n%s", arg, err, string(output))
				lastErr = err
				continue
			}
			fmt.Printf("%s Removed clan worktree: %s/%s\n",
				style.Bold.Render("✓"), r.Name, name)
		} else {
			// For regular clones, use the clan manager
			if err := crewMgr.Remove(name, forceRemove); err != nil {
				if err == clan.ErrCrewNotFound {
					fmt.Printf("Error removing %s: clan workspace not found\n", arg)
				} else if err == clan.ErrHasChanges {
					fmt.Printf("Error removing %s: uncommitted changes (use --force)\n", arg)
				} else {
					fmt.Printf("Error removing %s: %v\n", arg, err)
				}
				lastErr = err
				continue
			}
			fmt.Printf("%s Removed clan workspace: %s/%s\n",
				style.Bold.Render("✓"), r.Name, name)
		}

		// Handle agent bead
		townRoot, _ := workspace.Find(r.Path)
		if townRoot == "" {
			townRoot = r.Path
		}
		prefix := relics.GetPrefixForRig(townRoot, r.Name)
		agentBeadID := relics.CrewBeadIDWithPrefix(prefix, r.Name, name)

		if crewPurge {
			// --purge: DELETE the agent bead entirely (obliterate)
			deleteArgs := []string{"delete", agentBeadID, "--force"}
			deleteCmd := exec.Command("rl", deleteArgs...)
			deleteCmd.Dir = r.Path
			if output, err := deleteCmd.CombinedOutput(); err != nil {
				// Non-fatal: bead might not exist
				if !strings.Contains(string(output), "no issue found") &&
					!strings.Contains(string(output), "not found") {
					style.PrintWarning("could not delete agent bead %s: %v", agentBeadID, err)
				}
			} else {
				fmt.Printf("Deleted agent bead: %s\n", agentBeadID)
			}

			// Unassign any relics assigned to this clan member
			agentAddr := fmt.Sprintf("%s/clan/%s", r.Name, name)
			unassignArgs := []string{"list", "--assignee=" + agentAddr, "--format=id"}
			unassignCmd := exec.Command("rl", unassignArgs...)
			unassignCmd.Dir = r.Path
			if output, err := unassignCmd.CombinedOutput(); err == nil {
				ids := strings.Fields(strings.TrimSpace(string(output)))
				for _, id := range ids {
					if id == "" {
						continue
					}
					updateCmd := exec.Command("rl", "update", id, "--unassign")
					updateCmd.Dir = r.Path
					if _, err := updateCmd.CombinedOutput(); err == nil {
						fmt.Printf("Unassigned: %s\n", id)
					}
				}
			}

			// Clear drums directory if it exists
			mailDir := filepath.Join(crewPath, "drums")
			if _, err := os.Stat(mailDir); err == nil {
				// Drums dir was removed with the workspace, so nothing to do
				// But if we want to be extra thorough, we could look in encampment relics
			}
		} else {
			// Default: CLOSE the agent bead (preserves CV history)
			closeArgs := []string{"close", agentBeadID, "--reason=Clan workspace removed"}
			if sessionID := runtime.SessionIDFromEnv(); sessionID != "" {
				closeArgs = append(closeArgs, "--session="+sessionID)
			}
			closeCmd := exec.Command("rl", closeArgs...)
			closeCmd.Dir = r.Path
			if output, err := closeCmd.CombinedOutput(); err != nil {
				// Non-fatal: bead might not exist or already be closed
				if !strings.Contains(string(output), "no issue found") &&
					!strings.Contains(string(output), "already closed") {
					style.PrintWarning("could not close agent bead %s: %v", agentBeadID, err)
				}
			} else {
				fmt.Printf("Closed agent bead: %s\n", agentBeadID)
			}
		}
	}

	return lastErr
}

func runCrewRefresh(cmd *cobra.Command, args []string) error {
	name := args[0]
	// Parse warband/name format (e.g., "relics/emma" -> warband=relics, name=emma)
	if warband, crewName, ok := parseRigSlashName(name); ok {
		if crewRig == "" {
			crewRig = warband
		}
		name = crewName
	}

	crewMgr, r, err := getCrewManager(crewRig)
	if err != nil {
		return err
	}

	// Get the clan worker (must exist for refresh)
	worker, err := crewMgr.Get(name)
	if err != nil {
		if err == clan.ErrCrewNotFound {
			return fmt.Errorf("clan workspace '%s' not found", name)
		}
		return fmt.Errorf("getting clan worker: %w", err)
	}

	// Create handoff message
	handoffMsg := crewMessage
	if handoffMsg == "" {
		handoffMsg = fmt.Sprintf("Context refresh for %s. Check drums and relics for current work state.", name)
	}

	// Send handoff drums to self
	mailDir := filepath.Join(worker.ClonePath, "drums")
	if _, err := os.Stat(mailDir); os.IsNotExist(err) {
		if err := os.MkdirAll(mailDir, 0755); err != nil {
			return fmt.Errorf("creating drums dir: %w", err)
		}
	}

	// Create and send drums
	wardrums := drums.NewMailbox(mailDir)
	msg := &drums.Message{
		From:    fmt.Sprintf("%s/%s", r.Name, name),
		To:      fmt.Sprintf("%s/%s", r.Name, name),
		Subject: "🤝 HANDOFF: Context Refresh",
		Body:    handoffMsg,
	}
	if err := wardrums.Append(msg); err != nil {
		return fmt.Errorf("sending handoff drums: %w", err)
	}
	fmt.Printf("Sent handoff drums to %s/%s\n", r.Name, name)

	// Use manager's Start() with refresh options
	err = crewMgr.Start(name, clan.StartOptions{
		KillExisting:  true,      // Kill old session if running
		Topic:         "refresh", // Startup signal topic
		Interactive:   true,      // No --dangerously-skip-permissions
		AgentOverride: crewAgentOverride,
	})
	if err != nil {
		return fmt.Errorf("starting clan session: %w", err)
	}

	fmt.Printf("%s Refreshed clan workspace: %s/%s\n",
		style.Bold.Render("✓"), r.Name, name)
	fmt.Printf("Summon with: %s\n", style.Dim.Render(fmt.Sprintf("hd clan at %s", name)))

	return nil
}

// runCrewStart starts clan workers in a warband.
// If first arg is a valid warband name, it's used as the warband; otherwise warband is inferred from cwd.
// Remaining args (or all args if warband is inferred) are clan member names.
// Defaults to all clan members if no names specified.
func runCrewStart(cmd *cobra.Command, args []string) error {
	var rigName string
	var crewNames []string

	if len(args) == 0 {
		// No args - infer warband from cwd
		rigName = "" // getCrewManager will infer from cwd
	} else {
		// Check if first arg is a valid warband name
		if _, _, err := getRig(args[0]); err == nil {
			// First arg is a warband name
			rigName = args[0]
			crewNames = args[1:]
		} else {
			// First arg is not a warband - infer warband from cwd and treat all args as clan names
			rigName = "" // getCrewManager will infer from cwd
			crewNames = args
		}
	}

	// Get the warband manager and warband (infers from cwd if rigName is empty)
	crewMgr, r, err := getCrewManager(rigName)
	if err != nil {
		return err
	}
	// Update rigName in case it was inferred
	rigName = r.Name

	// If --all flag OR no clan names specified, get all clan members
	if crewAll || len(crewNames) == 0 {
		workers, err := crewMgr.List()
		if err != nil {
			return fmt.Errorf("listing clan: %w", err)
		}
		if len(workers) == 0 {
			fmt.Printf("No clan members in warband %s\n", rigName)
			return nil
		}
		for _, w := range workers {
			crewNames = append(crewNames, w.Name)
		}
	}

	// Resolve account config once for all clan members
	townRoot, _ := workspace.Find(r.Path)
	if townRoot == "" {
		townRoot = filepath.Dir(r.Path)
	}
	accountsPath := constants.WarchiefAccountsPath(townRoot)
	claudeConfigDir, _, _ := config.ResolveAccountConfigDir(accountsPath, crewAccount)

	// Build start options (shared across all clan members)
	opts := clan.StartOptions{
		Account:         crewAccount,
		ClaudeConfigDir: claudeConfigDir,
		AgentOverride:   crewAgentOverride,
	}

	// Start each clan member in parallel
	type result struct {
		name    string
		err     error
		skipped bool // true if session was already running
	}
	results := make(chan result, len(crewNames))
	var wg sync.WaitGroup

	fmt.Printf("Starting %d clan member(s) in %s...\n", len(crewNames), rigName)

	for _, name := range crewNames {
		wg.Add(1)
		go func(crewName string) {
			defer wg.Done()
			err := crewMgr.Start(crewName, opts)
			skipped := errors.Is(err, clan.ErrSessionRunning)
			if skipped {
				err = nil // Not an error, just already running
			}
			results <- result{name: crewName, err: err, skipped: skipped}
		}(name)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var lastErr error
	startedCount := 0
	skippedCount := 0
	for res := range results {
		if res.err != nil {
			fmt.Printf("  %s %s/%s: %v\n", style.ErrorPrefix, rigName, res.name, res.err)
			lastErr = res.err
		} else if res.skipped {
			fmt.Printf("  %s %s/%s: already running\n", style.Dim.Render("○"), rigName, res.name)
			skippedCount++
		} else {
			fmt.Printf("  %s %s/%s: started\n", style.SuccessPrefix, rigName, res.name)
			startedCount++
		}
	}

	// Summary
	fmt.Println()
	if startedCount > 0 || skippedCount > 0 {
		fmt.Printf("%s Started %d, skipped %d (already running) in %s\n",
			style.Bold.Render("✓"), startedCount, skippedCount, r.Name)
	}

	return lastErr
}

func runCrewRestart(cmd *cobra.Command, args []string) error {
	// Handle --all flag
	if crewAll {
		return runCrewRestartAll()
	}

	var lastErr error

	for _, arg := range args {
		name := arg
		rigOverride := crewRig

		// Parse warband/name format (e.g., "relics/emma" -> warband=relics, name=emma)
		if warband, crewName, ok := parseRigSlashName(name); ok {
			if rigOverride == "" {
				rigOverride = warband
			}
			name = crewName
		}

		crewMgr, r, err := getCrewManager(rigOverride)
		if err != nil {
			fmt.Printf("Error restarting %s: %v\n", arg, err)
			lastErr = err
			continue
		}

		// Use manager's Start() with restart options
		// Start() will create workspace if needed (idempotent)
		err = crewMgr.Start(name, clan.StartOptions{
			KillExisting:  true,      // Kill old session if running
			Topic:         "restart", // Startup signal topic
			AgentOverride: crewAgentOverride,
		})
		if err != nil {
			fmt.Printf("Error restarting %s: %v\n", arg, err)
			lastErr = err
			continue
		}

		fmt.Printf("%s Restarted clan workspace: %s/%s\n",
			style.Bold.Render("✓"), r.Name, name)
		fmt.Printf("Summon with: %s\n", style.Dim.Render(fmt.Sprintf("hd clan at %s", name)))
	}

	return lastErr
}

// runCrewRestartAll restarts all running clan sessions.
// If crewRig is set, only restarts clan in that warband.
func runCrewRestartAll() error {
	// Get all agent sessions (including raiders to find clan)
	agents, err := getAgentSessions(true)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	// Filter to clan agents only
	var targets []*AgentSession
	for _, agent := range agents {
		if agent.Type != AgentCrew {
			continue
		}
		// Filter by warband if specified
		if crewRig != "" && agent.Warband != crewRig {
			continue
		}
		targets = append(targets, agent)
	}

	if len(targets) == 0 {
		fmt.Println("No running clan sessions to restart.")
		if crewRig != "" {
			fmt.Printf("  (filtered by warband: %s)\n", crewRig)
		}
		return nil
	}

	// Dry run - just show what would be restarted
	if crewDryRun {
		fmt.Printf("Would restart %d clan session(s):\n\n", len(targets))
		for _, agent := range targets {
			fmt.Printf("  %s %s/clan/%s\n", AgentTypeIcons[AgentCrew], agent.Warband, agent.AgentName)
		}
		return nil
	}

	fmt.Printf("Restarting %d clan session(s)...\n\n", len(targets))

	var succeeded, failed int
	var failures []string

	for _, agent := range targets {
		agentName := fmt.Sprintf("%s/clan/%s", agent.Warband, agent.AgentName)

		// Use crewRig temporarily to get the right clan manager
		savedRig := crewRig
		crewRig = agent.Warband

		crewMgr, _, err := getCrewManager(crewRig)
		if err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: %v", agentName, err))
			fmt.Printf("  %s %s\n", style.ErrorPrefix, agentName)
			crewRig = savedRig
			continue
		}

		// Use manager's Start() with restart options
		err = crewMgr.Start(agent.AgentName, clan.StartOptions{
			KillExisting:  true,      // Kill old session if running
			Topic:         "restart", // Startup signal topic
			AgentOverride: crewAgentOverride,
		})
		if err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: %v", agentName, err))
			fmt.Printf("  %s %s\n", style.ErrorPrefix, agentName)
		} else {
			succeeded++
			fmt.Printf("  %s %s\n", style.SuccessPrefix, agentName)
		}

		crewRig = savedRig

		// Small delay between restarts to avoid overwhelming the system
		time.Sleep(constants.ShutdownNotifyDelay)
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("%s Restart complete: %d succeeded, %d failed\n",
			style.WarningPrefix, succeeded, failed)
		for _, f := range failures {
			fmt.Printf("  %s\n", style.Dim.Render(f))
		}
		return fmt.Errorf("%d restart(s) failed", failed)
	}

	fmt.Printf("%s Restart complete: %d clan session(s) restarted\n", style.SuccessPrefix, succeeded)
	return nil
}

// runCrewStop stops one or more clan workers.
// Supports: "name", "warband/name" formats, "warband" (to stop all in warband), or --all.
func runCrewStop(cmd *cobra.Command, args []string) error {
	// Handle --all flag
	if crewAll {
		return runCrewStopAll()
	}

	// Handle 0 args: default to all in inferred warband
	if len(args) == 0 {
		return runCrewStopAll()
	}

	// Handle 1 arg without "/": check if it's a warband name
	// If so, stop all clan in that warband
	if len(args) == 1 && !strings.Contains(args[0], "/") {
		// Try to interpret as warband name
		if _, _, err := getRig(args[0]); err == nil {
			// It's a valid warband name - stop all clan in that warband
			crewRig = args[0]
			return runCrewStopAll()
		}
		// Not a warband name - fall through to treat as clan name
	}

	var lastErr error
	t := tmux.NewTmux()

	for _, arg := range args {
		name := arg
		rigOverride := crewRig

		// Parse warband/name format (e.g., "relics/emma" -> warband=relics, name=emma)
		if warband, crewName, ok := parseRigSlashName(name); ok {
			if rigOverride == "" {
				rigOverride = warband
			}
			name = crewName
		}

		_, r, err := getCrewManager(rigOverride)
		if err != nil {
			fmt.Printf("Error stopping %s: %v\n", arg, err)
			lastErr = err
			continue
		}

		sessionID := crewSessionName(r.Name, name)

		// Check if session exists
		hasSession, err := t.HasSession(sessionID)
		if err != nil {
			fmt.Printf("Error checking session %s: %v\n", sessionID, err)
			lastErr = err
			continue
		}
		if !hasSession {
			fmt.Printf("No session found for %s/%s\n", r.Name, name)
			continue
		}

		// Dry run - just show what would be stopped
		if crewDryRun {
			fmt.Printf("Would stop %s/%s (session: %s)\n", r.Name, name, sessionID)
			continue
		}

		// Capture output before stopping (best effort)
		var output string
		if !crewForce {
			output, _ = t.CapturePane(sessionID, 50)
		}

		// Kill the session
		if err := t.KillSession(sessionID); err != nil {
			fmt.Printf("  %s [%s] %s: %s\n",
				style.ErrorPrefix,
				r.Name, name,
				style.Dim.Render(err.Error()))
			lastErr = err
			continue
		}

		fmt.Printf("  %s [%s] %s: stopped\n",
			style.SuccessPrefix,
			r.Name, name)

		// Log kill event to encampment log
		townRoot, _ := workspace.Find(r.Path)
		if townRoot != "" {
			agent := fmt.Sprintf("%s/clan/%s", r.Name, name)
			logger := encampmentlog.NewLogger(townRoot)
			_ = logger.Log(encampmentlog.EventKill, agent, "hd clan stop")
		}

		// Log captured output (truncated)
		if len(output) > 200 {
			output = output[len(output)-200:]
		}
		if output != "" {
			fmt.Printf("      %s\n", style.Dim.Render("(output captured)"))
		}
	}

	return lastErr
}

// runCrewStopAll stops all running clan sessions.
// If crewRig is set, only stops clan in that warband.
func runCrewStopAll() error {
	// Get all agent sessions (including raiders to find clan)
	agents, err := getAgentSessions(true)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	// Filter to clan agents only
	var targets []*AgentSession
	for _, agent := range agents {
		if agent.Type != AgentCrew {
			continue
		}
		// Filter by warband if specified
		if crewRig != "" && agent.Warband != crewRig {
			continue
		}
		targets = append(targets, agent)
	}

	if len(targets) == 0 {
		fmt.Println("No running clan sessions to stop.")
		if crewRig != "" {
			fmt.Printf("  (filtered by warband: %s)\n", crewRig)
		}
		return nil
	}

	// Dry run - just show what would be stopped
	if crewDryRun {
		fmt.Printf("Would stop %d clan session(s):\n\n", len(targets))
		for _, agent := range targets {
			fmt.Printf("  %s %s/clan/%s\n", AgentTypeIcons[AgentCrew], agent.Warband, agent.AgentName)
		}
		return nil
	}

	fmt.Printf("%s Stopping %d clan session(s)...\n\n",
		style.Bold.Render("🛑"), len(targets))

	t := tmux.NewTmux()
	var succeeded, failed int
	var failures []string

	for _, agent := range targets {
		agentName := fmt.Sprintf("%s/clan/%s", agent.Warband, agent.AgentName)
		sessionID := agent.Name // agent.Name IS the tmux session name

		// Capture output before stopping (best effort)
		var output string
		if !crewForce {
			output, _ = t.CapturePane(sessionID, 50)
		}

		// Kill the session
		if err := t.KillSession(sessionID); err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: %v", agentName, err))
			fmt.Printf("  %s %s\n", style.ErrorPrefix, agentName)
			continue
		}

		succeeded++
		fmt.Printf("  %s %s\n", style.SuccessPrefix, agentName)

		// Log kill event to encampment log
		townRoot, _ := workspace.FindFromCwd()
		if townRoot != "" {
			logger := encampmentlog.NewLogger(townRoot)
			_ = logger.Log(encampmentlog.EventKill, agentName, "hd clan stop --all")
		}

		// Log captured output (truncated)
		if len(output) > 200 {
			output = output[len(output)-200:]
		}
		if output != "" {
			fmt.Printf("      %s\n", style.Dim.Render("(output captured)"))
		}
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("%s Stop complete: %d succeeded, %d failed\n",
			style.WarningPrefix, succeeded, failed)
		for _, f := range failures {
			fmt.Printf("  %s\n", style.Dim.Render(f))
		}
		return fmt.Errorf("%d stop(s) failed", failed)
	}

	fmt.Printf("%s Stop complete: %d clan session(s) stopped\n", style.SuccessPrefix, succeeded)
	return nil
}
