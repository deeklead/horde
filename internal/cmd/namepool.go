package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/raider"
	"github.com/OWNER/horde/internal/workspace"
)

var (
	namepoolListFlag  bool
	namepoolThemeFlag string
)

var namepoolCmd = &cobra.Command{
	Use:     "namepool",
	GroupID: GroupWorkspace,
	Short:   "Manage raider name pools",
	Long: `Manage themed name pools for raiders in Horde.

By default, raiders get themed names from the  universe
(furiosa, nux, slit, etc.). You can change the theme or add custom names.

Examples:
  hd namepool              # Show current pool status
  hd namepool --list       # List available themes
  hd namepool themes       # Show theme names
  hd namepool set minerals # Set theme to 'minerals'
  hd namepool add ember    # Add custom name to pool
  hd namepool reset        # Reset pool state`,
	RunE: runNamepool,
}

var namepoolThemesCmd = &cobra.Command{
	Use:   "themes [theme]",
	Short: "List available themes and their names",
	RunE:  runNamepoolThemes,
}

var namepoolSetCmd = &cobra.Command{
	Use:   "set <theme>",
	Short: "Set the namepool theme for this warband",
	Args:  cobra.ExactArgs(1),
	RunE:  runNamepoolSet,
}

var namepoolAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a custom name to the pool",
	Args:  cobra.ExactArgs(1),
	RunE:  runNamepoolAdd,
}

var namepoolResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset the pool state (release all names)",
	RunE:  runNamepoolReset,
}

func init() {
	rootCmd.AddCommand(namepoolCmd)
	namepoolCmd.AddCommand(namepoolThemesCmd)
	namepoolCmd.AddCommand(namepoolSetCmd)
	namepoolCmd.AddCommand(namepoolAddCmd)
	namepoolCmd.AddCommand(namepoolResetCmd)
	namepoolCmd.Flags().BoolVarP(&namepoolListFlag, "list", "l", false, "List available themes")
}

func runNamepool(cmd *cobra.Command, args []string) error {
	// List themes mode
	if namepoolListFlag {
		return runNamepoolThemes(cmd, nil)
	}

	// Show current pool status
	rigName, rigPath := detectCurrentRigWithPath()
	if rigName == "" {
		return fmt.Errorf("not in a warband directory")
	}

	// Load pool
	pool := raider.NewNamePool(rigPath, rigName)
	if err := pool.Load(); err != nil {
		// Pool doesn't exist yet, show defaults
		fmt.Printf("Warband: %s\n", rigName)
		fmt.Printf("Theme: %s (default)\n", raider.DefaultTheme)
		fmt.Printf("Active raiders: 0\n")
		fmt.Printf("Max pool size: %d\n", raider.DefaultPoolSize)
		return nil
	}

	// Show pool status
	fmt.Printf("Warband: %s\n", rigName)
	fmt.Printf("Theme: %s\n", pool.GetTheme())
	fmt.Printf("Active raiders: %d\n", pool.ActiveCount())
	
	activeNames := pool.ActiveNames()
	if len(activeNames) > 0 {
		fmt.Printf("In use: %s\n", strings.Join(activeNames, ", "))
	}

	// Check if configured
	settingsPath := filepath.Join(rigPath, "settings", "config.json")
	if settings, err := config.LoadRigSettings(settingsPath); err == nil && settings.Namepool != nil {
		fmt.Printf("(configured in settings/config.json)\n")
	}

	return nil
}

func runNamepoolThemes(cmd *cobra.Command, args []string) error {
	themes := raider.ListThemes()

	if len(args) == 0 {
		// List all themes
		fmt.Println("Available themes:")
		for _, theme := range themes {
			names, _ := raider.GetThemeNames(theme)
			fmt.Printf("\n  %s (%d names):\n", theme, len(names))
			// Show first 10 names
			preview := names
			if len(preview) > 10 {
				preview = preview[:10]
			}
			fmt.Printf("    %s...\n", strings.Join(preview, ", "))
		}
		return nil
	}

	// Show specific theme names
	theme := args[0]
	names, err := raider.GetThemeNames(theme)
	if err != nil {
		return fmt.Errorf("unknown theme: %s (available: %s)", theme, strings.Join(themes, ", "))
	}

	fmt.Printf("Theme: %s (%d names)\n\n", theme, len(names))
	for i, name := range names {
		if i > 0 && i%5 == 0 {
			fmt.Println()
		}
		fmt.Printf("  %-12s", name)
	}
	fmt.Println()

	return nil
}

