package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/runtime"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/swarm"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

// Swarm command flags
var (
	swarmEpic       string
	swarmTasks      []string
	swarmWorkers    []string
	swarmStart      bool
	swarmStatusJSON bool
	swarmListRig    string
	swarmListStatus string
	swarmListJSON   bool
	swarmTarget     string
)

var swarmCmd = &cobra.Command{
	Use:        "swarm",
	GroupID:    GroupWork,
	Short:      "[DEPRECATED] Use 'hd raid' instead",
	Deprecated: "Use 'hd raid' for work tracking. A 'swarm' is now just the ephemeral workers on a raid.",
	RunE:       requireSubcommand,
	Long: `DEPRECATED: Use 'hd raid' instead.

The term "swarm" now refers to the ephemeral set of workers on a raid's issues,
not a persistent tracking unit. Use 'hd raid' for creating and tracking batched work.

TERMINOLOGY:
  Raid: Persistent tracking unit (what this command was trying to be)
  Swarm:  Ephemeral workers on a raid (no separate tracking needed)

MIGRATION:
  hd swarm create  →  hd raid create
  hd swarm status  →  hd raid status
  hd swarm list    →  hd raid list

See 'hd raid --help' for the new workflow.`,
}

var swarmCreateCmd = &cobra.Command{
	Use:   "create <warband>",
	Short: "Create a new swarm",
	Long: `Create a new swarm in a warband.

Creates a swarm that coordinates multiple raiders working on tasks from
a relics epic. All workers branch from the same base commit.

Examples:
  hd swarm create greenplace --epic gp-abc --worker Toast --worker Nux
  hd swarm create greenplace --epic gp-abc --worker Toast --start`,
	Args: cobra.ExactArgs(1),
	RunE: runSwarmCreate,
}

var swarmStatusCmd = &cobra.Command{
	Use:   "status <swarm-id>",
	Short: "Show swarm status",
	Long: `Show detailed status for a swarm.

Displays swarm metadata, task progress, worker assignments, and integration
branch status.`,
	Args: cobra.ExactArgs(1),
	RunE: runSwarmStatus,
}

var swarmListCmd = &cobra.Command{
	Use:   "list [warband]",
	Short: "List swarms",
	Long: `List swarms, optionally filtered by warband or status.

Examples:
  hd swarm list
  hd swarm list greenplace
  hd swarm list --status=active
  hd swarm list greenplace --status=landed`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSwarmList,
}

var swarmLandCmd = &cobra.Command{
	Use:   "land <swarm-id>",
	Short: "Land a swarm to main",
	Long: `Manually trigger landing for a completed swarm.

Merges the integration branch to the target branch (usually main).
Normally this is done automatically by the Forge.`,
	Args: cobra.ExactArgs(1),
	RunE: runSwarmLand,
}

var swarmCancelCmd = &cobra.Command{
	Use:   "cancel <swarm-id>",
	Short: "Cancel a swarm",
	Long: `Cancel an active swarm.

Marks the swarm as canceled and optionally cleans up branches.`,
	Args: cobra.ExactArgs(1),
	RunE: runSwarmCancel,
}

var swarmStartCmd = &cobra.Command{
	Use:   "start <swarm-id>",
	Short: "Start a created swarm",
	Long: `Start a swarm that was created without --start.

Transitions the swarm from 'created' to 'active' state.`,
	Args: cobra.ExactArgs(1),
	RunE: runSwarmStart,
}

var swarmDispatchCmd = &cobra.Command{
	Use:   "dispatch <epic-id>",
	Short: "Assign next ready task to a fresh raider",
	Long: `Dispatch the next ready task from an epic to a new raider.

Finds the first unassigned task in the epic's ready front and spawns a
fresh raider to work on it. Self-cleaning model: raiders are always
fresh - there are no idle raiders to reuse.

Examples:
  hd swarm dispatch gt-abc         # Dispatch next task from epic gt-abc
  hd swarm dispatch gt-abc --warband greenplace  # Dispatch in specific warband`,
	Args: cobra.ExactArgs(1),
	RunE: runSwarmDispatch,
}

var swarmDispatchRig string

