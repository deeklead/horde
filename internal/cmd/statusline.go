package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/drums"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

var (
	statusLineSession string
)

var statusLineCmd = &cobra.Command{
	Use:    "status-line",
	Short:  "Output status line content for tmux (internal use)",
	Hidden: true, // Internal command called by tmux
	RunE:   runStatusLine,
}

func init() {
	rootCmd.AddCommand(statusLineCmd)
	statusLineCmd.Flags().StringVar(&statusLineSession, "session", "", "Tmux session name")
}

func runStatusLine(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Get session environment
	var rigName, raider, clan, issue, role string

	if statusLineSession != "" {
		// Non-fatal: missing env vars are handled gracefully below
		rigName, _ = t.GetEnvironment(statusLineSession, "HD_WARBAND")
		raider, _ = t.GetEnvironment(statusLineSession, "HD_RAIDER")
		clan, _ = t.GetEnvironment(statusLineSession, "HD_CLAN")
		issue, _ = t.GetEnvironment(statusLineSession, "HD_ISSUE")
		role, _ = t.GetEnvironment(statusLineSession, "HD_ROLE")
	} else {
		// Fallback to process environment
		rigName = os.Getenv("HD_WARBAND")
		raider = os.Getenv("HD_RAIDER")
		clan = os.Getenv("HD_CLAN")
		issue = os.Getenv("HD_ISSUE")
		role = os.Getenv("HD_ROLE")
	}

	// Get session names for comparison
	warchiefSession := getWarchiefSessionName()
	shamanSession := getShamanSessionName()

	// Determine identity and output based on role
	if role == "warchief" || statusLineSession == warchiefSession {
		return runWarchiefStatusLine(t)
	}

	// Shaman status line
	if role == "shaman" || statusLineSession == shamanSession {
		return runShamanStatusLine(t)
	}

	// Witness status line (session naming: gt-<warband>-witness)
	if role == "witness" || strings.HasSuffix(statusLineSession, "-witness") {
		return runWitnessStatusLine(t, rigName)
	}

	// Forge status line
	if role == "forge" || strings.HasSuffix(statusLineSession, "-forge") {
		return runForgeStatusLine(t, rigName)
	}

	// Clan/Raider status line
	return runWorkerStatusLine(t, statusLineSession, rigName, raider, clan, issue)
}

// runWorkerStatusLine outputs status for clan or raider sessions.
func runWorkerStatusLine(t *tmux.Tmux, session, rigName, raider, clan, issue string) error {
	// Determine agent type and identity
	var icon, identity string
	if raider != "" {
		icon = AgentTypeIcons[AgentRaider]
		identity = fmt.Sprintf("%s/%s", rigName, raider)
	} else if clan != "" {
		icon = AgentTypeIcons[AgentCrew]
		identity = fmt.Sprintf("%s/clan/%s", rigName, clan)
	}

	// Get pane's working directory to find workspace
	var townRoot string
	if session != "" {
		paneDir, err := t.GetPaneWorkDir(session)
		if err == nil && paneDir != "" {
			townRoot, _ = workspace.Find(paneDir)
		}
	}

	// Build status parts
	var parts []string

	// Priority 1: Check for bannered work (use warband relics)
	hookedWork := ""
	if identity != "" && rigName != "" && townRoot != "" {
		rigRelicsDir := filepath.Join(townRoot, rigName, "warchief", "warband")
		hookedWork = getHookedWork(identity, 40, rigRelicsDir)
	}

	// Priority 2: Fall back to HD_ISSUE env var or in_progress relics
	currentWork := issue
	if currentWork == "" && hookedWork == "" && session != "" {
		currentWork = getCurrentWork(t, session, 40)
	}

	// Show bannered work (takes precedence)
	if hookedWork != "" {
		if icon != "" {
			parts = append(parts, fmt.Sprintf("%s 🪝 %s", icon, hookedWork))
		} else {
			parts = append(parts, fmt.Sprintf("🪝 %s", hookedWork))
		}
	} else if currentWork != "" {
		// Fall back to current work (in_progress)
		if icon != "" {
			parts = append(parts, fmt.Sprintf("%s %s", icon, currentWork))
		} else {
			parts = append(parts, currentWork)
		}
	} else if icon != "" {
		parts = append(parts, icon)
	}

	// Drums preview - only show if hook is empty
	if hookedWork == "" && identity != "" && townRoot != "" {
		unread, subject := getMailPreviewWithRoot(identity, 45, townRoot)
		if unread > 0 {
			if subject != "" {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %s", subject))
			} else {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %d", unread))
			}
		}
	}

	// Output
	if len(parts) > 0 {
		fmt.Print(strings.Join(parts, " | ") + " |")
	}

	return nil
}

