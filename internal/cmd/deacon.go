package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/claude"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/shaman"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/runtime"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

// getShamanSessionName returns the Shaman session name.
func getShamanSessionName() string {
	return session.ShamanSessionName()
}

var shamanCmd = &cobra.Command{
	Use:     "shaman",
	Aliases: []string{"dea"},
	GroupID: GroupAgents,
	Short:   "Manage the Shaman session",
	RunE:    requireSubcommand,
	Long: `Manage the Shaman tmux session.

The Shaman is the hierarchical health-check orchestrator for Horde.
It monitors the Warchief and Witnesses, handles lifecycle requests, and
keeps the encampment running. Use the subcommands to start, stop, summon,
and check status.`,
}

var shamanStartCmd = &cobra.Command{
	Use:     "start",
	Aliases: []string{"muster"},
	Short:   "Start the Shaman session",
	Long: `Start the Shaman tmux session.

Creates a new detached tmux session for the Shaman and launches Claude.
The session runs in the workspace root directory.`,
	RunE: runShamanStart,
}

var shamanStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Shaman session",
	Long: `Stop the Shaman tmux session.

Attempts graceful shutdown first (Ctrl-C), then kills the tmux session.`,
	RunE: runShamanStop,
}

var shamanAttachCmd = &cobra.Command{
	Use:     "summon",
	Aliases: []string{"at"},
	Short:   "Summon to the Shaman session",
	Long: `Summon to the running Shaman tmux session.

Attaches the current terminal to the Shaman's tmux session.
Dismiss with Ctrl-B D.`,
	RunE: runShamanAttach,
}

var shamanStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check Shaman session status",
	Long:  `Check if the Shaman tmux session is currently running.`,
	RunE:  runShamanStatus,
}

var shamanRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Shaman session",
	Long: `Restart the Shaman tmux session.

Stops the current session (if running) and starts a fresh one.`,
	RunE: runShamanRestart,
}

var shamanAgentOverride string

var shamanHeartbeatCmd = &cobra.Command{
	Use:   "heartbeat [action]",
	Short: "Update the Shaman heartbeat",
	Long: `Update the Shaman heartbeat file.

The heartbeat signals to the daemon that the Shaman is alive and working.
Call this at the start of each wake cycle to prevent daemon pokes.

Examples:
  hd shaman heartbeat                    # Touch heartbeat with timestamp
  hd shaman heartbeat "checking warchief"   # Touch with action description`,
	RunE: runShamanHeartbeat,
}

var shamanTriggerPendingCmd = &cobra.Command{
	Use:   "trigger-pending",
	Short: "Trigger pending raider spawns (bootstrap mode)",
	Long: `Check inbox for RAIDER_STARTED messages and trigger ready raiders.

⚠️  BOOTSTRAP MODE ONLY - Uses regex detection (ZFC violation acceptable).

This command uses WaitForRuntimeReady (regex) to detect when the runtime is ready.
This is appropriate for daemon bootstrap when no AI is available.

In steady-state, the Shaman should use AI-based observation instead:
  hd shaman pending     # View pending spawns with captured output
  hd peek <session>     # Observe session output (AI analyzes)
  hd signal <session>    # Trigger when AI determines ready

This command is typically called by the daemon during cold startup.`,
	RunE: runShamanTriggerPending,
}

var shamanHealthCheckCmd = &cobra.Command{
	Use:   "health-check <agent>",
	Short: "Send a health check ping to an agent and track response",
	Long: `Send a HEALTH_CHECK signal to an agent and wait for response.

This command is used by the Shaman during health rounds to detect stuck sessions.
It tracks consecutive failures and determines when force-kill is warranted.

The detection protocol:
1. Send HEALTH_CHECK signal to the agent
2. Wait for agent to update their bead (configurable timeout, default 30s)
3. If no activity update, increment failure counter
4. After N consecutive failures (default 3), recommend force-kill

Exit codes:
  0 - Agent responded or is in cooldown (no action needed)
  1 - Error occurred
  2 - Agent should be force-killed (consecutive failures exceeded)

Examples:
  hd shaman health-check horde/raiders/max
  hd shaman health-check horde/witness --timeout=60s
  hd shaman health-check shaman --failures=5`,
	Args: cobra.ExactArgs(1),
	RunE: runShamanHealthCheck,
}

