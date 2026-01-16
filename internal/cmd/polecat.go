package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/runtime"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
)

// Raider command flags
var (
	raiderListJSON  bool
	raiderListAll   bool
	raiderForce     bool
	raiderRemoveAll bool
)

var raiderCmd = &cobra.Command{
	Use:     "raider",
	Aliases: []string{"raiders"},
	GroupID: GroupAgents,
	Short:   "Manage raiders in warbands",
	RunE:    requireSubcommand,
	Long: `Manage raider lifecycle in warbands.

Raiders are worker agents that operate in their own git worktrees.
Use the subcommands to add, remove, list, wake, and sleep raiders.`,
}

var raiderListCmd = &cobra.Command{
	Use:   "list [warband]",
	Short: "List raiders in a warband",
	Long: `List raiders in a warband or all warbands.

In the transient model, raiders exist only while working. The list shows
all currently active raiders with their states:
  - working: Actively working on an issue
  - done: Completed work, waiting for cleanup
  - stuck: Needs assistance

Examples:
  hd raider list greenplace
  hd raider list --all
  hd raider list greenplace --json`,
	RunE: runRaiderList,
}

var raiderAddCmd = &cobra.Command{
	Use:        "add <warband> <name>",
	Short:      "Add a new raider to a warband (DEPRECATED)",
	Deprecated: "use 'hd raider identity add' instead. This command will be removed in v1.0.",
	Long: `Add a new raider to a warband.

DEPRECATED: Use 'hd raider identity add' instead. This command will be removed in v1.0.

Creates a raider directory, clones the warband repo, creates a work branch,
and initializes state.

Example:
  hd raider identity add greenplace Toast  # Preferred
  hd raider add greenplace Toast           # Deprecated`,
	Args: cobra.ExactArgs(2),
	RunE: runRaiderAdd,
}

var raiderRemoveCmd = &cobra.Command{
	Use:   "remove <warband>/<raider>... | <warband> --all",
	Short: "Remove raiders from a warband",
	Long: `Remove one or more raiders from a warband.

Fails if session is running (stop first).
Warns if uncommitted changes exist.
Use --force to bypass checks.

Examples:
  hd raider remove greenplace/Toast
  hd raider remove greenplace/Toast greenplace/Furiosa
  hd raider remove greenplace --all
  hd raider remove greenplace --all --force`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRaiderRemove,
}

var raiderSyncCmd = &cobra.Command{
	Use:   "sync <warband>/<raider>",
	Short: "Sync relics for a raider",
	Long: `Sync relics for a raider's worktree.

Runs 'bd sync' in the raider's worktree to push local relics changes
to the shared sync branch and pull remote changes.

Use --all to sync all raiders in a warband.
Use --from-main to only pull (no push).

Examples:
  hd raider sync greenplace/Toast
  hd raider sync greenplace --all
  hd raider sync greenplace/Toast --from-main`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRaiderSync,
}

var raiderStatusCmd = &cobra.Command{
	Use:   "status <warband>/<raider>",
	Short: "Show detailed status for a raider",
	Long: `Show detailed status for a raider.

Displays comprehensive information including:
  - Current lifecycle state (working, done, stuck, idle)
  - Assigned issue (if any)
  - Session status (running/stopped, attached/detached)
  - Session creation time
  - Last activity time

Examples:
  hd raider status greenplace/Toast
  hd raider status greenplace/Toast --json`,
	Args: cobra.ExactArgs(1),
	RunE: runRaiderStatus,
}

var (
	raiderSyncAll           bool
	raiderSyncFromMain      bool
	raiderStatusJSON        bool
	raiderGitStateJSON      bool
	raiderGCDryRun          bool
	raiderNukeAll           bool
	raiderNukeDryRun        bool
	raiderNukeForce         bool
	raiderCheckRecoveryJSON bool
)

var raiderGCCmd = &cobra.Command{
	Use:   "gc <warband>",
	Short: "Garbage collect stale raider branches",
	Long: `Garbage collect stale raider branches in a warband.

Raiders use unique timestamped branches (raider/<name>-<timestamp>) to
prevent drift issues. Over time, these branches accumulate when stale
raiders are repaired.

This command removes orphaned branches:
  - Branches for raiders that no longer exist
  - Old timestamped branches (keeps only the current one per raider)

Examples:
  hd raider gc greenplace
  hd raider gc greenplace --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runRaiderGC,
}

var raiderNukeCmd = &cobra.Command{
	Use:   "nuke <warband>/<raider>... | <warband> --all",
	Short: "Completely destroy a raider (session, worktree, branch, agent bead)",
	Long: `Completely destroy a raider and all its artifacts.

This is the nuclear option for post-merge cleanup. It:
  1. Kills the Claude session (if running)
  2. Deletes the git worktree (bypassing all safety checks)
  3. Deletes the raider branch
  4. Closes the agent bead (if exists)

SAFETY CHECKS: The command refuses to nuke a raider if:
  - Worktree has unpushed/uncommitted changes
  - Raider has an open merge request (MR bead)
  - Raider has work on its hook

Use --force to bypass safety checks (LOSES WORK).
Use --dry-run to see what would happen and safety check status.

Examples:
  hd raider nuke greenplace/Toast
  hd raider nuke greenplace/Toast greenplace/Furiosa
  hd raider nuke greenplace --all
  hd raider nuke greenplace --all --dry-run
  hd raider nuke greenplace/Toast --force  # bypass safety checks`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRaiderNuke,
}