func runWarchiefStatusLine(t *tmux.Tmux) error {
	// Count active sessions by listing tmux sessions
	sessions, err := t.ListSessions()
	if err != nil {
		return nil // Silent fail
	}

	// Get encampment root from warchief pane's working directory
	var townRoot string
	warchiefSession := getWarchiefSessionName()
	paneDir, err := t.GetPaneWorkDir(warchiefSession)
	if err == nil && paneDir != "" {
		townRoot, _ = workspace.Find(paneDir)
	}

	// Load registered warbands to validate against
	registeredRigs := make(map[string]bool)
	if townRoot != "" {
		rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
		if rigsConfig, err := config.LoadRigsConfig(rigsConfigPath); err == nil {
			for rigName := range rigsConfig.Warbands {
				registeredRigs[rigName] = true
			}
		}
	}

	// Track per-warband status for LED indicators and sorting
	type rigStatus struct {
		hasWitness   bool
		hasForge  bool
		raiderCount int
		opState      string // "OPERATIONAL", "PARKED", or "DOCKED"
	}
	rigStatuses := make(map[string]*rigStatus)

	// Initialize for all registered warbands
	for rigName := range registeredRigs {
		rigStatuses[rigName] = &rigStatus{}
	}

	// Track per-agent-type health (working/zombie counts)
	type agentHealth struct {
		total   int
		working int
	}
	healthByType := map[AgentType]*agentHealth{
		AgentRaider:  {},
		AgentWitness:  {},
		AgentForge: {},
		AgentShaman:   {},
	}

	// Single pass: track warband status AND agent health
	for _, s := range sessions {
		agent := categorizeSession(s)
		if agent == nil {
			continue
		}

		// Track warband-level status (witness/forge/raider presence)
		if agent.Warband != "" && registeredRigs[agent.Warband] {
			if rigStatuses[agent.Warband] == nil {
				rigStatuses[agent.Warband] = &rigStatus{}
			}
			switch agent.Type {
			case AgentWitness:
				rigStatuses[agent.Warband].hasWitness = true
			case AgentForge:
				rigStatuses[agent.Warband].hasForge = true
			case AgentRaider:
				rigStatuses[agent.Warband].raiderCount++
			}
		}

		// Track agent health (skip Warchief and Clan)
		if health := healthByType[agent.Type]; health != nil {
			health.total++
			// Detect working state via ✻ symbol
			if isSessionWorking(t, s) {
				health.working++
			}
		}
	}

	// Get operational state for each warband
	for rigName, status := range rigStatuses {
		opState, _ := getRigOperationalState(townRoot, rigName)
		if opState == "PARKED" || opState == "DOCKED" {
			status.opState = opState
		} else {
			status.opState = "OPERATIONAL"
		}
	}

	// Build status
	var parts []string

	// Add per-agent-type health in consistent order
	// Format: "1/10 😺" = 1 working out of 10 total
	// Only show agent types that have sessions
	agentOrder := []AgentType{AgentRaider, AgentWitness, AgentForge, AgentShaman}
	var agentParts []string
	for _, agentType := range agentOrder {
		health := healthByType[agentType]
		if health.total == 0 {
			continue
		}
		icon := AgentTypeIcons[agentType]
		agentParts = append(agentParts, fmt.Sprintf("%d/%d %s", health.working, health.total, icon))
	}
	if len(agentParts) > 0 {
		parts = append(parts, strings.Join(agentParts, " "))
	}

	// Build warband status display with LED indicators
	// 🟢 = both witness and forge running (fully active)
	// 🟡 = one of witness/forge running (partially active)
	// 🅿️ = parked (nothing running, intentionally paused)
	// 🛑 = docked (nothing running, global shutdown)
	// ⚫ = operational but nothing running (unexpected state)

	// Create sortable warband list
	type rigInfo struct {
		name   string
		status *rigStatus
	}
	var warbands []rigInfo
	for rigName, status := range rigStatuses {
		warbands = append(warbands, rigInfo{name: rigName, status: status})
	}

	// Sort by: 1) running state, 2) raider count (desc), 3) operational state, 4) alphabetical
	sort.Slice(warbands, func(i, j int) bool {
		isRunningI := warbands[i].status.hasWitness || warbands[i].status.hasForge
		isRunningJ := warbands[j].status.hasWitness || warbands[j].status.hasForge

		// Primary sort: running warbands before non-running warbands
		if isRunningI != isRunningJ {
			return isRunningI
		}

		// Secondary sort: raider count (descending)
		if warbands[i].status.raiderCount != warbands[j].status.raiderCount {
			return warbands[i].status.raiderCount > warbands[j].status.raiderCount
		}

		// Tertiary sort: operational state (for non-running warbands: OPERATIONAL < PARKED < DOCKED)
		stateOrder := map[string]int{"OPERATIONAL": 0, "PARKED": 1, "DOCKED": 2}
		stateI := stateOrder[warbands[i].status.opState]
		stateJ := stateOrder[warbands[j].status.opState]
		if stateI != stateJ {
			return stateI < stateJ
		}

		// Quaternary sort: alphabetical
		return warbands[i].name < warbands[j].name
	})

	// Build display with group separators
	var rigParts []string
	var lastGroup string
	for _, warband := range warbands {
		isRunning := warband.status.hasWitness || warband.status.hasForge
		var currentGroup string
		if isRunning {
			currentGroup = "running"
		} else {
			currentGroup = "idle-" + warband.status.opState
		}

		// Add separator when group changes (running -> non-running, or different opStates within non-running)
		if lastGroup != "" && lastGroup != currentGroup {
			rigParts = append(rigParts, "|")
		}
		lastGroup = currentGroup

		status := warband.status
		var led string

		// Check if processes are running first (regardless of operational state)
		if status.hasWitness && status.hasForge {
			led = "🟢" // Both running - fully active
		} else if status.hasWitness || status.hasForge {
			led = "🟡" // One running - partially active
		} else {
			// Nothing running - show operational state
			switch status.opState {
			case "PARKED":
				led = "🅿️" // Parked - intentionally paused
			case "DOCKED":
				led = "🛑" // Docked - global shutdown
			default:
				led = "⚫" // Operational but nothing running
			}
		}

		// Show raider count if > 0
		// All icons get 1 space, Park gets 2
		space := " "
		if led == "🅿️" {
			space = "  "
		}
		display := led + space + warband.name
		if status.raiderCount > 0 {
			display += fmt.Sprintf("(%d)", status.raiderCount)
		}
		rigParts = append(rigParts, display)
	}

	if len(rigParts) > 0 {
		parts = append(parts, strings.Join(rigParts, " "))
	}

	// Priority 1: Check for bannered work (encampment relics for warchief)
	hookedWork := ""
	if townRoot != "" {
		hookedWork = getHookedWork("warchief", 40, townRoot)
	}
	if hookedWork != "" {
		parts = append(parts, fmt.Sprintf("🪝 %s", hookedWork))
	} else if townRoot != "" {
		// Priority 2: Fall back to drums preview
		unread, subject := getMailPreviewWithRoot("warchief/", 45, townRoot)
		if unread > 0 {
			if subject != "" {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %s", subject))
			} else {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %d", unread))
			}
		}
	}

	fmt.Print(strings.Join(parts, " | ") + " |")
	return nil
}