var shamanForceKillCmd = &cobra.Command{
	Use:   "force-kill <agent>",
	Short: "Force-kill an unresponsive agent session",
	Long: `Force-kill an agent session that has been detected as stuck.

This command is used by the Shaman when an agent fails consecutive health checks.
It performs the force-kill protocol:

1. Log the intervention (send drums to agent)
2. Kill the tmux session
3. Update agent bead state to "killed"
4. Notify warchief (optional, for visibility)

After force-kill, the agent is 'asleep'. Normal wake mechanisms apply:
- hd warband boot restarts it
- Or stays asleep until next activity trigger

This respects the cooldown period - won't kill if recently killed.

Examples:
  hd shaman force-kill horde/raiders/max
  hd shaman force-kill horde/witness --reason="unresponsive for 90s"`,
	Args: cobra.ExactArgs(1),
	RunE: runShamanForceKill,
}

var shamanHealthStateCmd = &cobra.Command{
	Use:   "health-state",
	Short: "Show health check state for all monitored agents",
	Long: `Display the current health check state including:
- Consecutive failure counts
- Last ping and response times
- Force-kill history and cooldowns

This helps the Shaman understand which agents may need attention.`,
	RunE: runShamanHealthState,
}

var shamanStaleHooksCmd = &cobra.Command{
	Use:   "stale-hooks",
	Short: "Find and unhook stale bannered relics",
	Long: `Find relics stuck in 'bannered' status and unhook them if the agent is gone.

Relics can get stuck in 'bannered' status when agents die or abandon work.
This command finds bannered relics older than the threshold (default: 1 hour),
checks if the assignee agent is still alive, and unhooks them if not.

Examples:
  hd shaman stale-hooks                 # Find and unhook stale relics
  hd shaman stale-hooks --dry-run       # Preview what would be unhooked
  hd shaman stale-hooks --max-age=30m   # Use 30 minute threshold`,
	RunE: runShamanStaleHooks,
}

var shamanPauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause the Shaman to prevent scout actions",
	Long: `Pause the Shaman to prevent it from performing any scout actions.

When paused, the Shaman:
- Will not create scout totems
- Will not run health checks
- Will not take any autonomous actions
- Will display a PAUSED message on startup

The pause state persists across session restarts. Use 'hd shaman resume'
to allow the Shaman to work again.

Examples:
  hd shaman pause                           # Pause with no reason
  hd shaman pause --reason="testing"        # Pause with a reason`,
	RunE: runShamanPause,
}

var shamanResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume the Shaman to allow scout actions",
	Long: `Resume the Shaman so it can perform scout actions again.

This removes the pause file and allows the Shaman to work normally.`,
	RunE: runShamanResume,
}

var (
	triggerTimeout time.Duration

	// Health check flags
	healthCheckTimeout  time.Duration
	healthCheckFailures int
	healthCheckCooldown time.Duration

	// Force kill flags
	forceKillReason     string
	forceKillSkipNotify bool

	// Stale hooks flags
	staleHooksMaxAge time.Duration
	staleHooksDryRun bool

	// Pause flags
	pauseReason string
)

