// Package cmd provides CLI commands for the hd tool.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/clan"
	"github.com/deeklead/horde/internal/deps"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/forge"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/wisp"
	"github.com/deeklead/horde/internal/witness"
	"github.com/deeklead/horde/internal/workspace"
)

var rigCmd = &cobra.Command{
	Use:     "warband",
	GroupID: GroupWorkspace,
	Short:   "Manage warbands in the workspace",
	RunE:    requireSubcommand,
	Long: `Manage warbands (project containers) in the Horde workspace.

A warband is a container for managing a project and its agents:
  - forge/warband/  Canonical main clone (Forge's working copy)
  - warchief/warband/     Warchief's working clone for this warband
  - clan/<name>/   Human workspace(s)
  - witness/       Witness agent (no clone)
  - raiders/      Worker directories
  - .relics/        Warband-level issue tracking`,
}

var rigAddCmd = &cobra.Command{
	Use:   "add <name> <git-url>",
	Short: "Add a new warband to the workspace",
	Long: `Add a new warband by cloning a repository.

This creates a warband container with:
  - config.json           Warband configuration
  - .relics/               Warband-level issue tracking (initialized)
  - plugins/              Warband-level plugin directory
  - forge/warband/         Canonical main clone
  - warchief/warband/            Warchief's working clone
  - clan/                 Empty clan directory (add members with 'hd clan add')
  - witness/              Witness agent directory
  - raiders/             Worker directory (empty)

The command also:
  - Seeds scout totems (Shaman, Witness, Forge)
  - Creates ~/horde/plugins/ (encampment-level) if it doesn't exist
  - Creates <warband>/plugins/ (warband-level)

Example:
  hd warband add horde https://github.com/deeklead/horde
  hd warband add my-project git@github.com:user/repo.git --prefix mp`,
	Args: cobra.ExactArgs(2),
	RunE: runRigAdd,
}

var rigListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all warbands in the workspace",
	RunE:  runRigList,
}

var rigRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a warband from the registry (does not delete files)",
	Args:  cobra.ExactArgs(1),
	RunE:  runRigRemove,
}

var rigResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset warband state (handoff content, drums, stale issues)",
	Long: `Reset various warband state.

By default, resets all resettable state. Use flags to reset specific items.

Examples:
  hd warband reset              # Reset all state
  hd warband reset --handoff    # Clear handoff content only
  hd warband reset --drums       # Clear stale drums messages only
  hd warband reset --stale      # Reset orphaned in_progress issues
  hd warband reset --stale --dry-run  # Preview what would be reset`,
	RunE: runRigReset,
}

var rigBootCmd = &cobra.Command{
	Use:   "boot <warband>",
	Short: "Start witness and forge for a warband",
	Long: `Start the witness and forge agents for a warband.

This is the inverse of 'hd warband shutdown'. It starts:
- The witness (if not already running)
- The forge (if not already running)

Raiders are NOT started by this command - they are spawned
on demand when work is assigned.

Examples:
  hd warband boot greenplace`,
	Args: cobra.ExactArgs(1),
	RunE: runRigBoot,
}

var rigStartCmd = &cobra.Command{
	Use:   "start <warband>...",
	Short: "Start witness and forge on scout for one or more warbands",
	Long: `Start the witness and forge agents on scout for one or more warbands.

This is similar to 'hd warband boot' but supports multiple warbands at once.
For each warband, it starts:
- The witness (if not already running)
- The forge (if not already running)

Raiders are NOT started by this command - they are spawned
on demand when work is assigned.

Examples:
  hd warband start horde
  hd warband start horde relics
  hd warband start horde relics myproject`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRigStart,
}

var rigRebootCmd = &cobra.Command{
	Use:   "reboot <warband>",
	Short: "Restart witness and forge for a warband",
	Long: `Restart the scout agents (witness and forge) for a warband.

This is equivalent to 'hd warband shutdown' followed by 'hd warband boot'.
Useful after raiders complete work and land their changes.

Examples:
  hd warband reboot greenplace
  hd warband reboot relics --force`,
	Args: cobra.ExactArgs(1),
	RunE: runRigReboot,
}

var rigShutdownCmd = &cobra.Command{
	Use:   "shutdown <warband>",
	Short: "Gracefully stop all warband agents",
	Long: `Stop all agents in a warband.

This command gracefully shuts down:
- All raider sessions
- The forge (if running)
- The witness (if running)

Before shutdown, checks all raiders for uncommitted work:
- Uncommitted changes (modified/untracked files)
- Stashes
- Unpushed commits

Use --force to skip graceful shutdown and kill immediately.
Use --nuclear to bypass ALL safety checks (will lose work!).

Examples:
  hd warband shutdown greenplace
  hd warband shutdown greenplace --force
  hd warband shutdown greenplace --nuclear  # DANGER: loses uncommitted work`,
	Args: cobra.ExactArgs(1),
	RunE: runRigShutdown,
}

var rigStatusCmd = &cobra.Command{
	Use:   "status [warband]",
	Short: "Show detailed status for a specific warband",
	Long: `Show detailed status for a specific warband including all workers.

If no warband is specified, infers the warband from the current directory.

Displays:
- Warband information (name, path, relics prefix)
- Witness status (running/stopped, uptime)
- Forge status (running/stopped, uptime, queue size)
- Raiders (name, state, assigned issue, session status)
- Clan members (name, branch, session status, git status)

Examples:
  hd warband status           # Infer warband from current directory
  hd warband status horde
  hd warband status relics`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRigStatus,
}

var rigStopCmd = &cobra.Command{
	Use:   "stop <warband>...",
	Short: "Stop one or more warbands (shutdown semantics)",
	Long: `Stop all agents in one or more warbands.

This command is similar to 'hd warband shutdown' but supports multiple warbands.
For each warband, it gracefully shuts down:
- All raider sessions
- The forge (if running)
- The witness (if running)

Before shutdown, checks all raiders for uncommitted work:
- Uncommitted changes (modified/untracked files)
- Stashes
- Unpushed commits

Use --force to skip graceful shutdown and kill immediately.
Use --nuclear to bypass ALL safety checks (will lose work!).

Examples:
  hd warband stop horde
  hd warband stop horde relics
  hd warband stop --force horde relics
  hd warband stop --nuclear horde  # DANGER: loses uncommitted work`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRigStop,
}

