package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
)

// Raider identity command flags
var (
	raiderIdentityListJSON    bool
	raiderIdentityShowJSON    bool
	raiderIdentityRemoveForce bool
)

var raiderIdentityCmd = &cobra.Command{
	Use:     "identity",
	Aliases: []string{"id"},
	Short:   "Manage raider identities",
	Long: `Manage raider identity relics in warbands.

Identity relics track raider metadata, CV history, and lifecycle state.
Use subcommands to create, list, show, rename, or remove identities.`,
	RunE: requireSubcommand,
}

var raiderIdentityAddCmd = &cobra.Command{
	Use:   "add <warband> [name]",
	Short: "Create an identity bead for a raider",
	Long: `Create an identity bead for a raider in a warband.

If name is not provided, a name will be generated from the warband's name pool.

The identity bead tracks:
  - Role type (raider)
  - Warband assignment
  - Agent state
  - Hook bead (current work)
  - Cleanup status

Example:
  hd raider identity add horde Toast
  hd raider identity add horde  # auto-generate name`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runRaiderIdentityAdd,
}

var raiderIdentityListCmd = &cobra.Command{
	Use:   "list <warband>",
	Short: "List raider identity relics in a warband",
	Long: `List all raider identity relics in a warband.

Shows:
  - Raider name
  - Agent state
  - Current hook (if any)
  - Whether worktree exists

Example:
  hd raider identity list horde
  hd raider identity list horde --json`,
	Args: cobra.ExactArgs(1),
	RunE: runRaiderIdentityList,
}

var raiderIdentityShowCmd = &cobra.Command{
	Use:   "show <warband> <name>",
	Short: "Show raider identity with CV summary",
	Long: `Show detailed identity information for a raider including work history.

Displays:
  - Identity bead ID and creation date
  - Session count
  - Completion statistics (issues completed, failed, abandoned)
  - Language breakdown from file extensions
  - Work type breakdown (feat, fix, refactor, etc.)
  - Recent work list with relative timestamps

Examples:
  hd raider identity show horde Toast
  hd raider identity show horde Toast --json`,
	Args: cobra.ExactArgs(2),
	RunE: runRaiderIdentityShow,
}

var raiderIdentityRenameCmd = &cobra.Command{
	Use:   "rename <warband> <old-name> <new-name>",
	Short: "Rename a raider identity (preserves CV)",
	Long: `Rename a raider identity bead, preserving CV history.

The rename:
  1. Creates a new identity bead with the new name
  2. Copies CV history links to the new bead
  3. Closes the old bead with a reference to the new one

Safety checks:
  - Old identity must exist
  - New name must not already exist
  - Raider session must not be running

Example:
  hd raider identity rename horde Toast Imperator`,
	Args: cobra.ExactArgs(3),
	RunE: runRaiderIdentityRename,
}

var raiderIdentityRemoveCmd = &cobra.Command{
	Use:   "remove <warband> <name>",
	Short: "Remove a raider identity",
	Long: `Remove a raider identity bead.

Safety checks:
  - No active tmux session
  - No work on hook (unless using --force)
  - Warns if CV exists

Use --force to bypass safety checks.

Example:
  hd raider identity remove horde Toast
  hd raider identity remove horde Toast --force`,
	Args: cobra.ExactArgs(2),
	RunE: runRaiderIdentityRemove,
}

func init() {
	// List flags
	raiderIdentityListCmd.Flags().BoolVar(&raiderIdentityListJSON, "json", false, "Output as JSON")

	// Show flags
	raiderIdentityShowCmd.Flags().BoolVar(&raiderIdentityShowJSON, "json", false, "Output as JSON")

	// Remove flags
	raiderIdentityRemoveCmd.Flags().BoolVarP(&raiderIdentityRemoveForce, "force", "f", false, "Force removal, bypassing safety checks")

	// Add subcommands to identity
	raiderIdentityCmd.AddCommand(raiderIdentityAddCmd)
	raiderIdentityCmd.AddCommand(raiderIdentityListCmd)
	raiderIdentityCmd.AddCommand(raiderIdentityShowCmd)
	raiderIdentityCmd.AddCommand(raiderIdentityRenameCmd)
	raiderIdentityCmd.AddCommand(raiderIdentityRemoveCmd)

	// Add identity to raider command
	raiderCmd.AddCommand(raiderIdentityCmd)
}