func init() {
	shamanCmd.AddCommand(shamanStartCmd)
	shamanCmd.AddCommand(shamanStopCmd)
	shamanCmd.AddCommand(shamanAttachCmd)
	shamanCmd.AddCommand(shamanStatusCmd)
	shamanCmd.AddCommand(shamanRestartCmd)
	shamanCmd.AddCommand(shamanHeartbeatCmd)
	shamanCmd.AddCommand(shamanTriggerPendingCmd)
	shamanCmd.AddCommand(shamanHealthCheckCmd)
	shamanCmd.AddCommand(shamanForceKillCmd)
	shamanCmd.AddCommand(shamanHealthStateCmd)
	shamanCmd.AddCommand(shamanStaleHooksCmd)
	shamanCmd.AddCommand(shamanPauseCmd)
	shamanCmd.AddCommand(shamanResumeCmd)

	// Flags for trigger-pending
	shamanTriggerPendingCmd.Flags().DurationVar(&triggerTimeout, "timeout", 2*time.Second,
		"Timeout for checking if Claude is ready")

	// Flags for health-check
	shamanHealthCheckCmd.Flags().DurationVar(&healthCheckTimeout, "timeout", 30*time.Second,
		"How long to wait for agent response")
	shamanHealthCheckCmd.Flags().IntVar(&healthCheckFailures, "failures", 3,
		"Number of consecutive failures before recommending force-kill")
	shamanHealthCheckCmd.Flags().DurationVar(&healthCheckCooldown, "cooldown", 5*time.Minute,
		"Minimum time between force-kills of same agent")

	// Flags for force-kill
	shamanForceKillCmd.Flags().StringVar(&forceKillReason, "reason", "",
		"Reason for force-kill (included in notifications)")
	shamanForceKillCmd.Flags().BoolVar(&forceKillSkipNotify, "skip-notify", false,
		"Skip sending notification drums to warchief")

	// Flags for stale-hooks
	shamanStaleHooksCmd.Flags().DurationVar(&staleHooksMaxAge, "max-age", 1*time.Hour,
		"Maximum age before a bannered bead is considered stale")
	shamanStaleHooksCmd.Flags().BoolVar(&staleHooksDryRun, "dry-run", false,
		"Preview what would be unhooked without making changes")

	// Flags for pause
	shamanPauseCmd.Flags().StringVar(&pauseReason, "reason", "",
		"Reason for pausing the Shaman")

	shamanStartCmd.Flags().StringVar(&shamanAgentOverride, "agent", "", "Agent alias to run the Shaman with (overrides encampment default)")
	shamanAttachCmd.Flags().StringVar(&shamanAgentOverride, "agent", "", "Agent alias to run the Shaman with (overrides encampment default)")
	shamanRestartCmd.Flags().StringVar(&shamanAgentOverride, "agent", "", "Agent alias to run the Shaman with (overrides encampment default)")

	rootCmd.AddCommand(shamanCmd)
}

func runShamanStart(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	sessionName := getShamanSessionName()

	// Check if session already exists
	running, err := t.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if running {
		return fmt.Errorf("Shaman session already running. Summon with: hd shaman summon")
	}

	if err := startShamanSession(t, sessionName, shamanAgentOverride); err != nil {
		return err
	}

	fmt.Printf("%s Shaman session started. Summon with: %s\n",
		style.Bold.Render("✓"),
		style.Dim.Render("hd shaman summon"))

	return nil
}

// startShamanSession creates and initializes the Shaman tmux session.
func startShamanSession(t *tmux.Tmux, sessionName, agentOverride string) error {
	// Find workspace root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Shaman runs from its own directory (for correct role detection by hd rally)
	shamanDir := filepath.Join(townRoot, "shaman")

	// Ensure shaman directory exists
	if err := os.MkdirAll(shamanDir, 0755); err != nil {
		return fmt.Errorf("creating shaman directory: %w", err)
	}

	// Ensure Claude settings exist (autonomous role needs drums in SessionStart)
	if err := claude.EnsureSettingsForRole(shamanDir, "shaman"); err != nil {
		return fmt.Errorf("creating shaman settings: %w", err)
	}

	// Build startup command first
	// Export HD_ROLE and BD_ACTOR in the command since tmux SetEnvironment only affects new panes
	startupCmd, err := config.BuildAgentStartupCommandWithAgentOverride("shaman", "", townRoot, "", "", agentOverride)
	if err != nil {
		return fmt.Errorf("building startup command: %w", err)
	}

	// Create session with command directly to avoid send-keys race condition.
	// See: https://github.com/anthropics/horde/issues/280
	fmt.Println("Starting Shaman session...")
	if err := t.NewSessionWithCommand(sessionName, shamanDir, startupCmd); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	// Set environment (non-fatal: session works without these)
	// Use centralized AgentEnv for consistency across all role startup paths
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:     "shaman",
		TownRoot: townRoot,
	})
	for k, v := range envVars {
		_ = t.SetEnvironment(sessionName, k, v)
	}

	// Apply Shaman theme (non-fatal: theming failure doesn't affect operation)
	// Note: ConfigureHordeSession includes cycle bindings
	theme := tmux.ShamanTheme()
	_ = t.ConfigureHordeSession(sessionName, theme, "", "Shaman", "health-check")

	// Wait for Claude to start
	if err := t.WaitForCommand(sessionName, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		return fmt.Errorf("waiting for shaman to start: %w", err)
	}
	time.Sleep(constants.ShutdownNotifyDelay)

	runtimeConfig := config.LoadRuntimeConfig("")
	_ = runtime.RunStartupFallback(t, sessionName, "shaman", runtimeConfig)

	// Inject startup signal for predecessor discovery via /resume
	if err := session.StartupNudge(t, sessionName, session.StartupNudgeConfig{
		Recipient: "shaman",
		Sender:    "daemon",
		Topic:     "scout",
	}); err != nil {
		style.PrintWarning("failed to send startup signal: %v", err)
	}

	// GUPP: Horde Universal Propulsion Principle
	// Send the propulsion signal to trigger autonomous scout execution.
	// Wait for beacon to be fully processed (needs to be separate prompt)
	time.Sleep(2 * time.Second)
	if err := t.SignalSession(sessionName, session.PropulsionNudgeForRole("shaman", shamanDir)); err != nil {
		return fmt.Errorf("sending propulsion signal: %w", err)
	}

	return nil
}