func init() {
	// Create flags
	swarmCreateCmd.Flags().StringVar(&swarmEpic, "epic", "", "Relics epic ID for this swarm (required)")
	swarmCreateCmd.Flags().StringSliceVar(&swarmWorkers, "worker", nil, "Raider names to assign (repeatable)")
	swarmCreateCmd.Flags().BoolVar(&swarmStart, "start", false, "Start swarm immediately after creation")
	swarmCreateCmd.Flags().StringVar(&swarmTarget, "target", "main", "Target branch for landing")
	_ = swarmCreateCmd.MarkFlagRequired("epic") // cobra flags: error only at runtime if missing

	// Status flags
	swarmStatusCmd.Flags().BoolVar(&swarmStatusJSON, "json", false, "Output as JSON")

	// List flags
	swarmListCmd.Flags().StringVar(&swarmListStatus, "status", "", "Filter by status (active, landed, canceled, failed)")
	swarmListCmd.Flags().BoolVar(&swarmListJSON, "json", false, "Output as JSON")

	// Dispatch flags
	swarmDispatchCmd.Flags().StringVar(&swarmDispatchRig, "warband", "", "Warband to dispatch in (auto-detected from epic if not specified)")

	// Add subcommands
	swarmCmd.AddCommand(swarmCreateCmd)
	swarmCmd.AddCommand(swarmStartCmd)
	swarmCmd.AddCommand(swarmStatusCmd)
	swarmCmd.AddCommand(swarmListCmd)
	swarmCmd.AddCommand(swarmLandCmd)
	swarmCmd.AddCommand(swarmCancelCmd)
	swarmCmd.AddCommand(swarmDispatchCmd)

	rootCmd.AddCommand(swarmCmd)
}

// getSwarmRig gets a warband by name.
func getSwarmRig(rigName string) (*warband.Warband, string, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return nil, "", fmt.Errorf("not in a Horde workspace: %w", err)
	}

	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(rigName)
	if err != nil {
		return nil, "", fmt.Errorf("warband '%s' not found", rigName)
	}

	return r, townRoot, nil
}

// getAllRigs returns all discovered warbands.
func getAllRigs() ([]*warband.Warband, string, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return nil, "", fmt.Errorf("not in a Horde workspace: %w", err)
	}

	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	warbands, err := rigMgr.DiscoverRigs()
	if err != nil {
		return nil, "", err
	}

	return warbands, townRoot, nil
}

