package cmd

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tui/raid"
	"github.com/deeklead/horde/internal/workspace"
)

// generateShortID generates a short random ID (5 lowercase chars).
func generateShortID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return strings.ToLower(base32.StdEncoding.EncodeToString(b)[:5])
}

// looksLikeIssueID checks if a string looks like a relics issue ID.
// Issue IDs have the format: prefix-id (e.g., gt-abc, bd-xyz, hq-123).
func looksLikeIssueID(s string) bool {
	// Common relics prefixes
	prefixes := []string{"hd-", "bd-", "hq-"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	// Also check for pattern: 2-3 lowercase letters followed by hyphen
	// This catches custom prefixes defined in routes.jsonl
	if len(s) >= 4 && s[2] == '-' || (len(s) >= 5 && s[3] == '-') {
		hyphenIdx := strings.Index(s, "-")
		if hyphenIdx >= 2 && hyphenIdx <= 3 {
			prefix := s[:hyphenIdx]
			// Check if prefix is all lowercase letters
			allLower := true
			for _, c := range prefix {
				if c < 'a' || c > 'z' {
					allLower = false
					break
				}
			}
			return allLower
		}
	}
	return false
}

// Raid command flags
var (
	raidMolecule     string
	raidNotify       string
	raidOwner        string
	raidStatusJSON   bool
	raidListJSON     bool
	raidListStatus   string
	raidListAll      bool
	raidListTree     bool
	raidInteractive  bool
	raidStrandedJSON bool
	raidCloseReason  string
	raidCloseNotify  string
)

var raidCmd = &cobra.Command{
	Use:     "raid",
	GroupID: GroupWork,
	Short:   "Track batches of work across warbands",
	RunE: func(cmd *cobra.Command, args []string) error {
		if raidInteractive {
			return runRaidTUI()
		}
		return requireSubcommand(cmd, args)
	},
	Long: `Manage raids - the primary unit for tracking batched work.

A raid is a persistent tracking unit that monitors related issues across
warbands. When you kick off work (even a single issue), a raid tracks it so
you can see when it lands and what was included.

WHAT IS A RAID:
  - Persistent tracking unit with an ID (hq-*)
  - Tracks issues across warbands (frontend+backend, relics+horde, etc.)
  - Auto-closes when all tracked issues complete → notifies subscribers
  - Can be reopened by adding more issues

WHAT IS A SWARM:
  - Ephemeral: "the workers currently assigned to a raid's issues"
  - No separate ID - uses the raid ID
  - Dissolves when work completes

TRACKING SEMANTICS:
  - 'tracks' relation is non-blocking (tracked issues don't block raid)
  - Cross-prefix capable (raid in hq-* tracks issues in gt-*, bd-*)
  - Landed: all tracked issues closed → notification sent to subscribers

COMMANDS:
  create    Create a raid tracking specified issues
  add       Add issues to an existing raid (reopens if closed)
  close     Close a raid (manually, regardless of tracked issue status)
  status    Show raid progress, tracked issues, and active workers
  list      List raids (the warmap view)`,
}

var raidCreateCmd = &cobra.Command{
	Use:   "create <name> [issues...]",
	Short: "Create a new raid",
	Long: `Create a new raid that tracks the specified issues.

The raid is created in encampment-level relics (hq-* prefix) and can track
issues across any warband.

The --owner flag specifies who requested the raid (receives completion
notification by default). If not specified, defaults to created_by.
The --notify flag adds additional subscribers beyond the owner.

Examples:
  hd raid create "Deploy v2.0" gt-abc bd-xyz
  hd raid create "Release prep" gt-abc --notify           # defaults to warchief/
  hd raid create "Release prep" gt-abc --notify ops/      # notify ops/
  hd raid create "Feature rollout" gt-a gt-b --owner warchief/ --notify ops/
  hd raid create "Feature rollout" gt-a gt-b gt-c --totem totem-release`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRaidCreate,
}

var raidStatusCmd = &cobra.Command{
	Use:   "status [raid-id]",
	Short: "Show raid status",
	Long: `Show detailed status for a raid.

Displays raid metadata, tracked issues, and completion progress.
Without an ID, shows status of all active raids.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRaidStatus,
}

var raidListCmd = &cobra.Command{
	Use:   "list",
	Short: "List raids",
	Long: `List raids, showing open raids by default.

Examples:
  hd raid list              # Open raids only (default)
  hd raid list --all        # All raids (open + closed)
  hd raid list --status=closed  # Recently landed
  hd raid list --tree       # Show raid + child status tree
  hd raid list --json`,
	RunE: runRaidList,
}

var raidAddCmd = &cobra.Command{
	Use:   "add <raid-id> <issue-id> [issue-id...]",
	Short: "Add issues to an existing raid",
	Long: `Add issues to an existing raid.

If the raid is closed, it will be automatically reopened.

Examples:
  hd raid add hq-cv-abc gt-new-issue
  hd raid add hq-cv-abc gt-issue1 gt-issue2 gt-issue3`,
	Args: cobra.MinimumNArgs(2),
	RunE: runRaidAdd,
}

var raidCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check and auto-close completed raids",
	Long: `Check all open raids and auto-close any where all tracked issues are complete.

This handles cross-warband raid completion: raids in encampment relics tracking issues
in warband relics won't auto-close via rl close alone. This command bridges that gap.

Can be run manually or by shaman scout to ensure raids close promptly.`,
	RunE: runRaidCheck,
}

var raidStrandedCmd = &cobra.Command{
	Use:   "stranded",
	Short: "Find stranded raids with ready work but no workers",
	Long: `Find raids that have ready issues but no workers processing them.

A raid is "stranded" when:
- Raid is open
- Has tracked issues where:
  - status = open (not in_progress, not closed)
  - not blocked (all dependencies met)
  - no assignee OR assignee session is dead

Use this to detect raids that need feeding. The Shaman scout runs this
periodically and dispatches dogs to feed stranded raids.

Examples:
  hd raid stranded              # Show stranded raids
  hd raid stranded --json       # Machine-readable output for automation`,
	RunE: runRaidStranded,
}

var raidCloseCmd = &cobra.Command{
	Use:   "close <raid-id>",
	Short: "Close a raid",
	Long: `Close a raid, optionally with a reason.

Closes the raid regardless of tracked issue status. Use this to:
- Force-close abandoned raids no longer relevant
- Close raids where work completed outside the tracked path
- Manually close stuck raids

The close is idempotent - closing an already-closed raid is a no-op.

Examples:
  hd raid close hq-cv-abc
  hd raid close hq-cv-abc --reason="work done differently"
  hd raid close hq-cv-xyz --notify warchief/`,
	Args: cobra.ExactArgs(1),
	RunE: runRaidClose,
}

func init() {
	// Create flags
	raidCreateCmd.Flags().StringVar(&raidMolecule, "totem", "", "Associated totem ID")
	raidCreateCmd.Flags().StringVar(&raidOwner, "owner", "", "Owner who requested raid (gets completion notification)")
	raidCreateCmd.Flags().StringVar(&raidNotify, "notify", "", "Additional address to notify on completion (default: warchief/ if flag used without value)")
	raidCreateCmd.Flags().Lookup("notify").NoOptDefVal = "warchief/"

	// Status flags
	raidStatusCmd.Flags().BoolVar(&raidStatusJSON, "json", false, "Output as JSON")

	// List flags
	raidListCmd.Flags().BoolVar(&raidListJSON, "json", false, "Output as JSON")
	raidListCmd.Flags().StringVar(&raidListStatus, "status", "", "Filter by status (open, closed)")
	raidListCmd.Flags().BoolVar(&raidListAll, "all", false, "Show all raids (open and closed)")
	raidListCmd.Flags().BoolVar(&raidListTree, "tree", false, "Show raid + child status tree")

	// Interactive TUI flag (on parent command)
	raidCmd.Flags().BoolVarP(&raidInteractive, "interactive", "i", false, "Interactive tree view")

	// Stranded flags
	raidStrandedCmd.Flags().BoolVar(&raidStrandedJSON, "json", false, "Output as JSON")

	// Close flags
	raidCloseCmd.Flags().StringVar(&raidCloseReason, "reason", "", "Reason for closing the raid")
	raidCloseCmd.Flags().StringVar(&raidCloseNotify, "notify", "", "Agent to notify on close (e.g., warchief/)")

	// Add subcommands
	raidCmd.AddCommand(raidCreateCmd)
	raidCmd.AddCommand(raidStatusCmd)
	raidCmd.AddCommand(raidListCmd)
	raidCmd.AddCommand(raidAddCmd)
	raidCmd.AddCommand(raidCheckCmd)
	raidCmd.AddCommand(raidStrandedCmd)
	raidCmd.AddCommand(raidCloseCmd)

	rootCmd.AddCommand(raidCmd)
}

// getTownRelicsDir returns the path to encampment-level relics directory.
func getTownRelicsDir() (string, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return "", fmt.Errorf("not in a Horde workspace: %w", err)
	}
	return filepath.Join(townRoot, ".relics"), nil
}

func runRaidCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	trackedIssues := args[1:]

	// If first arg looks like an issue ID (has relics prefix), treat all args as issues
	// and auto-generate a name from the first issue's title
	if looksLikeIssueID(name) {
		trackedIssues = args // All args are issue IDs
		// Get the first issue's title to use as raid name
		if details := getIssueDetails(args[0]); details != nil && details.Title != "" {
			name = details.Title
		} else {
			name = fmt.Sprintf("Tracking %s", args[0])
		}
	}

	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	// Create raid issue in encampment relics
	description := fmt.Sprintf("Raid tracking %d issues", len(trackedIssues))
	if raidOwner != "" {
		description += fmt.Sprintf("\nOwner: %s", raidOwner)
	}
	if raidNotify != "" {
		description += fmt.Sprintf("\nNotify: %s", raidNotify)
	}
	if raidMolecule != "" {
		description += fmt.Sprintf("\nMolecule: %s", raidMolecule)
	}

	// Generate raid ID with cv- prefix
	raidID := fmt.Sprintf("hq-cv-%s", generateShortID())

	createArgs := []string{
		"create",
		"--type=raid",
		"--id=" + raidID,
		"--title=" + name,
		"--description=" + description,
		"--json",
	}

	createCmd := exec.Command("rl", createArgs...)
	createCmd.Dir = townRelics
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	createCmd.Stdout = &stdout
	createCmd.Stderr = &stderr

	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("creating raid: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	// Notify address is stored in description (line 166-168) and read from there

	// Add 'tracks' relations for each tracked issue
	trackedCount := 0
	for _, issueID := range trackedIssues {
		// Use --type=tracks for non-blocking tracking relation
		depArgs := []string{"dep", "add", raidID, issueID, "--type=tracks"}
		depCmd := exec.Command("rl", depArgs...)
		depCmd.Dir = townRelics

		if err := depCmd.Run(); err != nil {
			style.PrintWarning("couldn't track %s: %v", issueID, err)
		} else {
			trackedCount++
		}
	}

	// Output
	fmt.Printf("%s Created raid 🚚 %s\n\n", style.Bold.Render("✓"), raidID)
	fmt.Printf("  Name:     %s\n", name)
	fmt.Printf("  Tracking: %d issues\n", trackedCount)
	if len(trackedIssues) > 0 {
		fmt.Printf("  Issues:   %s\n", strings.Join(trackedIssues, ", "))
	}
	if raidOwner != "" {
		fmt.Printf("  Owner:    %s\n", raidOwner)
	}
	if raidNotify != "" {
		fmt.Printf("  Notify:   %s\n", raidNotify)
	}
	if raidMolecule != "" {
		fmt.Printf("  Totem: %s\n", raidMolecule)
	}

	fmt.Printf("\n  %s\n", style.Dim.Render("Raid auto-closes when all tracked issues complete"))

	return nil
}