func runShamanStop(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	sessionName := getShamanSessionName()

	// Check if session exists
	running, err := t.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return errors.New("Shaman session is not running")
	}

	fmt.Println("Stopping Shaman session...")

	// Try graceful shutdown first (best-effort interrupt)
	_ = t.SendKeysRaw(sessionName, "C-c")
	time.Sleep(100 * time.Millisecond)

	// Kill the session
	if err := t.KillSession(sessionName); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}

	fmt.Printf("%s Shaman session stopped.\n", style.Bold.Render("✓"))
	return nil
}

func runShamanAttach(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	sessionName := getShamanSessionName()

	// Check if session exists
	running, err := t.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		// Auto-start if not running
		fmt.Println("Shaman session not running, starting...")
		if err := startShamanSession(t, sessionName, shamanAgentOverride); err != nil {
			return err
		}
	}
	// Session uses a respawn loop, so Claude restarts automatically if it exits

	// Use shared summon helper (smart: links if inside tmux, attaches if outside)
	return attachToTmuxSession(sessionName)
}

func runShamanStatus(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	sessionName := getShamanSessionName()

	// Check pause state first (most important)
	townRoot, _ := workspace.FindFromCwdOrError()
	if townRoot != "" {
		paused, state, err := shaman.IsPaused(townRoot)
		if err == nil && paused {
			fmt.Printf("%s SHAMAN PAUSED\n", style.Bold.Render("⏸️"))
			if state.Reason != "" {
				fmt.Printf("  Reason: %s\n", state.Reason)
			}
			fmt.Printf("  Paused at: %s\n", state.PausedAt.Format(time.RFC3339))
			fmt.Printf("  Paused by: %s\n", state.PausedBy)
			fmt.Println()
			fmt.Printf("Resume with: %s\n", style.Dim.Render("hd shaman resume"))
			fmt.Println()
		}
	}

	running, err := t.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}

	if running {
		// Get session info for more details
		info, err := t.GetSessionInfo(sessionName)
		if err == nil {
			status := "detached"
			if info.Attached {
				status = "attached"
			}
			fmt.Printf("%s Shaman session is %s\n",
				style.Bold.Render("●"),
				style.Bold.Render("running"))
			fmt.Printf("  Status: %s\n", status)
			fmt.Printf("  Created: %s\n", info.Created)
			fmt.Printf("\nAttach with: %s\n", style.Dim.Render("hd shaman summon"))
		} else {
			fmt.Printf("%s Shaman session is %s\n",
				style.Bold.Render("●"),
				style.Bold.Render("running"))
		}
	} else {
		fmt.Printf("%s Shaman session is %s\n",
			style.Dim.Render("○"),
			"not running")
		fmt.Printf("\nStart with: %s\n", style.Dim.Render("hd shaman start"))
	}

	return nil
}

func runShamanRestart(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	sessionName := getShamanSessionName()

	running, err := t.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}

	fmt.Println("Restarting Shaman...")

	if running {
		// Kill existing session
		if err := t.KillSession(sessionName); err != nil {
			style.PrintWarning("failed to kill session: %v", err)
		}
	}

	// Start fresh
	if err := runShamanStart(cmd, args); err != nil {
		return err
	}

	fmt.Printf("%s Shaman restarted\n", style.Bold.Render("✓"))
	fmt.Printf("  %s\n", style.Dim.Render("Use 'hd shaman summon' to connect"))
	return nil
}

