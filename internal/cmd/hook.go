package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/runtime"
	"github.com/deeklead/horde/internal/style"
)

var hookCmd = &cobra.Command{
	Use:     "hook [bead-id]",
	GroupID: GroupWork,
	Short:   "Show or summon work on your hook",
	Long: `Show what's on your hook, or summon new work.

With no arguments, shows your current hook status (alias for 'hd totem status').
With a bead ID, attaches that work to your hook.

The hook is the "durability primitive" - work on your hook survives session
restarts, context compaction, and handoffs. When you restart (via hd handoff),
your SessionStart hook finds the attached work and you continue from where
you left off.

Examples:
  hd hook                           # Show what's on my hook
  hd hook status                    # Same as above
  hd hook gt-abc                    # Summon issue gt-abc to your hook
  hd hook gt-abc -s "Fix the bug"   # With subject for handoff drums

Related commands:
  hd charge <bead>    # Hook + start now (keep context)
  hd handoff <bead>  # Hook + restart (fresh context)
  hd unsling         # Remove work from hook`,
	Args: cobra.MaximumNArgs(1),
	RunE: runHookOrStatus,
}

// hookStatusCmd shows hook status (alias for mol status)
var hookStatusCmd = &cobra.Command{
	Use:   "status [target]",
	Short: "Show what's on your hook",
	Long: `Show what's charged on your hook.

This is an alias for 'hd totem status'. Shows what work is currently
attached to your hook, along with progress information.

Examples:
  hd hook status                    # Show my hook
  hd hook status greenplace/nux     # Show nux's hook`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMoleculeStatus,
}

// hookShowCmd shows hook status in compact one-line format
var hookShowCmd = &cobra.Command{
	Use:   "show [agent]",
	Short: "Show what's on an agent's hook (compact)",
	Long: `Show what's on any agent's hook in compact one-line format.

With no argument, shows your own hook status (auto-detected from context).

Use cases:
- Warchief checking what raiders are working on
- Witness checking raider status
- Debugging coordination issues
- Quick status overview

Examples:
  hd hook show                         # What's on MY hook? (auto-detect)
  hd hook show horde/raiders/nux    # What's nux working on?
  hd hook show horde/witness         # What's the witness bannered to?
  hd hook show warchief                   # What's the warchief working on?

Output format (one line):
  horde/raiders/nux: gt-abc123 'Fix the widget bug' [in_progress]`,
	Args: cobra.MaximumNArgs(1),
	RunE: runHookShow,
}

var (
	hookSubject string
	hookMessage string
	hookDryRun  bool
	hookForce   bool
)

func init() {
	// Flags for attaching work (hd banner <bead-id>)
	hookCmd.Flags().StringVarP(&hookSubject, "subject", "s", "", "Subject for handoff drums (optional)")
	hookCmd.Flags().StringVarP(&hookMessage, "message", "m", "", "Message for handoff drums (optional)")
	hookCmd.Flags().BoolVarP(&hookDryRun, "dry-run", "n", false, "Show what would be done")
	hookCmd.Flags().BoolVarP(&hookForce, "force", "f", false, "Replace existing incomplete bannered bead")

	// --json flag for status output (used when no args, i.e., hd hook --json)
	hookCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON (for status)")
	hookStatusCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")
	hookShowCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")
	hookCmd.AddCommand(hookStatusCmd)
	hookCmd.AddCommand(hookShowCmd)

	rootCmd.AddCommand(hookCmd)
}

// runHookOrStatus dispatches to status or hook based on args
func runHookOrStatus(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		// No args - show status
		return runMoleculeStatus(cmd, args)
	}
	// Has arg - summon work
	return runHook(cmd, args)
}