func runRaidAdd(cmd *cobra.Command, args []string) error {
	raidID := args[0]
	issuesToAdd := args[1:]

	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	// Validate raid exists and get its status
	showArgs := []string{"show", raidID, "--json"}
	showCmd := exec.Command("rl", showArgs...)
	showCmd.Dir = townRelics
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		return fmt.Errorf("raid '%s' not found", raidID)
	}

	var raids []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
		Type   string `json:"issue_type"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return fmt.Errorf("parsing raid data: %w", err)
	}

	if len(raids) == 0 {
		return fmt.Errorf("raid '%s' not found", raidID)
	}

	raid := raids[0]

	// Verify it's actually a raid type
	if raid.Type != "raid" {
		return fmt.Errorf("'%s' is not a raid (type: %s)", raidID, raid.Type)
	}

	// If raid is closed, reopen it
	reopened := false
	if raid.Status == "closed" {
		reopenArgs := []string{"update", raidID, "--status=open"}
		reopenCmd := exec.Command("rl", reopenArgs...)
		reopenCmd.Dir = townRelics
		if err := reopenCmd.Run(); err != nil {
			return fmt.Errorf("couldn't reopen raid: %w", err)
		}
		reopened = true
		fmt.Printf("%s Reopened raid %s\n", style.Bold.Render("↺"), raidID)
	}

	// Add 'tracks' relations for each issue
	addedCount := 0
	for _, issueID := range issuesToAdd {
		depArgs := []string{"dep", "add", raidID, issueID, "--type=tracks"}
		depCmd := exec.Command("rl", depArgs...)
		depCmd.Dir = townRelics

		if err := depCmd.Run(); err != nil {
			style.PrintWarning("couldn't add %s: %v", issueID, err)
		} else {
			addedCount++
		}
	}

	// Output
	if reopened {
		fmt.Println()
	}
	fmt.Printf("%s Added %d issue(s) to raid 🚚 %s\n", style.Bold.Render("✓"), addedCount, raidID)
	if addedCount > 0 {
		fmt.Printf("  Issues: %s\n", strings.Join(issuesToAdd[:addedCount], ", "))
	}

	return nil
}

func runRaidCheck(cmd *cobra.Command, args []string) error {
	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	closed, err := checkAndCloseCompletedRaids(townRelics)
	if err != nil {
		return err
	}

	if len(closed) == 0 {
		fmt.Println("No raids ready to close.")
	} else {
		fmt.Printf("%s Auto-closed %d raid(s):\n", style.Bold.Render("✓"), len(closed))
		for _, c := range closed {
			fmt.Printf("  🚚 %s: %s\n", c.ID, c.Title)
		}
	}

	return nil
}

func runRaidClose(cmd *cobra.Command, args []string) error {
	raidID := args[0]

	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	// Get raid details
	showArgs := []string{"show", raidID, "--json"}
	showCmd := exec.Command("rl", showArgs...)
	showCmd.Dir = townRelics
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		return fmt.Errorf("raid '%s' not found", raidID)
	}

	var raids []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		Type        string `json:"issue_type"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return fmt.Errorf("parsing raid data: %w", err)
	}

	if len(raids) == 0 {
		return fmt.Errorf("raid '%s' not found", raidID)
	}

	raid := raids[0]

	// Verify it's actually a raid type
	if raid.Type != "raid" {
		return fmt.Errorf("'%s' is not a raid (type: %s)", raidID, raid.Type)
	}

	// Idempotent: if already closed, just report it
	if raid.Status == "closed" {
		fmt.Printf("%s Raid %s is already closed\n", style.Dim.Render("○"), raidID)
		return nil
	}

	// Build close reason
	reason := raidCloseReason
	if reason == "" {
		reason = "Manually closed"
	}

	// Close the raid
	closeArgs := []string{"close", raidID, "-r", reason}
	closeCmd := exec.Command("rl", closeArgs...)
	closeCmd.Dir = townRelics

	if err := closeCmd.Run(); err != nil {
		return fmt.Errorf("closing raid: %w", err)
	}

	fmt.Printf("%s Closed raid 🚚 %s: %s\n", style.Bold.Render("✓"), raidID, raid.Title)
	if raidCloseReason != "" {
		fmt.Printf("  Reason: %s\n", raidCloseReason)
	}

	// Send notification if --notify flag provided
	if raidCloseNotify != "" {
		sendCloseNotification(raidCloseNotify, raidID, raid.Title, reason)
	} else {
		// Check if raid has a notify address in description
		notifyRaidCompletion(townRelics, raidID, raid.Title)
	}

	return nil
}