func runSwarmCreate(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	r, townRoot, err := getSwarmRig(rigName)
	if err != nil {
		return err
	}

	// Use relics to create the swarm totem
	// First check if the epic already exists (it may be pre-created)
	// Use RelicsPath() to ensure we read from git-synced relics location
	relicsPath := r.RelicsPath()
	checkCmd := exec.Command("rl", "show", swarmEpic, "--json")
	checkCmd.Dir = relicsPath
	if err := checkCmd.Run(); err != nil {
		// Epic doesn't exist, create it as a swarm totem
		createArgs := []string{
			"create",
			"--type=epic",
			"--totem-type=swarm",
			"--title", swarmEpic,
			"--silent",
		}
		createCmd := exec.Command("rl", createArgs...)
		createCmd.Dir = relicsPath
		var stdout bytes.Buffer
		createCmd.Stdout = &stdout
		if err := createCmd.Run(); err != nil {
			return fmt.Errorf("creating swarm epic: %w", err)
		}
	}

	// Get current git commit as base
	baseCommit := "unknown"
	gitCmd := exec.Command("git", "rev-parse", "HEAD")
	gitCmd.Dir = r.Path
	if out, err := gitCmd.Output(); err == nil {
		baseCommit = strings.TrimSpace(string(out))
	}

	integration := fmt.Sprintf("swarm/%s", swarmEpic)

	// Output
	fmt.Printf("%s Created swarm %s\n\n", style.Bold.Render("✓"), swarmEpic)
	fmt.Printf("  Epic:        %s\n", swarmEpic)
	fmt.Printf("  Warband:         %s\n", rigName)
	fmt.Printf("  Base commit: %s\n", truncate(baseCommit, 8))
	fmt.Printf("  Integration: %s\n", integration)
	fmt.Printf("  Target:      %s\n", swarmTarget)
	fmt.Printf("  Workers:     %s\n", strings.Join(swarmWorkers, ", "))

	// If workers specified, assign them to tasks
	if len(swarmWorkers) > 0 {
		fmt.Printf("\nNote: Worker assignment to tasks is handled during swarm start\n")
	}

	// Start if requested
	if swarmStart {
		// Get swarm status to find ready tasks
		statusCmd := exec.Command("rl", "swarm", "status", swarmEpic, "--json")
		statusCmd.Dir = relicsPath
		var statusOut bytes.Buffer
		statusCmd.Stdout = &statusOut
		if err := statusCmd.Run(); err != nil {
			return fmt.Errorf("getting swarm status: %w", err)
		}

		// Parse status to dispatch workers
		var status struct {
			Ready []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"ready"`
		}
		if err := json.Unmarshal(statusOut.Bytes(), &status); err == nil && len(status.Ready) > 0 {
			fmt.Printf("\nReady front has %d tasks available\n", len(status.Ready))
			if len(swarmWorkers) > 0 {
				// Muster workers for ready tasks
				fmt.Printf("Spawning workers...\n")
				_ = spawnSwarmWorkersFromRelics(r, townRoot, swarmEpic, swarmWorkers, status.Ready)
			}
		}
	} else {
		fmt.Printf("\n  %s\n", style.Dim.Render("Use --start or 'hd swarm start' to activate"))
	}

	return nil
}

func runSwarmStart(cmd *cobra.Command, args []string) error {
	swarmID := args[0]

	// Find the swarm's warband
	warbands, townRoot, err := getAllRigs()
	if err != nil {
		return err
	}

	var foundRig *warband.Warband
	for _, r := range warbands {
		// Check if swarm exists in this warband by querying relics
		// Use RelicsPath() to ensure we read from git-synced location
		checkCmd := exec.Command("rl", "show", swarmID, "--json")
		checkCmd.Dir = r.RelicsPath()
		if err := checkCmd.Run(); err == nil {
			foundRig = r
			break
		}
	}

	if foundRig == nil {
		return fmt.Errorf("swarm '%s' not found", swarmID)
	}

	// Get swarm status from relics
	statusCmd := exec.Command("rl", "swarm", "status", swarmID, "--json")
	statusCmd.Dir = foundRig.RelicsPath()
	var stdout bytes.Buffer
	statusCmd.Stdout = &stdout

	if err := statusCmd.Run(); err != nil {
		return fmt.Errorf("getting swarm status: %w", err)
	}

	var status struct {
		EpicID string `json:"epic_id"`
		Ready  []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"ready"`
		Active []struct {
			ID       string `json:"id"`
			Assignee string `json:"assignee"`
		} `json:"active"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		return fmt.Errorf("parsing swarm status: %w", err)
	}

	if len(status.Active) > 0 {
		fmt.Printf("Swarm already has %d active tasks\n", len(status.Active))
	}

	if len(status.Ready) == 0 {
		fmt.Println("No ready tasks to dispatch")
		return nil
	}

	fmt.Printf("%s Swarm %s starting with %d ready tasks\n", style.Bold.Render("✓"), swarmID, len(status.Ready))

	// If workers were specified in create, use them; otherwise prompt user
	if len(swarmWorkers) > 0 {
		fmt.Printf("\nSpawning workers...\n")
		_ = spawnSwarmWorkersFromRelics(foundRig, townRoot, swarmID, swarmWorkers, status.Ready)
	} else {
		fmt.Printf("\nReady tasks:\n")
		for _, task := range status.Ready {
			fmt.Printf("  ○ %s: %s\n", task.ID, task.Title)
		}
		fmt.Printf("\nUse 'hd charge <task-id> <warband>/<worker>' to assign tasks\n")
	}

	return nil
}

func runSwarmDispatch(cmd *cobra.Command, args []string) error {
	epicID := args[0]

	// Find the epic's warband by trying to show it in each warband
	warbands, townRoot, err := getAllRigs()
	if err != nil {
		return err
	}

	var foundRig *warband.Warband
	for _, r := range warbands {
		// If --warband specified, only check that warband
		if swarmDispatchRig != "" && r.Name != swarmDispatchRig {
			continue
		}
		// Use RelicsPath() to ensure we read from git-synced location
		checkCmd := exec.Command("rl", "show", epicID, "--json")
		checkCmd.Dir = r.RelicsPath()
		if err := checkCmd.Run(); err == nil {
			foundRig = r
			break
		}
	}

	if foundRig == nil {
		if swarmDispatchRig != "" {
			return fmt.Errorf("epic '%s' not found in warband '%s'", epicID, swarmDispatchRig)
		}
		return fmt.Errorf("epic '%s' not found in any warband", epicID)
	}

	// Get swarm/epic status to find ready tasks
	statusCmd := exec.Command("rl", "swarm", "status", epicID, "--json")
	statusCmd.Dir = foundRig.RelicsPath()
	var stdout bytes.Buffer
	statusCmd.Stdout = &stdout

	if err := statusCmd.Run(); err != nil {
		return fmt.Errorf("getting epic status: %w", err)
	}

	var status struct {
		Ready []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Assignee string `json:"assignee"`
		} `json:"ready"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		return fmt.Errorf("parsing epic status: %w", err)
	}

	// Filter to unassigned ready tasks
	var unassigned []struct {
		ID    string
		Title string
	}
	for _, task := range status.Ready {
		if task.Assignee == "" {
			unassigned = append(unassigned, struct {
				ID    string
				Title string
			}{task.ID, task.Title})
		}
	}

	if len(unassigned) == 0 {
		fmt.Println("No unassigned ready tasks to dispatch")
		return nil
	}

	// Self-cleaning model: Always muster fresh raiders for work.
	// There are no "idle" raiders - raiders self-nuke when done.
	// Just charge to the warband and let hd charge muster a fresh raider.
	task := unassigned[0]

	fmt.Printf("Dispatching %s to fresh raider in %s...\n", task.ID, foundRig.Name)

	// Use hd charge to muster a fresh raider and assign the task
	slingCmd := exec.Command("hd", "charge", task.ID, foundRig.Name)
	slingCmd.Dir = townRoot
	slingCmd.Stdout = os.Stdout
	slingCmd.Stderr = os.Stderr

	if err := slingCmd.Run(); err != nil {
		return fmt.Errorf("charging task: %w", err)
	}

	fmt.Printf("%s Dispatched %s: %s → fresh raider\n", style.Bold.Render("✓"), task.ID, task.Title)

	// Show remaining tasks
	if len(unassigned) > 1 {
		fmt.Printf("\n%d more ready tasks available\n", len(unassigned)-1)
	}

	return nil
}

// spawnSwarmWorkersFromRelics spawns sessions for swarm workers using relics task list.
func spawnSwarmWorkersFromRelics(r *warband.Warband, townRoot string, swarmID string, workers []string, tasks []struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}) error { //nolint:unparam // error return kept for future use
	t := tmux.NewTmux()
	raiderSessMgr := raider.NewSessionManager(t, r)
	raiderGit := git.NewGit(r.Path)
	raiderMgr := raider.NewManager(r, raiderGit, t)

	// Pair workers with tasks (round-robin if more tasks than workers)
	workerIdx := 0
	for _, task := range tasks {
		if workerIdx >= len(workers) {
			break // No more workers
		}

		worker := workers[workerIdx]
		workerIdx++

		// Use hd charge to assign task to worker (this updates relics)
		slingCmd := exec.Command("hd", "charge", task.ID, fmt.Sprintf("%s/%s", r.Name, worker))
		slingCmd.Dir = townRoot
		if err := slingCmd.Run(); err != nil {
			style.PrintWarning("  couldn't charge %s to %s: %v", task.ID, worker, err)

			// Fallback: update raider state directly
			if err := raiderMgr.AssignIssue(worker, task.ID); err != nil {
				style.PrintWarning("  couldn't assign %s to %s: %v", task.ID, worker, err)
				continue
			}
		}

		// Check if already running
		running, _ := raiderSessMgr.IsRunning(worker)
		if running {
			fmt.Printf("  %s already running, injecting task...\n", worker)
		} else {
			fmt.Printf("  Starting %s...\n", worker)
			if err := raiderSessMgr.Start(worker, raider.SessionStartOptions{}); err != nil {
				style.PrintWarning("  couldn't start %s: %v", worker, err)
				continue
			}
			// Wait for Claude to initialize
			time.Sleep(5 * time.Second)
		}

		// Inject work assignment
		context := fmt.Sprintf("[SWARM] You are part of swarm %s.\n\nAssigned task: %s\nTitle: %s\n\nWork on this task. When complete, commit and signal DONE.",
			swarmID, task.ID, task.Title)
		if err := raiderSessMgr.Inject(worker, context); err != nil {
			style.PrintWarning("  couldn't inject to %s: %v", worker, err)
		} else {
			fmt.Printf("  %s → %s ✓\n", worker, task.ID)
		}
	}

	return nil
}