// IdentityInfo holds identity bead information for display.
type IdentityInfo struct {
	Warband            string `json:"warband"`
	Name           string `json:"name"`
	BeadID         string `json:"bead_id"`
	AgentState     string `json:"agent_state,omitempty"`
	BannerBead       string `json:"banner_bead,omitempty"`
	CleanupStatus  string `json:"cleanup_status,omitempty"`
	WorktreeExists bool   `json:"worktree_exists"`
	SessionRunning bool   `json:"session_running"`
}

// IdentityDetails holds detailed identity information for show command.
type IdentityDetails struct {
	IdentityInfo
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	CVRelics     []string `json:"cv_relics,omitempty"`
}

// CVSummary represents the CV/work history summary for a raider.
type CVSummary struct {
	Identity         string           `json:"identity"`
	Created          string           `json:"created,omitempty"`
	Sessions         int              `json:"sessions"`
	IssuesCompleted  int              `json:"issues_completed"`
	IssuesFailed     int              `json:"issues_failed"`
	IssuesAbandoned  int              `json:"issues_abandoned"`
	Languages        map[string]int   `json:"languages,omitempty"`
	WorkTypes        map[string]int   `json:"work_types,omitempty"`
	AvgCompletionMin int              `json:"avg_completion_minutes,omitempty"`
	FirstPassRate    float64          `json:"first_pass_rate,omitempty"`
	RecentWork       []RecentWorkItem `json:"recent_work,omitempty"`
}

// RecentWorkItem represents a recent work item in the CV.
type RecentWorkItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Type      string `json:"type,omitempty"`
	Completed string `json:"completed"`
	Ago       string `json:"ago"`
}

func runRaiderIdentityAdd(cmd *cobra.Command, args []string) error {
	rigName := args[0]
	var raiderName string

	if len(args) > 1 {
		raiderName = args[1]
	}

	// Get warband
	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	// Generate name if not provided
	if raiderName == "" {
		raiderGit := git.NewGit(r.Path)
		t := tmux.NewTmux()
		mgr := raider.NewManager(r, raiderGit, t)
		raiderName, err = mgr.AllocateName()
		if err != nil {
			return fmt.Errorf("generating raider name: %w", err)
		}
		fmt.Printf("Generated name: %s\n", raiderName)
	}

	// Check if identity already exists
	bd := relics.New(r.Path)
	beadID := relics.RaiderBeadID(rigName, raiderName)
	existingIssue, _, _ := bd.GetAgentBead(beadID)
	if existingIssue != nil && existingIssue.Status != "closed" {
		return fmt.Errorf("identity bead %s already exists", beadID)
	}

	// Create identity bead
	fields := &relics.AgentFields{
		RoleType:   "raider",
		Warband:        rigName,
		AgentState: "idle",
	}

	title := fmt.Sprintf("Raider %s in %s", raiderName, rigName)
	issue, err := bd.CreateOrReopenAgentBead(beadID, title, fields)
	if err != nil {
		return fmt.Errorf("creating identity bead: %w", err)
	}

	fmt.Printf("%s Created identity bead: %s\n", style.SuccessPrefix, issue.ID)
	fmt.Printf("  Raider: %s\n", raiderName)
	fmt.Printf("  Warband:     %s\n", rigName)

	return nil
}