// sendCloseNotification sends a notification about raid closure.
func sendCloseNotification(addr, raidID, title, reason string) {
	subject := fmt.Sprintf("🚚 Raid closed: %s", title)
	body := fmt.Sprintf("Raid %s has been closed.\n\nReason: %s", raidID, reason)

	mailArgs := []string{"drums", "send", addr, "-s", subject, "-m", body}
	mailCmd := exec.Command("hd", mailArgs...)
	if err := mailCmd.Run(); err != nil {
		style.PrintWarning("couldn't send notification: %v", err)
	} else {
		fmt.Printf("  Notified: %s\n", addr)
	}
}

// strandedRaidInfo holds info about a stranded raid.
type strandedRaidInfo struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	ReadyCount  int      `json:"ready_count"`
	ReadyIssues []string `json:"ready_issues"`
}

// readyIssueInfo holds info about a ready (stranded) issue.
type readyIssueInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Priority string `json:"priority"`
}

func runRaidStranded(cmd *cobra.Command, args []string) error {
	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	stranded, err := findStrandedRaids(townRelics)
	if err != nil {
		return err
	}

	if raidStrandedJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(stranded)
	}

	if len(stranded) == 0 {
		fmt.Println("No stranded raids found.")
		return nil
	}

	fmt.Printf("%s Found %d stranded raid(s):\n\n", style.Warning.Render("⚠"), len(stranded))
	for _, s := range stranded {
		fmt.Printf("  🚚 %s: %s\n", s.ID, s.Title)
		fmt.Printf("     Ready issues: %d\n", s.ReadyCount)
		for _, issueID := range s.ReadyIssues {
			fmt.Printf("       • %s\n", issueID)
		}
		fmt.Println()
	}

	fmt.Println("To feed stranded raids, run:")
	for _, s := range stranded {
		fmt.Printf("  hd charge totem-raid-feed shaman/dogs --var raid=%s\n", s.ID)
	}

	return nil
}

// findStrandedRaids finds raids with ready work but no workers.
func findStrandedRaids(townRelics string) ([]strandedRaidInfo, error) {
	var stranded []strandedRaidInfo

	// Get blocked issues (we need this to filter out blocked issues)
	blockedIssues := getBlockedIssueIDs()

	// List all open raids
	listArgs := []string{"list", "--type=raid", "--status=open", "--json"}
	listCmd := exec.Command("rl", listArgs...)
	listCmd.Dir = townRelics
	var stdout bytes.Buffer
	listCmd.Stdout = &stdout

	if err := listCmd.Run(); err != nil {
		return nil, fmt.Errorf("listing raids: %w", err)
	}

	var raids []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return nil, fmt.Errorf("parsing raid list: %w", err)
	}

	// Check each raid for stranded state
	for _, raid := range raids {
		tracked := getTrackedIssues(townRelics, raid.ID)
		if len(tracked) == 0 {
			continue
		}

		// Find ready issues (open, not blocked, no live assignee)
		var readyIssues []string
		for _, t := range tracked {
			if isReadyIssue(t, blockedIssues) {
				readyIssues = append(readyIssues, t.ID)
			}
		}

		if len(readyIssues) > 0 {
			stranded = append(stranded, strandedRaidInfo{
				ID:          raid.ID,
				Title:       raid.Title,
				ReadyCount:  len(readyIssues),
				ReadyIssues: readyIssues,
			})
		}
	}

	return stranded, nil
}

// getBlockedIssueIDs returns a set of issue IDs that are currently blocked.
func getBlockedIssueIDs() map[string]bool {
	blocked := make(map[string]bool)

	// Run rl blocked --json
	blockedCmd := exec.Command("rl", "blocked", "--json")
	var stdout bytes.Buffer
	blockedCmd.Stdout = &stdout

	if err := blockedCmd.Run(); err != nil {
		return blocked // Return empty set on error
	}

	var issues []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &issues); err != nil {
		return blocked
	}

	for _, issue := range issues {
		blocked[issue.ID] = true
	}

	return blocked
}

// isReadyIssue checks if an issue is ready for dispatch (stranded).
// An issue is ready if:
// - status = "open" (not in_progress, closed, bannered)
// - not in blocked set
// - no assignee OR assignee session is dead
func isReadyIssue(t trackedIssueInfo, blockedIssues map[string]bool) bool {
	// Must be open status (not in_progress, closed, bannered)
	if t.Status != "open" {
		return false
	}

	// Must not be blocked
	if blockedIssues[t.ID] {
		return false
	}

	// Check assignee
	if t.Assignee == "" {
		return true // No assignee = ready
	}

	// Has assignee - check if session is alive
	// Use the shared assigneeToSessionName from warband.go
	sessionName, _ := assigneeToSessionName(t.Assignee)
	if sessionName == "" {
		return true // Can't determine session = treat as ready
	}

	// Check if tmux session exists
	checkCmd := exec.Command("tmux", "has-session", "-t", sessionName)
	if err := checkCmd.Run(); err != nil {
		return true // Session doesn't exist = ready
	}

	return false // Session exists = not ready (worker is active)
}