func runNamepoolSet(cmd *cobra.Command, args []string) error {
	theme := args[0]

	// Validate theme
	themes := raider.ListThemes()
	valid := false
	for _, t := range themes {
		if t == theme {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown theme: %s (available: %s)", theme, strings.Join(themes, ", "))
	}

	// Get warband
	rigName, rigPath := detectCurrentRigWithPath()
	if rigName == "" {
		return fmt.Errorf("not in a warband directory")
	}

	// Update pool
	pool := raider.NewNamePool(rigPath, rigName)
	if err := pool.Load(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading pool: %w", err)
	}
	
	if err := pool.SetTheme(theme); err != nil {
		return err
	}
	
	if err := pool.Save(); err != nil {
		return fmt.Errorf("saving pool: %w", err)
	}

	// Also save to warband config
	if err := saveRigNamepoolConfig(rigPath, theme, nil); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Theme '%s' set for warband '%s'\n", theme, rigName)
	fmt.Printf("New raiders will use names from this theme.\n")

	return nil
}

func runNamepoolAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	rigName, rigPath := detectCurrentRigWithPath()
	if rigName == "" {
		return fmt.Errorf("not in a warband directory")
	}

	// Load pool
	pool := raider.NewNamePool(rigPath, rigName)
	if err := pool.Load(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading pool: %w", err)
	}

	pool.AddCustomName(name)
	
	if err := pool.Save(); err != nil {
		return fmt.Errorf("saving pool: %w", err)
	}

	fmt.Printf("Added '%s' to the name pool\n", name)
	return nil
}

func runNamepoolReset(cmd *cobra.Command, args []string) error {
	rigName, rigPath := detectCurrentRigWithPath()
	if rigName == "" {
		return fmt.Errorf("not in a warband directory")
	}

	// Load pool
	pool := raider.NewNamePool(rigPath, rigName)
	if err := pool.Load(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading pool: %w", err)
	}

	pool.Reset()
	
	if err := pool.Save(); err != nil {
		return fmt.Errorf("saving pool: %w", err)
	}

	fmt.Printf("Pool reset for warband '%s'\n", rigName)
	fmt.Printf("All names released and available for reuse.\n")
	return nil
}

// detectCurrentRigWithPath determines the warband name and path from cwd.
func detectCurrentRigWithPath() (string, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", ""
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		return "", ""
	}

	// Get path relative to encampment root
	rel, err := filepath.Rel(townRoot, cwd)
	if err != nil {
		return "", ""
	}

	// Extract first path component (warband name)
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > 0 && parts[0] != "." && parts[0] != "warchief" && parts[0] != "shaman" {
		return parts[0], filepath.Join(townRoot, parts[0])
	}

	return "", ""
}

// saveRigNamepoolConfig saves the namepool config to warband settings.
func saveRigNamepoolConfig(rigPath, theme string, customNames []string) error {
	settingsPath := filepath.Join(rigPath, "settings", "config.json")

	// Load existing settings or create new
	var settings *config.RigSettings
	settings, err := config.LoadRigSettings(settingsPath)
	if err != nil {
		// Create new settings if not found
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") {
			settings = config.NewRigSettings()
		} else {
			return fmt.Errorf("loading settings: %w", err)
		}
	}

	// Set namepool
	settings.Namepool = &config.NamepoolConfig{
		Style: theme,
		Names: customNames,
	}

	// Save (creates directory if needed)
	if err := config.SaveRigSettings(settingsPath, settings); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}

	return nil
}