func runRaiderIdentityList(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	// Get warband
	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	// Get all agent relics
	bd := relics.New(r.Path)
	agentRelics, err := bd.ListAgentRelics()
	if err != nil {
		return fmt.Errorf("listing agent relics: %w", err)
	}

	// Filter for raider relics in this warband
	identities := []IdentityInfo{} // Initialize to empty slice (not nil) for JSON
	t := tmux.NewTmux()
	raiderMgr := raider.NewSessionManager(t, r)

	for id, issue := range agentRelics {
		// Parse the bead ID to check if it's a raider for this warband
		beadRig, role, name, ok := relics.ParseAgentBeadID(id)
		if !ok || role != "raider" || beadRig != rigName {
			continue
		}

		// Skip closed relics
		if issue.Status == "closed" {
			continue
		}

		fields := relics.ParseAgentFields(issue.Description)

		// Check if worktree exists
		worktreeExists := false
		mgr := raider.NewManager(r, nil, t)
		if p, err := mgr.Get(name); err == nil && p != nil {
			worktreeExists = true
		}

		// Check if session is running
		sessionRunning, _ := raiderMgr.IsRunning(name)

		info := IdentityInfo{
			Warband:            rigName,
			Name:           name,
			BeadID:         id,
			AgentState:     fields.AgentState,
			BannerBead:       issue.BannerBead,
			CleanupStatus:  fields.CleanupStatus,
			WorktreeExists: worktreeExists,
			SessionRunning: sessionRunning,
		}
		if info.BannerBead == "" {
			info.BannerBead = fields.BannerBead
		}
		identities = append(identities, info)
	}

	// JSON output
	if raiderIdentityListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(identities)
	}

	// Human-readable output
	if len(identities) == 0 {
		fmt.Printf("No raider identities found in %s.\n", rigName)
		return nil
	}

	fmt.Printf("%s\n\n", style.Bold.Render(fmt.Sprintf("Raider Identities in %s", rigName)))

	for _, info := range identities {
		// Status indicators
		sessionIcon := style.Dim.Render("○")
		if info.SessionRunning {
			sessionIcon = style.Success.Render("●")
		}

		worktreeIcon := ""
		if info.WorktreeExists {
			worktreeIcon = " " + style.Dim.Render("[worktree]")
		}

		// Agent state with color
		stateStr := info.AgentState
		if stateStr == "" {
			stateStr = "unknown"
		}
		switch stateStr {
		case "working":
			stateStr = style.Info.Render(stateStr)
		case "done":
			stateStr = style.Success.Render(stateStr)
		case "stuck":
			stateStr = style.Warning.Render(stateStr)
		default:
			stateStr = style.Dim.Render(stateStr)
		}

		fmt.Printf("  %s %s  %s%s\n", sessionIcon, style.Bold.Render(info.Name), stateStr, worktreeIcon)

		if info.BannerBead != "" {
			fmt.Printf("    Hook: %s\n", style.Dim.Render(info.BannerBead))
		}
	}

	fmt.Printf("\n%d identity bead(s)\n", len(identities))
	return nil
}

