package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/OWNER/horde/internal/session"
	"github.com/spf13/cobra"
)

// Peek command flags
var peekLines int

func init() {
	rootCmd.AddCommand(peekCmd)
	peekCmd.Flags().IntVarP(&peekLines, "lines", "n", 100, "Number of lines to capture")
}

var peekCmd = &cobra.Command{
	Use:     "peek <warband/raider> [count]",
	GroupID: GroupComm,
	Short:   "View recent output from a raider or clan session",
	Long: `Capture and display recent terminal output from an agent session.

This is the ergonomic alias for 'hd session capture'. Use it to check
what an agent is currently doing or has recently output.

The signal/peek pair provides the canonical interface for agent sessions:
  hd signal - send messages TO a session (reliable delivery)
  hd peek  - read output FROM a session (capture-pane wrapper)

Supports both raiders and clan workers:
  - Raiders: warband/name format (e.g., greenplace/furiosa)
  - Clan: warband/clan/name format (e.g., relics/clan/dave)

Examples:
  hd peek greenplace/furiosa         # Raider: last 100 lines (default)
  hd peek greenplace/furiosa 50      # Raider: last 50 lines
  hd peek relics/clan/dave            # Clan: last 100 lines
  hd peek relics/clan/dave -n 200     # Clan: last 200 lines`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPeek,
}

func runPeek(cmd *cobra.Command, args []string) error {
	address := args[0]

	// Handle optional positional count argument
	lines := peekLines
	if len(args) > 1 {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid line count: %s", args[1])
		}
		lines = n
	}

	rigName, raiderName, err := parseAddress(address)
	if err != nil {
		return err
	}

	mgr, _, err := getSessionManager(rigName)
	if err != nil {
		return err
	}

	var output string

	// Handle clan/ prefix for cross-warband clan workers
	// e.g., "relics/clan/dave" -> session name "gt-relics-clan-dave"
	if strings.HasPrefix(raiderName, "clan/") {
		crewName := strings.TrimPrefix(raiderName, "clan/")
		sessionID := session.CrewSessionName(rigName, crewName)
		output, err = mgr.CaptureSession(sessionID, lines)
	} else {
		output, err = mgr.Capture(raiderName, lines)
	}

	if err != nil {
		return fmt.Errorf("capturing output: %w", err)
	}

	fmt.Print(output)
	return nil
}