// runShamanStatusLine outputs status for the shaman session.
// Shows: active warbands, raider count, hook or drums preview
func runShamanStatusLine(t *tmux.Tmux) error {
	// Count active warbands and raiders
	sessions, err := t.ListSessions()
	if err != nil {
		return nil // Silent fail
	}

	// Get encampment root from shaman pane's working directory
	var townRoot string
	shamanSession := getShamanSessionName()
	paneDir, err := t.GetPaneWorkDir(shamanSession)
	if err == nil && paneDir != "" {
		townRoot, _ = workspace.Find(paneDir)
	}

	// Load registered warbands to validate against
	registeredRigs := make(map[string]bool)
	if townRoot != "" {
		rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
		if rigsConfig, err := config.LoadRigsConfig(rigsConfigPath); err == nil {
			for rigName := range rigsConfig.Warbands {
				registeredRigs[rigName] = true
			}
		}
	}

	warbands := make(map[string]bool)
	raiderCount := 0
	for _, s := range sessions {
		agent := categorizeSession(s)
		if agent == nil {
			continue
		}
		// Only count registered warbands
		if agent.Warband != "" && registeredRigs[agent.Warband] {
			warbands[agent.Warband] = true
		}
		if agent.Type == AgentRaider && registeredRigs[agent.Warband] {
			raiderCount++
		}
	}
	rigCount := len(warbands)

	// Build status
	var parts []string
	parts = append(parts, fmt.Sprintf("%d warbands", rigCount))
	parts = append(parts, fmt.Sprintf("%d 😺", raiderCount))

	// Priority 1: Check for bannered work (encampment relics for shaman)
	hookedWork := ""
	if townRoot != "" {
		hookedWork = getHookedWork("shaman", 35, townRoot)
	}
	if hookedWork != "" {
		parts = append(parts, fmt.Sprintf("🪝 %s", hookedWork))
	} else if townRoot != "" {
		// Priority 2: Fall back to drums preview
		unread, subject := getMailPreviewWithRoot("shaman/", 40, townRoot)
		if unread > 0 {
			if subject != "" {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %s", subject))
			} else {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %d", unread))
			}
		}
	}

	fmt.Print(strings.Join(parts, " | ") + " |")
	return nil
}

