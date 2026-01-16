// ABOUTME: Command to completely uninstall Horde from the system.
// ABOUTME: Removes shell integration, wrappers, state, and optionally workspace.

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/shell"
	"github.com/deeklead/horde/internal/state"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/wrappers"
)

var (
	uninstallWorkspace bool
	uninstallForce     bool
)

var uninstallCmd = &cobra.Command{
	Use:     "uninstall",
	GroupID: GroupConfig,
	Short:   "Remove Horde from the system",
	Long: `Completely remove Horde from the system.

By default, removes:
  - Shell integration (~/.zshrc or ~/.bashrc)
  - Wrapper scripts (~/bin/hd-codex, ~/bin/hd-opencode)
  - State directory (~/.local/state/horde/)
  - Config directory (~/.config/horde/)
  - Cache directory (~/.cache/horde/)

The workspace (e.g., ~/horde) is NOT removed unless --workspace is specified.

Use --force to skip confirmation prompts.

Examples:
  hd uninstall                    # Remove Horde, keep workspace
  hd uninstall --workspace        # Also remove workspace directory
  hd uninstall --force            # Skip confirmation`,
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallWorkspace, "workspace", false,
		"Also remove the workspace directory (DESTRUCTIVE)")
	uninstallCmd.Flags().BoolVarP(&uninstallForce, "force", "f", false,
		"Skip confirmation prompts")
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	if !uninstallForce {
		fmt.Println("This will remove Horde from your system.")
		fmt.Println()
		fmt.Println("The following will be removed:")
		fmt.Printf("  • Shell integration (%s)\n", shell.RCFilePath(shell.DetectShell()))
		fmt.Printf("  • Wrapper scripts (%s)\n", wrappers.BinDir())
		fmt.Printf("  • State directory (%s)\n", state.StateDir())
		fmt.Printf("  • Config directory (%s)\n", state.ConfigDir())
		fmt.Printf("  • Cache directory (%s)\n", state.CacheDir())

		if uninstallWorkspace {
			fmt.Println()
			fmt.Printf("  %s WORKSPACE WILL BE DELETED\n", style.Warning.Render("⚠"))
			fmt.Println("     This cannot be undone!")
		}

		fmt.Println()
		fmt.Print("Continue? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var errors []string

	fmt.Println()
	fmt.Println("Removing Horde...")

	if err := shell.Remove(); err != nil {
		errors = append(errors, fmt.Sprintf("shell integration: %v", err))
	} else {
		fmt.Printf("  %s Removed shell integration\n", style.Success.Render("✓"))
	}

	if err := wrappers.Remove(); err != nil {
		errors = append(errors, fmt.Sprintf("wrapper scripts: %v", err))
	} else {
		fmt.Printf("  %s Removed wrapper scripts\n", style.Success.Render("✓"))
	}

	if err := os.RemoveAll(state.StateDir()); err != nil && !os.IsNotExist(err) {
		errors = append(errors, fmt.Sprintf("state directory: %v", err))
	} else {
		fmt.Printf("  %s Removed state directory\n", style.Success.Render("✓"))
	}

	if err := os.RemoveAll(state.ConfigDir()); err != nil && !os.IsNotExist(err) {
		errors = append(errors, fmt.Sprintf("config directory: %v", err))
	} else {
		fmt.Printf("  %s Removed config directory\n", style.Success.Render("✓"))
	}

	if err := os.RemoveAll(state.CacheDir()); err != nil && !os.IsNotExist(err) {
		errors = append(errors, fmt.Sprintf("cache directory: %v", err))
	} else {
		fmt.Printf("  %s Removed cache directory\n", style.Success.Render("✓"))
	}

	if uninstallWorkspace {
		workspaceDir := findWorkspaceForUninstall()
		if workspaceDir != "" {
			if err := os.RemoveAll(workspaceDir); err != nil {
				errors = append(errors, fmt.Sprintf("workspace: %v", err))
			} else {
				fmt.Printf("  %s Removed workspace: %s\n", style.Success.Render("✓"), workspaceDir)
			}
		}
	}

	if len(errors) > 0 {
		fmt.Println()
		fmt.Printf("%s Some components could not be removed:\n", style.Warning.Render("⚠"))
		for _, e := range errors {
			fmt.Printf("  • %s\n", e)
		}
		return fmt.Errorf("uninstall incomplete")
	}

	fmt.Println()
	fmt.Printf("%s Horde has been uninstalled\n", style.Success.Render("✓"))
	fmt.Println()
	fmt.Println("To reinstall, run:")
	fmt.Printf("  %s\n", style.Dim.Render("go install github.com/deeklead/horde/cmd/hd@latest"))
	fmt.Printf("  %s\n", style.Dim.Render("hd install ~/horde --shell"))

	return nil
}

func findWorkspaceForUninstall() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	candidates := []string{
		filepath.Join(home, "hd"),
		filepath.Join(home, "horde"),
	}

	for _, path := range candidates {
		warchiefDir := filepath.Join(path, "warchief")
		if _, err := os.Stat(warchiefDir); err == nil {
			return path
		}
	}

	return ""
}