func runShamanHeartbeat(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Check if Shaman is paused - if so, refuse to update heartbeat
	paused, state, err := shaman.IsPaused(townRoot)
	if err != nil {
		return fmt.Errorf("checking pause state: %w", err)
	}
	if paused {
		fmt.Printf("%s Shaman is paused. Use 'hd shaman resume' to unpause.\n", style.Bold.Render("⏸️"))
		if state.Reason != "" {
			fmt.Printf("  Reason: %s\n", state.Reason)
		}
		return errors.New("Shaman is paused")
	}

	action := ""
	if len(args) > 0 {
		action = strings.Join(args, " ")
	}

	if action != "" {
		if err := shaman.TouchWithAction(townRoot, action, 0, 0); err != nil {
			return fmt.Errorf("updating heartbeat: %w", err)
		}
		fmt.Printf("%s Heartbeat updated: %s\n", style.Bold.Render("✓"), action)
	} else {
		if err := shaman.Touch(townRoot); err != nil {
			return fmt.Errorf("updating heartbeat: %w", err)
		}
		fmt.Printf("%s Heartbeat updated\n", style.Bold.Render("✓"))
	}

	return nil
}

func runShamanTriggerPending(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Step 1: Check inbox for new RAIDER_STARTED messages
	pending, err := raider.CheckInboxForSpawns(townRoot)
	if err != nil {
		return fmt.Errorf("checking inbox: %w", err)
	}

	if len(pending) == 0 {
		fmt.Printf("%s No pending spawns\n", style.Dim.Render("○"))
		return nil
	}

	fmt.Printf("%s Found %d pending muster(s)\n", style.Bold.Render("●"), len(pending))

	// Step 2: Try to trigger each pending muster
	results, err := raider.TriggerPendingSpawns(townRoot, triggerTimeout)
	if err != nil {
		return fmt.Errorf("triggering: %w", err)
	}

	// Report results
	triggered := 0
	for _, r := range results {
		if r.Triggered {
			triggered++
			fmt.Printf("  %s Triggered %s/%s\n",
				style.Bold.Render("✓"),
				r.Muster.Warband, r.Muster.Raider)
		} else if r.Error != nil {
			fmt.Printf("  %s %s/%s: %v\n",
				style.Dim.Render("⚠"),
				r.Muster.Warband, r.Muster.Raider, r.Error)
		}
	}

	// Step 3: Prune stale pending spawns (older than 5 minutes)
	pruned, _ := raider.PruneStalePending(townRoot, 5*time.Minute)
	if pruned > 0 {
		fmt.Printf("  %s Pruned %d stale muster(s)\n", style.Dim.Render("○"), pruned)
	}

	// Summary
	remaining := len(pending) - triggered
	if remaining > 0 {
		fmt.Printf("%s %d muster(s) still waiting for Claude\n",
			style.Dim.Render("○"), remaining)
	}

	return nil
}

// runShamanHealthCheck implements the health-check command.
// It sends a HEALTH_CHECK signal to an agent, waits for response, and tracks state.
func runShamanHealthCheck(cmd *cobra.Command, args []string) error {
	agent := args[0]

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load health check state
	state, err := shaman.LoadHealthCheckState(townRoot)
	if err != nil {
		return fmt.Errorf("loading health check state: %w", err)
	}
	agentState := state.GetAgentState(agent)

	// Check if agent is in cooldown
	if agentState.IsInCooldown(healthCheckCooldown) {
		remaining := agentState.CooldownRemaining(healthCheckCooldown)
		fmt.Printf("%s Agent %s is in cooldown (remaining: %s)\n",
			style.Dim.Render("○"), agent, remaining.Round(time.Second))
		return nil
	}

	// Get agent bead info before ping (for baseline)
	beadID, sessionName, err := agentAddressToIDs(agent)
	if err != nil {
		return fmt.Errorf("invalid agent address: %w", err)
	}

	t := tmux.NewTmux()

	// Check if session exists
	exists, err := t.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !exists {
		fmt.Printf("%s Agent %s session not running\n", style.Dim.Render("○"), agent)
		return nil
	}

	// Get current bead update time
	baselineTime, err := getAgentBeadUpdateTime(townRoot, beadID)
	if err != nil {
		// Bead might not exist yet - that's okay
		baselineTime = time.Time{}
	}

	// Record ping
	agentState.RecordPing()

	// Send health check signal
	if err := t.SignalSession(sessionName, "HEALTH_CHECK: respond with any action to confirm responsiveness"); err != nil {
		return fmt.Errorf("sending signal: %w", err)
	}

	fmt.Printf("%s Sent HEALTH_CHECK to %s, waiting %s...\n",
		style.Bold.Render("→"), agent, healthCheckTimeout)

	// Wait for response using context and ticker for reliability
	// This prevents loop hangs if system clock changes
	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	responded := false

	for {
		select {
		case <-ctx.Done():
			goto Done
		case <-ticker.C:
			newTime, err := getAgentBeadUpdateTime(townRoot, beadID)
			if err != nil {
				continue
			}

			// If bead was updated after our baseline, agent responded
			if newTime.After(baselineTime) {
				responded = true
				goto Done
			}
		}
	}

Done:
	// Record result
	if responded {
		agentState.RecordResponse()
		if err := shaman.SaveHealthCheckState(townRoot, state); err != nil {
			style.PrintWarning("failed to save health check state: %v", err)
		}
		fmt.Printf("%s Agent %s responded (failures reset to 0)\n",
			style.Bold.Render("✓"), agent)
		return nil
	}

	// No response - record failure
	agentState.RecordFailure()
	if err := shaman.SaveHealthCheckState(townRoot, state); err != nil {
		style.PrintWarning("failed to save health check state: %v", err)
	}

	fmt.Printf("%s Agent %s did not respond (consecutive failures: %d/%d)\n",
		style.Dim.Render("⚠"), agent, agentState.ConsecutiveFailures, healthCheckFailures)

	// Check if force-kill threshold reached
	if agentState.ShouldForceKill(healthCheckFailures) {
		fmt.Printf("%s Agent %s should be force-killed\n", style.Bold.Render("✗"), agent)
		os.Exit(2) // Exit code 2 = should force-kill
	}

	return nil
}