var rigRestartCmd = &cobra.Command{
	Use:   "restart <warband>...",
	Short: "Restart one or more warbands (stop then start)",
	Long: `Restart the scout agents (witness and forge) for one or more warbands.

This is equivalent to 'hd warband stop' followed by 'hd warband start' for each warband.
Useful after raiders complete work and land their changes.

Before shutdown, checks all raiders for uncommitted work:
- Uncommitted changes (modified/untracked files)
- Stashes
- Unpushed commits

Use --force to skip graceful shutdown and kill immediately.
Use --nuclear to bypass ALL safety checks (will lose work!).

Examples:
  hd warband restart horde
  hd warband restart horde relics
  hd warband restart --force horde relics
  hd warband restart --nuclear horde  # DANGER: loses uncommitted work`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRigRestart,
}

// Flags
var (
	rigAddPrefix       string
	rigAddLocalRepo    string
	rigAddBranch       string
	rigResetHandoff    bool
	rigResetMail       bool
	rigResetStale      bool
	rigResetDryRun     bool
	rigResetRole       string
	rigShutdownForce   bool
	rigShutdownNuclear bool
	rigStopForce       bool
	rigStopNuclear     bool
	rigRestartForce    bool
	rigRestartNuclear  bool
)

func init() {
	rootCmd.AddCommand(rigCmd)
	rigCmd.AddCommand(rigAddCmd)
	rigCmd.AddCommand(rigBootCmd)
	rigCmd.AddCommand(rigListCmd)
	rigCmd.AddCommand(rigRebootCmd)
	rigCmd.AddCommand(rigRemoveCmd)
	rigCmd.AddCommand(rigResetCmd)
	rigCmd.AddCommand(rigRestartCmd)
	rigCmd.AddCommand(rigShutdownCmd)
	rigCmd.AddCommand(rigStartCmd)
	rigCmd.AddCommand(rigStatusCmd)
	rigCmd.AddCommand(rigStopCmd)

	rigAddCmd.Flags().StringVar(&rigAddPrefix, "prefix", "", "Relics issue prefix (default: derived from name)")
	rigAddCmd.Flags().StringVar(&rigAddLocalRepo, "local-repo", "", "Local repo path to share git objects (optional)")
	rigAddCmd.Flags().StringVar(&rigAddBranch, "branch", "", "Default branch name (default: auto-detected from remote)")

	rigResetCmd.Flags().BoolVar(&rigResetHandoff, "handoff", false, "Clear handoff content")
	rigResetCmd.Flags().BoolVar(&rigResetMail, "drums", false, "Clear stale drums messages")
	rigResetCmd.Flags().BoolVar(&rigResetStale, "stale", false, "Reset orphaned in_progress issues (no active session)")
	rigResetCmd.Flags().BoolVar(&rigResetDryRun, "dry-run", false, "Show what would be reset without making changes")
	rigResetCmd.Flags().StringVar(&rigResetRole, "role", "", "Role to reset (default: auto-detect from cwd)")

	rigShutdownCmd.Flags().BoolVarP(&rigShutdownForce, "force", "f", false, "Force immediate shutdown")
	rigShutdownCmd.Flags().BoolVar(&rigShutdownNuclear, "nuclear", false, "DANGER: Bypass ALL safety checks (loses uncommitted work!)")

	rigRebootCmd.Flags().BoolVarP(&rigShutdownForce, "force", "f", false, "Force immediate shutdown during reboot")

	rigStopCmd.Flags().BoolVarP(&rigStopForce, "force", "f", false, "Force immediate shutdown")
	rigStopCmd.Flags().BoolVar(&rigStopNuclear, "nuclear", false, "DANGER: Bypass ALL safety checks (loses uncommitted work!)")

	rigRestartCmd.Flags().BoolVarP(&rigRestartForce, "force", "f", false, "Force immediate shutdown during restart")
	rigRestartCmd.Flags().BoolVar(&rigRestartNuclear, "nuclear", false, "DANGER: Bypass ALL safety checks (loses uncommitted work!)")
}

func runRigAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	gitURL := args[1]

	// Ensure relics (bd) is available before proceeding
	if err := deps.EnsureRelics(true); err != nil {
		return fmt.Errorf("relics dependency check failed: %w", err)
	}

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		// Create new if doesn't exist
		rigsConfig = &config.RigsConfig{
			Version: 1,
			Warbands:    make(map[string]config.RigEntry),
		}
	}

	// Create warband manager
	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	fmt.Printf("Creating warband %s...\n", style.Bold.Render(name))
	fmt.Printf("  Repository: %s\n", gitURL)
	if rigAddLocalRepo != "" {
		fmt.Printf("  Local repo: %s\n", rigAddLocalRepo)
	}

	startTime := time.Now()

	// Add the warband
	newRig, err := mgr.AddRig(warband.AddRigOptions{
		Name:          name,
		GitURL:        gitURL,
		RelicsPrefix:   rigAddPrefix,
		LocalRepo:     rigAddLocalRepo,
		DefaultBranch: rigAddBranch,
	})
	if err != nil {
		return fmt.Errorf("adding warband: %w", err)
	}

	// Save updated warbands config
	if err := config.SaveRigsConfig(rigsPath, rigsConfig); err != nil {
		return fmt.Errorf("saving warbands config: %w", err)
	}

	// Add route to encampment-level routes.jsonl for prefix-based routing.
	// Route points to the canonical relics location:
	// - If source repo has .relics/ tracked in git, route to warchief/warband
	// - Otherwise route to warband root (where initRelics creates the database)
	// The conditional routing is necessary because initRelics creates the database at
	// "<warband>/.relics", while repos with tracked relics have their database at warchief/warband/.relics.
	var relicsWorkDir string
	if newRig.Config.Prefix != "" {
		routePath := name
		warchiefRigRelics := filepath.Join(townRoot, name, "warchief", "warband", ".relics")
		if _, err := os.Stat(warchiefRigRelics); err == nil {
			// Source repo has .relics/ tracked - route to warchief/warband
			routePath = name + "/warchief/warband"
			relicsWorkDir = filepath.Join(townRoot, name, "warchief", "warband")
		} else {
			relicsWorkDir = filepath.Join(townRoot, name)
		}
		route := relics.Route{
			Prefix: newRig.Config.Prefix + "-",
			Path:   routePath,
		}
		if err := relics.AppendRoute(townRoot, route); err != nil {
			// Non-fatal: routing will still work, just not from encampment root
			fmt.Printf("  %s Could not update routes.jsonl: %v\n", style.Warning.Render("!"), err)
		}
	}

	// Create warband identity bead
	if newRig.Config.Prefix != "" && relicsWorkDir != "" {
		bd := relics.New(relicsWorkDir)
		rigBeadID := relics.RigBeadIDWithPrefix(newRig.Config.Prefix, name)
		fields := &relics.RigFields{
			Repo:   gitURL,
			Prefix: newRig.Config.Prefix,
			State:  "active",
		}
		if _, err := bd.CreateRigBead(rigBeadID, name, fields); err != nil {
			// Non-fatal: warband is functional without the identity bead
			fmt.Printf("  %s Could not create warband identity bead: %v\n", style.Warning.Render("!"), err)
		} else {
			fmt.Printf("  Created warband identity bead: %s\n", rigBeadID)
		}
	}

	elapsed := time.Since(startTime)

	// Read default branch from warband config
	defaultBranch := "main"
	if rigCfg, err := warband.LoadRigConfig(filepath.Join(townRoot, name)); err == nil && rigCfg.DefaultBranch != "" {
		defaultBranch = rigCfg.DefaultBranch
	}

	fmt.Printf("\n%s Warband created in %.1fs\n", style.Success.Render("✓"), elapsed.Seconds())
	fmt.Printf("\nStructure:\n")
	fmt.Printf("  %s/\n", name)
	fmt.Printf("  ├── config.json\n")
	fmt.Printf("  ├── .repo.git/        (shared bare repo for forge+raiders)\n")
	fmt.Printf("  ├── .relics/           (prefix: %s)\n", newRig.Config.Prefix)
	fmt.Printf("  ├── plugins/          (warband-level plugins)\n")
	fmt.Printf("  ├── warchief/warband/        (clone: %s)\n", defaultBranch)
	fmt.Printf("  ├── forge/warband/     (worktree: %s, sees raider branches)\n", defaultBranch)
	fmt.Printf("  ├── clan/             (empty - add clan with 'hd clan add')\n")
	fmt.Printf("  ├── witness/\n")
	fmt.Printf("  └── raiders/\n")

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  hd clan add <name> --warband %s   # Create your personal workspace\n", name)
	fmt.Printf("  cd %s/clan/<name>              # Start working\n", filepath.Join(townRoot, name))

	return nil
}

func runRigList(cmd *cobra.Command, args []string) error {
	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		fmt.Println("No warbands configured.")
		return nil
	}

	if len(rigsConfig.Warbands) == 0 {
		fmt.Println("No warbands configured.")
		fmt.Printf("\nAdd one with: %s\n", style.Dim.Render("hd warband add <name> <git-url>"))
		return nil
	}

	// Create warband manager to get details
	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	fmt.Printf("Warbands in %s:\n\n", townRoot)

	for name := range rigsConfig.Warbands {
		r, err := mgr.GetRig(name)
		if err != nil {
			fmt.Printf("  %s %s\n", style.Warning.Render("!"), name)
			continue
		}

		summary := r.Summary()
		fmt.Printf("  %s\n", style.Bold.Render(name))
		fmt.Printf("    Raiders: %d  Clan: %d\n", summary.RaiderCount, summary.CrewCount)

		agents := []string{}
		if summary.HasForge {
			agents = append(agents, "forge")
		}
		if summary.HasWitness {
			agents = append(agents, "witness")
		}
		if r.HasWarchief {
			agents = append(agents, "warchief")
		}
		if len(agents) > 0 {
			fmt.Printf("    Agents: %v\n", agents)
		}
		fmt.Println()
	}

	return nil
}

func runRigRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		return fmt.Errorf("loading warbands config: %w", err)
	}

	// Create warband manager
	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	if err := mgr.RemoveRig(name); err != nil {
		return fmt.Errorf("removing warband: %w", err)
	}

	// Save updated config
	if err := config.SaveRigsConfig(rigsPath, rigsConfig); err != nil {
		return fmt.Errorf("saving warbands config: %w", err)
	}

	fmt.Printf("%s Warband %s removed from registry\n", style.Success.Render("✓"), name)
	fmt.Printf("\nNote: Files at %s were NOT deleted.\n", filepath.Join(townRoot, name))
	fmt.Printf("To delete: %s\n", style.Dim.Render(fmt.Sprintf("rm -rf %s", filepath.Join(townRoot, name))))

	return nil
}

func runRigReset(cmd *cobra.Command, args []string) error {
	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Determine role to reset
	roleKey := rigResetRole
	if roleKey == "" {
		// Auto-detect using env-aware role detection
		roleInfo, err := GetRoleWithContext(cwd, townRoot)
		if err != nil {
			return fmt.Errorf("detecting role: %w", err)
		}
		if roleInfo.Role == RoleUnknown {
			return fmt.Errorf("could not detect role; use --role to specify")
		}
		roleKey = string(roleInfo.Role)
	}

	// If no specific flags, reset all; otherwise only reset what's specified
	resetAll := !rigResetHandoff && !rigResetMail && !rigResetStale

	// Encampment relics for handoff/drums operations
	townBd := relics.New(townRoot)
	// Warband relics for issue operations (uses cwd to find .relics/)
	rigBd := relics.New(cwd)

	// Reset handoff content
	if resetAll || rigResetHandoff {
		if err := townBd.ClearHandoffContent(roleKey); err != nil {
			return fmt.Errorf("clearing handoff content: %w", err)
		}
		fmt.Printf("%s Cleared handoff content for %s\n", style.Success.Render("✓"), roleKey)
	}

	// Clear stale drums messages
	if resetAll || rigResetMail {
		result, err := townBd.ClearMail("Cleared during reset")
		if err != nil {
			return fmt.Errorf("clearing drums: %w", err)
		}
		if result.Closed > 0 || result.Cleared > 0 {
			fmt.Printf("%s Cleared drums: %d closed, %d pinned cleared\n",
				style.Success.Render("✓"), result.Closed, result.Cleared)
		} else {
			fmt.Printf("%s No drums to clear\n", style.Success.Render("✓"))
		}
	}

	// Reset stale in_progress issues
	if resetAll || rigResetStale {
		if err := runResetStale(rigBd, rigResetDryRun); err != nil {
			return fmt.Errorf("resetting stale issues: %w", err)
		}
	}

	return nil
}

