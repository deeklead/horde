package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/forge"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

// Forge command flags
var (
	forgeForeground    bool
	forgeStatusJSON    bool
	forgeQueueJSON     bool
	forgeAgentOverride string
)

var forgeCmd = &cobra.Command{
	Use:     "forge",
	Aliases: []string{"ref"},
	GroupID: GroupAgents,
	Short:   "Manage the merge queue processor",
	RunE:    requireSubcommand,
	Long: `Manage the Forge merge queue processor for a warband.

The Forge processes merge requests from raiders, merging their work
into integration branches and ultimately to main.`,
}

var forgeStartCmd = &cobra.Command{
	Use:     "start [warband]",
	Aliases: []string{"muster"},
	Short:   "Start the forge",
	Long: `Start the Forge for a warband.

Launches the merge queue processor which monitors for raider work branches
and merges them to the appropriate target branches.

If warband is not specified, infers it from the current directory.

Examples:
  hd forge start greenplace
  hd forge start greenplace --foreground
  hd forge start              # infer warband from cwd`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeStart,
}

var forgeStopCmd = &cobra.Command{
	Use:   "stop [warband]",
	Short: "Stop the forge",
	Long: `Stop a running Forge.

Gracefully stops the forge, completing any in-progress merge first.
If warband is not specified, infers it from the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeStop,
}

var forgeStatusCmd = &cobra.Command{
	Use:   "status [warband]",
	Short: "Show forge status",
	Long: `Show the status of a warband's Forge.

Displays running state, current work, queue length, and statistics.
If warband is not specified, infers it from the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeStatus,
}

var forgeQueueCmd = &cobra.Command{
	Use:   "queue [warband]",
	Short: "Show merge queue",
	Long: `Show the merge queue for a warband.

Lists all pending merge requests waiting to be processed.
If warband is not specified, infers it from the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeQueue,
}

var forgeAttachCmd = &cobra.Command{
	Use:   "summon [warband]",
	Short: "Summon to forge session",
	Long: `Summon to a running Forge's Claude session.

Allows interactive access to the Forge agent for debugging
or manual intervention.

If warband is not specified, infers it from the current directory.

Examples:
  hd forge summon greenplace
  hd forge summon          # infer warband from cwd`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeAttach,
}

var forgeRestartCmd = &cobra.Command{
	Use:   "restart [warband]",
	Short: "Restart the forge",
	Long: `Restart the Forge for a warband.

Stops the current session (if running) and starts a fresh one.
If warband is not specified, infers it from the current directory.

Examples:
  hd forge restart greenplace
  hd forge restart          # infer warband from cwd`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeRestart,
}

var forgeClaimCmd = &cobra.Command{
	Use:   "claim <mr-id>",
	Short: "Claim an MR for processing",
	Long: `Claim a merge request for processing by this forge worker.

When running multiple forge workers in parallel, each worker must claim
an MR before processing to prevent double-processing. Claims expire after
10 minutes if not processed (for crash recovery).

The worker ID is automatically determined from the HD_FORGE_WORKER
environment variable, or defaults to "forge-1".

Examples:
  hd forge claim gt-abc123
  HD_FORGE_WORKER=forge-2 hd forge claim gt-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runForgeClaim,
}

var forgeReleaseCmd = &cobra.Command{
	Use:   "release <mr-id>",
	Short: "Release a claimed MR back to the queue",
	Long: `Release a claimed merge request back to the queue.

Called when processing fails and the MR should be retried by another worker.
This clears the claim so other workers can pick up the MR.

Examples:
  hd forge release gt-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runForgeRelease,
}

var forgeUnclaimedCmd = &cobra.Command{
	Use:   "unclaimed [warband]",
	Short: "List unclaimed MRs available for processing",
	Long: `List merge requests that are available for claiming.

Shows MRs that are not currently claimed by any worker, or have stale
claims (worker may have crashed). Useful for parallel forge workers
to find work.

Examples:
  hd forge unclaimed
  hd forge unclaimed --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeUnclaimed,
}

var forgeUnclaimedJSON bool

var forgeReadyCmd = &cobra.Command{
	Use:   "ready [warband]",
	Short: "List MRs ready for processing (unclaimed and unblocked)",
	Long: `List merge requests ready for processing.

Shows MRs that are:
- Not currently claimed by any worker (or claim is stale)
- Not blocked by an open task (e.g., conflict resolution in progress)

This is the preferred command for finding work to process.

Examples:
  hd forge ready
  hd forge ready --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeReady,
}

var forgeReadyJSON bool

