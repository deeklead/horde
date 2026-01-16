// Package cmd provides CLI commands for the hd tool.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

var (
	costsJSON    bool
	costsToday   bool
	costsWeek    bool
	costsByRole  bool
	costsByRig   bool
	costsVerbose bool

	// Record subcommand flags
	recordSession  string
	recordWorkItem string

	// Digest subcommand flags
	digestYesterday bool
	digestDate      string
	digestDryRun    bool

	// Migrate subcommand flags
	migrateDryRun bool
)

var costsCmd = &cobra.Command{
	Use:     "costs",
	GroupID: GroupDiag,
	Short:   "Show costs for running Claude sessions",
	Long: `Display costs for Claude Code sessions in Horde.

By default, shows live costs scraped from running tmux sessions.

Cost tracking uses ephemeral wisps for individual sessions that are
aggregated into daily "Cost Report" digest relics for audit purposes.

Examples:
  hd costs              # Live costs from running sessions
  hd costs --today      # Today's costs from wisps (not yet digested)
  hd costs --week       # This week's costs from digest relics + today's wisps
  hd costs --by-role    # Breakdown by role (raider, witness, etc.)
  hd costs --by-warband     # Breakdown by warband
  hd costs --json       # Output as JSON

Subcommands:
  hd costs record       # Record session cost as ephemeral wisp (Stop hook)
  hd costs digest       # Aggregate wisps into daily digest bead (Shaman scout)`,
	RunE: runCosts,
}

var costsRecordCmd = &cobra.Command{
	Use:   "record",
	Short: "Record session cost as an ephemeral wisp (called by Stop hook)",
	Long: `Record the final cost of a session as an ephemeral wisp.

This command is intended to be called from a Claude Code Stop hook.
It captures the final cost from the tmux session and creates an ephemeral
event that is NOT exported to JSONL (avoiding log-in-database pollution).

Session cost wisps are aggregated daily by 'hd costs digest' into a single
permanent "Cost Report YYYY-MM-DD" bead for audit purposes.

Examples:
  hd costs record --session gt-horde-toast
  hd costs record --session gt-horde-toast --work-item gt-abc123`,
	RunE: runCostsRecord,
}

var costsDigestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Aggregate session cost wisps into a daily digest bead",
	Long: `Aggregate ephemeral session cost wisps into a permanent daily digest.

This command is intended to be run by Shaman scout (daily) or manually.
It queries session.ended wisps for a target date, creates a single aggregate
"Cost Report YYYY-MM-DD" bead, then deletes the source wisps.

The resulting digest bead is permanent (exported to JSONL, synced via git)
and provides an audit trail without log-in-database pollution.

Examples:
  hd costs digest --yesterday   # Digest yesterday's costs (default for scout)
  hd costs digest --date 2026-01-07  # Digest a specific date
  hd costs digest --yesterday --dry-run  # Preview without changes`,
	RunE: runCostsDigest,
}

var costsMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate legacy session.ended relics to the new wisp architecture",
	Long: `Migrate legacy session.ended event relics to the new cost tracking system.

This command handles the transition from the old architecture (where each
session.ended event was a permanent bead) to the new wisp-based system.

The migration:
1. Finds all open session.ended event relics (should be none if auto-close worked)
2. Closes them with reason "migrated to wisp architecture"

Legacy relics remain in the database for historical queries but won't interfere
with the new wisp-based cost tracking.

Examples:
  hd costs migrate            # Migrate legacy relics
  hd costs migrate --dry-run  # Preview what would be migrated`,
	RunE: runCostsMigrate,
}

func init() {
	rootCmd.AddCommand(costsCmd)
	costsCmd.Flags().BoolVar(&costsJSON, "json", false, "Output as JSON")
	costsCmd.Flags().BoolVar(&costsToday, "today", false, "Show today's total from session events")
	costsCmd.Flags().BoolVar(&costsWeek, "week", false, "Show this week's total from session events")
	costsCmd.Flags().BoolVar(&costsByRole, "by-role", false, "Show breakdown by role")
	costsCmd.Flags().BoolVar(&costsByRig, "by-warband", false, "Show breakdown by warband")
	costsCmd.Flags().BoolVarP(&costsVerbose, "verbose", "v", false, "Show debug output for failures")

	// Add record subcommand
	costsCmd.AddCommand(costsRecordCmd)
	costsRecordCmd.Flags().StringVar(&recordSession, "session", "", "Tmux session name to record")
	costsRecordCmd.Flags().StringVar(&recordWorkItem, "work-item", "", "Work item ID (bead) for attribution")

	// Add digest subcommand
	costsCmd.AddCommand(costsDigestCmd)
	costsDigestCmd.Flags().BoolVar(&digestYesterday, "yesterday", false, "Digest yesterday's costs (default for scout)")
	costsDigestCmd.Flags().StringVar(&digestDate, "date", "", "Digest a specific date (YYYY-MM-DD)")
	costsDigestCmd.Flags().BoolVar(&digestDryRun, "dry-run", false, "Preview what would be done without making changes")

	// Add migrate subcommand
	costsCmd.AddCommand(costsMigrateCmd)
	costsMigrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Preview what would be migrated without making changes")
}