// checkAndCloseCompletedRaids finds open raids where all tracked issues are closed
// and auto-closes them. Returns the list of raids that were closed.
func checkAndCloseCompletedRaids(townRelics string) ([]struct{ ID, Title string }, error) {
	var closed []struct{ ID, Title string }

	// List all open raids
	listArgs := []string{"list", "--type=raid", "--status=open", "--json"}
	listCmd := exec.Command("rl", listArgs...)
	listCmd.Dir = townRelics
	var stdout bytes.Buffer
	listCmd.Stdout = &stdout

	if err := listCmd.Run(); err != nil {
		return nil, fmt.Errorf("listing raids: %w", err)
	}

	var raids []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return nil, fmt.Errorf("parsing raid list: %w", err)
	}

	// Check each raid
	for _, raid := range raids {
		tracked := getTrackedIssues(townRelics, raid.ID)
		if len(tracked) == 0 {
			continue // No tracked issues, nothing to check
		}

		// Check if all tracked issues are closed
		allClosed := true
		for _, t := range tracked {
			if t.Status != "closed" && t.Status != "tombstone" {
				allClosed = false
				break
			}
		}

		if allClosed {
			// Close the raid
			closeArgs := []string{"close", raid.ID, "-r", "All tracked issues completed"}
			closeCmd := exec.Command("rl", closeArgs...)
			closeCmd.Dir = townRelics

			if err := closeCmd.Run(); err != nil {
				style.PrintWarning("couldn't close raid %s: %v", raid.ID, err)
				continue
			}

			closed = append(closed, struct{ ID, Title string }{raid.ID, raid.Title})

			// Check if raid has notify address and send notification
			notifyRaidCompletion(townRelics, raid.ID, raid.Title)
		}
	}

	return closed, nil
}

// notifyRaidCompletion sends notifications to owner and any notify addresses.
func notifyRaidCompletion(townRelics, raidID, title string) {
	// Get raid description to find owner and notify addresses
	showArgs := []string{"show", raidID, "--json"}
	showCmd := exec.Command("rl", showArgs...)
	showCmd.Dir = townRelics
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		return
	}

	var raids []struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil || len(raids) == 0 {
		return
	}

	// Parse owner and notify addresses from description
	desc := raids[0].Description
	notified := make(map[string]bool) // Track who we've notified to avoid duplicates

	for _, line := range strings.Split(desc, "\n") {
		var addr string
		if strings.HasPrefix(line, "Owner: ") {
			addr = strings.TrimPrefix(line, "Owner: ")
		} else if strings.HasPrefix(line, "Notify: ") {
			addr = strings.TrimPrefix(line, "Notify: ")
		}

		if addr != "" && !notified[addr] {
			// Send notification via hd drums
			mailArgs := []string{"drums", "send", addr,
				"-s", fmt.Sprintf("🚚 Raid landed: %s", title),
				"-m", fmt.Sprintf("Raid %s has completed.\n\nAll tracked issues are now closed.", raidID)}
			mailCmd := exec.Command("hd", mailArgs...)
			_ = mailCmd.Run() // Best effort, ignore errors
			notified[addr] = true
		}
	}
}

