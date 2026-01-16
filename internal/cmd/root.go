// Package cmd provides CLI commands for the hd tool.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/version"
	"github.com/deeklead/horde/internal/workspace"
)

var rootCmd = &cobra.Command{
	Use:     "hd",
	Short:   "Horde - Multi-agent workspace manager",
	Version: Version,
	Long: `Horde (gt) manages multi-agent workspaces called warbands.

It coordinates agent spawning, work distribution, and communication
across distributed teams of AI agents working on shared codebases.`,
	PersistentPreRunE: persistentPreRun,
}

// Commands that don't require relics to be installed/checked.
// These are basic utility commands that should work without relics.
var relicsExemptCommands = map[string]bool{
	"version":    true,
	"help":       true,
	"completion": true,
}

// Commands exempt from the encampment root branch warning.
// These are commands that help fix the problem or are diagnostic.
var branchCheckExemptCommands = map[string]bool{
	"version":    true,
	"help":       true,
	"completion": true,
	"doctor":     true, // Used to fix the problem
	"install":    true, // Initial setup
	"git-init":   true, // Git setup
}

// persistentPreRun runs before every command.
func persistentPreRun(cmd *cobra.Command, args []string) error {
	// Get the root command name being run
	cmdName := cmd.Name()

	// Check encampment root branch (warning only, non-blocking)
	if !branchCheckExemptCommands[cmdName] {
		warnIfTownRootOffMain()
	}

	// Skip relics check for exempt commands
	if relicsExemptCommands[cmdName] {
		return nil
	}

	// Check relics version
	return CheckRelicsVersion()
}

// warnIfTownRootOffMain prints a warning if the encampment root is not on main branch.
// This is a non-blocking warning to help catch accidental branch switches.
func warnIfTownRootOffMain() {
	// Find encampment root (silently - don't error if not in workspace)
	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		return
	}

	// Check if it's a git repo
	gitDir := townRoot + "/.git"
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return
	}

	// Get current branch
	gitCmd := exec.Command("git", "branch", "--show-current")
	gitCmd.Dir = townRoot
	out, err := gitCmd.Output()
	if err != nil {
		return
	}

	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "main" || branch == "master" {
		return
	}

	// Encampment root is on wrong branch - warn the user
	fmt.Fprintf(os.Stderr, "\n%s Encampment root is on branch '%s' (should be 'main')\n",
		style.Bold.Render("⚠️  WARNING:"), branch)
	fmt.Fprintf(os.Stderr, "   This can cause hd commands to fail. Run: %s\n\n",
		style.Dim.Render("hd doctor --fix"))
}

// checkRelicsDependency verifies relics meets minimum version requirements.
// Skips check for exempt commands (version, help, completion).
// Deprecated: Use persistentPreRun instead, which calls CheckRelicsVersion.
func checkRelicsDependency(cmd *cobra.Command, _ []string) error {
	// Get the root command name being run
	cmdName := cmd.Name()

	// Skip check for exempt commands
	if relicsExemptCommands[cmdName] {
		return nil
	}

	// Check for stale binary (warning only, doesn't block)
	checkStaleBinaryWarning()

	// Check relics version
	return CheckRelicsVersion()
}

// staleBinaryWarned tracks if we've already warned about stale binary in this session.
// We use an environment variable since the binary restarts on each command.
var staleBinaryWarned = os.Getenv("GT_STALE_WARNED") == "1"

// checkStaleBinaryWarning checks if the installed binary is stale and prints a warning.
// This is a non-blocking check - errors are silently ignored.
func checkStaleBinaryWarning() {
	// Only warn once per shell session
	if staleBinaryWarned {
		return
	}

	repoRoot, err := version.GetRepoRoot()
	if err != nil {
		// Can't find repo - silently skip (might be running from non-dev environment)
		return
	}

	info := version.CheckStaleBinary(repoRoot)
	if info.Error != nil {
		// Check failed - silently skip
		return
	}

	if info.IsStale {
		staleBinaryWarned = true
		_ = os.Setenv("GT_STALE_WARNED", "1")

		msg := fmt.Sprintf("hd binary is stale (built from %s, repo at %s)",
			version.ShortCommit(info.BinaryCommit), version.ShortCommit(info.RepoCommit))
		if info.CommitsBehind > 0 {
			msg = fmt.Sprintf("hd binary is %d commits behind (built from %s, repo at %s)",
				info.CommitsBehind, version.ShortCommit(info.BinaryCommit), version.ShortCommit(info.RepoCommit))
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", style.WarningPrefix, msg)
		fmt.Fprintf(os.Stderr, "    %s Run 'hd install' to update\n", style.ArrowPrefix)
	}
}

// Execute runs the root command and returns an exit code.
// The caller (main) should call os.Exit with this code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		// Check for silent exit (scripting commands that signal status via exit code)
		if code, ok := IsSilentExit(err); ok {
			return code
		}
		// Other errors already printed by cobra
		return 1
	}
	return 0
}

// Command group IDs - used by subcommands to organize help output
const (
	GroupWork      = "work"
	GroupAgents    = "agents"
	GroupComm      = "comm"
	GroupServices  = "services"
	GroupWorkspace = "workspace"
	GroupConfig    = "config"
	GroupDiag      = "diag"
)

func init() {
	// Enable prefix matching for subcommands (e.g., "hd ref at" -> "hd forge summon")
	cobra.EnablePrefixMatching = true

	// Define command groups (order determines help output order)
	rootCmd.AddGroup(
		&cobra.Group{ID: GroupWork, Title: "Work Management:"},
		&cobra.Group{ID: GroupAgents, Title: "Agent Management:"},
		&cobra.Group{ID: GroupComm, Title: "Communication:"},
		&cobra.Group{ID: GroupServices, Title: "Services:"},
		&cobra.Group{ID: GroupWorkspace, Title: "Workspace:"},
		&cobra.Group{ID: GroupConfig, Title: "Configuration:"},
		&cobra.Group{ID: GroupDiag, Title: "Diagnostics:"},
	)

	// Put help and completion in a sensible group
	rootCmd.SetHelpCommandGroupID(GroupDiag)
	rootCmd.SetCompletionCommandGroupID(GroupConfig)

	// Global flags can be added here
	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file")
}

// buildCommandPath walks the command hierarchy to build the full command path.
// For example: "hd drums send", "hd status", etc.
func buildCommandPath(cmd *cobra.Command) string {
	var parts []string
	for c := cmd; c != nil; c = c.Parent() {
		parts = append([]string{c.Name()}, parts...)
	}
	return strings.Join(parts, " ")
}

// requireSubcommand returns a RunE function for parent commands that require
// a subcommand. Without this, Cobra silently shows help and exits 0 for
// unknown subcommands like "hd mol foobar", masking errors.
func requireSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("requires a subcommand\n\nRun '%s --help' for usage", buildCommandPath(cmd))
	}
	return fmt.Errorf("unknown command %q for %q\n\nRun '%s --help' for available commands",
		args[0], buildCommandPath(cmd), buildCommandPath(cmd))
}
