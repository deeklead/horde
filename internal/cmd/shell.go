// ABOUTME: Shell integration management commands.
// ABOUTME: Install/remove shell hooks without full HQ setup.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/shell"
	"github.com/deeklead/horde/internal/state"
	"github.com/deeklead/horde/internal/style"
)

var shellCmd = &cobra.Command{
	Use:     "shell",
	GroupID: GroupConfig,
	Short:   "Manage shell integration",
	RunE:    requireSubcommand,
}

var shellInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install or update shell integration",
	Long: `Install or update the Horde shell integration.

This adds a hook to your shell RC file that:
  - Sets HD_ENCAMPMENT_ROOT and HD_WARBAND when you cd into a Horde warband
  - Offers to add new git repos to Horde on first visit

Run this after upgrading hd to get the latest shell hook features.`,
	RunE: runShellInstall,
}

var shellRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove shell integration",
	RunE:  runShellRemove,
}

var shellStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show shell integration status",
	RunE:  runShellStatus,
}

func init() {
	shellCmd.AddCommand(shellInstallCmd)
	shellCmd.AddCommand(shellRemoveCmd)
	shellCmd.AddCommand(shellStatusCmd)
	rootCmd.AddCommand(shellCmd)
}

func runShellInstall(cmd *cobra.Command, args []string) error {
	if err := shell.Install(); err != nil {
		return err
	}

	if err := state.Enable(Version); err != nil {
		fmt.Printf("%s Could not enable Horde: %v\n", style.Dim.Render("⚠"), err)
	}

	fmt.Printf("%s Shell integration installed (%s)\n", style.Success.Render("✓"), shell.RCFilePath(shell.DetectShell()))
	fmt.Println()
	fmt.Println("Run 'source ~/.zshrc' or open a new terminal to activate.")
	return nil
}

func runShellRemove(cmd *cobra.Command, args []string) error {
	if err := shell.Remove(); err != nil {
		return err
	}

	fmt.Printf("%s Shell integration removed\n", style.Success.Render("✓"))
	return nil
}

func runShellStatus(cmd *cobra.Command, args []string) error {
	s, err := state.Load()
	if err != nil {
		fmt.Println("Horde: not configured")
		fmt.Println("Shell integration: not installed")
		return nil
	}

	if s.Enabled {
		fmt.Println("Horde: enabled")
	} else {
		fmt.Println("Horde: disabled")
	}

	if s.ShellIntegration != "" {
		fmt.Printf("Shell integration: %s (%s)\n", s.ShellIntegration, shell.RCFilePath(s.ShellIntegration))
	} else {
		fmt.Println("Shell integration: not installed")
	}

	return nil
}
