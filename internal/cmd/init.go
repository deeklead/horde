package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/style"
)

var initForce bool

var initCmd = &cobra.Command{
	Use:     "init",
	GroupID: GroupWorkspace,
	Short:   "Initialize current directory as a Horde warband",
	Long: `Initialize the current directory for use as a Horde warband.

This creates the standard agent directories (raiders/, witness/, forge/,
warchief/) and updates .git/info/exclude to ignore them.

The current directory must be a git repository. Use --force to reinitialize
an existing warband structure.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "Reinitialize existing structure")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Check if it's a git repository
	g := git.NewGit(cwd)
	if _, err := g.CurrentBranch(); err != nil {
		return fmt.Errorf("not a git repository (run 'git init' first)")
	}

	// Check if already initialized
	raidersDir := filepath.Join(cwd, "raiders")
	if _, err := os.Stat(raidersDir); err == nil && !initForce {
		return fmt.Errorf("warband already initialized (use --force to reinitialize)")
	}

	fmt.Printf("%s Initializing Horde warband in %s\n\n",
		style.Bold.Render("⚙️"), style.Dim.Render(cwd))

	// Create agent directories
	created := 0
	for _, dir := range warband.AgentDirs {
		dirPath := filepath.Join(cwd, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}

		// Create .gitkeep to ensure directory is tracked if needed (non-fatal)
		gitkeep := filepath.Join(dirPath, ".gitkeep")
		if _, err := os.Stat(gitkeep); os.IsNotExist(err) {
			_ = os.WriteFile(gitkeep, []byte(""), 0644)
		}

		fmt.Printf("   ✓ Created %s/\n", dir)
		created++
	}

	// Update .git/info/exclude
	if err := updateGitExclude(cwd); err != nil {
		fmt.Printf("   %s Could not update .git/info/exclude: %v\n",
			style.Dim.Render("⚠"), err)
	} else {
		fmt.Printf("   ✓ Updated .git/info/exclude\n")
	}

	// Register custom relics types for Horde (agent, role, warband, raid, slot).
	// This is best-effort: if relics isn't installed or DB doesn't exist, we skip.
	// The doctor check will catch missing types later.
	if err := registerCustomTypes(cwd); err != nil {
		fmt.Printf("   %s Could not register custom types: %v\n",
			style.Dim.Render("⚠"), err)
	} else {
		fmt.Printf("   ✓ Registered custom relics types\n")
	}

	fmt.Printf("\n%s Warband initialized with %d directories.\n",
		style.Bold.Render("✓"), created)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Add this warband to a encampment: %s\n",
		style.Dim.Render("hd warband add <name> <git-url>"))
	fmt.Printf("  2. Create a raider: %s\n",
		style.Dim.Render("hd raider add <name>"))

	return nil
}

func updateGitExclude(repoPath string) error {
	excludePath := filepath.Join(repoPath, ".git", "info", "exclude")

	// Ensure directory exists
	excludeDir := filepath.Dir(excludePath)
	if err := os.MkdirAll(excludeDir, 0755); err != nil {
		return fmt.Errorf("creating .git/info: %w", err)
	}

	// Read existing content
	content, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Check if already has Horde section
	if strings.Contains(string(content), "Horde") {
		return nil // Already configured
	}

	// Append agent dirs
	additions := "\n# Horde agent directories\n"
	for _, dir := range warband.AgentDirs {
		// Get first component (e.g., "raiders" from "raiders")
		// or "forge" from "forge/warband"
		base := filepath.Dir(dir)
		if base == "." {
			base = dir
		}
		additions += base + "/\n"
	}

	// Write back
	return os.WriteFile(excludePath, append(content, []byte(additions)...), 0644)
}

// registerCustomTypes registers Horde custom issue types with relics.
// This is best-effort: returns nil if relics isn't available or DB doesn't exist.
// Handles gracefully: relics not installed, no .relics directory, or config errors.
func registerCustomTypes(workDir string) error {
	// Check if rl command is available
	if _, err := exec.LookPath("rl"); err != nil {
		return nil // relics not installed, skip silently
	}

	// Check if .relics directory exists
	relicsDir := filepath.Join(workDir, ".relics")
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		return nil // no relics DB yet, skip silently
	}

	// Try to set custom types
	cmd := exec.Command("rl", "config", "set", "types.custom", constants.RelicsCustomTypes)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check for common expected errors
		outStr := string(output)
		if strings.Contains(outStr, "not initialized") ||
			strings.Contains(outStr, "no such file") {
			return nil // DB not initialized, skip silently
		}
		return fmt.Errorf("%s", strings.TrimSpace(outStr))
	}
	return nil
}
