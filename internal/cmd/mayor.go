package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/warchief"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

var warchiefCmd = &cobra.Command{
	Use:     "warchief",
	Aliases: []string{"may"},
	GroupID: GroupAgents,
	Short:   "Manage the Warchief session",
	RunE:    requireSubcommand,
	Long: `Manage the Warchief tmux session.

The Warchief is the global coordinator for Horde, running as a persistent
tmux session. Use the subcommands to start, stop, summon, and check status.`,
}

var warchiefAgentOverride string

var warchiefStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Warchief session",
	Long: `Start the Warchief tmux session.

Creates a new detached tmux session for the Warchief and launches Claude.
The session runs in the workspace root directory.`,
	RunE: runWarchiefStart,
}

var warchiefStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Warchief session",
	Long: `Stop the Warchief tmux session.

Attempts graceful shutdown first (Ctrl-C), then kills the tmux session.`,
	RunE: runWarchiefStop,
}

var warchiefAttachCmd = &cobra.Command{
	Use:     "summon",
	Aliases: []string{"at"},
	Short:   "Summon to the Warchief session",
	Long: `Summon to the running Warchief tmux session.

Attaches the current terminal to the Warchief's tmux session.
Dismiss with Ctrl-B D.`,
	RunE: runWarchiefAttach,
}

var warchiefStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check Warchief session status",
	Long:  `Check if the Warchief tmux session is currently running.`,
	RunE:  runWarchiefStatus,
}

var warchiefRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Warchief session",
	Long: `Restart the Warchief tmux session.

Stops the current session (if running) and starts a fresh one.`,
	RunE: runWarchiefRestart,
}

func init() {
	warchiefCmd.AddCommand(warchiefStartCmd)
	warchiefCmd.AddCommand(warchiefStopCmd)
	warchiefCmd.AddCommand(warchiefAttachCmd)
	warchiefCmd.AddCommand(warchiefStatusCmd)
	warchiefCmd.AddCommand(warchiefRestartCmd)

	warchiefStartCmd.Flags().StringVar(&warchiefAgentOverride, "agent", "", "Agent alias to run the Warchief with (overrides encampment default)")
	warchiefAttachCmd.Flags().StringVar(&warchiefAgentOverride, "agent", "", "Agent alias to run the Warchief with (overrides encampment default)")
	warchiefRestartCmd.Flags().StringVar(&warchiefAgentOverride, "agent", "", "Agent alias to run the Warchief with (overrides encampment default)")

	rootCmd.AddCommand(warchiefCmd)
}

// getWarchiefManager returns a warchief manager for the current workspace.
func getWarchiefManager() (*warchief.Manager, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return nil, fmt.Errorf("not in a Horde workspace: %w", err)
	}
	return warchief.NewManager(townRoot), nil
}

// getWarchiefSessionName returns the Warchief session name.
func getWarchiefSessionName() string {
	return warchief.SessionName()
}

func runWarchiefStart(cmd *cobra.Command, args []string) error {
	mgr, err := getWarchiefManager()
	if err != nil {
		return err
	}

	fmt.Println("Starting Warchief session...")
	if err := mgr.Start(warchiefAgentOverride); err != nil {
		if err == warchief.ErrAlreadyRunning {
			return fmt.Errorf("Warchief session already running. Summon with: hd warchief summon")
		}
		return err
	}

	fmt.Printf("%s Warchief session started. Summon with: %s\n",
		style.Bold.Render("✓"),
		style.Dim.Render("hd warchief summon"))

	return nil
}

func runWarchiefStop(cmd *cobra.Command, args []string) error {
	mgr, err := getWarchiefManager()
	if err != nil {
		return err
	}

	fmt.Println("Stopping Warchief session...")
	if err := mgr.Stop(); err != nil {
		if err == warchief.ErrNotRunning {
			return fmt.Errorf("Warchief session is not running")
		}
		return err
	}

	fmt.Printf("%s Warchief session stopped.\n", style.Bold.Render("✓"))
	return nil
}

func runWarchiefAttach(cmd *cobra.Command, args []string) error {
	mgr, err := getWarchiefManager()
	if err != nil {
		return err
	}

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}

	t := tmux.NewTmux()
	sessionID := mgr.SessionName()

	running, err := mgr.IsRunning()
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		// Auto-start if not running
		fmt.Println("Warchief session not running, starting...")
		if err := mgr.Start(warchiefAgentOverride); err != nil {
			return err
		}
	} else {
		// Session exists - check if runtime is still running (hq-95xfq)
		// If runtime exited or sitting at shell, restart with proper context
		agentCfg, _, err := config.ResolveAgentConfigWithOverride(townRoot, townRoot, warchiefAgentOverride)
		if err != nil {
			return fmt.Errorf("resolving agent: %w", err)
		}
		if !t.IsAgentRunning(sessionID, config.ExpectedPaneCommands(agentCfg)...) {
			// Runtime has exited, restart it with proper context
			fmt.Println("Runtime exited, restarting with context...")

			paneID, err := t.GetPaneID(sessionID)
			if err != nil {
				return fmt.Errorf("getting pane ID: %w", err)
			}

			// Build startup beacon for context (like hd handoff does)
			beacon := session.FormatStartupNudge(session.StartupNudgeConfig{
				Recipient: "warchief",
				Sender:    "human",
				Topic:     "summon",
			})

			// Build startup command with beacon
			startupCmd, err := config.BuildAgentStartupCommandWithAgentOverride("warchief", "", townRoot, "", beacon, warchiefAgentOverride)
			if err != nil {
				return fmt.Errorf("building startup command: %w", err)
			}

			if err := t.RespawnPane(paneID, startupCmd); err != nil {
				return fmt.Errorf("restarting runtime: %w", err)
			}

			fmt.Printf("%s Warchief restarted with context\n", style.Bold.Render("✓"))
		}
	}

	// Use shared summon helper (smart: links if inside tmux, attaches if outside)
	return attachToTmuxSession(sessionID)
}

func runWarchiefStatus(cmd *cobra.Command, args []string) error {
	mgr, err := getWarchiefManager()
	if err != nil {
		return err
	}

	info, err := mgr.Status()
	if err != nil {
		if err == warchief.ErrNotRunning {
			fmt.Printf("%s Warchief session is %s\n",
				style.Dim.Render("○"),
				"not running")
			fmt.Printf("\nStart with: %s\n", style.Dim.Render("hd warchief start"))
			return nil
		}
		return fmt.Errorf("checking status: %w", err)
	}

	status := "detached"
	if info.Attached {
		status = "attached"
	}
	fmt.Printf("%s Warchief session is %s\n",
		style.Bold.Render("●"),
		style.Bold.Render("running"))
	fmt.Printf("  Status: %s\n", status)
	fmt.Printf("  Created: %s\n", info.Created)
	fmt.Printf("\nAttach with: %s\n", style.Dim.Render("hd warchief summon"))

	return nil
}

func runWarchiefRestart(cmd *cobra.Command, args []string) error {
	mgr, err := getWarchiefManager()
	if err != nil {
		return err
	}

	// Stop if running (ignore not-running error)
	if err := mgr.Stop(); err != nil && err != warchief.ErrNotRunning {
		return fmt.Errorf("stopping session: %w", err)
	}

	// Start fresh
	return runWarchiefStart(cmd, args)
}