// runResetStale resets in_progress issues whose assigned agent no longer has a session.
func runResetStale(bd *relics.Relics, dryRun bool) error {
	t := tmux.NewTmux()

	// Get all in_progress issues
	issues, err := bd.List(relics.ListOptions{
		Status:   "in_progress",
		Priority: -1, // All priorities
	})
	if err != nil {
		return fmt.Errorf("listing in_progress issues: %w", err)
	}

	if len(issues) == 0 {
		fmt.Printf("%s No in_progress issues found\n", style.Success.Render("✓"))
		return nil
	}

	var resetCount, skippedCount int
	var resetIssues []string

	for _, issue := range issues {
		if issue.Assignee == "" {
			continue // No assignee to check
		}

		// Parse assignee: warband/name or warband/clan/name
		sessionName, isPersistent := assigneeToSessionName(issue.Assignee)
		if sessionName == "" {
			continue // Couldn't parse assignee
		}

		// Check if session exists
		hasSession, err := t.HasSession(sessionName)
		if err != nil {
			// tmux error, skip this one
			continue
		}

		if hasSession {
			continue // Session exists, not stale
		}

		// For clan (persistent identities), only reset if explicitly checking sessions
		if isPersistent {
			skippedCount++
			if dryRun {
				fmt.Printf("  %s: %s %s\n",
					style.Dim.Render(issue.ID),
					issue.Assignee,
					style.Dim.Render("(persistent, skipped)"))
			}
			continue
		}

		// Session doesn't exist - this is stale
		if dryRun {
			fmt.Printf("  %s: %s (no session) → open\n",
				style.Bold.Render(issue.ID),
				issue.Assignee)
		} else {
			// Reset status to open and clear assignee
			openStatus := "open"
			emptyAssignee := ""
			if err := bd.Update(issue.ID, relics.UpdateOptions{
				Status:   &openStatus,
				Assignee: &emptyAssignee,
			}); err != nil {
				fmt.Printf("  %s Failed to reset %s: %v\n",
					style.Warning.Render("⚠"),
					issue.ID, err)
				continue
			}
		}
		resetCount++
		resetIssues = append(resetIssues, issue.ID)
	}

	if dryRun {
		if resetCount > 0 || skippedCount > 0 {
			fmt.Printf("\n%s Would reset %d issues, skip %d persistent\n",
				style.Dim.Render("(dry-run)"),
				resetCount, skippedCount)
		} else {
			fmt.Printf("%s No stale issues found\n", style.Success.Render("✓"))
		}
	} else {
		if resetCount > 0 {
			fmt.Printf("%s Reset %d stale issues: %v\n",
				style.Success.Render("✓"),
				resetCount, resetIssues)
		} else {
			fmt.Printf("%s No stale issues to reset\n", style.Success.Render("✓"))
		}
		if skippedCount > 0 {
			fmt.Printf("  Skipped %d persistent (clan) issues\n", skippedCount)
		}
	}

	return nil
}

// assigneeToSessionName converts an assignee (warband/name or warband/clan/name) to tmux session name.
// Returns the session name and whether this is a persistent identity (clan).
func assigneeToSessionName(assignee string) (sessionName string, isPersistent bool) {
	parts := strings.Split(assignee, "/")

	switch len(parts) {
	case 2:
		// warband/raiderName -> gt-warband-raiderName
		return fmt.Sprintf("hd-%s-%s", parts[0], parts[1]), false
	case 3:
		// warband/clan/name -> gt-warband-clan-name
		if parts[1] == "clan" {
			return fmt.Sprintf("hd-%s-clan-%s", parts[0], parts[2]), true
		}
		// Other 3-part formats not recognized
		return "", false
	default:
		return "", false
	}
}

// Helper to check if path exists
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runRigBoot(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config and get warband
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(rigName)
	if err != nil {
		return fmt.Errorf("warband '%s' not found", rigName)
	}

	fmt.Printf("Booting warband %s...\n", style.Bold.Render(rigName))

	var started []string
	var skipped []string

	t := tmux.NewTmux()

	// 1. Start the witness
	// Check actual tmux session, not state file (may be stale)
	witnessSession := fmt.Sprintf("hd-%s-witness", rigName)
	witnessRunning, _ := t.HasSession(witnessSession)
	if witnessRunning {
		skipped = append(skipped, "witness (already running)")
	} else {
		fmt.Printf("  Starting witness...\n")
		witMgr := witness.NewManager(r)
		if err := witMgr.Start(false, "", nil); err != nil {
			if err == witness.ErrAlreadyRunning {
				skipped = append(skipped, "witness (already running)")
			} else {
				return fmt.Errorf("starting witness: %w", err)
			}
		} else {
			started = append(started, "witness")
		}
	}

	// 2. Start the forge
	// Check actual tmux session, not state file (may be stale)
	forgeSession := fmt.Sprintf("hd-%s-forge", rigName)
	forgeRunning, _ := t.HasSession(forgeSession)
	if forgeRunning {
		skipped = append(skipped, "forge (already running)")
	} else {
		fmt.Printf("  Starting forge...\n")
		refMgr := forge.NewManager(r)
		if err := refMgr.Start(false, ""); err != nil { // false = background mode
			return fmt.Errorf("starting forge: %w", err)
		}
		started = append(started, "forge")
	}

	// Report results
	if len(started) > 0 {
		fmt.Printf("%s Started: %s\n", style.Success.Render("✓"), strings.Join(started, ", "))
	}
	if len(skipped) > 0 {
		fmt.Printf("%s Skipped: %s\n", style.Dim.Render("•"), strings.Join(skipped, ", "))
	}

	return nil
}

