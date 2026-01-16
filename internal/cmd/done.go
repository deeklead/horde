package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/drums"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/encampmentlog"
	"github.com/deeklead/horde/internal/workspace"
)

var doneCmd = &cobra.Command{
	Use:     "done",
	GroupID: GroupWork,
	Short:   "Signal work ready for merge queue",
	Long: `Signal that your work is complete and ready for the merge queue.

This is a convenience command for raiders that:
1. Submits the current branch to the merge queue
2. Auto-detects issue ID from branch name
3. Notifies the Witness with the exit outcome
4. Exits the Claude session (raiders don't stay alive after completion)

Exit statuses:
  COMPLETED      - Work done, MR submitted (default)
  ESCALATED      - Hit blocker, needs human intervention
  DEFERRED       - Work paused, issue still open
  PHASE_COMPLETE - Phase done, awaiting gate (use --phase-complete)

Phase handoff workflow:
  When a totem has gate steps (async waits), use --phase-complete to signal
  that the current phase is complete but work continues after the gate closes.
  The Witness will recycle this raider and dispatch a new one when the gate
  resolves.

Examples:
  hd done                              # Submit branch, notify COMPLETED, exit session
  hd done --issue gt-abc               # Explicit issue ID
  hd done --status ESCALATED           # Signal blocker, skip MR
  hd done --status DEFERRED            # Pause work, skip MR
  hd done --phase-complete --gate g-x  # Phase done, waiting on gate g-x`,
	RunE: runDone,
}

var (
	doneIssue         string
	donePriority      int
	doneStatus        string
	donePhaseComplete bool
	doneGate          string
	doneCleanupStatus string
)

// Valid exit types for hd done
const (
	ExitCompleted     = "COMPLETED"
	ExitEscalated     = "ESCALATED"
	ExitDeferred      = "DEFERRED"
	ExitPhaseComplete = "PHASE_COMPLETE"
)

func init() {
	doneCmd.Flags().StringVar(&doneIssue, "issue", "", "Source issue ID (default: parse from branch name)")
	doneCmd.Flags().IntVarP(&donePriority, "priority", "p", -1, "Override priority (0-4, default: inherit from issue)")
	doneCmd.Flags().StringVar(&doneStatus, "status", ExitCompleted, "Exit status: COMPLETED, ESCALATED, or DEFERRED")
	doneCmd.Flags().BoolVar(&donePhaseComplete, "phase-complete", false, "Signal phase complete - await gate before continuing")
	doneCmd.Flags().StringVar(&doneGate, "gate", "", "Gate bead ID to wait on (with --phase-complete)")
	doneCmd.Flags().StringVar(&doneCleanupStatus, "cleanup-status", "", "Git cleanup status: clean, uncommitted, unpushed, stash, unknown (ZFC: agent-observed)")

	rootCmd.AddCommand(doneCmd)
}