// SessionCost represents cost info for a single session.
type SessionCost struct {
	Session string  `json:"session"`
	Role    string  `json:"role"`
	Warband     string  `json:"warband,omitempty"`
	Worker  string  `json:"worker,omitempty"`
	Cost    float64 `json:"cost_usd"`
	Running bool    `json:"running"`
}

// CostEntry is a ledger entry for historical cost tracking.
type CostEntry struct {
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Warband       string    `json:"warband,omitempty"`
	Worker    string    `json:"worker,omitempty"`
	CostUSD   float64   `json:"cost_usd"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	WorkItem  string    `json:"work_item,omitempty"`
}

// CostsOutput is the JSON output structure.
type CostsOutput struct {
	Sessions []SessionCost      `json:"sessions,omitempty"`
	Total    float64            `json:"total_usd"`
	ByRole   map[string]float64 `json:"by_role,omitempty"`
	ByRig    map[string]float64 `json:"by_rig,omitempty"`
	Period   string             `json:"period,omitempty"`
}

// costRegex matches cost patterns like "$1.23" or "$12.34"
var costRegex = regexp.MustCompile(`\$(\d+\.\d{2})`)

func runCosts(cmd *cobra.Command, args []string) error {
	// If querying ledger, use ledger functions
	if costsToday || costsWeek || costsByRole || costsByRig {
		return runCostsFromLedger()
	}

	// Default: show live costs from running sessions
	return runLiveCosts()
}

func runLiveCosts() error {
	t := tmux.NewTmux()

	// Get all tmux sessions
	sessions, err := t.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	var costs []SessionCost
	var total float64

	for _, session := range sessions {
		// Only process Horde sessions (start with "hd-")
		if !strings.HasPrefix(session, constants.SessionPrefix) {
			continue
		}

		// Parse session name to get role/warband/worker
		role, warband, worker := parseSessionName(session)

		// Capture pane content
		content, err := t.CapturePaneAll(session)
		if err != nil {
			continue // Skip sessions we can't capture
		}

		// Extract cost from content
		cost := extractCost(content)

		// Check if an agent appears to be running
		running := t.IsAgentRunning(session)

		costs = append(costs, SessionCost{
			Session: session,
			Role:    role,
			Warband:     warband,
			Worker:  worker,
			Cost:    cost,
			Running: running,
		})
		total += cost
	}

	// Sort by session name
	sort.Slice(costs, func(i, j int) bool {
		return costs[i].Session < costs[j].Session
	})

	if costsJSON {
		return outputCostsJSON(CostsOutput{
			Sessions: costs,
			Total:    total,
		})
	}

	return outputCostsHuman(costs, total)
}

func runCostsFromLedger() error {
	now := time.Now()
	var entries []CostEntry
	var err error

	if costsToday {
		// For today: query ephemeral wisps (not yet digested)
		// This gives real-time view of today's costs
		entries, err = querySessionCostWisps(now)
		if err != nil {
			return fmt.Errorf("querying session cost wisps: %w", err)
		}
	} else if costsWeek {
		// For week: query digest relics (costs.digest events)
		// These are the aggregated daily reports
		entries, err = queryDigestRelics(7)
		if err != nil {
			return fmt.Errorf("querying digest relics: %w", err)
		}

		// Also include today's wisps (not yet digested)
		todayWisps, _ := querySessionCostWisps(now)
		entries = append(entries, todayWisps...)
	} else {
		// No time filter: query both digests and legacy session.ended events
		// (for backwards compatibility during migration)
		entries = querySessionEvents()
	}

	if len(entries) == 0 {
		fmt.Println(style.Dim.Render("No cost data found. Costs are recorded when sessions end."))
		return nil
	}

	// Calculate totals
	var total float64
	byRole := make(map[string]float64)
	byRig := make(map[string]float64)

	for _, entry := range entries {
		total += entry.CostUSD
		byRole[entry.Role] += entry.CostUSD
		if entry.Warband != "" {
			byRig[entry.Warband] += entry.CostUSD
		}
	}

	// Build output
	output := CostsOutput{
		Total: total,
	}

	if costsByRole {
		output.ByRole = byRole
	}
	if costsByRig {
		output.ByRig = byRig
	}

	// Set period label
	if costsToday {
		output.Period = "today"
	} else if costsWeek {
		output.Period = "this week"
	}

	if costsJSON {
		return outputCostsJSON(output)
	}

	return outputLedgerHuman(output, entries)
}

// SessionEvent represents a session.ended event from relics.
type SessionEvent struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	EventKind string    `json:"event_kind"`
	Actor     string    `json:"actor"`
	Target    string    `json:"target"`
	Payload   string    `json:"payload"`
}

// SessionPayload represents the JSON payload of a session event.
type SessionPayload struct {
	CostUSD   float64 `json:"cost_usd"`
	SessionID string  `json:"session_id"`
	Role      string  `json:"role"`
	Warband       string  `json:"warband"`
	Worker    string  `json:"worker"`
	EndedAt   string  `json:"ended_at"`
}

// EventListItem represents an event from rl list (minimal fields).
type EventListItem struct {
	ID string `json:"id"`
}

// querySessionEvents queries relics for session.ended events and converts them to CostEntry.
// It queries both encampment-level relics and all warband-level relics to find all session events.
// Errors from individual locations are logged (if verbose) but don't fail the query.
func querySessionEvents() []CostEntry {
	// Discover encampment root for cwd-based rl discovery
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		// Not in a Horde workspace - return empty list
		return nil
	}

	// Collect all relics locations to query
	relicsLocations := []string{townRoot}

	// Load warbands to find all warband relics locations
	rigsConfigPath := filepath.Join(townRoot, constants.DirWarchief, constants.FileRigsJSON)
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err == nil && rigsConfig != nil {
		for rigName := range rigsConfig.Warbands {
			rigPath := filepath.Join(townRoot, rigName)
			// Verify warband has a relics database
			rigRelicsPath := filepath.Join(rigPath, constants.DirRelics)
			if _, statErr := os.Stat(rigRelicsPath); statErr == nil {
				relicsLocations = append(relicsLocations, rigPath)
			}
		}
	}

	// Query each relics location and merge results
	var allEntries []CostEntry
	seenIDs := make(map[string]bool)

	for _, location := range relicsLocations {
		entries, err := querySessionEventsFromLocation(location)
		if err != nil {
			// Log but continue with other locations
			if costsVerbose {
				fmt.Fprintf(os.Stderr, "[costs] query from %s failed: %v\n", location, err)
			}
			continue
		}

		// Deduplicate by event ID (use SessionID as key)
		for _, entry := range entries {
			key := entry.SessionID + entry.EndedAt.String()
			if !seenIDs[key] {
				seenIDs[key] = true
				allEntries = append(allEntries, entry)
			}
		}
	}

	return allEntries
}

// querySessionEventsFromLocation queries a single relics location for session.ended events.
func querySessionEventsFromLocation(location string) ([]CostEntry, error) {
	// Step 1: Get list of event IDs
	listArgs := []string{
		"list",
		"--type=event",
		"--all",
		"--limit=0",
		"--json",
	}

	listCmd := exec.Command("rl", listArgs...)
	listCmd.Dir = location
	listOutput, err := listCmd.Output()
	if err != nil {
		// If rl fails (e.g., no relics database), return empty list
		return nil, nil
	}

	var listItems []EventListItem
	if err := json.Unmarshal(listOutput, &listItems); err != nil {
		return nil, fmt.Errorf("parsing event list: %w", err)
	}

	if len(listItems) == 0 {
		return nil, nil
	}

	// Step 2: Get full details for all events using rl show
	// (bd list doesn't include event_kind, actor, payload)
	showArgs := []string{"show", "--json"}
	for _, item := range listItems {
		showArgs = append(showArgs, item.ID)
	}

	showCmd := exec.Command("rl", showArgs...)
	showCmd.Dir = location
	showOutput, err := showCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("showing events: %w", err)
	}

	var events []SessionEvent
	if err := json.Unmarshal(showOutput, &events); err != nil {
		return nil, fmt.Errorf("parsing event details: %w", err)
	}

	var entries []CostEntry
	for _, event := range events {
		// Filter for session.ended events only
		if event.EventKind != "session.ended" {
			continue
		}

		// Parse payload
		var payload SessionPayload
		if event.Payload != "" {
			if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
				continue // Skip malformed payloads
			}
		}

		// Parse ended_at from payload, fall back to created_at
		endedAt := event.CreatedAt
		if payload.EndedAt != "" {
			if parsed, err := time.Parse(time.RFC3339, payload.EndedAt); err == nil {
				endedAt = parsed
			}
		}

		entries = append(entries, CostEntry{
			SessionID: payload.SessionID,
			Role:      payload.Role,
			Warband:       payload.Warband,
			Worker:    payload.Worker,
			CostUSD:   payload.CostUSD,
			EndedAt:   endedAt,
			WorkItem:  event.Target,
		})
	}

	return entries, nil
}

// queryDigestRelics queries costs.digest events from the past N days and extracts session entries.
func queryDigestRelics(days int) ([]CostEntry, error) {
	// Get list of event IDs
	listArgs := []string{
		"list",
		"--type=event",
		"--all",
		"--limit=0",
		"--json",
	}

	listCmd := exec.Command("rl", listArgs...)
	listOutput, err := listCmd.Output()
	if err != nil {
		return nil, nil
	}

	var listItems []EventListItem
	if err := json.Unmarshal(listOutput, &listItems); err != nil {
		return nil, fmt.Errorf("parsing event list: %w", err)
	}

	if len(listItems) == 0 {
		return nil, nil
	}

	// Get full details for all events
	showArgs := []string{"show", "--json"}
	for _, item := range listItems {
		showArgs = append(showArgs, item.ID)
	}

	showCmd := exec.Command("rl", showArgs...)
	showOutput, err := showCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("showing events: %w", err)
	}

	var events []SessionEvent
	if err := json.Unmarshal(showOutput, &events); err != nil {
		return nil, fmt.Errorf("parsing event details: %w", err)
	}

	// Calculate date range
	now := time.Now()
	cutoff := now.AddDate(0, 0, -days)

	var entries []CostEntry
	for _, event := range events {
		// Filter for costs.digest events only
		if event.EventKind != "costs.digest" {
			continue
		}

		// Parse the digest payload
		var digest CostDigest
		if event.Payload != "" {
			if err := json.Unmarshal([]byte(event.Payload), &digest); err != nil {
				continue
			}
		}

		// Check date is within range
		digestDate, err := time.Parse("2006-01-02", digest.Date)
		if err != nil {
			continue
		}
		if digestDate.Before(cutoff) {
			continue
		}

		// Extract individual session entries from the digest
		entries = append(entries, digest.Sessions...)
	}

	return entries, nil
}

// parseSessionName extracts role, warband, and worker from a session name.
// Session names follow the pattern: gt-<warband>-<worker> or gt-<global-agent>
// Examples:
//   - gt-warchief -> role=warchief, warband="", worker="warchief"
//   - gt-shaman -> role=shaman, warband="", worker="shaman"
//   - gt-horde-toast -> role=raider, warband=horde, worker=toast
//   - gt-horde-witness -> role=witness, warband=horde, worker=""
//   - gt-horde-forge -> role=forge, warband=horde, worker=""
//   - gt-horde-clan-joe -> role=clan, warband=horde, worker=joe
func parseSessionName(session string) (role, warband, worker string) {
	// Remove gt- prefix
	name := strings.TrimPrefix(session, constants.SessionPrefix)

	// Check for global agents
	switch name {
	case "warchief":
		return constants.RoleWarchief, "", "warchief"
	case "shaman":
		return constants.RoleShaman, "", "shaman"
	}

	// Parse warband-based session: warband-worker or warband-clan-name
	parts := strings.SplitN(name, "-", 3)
	if len(parts) < 2 {
		return "unknown", "", name
	}

	warband = parts[0]
	worker = parts[1]

	// Check for clan pattern: warband-clan-name
	if worker == "clan" && len(parts) >= 3 {
		return constants.RoleCrew, warband, parts[2]
	}

	// Check for special workers
	switch worker {
	case "witness":
		return constants.RoleWitness, warband, ""
	case "forge":
		return constants.RoleForge, warband, ""
	}

	// Default to raider
	return constants.RoleRaider, warband, worker
}

// extractCost finds the most recent cost value in pane content.
// Claude Code displays cost in the format "$X.XX" in the status area.
func extractCost(content string) float64 {
	matches := costRegex.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return 0.0
	}

	// Get the last (most recent) match
	lastMatch := matches[len(matches)-1]
	if len(lastMatch) < 2 {
		return 0.0
	}

	var cost float64
	_, _ = fmt.Sscanf(lastMatch[1], "%f", &cost)
	return cost
}

func outputCostsJSON(output CostsOutput) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func outputCostsHuman(costs []SessionCost, total float64) error {
	if len(costs) == 0 {
		fmt.Println(style.Dim.Render("No Horde sessions found"))
		return nil
	}

	fmt.Printf("\n%s Live Session Costs\n\n", style.Bold.Render("💰"))

	// Print table header
	fmt.Printf("%-25s %-10s %-15s %10s %8s\n",
		"Session", "Role", "Warband/Worker", "Cost", "Status")
	fmt.Println(strings.Repeat("─", 75))

	// Print each session
	for _, c := range costs {
		statusIcon := style.Success.Render("●")
		if !c.Running {
			statusIcon = style.Dim.Render("○")
		}

		rigWorker := c.Warband
		if c.Worker != "" && c.Worker != c.Warband {
			if rigWorker != "" {
				rigWorker += "/" + c.Worker
			} else {
				rigWorker = c.Worker
			}
		}

		fmt.Printf("%-25s %-10s %-15s %10s %8s\n",
			c.Session,
			c.Role,
			rigWorker,
			fmt.Sprintf("$%.2f", c.Cost),
			statusIcon)
	}

	// Print total
	fmt.Println(strings.Repeat("─", 75))
	fmt.Printf("%s %s\n", style.Bold.Render("Total:"), fmt.Sprintf("$%.2f", total))

	return nil
}

func outputLedgerHuman(output CostsOutput, entries []CostEntry) error {
	periodStr := ""
	if output.Period != "" {
		periodStr = fmt.Sprintf(" (%s)", output.Period)
	}

	fmt.Printf("\n%s Cost Summary%s\n\n", style.Bold.Render("📊"), periodStr)

	// Total
	fmt.Printf("%s $%.2f\n", style.Bold.Render("Total:"), output.Total)

	// By role breakdown
	if output.ByRole != nil && len(output.ByRole) > 0 {
		fmt.Printf("\n%s\n", style.Bold.Render("By Role:"))
		for role, cost := range output.ByRole {
			icon := constants.RoleEmoji(role)
			fmt.Printf("  %s %-12s $%.2f\n", icon, role, cost)
		}
	}

	// By warband breakdown
	if output.ByRig != nil && len(output.ByRig) > 0 {
		fmt.Printf("\n%s\n", style.Bold.Render("By Warband:"))
		for warband, cost := range output.ByRig {
			fmt.Printf("  %-15s $%.2f\n", warband, cost)
		}
	}

	// Session count
	fmt.Printf("\n%s %d sessions\n", style.Dim.Render("Entries:"), len(entries))

	return nil
}

// runCostsRecord captures the final cost from a session and records it as a bead event.
// This is called by the Claude Code Stop hook.
func runCostsRecord(cmd *cobra.Command, args []string) error {
	// Get session from flag or try to detect from environment
	session := recordSession
	if session == "" {
		session = os.Getenv("GT_SESSION")
	}
	if session == "" {
		// Derive session name from GT_* environment variables
		session = deriveSessionName()
	}
	if session == "" {
		// Try to detect current tmux session (works when running inside tmux)
		session = detectCurrentTmuxSession()
	}
	if session == "" {
		return fmt.Errorf("--session flag required (or set GT_SESSION env var, or GT_RIG/GT_ROLE)")
	}

	t := tmux.NewTmux()

	// Capture pane content
	content, err := t.CapturePaneAll(session)
	if err != nil {
		// Session may already be gone - that's OK, we'll record with zero cost
		content = ""
	}

	// Extract cost
	cost := extractCost(content)

	// Parse session name
	role, warband, worker := parseSessionName(session)

	// Build agent path for actor field
	agentPath := buildAgentPath(role, warband, worker)

	// Build event title
	title := fmt.Sprintf("Session ended: %s", session)
	if recordWorkItem != "" {
		title = fmt.Sprintf("Session: %s completed %s", session, recordWorkItem)
	}

	// Build payload JSON
	payload := map[string]interface{}{
		"cost_usd":   cost,
		"session_id": session,
		"role":       role,
		"ended_at":   time.Now().Format(time.RFC3339),
	}
	if warband != "" {
		payload["warband"] = warband
	}
	if worker != "" {
		payload["worker"] = worker
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	// Build rl create command for ephemeral wisp
	// Using --ephemeral creates a wisp that:
	// - Is stored locally only (not exported to JSONL)
	// - Won't pollute git history with O(sessions/day) events
	// - Will be aggregated into daily digests by 'hd costs digest'
	bdArgs := []string{
		"create",
		"--ephemeral",
		"--type=event",
		"--title=" + title,
		"--event-category=session.ended",
		"--event-actor=" + agentPath,
		"--event-payload=" + string(payloadJSON),
		"--silent",
	}

	// Add work item as event target if specified
	if recordWorkItem != "" {
		bdArgs = append(bdArgs, "--event-target="+recordWorkItem)
	}

	// NOTE: We intentionally don't use --warband flag here because it causes
	// event fields (event_kind, actor, payload) to not be stored properly.
	// The rl command will auto-detect the correct warband from cwd.

	// Execute rl create
	bdCmd := exec.Command("rl", bdArgs...)
	output, err := bdCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating session cost wisp: %w\nOutput: %s", err, string(output))
	}

	wispID := strings.TrimSpace(string(output))

	// Auto-close session cost wisps immediately after creation.
	// These are informational records that don't need to stay open.
	// The wisp data is preserved and queryable until digested.
	closeCmd := exec.Command("rl", "close", wispID, "--reason=auto-closed session cost wisp")
	if closeErr := closeCmd.Run(); closeErr != nil {
		// Non-fatal: wisp was created, just couldn't auto-close
		fmt.Fprintf(os.Stderr, "warning: could not auto-close session cost wisp %s: %v\n", wispID, closeErr)
	}

	// Output confirmation (silent if cost is zero and no work item)
	if cost > 0 || recordWorkItem != "" {
		fmt.Printf("%s Recorded $%.2f for %s (wisp: %s)", style.Success.Render("✓"), cost, session, wispID)
		if recordWorkItem != "" {
			fmt.Printf(" (work: %s)", recordWorkItem)
		}
		fmt.Println()
	}

	return nil
}

// deriveSessionName derives the tmux session name from GT_* environment variables.
// Session naming patterns:
//   - Raiders: gt-{warband}-{raider} (e.g., gt-horde-toast)
//   - Clan: gt-{warband}-clan-{clan} (e.g., gt-horde-clan-max)
//   - Witness/Forge: gt-{warband}-{role} (e.g., gt-horde-witness)
//   - Warchief/Shaman: gt-{encampment}-{role} (e.g., gt-ai-warchief)
func deriveSessionName() string {
	role := os.Getenv("GT_ROLE")
	warband := os.Getenv("GT_RIG")
	raider := os.Getenv("GT_RAIDER")
	clan := os.Getenv("GT_CREW")
	encampment := os.Getenv("GT_TOWN")

	// Raider: gt-{warband}-{raider}
	if raider != "" && warband != "" {
		return fmt.Sprintf("gt-%s-%s", warband, raider)
	}

	// Clan: gt-{warband}-clan-{clan}
	if clan != "" && warband != "" {
		return fmt.Sprintf("gt-%s-clan-%s", warband, clan)
	}

	// Encampment-level roles (warchief, shaman): gt-{encampment}-{role} or gt-{role}
	if role == "warchief" || role == "shaman" {
		if encampment != "" {
			return fmt.Sprintf("gt-%s-%s", encampment, role)
		}
		// No encampment set - use simple gt-{role} pattern
		return fmt.Sprintf("gt-%s", role)
	}

	// Warband-based roles (witness, forge): gt-{warband}-{role}
	if role != "" && warband != "" {
		return fmt.Sprintf("gt-%s-%s", warband, role)
	}

	return ""
}

// detectCurrentTmuxSession returns the current tmux session name if running inside tmux.
// Uses `tmux display-message -p '#S'` which prints the session name.
// Note: We don't check TMUX env var because it may not be inherited when Claude Code
// runs bash commands, even though we are inside a tmux session.
func detectCurrentTmuxSession() string {
	cmd := exec.Command("tmux", "display-message", "-p", "#S")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	session := strings.TrimSpace(string(output))
	// Only return if it looks like a Horde session
	// Accept both gt- (warband sessions) and hq- (encampment-level sessions like hq-warchief)
	if strings.HasPrefix(session, constants.SessionPrefix) || strings.HasPrefix(session, constants.HQSessionPrefix) {
		return session
	}
	return ""
}

// buildAgentPath builds the agent path from role, warband, and worker.
// Examples: "warchief", "horde/witness", "horde/raiders/toast"
func buildAgentPath(role, warband, worker string) string {
	switch role {
	case constants.RoleWarchief, constants.RoleShaman:
		return role
	case constants.RoleWitness, constants.RoleForge:
		if warband != "" {
			return warband + "/" + role
		}
		return role
	case constants.RoleRaider:
		if warband != "" && worker != "" {
			return warband + "/raiders/" + worker
		}
		if warband != "" {
			return warband + "/raider"
		}
		return "raider/" + worker
	case constants.RoleCrew:
		if warband != "" && worker != "" {
			return warband + "/clan/" + worker
		}
		if warband != "" {
			return warband + "/clan"
		}
		return "clan/" + worker
	default:
		if warband != "" && worker != "" {
			return warband + "/" + worker
		}
		if warband != "" {
			return warband
		}
		return worker
	}
}

// CostDigest represents the aggregated daily cost report.
type CostDigest struct {
	Date         string             `json:"date"`
	TotalUSD     float64            `json:"total_usd"`
	SessionCount int                `json:"session_count"`
	Sessions     []CostEntry        `json:"sessions"`
	ByRole       map[string]float64 `json:"by_role"`
	ByRig        map[string]float64 `json:"by_rig,omitempty"`
}

// WispListOutput represents the JSON output from rl mol wisp list.
type WispListOutput struct {
	Wisps []WispItem `json:"wisps"`
	Count int        `json:"count"`
}

// WispItem represents a single wisp from rl mol wisp list.
type WispItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// runCostsDigest aggregates session cost wisps into a daily digest bead.
func runCostsDigest(cmd *cobra.Command, args []string) error {
	// Determine target date
	var targetDate time.Time

	if digestDate != "" {
		parsed, err := time.Parse("2006-01-02", digestDate)
		if err != nil {
			return fmt.Errorf("invalid date format (use YYYY-MM-DD): %w", err)
		}
		targetDate = parsed
	} else if digestYesterday {
		targetDate = time.Now().AddDate(0, 0, -1)
	} else {
		return fmt.Errorf("specify --yesterday or --date YYYY-MM-DD")
	}

	dateStr := targetDate.Format("2006-01-02")

	// Query ephemeral session.ended wisps for target date
	wisps, err := querySessionCostWisps(targetDate)
	if err != nil {
		return fmt.Errorf("querying session cost wisps: %w", err)
	}

	if len(wisps) == 0 {
		fmt.Printf("%s No session cost wisps found for %s\n", style.Dim.Render("○"), dateStr)
		return nil
	}

	// Build digest
	digest := CostDigest{
		Date:     dateStr,
		Sessions: wisps,
		ByRole:   make(map[string]float64),
		ByRig:    make(map[string]float64),
	}

	for _, w := range wisps {
		digest.TotalUSD += w.CostUSD
		digest.SessionCount++
		digest.ByRole[w.Role] += w.CostUSD
		if w.Warband != "" {
			digest.ByRig[w.Warband] += w.CostUSD
		}
	}

	if digestDryRun {
		fmt.Printf("%s [DRY RUN] Would create Cost Report %s:\n", style.Bold.Render("📊"), dateStr)
		fmt.Printf("  Total: $%.2f\n", digest.TotalUSD)
		fmt.Printf("  Sessions: %d\n", digest.SessionCount)
		fmt.Printf("  By Role:\n")
		for role, cost := range digest.ByRole {
			fmt.Printf("    %s: $%.2f\n", role, cost)
		}
		if len(digest.ByRig) > 0 {
			fmt.Printf("  By Warband:\n")
			for warband, cost := range digest.ByRig {
				fmt.Printf("    %s: $%.2f\n", warband, cost)
			}
		}
		return nil
	}

	// Create permanent digest bead
	digestID, err := createCostDigestBead(digest)
	if err != nil {
		return fmt.Errorf("creating digest bead: %w", err)
	}

	// Delete source wisps (they're ephemeral, use rl mol burn)
	deletedCount, deleteErr := deleteSessionCostWisps(targetDate)
	if deleteErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to delete some source wisps: %v\n", deleteErr)
	}

	fmt.Printf("%s Created Cost Report %s (bead: %s)\n", style.Success.Render("✓"), dateStr, digestID)
	fmt.Printf("  Total: $%.2f from %d sessions\n", digest.TotalUSD, digest.SessionCount)
	if deletedCount > 0 {
		fmt.Printf("  Deleted %d source wisps\n", deletedCount)
	}

	return nil
}

// querySessionCostWisps queries ephemeral session.ended events for a target date.
func querySessionCostWisps(targetDate time.Time) ([]CostEntry, error) {
	// List all wisps including closed ones
	listCmd := exec.Command("rl", "mol", "wisp", "list", "--all", "--json")
	listOutput, err := listCmd.Output()
	if err != nil {
		// No wisps database or command failed
		if costsVerbose {
			fmt.Fprintf(os.Stderr, "[costs] wisp list failed: %v\n", err)
		}
		return nil, nil
	}

	var wispList WispListOutput
	if err := json.Unmarshal(listOutput, &wispList); err != nil {
		return nil, fmt.Errorf("parsing wisp list: %w", err)
	}

	if wispList.Count == 0 {
		return nil, nil
	}

	// Batch all wisp IDs into a single rl show call to avoid N+1 queries
	showArgs := []string{"show", "--json"}
	for _, wisp := range wispList.Wisps {
		showArgs = append(showArgs, wisp.ID)
	}

	showCmd := exec.Command("rl", showArgs...)
	showOutput, err := showCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("showing wisps: %w", err)
	}

	var events []SessionEvent
	if err := json.Unmarshal(showOutput, &events); err != nil {
		return nil, fmt.Errorf("parsing wisp details: %w", err)
	}

	var sessionCostWisps []CostEntry
	targetDay := targetDate.Format("2006-01-02")

	for _, event := range events {
		// Filter for session.ended events only
		if event.EventKind != "session.ended" {
			continue
		}

		// Parse payload
		var payload SessionPayload
		if event.Payload != "" {
			if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
				if costsVerbose {
					fmt.Fprintf(os.Stderr, "[costs] payload unmarshal failed for event %s: %v\n", event.ID, err)
				}
				continue
			}
		}

		// Parse ended_at and filter by target date
		endedAt := event.CreatedAt
		if payload.EndedAt != "" {
			if parsed, err := time.Parse(time.RFC3339, payload.EndedAt); err == nil {
				endedAt = parsed
			}
		}

		// Check if this event is from the target date
		if endedAt.Format("2006-01-02") != targetDay {
			continue
		}

		sessionCostWisps = append(sessionCostWisps, CostEntry{
			SessionID: payload.SessionID,
			Role:      payload.Role,
			Warband:       payload.Warband,
			Worker:    payload.Worker,
			CostUSD:   payload.CostUSD,
			EndedAt:   endedAt,
			WorkItem:  event.Target,
		})
	}

	return sessionCostWisps, nil
}

// createCostDigestBead creates a permanent bead for the daily cost digest.
func createCostDigestBead(digest CostDigest) (string, error) {
	// Build description with aggregate data
	var desc strings.Builder
	desc.WriteString(fmt.Sprintf("Daily cost aggregate for %s.\n\n", digest.Date))
	desc.WriteString(fmt.Sprintf("**Total:** $%.2f from %d sessions\n\n", digest.TotalUSD, digest.SessionCount))

	if len(digest.ByRole) > 0 {
		desc.WriteString("## By Role\n")
		roles := make([]string, 0, len(digest.ByRole))
		for role := range digest.ByRole {
			roles = append(roles, role)
		}
		sort.Strings(roles)
		for _, role := range roles {
			icon := constants.RoleEmoji(role)
			desc.WriteString(fmt.Sprintf("- %s %s: $%.2f\n", icon, role, digest.ByRole[role]))
		}
		desc.WriteString("\n")
	}

	if len(digest.ByRig) > 0 {
		desc.WriteString("## By Warband\n")
		warbands := make([]string, 0, len(digest.ByRig))
		for warband := range digest.ByRig {
			warbands = append(warbands, warband)
		}
		sort.Strings(warbands)
		for _, warband := range warbands {
			desc.WriteString(fmt.Sprintf("- %s: $%.2f\n", warband, digest.ByRig[warband]))
		}
		desc.WriteString("\n")
	}

	// Build payload JSON with full session details
	payloadJSON, err := json.Marshal(digest)
	if err != nil {
		return "", fmt.Errorf("marshaling digest payload: %w", err)
	}

	// Create the digest bead (NOT ephemeral - this is permanent)
	title := fmt.Sprintf("Cost Report %s", digest.Date)
	bdArgs := []string{
		"create",
		"--type=event",
		"--title=" + title,
		"--event-category=costs.digest",
		"--event-payload=" + string(payloadJSON),
		"--description=" + desc.String(),
		"--silent",
	}

	bdCmd := exec.Command("rl", bdArgs...)
	output, err := bdCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("creating digest bead: %w\nOutput: %s", err, string(output))
	}

	digestID := strings.TrimSpace(string(output))

	// Auto-close the digest (it's an audit record, not work)
	closeCmd := exec.Command("rl", "close", digestID, "--reason=daily cost digest")
	_ = closeCmd.Run() // Best effort

	return digestID, nil
}

// deleteSessionCostWisps deletes ephemeral session.ended wisps for a target date.
func deleteSessionCostWisps(targetDate time.Time) (int, error) {
	// List all wisps
	listCmd := exec.Command("rl", "mol", "wisp", "list", "--all", "--json")
	listOutput, err := listCmd.Output()
	if err != nil {
		if costsVerbose {
			fmt.Fprintf(os.Stderr, "[costs] wisp list failed in deletion: %v\n", err)
		}
		return 0, nil
	}

	var wispList WispListOutput
	if err := json.Unmarshal(listOutput, &wispList); err != nil {
		return 0, fmt.Errorf("parsing wisp list: %w", err)
	}

	targetDay := targetDate.Format("2006-01-02")

	// Collect all wisp IDs that match our criteria
	var wispIDsToDelete []string

	for _, wisp := range wispList.Wisps {
		// Get full wisp details to check if it's a session.ended event
		showCmd := exec.Command("rl", "show", wisp.ID, "--json")
		showOutput, err := showCmd.Output()
		if err != nil {
			if costsVerbose {
				fmt.Fprintf(os.Stderr, "[costs] rl show failed for wisp %s: %v\n", wisp.ID, err)
			}
			continue
		}

		var events []SessionEvent
		if err := json.Unmarshal(showOutput, &events); err != nil {
			if costsVerbose {
				fmt.Fprintf(os.Stderr, "[costs] JSON unmarshal failed for wisp %s: %v\n", wisp.ID, err)
			}
			continue
		}

		if len(events) == 0 {
			continue
		}

		event := events[0]

		// Only delete session.ended wisps
		if event.EventKind != "session.ended" {
			continue
		}

		// Parse payload to get ended_at for date filtering
		var payload SessionPayload
		if event.Payload != "" {
			if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
				if costsVerbose {
					fmt.Fprintf(os.Stderr, "[costs] payload unmarshal failed for wisp %s: %v\n", wisp.ID, err)
				}
				continue
			}
		}

		endedAt := event.CreatedAt
		if payload.EndedAt != "" {
			if parsed, err := time.Parse(time.RFC3339, payload.EndedAt); err == nil {
				endedAt = parsed
			}
		}

		// Only delete wisps from the target date
		if endedAt.Format("2006-01-02") != targetDay {
			continue
		}

		wispIDsToDelete = append(wispIDsToDelete, wisp.ID)
	}

	if len(wispIDsToDelete) == 0 {
		return 0, nil
	}

	// Batch delete all wisps in a single subprocess call
	burnArgs := append([]string{"mol", "burn", "--force"}, wispIDsToDelete...)
	burnCmd := exec.Command("rl", burnArgs...)
	if burnErr := burnCmd.Run(); burnErr != nil {
		return 0, fmt.Errorf("batch burn failed: %w", burnErr)
	}

	return len(wispIDsToDelete), nil
}

// runCostsMigrate migrates legacy session.ended relics to the new architecture.
func runCostsMigrate(cmd *cobra.Command, args []string) error {
	// Query all session.ended events (both open and closed)
	listArgs := []string{
		"list",
		"--type=event",
		"--all",
		"--limit=0",
		"--json",
	}

	listCmd := exec.Command("rl", listArgs...)
	listOutput, err := listCmd.Output()
	if err != nil {
		fmt.Println(style.Dim.Render("No events found or rl command failed"))
		return nil
	}

	var listItems []EventListItem
	if err := json.Unmarshal(listOutput, &listItems); err != nil {
		return fmt.Errorf("parsing event list: %w", err)
	}

	if len(listItems) == 0 {
		fmt.Println(style.Dim.Render("No events found"))
		return nil
	}

	// Get full details for all events
	showArgs := []string{"show", "--json"}
	for _, item := range listItems {
		showArgs = append(showArgs, item.ID)
	}

	showCmd := exec.Command("rl", showArgs...)
	showOutput, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("showing events: %w", err)
	}

	var events []SessionEvent
	if err := json.Unmarshal(showOutput, &events); err != nil {
		return fmt.Errorf("parsing event details: %w", err)
	}

	// Find open session.ended events
	var openEvents []SessionEvent
	var closedCount int
	for _, event := range events {
		if event.EventKind != "session.ended" {
			continue
		}
		if event.Status == "closed" {
			closedCount++
			continue
		}
		openEvents = append(openEvents, event)
	}

	fmt.Printf("%s Legacy session.ended relics:\n", style.Bold.Render("📊"))
	fmt.Printf("  Closed: %d (no action needed)\n", closedCount)
	fmt.Printf("  Open:   %d (will be closed)\n", len(openEvents))

	if len(openEvents) == 0 {
		fmt.Println(style.Success.Render("\n✓ No migration needed - all session.ended events are already closed"))
		return nil
	}

	if migrateDryRun {
		fmt.Printf("\n%s Would close %d open session.ended events\n", style.Bold.Render("[DRY RUN]"), len(openEvents))
		for _, event := range openEvents {
			fmt.Printf("  - %s: %s\n", event.ID, event.Title)
		}
		return nil
	}

	// Close all open session.ended events
	closedMigrated := 0
	for _, event := range openEvents {
		closeCmd := exec.Command("rl", "close", event.ID, "--reason=migrated to wisp architecture")
		if err := closeCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not close %s: %v\n", event.ID, err)
			continue
		}
		closedMigrated++
	}

	fmt.Printf("\n%s Migrated %d session.ended events (closed)\n", style.Success.Render("✓"), closedMigrated)
	fmt.Println(style.Dim.Render("Legacy relics preserved for historical queries."))
	fmt.Println(style.Dim.Render("New session costs will use ephemeral wisps + daily digests."))

	return nil
}