// runShamanForceKill implements the force-kill command.
// It kills a stuck agent session and updates its bead state.
func runShamanForceKill(cmd *cobra.Command, args []string) error {
	agent := args[0]

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load health check state
	state, err := shaman.LoadHealthCheckState(townRoot)
	if err != nil {
		return fmt.Errorf("loading health check state: %w", err)
	}
	agentState := state.GetAgentState(agent)

	// Check cooldown (unless bypassed)
	if agentState.IsInCooldown(healthCheckCooldown) {
		remaining := agentState.CooldownRemaining(healthCheckCooldown)
		return fmt.Errorf("agent %s is in cooldown (remaining: %s) - cannot force-kill yet",
			agent, remaining.Round(time.Second))
	}

	// Get session name
	_, sessionName, err := agentAddressToIDs(agent)
	if err != nil {
		return fmt.Errorf("invalid agent address: %w", err)
	}

	t := tmux.NewTmux()

	// Check if session exists
	exists, err := t.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !exists {
		fmt.Printf("%s Agent %s session not running\n", style.Dim.Render("○"), agent)
		return nil
	}

	// Build reason
	reason := forceKillReason
	if reason == "" {
		reason = fmt.Sprintf("unresponsive after %d consecutive health check failures",
			agentState.ConsecutiveFailures)
	}

	// Step 1: Log the intervention (send drums to agent)
	fmt.Printf("%s Sending force-kill notification to %s...\n", style.Dim.Render("1."), agent)
	mailBody := fmt.Sprintf("Shaman detected %s as unresponsive.\nReason: %s\nAction: force-killing session", agent, reason)
	sendMail(townRoot, agent, "FORCE_KILL: unresponsive", mailBody)

	// Step 2: Kill the tmux session
	fmt.Printf("%s Killing tmux session %s...\n", style.Dim.Render("2."), sessionName)
	if err := t.KillSession(sessionName); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}

	// Step 3: Update agent bead state (optional - best effort)
	fmt.Printf("%s Updating agent bead state to 'killed'...\n", style.Dim.Render("3."))
	updateAgentBeadState(townRoot, agent, "killed", reason)

	// Step 4: Notify warchief (optional)
	if !forceKillSkipNotify {
		fmt.Printf("%s Notifying warchief...\n", style.Dim.Render("4."))
		notifyBody := fmt.Sprintf("Agent %s was force-killed by Shaman.\nReason: %s", agent, reason)
		sendMail(townRoot, "warchief/", "Agent killed: "+agent, notifyBody)
	}

	// Record force-kill in state
	agentState.RecordForceKill()
	if err := shaman.SaveHealthCheckState(townRoot, state); err != nil {
		style.PrintWarning("failed to save health check state: %v", err)
	}

	fmt.Printf("%s Force-killed agent %s (total kills: %d)\n",
		style.Bold.Render("✓"), agent, agentState.ForceKillCount)
	fmt.Printf("  %s\n", style.Dim.Render("Agent is now 'asleep'. Use 'hd warband boot' to restart."))

	return nil
}