func runRaidStatus(cmd *cobra.Command, args []string) error {
	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	// If no ID provided, show all active raids
	if len(args) == 0 {
		return showAllRaidStatus(townRelics)
	}

	raidID := args[0]

	// Check if it's a numeric shortcut (e.g., "1" instead of "hq-cv-xyz")
	if n, err := strconv.Atoi(raidID); err == nil && n > 0 {
		resolved, err := resolveRaidNumber(townRelics, n)
		if err != nil {
			return err
		}
		raidID = resolved
	}

	// Get raid details
	showArgs := []string{"show", raidID, "--json"}
	showCmd := exec.Command("rl", showArgs...)
	showCmd.Dir = townRelics
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		return fmt.Errorf("raid '%s' not found", raidID)
	}

	// Parse raid data
	var raids []struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		Status      string   `json:"status"`
		Description string   `json:"description"`
		CreatedAt   string   `json:"created_at"`
		ClosedAt    string   `json:"closed_at,omitempty"`
		DependsOn   []string `json:"depends_on,omitempty"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return fmt.Errorf("parsing raid data: %w", err)
	}

	if len(raids) == 0 {
		return fmt.Errorf("raid '%s' not found", raidID)
	}

	raid := raids[0]

	// Get tracked issues by querying SQLite directly
	// (bd dep list doesn't properly show cross-warband external dependencies)
	type trackedIssue struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		Type      string `json:"dependency_type"`
		IssueType string `json:"issue_type"`
	}

	tracked := getTrackedIssues(townRelics, raidID)

	// Count completed
	completed := 0
	for _, t := range tracked {
		if t.Status == "closed" {
			completed++
		}
	}

	if raidStatusJSON {
		type jsonStatus struct {
			ID        string             `json:"id"`
			Title     string             `json:"title"`
			Status    string             `json:"status"`
			Tracked   []trackedIssueInfo `json:"tracked"`
			Completed int                `json:"completed"`
			Total     int                `json:"total"`
		}
		out := jsonStatus{
			ID:        raid.ID,
			Title:     raid.Title,
			Status:    raid.Status,
			Tracked:   tracked,
			Completed: completed,
			Total:     len(tracked),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	// Human-readable output
	fmt.Printf("🚚 %s %s\n\n", style.Bold.Render(raid.ID+":"), raid.Title)
	fmt.Printf("  Status:    %s\n", formatRaidStatus(raid.Status))
	fmt.Printf("  Progress:  %d/%d completed\n", completed, len(tracked))
	fmt.Printf("  Created:   %s\n", raid.CreatedAt)
	if raid.ClosedAt != "" {
		fmt.Printf("  Closed:    %s\n", raid.ClosedAt)
	}

	if len(tracked) > 0 {
		fmt.Printf("\n  %s\n", style.Bold.Render("Tracked Issues:"))
		for _, t := range tracked {
			// Status symbol: ✓ closed, ▶ in_progress/bannered, ○ other
			status := "○"
			switch t.Status {
			case "closed":
				status = "✓"
			case "in_progress", "bannered":
				status = "▶"
			}

			// Show assignee in brackets (extract short name from path like horde/raiders/goose -> goose)
			bracketContent := t.IssueType
			if t.Assignee != "" {
				parts := strings.Split(t.Assignee, "/")
				bracketContent = parts[len(parts)-1] // Last part of path
			} else if bracketContent == "" {
				bracketContent = "unassigned"
			}

			line := fmt.Sprintf("    %s %s: %s [%s]", status, t.ID, t.Title, bracketContent)
			if t.Worker != "" {
				workerDisplay := "@" + t.Worker
				if t.WorkerAge != "" {
					workerDisplay += fmt.Sprintf(" (%s)", t.WorkerAge)
				}
				line += fmt.Sprintf("  %s", style.Dim.Render(workerDisplay))
			}
			fmt.Println(line)
		}
	}

	return nil
}

func showAllRaidStatus(townRelics string) error {
	// List all raid-type issues
	listArgs := []string{"list", "--type=raid", "--status=open", "--json"}
	listCmd := exec.Command("rl", listArgs...)
	listCmd.Dir = townRelics
	var stdout bytes.Buffer
	listCmd.Stdout = &stdout

	if err := listCmd.Run(); err != nil {
		return fmt.Errorf("listing raids: %w", err)
	}

	var raids []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return fmt.Errorf("parsing raid list: %w", err)
	}

	if len(raids) == 0 {
		fmt.Println("No active raids.")
		fmt.Println("Create a raid with: hd raid create <name> [issues...]")
		return nil
	}

	if raidStatusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(raids)
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Active Raids"))
	for _, c := range raids {
		fmt.Printf("  🚚 %s: %s\n", c.ID, c.Title)
	}
	fmt.Printf("\nUse 'hd raid status <id>' for detailed status.\n")

	return nil
}

func runRaidList(cmd *cobra.Command, args []string) error {
	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	// List raid-type issues
	listArgs := []string{"list", "--type=raid", "--json"}
	if raidListStatus != "" {
		listArgs = append(listArgs, "--status="+raidListStatus)
	} else if raidListAll {
		listArgs = append(listArgs, "--all")
	}
	// Default (no flags) = open only (bd's default behavior)

	listCmd := exec.Command("rl", listArgs...)
	listCmd.Dir = townRelics
	var stdout bytes.Buffer
	listCmd.Stdout = &stdout

	if err := listCmd.Run(); err != nil {
		return fmt.Errorf("listing raids: %w", err)
	}

	var raids []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return fmt.Errorf("parsing raid list: %w", err)
	}

	if raidListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(raids)
	}

	if len(raids) == 0 {
		fmt.Println("No raids found.")
		fmt.Println("Create a raid with: hd raid create <name> [issues...]")
		return nil
	}

	// Tree view: show raids with their child issues
	if raidListTree {
		return printRaidTree(townRelics, raids)
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Raids"))
	for i, c := range raids {
		status := formatRaidStatus(c.Status)
		fmt.Printf("  %d. 🚚 %s: %s %s\n", i+1, c.ID, c.Title, status)
	}
	fmt.Printf("\nUse 'hd raid status <id>' or 'hd raid status <n>' for detailed view.\n")

	return nil
}

// printRaidTree displays raids with their child issues in a tree format.
func printRaidTree(townRelics string, raids []struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}) error {
	for _, c := range raids {
		// Get tracked issues for this raid
		tracked := getTrackedIssues(townRelics, c.ID)

		// Count completed
		completed := 0
		for _, t := range tracked {
			if t.Status == "closed" {
				completed++
			}
		}

		// Print raid header with progress
		total := len(tracked)
		progress := ""
		if total > 0 {
			progress = fmt.Sprintf(" (%d/%d)", completed, total)
		}
		fmt.Printf("🚚 %s: %s%s\n", c.ID, c.Title, progress)

		// Print tracked issues as tree children
		for i, t := range tracked {
			// Determine tree connector
			isLast := i == len(tracked)-1
			connector := "├──"
			if isLast {
				connector = "└──"
			}

			// Status symbol: ✓ closed, ▶ in_progress/bannered, ○ other
			status := "○"
			switch t.Status {
			case "closed":
				status = "✓"
			case "in_progress", "bannered":
				status = "▶"
			}

			fmt.Printf("%s %s %s: %s\n", connector, status, t.ID, t.Title)
		}

		// Add blank line between raids
		fmt.Println()
	}

	return nil
}

func formatRaidStatus(status string) string {
	switch status {
	case "open":
		return style.Warning.Render("●")
	case "closed":
		return style.Success.Render("✓")
	case "in_progress":
		return style.Info.Render("→")
	default:
		return status
	}
}

// trackedIssueInfo holds info about an issue being tracked by a raid.
type trackedIssueInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Type      string `json:"dependency_type"`
	IssueType string `json:"issue_type"`
	Assignee  string `json:"assignee,omitempty"`   // Assigned agent (e.g., horde/raiders/goose)
	Worker    string `json:"worker,omitempty"`     // Worker currently assigned (e.g., horde/nux)
	WorkerAge string `json:"worker_age,omitempty"` // How long worker has been on this issue
}

// getTrackedIssues queries SQLite directly to get issues tracked by a raid.
// This is needed because rl dep list doesn't properly show cross-warband external dependencies.
// Uses batched lookup to avoid N+1 subprocess calls.
func getTrackedIssues(townRelics, raidID string) []trackedIssueInfo {
	dbPath := filepath.Join(townRelics, "relics.db")

	// Query tracked dependencies from SQLite
	// Escape single quotes to prevent SQL injection
	safeRaidID := strings.ReplaceAll(raidID, "'", "''")
	queryCmd := exec.Command("sqlite3", "-json", dbPath,
		fmt.Sprintf(`SELECT depends_on_id, type FROM dependencies WHERE issue_id = '%s' AND type = 'tracks'`, safeRaidID))

	var stdout bytes.Buffer
	queryCmd.Stdout = &stdout
	if err := queryCmd.Run(); err != nil {
		return nil
	}

	var deps []struct {
		DependsOnID string `json:"depends_on_id"`
		Type        string `json:"type"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &deps); err != nil {
		return nil
	}

	// First pass: collect all issue IDs (normalized from external refs)
	issueIDs := make([]string, 0, len(deps))
	idToDepType := make(map[string]string)
	for _, dep := range deps {
		issueID := dep.DependsOnID

		// Handle external reference format: external:warband:issue-id
		if strings.HasPrefix(issueID, "external:") {
			parts := strings.SplitN(issueID, ":", 3)
			if len(parts) == 3 {
				issueID = parts[2] // Extract the actual issue ID
			}
		}

		issueIDs = append(issueIDs, issueID)
		idToDepType[issueID] = dep.Type
	}

	// Single batch call to get all issue details
	detailsMap := getIssueDetailsBatch(issueIDs)

	// Get workers for these issues (only for non-closed issues)
	openIssueIDs := make([]string, 0, len(issueIDs))
	for _, id := range issueIDs {
		if details, ok := detailsMap[id]; ok && details.Status != "closed" {
			openIssueIDs = append(openIssueIDs, id)
		}
	}
	workersMap := getWorkersForIssues(openIssueIDs)

	// Second pass: build result using the batch lookup
	var tracked []trackedIssueInfo
	for _, issueID := range issueIDs {
		info := trackedIssueInfo{
			ID:   issueID,
			Type: idToDepType[issueID],
		}

		if details, ok := detailsMap[issueID]; ok {
			info.Title = details.Title
			info.Status = details.Status
			info.IssueType = details.IssueType
			info.Assignee = details.Assignee
		} else {
			info.Title = "(external)"
			info.Status = "unknown"
		}

		// Add worker info if available
		if worker, ok := workersMap[issueID]; ok {
			info.Worker = worker.Worker
			info.WorkerAge = worker.Age
		}

		tracked = append(tracked, info)
	}

	return tracked
}