func runRigStart(cmd *cobra.Command, args []string) error {
	// Find workspace once
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	t := tmux.NewTmux()

	var successRigs []string
	var failedRigs []string

	for _, rigName := range args {
		r, err := rigMgr.GetRig(rigName)
		if err != nil {
			fmt.Printf("%s Warband '%s' not found\n", style.Warning.Render("⚠"), rigName)
			failedRigs = append(failedRigs, rigName)
			continue
		}

		fmt.Printf("Starting warband %s...\n", style.Bold.Render(rigName))

		var started []string
		var skipped []string
		hasError := false

		// 1. Start the witness
		witnessSession := fmt.Sprintf("hd-%s-witness", rigName)
		witnessRunning, _ := t.HasSession(witnessSession)
		if witnessRunning {
			skipped = append(skipped, "witness")
		} else {
			fmt.Printf("  Starting witness...\n")
			witMgr := witness.NewManager(r)
			if err := witMgr.Start(false, "", nil); err != nil {
				if err == witness.ErrAlreadyRunning {
					skipped = append(skipped, "witness")
				} else {
					fmt.Printf("  %s Failed to start witness: %v\n", style.Warning.Render("⚠"), err)
					hasError = true
				}
			} else {
				started = append(started, "witness")
			}
		}

		// 2. Start the forge
		forgeSession := fmt.Sprintf("hd-%s-forge", rigName)
		forgeRunning, _ := t.HasSession(forgeSession)
		if forgeRunning {
			skipped = append(skipped, "forge")
		} else {
			fmt.Printf("  Starting forge...\n")
			refMgr := forge.NewManager(r)
			if err := refMgr.Start(false, ""); err != nil {
				fmt.Printf("  %s Failed to start forge: %v\n", style.Warning.Render("⚠"), err)
				hasError = true
			} else {
				started = append(started, "forge")
			}
		}

		// Report results for this warband
		if len(started) > 0 {
			fmt.Printf("  %s Started: %s\n", style.Success.Render("✓"), strings.Join(started, ", "))
		}
		if len(skipped) > 0 {
			fmt.Printf("  %s Skipped: %s (already running)\n", style.Dim.Render("•"), strings.Join(skipped, ", "))
		}

		if hasError {
			failedRigs = append(failedRigs, rigName)
		} else {
			successRigs = append(successRigs, rigName)
		}
		fmt.Println()
	}

	// Summary
	if len(successRigs) > 0 {
		fmt.Printf("%s Started warbands: %s\n", style.Success.Render("✓"), strings.Join(successRigs, ", "))
	}
	if len(failedRigs) > 0 {
		fmt.Printf("%s Failed warbands: %s\n", style.Warning.Render("⚠"), strings.Join(failedRigs, ", "))
		return fmt.Errorf("some warbands failed to start")
	}

	return nil
}

func runRigShutdown(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config and get warband
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(rigName)
	if err != nil {
		return fmt.Errorf("warband '%s' not found", rigName)
	}

	// Check all raiders for uncommitted work (unless nuclear)
	if !rigShutdownNuclear {
		raiderGit := git.NewGit(r.Path)
		raiderMgr := raider.NewManager(r, raiderGit, nil) // nil tmux: just listing
		raiders, err := raiderMgr.List()
		if err == nil && len(raiders) > 0 {
			var problemRaiders []struct {
				name   string
				status *git.UncommittedWorkStatus
			}

			for _, p := range raiders {
				pGit := git.NewGit(p.ClonePath)
				status, err := pGit.CheckUncommittedWork()
				if err == nil && !status.Clean() {
					problemRaiders = append(problemRaiders, struct {
						name   string
						status *git.UncommittedWorkStatus
					}{p.Name, status})
				}
			}

			if len(problemRaiders) > 0 {
				fmt.Printf("\n%s Cannot shutdown - raiders have uncommitted work:\n\n", style.Warning.Render("⚠"))
				for _, pp := range problemRaiders {
					fmt.Printf("  %s: %s\n", style.Bold.Render(pp.name), pp.status.String())
				}
				fmt.Printf("\nUse %s to force shutdown (DANGER: will lose work!)\n", style.Bold.Render("--nuclear"))
				return fmt.Errorf("refusing to shutdown with uncommitted work")
			}
		}
	}

	fmt.Printf("Shutting down warband %s...\n", style.Bold.Render(rigName))

	var errors []string

	// 1. Stop all raider sessions
	t := tmux.NewTmux()
	raiderMgr := raider.NewSessionManager(t, r)
	infos, err := raiderMgr.List()
	if err == nil && len(infos) > 0 {
		fmt.Printf("  Stopping %d raider session(s)...\n", len(infos))
		if err := raiderMgr.StopAll(rigShutdownForce); err != nil {
			errors = append(errors, fmt.Sprintf("raider sessions: %v", err))
		}
	}

	// 2. Stop the forge
	refMgr := forge.NewManager(r)
	refStatus, err := refMgr.Status()
	if err == nil && refStatus.State == forge.StateRunning {
		fmt.Printf("  Stopping forge...\n")
		if err := refMgr.Stop(); err != nil {
			errors = append(errors, fmt.Sprintf("forge: %v", err))
		}
	}

	// 3. Stop the witness
	witMgr := witness.NewManager(r)
	witStatus, err := witMgr.Status()
	if err == nil && witStatus.State == witness.StateRunning {
		fmt.Printf("  Stopping witness...\n")
		if err := witMgr.Stop(); err != nil {
			errors = append(errors, fmt.Sprintf("witness: %v", err))
		}
	}

	if len(errors) > 0 {
		fmt.Printf("\n%s Some agents failed to stop:\n", style.Warning.Render("⚠"))
		for _, e := range errors {
			fmt.Printf("  - %s\n", e)
		}
		return fmt.Errorf("shutdown incomplete")
	}

	fmt.Printf("%s Warband %s shut down successfully\n", style.Success.Render("✓"), rigName)
	return nil
}