func runDone(cmd *cobra.Command, args []string) error {
	// Handle --phase-complete flag (overrides --status)
	var exitType string
	if donePhaseComplete {
		exitType = ExitPhaseComplete
		if doneGate == "" {
			return fmt.Errorf("--phase-complete requires --gate <gate-id>")
		}
	} else {
		// Validate exit status
		exitType = strings.ToUpper(doneStatus)
		if exitType != ExitCompleted && exitType != ExitEscalated && exitType != ExitDeferred {
			return fmt.Errorf("invalid exit status '%s': must be COMPLETED, ESCALATED, or DEFERRED", doneStatus)
		}
	}

	// Find workspace with fallback for deleted worktrees (hq-3xaxy)
	// If the raider's worktree was deleted by Witness before hd done finishes,
	// getcwd will fail. We fall back to HD_ENCAMPMENT_ROOT env var in that case.
	townRoot, cwd, err := workspace.FindFromCwdWithFallback()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Track if cwd is available - affects which operations we can do
	cwdAvailable := cwd != ""
	if !cwdAvailable {
		style.PrintWarning("working directory deleted (worktree nuked?), using fallback paths")
		// Try to get cwd from HD_RAIDER_PATH env var (set by session manager)
		if raiderPath := os.Getenv("HD_RAIDER_PATH"); raiderPath != "" {
			cwd = raiderPath // May still be gone, but we have a path to use
		}
	}

	// Find current warband
	rigName, _, err := findCurrentRig(townRoot)
	if err != nil {
		return err
	}

	// Initialize git - use cwd if available, otherwise use warband's warchief clone
	var g *git.Git
	if cwdAvailable {
		g = git.NewGit(cwd)
	} else {
		// Fallback: use the warband's warchief clone for git operations
		warchiefClone := filepath.Join(townRoot, rigName, "warchief", "warband")
		g = git.NewGit(warchiefClone)
	}

	// Get current branch - try env var first if cwd is gone
	var branch string
	if !cwdAvailable {
		// Try to get branch from HD_BRANCH env var (set by session manager)
		branch = os.Getenv("HD_BRANCH")
	}
	if branch == "" {
		var err error
		branch, err = g.CurrentBranch()
		if err != nil {
			// Last resort: try to extract from raider name (raider/<name>-<suffix>)
			if raiderName := os.Getenv("HD_RAIDER"); raiderName != "" {
				branch = fmt.Sprintf("raider/%s", raiderName)
				style.PrintWarning("could not get branch from git, using fallback: %s", branch)
			} else {
				return fmt.Errorf("getting current branch: %w", err)
			}
		}
	}

	// Auto-detect cleanup status if not explicitly provided
	// This prevents premature raider cleanup by ensuring witness knows git state
	if doneCleanupStatus == "" {
		if !cwdAvailable {
			// Can't detect git state without working directory, default to unknown
			doneCleanupStatus = "unknown"
			style.PrintWarning("cannot detect cleanup status - working directory deleted")
		} else {
			workStatus, err := g.CheckUncommittedWork()
			if err != nil {
				style.PrintWarning("could not auto-detect cleanup status: %v", err)
			} else {
				switch {
				case workStatus.HasUncommittedChanges:
					doneCleanupStatus = "uncommitted"
				case workStatus.StashCount > 0:
					doneCleanupStatus = "stash"
				default:
					// CheckUncommittedWork.UnpushedCommits doesn't work for branches
					// without upstream tracking (common for raiders). Use the more
					// robust BranchPushedToRemote which compares against origin/main.
					pushed, unpushedCount, err := g.BranchPushedToRemote(branch, "origin")
					if err != nil {
						style.PrintWarning("could not check if branch is pushed: %v", err)
						doneCleanupStatus = "unpushed" // err on side of caution
					} else if !pushed || unpushedCount > 0 {
						doneCleanupStatus = "unpushed"
					} else {
						doneCleanupStatus = "clean"
					}
				}
			}
		}
	}

	// Parse branch info
	info := parseBranchName(branch)

	// Override with explicit flags
	issueID := doneIssue
	if issueID == "" {
		issueID = info.Issue
	}
	worker := info.Worker

	// Determine raider name from sender detection
	sender := detectSender()
	raiderName := ""
	if parts := strings.Split(sender, "/"); len(parts) >= 2 {
		raiderName = parts[len(parts)-1]
	}

	// Get agent bead ID for cross-referencing
	var agentBeadID string
	if roleInfo, err := GetRoleWithContext(cwd, townRoot); err == nil {
		ctx := RoleContext{
			Role:     roleInfo.Role,
			Warband:      roleInfo.Warband,
			Raider:  roleInfo.Raider,
			TownRoot: townRoot,
			WorkDir:  cwd,
		}
		agentBeadID = getAgentBeadID(ctx)
	}

	// Get configured default branch for this warband
	defaultBranch := "main" // fallback
	if rigCfg, err := warband.LoadRigConfig(filepath.Join(townRoot, rigName)); err == nil && rigCfg.DefaultBranch != "" {
		defaultBranch = rigCfg.DefaultBranch
	}

	// For COMPLETED, we need an issue ID and branch must not be the default branch
	var mrID string
	if exitType == ExitCompleted {
		if branch == defaultBranch || branch == "master" {
			return fmt.Errorf("cannot submit %s/master branch to merge queue", defaultBranch)
		}

		// CRITICAL: Verify work exists before completing (hq-xthqf)
		// Raiders calling hd done without commits results in lost work.
		// We MUST check for:
		// 1. Working directory availability (can't verify git state without it)
		// 2. Uncommitted changes (work that would be lost)
		// 3. Unique commits compared to origin (ensures branch was pushed with actual work)

		// Block if working directory not available - can't verify git state
		if !cwdAvailable {
			return fmt.Errorf("cannot complete: working directory not available (worktree deleted?)\nUse --status DEFERRED to exit without completing")
		}

		// Block if there are uncommitted changes (would be lost on completion)
		workStatus, err := g.CheckUncommittedWork()
		if err != nil {
			return fmt.Errorf("checking git status: %w", err)
		}
		if workStatus.HasUncommittedChanges {
			return fmt.Errorf("cannot complete: uncommitted changes would be lost\nCommit your changes first, or use --status DEFERRED to exit without completing\nUncommitted: %s", workStatus.String())
		}

		// Check that branch has commits ahead of origin/default (not local default)
		// This ensures we compare against the remote, not a potentially stale local copy
		originDefault := "origin/" + defaultBranch
		aheadCount, err := g.CommitsAhead(originDefault, "HEAD")
		if err != nil {
			// Fallback to local branch comparison if origin not available
			aheadCount, err = g.CommitsAhead(defaultBranch, branch)
			if err != nil {
				return fmt.Errorf("checking commits ahead of %s: %w", defaultBranch, err)
			}
		}
		if aheadCount == 0 {
			return fmt.Errorf("branch '%s' has 0 commits ahead of %s; nothing to merge\nMake and commit changes first, or use --status DEFERRED to exit without completing", branch, originDefault)
		}

		// CRITICAL: Push branch BEFORE creating MR bead (hq-6dk53, hq-a4ksk)
		// The MR bead triggers Forge to process this branch. If the branch
		// isn't pushed yet, Forge finds nothing to merge. The worktree gets
		// nuked at the end of hd done, so the commits are lost forever.
		fmt.Printf("Pushing branch to remote...\n")
		if err := g.Push("origin", branch, false); err != nil {
			return fmt.Errorf("pushing branch '%s' to origin: %w\nCommits exist locally but failed to push. Fix the issue and retry.", branch, err)
		}
		fmt.Printf("%s Branch pushed to origin\n", style.Bold.Render("✓"))

		if issueID == "" {
			return fmt.Errorf("cannot determine source issue from branch '%s'; use --issue to specify", branch)
		}

		// Initialize relics
		bd := relics.New(relics.ResolveRelicsDir(cwd))

		// Determine target branch (auto-detect integration branch if applicable)
		target := defaultBranch
		autoTarget, err := detectIntegrationBranch(bd, g, issueID)
		if err == nil && autoTarget != "" {
			target = autoTarget
		}

		// Get source issue for priority inheritance
		var priority int
		if donePriority >= 0 {
			priority = donePriority
		} else {
			// Try to inherit from source issue
			sourceIssue, err := bd.Show(issueID)
			if err != nil {
				priority = 2 // Default
			} else {
				priority = sourceIssue.Priority
			}
		}

		// Check if MR bead already exists for this branch (idempotency)
		existingMR, err := bd.FindMRForBranch(branch)
		if err != nil {
			style.PrintWarning("could not check for existing MR: %v", err)
			// Continue with creation attempt - Create will fail if duplicate
		}

		if existingMR != nil {
			// MR already exists - use it instead of creating a new one
			mrID = existingMR.ID
			fmt.Printf("%s MR already exists (idempotent)\n", style.Bold.Render("✓"))
			fmt.Printf("  MR ID: %s\n", style.Bold.Render(mrID))
		} else {
			// Build MR bead title and description
			title := fmt.Sprintf("Merge: %s", issueID)
			description := fmt.Sprintf("branch: %s\ntarget: %s\nsource_issue: %s\nrig: %s",
				branch, target, issueID, rigName)
			if worker != "" {
				description += fmt.Sprintf("\nworker: %s", worker)
			}
			if agentBeadID != "" {
				description += fmt.Sprintf("\nagent_bead: %s", agentBeadID)
			}

			// Add conflict resolution tracking fields (initialized, updated by Forge)
			description += "\nretry_count: 0"
			description += "\nlast_conflict_sha: null"
			description += "\nconflict_task_id: null"

			// Create MR bead (ephemeral wisp - will be cleaned up after merge)
			mrIssue, err := bd.Create(relics.CreateOptions{
				Title:       title,
				Type:        "merge-request",
				Priority:    priority,
				Description: description,
				Ephemeral:   true,
			})
			if err != nil {
				return fmt.Errorf("creating merge request bead: %w", err)
			}
			mrID = mrIssue.ID

			// Update agent bead with active_mr reference (for traceability)
			if agentBeadID != "" {
				if err := bd.UpdateAgentActiveMR(agentBeadID, mrID); err != nil {
					style.PrintWarning("could not update agent bead with active_mr: %v", err)
				}
			}

			// Success output
			fmt.Printf("%s Work submitted to merge queue\n", style.Bold.Render("✓"))
			fmt.Printf("  MR ID: %s\n", style.Bold.Render(mrID))
		}
		fmt.Printf("  Source: %s\n", branch)
		fmt.Printf("  Target: %s\n", target)
		fmt.Printf("  Issue: %s\n", issueID)
		if worker != "" {
			fmt.Printf("  Worker: %s\n", worker)
		}
		fmt.Printf("  Priority: P%d\n", priority)
		fmt.Println()
		fmt.Printf("%s\n", style.Dim.Render("The Forge will process your merge request."))
	} else if exitType == ExitPhaseComplete {
		// Phase complete - register as waiter on gate, then recycle
		fmt.Printf("%s Phase complete, awaiting gate\n", style.Bold.Render("→"))
		fmt.Printf("  Gate: %s\n", doneGate)
		if issueID != "" {
			fmt.Printf("  Issue: %s\n", issueID)
		}
		fmt.Printf("  Branch: %s\n", branch)
		fmt.Println()
		fmt.Printf("%s\n", style.Dim.Render("Witness will dispatch new raider when gate closes."))

		// Register this raider as a waiter on the gate
		bd := relics.New(relics.ResolveRelicsDir(cwd))
		if err := bd.AddGateWaiter(doneGate, sender); err != nil {
			style.PrintWarning("could not register as gate waiter: %v", err)
		} else {
			fmt.Printf("%s Registered as waiter on gate %s\n", style.Bold.Render("✓"), doneGate)
		}
	} else {
		// For ESCALATED or DEFERRED, just print status
		fmt.Printf("%s Signaling %s\n", style.Bold.Render("→"), exitType)
		if issueID != "" {
			fmt.Printf("  Issue: %s\n", issueID)
		}
		fmt.Printf("  Branch: %s\n", branch)
	}

	// Notify Witness about completion
	// Use encampment-level relics for cross-agent drums
	townRouter := drums.NewRouter(townRoot)
	witnessAddr := fmt.Sprintf("%s/witness", rigName)

	// Build notification body
	var bodyLines []string
	bodyLines = append(bodyLines, fmt.Sprintf("Exit: %s", exitType))
	if issueID != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Issue: %s", issueID))
	}
	if mrID != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("MR: %s", mrID))
	}
	if doneGate != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Gate: %s", doneGate))
	}
	bodyLines = append(bodyLines, fmt.Sprintf("Branch: %s", branch))

	doneNotification := &drums.Message{
		To:      witnessAddr,
		From:    sender,
		Subject: fmt.Sprintf("RAIDER_DONE %s", raiderName),
		Body:    strings.Join(bodyLines, "\n"),
	}

	fmt.Printf("\nNotifying Witness...\n")
	if err := townRouter.Send(doneNotification); err != nil {
		style.PrintWarning("could not notify witness: %v", err)
	} else {
		fmt.Printf("%s Witness notified of %s\n", style.Bold.Render("✓"), exitType)
	}

	// Notify dispatcher if work was dispatched by another agent
	if issueID != "" {
		if dispatcher := getDispatcherFromBead(cwd, issueID); dispatcher != "" && dispatcher != sender {
			dispatcherNotification := &drums.Message{
				To:      dispatcher,
				From:    sender,
				Subject: fmt.Sprintf("WORK_DONE: %s", issueID),
				Body:    strings.Join(bodyLines, "\n"),
			}
			if err := townRouter.Send(dispatcherNotification); err != nil {
				style.PrintWarning("could not notify dispatcher %s: %v", dispatcher, err)
			} else {
				fmt.Printf("%s Dispatcher %s notified of %s\n", style.Bold.Render("✓"), dispatcher, exitType)
			}
		}
	}

	// Log done event (encampmentlog and activity feed)
	_ = LogDone(townRoot, sender, issueID)
	_ = events.LogFeed(events.TypeDone, sender, events.DonePayload(issueID, branch))

	// Update agent bead state (ZFC: self-report completion)
	updateAgentStateOnDone(cwd, townRoot, exitType, issueID)

	// Self-cleaning: Nuke our own sandbox and session (if we're a raider)
	// This is the self-cleaning model - raiders clean up after themselves
	// "done means gone" - both worktree and session are terminated
	selfCleanAttempted := false
	if exitType == ExitCompleted {
		if roleInfo, err := GetRoleWithContext(cwd, townRoot); err == nil && roleInfo.Role == RoleRaider {
			selfCleanAttempted = true

			// Step 1: Nuke the worktree
			if err := selfNukeRaider(roleInfo, townRoot); err != nil {
				// Non-fatal: Witness will clean up if we fail
				style.PrintWarning("worktree nuke failed: %v (Witness will clean up)", err)
			} else {
				fmt.Printf("%s Worktree nuked\n", style.Bold.Render("✓"))
			}

			// Step 2: Kill our own session (this terminates Claude and the shell)
			// This is the last thing we do - the process will be killed when tmux session dies
			fmt.Printf("%s Terminating session (done means gone)\n", style.Bold.Render("→"))
			if err := selfKillSession(townRoot, roleInfo); err != nil {
				// If session kill fails, fall through to os.Exit
				style.PrintWarning("session kill failed: %v", err)
			}
			// If selfKillSession succeeds, we won't reach here (process killed by tmux)
		}
	}

	// Fallback exit for non-raiders or if self-clean failed
	fmt.Println()
	fmt.Printf("%s Session exiting\n", style.Bold.Render("→"))
	if !selfCleanAttempted {
		fmt.Printf("  Witness will handle cleanup.\n")
	}
	fmt.Printf("  Goodbye!\n")
	os.Exit(0)

	return nil // unreachable, but keeps compiler happy
}