// runWitnessStatusLine outputs status for a witness session.
// Shows: raider count, clan count, hook or drums preview
func runWitnessStatusLine(t *tmux.Tmux, rigName string) error {
	if rigName == "" {
		// Try to extract from session name: gt-<warband>-witness
		if strings.HasSuffix(statusLineSession, "-witness") && strings.HasPrefix(statusLineSession, "hd-") {
			rigName = strings.TrimPrefix(strings.TrimSuffix(statusLineSession, "-witness"), "hd-")
		}
	}

	// Get encampment root from witness pane's working directory
	var townRoot string
	sessionName := fmt.Sprintf("hd-%s-witness", rigName)
	paneDir, err := t.GetPaneWorkDir(sessionName)
	if err == nil && paneDir != "" {
		townRoot, _ = workspace.Find(paneDir)
	}

	// Count raiders and clan in this warband
	sessions, err := t.ListSessions()
	if err != nil {
		return nil // Silent fail
	}

	raiderCount := 0
	crewCount := 0
	for _, s := range sessions {
		agent := categorizeSession(s)
		if agent == nil {
			continue
		}
		if agent.Warband == rigName {
			if agent.Type == AgentRaider {
				raiderCount++
			} else if agent.Type == AgentCrew {
				crewCount++
			}
		}
	}

	identity := fmt.Sprintf("%s/witness", rigName)

	// Build status
	var parts []string
	parts = append(parts, fmt.Sprintf("%d 😺", raiderCount))
	if crewCount > 0 {
		parts = append(parts, fmt.Sprintf("%d clan", crewCount))
	}

	// Priority 1: Check for bannered work (warband relics for witness)
	hookedWork := ""
	if townRoot != "" && rigName != "" {
		rigRelicsDir := filepath.Join(townRoot, rigName, "warchief", "warband")
		hookedWork = getHookedWork(identity, 30, rigRelicsDir)
	}
	if hookedWork != "" {
		parts = append(parts, fmt.Sprintf("🪝 %s", hookedWork))
	} else if townRoot != "" {
		// Priority 2: Fall back to drums preview
		unread, subject := getMailPreviewWithRoot(identity, 35, townRoot)
		if unread > 0 {
			if subject != "" {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %s", subject))
			} else {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %d", unread))
			}
		}
	}

	fmt.Print(strings.Join(parts, " | ") + " |")
	return nil
}