func runRaiderIdentityShow(cmd *cobra.Command, args []string) error {
	rigName := args[0]
	raiderName := args[1]

	// Get warband
	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	// Get identity bead
	bd := relics.New(r.Path)
	beadID := relics.RaiderBeadID(rigName, raiderName)
	issue, fields, err := bd.GetAgentBead(beadID)
	if err != nil {
		return fmt.Errorf("getting identity bead: %w", err)
	}
	if issue == nil {
		return fmt.Errorf("identity bead %s not found", beadID)
	}

	// Check worktree and session
	t := tmux.NewTmux()
	raiderMgr := raider.NewSessionManager(t, r)
	mgr := raider.NewManager(r, nil, t)

	worktreeExists := false
	var clonePath string
	if p, err := mgr.Get(raiderName); err == nil && p != nil {
		worktreeExists = true
		clonePath = p.ClonePath
	}
	sessionRunning, _ := raiderMgr.IsRunning(raiderName)

	// Build CV summary with enhanced analytics
	cv := buildCVSummary(r.Path, rigName, raiderName, beadID, clonePath)

	// JSON output - include both identity details and CV
	if raiderIdentityShowJSON {
		output := struct {
			IdentityInfo
			Title       string     `json:"title"`
			CreatedAt   string     `json:"created_at,omitempty"`
			UpdatedAt   string     `json:"updated_at,omitempty"`
			CV          *CVSummary `json:"cv,omitempty"`
		}{
			IdentityInfo: IdentityInfo{
				Warband:            rigName,
				Name:           raiderName,
				BeadID:         beadID,
				AgentState:     fields.AgentState,
				BannerBead:       issue.BannerBead,
				CleanupStatus:  fields.CleanupStatus,
				WorktreeExists: worktreeExists,
				SessionRunning: sessionRunning,
			},
			Title:     issue.Title,
			CreatedAt: issue.CreatedAt,
			UpdatedAt: issue.UpdatedAt,
			CV:        cv,
		}
		if output.BannerBead == "" {
			output.BannerBead = fields.BannerBead
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	// Human-readable output
	fmt.Printf("\n%s %s/%s\n", style.Bold.Render("Identity:"), rigName, raiderName)
	fmt.Printf("  Bead ID:       %s\n", beadID)
	fmt.Printf("  Title:         %s\n", issue.Title)

	// Status
	sessionStr := style.Dim.Render("stopped")
	if sessionRunning {
		sessionStr = style.Success.Render("running")
	}
	fmt.Printf("  Session:       %s\n", sessionStr)

	worktreeStr := style.Dim.Render("no")
	if worktreeExists {
		worktreeStr = style.Success.Render("yes")
	}
	fmt.Printf("  Worktree:      %s\n", worktreeStr)

	// Agent state
	stateStr := fields.AgentState
	if stateStr == "" {
		stateStr = "unknown"
	}
	switch stateStr {
	case "working":
		stateStr = style.Info.Render(stateStr)
	case "done":
		stateStr = style.Success.Render(stateStr)
	case "stuck":
		stateStr = style.Warning.Render(stateStr)
	default:
		stateStr = style.Dim.Render(stateStr)
	}
	fmt.Printf("  Agent State:   %s\n", stateStr)

	// Hook
	bannerBead := issue.BannerBead
	if bannerBead == "" {
		bannerBead = fields.BannerBead
	}
	if bannerBead != "" {
		fmt.Printf("  Hook:          %s\n", bannerBead)
	} else {
		fmt.Printf("  Hook:          %s\n", style.Dim.Render("(empty)"))
	}

	// Cleanup status
	if fields.CleanupStatus != "" {
		fmt.Printf("  Cleanup:       %s\n", fields.CleanupStatus)
	}

	// Timestamps
	if issue.CreatedAt != "" {
		fmt.Printf("  Created:       %s\n", style.Dim.Render(issue.CreatedAt))
	}
	if issue.UpdatedAt != "" {
		fmt.Printf("  Updated:       %s\n", style.Dim.Render(issue.UpdatedAt))
	}

	// CV Summary section with enhanced analytics
	fmt.Printf("\n%s\n", style.Bold.Render("CV Summary:"))
	fmt.Printf("  Sessions:         %d\n", cv.Sessions)
	fmt.Printf("  Issues completed: %s\n", style.Success.Render(fmt.Sprintf("%d", cv.IssuesCompleted)))
	fmt.Printf("  Issues failed:    %s\n", formatCountStyled(cv.IssuesFailed, style.Error))
	fmt.Printf("  Issues abandoned: %s\n", formatCountStyled(cv.IssuesAbandoned, style.Warning))

	// Language stats
	if len(cv.Languages) > 0 {
		fmt.Printf("\n  %s %s\n", style.Bold.Render("Languages:"), formatLanguageStats(cv.Languages))
	}

	// Work type stats
	if len(cv.WorkTypes) > 0 {
		fmt.Printf("  %s     %s\n", style.Bold.Render("Types:"), formatWorkTypeStats(cv.WorkTypes))
	}

	// Performance metrics
	if cv.AvgCompletionMin > 0 {
		fmt.Printf("\n  Avg completion time: %d minutes\n", cv.AvgCompletionMin)
	}
	if cv.FirstPassRate > 0 {
		fmt.Printf("  First-pass success:  %.0f%%\n", cv.FirstPassRate*100)
	}

	// Recent work
	if len(cv.RecentWork) > 0 {
		fmt.Printf("\n%s\n", style.Bold.Render("Recent work:"))
		for _, work := range cv.RecentWork {
			typeStr := ""
			if work.Type != "" {
				typeStr = work.Type + ": "
			}
			title := work.Title
			if len(title) > 40 {
				title = title[:37] + "..."
			}
			fmt.Printf("  %-10s %s%s  %s\n", work.ID, typeStr, title, style.Dim.Render(work.Ago))
		}
	}

	fmt.Println()
	return nil
}

func runRaiderIdentityRename(cmd *cobra.Command, args []string) error {
	rigName := args[0]
	oldName := args[1]
	newName := args[2]

	// Validate names
	if oldName == newName {
		return fmt.Errorf("old and new names are the same")
	}

	// Get warband
	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	bd := relics.New(r.Path)
	oldBeadID := relics.RaiderBeadID(rigName, oldName)
	newBeadID := relics.RaiderBeadID(rigName, newName)

	// Check old identity exists
	oldIssue, oldFields, err := bd.GetAgentBead(oldBeadID)
	if err != nil {
		return fmt.Errorf("getting old identity bead: %w", err)
	}
	if oldIssue == nil || oldIssue.Status == "closed" {
		return fmt.Errorf("identity bead %s not found or already closed", oldBeadID)
	}

	// Check new identity doesn't exist
	newIssue, _, _ := bd.GetAgentBead(newBeadID)
	if newIssue != nil && newIssue.Status != "closed" {
		return fmt.Errorf("identity bead %s already exists", newBeadID)
	}

	// Safety check: no active session
	t := tmux.NewTmux()
	raiderMgr := raider.NewSessionManager(t, r)
	running, _ := raiderMgr.IsRunning(oldName)
	if running {
		return fmt.Errorf("cannot rename: raider session %s is running", oldName)
	}

	// Create new identity bead with inherited fields
	newFields := &relics.AgentFields{
		RoleType:      "raider",
		Warband:           rigName,
		AgentState:    oldFields.AgentState,
		CleanupStatus: oldFields.CleanupStatus,
	}

	newTitle := fmt.Sprintf("Raider %s in %s", newName, rigName)
	_, err = bd.CreateOrReopenAgentBead(newBeadID, newTitle, newFields)
	if err != nil {
		return fmt.Errorf("creating new identity bead: %w", err)
	}

	// Close old bead with reference to new one
	closeReason := fmt.Sprintf("renamed to %s", newBeadID)
	if err := bd.CloseWithReason(closeReason, oldBeadID); err != nil {
		// Try to clean up new bead
		_ = bd.CloseWithReason("rename failed", newBeadID)
		return fmt.Errorf("closing old identity bead: %w", err)
	}

	fmt.Printf("%s Renamed identity:\n", style.SuccessPrefix)
	fmt.Printf("  Old: %s\n", oldBeadID)
	fmt.Printf("  New: %s\n", newBeadID)
	fmt.Printf("\n%s Note: If a worktree exists for %s, you'll need to recreate it with the new name.\n",
		style.Warning.Render("⚠"), oldName)

	return nil
}

func runRaiderIdentityRemove(cmd *cobra.Command, args []string) error {
	rigName := args[0]
	raiderName := args[1]

	// Get warband
	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	bd := relics.New(r.Path)
	beadID := relics.RaiderBeadID(rigName, raiderName)

	// Check identity exists
	issue, fields, err := bd.GetAgentBead(beadID)
	if err != nil {
		return fmt.Errorf("getting identity bead: %w", err)
	}
	if issue == nil {
		return fmt.Errorf("identity bead %s not found", beadID)
	}
	if issue.Status == "closed" {
		return fmt.Errorf("identity bead %s is already closed", beadID)
	}

	// Safety checks (unless --force)
	if !raiderIdentityRemoveForce {
		var reasons []string

		// Check for active session
		t := tmux.NewTmux()
		raiderMgr := raider.NewSessionManager(t, r)
		running, _ := raiderMgr.IsRunning(raiderName)
		if running {
			reasons = append(reasons, "session is running")
		}

		// Check for work on hook
		bannerBead := issue.BannerBead
		if bannerBead == "" && fields != nil {
			bannerBead = fields.BannerBead
		}
		if bannerBead != "" {
			// Check if bannered bead is still open
			hookedIssue, _ := bd.Show(bannerBead)
			if hookedIssue != nil && hookedIssue.Status != "closed" {
				reasons = append(reasons, fmt.Sprintf("has work on hook (%s)", bannerBead))
			}
		}

		if len(reasons) > 0 {
			fmt.Printf("%s Cannot remove identity %s:\n", style.Error.Render("Error:"), beadID)
			for _, r := range reasons {
				fmt.Printf("  - %s\n", r)
			}
			fmt.Println("\nUse --force to bypass safety checks.")
			return fmt.Errorf("safety checks failed")
		}

		// Warn if CV exists
		assignee := fmt.Sprintf("%s/%s", rigName, raiderName)
		cvRelics, _ := bd.ListByAssignee(assignee)
		cvCount := 0
		for _, cv := range cvRelics {
			if cv.ID != beadID && cv.Status == "closed" {
				cvCount++
			}
		}
		if cvCount > 0 {
			fmt.Printf("%s Warning: This raider has %d completed work item(s) in CV.\n",
				style.Warning.Render("⚠"), cvCount)
		}
	}

	// Close the identity bead
	if err := bd.CloseWithReason("removed via hd raider identity remove", beadID); err != nil {
		return fmt.Errorf("closing identity bead: %w", err)
	}

	fmt.Printf("%s Removed identity bead: %s\n", style.SuccessPrefix, beadID)
	return nil
}

// buildCVSummary constructs the CV summary for a raider.
// Returns a partial CV on errors rather than failing - CV data is best-effort.
func buildCVSummary(rigPath, rigName, raiderName, identityBeadID, clonePath string) *CVSummary {
	cv := &CVSummary{
		Identity:   identityBeadID,
		Languages:  make(map[string]int),
		WorkTypes:  make(map[string]int),
		RecentWork: []RecentWorkItem{},
	}

	// Use clonePath for relics queries (has proper redirect setup)
	// Fall back to rigPath if clonePath is empty
	relicsQueryPath := clonePath
	if relicsQueryPath == "" {
		relicsQueryPath = rigPath
	}

	// Get agent bead info for creation date
	bd := relics.New(relicsQueryPath)
	agentBead, _, err := bd.GetAgentBead(identityBeadID)
	if err == nil && agentBead != nil {
		if agentBead.CreatedAt != "" && len(agentBead.CreatedAt) >= 10 {
			cv.Created = agentBead.CreatedAt[:10] // Just the date part
		}
	}

	// Count sessions from checkpoint files (session history)
	cv.Sessions = countRaiderSessions(rigPath, raiderName)

	// Query completed issues assigned to this raider
	assignee := fmt.Sprintf("%s/raiders/%s", rigName, raiderName)
	completedIssues, err := queryAssignedIssues(relicsQueryPath, assignee, "closed")
	if err == nil {
		cv.IssuesCompleted = len(completedIssues)

		// Extract work types from issue titles/types
		for _, issue := range completedIssues {
			workType := extractWorkType(issue.Title, issue.Type)
			if workType != "" {
				cv.WorkTypes[workType]++
			}

			// Add to recent work (limit to 5)
			if len(cv.RecentWork) < 5 {
				ago := formatRelativeTimeCV(issue.Updated)
				cv.RecentWork = append(cv.RecentWork, RecentWorkItem{
					ID:        issue.ID,
					Title:     issue.Title,
					Type:      workType,
					Completed: issue.Updated,
					Ago:       ago,
				})
			}
		}
	}

	// Query failed/escalated issues
	escalatedIssues, err := queryAssignedIssues(relicsQueryPath, assignee, "escalated")
	if err == nil {
		cv.IssuesFailed = len(escalatedIssues)
	}

	// Query abandoned issues (deferred)
	deferredIssues, err := queryAssignedIssues(relicsQueryPath, assignee, "deferred")
	if err == nil {
		cv.IssuesAbandoned = len(deferredIssues)
	}

	// Get language stats from git commits
	if clonePath != "" {
		langStats := getLanguageStats(clonePath)
		if len(langStats) > 0 {
			cv.Languages = langStats
		}
	}

	// Calculate first-pass success rate
	total := cv.IssuesCompleted + cv.IssuesFailed + cv.IssuesAbandoned
	if total > 0 {
		cv.FirstPassRate = float64(cv.IssuesCompleted) / float64(total)
	}

	return cv
}

// IssueInfo holds basic issue information for CV queries.
type IssueInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Type    string `json:"issue_type"`
	Status  string `json:"status"`
	Updated string `json:"updated_at"`
}

// queryAssignedIssues queries relics for issues assigned to a specific agent.
func queryAssignedIssues(rigPath, assignee, status string) ([]IssueInfo, error) {
	// Use rl list with filters
	args := []string{"list", "--assignee=" + assignee, "--json"}
	if status != "" {
		args = append(args, "--status="+status)
	}

	cmd := exec.Command("rl", args...)
	cmd.Dir = rigPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	if len(out) == 0 {
		return []IssueInfo{}, nil
	}

	var issues []IssueInfo
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, err
	}

	// Sort by updated date (most recent first)
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Updated > issues[j].Updated
	})

	return issues, nil
}

