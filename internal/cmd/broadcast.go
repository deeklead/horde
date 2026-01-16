package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/tmux"
)

var (
	broadcastRig    string
	broadcastAll    bool
	broadcastDryRun bool
)

func init() {
	broadcastCmd.Flags().StringVar(&broadcastRig, "warband", "", "Only broadcast to workers in this warband")
	broadcastCmd.Flags().BoolVar(&broadcastAll, "all", false, "Include all agents (warchief, witness, etc.), not just workers")
	broadcastCmd.Flags().BoolVar(&broadcastDryRun, "dry-run", false, "Show what would be sent without sending")
	rootCmd.AddCommand(broadcastCmd)
}

var broadcastCmd = &cobra.Command{
	Use:     "broadcast <message>",
	GroupID: GroupComm,
	Short:   "Send a signal message to all workers",
	Long: `Broadcasts a message to all active workers (raiders and clan).

By default, only workers (raiders and clan) receive the message.
Use --all to include infrastructure agents (warchief, shaman, witness, forge).

The message is sent as a signal to each worker's Claude Code session.

Examples:
  hd broadcast "Check your drums"
  hd broadcast --warband greenplace "New priority work available"
  hd broadcast --all "System maintenance in 5 minutes"
  hd broadcast --dry-run "Test message"`,
	Args: cobra.ExactArgs(1),
	RunE: runBroadcast,
}

func runBroadcast(cmd *cobra.Command, args []string) error {
	message := args[0]

	if message == "" {
		return fmt.Errorf("message cannot be empty")
	}

	// Get all agent sessions (including raiders)
	agents, err := getAgentSessions(true)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	// Filter to target agents
	var targets []*AgentSession
	for _, agent := range agents {
		// Filter by warband if specified
		if broadcastRig != "" && agent.Warband != broadcastRig {
			continue
		}

		// Unless --all, only include workers (clan + raiders)
		if !broadcastAll {
			if agent.Type != AgentCrew && agent.Type != AgentRaider {
				continue
			}
		}

		targets = append(targets, agent)
	}

	if len(targets) == 0 {
		fmt.Println("No workers running to broadcast to.")
		if broadcastRig != "" {
			fmt.Printf("  (filtered by warband: %s)\n", broadcastRig)
		}
		return nil
	}

	// Dry run - just show what would be sent
	if broadcastDryRun {
		fmt.Printf("Would broadcast to %d agent(s):\n\n", len(targets))
		for _, agent := range targets {
			fmt.Printf("  %s %s\n", AgentTypeIcons[agent.Type], formatAgentName(agent))
		}
		fmt.Printf("\nMessage: %s\n", message)
		return nil
	}

	// Send nudges
	t := tmux.NewTmux()
	var succeeded, failed int
	var failures []string

	fmt.Printf("Broadcasting to %d agent(s)...\n\n", len(targets))

	for i, agent := range targets {
		agentName := formatAgentName(agent)

		if err := t.SignalSession(agent.Name, message); err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: %v", agentName, err))
			fmt.Printf("  %s %s %s\n", style.ErrorPrefix, AgentTypeIcons[agent.Type], agentName)
		} else {
			succeeded++
			fmt.Printf("  %s %s %s\n", style.SuccessPrefix, AgentTypeIcons[agent.Type], agentName)
		}

		// Small delay between nudges to avoid overwhelming tmux
		if i < len(targets)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("%s Broadcast complete: %d succeeded, %d failed\n",
			style.WarningPrefix, succeeded, failed)
		for _, f := range failures {
			fmt.Printf("  %s\n", style.Dim.Render(f))
		}
		return fmt.Errorf("%d signal(s) failed", failed)
	}

	fmt.Printf("%s Broadcast complete: %d agent(s) nudged\n", style.SuccessPrefix, succeeded)
	return nil
}

// formatAgentName returns a display name for an agent.
func formatAgentName(agent *AgentSession) string {
	switch agent.Type {
	case AgentWarchief:
		return "warchief"
	case AgentShaman:
		return "shaman"
	case AgentWitness:
		return fmt.Sprintf("%s/witness", agent.Warband)
	case AgentForge:
		return fmt.Sprintf("%s/forge", agent.Warband)
	case AgentCrew:
		return fmt.Sprintf("%s/clan/%s", agent.Warband, agent.AgentName)
	case AgentRaider:
		return fmt.Sprintf("%s/%s", agent.Warband, agent.AgentName)
	}
	return agent.Name
}