// runForgeStatusLine outputs status for a forge session.
// Shows: MQ length, current item, hook or drums preview
func runForgeStatusLine(t *tmux.Tmux, rigName string) error {
	if rigName == "" {
		// Try to extract from session name: gt-<warband>-forge
		if strings.HasPrefix(statusLineSession, "hd-") && strings.HasSuffix(statusLineSession, "-forge") {
			rigName = strings.TrimPrefix(statusLineSession, "hd-")
			rigName = strings.TrimSuffix(rigName, "-forge")
		}
	}

	if rigName == "" {
		fmt.Printf("%s ? |", AgentTypeIcons[AgentForge])
		return nil
	}

	// Get encampment root from forge pane's working directory
	var townRoot string
	sessionName := fmt.Sprintf("hd-%s-forge", rigName)
	paneDir, err := t.GetPaneWorkDir(sessionName)
	if err == nil && paneDir != "" {
		townRoot, _ = workspace.Find(paneDir)
	}

	// Get forge manager using shared helper
	mgr, _, _, err := getForgeManager(rigName)
	if err != nil {
		// Fallback to simple status if we can't access forge
		fmt.Printf("%s MQ: ? |", AgentTypeIcons[AgentForge])
		return nil
	}

	// Get queue
	queue, err := mgr.Queue()
	if err != nil {
		// Fallback to simple status if we can't read queue
		fmt.Printf("%s MQ: ? |", AgentTypeIcons[AgentForge])
		return nil
	}

	// Count pending items and find current item
	pending := 0
	var currentItem string
	for _, item := range queue {
		if item.Position == 0 && item.MR != nil {
			// Currently processing - show issue ID
			currentItem = item.MR.IssueID
		} else {
			pending++
		}
	}

	identity := fmt.Sprintf("%s/forge", rigName)

	// Build status
	var parts []string
	if currentItem != "" {
		parts = append(parts, fmt.Sprintf("merging %s", currentItem))
		if pending > 0 {
			parts = append(parts, fmt.Sprintf("+%d queued", pending))
		}
	} else if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", pending))
	} else {
		parts = append(parts, "idle")
	}

	// Priority 1: Check for bannered work (warband relics for forge)
	hookedWork := ""
	if townRoot != "" && rigName != "" {
		rigRelicsDir := filepath.Join(townRoot, rigName, "warchief", "warband")
		hookedWork = getHookedWork(identity, 25, rigRelicsDir)
	}
	if hookedWork != "" {
		parts = append(parts, fmt.Sprintf("🪝 %s", hookedWork))
	} else if townRoot != "" {
		// Priority 2: Fall back to drums preview
		unread, subject := getMailPreviewWithRoot(identity, 30, townRoot)
		if unread > 0 {
			if subject != "" {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %s", subject))
			} else {
				parts = append(parts, fmt.Sprintf("\U0001F4EC %d", unread))
			}
		}
	}

	fmt.Print(strings.Join(parts, " | ") + " |")
	return nil
}

// isSessionWorking detects if a Claude Code session is actively working.
// Returns true if the ✻ symbol is visible in the pane (indicates Claude is processing).
// Returns false for idle sessions (showing ❯ prompt) or if state cannot be determined.
func isSessionWorking(t *tmux.Tmux, session string) bool {
	// Capture last few lines of the pane
	lines, err := t.CapturePaneLines(session, 5)
	if err != nil || len(lines) == 0 {
		return false
	}

	// Check all captured lines for the working indicator
	// ✻ appears in Claude's status line when actively processing
	for _, line := range lines {
		if strings.Contains(line, "✻") {
			return true
		}
	}

	return false
}