// updateAgentStateOnDone clears the agent's hook and reports cleanup status.
// Per gt-zecmc: observable states ("done", "idle") removed - use tmux to discover.
// Non-observable states ("stuck", "awaiting-gate") are still set since they represent
// intentional agent decisions that can't be observed from tmux.
//
// Also self-reports cleanup_status for ZFC compliance (#10).
//
// BUG FIX (hq-3xaxy): This function must be resilient to working directory deletion.
// If the raider's worktree is deleted before hd done finishes, we use env vars as fallback.
// All errors are warnings, not failures - hd done must complete even if bead ops fail.
func updateAgentStateOnDone(cwd, townRoot, exitType, _ string) { // issueID unused but kept for future audit logging
	// Get role context - try multiple sources for resilience
	roleInfo, err := GetRoleWithContext(cwd, townRoot)
	if err != nil {
		// Fallback: try to construct role info from environment variables
		// This handles the case where cwd is deleted but env vars are set
		envRole := os.Getenv("HD_ROLE")
		envRig := os.Getenv("HD_WARBAND")
		envRaider := os.Getenv("HD_RAIDER")

		if envRole == "" || envRig == "" {
			// Can't determine role, skip agent state update
			return
		}

		// Parse role string to get Role type
		parsedRole, _, _ := parseRoleString(envRole)

		roleInfo = RoleInfo{
			Role:     parsedRole,
			Warband:      envRig,
			Raider:  envRaider,
			TownRoot: townRoot,
			WorkDir:  cwd,
			Source:   "env-fallback",
		}
	}

	ctx := RoleContext{
		Role:     roleInfo.Role,
		Warband:      roleInfo.Warband,
		Raider:  roleInfo.Raider,
		TownRoot: townRoot,
		WorkDir:  cwd,
	}

	agentBeadID := getAgentBeadID(ctx)
	if agentBeadID == "" {
		return
	}

	// Use warband path for slot commands - rl slot doesn't route from encampment root
	// IMPORTANT: Use the warband's directory (not raider worktree) so rl commands
	// work even if the raider worktree is deleted.
	var relicsPath string
	switch ctx.Role {
	case RoleWarchief, RoleShaman:
		relicsPath = townRoot
	default:
		relicsPath = filepath.Join(townRoot, ctx.Warband)
	}
	bd := relics.New(relicsPath)

	// BUG FIX (gt-vwjz6): Close bannered relics before clearing the hook.
	// Previously, the agent's banner_bead slot was cleared but the bannered bead itself
	// stayed status=bannered forever. Now we close the bannered bead before clearing.
	//
	// BUG FIX (hq-i26n2): Check if agent bead exists before clearing hook.
	// Old raiders may not have identity relics, so ClearBannerBead would fail.
	// hd done must be resilient - missing agent bead is not an error.
	//
	// BUG FIX (hq-3xaxy): All bead operations are non-fatal. If the agent bead
	// is deleted by another process (e.g., Witness cleanup), we just warn.
	agentBead, err := bd.Show(agentBeadID)
	if err != nil {
		// Agent bead doesn't exist - nothing to clear, that's fine
		// This happens for raiders created before identity relics existed,
		// or if the agent bead was deleted by another process
		return
	}

	if agentBead.BannerBead != "" {
		hookedBeadID := agentBead.BannerBead
		// Only close if the bannered bead exists and is still in "bannered" status
		if hookedBead, err := bd.Show(hookedBeadID); err == nil && hookedBead.Status == relics.StatusHooked {
			if err := bd.Close(hookedBeadID); err != nil {
				// Non-fatal: warn but continue
				fmt.Fprintf(os.Stderr, "Warning: couldn't close bannered bead %s: %v\n", hookedBeadID, err)
			}
		}
	}

	// Clear the hook (work is done) - gt-zecmc
	// BUG FIX (hq-3xaxy): This is non-fatal - if hook clearing fails, warn and continue.
	// The Witness will clean up any orphaned state.
	if err := bd.ClearBannerBead(agentBeadID); err != nil {
		// Non-fatal: warn but don't fail hd done
		fmt.Fprintf(os.Stderr, "Warning: couldn't clear agent %s hook: %v\n", agentBeadID, err)
	}

	// Only set non-observable states - "stuck" and "awaiting-gate" are intentional
	// agent decisions that can't be discovered from tmux. Skip "done" and "idle"
	// since those are observable (no session = done, session + no hook = idle).
	switch exitType {
	case ExitEscalated:
		// "stuck" = agent is requesting help - not observable from tmux
		if _, err := bd.Run("agent", "state", agentBeadID, "stuck"); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: couldn't set agent %s to stuck: %v\n", agentBeadID, err)
		}
	case ExitPhaseComplete:
		// "awaiting-gate" = agent is waiting for external trigger - not observable
		if _, err := bd.Run("agent", "state", agentBeadID, "awaiting-gate"); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: couldn't set agent %s to awaiting-gate: %v\n", agentBeadID, err)
		}
	// ExitCompleted and ExitDeferred don't set state - observable from tmux
	}

	// ZFC #10: Self-report cleanup status
	// Agent observes git state and passes cleanup status via --cleanup-status flag
	if doneCleanupStatus != "" {
		cleanupStatus := parseCleanupStatus(doneCleanupStatus)
		if cleanupStatus != raider.CleanupUnknown {
			if err := bd.UpdateAgentCleanupStatus(agentBeadID, string(cleanupStatus)); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: couldn't update agent %s cleanup status: %v\n", agentBeadID, err)
				return
			}
		}
	}
}