// issueDetails holds basic issue info.
type issueDetails struct {
	ID        string
	Title     string
	Status    string
	IssueType string
	Assignee  string
}

// getIssueDetailsBatch fetches details for multiple issues in a single rl show call.
// Returns a map from issue ID to details. Missing/invalid issues are omitted from the map.
func getIssueDetailsBatch(issueIDs []string) map[string]*issueDetails {
	result := make(map[string]*issueDetails)
	if len(issueIDs) == 0 {
		return result
	}

	// Build args: rl --no-daemon show id1 id2 id3 ... --json
	// Use --no-daemon to ensure fresh data (avoid stale cache from daemon)
	args := append([]string{"--no-daemon", "show"}, issueIDs...)
	args = append(args, "--json")

	showCmd := exec.Command("rl", args...)
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		// Batch failed - fall back to individual lookups for robustness
		// This handles cases where some IDs are invalid/missing
		for _, id := range issueIDs {
			if details := getIssueDetails(id); details != nil {
				result[id] = details
			}
		}
		return result
	}

	var issues []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		IssueType string `json:"issue_type"`
		Assignee  string `json:"assignee"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &issues); err != nil {
		return result
	}

	for _, issue := range issues {
		result[issue.ID] = &issueDetails{
			ID:        issue.ID,
			Title:     issue.Title,
			Status:    issue.Status,
			IssueType: issue.IssueType,
			Assignee:  issue.Assignee,
		}
	}

	return result
}

// getIssueDetails fetches issue details by trying to show it via bd.
// Prefer getIssueDetailsBatch for multiple issues to avoid N+1 subprocess calls.
func getIssueDetails(issueID string) *issueDetails {
	// Use rl show with routing - it should find the issue in the right warband
	// Use --no-daemon to ensure fresh data (avoid stale cache)
	showCmd := exec.Command("rl", "--no-daemon", "show", issueID, "--json")
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		return nil
	}
	// Handle rl --no-daemon exit 0 bug: empty stdout means not found
	if stdout.Len() == 0 {
		return nil
	}

	var issues []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		IssueType string `json:"issue_type"`
		Assignee  string `json:"assignee"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &issues); err != nil || len(issues) == 0 {
		return nil
	}

	return &issueDetails{
		ID:        issues[0].ID,
		Title:     issues[0].Title,
		Status:    issues[0].Status,
		IssueType: issues[0].IssueType,
		Assignee:  issues[0].Assignee,
	}
}

