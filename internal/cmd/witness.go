package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/witness"
	"github.com/deeklead/horde/internal/workspace"
)

// Witness command flags
var (
	witnessForeground    bool
	witnessStatusJSON    bool
	witnessAgentOverride string
	witnessEnvOverrides  []string
)

var witnessCmd = &cobra.Command{
	Use:     "witness",
	GroupID: GroupAgents,
	Short:   "Manage the raider monitoring agent",
	RunE:    requireSubcommand,
	Long: `Manage the Witness monitoring agent for a warband.

The Witness monitors raiders for stuck states and orphaned sandboxes,
nudges raiders that seem blocked, and reports status to the warchief.

In the self-cleaning model, raiders nuke themselves after work completion.
The Witness handles edge cases: crashed sessions, orphaned worktrees, and
stuck raiders that need intervention.`,
}

var witnessStartCmd = &cobra.Command{
	Use:     "start <warband>",
	Aliases: []string{"muster"},
	Short:   "Start the witness",
	Long: `Start the Witness for a warband.

Launches the monitoring agent which watches for stuck raiders and orphaned
sandboxes, taking action to keep work flowing.

Self-Cleaning Model: Raiders nuke themselves after work. The Witness handles
crash recovery (restart with bannered work) and orphan cleanup (nuke abandoned
sandboxes). There is no "idle" state - raiders either have work or don't exist.

Examples:
  hd witness start greenplace
  hd witness start greenplace --agent codex
  hd witness start greenplace --env ANTHROPIC_MODEL=claude-3-haiku
  hd witness start greenplace --foreground`,
	Args: cobra.ExactArgs(1),
	RunE: runWitnessStart,
}

var witnessStopCmd = &cobra.Command{
	Use:   "stop <warband>",
	Short: "Stop the witness",
	Long: `Stop a running Witness.

Gracefully stops the witness monitoring agent.`,
	Args: cobra.ExactArgs(1),
	RunE: runWitnessStop,
}

var witnessStatusCmd = &cobra.Command{
	Use:   "status <warband>",
	Short: "Show witness status",
	Long: `Show the status of a warband's Witness.

Displays running state, monitored raiders, and statistics.`,
	Args: cobra.ExactArgs(1),
	RunE: runWitnessStatus,
}

var witnessAttachCmd = &cobra.Command{
	Use:     "summon [warband]",
	Aliases: []string{"at"},
	Short:   "Summon to witness session",
	Long: `Summon to the Witness tmux session for a warband.

Attaches the current terminal to the witness's tmux session.
Dismiss with Ctrl-B D.

If the witness is not running, this will start it first.
If warband is not specified, infers it from the current directory.

Examples:
  hd witness summon greenplace
  hd witness summon          # infer warband from cwd`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWitnessAttach,
}

var witnessRestartCmd = &cobra.Command{
	Use:   "restart <warband>",
	Short: "Restart the witness",
	Long: `Restart the Witness for a warband.

Stops the current session (if running) and starts a fresh one.

Examples:
  hd witness restart greenplace
  hd witness restart greenplace --agent codex
  hd witness restart greenplace --env ANTHROPIC_MODEL=claude-3-haiku`,
	Args: cobra.ExactArgs(1),
	RunE: runWitnessRestart,
}

func init() {
	// Start flags
	witnessStartCmd.Flags().BoolVar(&witnessForeground, "foreground", false, "Run in foreground (default: background)")
	witnessStartCmd.Flags().StringVar(&witnessAgentOverride, "agent", "", "Agent alias to run the Witness with (overrides encampment default)")
	witnessStartCmd.Flags().StringArrayVar(&witnessEnvOverrides, "env", nil, "Environment variable override (KEY=VALUE, can be repeated)")

	// Status flags
	witnessStatusCmd.Flags().BoolVar(&witnessStatusJSON, "json", false, "Output as JSON")

	// Restart flags
	witnessRestartCmd.Flags().StringVar(&witnessAgentOverride, "agent", "", "Agent alias to run the Witness with (overrides encampment default)")
	witnessRestartCmd.Flags().StringArrayVar(&witnessEnvOverrides, "env", nil, "Environment variable override (KEY=VALUE, can be repeated)")

	// Add subcommands
	witnessCmd.AddCommand(witnessStartCmd)
	witnessCmd.AddCommand(witnessStopCmd)
	witnessCmd.AddCommand(witnessRestartCmd)
	witnessCmd.AddCommand(witnessStatusCmd)
	witnessCmd.AddCommand(witnessAttachCmd)

	rootCmd.AddCommand(witnessCmd)
}

// getWitnessManager creates a witness manager for a warband.
func getWitnessManager(rigName string) (*witness.Manager, error) {
	_, r, err := getRig(rigName)
	if err != nil {
		return nil, err
	}

	mgr := witness.NewManager(r)
	return mgr, nil
}