// getDispatcherFromBead retrieves the dispatcher agent ID from the bead's attachment fields.
// Returns empty string if no dispatcher is recorded.
func getDispatcherFromBead(cwd, issueID string) string {
	if issueID == "" {
		return ""
	}

	bd := relics.New(relics.ResolveRelicsDir(cwd))
	issue, err := bd.Show(issueID)
	if err != nil {
		return ""
	}

	fields := relics.ParseAttachmentFields(issue)
	if fields == nil {
		return ""
	}

	return fields.DispatchedBy
}

// parseCleanupStatus converts a string flag value to a CleanupStatus.
// ZFC: Agent observes git state and passes the appropriate status.
func parseCleanupStatus(s string) raider.CleanupStatus {
	switch strings.ToLower(s) {
	case "clean":
		return raider.CleanupClean
	case "uncommitted", "has_uncommitted":
		return raider.CleanupUncommitted
	case "stash", "has_stash":
		return raider.CleanupStash
	case "unpushed", "has_unpushed":
		return raider.CleanupUnpushed
	default:
		return raider.CleanupUnknown
	}
}

// selfNukeRaider deletes this raider's worktree (self-cleaning model).
// Called by raiders when they complete work via `hd done`.
// This is safe because:
// 1. Work has been pushed to origin (MR is in queue)
// 2. We're about to exit anyway
// 3. Unix allows deleting directories while processes run in them
func selfNukeRaider(roleInfo RoleInfo, _ string) error {
	if roleInfo.Role != RoleRaider || roleInfo.Raider == "" || roleInfo.Warband == "" {
		return fmt.Errorf("not a raider: role=%s, raider=%s, warband=%s", roleInfo.Role, roleInfo.Raider, roleInfo.Warband)
	}

	// Get raider manager using existing helper
	mgr, _, err := getRaiderManager(roleInfo.Warband)
	if err != nil {
		return fmt.Errorf("getting raider manager: %w", err)
	}

	// Use nuclear=true since we know we just pushed our work
	// The branch is pushed, MR is created, we're clean
	if err := mgr.RemoveWithOptions(roleInfo.Raider, true, true); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}

	return nil
}