func runRigReboot(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	fmt.Printf("Rebooting warband %s...\n\n", style.Bold.Render(rigName))

	// Shutdown first
	if err := runRigShutdown(cmd, args); err != nil {
		// If shutdown fails due to uncommitted work, propagate the error
		return err
	}

	fmt.Println() // Blank line between shutdown and boot

	// Boot
	if err := runRigBoot(cmd, args); err != nil {
		return fmt.Errorf("boot failed: %w", err)
	}

	fmt.Printf("\n%s Warband %s rebooted successfully\n", style.Success.Render("✓"), rigName)
	return nil
}

func runRigStatus(cmd *cobra.Command, args []string) error {
	var rigName string

	if len(args) > 0 {
		rigName = args[0]
	} else {
		// Infer warband from current directory
		roleInfo, err := GetRole()
		if err != nil {
			return fmt.Errorf("detecting warband from current directory: %w", err)
		}
		if roleInfo.Warband == "" {
			return fmt.Errorf("could not detect warband from current directory; please specify warband name")
		}
		rigName = roleInfo.Warband
	}

	// Get warband
	townRoot, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	t := tmux.NewTmux()

	// Header
	fmt.Printf("%s\n", style.Bold.Render(rigName))

	// Operational state
	opState, opSource := getRigOperationalState(townRoot, rigName)
	if opState == "OPERATIONAL" {
		fmt.Printf("  Status: %s\n", style.Success.Render(opState))
	} else if opState == "PARKED" {
		fmt.Printf("  Status: %s (%s)\n", style.Warning.Render(opState), opSource)
	} else if opState == "DOCKED" {
		fmt.Printf("  Status: %s (%s)\n", style.Dim.Render(opState), opSource)
	}

	fmt.Printf("  Path: %s\n", r.Path)
	if r.Config != nil && r.Config.Prefix != "" {
		fmt.Printf("  Relics prefix: %s-\n", r.Config.Prefix)
	}
	fmt.Println()

	// Witness status
	fmt.Printf("%s\n", style.Bold.Render("Witness"))
	witnessSession := fmt.Sprintf("hd-%s-witness", rigName)
	witnessRunning, _ := t.HasSession(witnessSession)
	witMgr := witness.NewManager(r)
	witStatus, _ := witMgr.Status()
	if witnessRunning {
		fmt.Printf("  %s running", style.Success.Render("●"))
		if witStatus != nil && witStatus.StartedAt != nil {
			fmt.Printf(" (uptime: %s)", formatDuration(time.Since(*witStatus.StartedAt)))
		}
		fmt.Printf("\n")
	} else {
		fmt.Printf("  %s stopped\n", style.Dim.Render("○"))
	}
	fmt.Println()

	// Forge status
	fmt.Printf("%s\n", style.Bold.Render("Forge"))
	forgeSession := fmt.Sprintf("hd-%s-forge", rigName)
	forgeRunning, _ := t.HasSession(forgeSession)
	refMgr := forge.NewManager(r)
	refStatus, _ := refMgr.Status()
	if forgeRunning {
		fmt.Printf("  %s running", style.Success.Render("●"))
		if refStatus != nil && refStatus.StartedAt != nil {
			fmt.Printf(" (uptime: %s)", formatDuration(time.Since(*refStatus.StartedAt)))
		}
		fmt.Printf("\n")
		// Show queue size
		queue, err := refMgr.Queue()
		if err == nil && len(queue) > 0 {
			fmt.Printf("  Queue: %d items\n", len(queue))
		}
	} else {
		fmt.Printf("  %s stopped\n", style.Dim.Render("○"))
	}
	fmt.Println()

	// Raiders
	raiderGit := git.NewGit(r.Path)
	raiderMgr := raider.NewManager(r, raiderGit, t)
	raiders, err := raiderMgr.List()
	fmt.Printf("%s", style.Bold.Render("Raiders"))
	if err != nil || len(raiders) == 0 {
		fmt.Printf(" (none)\n")
	} else {
		fmt.Printf(" (%d)\n", len(raiders))
		for _, p := range raiders {
			sessionName := fmt.Sprintf("hd-%s-%s", rigName, p.Name)
			hasSession, _ := t.HasSession(sessionName)

			sessionIcon := style.Dim.Render("○")
			if hasSession {
				sessionIcon = style.Success.Render("●")
			}

			stateStr := string(p.State)
			if p.Issue != "" {
				stateStr = fmt.Sprintf("%s → %s", p.State, p.Issue)
			}

			fmt.Printf("  %s %s: %s\n", sessionIcon, p.Name, stateStr)
		}
	}
	fmt.Println()

	// Clan
	crewMgr := clan.NewManager(r, git.NewGit(townRoot))
	crewWorkers, err := crewMgr.List()
	fmt.Printf("%s", style.Bold.Render("Clan"))
	if err != nil || len(crewWorkers) == 0 {
		fmt.Printf(" (none)\n")
	} else {
		fmt.Printf(" (%d)\n", len(crewWorkers))
		for _, w := range crewWorkers {
			sessionName := crewSessionName(rigName, w.Name)
			hasSession, _ := t.HasSession(sessionName)

			sessionIcon := style.Dim.Render("○")
			if hasSession {
				sessionIcon = style.Success.Render("●")
			}

			// Get git info
			crewGit := git.NewGit(w.ClonePath)
			branch, _ := crewGit.CurrentBranch()
			gitStatus, _ := crewGit.Status()

			gitInfo := ""
			if gitStatus != nil && !gitStatus.Clean {
				gitInfo = style.Warning.Render(" (dirty)")
			}

			fmt.Printf("  %s %s: %s%s\n", sessionIcon, w.Name, branch, gitInfo)
		}
	}

	return nil
}

