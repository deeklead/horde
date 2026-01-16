package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

func init() {
	showCmd.GroupID = GroupWork
	rootCmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:   "show <bead-id> [flags]",
	Short: "Show details of a bead",
	Long: `Displays the full details of a bead by ID.

Delegates to 'bd show' - all rl show flags are supported.
Works with any bead prefix (gt-, bd-, hq-, etc.) and routes
to the correct relics database automatically.

Examples:
  hd show gt-abc123          # Show a horde issue
  hd show hq-xyz789          # Show a encampment-level bead (raid, drums, etc.)
  hd show bd-def456          # Show a relics issue
  hd show gt-abc123 --json   # Output as JSON
  hd show gt-abc123 -v       # Verbose output`,
	DisableFlagParsing: true, // Pass all flags through to rl show
	RunE:               runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("bead ID required\n\nUsage: hd show <bead-id> [flags]")
	}

	return execBdShow(args)
}

// execBdShow replaces the current process with 'bd show'.
func execBdShow(args []string) error {
	bdPath, err := exec.LookPath("rl")
	if err != nil {
		return fmt.Errorf("bd not found in PATH: %w", err)
	}

	// Build args: rl show <all-args>
	// argv[0] must be the program name for exec
	fullArgs := append([]string{"rl", "show"}, args...)

	// Replace process with rl show
	return syscall.Exec(bdPath, fullArgs, os.Environ())
}