var forgeBlockedCmd = &cobra.Command{
	Use:   "blocked [warband]",
	Short: "List MRs blocked by open tasks",
	Long: `List merge requests blocked by open tasks.

Shows MRs waiting for conflict resolution or other blocking tasks to complete.
When the blocking task closes, the MR will appear in 'ready'.

Examples:
  hd forge blocked
  hd forge blocked --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runForgeBlocked,
}

var forgeBlockedJSON bool

func init() {
	// Start flags
	forgeStartCmd.Flags().BoolVar(&forgeForeground, "foreground", false, "Run in foreground (default: background)")
	forgeStartCmd.Flags().StringVar(&forgeAgentOverride, "agent", "", "Agent alias to run the Forge with (overrides encampment default)")

	// Summon flags
	forgeAttachCmd.Flags().StringVar(&forgeAgentOverride, "agent", "", "Agent alias to run the Forge with (overrides encampment default)")

	// Restart flags
	forgeRestartCmd.Flags().StringVar(&forgeAgentOverride, "agent", "", "Agent alias to run the Forge with (overrides encampment default)")

	// Status flags
	forgeStatusCmd.Flags().BoolVar(&forgeStatusJSON, "json", false, "Output as JSON")

	// Queue flags
	forgeQueueCmd.Flags().BoolVar(&forgeQueueJSON, "json", false, "Output as JSON")

	// Unclaimed flags
	forgeUnclaimedCmd.Flags().BoolVar(&forgeUnclaimedJSON, "json", false, "Output as JSON")

	// Ready flags
	forgeReadyCmd.Flags().BoolVar(&forgeReadyJSON, "json", false, "Output as JSON")

	// Blocked flags
	forgeBlockedCmd.Flags().BoolVar(&forgeBlockedJSON, "json", false, "Output as JSON")

	// Add subcommands
	forgeCmd.AddCommand(forgeStartCmd)
	forgeCmd.AddCommand(forgeStopCmd)
	forgeCmd.AddCommand(forgeRestartCmd)
	forgeCmd.AddCommand(forgeStatusCmd)
	forgeCmd.AddCommand(forgeQueueCmd)
	forgeCmd.AddCommand(forgeAttachCmd)
	forgeCmd.AddCommand(forgeClaimCmd)
	forgeCmd.AddCommand(forgeReleaseCmd)
	forgeCmd.AddCommand(forgeUnclaimedCmd)
	forgeCmd.AddCommand(forgeReadyCmd)
	forgeCmd.AddCommand(forgeBlockedCmd)

	rootCmd.AddCommand(forgeCmd)
}

// getForgeManager creates a forge manager for a warband.
// If rigName is empty, infers the warband from cwd.
func getForgeManager(rigName string) (*forge.Manager, *warband.Warband, string, error) {
	// Infer warband from cwd if not provided
	if rigName == "" {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return nil, nil, "", fmt.Errorf("not in a Horde workspace: %w", err)
		}
		rigName, err = inferRigFromCwd(townRoot)
		if err != nil {
			return nil, nil, "", fmt.Errorf("could not determine warband: %w\nUsage: hd forge <command> <warband>", err)
		}
	}

	_, r, err := getRig(rigName)
	if err != nil {
		return nil, nil, "", err
	}

	mgr := forge.NewManager(r)
	return mgr, r, rigName, nil
}

func runForgeStart(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Starting forge for %s...\n", rigName)

	if err := mgr.Start(forgeForeground, forgeAgentOverride); err != nil {
		if err == forge.ErrAlreadyRunning {
			fmt.Printf("%s Forge is already running\n", style.Dim.Render("⚠"))
			return nil
		}
		return fmt.Errorf("starting forge: %w", err)
	}

	if forgeForeground {
		// This will block until stopped
		return nil
	}

	fmt.Printf("%s Forge started for %s\n", style.Bold.Render("✓"), rigName)
	fmt.Printf("  %s\n", style.Dim.Render("Use 'hd forge status' to check progress"))
	return nil
}

func runForgeStop(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	if err := mgr.Stop(); err != nil {
		if err == forge.ErrNotRunning {
			fmt.Printf("%s Forge is not running\n", style.Dim.Render("⚠"))
			return nil
		}
		return fmt.Errorf("stopping forge: %w", err)
	}

	fmt.Printf("%s Forge stopped for %s\n", style.Bold.Render("✓"), rigName)
	return nil
}

func runForgeStatus(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	ref, err := mgr.Status()
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	// JSON output
	if forgeStatusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ref)
	}

	// Human-readable output
	fmt.Printf("%s Forge: %s\n\n", style.Bold.Render("⚙"), rigName)

	stateStr := string(ref.State)
	switch ref.State {
	case forge.StateRunning:
		stateStr = style.Bold.Render("● running")
	case forge.StateStopped:
		stateStr = style.Dim.Render("○ stopped")
	case forge.StatePaused:
		stateStr = style.Dim.Render("⏸ paused")
	}
	fmt.Printf("  State: %s\n", stateStr)

	if ref.StartedAt != nil {
		fmt.Printf("  Started: %s\n", ref.StartedAt.Format("2006-01-02 15:04:05"))
	}

	if ref.CurrentMR != nil {
		fmt.Printf("\n  %s\n", style.Bold.Render("Currently Processing:"))
		fmt.Printf("    Branch: %s\n", ref.CurrentMR.Branch)
		fmt.Printf("    Worker: %s\n", ref.CurrentMR.Worker)
		if ref.CurrentMR.IssueID != "" {
			fmt.Printf("    Issue:  %s\n", ref.CurrentMR.IssueID)
		}
	}

	// Get queue length
	queue, _ := mgr.Queue()
	pendingCount := 0
	for _, item := range queue {
		if item.Position > 0 { // Not currently processing
			pendingCount++
		}
	}
	fmt.Printf("\n  Queue: %d pending\n", pendingCount)

	if ref.LastMergeAt != nil {
		fmt.Printf("  Last merge: %s\n", ref.LastMergeAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

func runForgeQueue(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	queue, err := mgr.Queue()
	if err != nil {
		return fmt.Errorf("getting queue: %w", err)
	}

	// JSON output
	if forgeQueueJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(queue)
	}

	// Human-readable output
	fmt.Printf("%s Merge queue for '%s':\n\n", style.Bold.Render("📋"), rigName)

	if len(queue) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(empty)"))
		return nil
	}

	for _, item := range queue {
		status := ""
		prefix := fmt.Sprintf("  %d.", item.Position)

		if item.Position == 0 {
			prefix = "  ▶"
			status = style.Bold.Render("[processing]")
		} else {
			switch item.MR.Status {
			case forge.MROpen:
				if item.MR.Error != "" {
					status = style.Dim.Render("[needs-rework]")
				} else {
					status = style.Dim.Render("[pending]")
				}
			case forge.MRInProgress:
				status = style.Bold.Render("[processing]")
			case forge.MRClosed:
				switch item.MR.CloseReason {
				case forge.CloseReasonMerged:
					status = style.Bold.Render("[merged]")
				case forge.CloseReasonRejected:
					status = style.Dim.Render("[rejected]")
				case forge.CloseReasonConflict:
					status = style.Dim.Render("[conflict]")
				case forge.CloseReasonSuperseded:
					status = style.Dim.Render("[superseded]")
				default:
					status = style.Dim.Render("[closed]")
				}
			}
		}

		issueInfo := ""
		if item.MR.IssueID != "" {
			issueInfo = fmt.Sprintf(" (%s)", item.MR.IssueID)
		}

		fmt.Printf("%s %s %s/%s%s %s\n",
			prefix,
			status,
			item.MR.Worker,
			item.MR.Branch,
			issueInfo,
			style.Dim.Render(item.Age))
	}

	return nil
}

func runForgeAttach(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	// Use getForgeManager to validate warband (and infer from cwd if needed)
	mgr, _, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	// Session name follows the same pattern as forge manager
	sessionID := fmt.Sprintf("hd-%s-forge", rigName)

	// Check if session exists
	t := tmux.NewTmux()
	running, err := t.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		// Auto-start if not running
		fmt.Printf("Forge not running for %s, starting...\n", rigName)
		if err := mgr.Start(false, forgeAgentOverride); err != nil {
			return fmt.Errorf("starting forge: %w", err)
		}
		fmt.Printf("%s Forge started\n", style.Bold.Render("✓"))
	}

	// Summon to session using exec to properly forward TTY
	return attachToTmuxSession(sessionID)
}

func runForgeRestart(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	mgr, _, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Restarting forge for %s...\n", rigName)

	// Stop if running (ignore ErrNotRunning)
	if err := mgr.Stop(); err != nil && err != forge.ErrNotRunning {
		return fmt.Errorf("stopping forge: %w", err)
	}

	// Start fresh
	if err := mgr.Start(false, forgeAgentOverride); err != nil {
		return fmt.Errorf("starting forge: %w", err)
	}

	fmt.Printf("%s Forge restarted for %s\n", style.Bold.Render("✓"), rigName)
	fmt.Printf("  %s\n", style.Dim.Render("Use 'hd forge summon' to connect"))
	return nil
}

// getWorkerID returns the forge worker ID from environment or default.
func getWorkerID() string {
	if id := os.Getenv("HD_FORGE_WORKER"); id != "" {
		return id
	}
	return "forge-1"
}

func runForgeClaim(cmd *cobra.Command, args []string) error {
	mrID := args[0]
	workerID := getWorkerID()

	// Find relics from current working directory
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}
	rigName, err := inferRigFromCwd(townRoot)
	if err != nil {
		return fmt.Errorf("could not determine warband: %w", err)
	}

	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	eng := forge.NewEngineer(r)
	if err := eng.ClaimMR(mrID, workerID); err != nil {
		return fmt.Errorf("claiming MR: %w", err)
	}

	fmt.Printf("%s Claimed %s for %s\n", style.Bold.Render("✓"), mrID, workerID)
	return nil
}

func runForgeRelease(cmd *cobra.Command, args []string) error {
	mrID := args[0]

	// Find relics from current working directory
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}
	rigName, err := inferRigFromCwd(townRoot)
	if err != nil {
		return fmt.Errorf("could not determine warband: %w", err)
	}

	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	eng := forge.NewEngineer(r)
	if err := eng.ReleaseMR(mrID); err != nil {
		return fmt.Errorf("releasing MR: %w", err)
	}

	fmt.Printf("%s Released %s back to queue\n", style.Bold.Render("✓"), mrID)
	return nil
}

func runForgeUnclaimed(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	_, r, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	// Query relics for merge-request issues without assignee
	b := relics.New(r.Path)
	issues, err := b.List(relics.ListOptions{
		Status:   "open",
		Label:    "gt:merge-request",
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("listing merge requests: %w", err)
	}

	// Filter for unclaimed (no assignee)
	var unclaimed []*forge.MRInfo
	for _, issue := range issues {
		if issue.Assignee != "" {
			continue
		}
		fields := relics.ParseMRFields(issue)
		if fields == nil {
			continue
		}
		mr := &forge.MRInfo{
			ID:       issue.ID,
			Branch:   fields.Branch,
			Target:   fields.Target,
			Worker:   fields.Worker,
			Priority: issue.Priority,
		}
		unclaimed = append(unclaimed, mr)
	}

	// JSON output
	if forgeUnclaimedJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(unclaimed)
	}

	// Human-readable output
	fmt.Printf("%s Unclaimed MRs for '%s':\n\n", style.Bold.Render("📋"), rigName)

	if len(unclaimed) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(none available)"))
		return nil
	}

	for i, mr := range unclaimed {
		priority := fmt.Sprintf("P%d", mr.Priority)
		fmt.Printf("  %d. [%s] %s → %s\n", i+1, priority, mr.Branch, mr.Target)
		fmt.Printf("     ID: %s  Worker: %s\n", mr.ID, mr.Worker)
	}

	return nil
}

func runForgeReady(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	_, r, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	// Create engineer for the warband (it has relics access for status checking)
	eng := forge.NewEngineer(r)

	// Get ready MRs (unclaimed AND unblocked)
	ready, err := eng.ListReadyMRs()
	if err != nil {
		return fmt.Errorf("listing ready MRs: %w", err)
	}

	// JSON output
	if forgeReadyJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ready)
	}

	// Human-readable output
	fmt.Printf("%s Ready MRs for '%s':\n\n", style.Bold.Render("🚀"), rigName)

	if len(ready) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(none ready)"))
		return nil
	}

	for i, mr := range ready {
		priority := fmt.Sprintf("P%d", mr.Priority)
		fmt.Printf("  %d. [%s] %s → %s\n", i+1, priority, mr.Branch, mr.Target)
		fmt.Printf("     ID: %s  Worker: %s\n", mr.ID, mr.Worker)
	}

	return nil
}

func runForgeBlocked(cmd *cobra.Command, args []string) error {
	rigName := ""
	if len(args) > 0 {
		rigName = args[0]
	}

	_, r, rigName, err := getForgeManager(rigName)
	if err != nil {
		return err
	}

	// Create engineer for the warband (it has relics access for status checking)
	eng := forge.NewEngineer(r)

	// Get blocked MRs
	blocked, err := eng.ListBlockedMRs()
	if err != nil {
		return fmt.Errorf("listing blocked MRs: %w", err)
	}

	// JSON output
	if forgeBlockedJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(blocked)
	}

	// Human-readable output
	fmt.Printf("%s Blocked MRs for '%s':\n\n", style.Bold.Render("🚧"), rigName)

	if len(blocked) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(none blocked)"))
		return nil
	}

	for i, mr := range blocked {
		priority := fmt.Sprintf("P%d", mr.Priority)
		fmt.Printf("  %d. [%s] %s → %s\n", i+1, priority, mr.Branch, mr.Target)
		fmt.Printf("     ID: %s  Worker: %s\n", mr.ID, mr.Worker)
		if mr.BlockedBy != "" {
			fmt.Printf("     Blocked by: %s\n", mr.BlockedBy)
		}
	}

	return nil
}