func runSwarmStatus(cmd *cobra.Command, args []string) error {
	swarmID := args[0]

	// Find the swarm's warband by trying to show it in each warband
	warbands, _, err := getAllRigs()
	if err != nil {
		return err
	}
	if len(warbands) == 0 {
		return fmt.Errorf("no warbands found")
	}

	// Find which warband has this swarm
	var foundRig *warband.Warband
	for _, r := range warbands {
		// Use RelicsPath() to ensure we read from git-synced location
		checkCmd := exec.Command("rl", "show", swarmID, "--json")
		checkCmd.Dir = r.RelicsPath()
		if err := checkCmd.Run(); err == nil {
			foundRig = r
			break
		}
	}

	if foundRig == nil {
		return fmt.Errorf("swarm '%s' not found in any warband", swarmID)
	}

	// Use rl swarm status to get swarm info from relics
	bdArgs := []string{"swarm", "status", swarmID}
	if swarmStatusJSON {
		bdArgs = append(bdArgs, "--json")
	}

	bdCmd := exec.Command("rl", bdArgs...)
	bdCmd.Dir = foundRig.RelicsPath()
	bdCmd.Stdout = os.Stdout
	bdCmd.Stderr = os.Stderr

	return bdCmd.Run()
}

func runSwarmList(cmd *cobra.Command, args []string) error {
	warbands, _, err := getAllRigs()
	if err != nil {
		return err
	}

	// Filter by warband if specified
	if len(args) > 0 {
		rigName := args[0]
		var filtered []*warband.Warband
		for _, r := range warbands {
			if r.Name == rigName {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("warband '%s' not found", rigName)
		}
		warbands = filtered
	}

	if len(warbands) == 0 {
		fmt.Println("No warbands found.")
		return nil
	}

	// Use rl list --totem-type=swarm to find swarm totems
	bdArgs := []string{"list", "--totem-type=swarm", "--type=epic"}
	if swarmListJSON {
		bdArgs = append(bdArgs, "--json")
	}

	// Collect swarms from all warbands
	type swarmListEntry struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
		Warband    string `json:"warband"`
	}
	var allSwarms []swarmListEntry

	for _, r := range warbands {
		bdCmd := exec.Command("rl", bdArgs...)
		bdCmd.Dir = r.RelicsPath() // Use RelicsPath() for git-synced relics
		var stdout bytes.Buffer
		bdCmd.Stdout = &stdout

		if err := bdCmd.Run(); err != nil {
			continue
		}

		if swarmListJSON {
			// Parse JSON output
			var issues []struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &issues); err == nil {
				for _, issue := range issues {
					allSwarms = append(allSwarms, swarmListEntry{
						ID:     issue.ID,
						Title:  issue.Title,
						Status: issue.Status,
						Warband:    r.Name,
					})
				}
			}
		} else {
			// Parse line output - each line is an issue
			lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				// Filter by status if specified
				if swarmListStatus != "" && !strings.Contains(strings.ToLower(line), swarmListStatus) {
					continue
				}
				allSwarms = append(allSwarms, swarmListEntry{
					ID:  line,
					Warband: r.Name,
				})
			}
		}
	}

	// JSON output
	if swarmListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(allSwarms)
	}

	// Human-readable output
	if len(allSwarms) == 0 {
		fmt.Println("No swarms found.")
		fmt.Println("Create a swarm with: hd swarm create <warband> --epic <epic-id>")
		return nil
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Swarms"))
	for _, entry := range allSwarms {
		fmt.Printf("  %s [%s]\n", entry.ID, entry.Warband)
	}
	fmt.Printf("\nUse 'hd swarm status <id>' for detailed status.\n")

	return nil
}

