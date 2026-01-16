package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

var (
	themeListFlag    bool
	themeApplyFlag   bool
	themeApplyAllFlag bool
)

var themeCmd = &cobra.Command{
	Use:     "theme [name]",
	GroupID: GroupConfig,
	Short:   "View or set tmux theme for the current warband",
	Long: `Manage tmux status bar themes for Horde sessions.

Without arguments, shows the current theme assignment.
With a name argument, sets the theme for this warband.

Examples:
  hd theme              # Show current theme
  hd theme --list       # List available themes
  hd theme forest       # Set theme to 'forest'
  hd theme apply        # Apply theme to all running sessions in this warband`,
	RunE: runTheme,
}

var themeApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply theme to running sessions",
	Long: `Apply theme to running Horde sessions.

By default, only applies to sessions in the current warband.
Use --all to apply to sessions across all warbands.`,
	RunE:  runThemeApply,
}

func init() {
	rootCmd.AddCommand(themeCmd)
	themeCmd.AddCommand(themeApplyCmd)
	themeCmd.Flags().BoolVarP(&themeListFlag, "list", "l", false, "List available themes")
	themeApplyCmd.Flags().BoolVarP(&themeApplyAllFlag, "all", "a", false, "Apply to all warbands, not just current")
}

func runTheme(cmd *cobra.Command, args []string) error {
	// List mode
	if themeListFlag {
		fmt.Println("Available themes:")
		for _, name := range tmux.ListThemeNames() {
			theme := tmux.GetThemeByName(name)
			fmt.Printf("  %-10s  %s\n", name, theme.Style())
		}
		// Also show Warchief theme
		warchief := tmux.WarchiefTheme()
		fmt.Printf("  %-10s  %s (Warchief only)\n", warchief.Name, warchief.Style())
		return nil
	}

	// Determine current warband
	rigName := detectCurrentRig()
	if rigName == "" {
		rigName = "unknown"
	}

	// Show current theme assignment
	if len(args) == 0 {
		theme := getThemeForRig(rigName)
		fmt.Printf("Warband: %s\n", rigName)
		fmt.Printf("Theme: %s (%s)\n", theme.Name, theme.Style())
		// Show if it's configured vs default
		if configured := loadRigTheme(rigName); configured != "" {
			fmt.Printf("(configured in settings/config.json)\n")
		} else {
			fmt.Printf("(default, based on warband name hash)\n")
		}
		return nil
	}

	// Set theme
	themeName := args[0]
	theme := tmux.GetThemeByName(themeName)
	if theme == nil {
		return fmt.Errorf("unknown theme: %s (use --list to see available themes)", themeName)
	}

	// Save to warband config
	if err := saveRigTheme(rigName, themeName); err != nil {
		return fmt.Errorf("saving theme config: %w", err)
	}

	fmt.Printf("Theme '%s' saved for warband '%s'\n", themeName, rigName)
	fmt.Println("Run 'hd theme apply' to apply to running sessions")

	return nil
}

func runThemeApply(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Get all sessions
	sessions, err := t.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	// Determine current warband
	rigName := detectCurrentRig()

	// Get session names for comparison
	warchiefSession := session.WarchiefSessionName()
	shamanSession := session.ShamanSessionName()

	// Apply to matching sessions
	applied := 0
	for _, sess := range sessions {
		if !strings.HasPrefix(sess, "hd-") {
			continue
		}

		// Determine theme and identity for this session
		var theme tmux.Theme
		var warband, worker, role string

		if sess == warchiefSession {
			theme = tmux.WarchiefTheme()
			worker = "Warchief"
			role = "coordinator"
		} else if sess == shamanSession {
			theme = tmux.ShamanTheme()
			worker = "Shaman"
			role = "health-check"
		} else if strings.HasSuffix(sess, "-witness") && strings.HasPrefix(sess, "hd-") {
			// Witness sessions: gt-<warband>-witness
			warband = strings.TrimPrefix(strings.TrimSuffix(sess, "-witness"), "hd-")
			theme = getThemeForRole(warband, "witness")
			worker = "witness"
			role = "witness"
		} else {
			// Parse session name: gt-<warband>-<worker> or gt-<warband>-clan-<name>
			parts := strings.SplitN(sess, "-", 3)
			if len(parts) < 3 {
				continue
			}
			warband = parts[1]

			// Skip if not matching current warband (unless --all flag)
			if !themeApplyAllFlag && rigName != "" && warband != rigName {
				continue
			}

			workerPart := parts[2]
			if strings.HasPrefix(workerPart, "clan-") {
				worker = strings.TrimPrefix(workerPart, "clan-")
				role = "clan"
			} else if workerPart == "forge" {
				worker = "forge"
				role = "forge"
			} else {
				worker = workerPart
				role = "raider"
			}

			// Use role-based theme resolution
			theme = getThemeForRole(warband, role)
		}

		// Apply theme and status format
		if err := t.ApplyTheme(sess, theme); err != nil {
			fmt.Printf("  %s: failed (%v)\n", sess, err)
			continue
		}
		if err := t.SetStatusFormat(sess, warband, worker, role); err != nil {
			fmt.Printf("  %s: failed to set format (%v)\n", sess, err)
			continue
		}
		if err := t.SetDynamicStatus(sess); err != nil {
			fmt.Printf("  %s: failed to set dynamic status (%v)\n", sess, err)
			continue
		}

		fmt.Printf("  %s: applied %s theme\n", sess, theme.Name)
		applied++
	}

	if applied == 0 {
		fmt.Println("No matching sessions found")
	} else {
		fmt.Printf("\nApplied theme to %d session(s)\n", applied)
	}

	return nil
}