var raiderGitStateCmd = &cobra.Command{
	Use:   "git-state <warband>/<raider>",
	Short: "Show git state for pre-kill verification",
	Long: `Show git state for a raider's worktree.

Used by the Witness for pre-kill verification to ensure no work is lost.
Returns whether the worktree is clean (safe to kill) or dirty (needs cleanup).

Checks:
  - Working tree: uncommitted changes
  - Unpushed commits: commits ahead of origin/main
  - Stashes: stashed changes

Examples:
  hd raider git-state greenplace/Toast
  hd raider git-state greenplace/Toast --json`,
	Args: cobra.ExactArgs(1),
	RunE: runRaiderGitState,
}

var raiderCheckRecoveryCmd = &cobra.Command{
	Use:   "check-recovery <warband>/<raider>",
	Short: "Check if raider needs recovery vs safe to nuke",
	Long: `Check recovery status of a raider based on cleanup_status in agent bead.

Used by the Witness to determine appropriate cleanup action:
  - SAFE_TO_NUKE: cleanup_status is 'clean' - no work at risk
  - NEEDS_RECOVERY: cleanup_status indicates unpushed/uncommitted work

This prevents accidental data loss when cleaning up dormant raiders.
The Witness should escalate NEEDS_RECOVERY cases to the Warchief.

Examples:
  hd raider check-recovery greenplace/Toast
  hd raider check-recovery greenplace/Toast --json`,
	Args: cobra.ExactArgs(1),
	RunE: runRaiderCheckRecovery,
}

var (
	raiderStaleJSON      bool
	raiderStaleThreshold int
	raiderStaleCleanup   bool
)