func runSwarmLand(cmd *cobra.Command, args []string) error {
	swarmID := args[0]

	// Find the swarm's warband
	warbands, townRoot, err := getAllRigs()
	if err != nil {
		return err
	}

	var foundRig *warband.Warband
	for _, r := range warbands {
		// Use RelicsPath() for git-synced relics
		checkCmd := exec.Command("rl", "show", swarmID, "--json")
		checkCmd.Dir = r.RelicsPath()
		if err := checkCmd.Run(); err == nil {
			foundRig = r
			break
		}
	}

	if foundRig == nil {
		return fmt.Errorf("swarm '%s' not found", swarmID)
	}

	// Check swarm status - all children should be closed
	statusCmd := exec.Command("rl", "swarm", "status", swarmID, "--json")
	statusCmd.Dir = foundRig.RelicsPath()
	var stdout bytes.Buffer
	statusCmd.Stdout = &stdout

	if err := statusCmd.Run(); err != nil {
		return fmt.Errorf("getting swarm status: %w", err)
	}

	var status struct {
		Ready       []struct{ ID string } `json:"ready"`
		Active      []struct{ ID string } `json:"active"`
		Blocked     []struct{ ID string } `json:"blocked"`
		Completed   []struct{ ID string } `json:"completed"`
		TotalIssues int                   `json:"total_issues"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		return fmt.Errorf("parsing swarm status: %w", err)
	}

	// Check if all tasks are complete
	if len(status.Ready) > 0 || len(status.Active) > 0 || len(status.Blocked) > 0 {
		return fmt.Errorf("swarm has incomplete tasks: %d ready, %d active, %d blocked",
			len(status.Ready), len(status.Active), len(status.Blocked))
	}

	fmt.Printf("Landing swarm %s to main...\n", swarmID)

	// Use swarm manager for the actual landing (git operations)
	mgr := swarm.NewManager(foundRig)
	sw, err := mgr.LoadSwarm(swarmID)
	if err != nil {
		return fmt.Errorf("loading swarm from relics: %w", err)
	}

	// Execute full landing protocol
	config := swarm.LandingConfig{
		TownRoot: townRoot,
	}
	result, err := mgr.ExecuteLanding(swarmID, config)
	if err != nil {
		return fmt.Errorf("landing protocol: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("landing failed: %s", result.Error)
	}

	// Close the swarm epic in relics
	closeArgs := []string{"close", swarmID, "--reason", "Swarm landed to main"}
	if sessionID := runtime.SessionIDFromEnv(); sessionID != "" {
		closeArgs = append(closeArgs, "--session="+sessionID)
	}
	closeCmd := exec.Command("rl", closeArgs...)
	closeCmd.Dir = foundRig.RelicsPath()
	if err := closeCmd.Run(); err != nil {
		style.PrintWarning("couldn't close swarm epic in relics: %v", err)
	}

	fmt.Printf("%s Swarm %s landed to main\n", style.Bold.Render("✓"), sw.ID)
	fmt.Printf("  Sessions stopped: %d\n", result.SessionsStopped)
	fmt.Printf("  Branches cleaned: %d\n", result.BranchesCleaned)
	return nil
}

func runSwarmCancel(cmd *cobra.Command, args []string) error {
	swarmID := args[0]

	// Find the swarm's warband
	warbands, _, err := getAllRigs()
	if err != nil {
		return err
	}

	var foundRig *warband.Warband
	for _, r := range warbands {
		// Use RelicsPath() for git-synced relics
		checkCmd := exec.Command("rl", "show", swarmID, "--json")
		checkCmd.Dir = r.RelicsPath()
		if err := checkCmd.Run(); err == nil {
			foundRig = r
			break
		}
	}

	if foundRig == nil {
		return fmt.Errorf("swarm '%s' not found", swarmID)
	}

	// Check if swarm is already closed
	checkCmd := exec.Command("rl", "show", swarmID, "--json")
	checkCmd.Dir = foundRig.RelicsPath()
	var stdout bytes.Buffer
	checkCmd.Stdout = &stdout
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("checking swarm status: %w", err)
	}

	var issue struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &issue); err == nil {
		if issue.Status == "closed" {
			return fmt.Errorf("swarm already closed")
		}
	}

	// Close the swarm epic in relics with canceled reason
	closeArgs := []string{"close", swarmID, "--reason", "Swarm canceled"}
	if sessionID := runtime.SessionIDFromEnv(); sessionID != "" {
		closeArgs = append(closeArgs, "--session="+sessionID)
	}
	closeCmd := exec.Command("rl", closeArgs...)
	closeCmd.Dir = foundRig.RelicsPath()
	if err := closeCmd.Run(); err != nil {
		return fmt.Errorf("closing swarm: %w", err)
	}

	fmt.Printf("%s Swarm %s canceled\n", style.Bold.Render("✓"), swarmID)
	return nil
}

// Helper functions

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
