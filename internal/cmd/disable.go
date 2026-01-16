// ABOUTME: Command to disable Horde system-wide.
// ABOUTME: Sets the global state to disabled so tools work vanilla.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/shell"
	"github.com/deeklead/horde/internal/state"
	"github.com/deeklead/horde/internal/style"
)

var disableClean bool

var disableCmd = &cobra.Command{
	Use:     "disable",
	GroupID: GroupConfig,
	Short:   "Disable Horde system-wide",
	Long: `Disable Horde for all agentic coding tools.

When disabled:
  - Shell hooks become no-ops
  - Claude Code SessionStart hooks skip 'hd rally'
  - Tools work 100% vanilla (no Horde behavior)

The workspace (~/horde) is preserved. Use 'hd enable' to re-enable.

Flags:
  --clean  Also remove shell integration from ~/.zshrc/~/.bashrc

Environment overrides still work:
  HORDE_ENABLED=1   - Enable for current session only`,
	RunE: runDisable,
}

func init() {
	disableCmd.Flags().BoolVar(&disableClean, "clean", false,
		"Remove shell integration from RC files")
	rootCmd.AddCommand(disableCmd)
}

func runDisable(cmd *cobra.Command, args []string) error {
	if err := state.Disable(); err != nil {
		return fmt.Errorf("disabling Horde: %w", err)
	}

	if disableClean {
		if err := removeShellIntegration(); err != nil {
			fmt.Printf("%s Could not clean shell integration: %v\n",
				style.Warning.Render("!"), err)
		} else {
			fmt.Println("  Removed shell integration from RC files")
		}
	}

	fmt.Printf("%s Horde disabled\n", style.Success.Render("✓"))
	fmt.Println()
	fmt.Println("All agentic coding tools now work vanilla.")
	if !disableClean {
		fmt.Printf("Use %s to also remove shell hooks\n",
			style.Dim.Render("hd disable --clean"))
	}
	fmt.Printf("Use %s to re-enable\n", style.Dim.Render("hd enable"))

	return nil
}

func removeShellIntegration() error {
	return shell.Remove()
}