var raiderStaleCmd = &cobra.Command{
	Use:   "stale <warband>",
	Short: "Detect stale raiders that may need cleanup",
	Long: `Detect stale raiders in a warband that are candidates for cleanup.

A raider is considered stale if:
  - No active tmux session
  - Way behind main (>threshold commits) OR no agent bead
  - Has no uncommitted work that could be lost

The default threshold is 20 commits behind main.

Use --cleanup to automatically nuke stale raiders that are safe to remove.
Use --dry-run with --cleanup to see what would be cleaned.

Examples:
  hd raider stale greenplace
  hd raider stale greenplace --threshold 50
  hd raider stale greenplace --json
  hd raider stale greenplace --cleanup
  hd raider stale greenplace --cleanup --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runRaiderStale,
}

func init() {
	// List flags
	raiderListCmd.Flags().BoolVar(&raiderListJSON, "json", false, "Output as JSON")
	raiderListCmd.Flags().BoolVar(&raiderListAll, "all", false, "List raiders in all warbands")

	// Remove flags
	raiderRemoveCmd.Flags().BoolVarP(&raiderForce, "force", "f", false, "Force removal, bypassing checks")
	raiderRemoveCmd.Flags().BoolVar(&raiderRemoveAll, "all", false, "Remove all raiders in the warband")

	// Sync flags
	raiderSyncCmd.Flags().BoolVar(&raiderSyncAll, "all", false, "Sync all raiders in the warband")
	raiderSyncCmd.Flags().BoolVar(&raiderSyncFromMain, "from-main", false, "Pull only, no push")

	// Status flags
	raiderStatusCmd.Flags().BoolVar(&raiderStatusJSON, "json", false, "Output as JSON")

	// Git-state flags
	raiderGitStateCmd.Flags().BoolVar(&raiderGitStateJSON, "json", false, "Output as JSON")

	// GC flags
	raiderGCCmd.Flags().BoolVar(&raiderGCDryRun, "dry-run", false, "Show what would be deleted without deleting")

	// Nuke flags
	raiderNukeCmd.Flags().BoolVar(&raiderNukeAll, "all", false, "Nuke all raiders in the warband")
	raiderNukeCmd.Flags().BoolVar(&raiderNukeDryRun, "dry-run", false, "Show what would be nuked without doing it")
	raiderNukeCmd.Flags().BoolVarP(&raiderNukeForce, "force", "f", false, "Force nuke, bypassing all safety checks (LOSES WORK)")

	// Check-recovery flags
	raiderCheckRecoveryCmd.Flags().BoolVar(&raiderCheckRecoveryJSON, "json", false, "Output as JSON")

	// Stale flags
	raiderStaleCmd.Flags().BoolVar(&raiderStaleJSON, "json", false, "Output as JSON")
	raiderStaleCmd.Flags().IntVar(&raiderStaleThreshold, "threshold", 20, "Commits behind main to consider stale")
	raiderStaleCmd.Flags().BoolVar(&raiderStaleCleanup, "cleanup", false, "Automatically nuke stale raiders")

	// Add subcommands
	raiderCmd.AddCommand(raiderListCmd)
	raiderCmd.AddCommand(raiderAddCmd)
	raiderCmd.AddCommand(raiderRemoveCmd)
	raiderCmd.AddCommand(raiderSyncCmd)
	raiderCmd.AddCommand(raiderStatusCmd)
	raiderCmd.AddCommand(raiderGitStateCmd)
	raiderCmd.AddCommand(raiderCheckRecoveryCmd)
	raiderCmd.AddCommand(raiderGCCmd)
	raiderCmd.AddCommand(raiderNukeCmd)
	raiderCmd.AddCommand(raiderStaleCmd)

	rootCmd.AddCommand(raiderCmd)
}

// RaiderListItem represents a raider in list output.
type RaiderListItem struct {
	Warband            string        `json:"warband"`
	Name           string        `json:"name"`
	State          raider.State `json:"state"`
	Issue          string        `json:"issue,omitempty"`
	SessionRunning bool          `json:"session_running"`
}

// getRaiderManager creates a raider manager for the given warband.
func getRaiderManager(rigName string) (*raider.Manager, *warband.Warband, error) {
	_, r, err := getRig(rigName)
	if err != nil {
		return nil, nil, err
	}

	raiderGit := git.NewGit(r.Path)
	t := tmux.NewTmux()
	mgr := raider.NewManager(r, raiderGit, t)

	return mgr, r, nil
}

func runRaiderList(cmd *cobra.Command, args []string) error {
	var warbands []*warband.Warband

	if raiderListAll {
		// List all warbands
		allRigs, _, err := getAllRigs()
		if err != nil {
			return err
		}
		warbands = allRigs
	} else {
		// Need a warband name
		if len(args) < 1 {
			return fmt.Errorf("warband name required (or use --all)")
		}
		_, r, err := getRaiderManager(args[0])
		if err != nil {
			return err
		}
		warbands = []*warband.Warband{r}
	}

	// Collect raiders from all warbands
	t := tmux.NewTmux()
	var allRaiders []RaiderListItem

	for _, r := range warbands {
		raiderGit := git.NewGit(r.Path)
		mgr := raider.NewManager(r, raiderGit, t)
		raiderMgr := raider.NewSessionManager(t, r)

		raiders, err := mgr.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to list raiders in %s: %v\n", r.Name, err)
			continue
		}

		for _, p := range raiders {
			running, _ := raiderMgr.IsRunning(p.Name)
			allRaiders = append(allRaiders, RaiderListItem{
				Warband:            r.Name,
				Name:           p.Name,
				State:          p.State,
				Issue:          p.Issue,
				SessionRunning: running,
			})
		}
	}

	// Output
	if raiderListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(allRaiders)
	}

	if len(allRaiders) == 0 {
		fmt.Println("No active raiders found.")
		return nil
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Active Raiders"))
	for _, p := range allRaiders {
		// Session indicator
		sessionStatus := style.Dim.Render("○")
		if p.SessionRunning {
			sessionStatus = style.Success.Render("●")
		}

		// Display actual state (no normalization - idle means idle)
		displayState := p.State

		// State color
		stateStr := string(displayState)
		switch displayState {
		case raider.StateWorking:
			stateStr = style.Info.Render(stateStr)
		case raider.StateStuck:
			stateStr = style.Warning.Render(stateStr)
		case raider.StateDone:
			stateStr = style.Success.Render(stateStr)
		default:
			stateStr = style.Dim.Render(stateStr)
		}

		fmt.Printf("  %s %s/%s  %s\n", sessionStatus, p.Warband, p.Name, stateStr)
		if p.Issue != "" {
			fmt.Printf("    %s\n", style.Dim.Render(p.Issue))
		}
	}

	return nil
}

func runRaiderAdd(cmd *cobra.Command, args []string) error {
	// Emit deprecation warning
	fmt.Fprintf(os.Stderr, "%s 'hd raider add' is deprecated. Use 'hd raider identity add' instead.\n",
		style.Warning.Render("Warning:"))
	fmt.Fprintf(os.Stderr, "         This command will be removed in v1.0.\n\n")

	rigName := args[0]
	raiderName := args[1]

	mgr, _, err := getRaiderManager(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Adding raider %s to warband %s...\n", raiderName, rigName)

	p, err := mgr.Add(raiderName)
	if err != nil {
		return fmt.Errorf("adding raider: %w", err)
	}

	fmt.Printf("%s Raider %s added.\n", style.SuccessPrefix, p.Name)
	fmt.Printf("  %s\n", style.Dim.Render(p.ClonePath))
	fmt.Printf("  Branch: %s\n", style.Dim.Render(p.Branch))

	return nil
}

func runRaiderRemove(cmd *cobra.Command, args []string) error {
	targets, err := resolveRaiderTargets(args, raiderRemoveAll)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		fmt.Println("No raiders to remove.")
		return nil
	}

	// Remove each raider
	t := tmux.NewTmux()
	var removeErrors []string
	removed := 0

	for _, p := range targets {
		// Check if session is running
		if !raiderForce {
			raiderMgr := raider.NewSessionManager(t, p.r)
			running, _ := raiderMgr.IsRunning(p.raiderName)
			if running {
				removeErrors = append(removeErrors, fmt.Sprintf("%s/%s: session is running (stop first or use --force)", p.rigName, p.raiderName))
				continue
			}
		}

		fmt.Printf("Removing raider %s/%s...\n", p.rigName, p.raiderName)

		if err := p.mgr.Remove(p.raiderName, raiderForce); err != nil {
			if errors.Is(err, raider.ErrHasChanges) {
				removeErrors = append(removeErrors, fmt.Sprintf("%s/%s: has uncommitted changes (use --force)", p.rigName, p.raiderName))
			} else {
				removeErrors = append(removeErrors, fmt.Sprintf("%s/%s: %v", p.rigName, p.raiderName, err))
			}
			continue
		}

		fmt.Printf("  %s removed\n", style.Success.Render("✓"))
		removed++
	}

	// Report results
	if len(removeErrors) > 0 {
		fmt.Printf("\n%s Some removals failed:\n", style.Warning.Render("Warning:"))
		for _, e := range removeErrors {
			fmt.Printf("  - %s\n", e)
		}
	}

	if removed > 0 {
		fmt.Printf("\n%s Removed %d raider(s).\n", style.SuccessPrefix, removed)
	}

	if len(removeErrors) > 0 {
		return fmt.Errorf("%d removal(s) failed", len(removeErrors))
	}

	return nil
}

func runRaiderSync(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("warband or warband/raider address required")
	}

	// Parse address - could be "warband" or "warband/raider"
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		// Might just be a warband name
		rigName = args[0]
		raiderName = ""
	}

	mgr, _, err := getRaiderManager(rigName)
	if err != nil {
		return err
	}

	// Get list of raiders to sync
	var raidersToSync []string
	if raiderSyncAll || raiderName == "" {
		raiders, err := mgr.List()
		if err != nil {
			return fmt.Errorf("listing raiders: %w", err)
		}
		for _, p := range raiders {
			raidersToSync = append(raidersToSync, p.Name)
		}
	} else {
		raidersToSync = []string{raiderName}
	}

	if len(raidersToSync) == 0 {
		fmt.Println("No raiders to sync.")
		return nil
	}

	// Sync each raider
	var syncErrors []string
	for _, name := range raidersToSync {
		// Get raider to get correct clone path (handles old vs new structure)
		p, err := mgr.Get(name)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", name, err))
			continue
		}

		// Check directory exists
		if _, err := os.Stat(p.ClonePath); os.IsNotExist(err) {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: directory not found", name))
			continue
		}

		// Build sync command
		syncArgs := []string{"sync"}
		if raiderSyncFromMain {
			syncArgs = append(syncArgs, "--from-main")
		}

		fmt.Printf("Syncing %s/%s...\n", rigName, name)

		syncCmd := exec.Command("rl", syncArgs...)
		syncCmd.Dir = p.ClonePath
		output, err := syncCmd.CombinedOutput()
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", name, err))
			if len(output) > 0 {
				fmt.Printf("  %s\n", style.Dim.Render(string(output)))
			}
		} else {
			fmt.Printf("  %s\n", style.Success.Render("✓ synced"))
		}
	}

	if len(syncErrors) > 0 {
		fmt.Printf("\n%s Some syncs failed:\n", style.Warning.Render("Warning:"))
		for _, e := range syncErrors {
			fmt.Printf("  - %s\n", e)
		}
		return fmt.Errorf("%d sync(s) failed", len(syncErrors))
	}

	return nil
}

// RaiderStatus represents detailed raider status for JSON output.
type RaiderStatus struct {
	Warband            string        `json:"warband"`
	Name           string        `json:"name"`
	State          raider.State `json:"state"`
	Issue          string        `json:"issue,omitempty"`
	ClonePath      string        `json:"clone_path"`
	Branch         string        `json:"branch"`
	SessionRunning bool          `json:"session_running"`
	SessionID      string        `json:"session_id,omitempty"`
	Attached       bool          `json:"attached,omitempty"`
	Windows        int           `json:"windows,omitempty"`
	CreatedAt      string        `json:"created_at,omitempty"`
	LastActivity   string        `json:"last_activity,omitempty"`
}

func runRaiderStatus(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	mgr, r, err := getRaiderManager(rigName)
	if err != nil {
		return err
	}

	// Get raider info
	p, err := mgr.Get(raiderName)
	if err != nil {
		return fmt.Errorf("raider '%s' not found in warband '%s'", raiderName, rigName)
	}

	// Get session info
	t := tmux.NewTmux()
	raiderMgr := raider.NewSessionManager(t, r)
	sessInfo, err := raiderMgr.Status(raiderName)
	if err != nil {
		// Non-fatal - continue without session info
		sessInfo = &raider.SessionInfo{
			Raider: raiderName,
			Running: false,
		}
	}

	// JSON output
	if raiderStatusJSON {
		status := RaiderStatus{
			Warband:            rigName,
			Name:           raiderName,
			State:          p.State,
			Issue:          p.Issue,
			ClonePath:      p.ClonePath,
			Branch:         p.Branch,
			SessionRunning: sessInfo.Running,
			SessionID:      sessInfo.SessionID,
			Attached:       sessInfo.Attached,
			Windows:        sessInfo.Windows,
		}
		if !sessInfo.Created.IsZero() {
			status.CreatedAt = sessInfo.Created.Format("2006-01-02 15:04:05")
		}
		if !sessInfo.LastActivity.IsZero() {
			status.LastActivity = sessInfo.LastActivity.Format("2006-01-02 15:04:05")
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	// Human-readable output
	fmt.Printf("%s\n\n", style.Bold.Render(fmt.Sprintf("Raider: %s/%s", rigName, raiderName)))

	// State with color
	stateStr := string(p.State)
	switch p.State {
	case raider.StateWorking:
		stateStr = style.Info.Render(stateStr)
	case raider.StateStuck:
		stateStr = style.Warning.Render(stateStr)
	case raider.StateDone:
		stateStr = style.Success.Render(stateStr)
	default:
		stateStr = style.Dim.Render(stateStr)
	}
	fmt.Printf("  State:         %s\n", stateStr)

	// Issue
	if p.Issue != "" {
		fmt.Printf("  Issue:         %s\n", p.Issue)
	} else {
		fmt.Printf("  Issue:         %s\n", style.Dim.Render("(none)"))
	}

	// Clone path and branch
	fmt.Printf("  Clone:         %s\n", style.Dim.Render(p.ClonePath))
	fmt.Printf("  Branch:        %s\n", style.Dim.Render(p.Branch))

	// Session info
	fmt.Println()
	fmt.Printf("%s\n", style.Bold.Render("Session"))

	if sessInfo.Running {
		fmt.Printf("  Status:        %s\n", style.Success.Render("running"))
		fmt.Printf("  Session ID:    %s\n", style.Dim.Render(sessInfo.SessionID))

		if sessInfo.Attached {
			fmt.Printf("  Attached:      %s\n", style.Info.Render("yes"))
		} else {
			fmt.Printf("  Attached:      %s\n", style.Dim.Render("no"))
		}

		if sessInfo.Windows > 0 {
			fmt.Printf("  Windows:       %d\n", sessInfo.Windows)
		}

		if !sessInfo.Created.IsZero() {
			fmt.Printf("  Created:       %s\n", sessInfo.Created.Format("2006-01-02 15:04:05"))
		}

		if !sessInfo.LastActivity.IsZero() {
			// Show relative time for activity
			ago := formatActivityTime(sessInfo.LastActivity)
			fmt.Printf("  Last Activity: %s (%s)\n",
				sessInfo.LastActivity.Format("15:04:05"),
				style.Dim.Render(ago))
		}
	} else {
		fmt.Printf("  Status:        %s\n", style.Dim.Render("not running"))
	}

	return nil
}

// formatActivityTime returns a human-readable relative time string.
func formatActivityTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

// GitState represents the git state of a raider's worktree.
type GitState struct {
	Clean            bool     `json:"clean"`
	UncommittedFiles []string `json:"uncommitted_files"`
	UnpushedCommits  int      `json:"unpushed_commits"`
	StashCount       int      `json:"stash_count"`
}

func runRaiderGitState(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	mgr, r, err := getRaiderManager(rigName)
	if err != nil {
		return err
	}

	// Verify raider exists
	p, err := mgr.Get(raiderName)
	if err != nil {
		return fmt.Errorf("raider '%s' not found in warband '%s'", raiderName, rigName)
	}

	// Get git state from the raider's worktree
	state, err := getGitState(p.ClonePath)
	if err != nil {
		return fmt.Errorf("getting git state: %w", err)
	}

	// JSON output
	if raiderGitStateJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(state)
	}

	// Human-readable output
	fmt.Printf("%s\n\n", style.Bold.Render(fmt.Sprintf("Git State: %s/%s", r.Name, raiderName)))

	// Working tree status
	if len(state.UncommittedFiles) == 0 {
		fmt.Printf("  Working Tree:  %s\n", style.Success.Render("clean"))
	} else {
		fmt.Printf("  Working Tree:  %s\n", style.Warning.Render("dirty"))
		fmt.Printf("  Uncommitted:   %s\n", style.Warning.Render(fmt.Sprintf("%d files", len(state.UncommittedFiles))))
		for _, f := range state.UncommittedFiles {
			fmt.Printf("                 %s\n", style.Dim.Render(f))
		}
	}

	// Unpushed commits
	if state.UnpushedCommits == 0 {
		fmt.Printf("  Unpushed:      %s\n", style.Success.Render("0 commits"))
	} else {
		fmt.Printf("  Unpushed:      %s\n", style.Warning.Render(fmt.Sprintf("%d commits ahead", state.UnpushedCommits)))
	}

	// Stashes
	if state.StashCount == 0 {
		fmt.Printf("  Stashes:       %s\n", style.Dim.Render("0"))
	} else {
		fmt.Printf("  Stashes:       %s\n", style.Warning.Render(fmt.Sprintf("%d", state.StashCount)))
	}

	// Verdict
	fmt.Println()
	if state.Clean {
		fmt.Printf("  Verdict:       %s\n", style.Success.Render("CLEAN (safe to kill)"))
	} else {
		fmt.Printf("  Verdict:       %s\n", style.Error.Render("DIRTY (needs cleanup)"))
	}

	return nil
}

// getGitState checks the git state of a worktree.
func getGitState(worktreePath string) (*GitState, error) {
	state := &GitState{
		Clean:            true,
		UncommittedFiles: []string{},
	}

	// Check for uncommitted changes (git status --porcelain)
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = worktreePath
	output, err := statusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	if len(output) > 0 {
		lines := splitLines(string(output))
		for _, line := range lines {
			if line != "" {
				// Extract filename (skip the status prefix)
				if len(line) > 3 {
					state.UncommittedFiles = append(state.UncommittedFiles, line[3:])
				} else {
					state.UncommittedFiles = append(state.UncommittedFiles, line)
				}
			}
		}
		state.Clean = false
	}

	// Check for unpushed commits (git log origin/main..HEAD)
	// We check commits first, then verify if content differs.
	// After squash merge, commits may differ but content may be identical.
	mainRef := "origin/main"
	logCmd := exec.Command("git", "log", mainRef+"..HEAD", "--oneline")
	logCmd.Dir = worktreePath
	output, err = logCmd.Output()
	if err != nil {
		// origin/main might not exist - try origin/master
		mainRef = "origin/master"
		logCmd = exec.Command("git", "log", mainRef+"..HEAD", "--oneline")
		logCmd.Dir = worktreePath
		output, _ = logCmd.Output() // non-fatal: might be a new repo without remote tracking
	}
	if len(output) > 0 {
		lines := splitLines(string(output))
		count := 0
		for _, line := range lines {
			if line != "" {
				count++
			}
		}
		if count > 0 {
			// Commits exist that aren't on main. But after squash merge,
			// the content may actually be on main with different commit SHAs.
			// Check if there's any actual diff between HEAD and main.
			diffCmd := exec.Command("git", "diff", mainRef, "HEAD", "--quiet")
			diffCmd.Dir = worktreePath
			diffErr := diffCmd.Run()
			if diffErr == nil {
				// Exit code 0 means no diff - content IS on main (squash merged)
				// Don't count these as unpushed
				state.UnpushedCommits = 0
			} else {
				// Exit code 1 means there's a diff - truly unpushed work
				state.UnpushedCommits = count
				state.Clean = false
			}
		}
	}

	// Check for stashes (git stash list)
	stashCmd := exec.Command("git", "stash", "list")
	stashCmd.Dir = worktreePath
	output, err = stashCmd.Output()
	if err != nil {
		// Ignore stash errors
		output = nil
	}
	if len(output) > 0 {
		lines := splitLines(string(output))
		count := 0
		for _, line := range lines {
			if line != "" {
				count++
			}
		}
		state.StashCount = count
		if count > 0 {
			state.Clean = false
		}
	}

	return state, nil
}

// RecoveryStatus represents whether a raider needs recovery or is safe to nuke.
type RecoveryStatus struct {
	Warband           string                `json:"warband"`
	Raider       string                `json:"raider"`
	CleanupStatus raider.CleanupStatus `json:"cleanup_status"`
	NeedsRecovery bool                  `json:"needs_recovery"`
	Verdict       string                `json:"verdict"` // SAFE_TO_NUKE or NEEDS_RECOVERY
	Branch        string                `json:"branch,omitempty"`
	Issue         string                `json:"issue,omitempty"`
}

func runRaiderCheckRecovery(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	mgr, r, err := getRaiderManager(rigName)
	if err != nil {
		return err
	}

	// Verify raider exists and get info
	p, err := mgr.Get(raiderName)
	if err != nil {
		return fmt.Errorf("raider '%s' not found in warband '%s'", raiderName, rigName)
	}

	// Get cleanup_status from agent bead
	// We need to read it directly from relics since manager doesn't expose it
	rigPath := r.Path
	bd := relics.New(rigPath)
	agentBeadID := relics.RaiderBeadID(rigName, raiderName)
	_, fields, err := bd.GetAgentBead(agentBeadID)

	status := RecoveryStatus{
		Warband:     rigName,
		Raider: raiderName,
		Branch:  p.Branch,
		Issue:   p.Issue,
	}

	if err != nil || fields == nil {
		// No agent bead or no cleanup_status - fall back to git check
		// This handles raiders that haven't self-reported yet
		gitState, gitErr := getGitState(p.ClonePath)
		if gitErr != nil {
			status.CleanupStatus = raider.CleanupUnknown
			status.NeedsRecovery = true
			status.Verdict = "NEEDS_RECOVERY"
		} else if gitState.Clean {
			status.CleanupStatus = raider.CleanupClean
			status.NeedsRecovery = false
			status.Verdict = "SAFE_TO_NUKE"
		} else if gitState.UnpushedCommits > 0 {
			status.CleanupStatus = raider.CleanupUnpushed
			status.NeedsRecovery = true
			status.Verdict = "NEEDS_RECOVERY"
		} else if gitState.StashCount > 0 {
			status.CleanupStatus = raider.CleanupStash
			status.NeedsRecovery = true
			status.Verdict = "NEEDS_RECOVERY"
		} else {
			status.CleanupStatus = raider.CleanupUncommitted
			status.NeedsRecovery = true
			status.Verdict = "NEEDS_RECOVERY"
		}
	} else {
		// Use cleanup_status from agent bead
		status.CleanupStatus = raider.CleanupStatus(fields.CleanupStatus)
		if status.CleanupStatus.IsSafe() {
			status.NeedsRecovery = false
			status.Verdict = "SAFE_TO_NUKE"
		} else {
			// RequiresRecovery covers uncommitted, stash, unpushed
			// Unknown/empty also treated conservatively
			status.NeedsRecovery = true
			status.Verdict = "NEEDS_RECOVERY"
		}
	}

	// JSON output
	if raiderCheckRecoveryJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	// Human-readable output
	fmt.Printf("%s\n\n", style.Bold.Render(fmt.Sprintf("Recovery Status: %s/%s", rigName, raiderName)))
	fmt.Printf("  Cleanup Status:  %s\n", status.CleanupStatus)
	if status.Branch != "" {
		fmt.Printf("  Branch:          %s\n", status.Branch)
	}
	if status.Issue != "" {
		fmt.Printf("  Issue:           %s\n", status.Issue)
	}
	fmt.Println()

	if status.NeedsRecovery {
		fmt.Printf("  Verdict:         %s\n", style.Error.Render("NEEDS_RECOVERY"))
		fmt.Println()
		fmt.Printf("  %s This raider has unpushed/uncommitted work.\n", style.Warning.Render("⚠"))
		fmt.Println("  Escalate to Warchief for recovery before cleanup.")
	} else {
		fmt.Printf("  Verdict:         %s\n", style.Success.Render("SAFE_TO_NUKE"))
		fmt.Println()
		fmt.Printf("  %s Safe to nuke - no work at risk.\n", style.Success.Render("✓"))
	}

	return nil
}

func runRaiderGC(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	mgr, r, err := getRaiderManager(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Garbage collecting stale raider branches in %s...\n\n", r.Name)

	if raiderGCDryRun {
		// Dry run - list branches that would be deleted
		repoGit := git.NewGit(r.Path)

		// List all raider branches
		branches, err := repoGit.ListBranches("raider/*")
		if err != nil {
			return fmt.Errorf("listing branches: %w", err)
		}

		if len(branches) == 0 {
			fmt.Println("No raider branches found.")
			return nil
		}

		// Get current branches
		raiders, err := mgr.List()
		if err != nil {
			return fmt.Errorf("listing raiders: %w", err)
		}

		currentBranches := make(map[string]bool)
		for _, p := range raiders {
			currentBranches[p.Branch] = true
		}

		// Show what would be deleted
		toDelete := 0
		for _, branch := range branches {
			if !currentBranches[branch] {
				fmt.Printf("  Would delete: %s\n", style.Dim.Render(branch))
				toDelete++
			} else {
				fmt.Printf("  Keep (in use): %s\n", style.Success.Render(branch))
			}
		}

		fmt.Printf("\nWould delete %d branch(es), keep %d\n", toDelete, len(branches)-toDelete)
		return nil
	}

	// Actually clean up
	deleted, err := mgr.CleanupStaleBranches()
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	if deleted == 0 {
		fmt.Println("No stale branches to clean up.")
	} else {
		fmt.Printf("%s Deleted %d stale branch(es).\n", style.SuccessPrefix, deleted)
	}

	return nil
}

// splitLines splits a string into non-empty lines.
func splitLines(s string) []string {
	var lines []string
	for _, line := range filepath.SplitList(s) {
		if line != "" {
			lines = append(lines, line)
		}
	}
	// filepath.SplitList doesn't work for newlines, use strings.Split instead
	lines = nil
	for _, line := range strings.Split(s, "\n") {
		lines = append(lines, line)
	}
	return lines
}

func runRaiderNuke(cmd *cobra.Command, args []string) error {
	targets, err := resolveRaiderTargets(args, raiderNukeAll)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		fmt.Println("No raiders to nuke.")
		return nil
	}

	// Safety checks: refuse to nuke raiders with active work unless --force is set
	if !raiderNukeForce && !raiderNukeDryRun {
		var blocked []*SafetyCheckResult
		for _, p := range targets {
			result := checkRaiderSafety(p)
			if result.Blocked {
				blocked = append(blocked, result)
			}
		}

		if len(blocked) > 0 {
			displaySafetyCheckBlocked(blocked)
			return fmt.Errorf("blocked: %d raider(s) have active work", len(blocked))
		}
	}

	// Nuke each raider
	t := tmux.NewTmux()
	var nukeErrors []string
	nuked := 0

	for _, p := range targets {
		if raiderNukeDryRun {
			fmt.Printf("Would nuke %s/%s:\n", p.rigName, p.raiderName)
			fmt.Printf("  - Kill session: gt-%s-%s\n", p.rigName, p.raiderName)
			fmt.Printf("  - Delete worktree: %s/raiders/%s\n", p.r.Path, p.raiderName)
			fmt.Printf("  - Delete branch (if exists)\n")
			fmt.Printf("  - Close agent bead: %s\n", relics.RaiderBeadID(p.rigName, p.raiderName))

			displayDryRunSafetyCheck(p)
			fmt.Println()
			continue
		}

		if raiderNukeForce {
			fmt.Printf("%s Nuking %s/%s (--force)...\n", style.Warning.Render("⚠"), p.rigName, p.raiderName)
		} else {
			fmt.Printf("Nuking %s/%s...\n", p.rigName, p.raiderName)
		}

		// Step 1: Kill session (force mode - no graceful shutdown)
		raiderMgr := raider.NewSessionManager(t, p.r)
		running, _ := raiderMgr.IsRunning(p.raiderName)
		if running {
			if err := raiderMgr.Stop(p.raiderName, true); err != nil {
				fmt.Printf("  %s session kill failed: %v\n", style.Warning.Render("⚠"), err)
				// Continue anyway - worktree removal will still work
			} else {
				fmt.Printf("  %s killed session\n", style.Success.Render("✓"))
			}
		}

		// Step 2: Get raider info before deletion (for branch name)
		raiderInfo, err := p.mgr.Get(p.raiderName)
		var branchToDelete string
		if err == nil && raiderInfo != nil {
			branchToDelete = raiderInfo.Branch
		}

		// Step 3: Delete worktree (nuclear mode - bypass all safety checks)
		if err := p.mgr.RemoveWithOptions(p.raiderName, true, true); err != nil {
			if errors.Is(err, raider.ErrRaiderNotFound) {
				fmt.Printf("  %s worktree already gone\n", style.Dim.Render("○"))
			} else {
				nukeErrors = append(nukeErrors, fmt.Sprintf("%s/%s: worktree removal failed: %v", p.rigName, p.raiderName, err))
				continue
			}
		} else {
			fmt.Printf("  %s deleted worktree\n", style.Success.Render("✓"))
		}

		// Step 4: Delete branch (if we know it)
		if branchToDelete != "" {
			repoGit := git.NewGit(filepath.Join(p.r.Path, "warchief", "warband"))
			if err := repoGit.DeleteBranch(branchToDelete, true); err != nil {
				// Non-fatal - branch might already be gone
				fmt.Printf("  %s branch delete: %v\n", style.Dim.Render("○"), err)
			} else {
				fmt.Printf("  %s deleted branch %s\n", style.Success.Render("✓"), branchToDelete)
			}
		}

		// Step 5: Close agent bead (if exists)
		agentBeadID := relics.RaiderBeadID(p.rigName, p.raiderName)
		closeArgs := []string{"close", agentBeadID, "--reason=nuked"}
		if sessionID := runtime.SessionIDFromEnv(); sessionID != "" {
			closeArgs = append(closeArgs, "--session="+sessionID)
		}
		closeCmd := exec.Command("rl", closeArgs...)
		closeCmd.Dir = filepath.Join(p.r.Path, "warchief", "warband")
		if err := closeCmd.Run(); err != nil {
			// Non-fatal - agent bead might not exist
			fmt.Printf("  %s agent bead not found or already closed\n", style.Dim.Render("○"))
		} else {
			fmt.Printf("  %s closed agent bead %s\n", style.Success.Render("✓"), agentBeadID)
		}

		nuked++
	}

	// Report results
	if raiderNukeDryRun {
		fmt.Printf("\n%s Would nuke %d raider(s).\n", style.Info.Render("ℹ"), len(targets))
		return nil
	}

	if len(nukeErrors) > 0 {
		fmt.Printf("\n%s Some nukes failed:\n", style.Warning.Render("Warning:"))
		for _, e := range nukeErrors {
			fmt.Printf("  - %s\n", e)
		}
	}

	if nuked > 0 {
		fmt.Printf("\n%s Nuked %d raider(s).\n", style.SuccessPrefix, nuked)
	}

	if len(nukeErrors) > 0 {
		return fmt.Errorf("%d nuke(s) failed", len(nukeErrors))
	}

	return nil
}

func runRaiderStale(cmd *cobra.Command, args []string) error {
	rigName := args[0]
	mgr, r, err := getRaiderManager(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Detecting stale raiders in %s (threshold: %d commits behind main)...\n\n", r.Name, raiderStaleThreshold)

	staleInfos, err := mgr.DetectStaleRaiders(raiderStaleThreshold)
	if err != nil {
		return fmt.Errorf("detecting stale raiders: %w", err)
	}

	if len(staleInfos) == 0 {
		fmt.Println("No raiders found.")
		return nil
	}

	// JSON output
	if raiderStaleJSON {
		return json.NewEncoder(os.Stdout).Encode(staleInfos)
	}

	// Summary counts
	var staleCount, safeCount int
	for _, info := range staleInfos {
		if info.IsStale {
			staleCount++
		} else {
			safeCount++
		}
	}

	// Display results
	for _, info := range staleInfos {
		statusIcon := style.Success.Render("●")
		statusText := "active"
		if info.IsStale {
			statusIcon = style.Warning.Render("○")
			statusText = "stale"
		}

		fmt.Printf("%s %s (%s)\n", statusIcon, style.Bold.Render(info.Name), statusText)

		// Session status
		if info.HasActiveSession {
			fmt.Printf("    Session: %s\n", style.Success.Render("running"))
		} else {
			fmt.Printf("    Session: %s\n", style.Dim.Render("stopped"))
		}

		// Commits behind
		if info.CommitsBehind > 0 {
			behindStyle := style.Dim
			if info.CommitsBehind >= raiderStaleThreshold {
				behindStyle = style.Warning
			}
			fmt.Printf("    Behind main: %s\n", behindStyle.Render(fmt.Sprintf("%d commits", info.CommitsBehind)))
		}

		// Agent state
		if info.AgentState != "" {
			fmt.Printf("    Agent state: %s\n", info.AgentState)
		} else {
			fmt.Printf("    Agent state: %s\n", style.Dim.Render("no bead"))
		}

		// Uncommitted work
		if info.HasUncommittedWork {
			fmt.Printf("    Uncommitted: %s\n", style.Error.Render("yes"))
		}

		// Reason
		fmt.Printf("    Reason: %s\n", info.Reason)
		fmt.Println()
	}

	// Summary
	fmt.Printf("Summary: %d stale, %d active\n", staleCount, safeCount)

	// Cleanup if requested
	if raiderStaleCleanup && staleCount > 0 {
		fmt.Println()
		if raiderNukeDryRun {
			fmt.Printf("Would clean up %d stale raider(s):\n", staleCount)
			for _, info := range staleInfos {
				if info.IsStale {
					fmt.Printf("  - %s: %s\n", info.Name, info.Reason)
				}
			}
		} else {
			fmt.Printf("Cleaning up %d stale raider(s)...\n", staleCount)
			nuked := 0
			for _, info := range staleInfos {
				if !info.IsStale {
					continue
				}
				fmt.Printf("  Nuking %s...", info.Name)
				if err := mgr.RemoveWithOptions(info.Name, true, false); err != nil {
					fmt.Printf(" %s (%v)\n", style.Error.Render("failed"), err)
				} else {
					fmt.Printf(" %s\n", style.Success.Render("done"))
					nuked++
				}
			}
			fmt.Printf("\n%s Nuked %d stale raider(s).\n", style.SuccessPrefix, nuked)
		}
	}

	return nil
}
