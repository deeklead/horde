// ABOUTME: Command to enable Horde system-wide.
// ABOUTME: Sets the global state to enabled for all agentic coding tools.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/state"
	"github.com/deeklead/horde/internal/style"
)

var enableCmd = &cobra.Command{
	Use:     "enable",
	GroupID: GroupConfig,
	Short:   "Enable Horde system-wide",
	Long: `Enable Horde for all agentic coding tools.

When enabled:
  - Shell hooks set HD_ENCAMPMENT_ROOT and HD_WARBAND environment variables
  - Claude Code SessionStart hooks run 'hd rally' for context
  - Git repos are auto-registered as warbands (configurable)

Use 'hd disable' to turn off. Use 'hd status --global' to check state.

Environment overrides:
  HORDE_DISABLED=1  - Disable for current session only
  HORDE_ENABLED=1   - Enable for current session only`,
	RunE: runEnable,
}

func init() {
	rootCmd.AddCommand(enableCmd)
}

func runEnable(cmd *cobra.Command, args []string) error {
	if err := state.Enable(Version); err != nil {
		return fmt.Errorf("enabling Horde: %w", err)
	}

	fmt.Printf("%s Horde enabled\n", style.Success.Render("✓"))
	fmt.Println()
	fmt.Println("Horde will now:")
	fmt.Println("  • Inject context into Claude Code sessions")
	fmt.Println("  • Set HD_ENCAMPMENT_ROOT and HD_WARBAND environment variables")
	fmt.Println("  • Auto-register git repos as warbands (if configured)")
	fmt.Println()
	fmt.Printf("Use %s to disable, %s to check status\n",
		style.Dim.Render("hd disable"),
		style.Dim.Render("hd status --global"))

	return nil
}
