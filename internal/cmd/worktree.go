package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// Worktree command flags
var (
	worktreeNoCD bool
)

var worktreeCmd = &cobra.Command{
	Use:     "worktree <warband>",
	GroupID: GroupWorkspace,
	Short:   "Create worktree in another warband for cross-warband work",
	Long: `Create a git worktree in another warband for cross-warband work.

This command is for clan workers who need to work on another warband's codebase
while maintaining their identity. It creates a worktree in the target warband's
clan/ directory with a name that identifies your source warband and identity.

The worktree is created at: ~/horde/<target-warband>/clan/<source-warband>-<name>/

For example, if you're horde/clan/joe and run 'hd worktree relics':
- Creates worktree at ~/horde/relics/clan/horde-joe/
- The worktree checks out main branch
- Your identity (BD_ACTOR, HD_ROLE) remains horde/clan/joe

Use --no-cd to just print the path without printing shell commands.

Examples:
  hd worktree relics         # Create worktree in relics warband
  hd worktree horde       # Create worktree in horde warband (from another warband)
  hd worktree relics --no-cd # Just print the path`,
	Args: cobra.ExactArgs(1),
	RunE: runWorktree,
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all cross-warband worktrees owned by current clan member",
	Long: `List all git worktrees created for cross-warband work.

This command scans all warbands in the workspace and finds worktrees
that belong to the current clan member. Each worktree is shown with
its git status summary.

Example output:
  Cross-warband worktrees for horde/clan/joe:

    relics     ~/horde/relics/clan/horde-joe/     (clean)
    warchief     ~/horde/warchief/clan/horde-joe/     (2 uncommitted)`,
	RunE: runWorktreeList,
}

// Worktree remove command flags
var (
	worktreeRemoveForce bool
)

var worktreeRemoveCmd = &cobra.Command{
	Use:   "remove <warband>",
	Short: "Remove a cross-warband worktree",
	Long: `Remove a git worktree created for cross-warband work.

This command removes a worktree that was previously created with 'hd worktree <warband>'.
It will refuse to remove a worktree with uncommitted changes unless --force is used.

Examples:
  hd worktree remove relics         # Remove relics worktree
  hd worktree remove relics --force # Force remove even with uncommitted changes`,
	Args: cobra.ExactArgs(1),
	RunE: runWorktreeRemove,
}

func init() {
	worktreeCmd.Flags().BoolVar(&worktreeNoCD, "no-cd", false, "Just print path (don't print cd command)")
	worktreeCmd.AddCommand(worktreeListCmd)

	worktreeRemoveCmd.Flags().BoolVarP(&worktreeRemoveForce, "force", "f", false, "Force remove even with uncommitted changes")
	worktreeCmd.AddCommand(worktreeRemoveCmd)

	rootCmd.AddCommand(worktreeCmd)
}

func runWorktree(cmd *cobra.Command, args []string) error {
	targetRig := args[0]

	// Detect current clan identity from cwd
	detected, err := detectCrewFromCwd()
	if err != nil {
		return fmt.Errorf("must be in a clan workspace to use this command: %w", err)
	}

	sourceRig := detected.rigName
	crewName := detected.crewName

	// Cannot create worktree in your own warband
	if targetRig == sourceRig {
		return fmt.Errorf("already in warband '%s' - use hd worktree to work in a different warband", targetRig)
	}

	// Verify target warband exists
	_, targetRigInfo, err := getRig(targetRig)
	if err != nil {
		return fmt.Errorf("warband '%s' not found - run 'hd warband list' to see available warbands", targetRig)
	}

	// Compute worktree path: ~/horde/<target-warband>/clan/<source-warband>-<name>/
	worktreeName := fmt.Sprintf("%s-%s", sourceRig, crewName)
	worktreePath := filepath.Join(constants.RigCrewPath(targetRigInfo.Path), worktreeName)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		// Worktree exists
		if worktreeNoCD {
			fmt.Println(worktreePath)
		} else {
			fmt.Printf("%s Worktree already exists at %s\n", style.Success.Render("✓"), worktreePath)
			fmt.Printf("cd %s\n", worktreePath)
		}
		return nil
	}

	// Get the source warband's git repository (the bare repo for worktrees)
	// For cross-warband work, we need to use the target warband's repository
	// The target warband's warchief/warband is the main clone we create worktrees from
	targetWarchiefRig := constants.RigWarchiefPath(targetRigInfo.Path)
	g := git.NewGit(targetWarchiefRig)

	// Ensure clan directory exists in target warband
	crewDir := constants.RigCrewPath(targetRigInfo.Path)
	if err := os.MkdirAll(crewDir, 0755); err != nil {
		return fmt.Errorf("creating clan directory: %w", err)
	}

	// Fetch latest from remote before creating worktree
	if err := g.Fetch("origin"); err != nil {
		// Non-fatal - continue with local state
		fmt.Printf("%s Warning: could not fetch from origin: %v\n", style.Warning.Render("⚠"), err)
	}

	// Create the worktree on main branch
	// Use WorktreeAddExistingForce because main may already be checked out
	// in other worktrees (e.g., warchief/warband). This is safe for cross-warband work.
	if err := g.WorktreeAddExistingForce(worktreePath, "main"); err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}

	// Configure git author for identity preservation
	worktreeGit := git.NewGit(worktreePath)
	bdActor := fmt.Sprintf("%s/clan/%s", sourceRig, crewName)

	// Set local git config for this worktree
	if err := setGitConfig(worktreePath, "user.name", bdActor); err != nil {
		fmt.Printf("%s Warning: could not set git author name: %v\n", style.Warning.Render("⚠"), err)
	}

	fmt.Printf("%s Created worktree for cross-warband work\n", style.Success.Render("✓"))
	fmt.Printf("  Source: %s/clan/%s\n", sourceRig, crewName)
	fmt.Printf("  Target: %s\n", worktreePath)
	fmt.Printf("  Branch: main\n")
	fmt.Println()

	// Pull latest main in the new worktree
	if err := worktreeGit.Pull("origin", "main"); err != nil {
		fmt.Printf("%s Warning: could not pull latest: %v\n", style.Warning.Render("⚠"), err)
	}

	if worktreeNoCD {
		fmt.Println(worktreePath)
	} else {
		fmt.Printf("To enter the worktree:\n")
		fmt.Printf("  cd %s\n", worktreePath)
		fmt.Println()
		fmt.Printf("Environment variables to preserve your identity:\n")
		fmt.Printf("  export BD_ACTOR=%s\n", bdActor)
		fmt.Printf("  export HD_ROLE=clan\n")
		fmt.Printf("  export HD_WARBAND=%s\n", sourceRig)
		fmt.Printf("  export HD_CLAN=%s\n", crewName)
	}

	return nil
}

