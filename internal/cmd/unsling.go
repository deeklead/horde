package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

var unslingCmd = &cobra.Command{
	Use:     "unsling [bead-id] [target]",
	Aliases: []string{"unhook"},
	GroupID: GroupWork,
	Short:   "Remove work from an agent's hook",
	Long: `Remove work from an agent's hook (the inverse of charge/hook).

With no arguments, clears your own hook. With a bead ID, only unslings
if that specific bead is currently bannered. With a target, operates on
another agent's hook.

Examples:
  hd unsling                        # Clear my hook (whatever's there)
  hd unsling gt-abc                 # Only unsling if gt-abc is bannered
  hd unsling greenplace/joe            # Clear joe's hook
  hd unsling gt-abc greenplace/joe     # Unsling gt-abc from joe

The bead's status changes from 'bannered' back to 'open'.

Related commands:
  hd charge <bead>    # Hook + start (inverse of unsling)
  hd hook <bead>     # Hook without starting
  hd hook      # See what's on your hook`,
	Args: cobra.MaximumNArgs(2),
	RunE: runUnsling,
}

var (
	unslingDryRun bool
	unslingForce  bool
)

func init() {
	unslingCmd.Flags().BoolVarP(&unslingDryRun, "dry-run", "n", false, "Show what would be done")
	unslingCmd.Flags().BoolVarP(&unslingForce, "force", "f", false, "Unsling even if work is incomplete")
	rootCmd.AddCommand(unslingCmd)
}

func runUnsling(cmd *cobra.Command, args []string) error {
	var targetBeadID string
	var targetAgent string

	// Parse args: [bead-id] [target]
	switch len(args) {
	case 0:
		// No args - unsling self, whatever is bannered
	case 1:
		// Could be bead ID or target agent
		// If it contains "/" or is a known role, treat as target
		if isAgentTarget(args[0]) {
			targetAgent = args[0]
		} else {
			targetBeadID = args[0]
		}
	case 2:
		targetBeadID = args[0]
		targetAgent = args[1]
	}

	// Resolve target agent (default: self)
	var agentID string
	var err error
	if targetAgent != "" {
		agentID, _, _, err = resolveTargetAgent(targetAgent)
		if err != nil {
			return fmt.Errorf("resolving target agent: %w", err)
		}
	} else {
		agentID, _, _, err = resolveSelfTarget()
		if err != nil {
			return fmt.Errorf("detecting agent identity: %w", err)
		}
	}

	// Find encampment root and warband path for agent relics
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding encampment root: %w", err)
	}

	// Extract warband name from agent ID (e.g., "horde/clan/joe" -> "horde")
	// For encampment-level agents like "warchief/", use encampment root
	rigName := strings.Split(agentID, "/")[0]
	var relicsPath string
	if rigName == "warchief" || rigName == "shaman" {
		relicsPath = townRoot
	} else {
		relicsPath = filepath.Join(townRoot, rigName)
	}

	b := relics.New(relicsPath)

	// Convert agent ID to agent bead ID and look up the agent bead
	agentBeadID := agentIDToBeadID(agentID, townRoot)
	if agentBeadID == "" {
		return fmt.Errorf("could not convert agent ID %s to bead ID", agentID)
	}

	// Get the agent bead to find current hook
	agentBead, err := b.Show(agentBeadID)
	if err != nil {
		return fmt.Errorf("getting agent bead %s: %w", agentBeadID, err)
	}

	// Check if agent has work bannered (via banner_bead field)
	hookedBeadID := agentBead.BannerBead
	if hookedBeadID == "" {
		if targetAgent != "" {
			fmt.Printf("%s No work bannered for %s\n", style.Dim.Render("ℹ"), agentID)
		} else {
			fmt.Printf("%s Nothing on your hook\n", style.Dim.Render("ℹ"))
		}
		return nil
	}

	// If specific bead requested, verify it matches
	if targetBeadID != "" && hookedBeadID != targetBeadID {
		return fmt.Errorf("bead %s is not bannered (current hook: %s)", targetBeadID, hookedBeadID)
	}

	// Get the bannered bead to check completion and show title
	hookedBead, err := b.Show(hookedBeadID)
	if err != nil {
		// Bead might be deleted - still allow unsling with --force
		if !unslingForce {
			return fmt.Errorf("getting bannered bead %s: %w\n  Use --force to unsling anyway", hookedBeadID, err)
		}
		// Force mode - proceed without the bead details
		hookedBead = &relics.Issue{ID: hookedBeadID, Title: "(unknown)"}
	}

	// Check if work is complete (warn if not, unless --force)
	isComplete := hookedBead.Status == "closed"
	if !isComplete && !unslingForce {
		return fmt.Errorf("bannered work %s is incomplete (%s)\n  Use --force to unsling anyway",
			hookedBeadID, hookedBead.Title)
	}

	if targetAgent != "" {
		fmt.Printf("%s Unslinging %s from %s...\n", style.Bold.Render("🪝"), hookedBeadID, agentID)
	} else {
		fmt.Printf("%s Unslinging %s...\n", style.Bold.Render("🪝"), hookedBeadID)
	}

	if unslingDryRun {
		fmt.Printf("Would clear banner_bead from agent bead %s\n", agentBeadID)
		return nil
	}

	// Clear the hook (gt-zecmc: removed agent_state update - observable from tmux)
	if err := b.ClearBannerBead(agentBeadID); err != nil {
		return fmt.Errorf("clearing hook from agent bead %s: %w", agentBeadID, err)
	}

	// Log unhook event
	_ = events.LogFeed(events.TypeUnhook, agentID, events.UnhookPayload(hookedBeadID))

	fmt.Printf("%s Work removed from hook\n", style.Bold.Render("✓"))
	fmt.Printf("  Agent %s hook cleared (was: %s)\n", agentID, hookedBeadID)

	return nil
}

// isAgentTarget checks if a string looks like an agent target rather than a bead ID.
// Agent targets contain "/" or are known role names.
func isAgentTarget(s string) bool {
	// Contains "/" means it's a path like "greenplace/joe"
	for _, c := range s {
		if c == '/' {
			return true
		}
	}

	// Known role names
	switch s {
	case "warchief", "shaman", "witness", "forge", "clan":
		return true
	}

	return false
}