func runHook(_ *cobra.Command, args []string) error {
	beadID := args[0]

	// Raiders cannot hook - they use hd done for lifecycle
	if raiderName := os.Getenv("GT_RAIDER"); raiderName != "" {
		return fmt.Errorf("raiders cannot hook work (use hd done for handoff)")
	}

	// Verify the bead exists
	if err := verifyBeadExists(beadID); err != nil {
		return err
	}

	// Determine agent identity
	agentID, _, _, err := resolveSelfTarget()
	if err != nil {
		return fmt.Errorf("detecting agent identity: %w", err)
	}

	// Find relics directory
	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Check for existing bannered bead for this agent
	existingPinned, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: agentID,
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("checking existing bannered relics: %w", err)
	}

	// If there's an existing bannered bead, check if we can auto-replace
	if len(existingPinned) > 0 {
		existing := existingPinned[0]

		// Skip if it's the same bead we're trying to pin
		if existing.ID == beadID {
			fmt.Printf("%s Already bannered: %s\n", style.Bold.Render("✓"), beadID)
			return nil
		}

		// Check if existing bead is complete
		isComplete, hasAttachment := checkPinnedBeadComplete(b, existing)

		if isComplete {
			// Auto-replace completed bead
			fmt.Printf("%s Replacing completed bead %s...\n", style.Dim.Render("ℹ"), existing.ID)
			if !hookDryRun {
				if hasAttachment {
					// Close completed totem bead (use rl close --force for pinned)
					closeArgs := []string{"close", existing.ID, "--force",
						"--reason=Auto-replaced by hd hook (totem complete)"}
					if sessionID := runtime.SessionIDFromEnv(); sessionID != "" {
						closeArgs = append(closeArgs, "--session="+sessionID)
					}
					closeCmd := exec.Command("rl", closeArgs...)
					closeCmd.Stderr = os.Stderr
					if err := closeCmd.Run(); err != nil {
						return fmt.Errorf("closing completed bead %s: %w", existing.ID, err)
					}
				} else {
					// Naked bead - just unpin, don't close (might have value)
					status := "open"
					if err := b.Update(existing.ID, relics.UpdateOptions{Status: &status}); err != nil {
						return fmt.Errorf("unpinning bead %s: %w", existing.ID, err)
					}
				}
			}
		} else if hookForce {
			// Force replace incomplete bead
			fmt.Printf("%s Force-replacing incomplete bead %s...\n", style.Dim.Render("⚠"), existing.ID)
			if !hookDryRun {
				// Unpin by setting status back to open
				status := "open"
				if err := b.Update(existing.ID, relics.UpdateOptions{Status: &status}); err != nil {
					return fmt.Errorf("unpinning bead %s: %w", existing.ID, err)
				}
			}
		} else {
			// Existing incomplete bead blocks new hook
			return fmt.Errorf("existing bannered bead %s is incomplete (%s)\n  Use --force to replace, or complete the existing work first",
				existing.ID, existing.Title)
		}
	}

	fmt.Printf("%s Hooking %s...\n", style.Bold.Render("🪝"), beadID)

	if hookDryRun {
		fmt.Printf("Would run: rl update %s --status=bannered --assignee=%s\n", beadID, agentID)
		if hookSubject != "" {
			fmt.Printf("  subject (for handoff drums): %s\n", hookSubject)
		}
		if hookMessage != "" {
			fmt.Printf("  context (for handoff drums): %s\n", hookMessage)
		}
		return nil
	}

	// Hook the bead using rl update (discovery-based approach)
	hookCmd := exec.Command("rl", "update", beadID, "--status=bannered", "--assignee="+agentID)
	hookCmd.Stderr = os.Stderr
	if err := hookCmd.Run(); err != nil {
		return fmt.Errorf("hooking bead: %w", err)
	}

	fmt.Printf("%s Work attached to hook (bannered bead)\n", style.Bold.Render("✓"))
	fmt.Printf("  Use 'hd handoff' to restart with this work\n")
	fmt.Printf("  Use 'hd banner' to see hook status\n")

	// Log hook event to activity feed (non-fatal)
	if err := events.LogFeed(events.TypeHook, agentID, events.HookPayload(beadID)); err != nil {
		fmt.Fprintf(os.Stderr, "%s Warning: failed to log hook event: %v\n", style.Dim.Render("⚠"), err)
	}

	return nil
}

// checkPinnedBeadComplete checks if a pinned bead's attached totem is 100% complete.
// Returns (isComplete, hasAttachment):
// - isComplete=true if no totem attached OR all totem steps are closed
// - hasAttachment=true if there's an attached totem
func checkPinnedBeadComplete(b *relics.Relics, issue *relics.Issue) (isComplete bool, hasAttachment bool) {
	// Check for attached totem
	attachment := relics.ParseAttachmentFields(issue)
	if attachment == nil || attachment.AttachedMolecule == "" {
		// No totem attached - consider complete (naked bead)
		return true, false
	}

	// Get progress of attached totem
	progress, err := getMoleculeProgressInfo(b, attachment.AttachedMolecule)
	if err != nil {
		// Can't determine progress - be conservative, treat as incomplete
		return false, true
	}

	if progress == nil {
		// No steps found - might be a simple issue, treat as complete
		return true, true
	}

	return progress.Complete, true
}

// runHookShow displays another agent's hook in compact one-line format.
func runHookShow(cmd *cobra.Command, args []string) error {
	var target string
	if len(args) > 0 {
		target = args[0]
	} else {
		// Auto-detect current agent from context
		agentID, _, _, err := resolveSelfTarget()
		if err != nil {
			return fmt.Errorf("auto-detecting agent (use explicit argument): %w", err)
		}
		target = agentID
	}

	// Find relics directory
	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Query for bannered relics assigned to the target
	hookedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: target,
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("listing bannered relics: %w", err)
	}

	// If nothing found, try scanning all warbands for encampment-level roles
	if len(hookedRelics) == 0 && isTownLevelRole(target) {
		townRoot, err := findTownRoot()
		if err == nil && townRoot != "" {
			hookedRelics = scanAllRigsForHookedRelics(townRoot, target)
		}
	}

	// JSON output
	if moleculeJSON {
		type compactInfo struct {
			Agent  string `json:"agent"`
			BeadID string `json:"bead_id,omitempty"`
			Title  string `json:"title,omitempty"`
			Status string `json:"status"`
		}
		info := compactInfo{Agent: target}
		if len(hookedRelics) > 0 {
			info.BeadID = hookedRelics[0].ID
			info.Title = hookedRelics[0].Title
			info.Status = hookedRelics[0].Status
		} else {
			info.Status = "empty"
		}
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(info)
	}

	// Compact one-line output
	if len(hookedRelics) == 0 {
		fmt.Printf("%s: (empty)\n", target)
		return nil
	}

	bead := hookedRelics[0]
	fmt.Printf("%s: %s '%s' [%s]\n", target, bead.ID, bead.Title, bead.Status)
	return nil
}

// findTownRoot finds the Horde root directory.
func findTownRoot() (string, error) {
	cmd := exec.Command("hd", "root")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