// runShamanHealthState shows the current health check state.
func runShamanHealthState(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	state, err := shaman.LoadHealthCheckState(townRoot)
	if err != nil {
		return fmt.Errorf("loading health check state: %w", err)
	}

	if len(state.Agents) == 0 {
		fmt.Printf("%s No health check state recorded yet\n", style.Dim.Render("○"))
		return nil
	}

	fmt.Printf("%s Health Check State (updated %s)\n\n",
		style.Bold.Render("●"),
		state.LastUpdated.Format(time.RFC3339))

	for agentID, agentState := range state.Agents {
		fmt.Printf("Agent: %s\n", style.Bold.Render(agentID))

		if !agentState.LastPingTime.IsZero() {
			fmt.Printf("  Last ping: %s ago\n", time.Since(agentState.LastPingTime).Round(time.Second))
		}
		if !agentState.LastResponseTime.IsZero() {
			fmt.Printf("  Last response: %s ago\n", time.Since(agentState.LastResponseTime).Round(time.Second))
		}

		fmt.Printf("  Consecutive failures: %d\n", agentState.ConsecutiveFailures)
		fmt.Printf("  Total force-kills: %d\n", agentState.ForceKillCount)

		if !agentState.LastForceKillTime.IsZero() {
			fmt.Printf("  Last force-kill: %s ago\n", time.Since(agentState.LastForceKillTime).Round(time.Second))
			if agentState.IsInCooldown(healthCheckCooldown) {
				remaining := agentState.CooldownRemaining(healthCheckCooldown)
				fmt.Printf("  Cooldown: %s remaining\n", remaining.Round(time.Second))
			}
		}
		fmt.Println()
	}

	return nil
}

// agentAddressToIDs converts an agent address to bead ID and session name.
// Supports formats: "horde/raiders/max", "horde/witness", "shaman", "warchief"
// Note: Encampment-level agents (Warchief, Shaman) use hq- prefix bead IDs stored in encampment relics.
func agentAddressToIDs(address string) (beadID, sessionName string, err error) {
	switch address {
	case "shaman":
		return relics.ShamanBeadIDTown(), session.ShamanSessionName(), nil
	case "warchief":
		return relics.WarchiefBeadIDTown(), session.WarchiefSessionName(), nil
	}

	parts := strings.Split(address, "/")
	switch len(parts) {
	case 2:
		// warband/role: "horde/witness", "horde/forge"
		warband, role := parts[0], parts[1]
		switch role {
		case "witness":
			return fmt.Sprintf("hd-%s-witness", warband), fmt.Sprintf("hd-%s-witness", warband), nil
		case "forge":
			return fmt.Sprintf("hd-%s-forge", warband), fmt.Sprintf("hd-%s-forge", warband), nil
		default:
			return "", "", fmt.Errorf("unknown role: %s", role)
		}
	case 3:
		// warband/type/name: "horde/raiders/max", "horde/clan/alpha"
		warband, agentType, name := parts[0], parts[1], parts[2]
		switch agentType {
		case "raiders":
			return fmt.Sprintf("hd-%s-raider-%s", warband, name), fmt.Sprintf("hd-%s-%s", warband, name), nil
		case "clan":
			return fmt.Sprintf("hd-%s-clan-%s", warband, name), fmt.Sprintf("hd-%s-clan-%s", warband, name), nil
		default:
			return "", "", fmt.Errorf("unknown agent type: %s", agentType)
		}
	default:
		return "", "", fmt.Errorf("invalid agent address format: %s (expected warband/type/name or warband/role)", address)
	}
}

// getAgentBeadUpdateTime gets the update time from an agent bead.
func getAgentBeadUpdateTime(townRoot, beadID string) (time.Time, error) {
	cmd := exec.Command("rl", "show", beadID, "--json")
	cmd.Dir = townRoot

	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, err
	}

	var issues []struct {
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(output, &issues); err != nil {
		return time.Time{}, err
	}

	if len(issues) == 0 {
		return time.Time{}, fmt.Errorf("bead not found: %s", beadID)
	}

	return time.Parse(time.RFC3339, issues[0].UpdatedAt)
}

// sendMail sends a drums message using hd drums send.
func sendMail(townRoot, to, subject, body string) {
	cmd := exec.Command("hd", "drums", "send", to, "-s", subject, "-m", body)
	cmd.Dir = townRoot
	_ = cmd.Run() // Best effort
}