// selfKillSession terminates the raider's own tmux session after logging the event.
// This completes the self-cleaning model: "done means gone" - both worktree and session.
//
// The raider determines its session from environment variables:
// - HD_WARBAND: the warband name
// - HD_RAIDER: the raider name
// Session name format: gt-<warband>-<raider>
func selfKillSession(townRoot string, roleInfo RoleInfo) error {
	// Get session info from environment (set at session startup)
	rigName := os.Getenv("HD_WARBAND")
	raiderName := os.Getenv("HD_RAIDER")

	// Fall back to roleInfo if env vars not set (shouldn't happen but be safe)
	if rigName == "" {
		rigName = roleInfo.Warband
	}
	if raiderName == "" {
		raiderName = roleInfo.Raider
	}

	if rigName == "" || raiderName == "" {
		return fmt.Errorf("cannot determine session: warband=%q, raider=%q", rigName, raiderName)
	}

	sessionName := fmt.Sprintf("hd-%s-%s", rigName, raiderName)
	agentID := fmt.Sprintf("%s/raiders/%s", rigName, raiderName)

	// Log to encampmentlog (human-readable audit log)
	if townRoot != "" {
		logger := encampmentlog.NewLogger(townRoot)
		_ = logger.Log(encampmentlog.EventKill, agentID, "self-clean: done means gone")
	}

	// Log to events (JSON audit log with structured payload)
	_ = events.LogFeed(events.TypeSessionDeath, agentID,
		events.SessionDeathPayload(sessionName, agentID, "self-clean: done means gone", "hd done"))

	// Kill our own tmux session
	// This will terminate Claude and the shell, completing the self-cleaning cycle.
	// We use exec.Command instead of the tmux package to avoid import cycles.
	cmd := exec.Command("tmux", "kill-session", "-t", sessionName) //nolint:gosec // G204: sessionName is derived from env vars, not user input
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("killing session %s: %w", sessionName, err)
	}

	return nil
}