// workerInfo holds info about a worker assigned to an issue.
type workerInfo struct {
	Worker string // Agent identity (e.g., horde/nux)
	Age    string // How long assigned (e.g., "12m")
}

// getWorkersForIssues finds workers currently assigned to the given issues.
// Returns a map from issue ID to worker info.
//
// Optimized to batch queries per warband (O(R) instead of O(N×R)) and
// parallelize across warbands.
func getWorkersForIssues(issueIDs []string) map[string]*workerInfo {
	result := make(map[string]*workerInfo)
	if len(issueIDs) == 0 {
		return result
	}

	// Find encampment root
	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		return result
	}

	// Discover warbands with relics databases
	rigDirs, _ := filepath.Glob(filepath.Join(townRoot, "*", "raiders"))
	var relicsDBS []string
	for _, raidersDir := range rigDirs {
		rigDir := filepath.Dir(raidersDir)
		relicsDB := filepath.Join(rigDir, "warchief", "warband", ".relics", "relics.db")
		if _, err := os.Stat(relicsDB); err == nil {
			relicsDBS = append(relicsDBS, relicsDB)
		}
	}

	if len(relicsDBS) == 0 {
		return result
	}

	// Build the IN clause with properly escaped issue IDs
	var quotedIDs []string
	for _, id := range issueIDs {
		safeID := strings.ReplaceAll(id, "'", "''")
		quotedIDs = append(quotedIDs, fmt.Sprintf("'%s'", safeID))
	}
	inClause := strings.Join(quotedIDs, ", ")

	// Batch query: fetch all matching agents in one query per warband
	query := fmt.Sprintf(
		`SELECT id, banner_bead, last_activity FROM issues WHERE issue_type = 'agent' AND status = 'open' AND banner_bead IN (%s)`,
		inClause)

	// Query all warbands in parallel
	type rigResult struct {
		agents []struct {
			ID           string `json:"id"`
			BannerBead     string `json:"banner_bead"`
			LastActivity string `json:"last_activity"`
		}
	}

	resultChan := make(chan rigResult, len(relicsDBS))
	var wg sync.WaitGroup

	for _, relicsDB := range relicsDBS {
		wg.Add(1)
		go func(db string) {
			defer wg.Done()

			queryCmd := exec.Command("sqlite3", "-json", db, query)
			var stdout bytes.Buffer
			queryCmd.Stdout = &stdout
			if err := queryCmd.Run(); err != nil {
				resultChan <- rigResult{}
				return
			}

			var rr rigResult
			if err := json.Unmarshal(stdout.Bytes(), &rr.agents); err != nil {
				resultChan <- rigResult{}
				return
			}
			resultChan <- rr
		}(relicsDB)
	}

	// Wait for all queries to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results from all warbands
	for rr := range resultChan {
		for _, agent := range rr.agents {
			// Skip if we already found a worker for this issue
			if _, ok := result[agent.BannerBead]; ok {
				continue
			}

			// Parse agent ID to get worker identity
			workerID := parseWorkerFromAgentBead(agent.ID)
			if workerID == "" {
				continue
			}

			// Calculate age from last_activity
			age := ""
			if agent.LastActivity != "" {
				if t, err := time.Parse(time.RFC3339, agent.LastActivity); err == nil {
					age = formatWorkerAge(time.Since(t))
				}
			}

			result[agent.BannerBead] = &workerInfo{
				Worker: workerID,
				Age:    age,
			}
		}
	}

	return result
}

// parseWorkerFromAgentBead extracts worker identity from agent bead ID.
// Input: "hd-horde-raider-nux" -> Output: "horde/nux"
// Input: "hd-relics-clan-amber" -> Output: "relics/clan/amber"
func parseWorkerFromAgentBead(agentID string) string {
	// Remove prefix (gt-, bd-, etc.)
	parts := strings.Split(agentID, "-")
	if len(parts) < 3 {
		return ""
	}

	// Skip prefix
	parts = parts[1:]

	// Reconstruct as path
	return strings.Join(parts, "/")
}

// formatWorkerAge formats a duration as a short string (e.g., "5m", "2h", "1d")
func formatWorkerAge(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// runRaidTUI launches the interactive raid TUI.
func runRaidTUI() error {
	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	m := raid.New(townRelics)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// resolveRaidNumber converts a numeric shortcut (1, 2, 3...) to a raid ID.
// Numbers correspond to the order shown in 'hd raid list'.
func resolveRaidNumber(townRelics string, n int) (string, error) {
	// Get raid list (same query as runRaidList)
	listArgs := []string{"list", "--type=raid", "--json"}
	listCmd := exec.Command("rl", listArgs...)
	listCmd.Dir = townRelics
	var stdout bytes.Buffer
	listCmd.Stdout = &stdout

	if err := listCmd.Run(); err != nil {
		return "", fmt.Errorf("listing raids: %w", err)
	}

	var raids []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return "", fmt.Errorf("parsing raid list: %w", err)
	}

	if n < 1 || n > len(raids) {
		return "", fmt.Errorf("raid %d not found (have %d raids)", n, len(raids))
	}

	return raids[n-1].ID, nil
}