// setGitConfig sets a git config value in the specified worktree.
func setGitConfig(worktreePath, key, value string) error {
	cmd := exec.Command("git", "-C", worktreePath, "config", key, value)
	return cmd.Run()
}

func runWorktreeList(cmd *cobra.Command, args []string) error {
	// Detect current clan identity from cwd
	detected, err := detectCrewFromCwd()
	if err != nil {
		return fmt.Errorf("must be in a clan workspace to use this command: %w", err)
	}

	sourceRig := detected.rigName
	crewName := detected.crewName
	worktreeName := fmt.Sprintf("%s-%s", sourceRig, crewName)

	// Find encampment root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config to list all warbands
	rigsConfigPath := constants.WarchiefRigsPath(townRoot)
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		return fmt.Errorf("loading warbands config: %w", err)
	}

	fmt.Printf("Cross-warband worktrees for %s/clan/%s:\n\n", sourceRig, crewName)

	found := false
	for rigName := range rigsConfig.Warbands {
		// Skip our own warband - worktrees are for cross-warband work
		if rigName == sourceRig {
			continue
		}

		// Warband path is simply townRoot/<rigName>
		rigPath := filepath.Join(townRoot, rigName)

		// Check if worktree exists: <warband>/clan/<source-warband>-<name>/
		worktreePath := filepath.Join(constants.RigCrewPath(rigPath), worktreeName)

		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			continue
		}

		// Worktree exists - get git status
		statusSummary := getGitStatusSummary(worktreePath)

		// Format the path for display (use ~ for home directory)
		displayPath := worktreePath
		if home, err := os.UserHomeDir(); err == nil {
			if rel, err := filepath.Rel(home, worktreePath); err == nil && !filepath.IsAbs(rel) {
				displayPath = "~/" + rel
			}
		}

		fmt.Printf("  %-10s %s     (%s)\n", rigName, displayPath, statusSummary)
		found = true
	}

	if !found {
		fmt.Printf("  (none)\n")
		fmt.Printf("\nCreate a worktree with: hd worktree <warband>\n")
	}

	return nil
}

// getGitStatusSummary returns a brief status summary for a git directory.
func getGitStatusSummary(dir string) string {
	g := git.NewGit(dir)

	// Check for uncommitted changes
	status, err := g.Status()
	if err != nil {
		return "error"
	}

	if status.Clean {
		return "clean"
	}

	// Count uncommitted files (modified, added, deleted, untracked)
	uncommitted := len(status.Modified) + len(status.Added) + len(status.Deleted) + len(status.Untracked)

	return fmt.Sprintf("%d uncommitted", uncommitted)
}

func runWorktreeRemove(cmd *cobra.Command, args []string) error {
	targetRig := args[0]

	// Detect current clan identity from cwd
	detected, err := detectCrewFromCwd()
	if err != nil {
		return fmt.Errorf("must be in a clan workspace to use this command: %w", err)
	}

	sourceRig := detected.rigName
	crewName := detected.crewName

	// Cannot remove worktree in your own warband (doesn't make sense)
	if targetRig == sourceRig {
		return fmt.Errorf("cannot remove worktree in your own warband '%s'", targetRig)
	}

	// Verify target warband exists
	_, targetRigInfo, err := getRig(targetRig)
	if err != nil {
		return fmt.Errorf("warband '%s' not found - run 'hd warband list' to see available warbands", targetRig)
	}

	// Compute worktree path: ~/horde/<target-warband>/clan/<source-warband>-<name>/
	worktreeName := fmt.Sprintf("%s-%s", sourceRig, crewName)
	worktreePath := filepath.Join(constants.RigCrewPath(targetRigInfo.Path), worktreeName)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree does not exist at %s", worktreePath)
	}

	// Check for uncommitted changes (unless --force)
	if !worktreeRemoveForce {
		statusSummary := getGitStatusSummary(worktreePath)
		if statusSummary != "clean" && statusSummary != "error" {
			return fmt.Errorf("worktree has %s - use --force to remove anyway", statusSummary)
		}
	}

	// Get the target warband's warchief path (where the main git repo is)
	targetWarchiefRig := constants.RigWarchiefPath(targetRigInfo.Path)
	g := git.NewGit(targetWarchiefRig)

	// Remove the worktree
	if err := g.WorktreeRemove(worktreePath, worktreeRemoveForce); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}

	fmt.Printf("%s Removed worktree at %s\n", style.Success.Render("✓"), worktreePath)

	return nil
}
