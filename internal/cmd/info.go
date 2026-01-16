package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/version"
)

var infoCmd = &cobra.Command{
	Use:     "info",
	GroupID: GroupDiag,
	Short:   "Show Horde information and what's new",
	Long: `Display information about the current Horde installation.

This command shows:
  - Version information
  - What's new in recent versions (with --whats-new flag)

Examples:
  hd info
  hd info --whats-new
  hd info --whats-new --json`,
	Run: func(cmd *cobra.Command, args []string) {
		whatsNewFlag, _ := cmd.Flags().GetBool("whats-new")
		jsonFlag, _ := cmd.Flags().GetBool("json")

		if whatsNewFlag {
			showWhatsNew(jsonFlag)
			return
		}

		// Default: show basic info
		info := map[string]interface{}{
			"version": Version,
			"build":   Build,
		}

		if commit := resolveCommitHash(); commit != "" {
			info["commit"] = version.ShortCommit(commit)
		}
		if branch := resolveBranch(); branch != "" {
			info["branch"] = branch
		}

		if jsonFlag {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(info)
			return
		}

		fmt.Printf("Horde v%s (%s)\n", Version, Build)
		if commit, ok := info["commit"].(string); ok {
			if branch, ok := info["branch"].(string); ok {
				fmt.Printf("  %s@%s\n", branch, commit)
			} else {
				fmt.Printf("  %s\n", commit)
			}
		}
		fmt.Println("\nUse 'hd info --whats-new' to see recent changes")
	},
}

// VersionChange represents agent-relevant changes for a specific version
type VersionChange struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Changes []string `json:"changes"`
}

// versionChanges contains agent-actionable changes for recent versions
var versionChanges = []VersionChange{
	{
		Version: "0.2.0",
		Date:    "2026-01-04",
		Changes: []string{
			"NEW: Raid Warmap - Web UI for monitoring Horde (hd warmap)",
			"NEW: Two-level relics architecture - hq-* prefix for encampment, warband prefixes for projects",
			"NEW: Multi-agent support with pluggable registry",
			"NEW: hd warband start/stop/restart/status - Multi-warband management commands",
			"NEW: Ephemeral raider model - Immediate recycling after each work unit",
			"NEW: hd costs command - Session cost tracking and reporting",
			"NEW: Conflict resolution workflow for raiders with merge-slot gates",
			"NEW: hd raid --tree and hd raid check for cross-warband coordination",
			"NEW: Batch charging - hd charge supports multiple relics at once",
			"NEW: muster alias for start across all role subcommands",
			"NEW: hd drums archive supports multiple message IDs",
			"NEW: hd drums --all flag for clearing all drums",
			"NEW: Circuit breaker for stuck agents",
			"NEW: Binary age detection in hd status",
			"NEW: Shell completion installation instructions",
			"CHANGED: Handoff migrated to skills format",
			"CHANGED: Clan workers push directly to main (no PRs)",
			"CHANGED: Session names include encampment name",
			"FIX: Thread-safety for agent session resume",
			"FIX: Orphan daemon prevention via file locking",
			"FIX: Zombie tmux session cleanup",
			"FIX: Default branch detection (no longer hardcodes 'main')",
			"FIX: Enter key retry logic for reliable delivery",
			"FIX: Relics prefix routing for cross-warband operations",
		},
	},
	{
		Version: "0.1.1",
		Date:    "2026-01-02",
		Changes: []string{
			"FIX: Tmux keybindings scoped to Horde sessions only",
			"NEW: OSS project files - CHANGELOG.md, .golangci.yml, RELEASING.md",
			"NEW: Version bump script - scripts/bump-version.sh",
			"FIX: hd warband add and hd clan add CLI syntax documentation",
			"FIX: Warband prefix routing for agent relics",
			"FIX: Relics init targets correct database",
		},
	},
	{
		Version: "0.1.0",
		Date:    "2026-01-02",
		Changes: []string{
			"Initial public release of Horde",
			"NEW: Encampment structure - Hierarchical workspace with warbands, clans, and raiders",
			"NEW: Warband management - hd warband add/list/remove",
			"NEW: Clan workspaces - hd clan add for persistent developer workspaces",
			"NEW: Raider workers - Transient agent workers managed by Witness",
			"NEW: Warchief - Global coordinator for cross-warband work",
			"NEW: Shaman - Encampment-level lifecycle scout and heartbeat",
			"NEW: Witness - Per-warband raider lifecycle manager",
			"NEW: Forge - Merge queue processor with code review",
			"NEW: Raid system - hd raid create/list/status",
			"NEW: Charge workflow - hd charge <bead> <warband>",
			"NEW: Totem workflows - Ritual-based multi-step task execution",
			"NEW: Drums system - hd drums inbox/send/read",
			"NEW: Escalation protocol - hd escalate with severity levels",
			"NEW: Handoff mechanism - hd handoff for context-preserving session cycling",
			"NEW: Relics integration - Issue tracking via relics (bd commands)",
			"NEW: Tmux sessions with theming",
			"NEW: Status warmap - hd status",
			"NEW: Activity feed - hd feed",
			"NEW: Signal system - hd signal for reliable message delivery",
		},
	},
}

// showWhatsNew displays agent-relevant changes from recent versions
func showWhatsNew(jsonOutput bool) {
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]interface{}{
			"current_version": Version,
			"recent_changes":  versionChanges,
		})
		return
	}

	// Human-readable output
	fmt.Printf("\nWhat's New in Horde (Current: v%s)\n", Version)
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()

	for _, vc := range versionChanges {
		// Highlight if this is the current version
		versionMarker := ""
		if vc.Version == Version {
			versionMarker = " <- current"
		}

		fmt.Printf("## v%s (%s)%s\n\n", vc.Version, vc.Date, versionMarker)

		for _, change := range vc.Changes {
			fmt.Printf("  * %s\n", change)
		}
		fmt.Println()
	}

	fmt.Println("Tip: Use 'hd info --whats-new --json' for machine-readable output")
	fmt.Println()
}

func init() {
	infoCmd.Flags().Bool("whats-new", false, "Show agent-relevant changes from recent versions")
	infoCmd.Flags().Bool("json", false, "Output in JSON format")
	rootCmd.AddCommand(infoCmd)
}