func runRigStop(cmd *cobra.Command, args []string) error {
	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)

	// Track results
	var succeeded []string
	var failed []string

	// Process each warband
	for _, rigName := range args {
		r, err := rigMgr.GetRig(rigName)
		if err != nil {
			fmt.Printf("%s Warband '%s' not found\n", style.Warning.Render("⚠"), rigName)
			failed = append(failed, rigName)
			continue
		}

		// Check all raiders for uncommitted work (unless nuclear)
		if !rigStopNuclear {
			raiderGit := git.NewGit(r.Path)
			raiderMgr := raider.NewManager(r, raiderGit, nil) // nil tmux: just listing
			raiders, err := raiderMgr.List()
			if err == nil && len(raiders) > 0 {
				var problemRaiders []struct {
					name   string
					status *git.UncommittedWorkStatus
				}

				for _, p := range raiders {
					pGit := git.NewGit(p.ClonePath)
					status, err := pGit.CheckUncommittedWork()
					if err == nil && !status.Clean() {
						problemRaiders = append(problemRaiders, struct {
							name   string
							status *git.UncommittedWorkStatus
						}{p.Name, status})
					}
				}

				if len(problemRaiders) > 0 {
					fmt.Printf("\n%s Cannot stop %s - raiders have uncommitted work:\n", style.Warning.Render("⚠"), rigName)
					for _, pp := range problemRaiders {
						fmt.Printf("  %s: %s\n", style.Bold.Render(pp.name), pp.status.String())
					}
					failed = append(failed, rigName)
					continue
				}
			}
		}

		fmt.Printf("Stopping warband %s...\n", style.Bold.Render(rigName))

		var errors []string

		// 1. Stop all raider sessions
		t := tmux.NewTmux()
		raiderMgr := raider.NewSessionManager(t, r)
		infos, err := raiderMgr.List()
		if err == nil && len(infos) > 0 {
			fmt.Printf("  Stopping %d raider session(s)...\n", len(infos))
			if err := raiderMgr.StopAll(rigStopForce); err != nil {
				errors = append(errors, fmt.Sprintf("raider sessions: %v", err))
			}
		}

		// 2. Stop the forge
		refMgr := forge.NewManager(r)
		refStatus, err := refMgr.Status()
		if err == nil && refStatus.State == forge.StateRunning {
			fmt.Printf("  Stopping forge...\n")
			if err := refMgr.Stop(); err != nil {
				errors = append(errors, fmt.Sprintf("forge: %v", err))
			}
		}

		// 3. Stop the witness
		witMgr := witness.NewManager(r)
		witStatus, err := witMgr.Status()
		if err == nil && witStatus.State == witness.StateRunning {
			fmt.Printf("  Stopping witness...\n")
			if err := witMgr.Stop(); err != nil {
				errors = append(errors, fmt.Sprintf("witness: %v", err))
			}
		}

		if len(errors) > 0 {
			fmt.Printf("%s Some agents in %s failed to stop:\n", style.Warning.Render("⚠"), rigName)
			for _, e := range errors {
				fmt.Printf("  - %s\n", e)
			}
			failed = append(failed, rigName)
		} else {
			fmt.Printf("%s Warband %s stopped\n", style.Success.Render("✓"), rigName)
			succeeded = append(succeeded, rigName)
		}
	}

	// Summary
	if len(args) > 1 {
		fmt.Println()
		if len(succeeded) > 0 {
			fmt.Printf("%s Stopped: %s\n", style.Success.Render("✓"), strings.Join(succeeded, ", "))
		}
		if len(failed) > 0 {
			fmt.Printf("%s Failed: %s\n", style.Warning.Render("⚠"), strings.Join(failed, ", "))
			fmt.Printf("\nUse %s to force shutdown (DANGER: will lose work!)\n", style.Bold.Render("--nuclear"))
			return fmt.Errorf("some warbands failed to stop")
		}
	} else if len(failed) > 0 {
		fmt.Printf("\nUse %s to force shutdown (DANGER: will lose work!)\n", style.Bold.Render("--nuclear"))
		return fmt.Errorf("warband failed to stop")
	}

	return nil
}

