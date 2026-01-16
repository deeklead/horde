package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

var whoamiCmd = &cobra.Command{
	Use:     "whoami",
	GroupID: GroupDiag,
	Short:   "Show current identity for drums commands",
	Long: `Show the identity that will be used for drums commands.

Identity is determined by:
1. GT_ROLE env var (if set) - indicates an agent session
2. No GT_ROLE - you are the overseer (human)

Use --identity flag with drums commands to override.

Examples:
  hd whoami                      # Show current identity
  hd drums inbox                  # Check inbox for current identity
  hd drums inbox --identity warchief/  # Check Warchief's inbox instead`,
	RunE: runWhoami,
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}

func runWhoami(cmd *cobra.Command, args []string) error {
	// Get current identity using same logic as drums commands
	identity := detectSender()

	fmt.Printf("%s %s\n", style.Bold.Render("Identity:"), identity)

	// Show how it was determined
	gtRole := os.Getenv("GT_ROLE")
	if gtRole != "" {
		fmt.Printf("%s GT_ROLE=%s\n", style.Dim.Render("Source:"), gtRole)

		// Show additional env vars if present
		if warband := os.Getenv("GT_RIG"); warband != "" {
			fmt.Printf("%s GT_RIG=%s\n", style.Dim.Render("       "), warband)
		}
		if raider := os.Getenv("GT_RAIDER"); raider != "" {
			fmt.Printf("%s GT_RAIDER=%s\n", style.Dim.Render("       "), raider)
		}
		if clan := os.Getenv("GT_CREW"); clan != "" {
			fmt.Printf("%s GT_CREW=%s\n", style.Dim.Render("       "), clan)
		}
	} else {
		fmt.Printf("%s no GT_ROLE set (human at terminal)\n", style.Dim.Render("Source:"))

		// If overseer, show their configured identity
		if identity == "overseer" {
			townRoot, err := workspace.FindFromCwd()
			if err == nil && townRoot != "" {
				if overseerConfig, err := config.LoadOverseerConfig(config.OverseerConfigPath(townRoot)); err == nil {
					fmt.Printf("\n%s\n", style.Bold.Render("Overseer Identity:"))
					fmt.Printf("  Name:  %s\n", overseerConfig.Name)
					if overseerConfig.Email != "" {
						fmt.Printf("  Email: %s\n", overseerConfig.Email)
					}
					if overseerConfig.Username != "" {
						fmt.Printf("  User:  %s\n", overseerConfig.Username)
					}
					fmt.Printf("  %s %s\n", style.Dim.Render("(detected via"), style.Dim.Render(overseerConfig.Source+")"))
				}
			}
		}
	}

	return nil
}