func runWitnessStart(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	mgr, err := getWitnessManager(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Starting witness for %s...\n", rigName)

	if err := mgr.Start(witnessForeground, witnessAgentOverride, witnessEnvOverrides); err != nil {
		if err == witness.ErrAlreadyRunning {
			fmt.Printf("%s Witness is already running\n", style.Dim.Render("⚠"))
			fmt.Printf("  %s\n", style.Dim.Render("Use 'hd witness summon' to connect"))
			return nil
		}
		return fmt.Errorf("starting witness: %w", err)
	}

	if witnessForeground {
		fmt.Printf("%s Note: Foreground mode no longer runs scout loop\n", style.Dim.Render("⚠"))
		fmt.Printf("  %s\n", style.Dim.Render("Scout logic is now handled by totem-witness-scout totem"))
		return nil
	}

	fmt.Printf("%s Witness started for %s\n", style.Bold.Render("✓"), rigName)
	fmt.Printf("  %s\n", style.Dim.Render("Use 'hd witness summon' to connect"))
	fmt.Printf("  %s\n", style.Dim.Render("Use 'hd witness status' to check progress"))
	return nil
}

func runWitnessStop(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	mgr, err := getWitnessManager(rigName)
	if err != nil {
		return err
	}

	// Kill tmux session if it exists
	t := tmux.NewTmux()
	sessionName := witnessSessionName(rigName)
	running, _ := t.HasSession(sessionName)
	if running {
		if err := t.KillSession(sessionName); err != nil {
			style.PrintWarning("failed to kill session: %v", err)
		}
	}

	// Update state file
	if err := mgr.Stop(); err != nil {
		if err == witness.ErrNotRunning && !running {
			fmt.Printf("%s Witness is not running\n", style.Dim.Render("⚠"))
			return nil
		}
		// Even if manager.Stop fails, if we killed the session it's stopped
		if !running {
			return fmt.Errorf("stopping witness: %w", err)
		}
	}

	fmt.Printf("%s Witness stopped for %s\n", style.Bold.Render("✓"), rigName)
	return nil
}

func runWitnessStatus(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	mgr, err := getWitnessManager(rigName)
	if err != nil {
		return err
	}

	w, err := mgr.Status()
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	// Check actual tmux session state (more reliable than state file)
	t := tmux.NewTmux()
	sessionName := witnessSessionName(rigName)
	sessionRunning, _ := t.HasSession(sessionName)

	// Reconcile state: tmux session is the source of truth for background mode
	if sessionRunning && w.State != witness.StateRunning {
		w.State = witness.StateRunning
	} else if !sessionRunning && w.State == witness.StateRunning {
		w.State = witness.StateStopped
	}

	// JSON output
	if witnessStatusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(w)
	}

	// Human-readable output
	fmt.Printf("%s Witness: %s\n\n", style.Bold.Render(AgentTypeIcons[AgentWitness]), rigName)

	stateStr := string(w.State)
	switch w.State {
	case witness.StateRunning:
		stateStr = style.Bold.Render("● running")
	case witness.StateStopped:
		stateStr = style.Dim.Render("○ stopped")
	case witness.StatePaused:
		stateStr = style.Dim.Render("⏸ paused")
	}
	fmt.Printf("  State: %s\n", stateStr)
	if sessionRunning {
		fmt.Printf("  Session: %s\n", sessionName)
	}

	if w.StartedAt != nil {
		fmt.Printf("  Started: %s\n", w.StartedAt.Format("2006-01-02 15:04:05"))
	}

	// Show monitored raiders
	fmt.Printf("\n  %s\n", style.Bold.Render("Monitored Raiders:"))
	if len(w.MonitoredRaiders) == 0 {
		fmt.Printf("    %s\n", style.Dim.Render("(none)"))
	} else {
		for _, p := range w.MonitoredRaiders {
			fmt.Printf("    • %s\n", p)
		}
	}

	return nil
}

// witnessSessionName returns the tmux session name for a warband's witness.
func witnessSessionName(rigName string) string {
	return fmt.Sprintf("hd-%s-witness", rigName)
}

func runWitnessAttach(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	// Infer warband from cwd if not provided
	if rigName == "" {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return fmt.Errorf("not in a Horde workspace: %w", err)
		}
		rigName, err = inferRigFromCwd(townRoot)
		if err != nil {
			return fmt.Errorf("could not determine warband: %w\nUsage: hd witness summon <warband>", err)
		}
	}

	// Verify warband exists and get manager
	mgr, err := getWitnessManager(rigName)
	if err != nil {
		return err
	}

	sessionName := witnessSessionName(rigName)

	// Ensure session exists (creates if needed)
	if err := mgr.Start(false, "", nil); err != nil && err != witness.ErrAlreadyRunning {
		return err
	} else if err == nil {
		fmt.Printf("Started witness session for %s\n", rigName)
	}

	// Summon to the session
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}

	attachCmd := exec.Command(tmuxPath, "summon-session", "-t", sessionName)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr
	return attachCmd.Run()
}

func runWitnessRestart(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	mgr, err := getWitnessManager(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Restarting witness for %s...\n", rigName)

	// Stop existing session (non-fatal: may not be running)
	_ = mgr.Stop()

	// Start fresh
	if err := mgr.Start(false, witnessAgentOverride, witnessEnvOverrides); err != nil {
		return fmt.Errorf("starting witness: %w", err)
	}

	fmt.Printf("%s Witness restarted for %s\n", style.Bold.Render("✓"), rigName)
	fmt.Printf("  %s\n", style.Dim.Render("Use 'hd witness summon' to connect"))
	return nil
}