// extractWorkType extracts the work type from issue title or type.
func extractWorkType(title, issueType string) string {
	// Check explicit issue type first
	switch issueType {
	case "bug":
		return "fix"
	case "task", "feature":
		return "feat"
	case "epic":
		return "epic"
	}

	// Try to extract from conventional commit-style title
	title = strings.ToLower(title)
	prefixes := []string{"feat:", "fix:", "refactor:", "docs:", "test:", "chore:", "style:", "perf:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(title, prefix) {
			return strings.TrimSuffix(prefix, ":")
		}
	}

	// Try to infer from keywords
	if strings.Contains(title, "fix") || strings.Contains(title, "bug") {
		return "fix"
	}
	if strings.Contains(title, "add") || strings.Contains(title, "implement") || strings.Contains(title, "create") {
		return "feat"
	}
	if strings.Contains(title, "refactor") || strings.Contains(title, "cleanup") {
		return "refactor"
	}

	return ""
}

// getLanguageStats analyzes git history to determine language distribution.
func getLanguageStats(clonePath string) map[string]int {
	stats := make(map[string]int)

	// Get list of files changed in commits by this author
	// We use git log with --name-only to get file names
	cmd := exec.Command("git", "log", "--name-only", "--pretty=format:", "--diff-filter=ACMR", "-100")
	cmd.Dir = clonePath
	out, err := cmd.Output()
	if err != nil {
		return stats
	}

	// Count file extensions
	extCount := make(map[string]int)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ext := filepath.Ext(line)
		if ext != "" {
			extCount[ext]++
		}
	}

	// Map extensions to languages
	extToLang := map[string]string{
		".go":    "Go",
		".ts":    "TypeScript",
		".tsx":   "TypeScript",
		".js":    "JavaScript",
		".jsx":   "JavaScript",
		".py":    "Python",
		".rs":    "Rust",
		".java":  "Java",
		".rb":    "Ruby",
		".c":     "C",
		".cpp":   "C++",
		".h":     "C",
		".hpp":   "C++",
		".cs":    "C#",
		".swift": "Swift",
		".kt":    "Kotlin",
		".scala": "Scala",
		".php":   "PHP",
		".sh":    "Shell",
		".bash":  "Shell",
		".zsh":   "Shell",
		".md":    "Markdown",
		".yaml":  "YAML",
		".yml":   "YAML",
		".json":  "JSON",
		".toml":  "TOML",
		".sql":   "SQL",
		".html":  "HTML",
		".css":   "CSS",
		".scss":  "SCSS",
	}

	for ext, count := range extCount {
		if lang, ok := extToLang[ext]; ok {
			stats[lang] += count
		}
	}

	return stats
}