// getUnreadMailCount returns unread drums count for an identity.
// Fast path - returns 0 on any error.
func getUnreadMailCount(identity string) int {
	// Find workspace
	workDir, err := findMailWorkDir()
	if err != nil {
		return 0
	}

	// Create wardrums using relics
	wardrums := drums.NewMailboxRelics(identity, workDir)

	// Get count
	_, unread, err := wardrums.Count()
	if err != nil {
		return 0
	}

	return unread
}

// getMailPreview returns unread count and a truncated subject of the first unread message.
// Returns (count, subject) where subject is empty if no unread drums.
func getMailPreview(identity string, maxLen int) (int, string) {
	workDir, err := findMailWorkDir()
	if err != nil {
		return 0, ""
	}

	wardrums := drums.NewMailboxRelics(identity, workDir)

	// Get unread messages
	messages, err := wardrums.ListUnread()
	if err != nil || len(messages) == 0 {
		return 0, ""
	}

	// Get first message subject, truncated
	subject := messages[0].Subject
	if len(subject) > maxLen {
		subject = subject[:maxLen-1] + "…"
	}

	return len(messages), subject
}

// getMailPreviewWithRoot is like getMailPreview but uses an explicit encampment root.
func getMailPreviewWithRoot(identity string, maxLen int, townRoot string) (int, string) {
	// Use NewMailboxFromAddress to normalize identity (e.g., horde/clan/gus -> horde/gus)
	wardrums := drums.NewMailboxFromAddress(identity, townRoot)

	// Get unread messages
	messages, err := wardrums.ListUnread()
	if err != nil || len(messages) == 0 {
		return 0, ""
	}

	// Get first message subject, truncated
	subject := messages[0].Subject
	if len(subject) > maxLen {
		subject = subject[:maxLen-1] + "…"
	}

	return len(messages), subject
}

// getHookedWork returns a truncated title of the bannered bead for an agent.
// Returns empty string if nothing is bannered.
// relicsDir should be the directory containing .relics (for warband-level) or
// empty to use the encampment root (for encampment-level roles).
func getHookedWork(identity string, maxLen int, relicsDir string) string {
	// If no relicsDir specified, use encampment root
	if relicsDir == "" {
		var err error
		relicsDir, err = findMailWorkDir()
		if err != nil {
			return ""
		}
	}

	b := relics.New(relicsDir)

	// Query for bannered relics assigned to this agent
	hookedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: identity,
		Priority: -1,
	})
	if err != nil || len(hookedRelics) == 0 {
		return ""
	}

	// Return first bannered bead's ID and title, truncated
	bead := hookedRelics[0]
	display := fmt.Sprintf("%s: %s", bead.ID, bead.Title)
	if len(display) > maxLen {
		display = display[:maxLen-1] + "…"
	}
	return display
}

// getCurrentWork returns a truncated title of the first in_progress issue.
// Uses the pane's working directory to find the relics.
func getCurrentWork(t *tmux.Tmux, session string, maxLen int) string {
	// Get the pane's working directory
	workDir, err := t.GetPaneWorkDir(session)
	if err != nil || workDir == "" {
		return ""
	}

	// Check if there's a .relics directory
	relicsDir := filepath.Join(workDir, ".relics")
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		return ""
	}

	// Query relics for in_progress issues
	b := relics.New(workDir)
	issues, err := b.List(relics.ListOptions{
		Status:   "in_progress",
		Priority: -1,
	})
	if err != nil || len(issues) == 0 {
		return ""
	}

	// Return first issue's ID and title, truncated
	issue := issues[0]
	display := fmt.Sprintf("%s: %s", issue.ID, issue.Title)
	if len(display) > maxLen {
		display = display[:maxLen-1] + "…"
	}
	return display
}