func runRigRestart(cmd *cobra.Command, args []string) error {
	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	t := tmux.NewTmux()

	// Track results
	var succeeded []string
	var failed []string

	// Process each warband
	for _, rigName := range args {
		r, err := rigMgr.GetRig(rigName)
		if err != nil {
			fmt.Printf("%s Warband '%s' not found\n", style.Warning.Render("⚠"), rigName)
			failed = append(failed, rigName)
			continue
		}

		fmt.Printf("Restarting warband %s...\n", style.Bold.Render(rigName))

		// Check all raiders for uncommitted work (unless nuclear)
		if !rigRestartNuclear {
			raiderGit := git.NewGit(r.Path)
			raiderMgr := raider.NewManager(r, raiderGit, nil) // nil tmux: just listing
			raiders, err := raiderMgr.List()
			if err == nil && len(raiders) > 0 {
				var problemRaiders []struct {
					name   string
					status *git.UncommittedWorkStatus
				}

				for _, p := range raiders {
					pGit := git.NewGit(p.ClonePath)
					status, err := pGit.CheckUncommittedWork()
					if err == nil && !status.Clean() {
						problemRaiders = append(problemRaiders, struct {
							name   string
							status *git.UncommittedWorkStatus
						}{p.Name, status})
					}
				}

				if len(problemRaiders) > 0 {
					fmt.Printf("\n%s Cannot restart %s - raiders have uncommitted work:\n", style.Warning.Render("⚠"), rigName)
					for _, pp := range problemRaiders {
						fmt.Printf("  %s: %s\n", style.Bold.Render(pp.name), pp.status.String())
					}
					failed = append(failed, rigName)
					continue
				}
			}
		}

		var stopErrors []string
		var startErrors []string

		// === STOP PHASE ===
		fmt.Printf("  Stopping...\n")

		// 1. Stop all raider sessions
		raiderMgr := raider.NewSessionManager(t, r)
		infos, err := raiderMgr.List()
		if err == nil && len(infos) > 0 {
			fmt.Printf("    Stopping %d raider session(s)...\n", len(infos))
			if err := raiderMgr.StopAll(rigRestartForce); err != nil {
				stopErrors = append(stopErrors, fmt.Sprintf("raider sessions: %v", err))
			}
		}

		// 2. Stop the forge
		refMgr := forge.NewManager(r)
		refStatus, err := refMgr.Status()
		if err == nil && refStatus.State == forge.StateRunning {
			fmt.Printf("    Stopping forge...\n")
			if err := refMgr.Stop(); err != nil {
				stopErrors = append(stopErrors, fmt.Sprintf("forge: %v", err))
			}
		}

		// 3. Stop the witness
		witMgr := witness.NewManager(r)
		witStatus, err := witMgr.Status()
		if err == nil && witStatus.State == witness.StateRunning {
			fmt.Printf("    Stopping witness...\n")
			if err := witMgr.Stop(); err != nil {
				stopErrors = append(stopErrors, fmt.Sprintf("witness: %v", err))
			}
		}

		if len(stopErrors) > 0 {
			fmt.Printf("  %s Stop errors:\n", style.Warning.Render("⚠"))
			for _, e := range stopErrors {
				fmt.Printf("    - %s\n", e)
			}
			failed = append(failed, rigName)
			continue
		}

		// === START PHASE ===
		fmt.Printf("  Starting...\n")

		var started []string
		var skipped []string

		// 1. Start the witness
		witnessSession := fmt.Sprintf("hd-%s-witness", rigName)
		witnessRunning, _ := t.HasSession(witnessSession)
		if witnessRunning {
			skipped = append(skipped, "witness")
		} else {
			fmt.Printf("    Starting witness...\n")
			if err := witMgr.Start(false, "", nil); err != nil {
				if err == witness.ErrAlreadyRunning {
					skipped = append(skipped, "witness")
				} else {
					fmt.Printf("    %s Failed to start witness: %v\n", style.Warning.Render("⚠"), err)
					startErrors = append(startErrors, fmt.Sprintf("witness: %v", err))
				}
			} else {
				started = append(started, "witness")
			}
		}

		// 2. Start the forge
		forgeSession := fmt.Sprintf("hd-%s-forge", rigName)
		forgeRunning, _ := t.HasSession(forgeSession)
		if forgeRunning {
			skipped = append(skipped, "forge")
		} else {
			fmt.Printf("    Starting forge...\n")
			if err := refMgr.Start(false, ""); err != nil {
				fmt.Printf("    %s Failed to start forge: %v\n", style.Warning.Render("⚠"), err)
				startErrors = append(startErrors, fmt.Sprintf("forge: %v", err))
			} else {
				started = append(started, "forge")
			}
		}

		// Report results for this warband
		if len(started) > 0 {
			fmt.Printf("  %s Started: %s\n", style.Success.Render("✓"), strings.Join(started, ", "))
		}
		if len(skipped) > 0 {
			fmt.Printf("  %s Skipped: %s (already running)\n", style.Dim.Render("•"), strings.Join(skipped, ", "))
		}

		if len(startErrors) > 0 {
			fmt.Printf("  %s Start errors:\n", style.Warning.Render("⚠"))
			for _, e := range startErrors {
				fmt.Printf("    - %s\n", e)
			}
			failed = append(failed, rigName)
		} else {
			fmt.Printf("%s Warband %s restarted\n", style.Success.Render("✓"), rigName)
			succeeded = append(succeeded, rigName)
		}
		fmt.Println()
	}

	// Summary
	if len(args) > 1 {
		if len(succeeded) > 0 {
			fmt.Printf("%s Restarted: %s\n", style.Success.Render("✓"), strings.Join(succeeded, ", "))
		}
		if len(failed) > 0 {
			fmt.Printf("%s Failed: %s\n", style.Warning.Render("⚠"), strings.Join(failed, ", "))
			fmt.Printf("\nUse %s to force shutdown (DANGER: will lose work!)\n", style.Bold.Render("--nuclear"))
			return fmt.Errorf("some warbands failed to restart")
		}
	} else if len(failed) > 0 {
		fmt.Printf("\nUse %s to force shutdown (DANGER: will lose work!)\n", style.Bold.Render("--nuclear"))
		return fmt.Errorf("warband failed to restart")
	}

	return nil
}

// getRigOperationalState returns the operational state and source for a warband.
// It checks the wisp layer first (local/ephemeral), then warband bead labels (global).
// Returns state ("OPERATIONAL", "PARKED", or "DOCKED") and source ("local", "global - synced", or "default").
func getRigOperationalState(townRoot, rigName string) (state string, source string) {
	// Check wisp layer first (local/ephemeral overrides)
	wispConfig := wisp.NewConfig(townRoot, rigName)
	if status := wispConfig.GetString("status"); status != "" {
		switch strings.ToLower(status) {
		case "parked":
			return "PARKED", "local"
		case "docked":
			return "DOCKED", "local"
		}
	}

	// Check warband bead labels (global/synced)
	// Warband identity bead ID: <prefix>-warband-<name>
	// Look for status:docked or status:parked labels
	rigPath := filepath.Join(townRoot, rigName)
	rigRelicsDir := relics.ResolveRelicsDir(rigPath)
	bd := relics.NewWithRelicsDir(rigPath, rigRelicsDir)

	// Try to find the warband identity bead
	// Convention: <prefix>-warband-<rigName>
	if rigCfg, err := warband.LoadRigConfig(rigPath); err == nil && rigCfg.Relics != nil {
		rigBeadID := fmt.Sprintf("%s-warband-%s", rigCfg.Relics.Prefix, rigName)
		if issue, err := bd.Show(rigBeadID); err == nil {
			for _, label := range issue.Labels {
				if strings.HasPrefix(label, "status:") {
					statusValue := strings.TrimPrefix(label, "status:")
					switch strings.ToLower(statusValue) {
					case "docked":
						return "DOCKED", "global - synced"
					case "parked":
						return "PARKED", "global - synced"
					}
				}
			}
		}
	}

	// Default: operational
	return "OPERATIONAL", "default"
}