// formatRelativeTimeCV returns a human-readable relative time string for CV display.
func formatRelativeTimeCV(timestamp string) string {
	// Try RFC3339 format with timezone (ISO 8601)
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		// Try RFC3339Nano
		t, err = time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			// Try without timezone
			t, err = time.Parse("2006-01-02T15:04:05", timestamp)
			if err != nil {
				// Try alternative format
				t, err = time.Parse("2006-01-02 15:04:05", timestamp)
				if err != nil {
					// Try date only
					t, err = time.Parse("2006-01-02", timestamp)
					if err != nil {
						return ""
					}
				}
			}
		}
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		weeks := int(d.Hours() / 24 / 7)
		if weeks == 1 {
			return "1w ago"
		}
		return fmt.Sprintf("%dw ago", weeks)
	}
}

// formatCountStyled formats a count with appropriate styling using lipgloss.Style.
func formatCountStyled(count int, s lipgloss.Style) string {
	if count == 0 {
		return style.Dim.Render("0")
	}
	return s.Render(strconv.Itoa(count))
}

// countRaiderSessions counts the number of sessions from checkpoint files.
func countRaiderSessions(rigPath, raiderName string) int {
	// Look for checkpoint files in the raider's directory
	checkpointDir := filepath.Join(rigPath, "raiders", raiderName, ".checkpoints")
	entries, err := os.ReadDir(checkpointDir)
	if err != nil {
		// Also check at warband level
		checkpointDir = filepath.Join(rigPath, ".checkpoints")
		entries, err = os.ReadDir(checkpointDir)
		if err != nil {
			return 0
		}
	}

	// Count checkpoint files that contain this raider's name
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.Contains(entry.Name(), raiderName) {
			count++
		}
	}

	// If no checkpoint files found, return at least 1 if raider exists
	if count == 0 {
		return 1
	}
	return count
}

// formatLanguageStats formats language statistics for display.
func formatLanguageStats(langs map[string]int) string {
	// Sort by count descending
	type langCount struct {
		lang  string
		count int
	}
	var sorted []langCount
	for lang, count := range langs {
		sorted = append(sorted, langCount{lang, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	// Format top languages
	var parts []string
	for i, lc := range sorted {
		if i >= 3 { // Show top 3
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", lc.lang, lc.count))
	}
	return strings.Join(parts, ", ")
}

// formatWorkTypeStats formats work type statistics for display.
func formatWorkTypeStats(types map[string]int) string {
	// Sort by count descending
	type typeCount struct {
		typ   string
		count int
	}
	var sorted []typeCount
	for typ, count := range types {
		sorted = append(sorted, typeCount{typ, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	// Format all types
	var parts []string
	for _, tc := range sorted {
		parts = append(parts, fmt.Sprintf("%s (%d)", tc.typ, tc.count))
	}
	return strings.Join(parts, ", ")
}