// updateAgentBeadState updates an agent bead's state.
func updateAgentBeadState(townRoot, agent, state, _ string) { // reason unused but kept for API consistency
	beadID, _, err := agentAddressToIDs(agent)
	if err != nil {
		return
	}

	// Use rl agent state command
	cmd := exec.Command("rl", "agent", "state", beadID, state)
	cmd.Dir = townRoot
	_ = cmd.Run() // Best effort
}

// runShamanStaleHooks finds and unhooks stale bannered relics.
func runShamanStaleHooks(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	cfg := &shaman.StaleHookConfig{
		MaxAge: staleHooksMaxAge,
		DryRun: staleHooksDryRun,
	}

	result, err := shaman.ScanStaleHooks(townRoot, cfg)
	if err != nil {
		return fmt.Errorf("scanning stale hooks: %w", err)
	}

	// Print summary
	if result.TotalHooked == 0 {
		fmt.Printf("%s No bannered relics found\n", style.Dim.Render("○"))
		return nil
	}

	fmt.Printf("%s Found %d bannered bead(s), %d stale (older than %s)\n",
		style.Bold.Render("●"), result.TotalHooked, result.StaleCount, staleHooksMaxAge)

	if result.StaleCount == 0 {
		fmt.Printf("%s No stale bannered relics\n", style.Dim.Render("○"))
		return nil
	}

	// Print details for each stale bead
	for _, r := range result.Results {
		status := style.Dim.Render("○")
		action := "skipped (agent alive)"

		if !r.AgentAlive {
			if staleHooksDryRun {
				status = style.Bold.Render("?")
				action = "would unhook (agent dead)"
			} else if r.Unhooked {
				status = style.Bold.Render("✓")
				action = "unhooked (agent dead)"
			} else if r.Error != "" {
				status = style.Dim.Render("✗")
				action = fmt.Sprintf("error: %s", r.Error)
			}
		}

		fmt.Printf("  %s %s: %s (age: %s, assignee: %s)\n",
			status, r.BeadID, action, r.Age, r.Assignee)
	}

	// Summary
	if staleHooksDryRun {
		fmt.Printf("\n%s Dry run - no changes made. Run without --dry-run to unhook.\n",
			style.Dim.Render("ℹ"))
	} else if result.Unhooked > 0 {
		fmt.Printf("\n%s Unhooked %d stale bead(s)\n",
			style.Bold.Render("✓"), result.Unhooked)
	}

	return nil
}

// runShamanPause pauses the Shaman to prevent scout actions.
func runShamanPause(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Check if already paused
	paused, state, err := shaman.IsPaused(townRoot)
	if err != nil {
		return fmt.Errorf("checking pause state: %w", err)
	}
	if paused {
		fmt.Printf("%s Shaman is already paused\n", style.Dim.Render("○"))
		fmt.Printf("  Reason: %s\n", state.Reason)
		fmt.Printf("  Paused at: %s\n", state.PausedAt.Format(time.RFC3339))
		fmt.Printf("  Paused by: %s\n", state.PausedBy)
		return nil
	}

	// Pause the Shaman
	if err := shaman.Pause(townRoot, pauseReason, "human"); err != nil {
		return fmt.Errorf("pausing Shaman: %w", err)
	}

	fmt.Printf("%s Shaman paused\n", style.Bold.Render("⏸️"))
	if pauseReason != "" {
		fmt.Printf("  Reason: %s\n", pauseReason)
	}
	fmt.Printf("  Pause file: %s\n", shaman.GetPauseFile(townRoot))
	fmt.Println()
	fmt.Printf("The Shaman will not perform any scout actions until resumed.\n")
	fmt.Printf("Resume with: %s\n", style.Dim.Render("hd shaman resume"))

	return nil
}

// runShamanResume resumes the Shaman to allow scout actions.
func runShamanResume(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Check if paused
	paused, _, err := shaman.IsPaused(townRoot)
	if err != nil {
		return fmt.Errorf("checking pause state: %w", err)
	}
	if !paused {
		fmt.Printf("%s Shaman is not paused\n", style.Dim.Render("○"))
		return nil
	}

	// Resume the Shaman
	if err := shaman.Resume(townRoot); err != nil {
		return fmt.Errorf("resuming Shaman: %w", err)
	}

	fmt.Printf("%s Shaman resumed\n", style.Bold.Render("▶️"))
	fmt.Println("The Shaman can now perform scout actions.")

	return nil
}