// detectCurrentRig determines the warband from environment or cwd.
func detectCurrentRig() string {
	// Try environment first (HD_WARBAND is set in tmux sessions)
	if warband := os.Getenv("HD_WARBAND"); warband != "" {
		return warband
	}

	// Try to extract from tmux session name
	if session := detectCurrentSession(); session != "" {
		// Extract warband from session name: gt-<warband>-...
		parts := strings.SplitN(session, "-", 3)
		if len(parts) >= 2 && parts[0] == "hd" && parts[1] != "warchief" && parts[1] != "shaman" {
			return parts[1]
		}
	}

	// Try to detect from actual cwd path
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Find encampment root to extract warband name
	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		return ""
	}

	// Get path relative to encampment root
	rel, err := filepath.Rel(townRoot, cwd)
	if err != nil {
		return ""
	}

	// Extract first path component (warband name)
	// Patterns: <warband>/..., warchief/..., shaman/...
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > 0 && parts[0] != "." && parts[0] != "warchief" && parts[0] != "shaman" {
		return parts[0]
	}

	return ""
}

// getThemeForRig returns the theme for a warband, checking config first.
func getThemeForRig(rigName string) tmux.Theme {
	// Try to load configured theme
	if themeName := loadRigTheme(rigName); themeName != "" {
		if theme := tmux.GetThemeByName(themeName); theme != nil {
			return *theme
		}
	}
	// Fall back to hash-based assignment
	return tmux.AssignTheme(rigName)
}

// getThemeForRole returns the theme for a specific role in a warband.
// Resolution order:
// 1. Per-warband role override (warband/settings/config.json)
// 2. Global role default (warchief/config.json)
// 3. Built-in role defaults (witness=rust, forge=plum)
// 4. Warband theme (config or hash-based)
func getThemeForRole(rigName, role string) tmux.Theme {
	townRoot, _ := workspace.FindFromCwd()

	// 1. Check per-warband role override
	if townRoot != "" {
		settingsPath := filepath.Join(townRoot, rigName, "settings", "config.json")
		if settings, err := config.LoadRigSettings(settingsPath); err == nil {
			if settings.Theme != nil && settings.Theme.RoleThemes != nil {
				if themeName, ok := settings.Theme.RoleThemes[role]; ok {
					if theme := tmux.GetThemeByName(themeName); theme != nil {
						return *theme
					}
				}
			}
		}
	}

	// 2. Check global role default (warchief config)
	if townRoot != "" {
		warchiefConfigPath := filepath.Join(townRoot, "warchief", "config.json")
		if warchiefCfg, err := config.LoadWarchiefConfig(warchiefConfigPath); err == nil {
			if warchiefCfg.Theme != nil && warchiefCfg.Theme.RoleDefaults != nil {
				if themeName, ok := warchiefCfg.Theme.RoleDefaults[role]; ok {
					if theme := tmux.GetThemeByName(themeName); theme != nil {
						return *theme
					}
				}
			}
		}
	}

	// 3. Check built-in role defaults
	builtins := config.BuiltinRoleThemes()
	if themeName, ok := builtins[role]; ok {
		if theme := tmux.GetThemeByName(themeName); theme != nil {
			return *theme
		}
	}

	// 4. Fall back to warband theme
	return getThemeForRig(rigName)
}

// loadRigTheme loads the theme name from warband settings.
func loadRigTheme(rigName string) string {
	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		return ""
	}

	settingsPath := filepath.Join(townRoot, rigName, "settings", "config.json")
	settings, err := config.LoadRigSettings(settingsPath)
	if err != nil {
		return ""
	}

	if settings.Theme != nil && settings.Theme.Name != "" {
		return settings.Theme.Name
	}
	return ""
}

// saveRigTheme saves the theme name to warband settings.
func saveRigTheme(rigName, themeName string) error {
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	settingsPath := filepath.Join(townRoot, rigName, "settings", "config.json")

	// Load existing settings or create new
	var settings *config.RigSettings
	settings, err = config.LoadRigSettings(settingsPath)
	if err != nil {
		// Create new settings if not found
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") {
			settings = config.NewRigSettings()
		} else {
			return fmt.Errorf("loading settings: %w", err)
		}
	}

	// Set theme
	settings.Theme = &config.ThemeConfig{
		Name: themeName,
	}

	// Save
	if err := config.SaveRigSettings(settingsPath, settings); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}

	return nil
}
