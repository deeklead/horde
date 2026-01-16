package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/constants"
	"github.com/OWNER/horde/internal/clan"
	"github.com/OWNER/horde/internal/git"
	"github.com/OWNER/horde/internal/drums"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/tmux"
	"github.com/OWNER/horde/internal/workspace"
	"golang.org/x/term"
)

var statusJSON bool
var statusFast bool
var statusWatch bool
var statusInterval int
var statusVerbose bool

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"stat"},
	GroupID: GroupDiag,
	Short:   "Show overall encampment status",
	Long: `Display the current status of the Horde workspace.

Shows encampment name, registered warbands, active raiders, and witness status.

Use --fast to skip drums lookups for faster execution.
Use --watch to continuously refresh status at regular intervals.`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")
	statusCmd.Flags().BoolVar(&statusFast, "fast", false, "Skip drums lookups for faster execution")
	statusCmd.Flags().BoolVarP(&statusWatch, "watch", "w", false, "Watch mode: refresh status continuously")
	statusCmd.Flags().IntVarP(&statusInterval, "interval", "n", 2, "Refresh interval in seconds")
	statusCmd.Flags().BoolVarP(&statusVerbose, "verbose", "v", false, "Show detailed multi-line output per agent")
	rootCmd.AddCommand(statusCmd)
}

// TownStatus represents the overall status of the workspace.
type TownStatus struct {
	Name     string         `json:"name"`
	Location string         `json:"location"`
	Overseer *OverseerInfo  `json:"overseer,omitempty"` // Human operator
	Agents   []AgentRuntime `json:"agents"`             // Global agents (Warchief, Shaman)
	Warbands     []RigStatus    `json:"warbands"`
	Summary  StatusSum      `json:"summary"`
}

// OverseerInfo represents the human operator's identity and status.
type OverseerInfo struct {
	Name       string `json:"name"`
	Email      string `json:"email,omitempty"`
	Username   string `json:"username,omitempty"`
	Source     string `json:"source"`
	UnreadMail int    `json:"unread_mail"`
}

// AgentRuntime represents the runtime state of an agent.
type AgentRuntime struct {
	Name         string `json:"name"`                    // Display name (e.g., "warchief", "witness")
	Address      string `json:"address"`                 // Full address (e.g., "greenplace/witness")
	Session      string `json:"session"`                 // tmux session name
	Role         string `json:"role"`                    // Role type
	Running      bool   `json:"running"`                 // Is tmux session running?
	HasWork      bool   `json:"has_work"`                // Has pinned work?
	WorkTitle    string `json:"work_title,omitempty"`    // Title of pinned work
	BannerBead     string `json:"banner_bead,omitempty"`     // Pinned bead ID from agent bead
	State        string `json:"state,omitempty"`         // Agent state from agent bead
	UnreadMail   int    `json:"unread_mail"`             // Number of unread messages
	FirstSubject string `json:"first_subject,omitempty"` // Subject of first unread message
}

// RigStatus represents status of a single warband.
type RigStatus struct {
	Name         string          `json:"name"`
	Raiders     []string        `json:"raiders"`
	RaiderCount int             `json:"raider_count"`
	Clans        []string        `json:"clans"`
	CrewCount    int             `json:"crew_count"`
	HasWitness   bool            `json:"has_witness"`
	HasForge  bool            `json:"has_forge"`
	Hooks        []AgentHookInfo `json:"hooks,omitempty"`
	Agents       []AgentRuntime  `json:"agents,omitempty"` // Runtime state of all agents in warband
	MQ           *MQSummary      `json:"mq,omitempty"`     // Merge queue summary
}

// MQSummary represents the merge queue status for a warband.
type MQSummary struct {
	Pending  int    `json:"pending"`   // Open MRs ready to merge (no blockers)
	InFlight int    `json:"in_flight"` // MRs currently being processed
	Blocked  int    `json:"blocked"`   // MRs waiting on dependencies
	State    string `json:"state"`     // idle, processing, or blocked
	Health   string `json:"health"`    // healthy, stale, or empty
}

// AgentHookInfo represents an agent's hook (pinned work) status.
type AgentHookInfo struct {
	Agent    string `json:"agent"`              // Agent address (e.g., "greenplace/toast", "greenplace/witness")
	Role     string `json:"role"`               // Role type (raider, clan, witness, forge)
	HasWork  bool   `json:"has_work"`           // Whether agent has pinned work
	Totem string `json:"totem,omitempty"` // Attached totem ID
	Title    string `json:"title,omitempty"`    // Pinned bead title
}

// StatusSum provides summary counts.
type StatusSum struct {
	RigCount      int `json:"rig_count"`
	RaiderCount  int `json:"raider_count"`
	CrewCount     int `json:"crew_count"`
	WitnessCount  int `json:"witness_count"`
	ForgeCount int `json:"forge_count"`
	ActiveHooks   int `json:"active_hooks"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	if statusWatch {
		return runStatusWatch(cmd, args)
	}
	return runStatusOnce(cmd, args)
}

func runStatusWatch(cmd *cobra.Command, args []string) error {
	if statusJSON {
		return fmt.Errorf("--json and --watch cannot be used together")
	}
	if statusInterval <= 0 {
		return fmt.Errorf("interval must be positive, got %d", statusInterval)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	ticker := time.NewTicker(time.Duration(statusInterval) * time.Second)
	defer ticker.Stop()

	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	for {
		if isTTY {
			fmt.Print("\033[H\033[2J") // ANSI: cursor home + clear screen
		}

		timestamp := time.Now().Format("15:04:05")
		header := fmt.Sprintf("[%s] hd status --watch (every %ds, Ctrl+C to stop)", timestamp, statusInterval)
		if isTTY {
			fmt.Printf("%s\n\n", style.Dim.Render(header))
		} else {
			fmt.Printf("%s\n\n", header)
		}

		if err := runStatusOnce(cmd, args); err != nil {
			fmt.Printf("Error: %v\n", err)
		}

		select {
		case <-sigChan:
			if isTTY {
				fmt.Println("\nStopped.")
			}
			return nil
		case <-ticker.C:
		}
	}
}

func runStatusOnce(_ *cobra.Command, _ []string) error {
	// Find encampment root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Check rl daemon health and attempt restart if needed
	// This is non-blocking - if daemons can't be started, we show a warning but continue
	bdWarning := relics.EnsureBdDaemonHealth(townRoot)

	// Load encampment config
	townConfigPath := constants.WarchiefTownPath(townRoot)
	townConfig, err := config.LoadTownConfig(townConfigPath)
	if err != nil {
		// Try to continue without config
		townConfig = &config.TownConfig{Name: filepath.Base(townRoot)}
	}

	// Load warbands config
	rigsConfigPath := constants.WarchiefRigsPath(townRoot)
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		// Empty config if file doesn't exist
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	// Create warband manager
	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)

	// Create tmux instance for runtime checks
	t := tmux.NewTmux()

	// Pre-fetch all tmux sessions for O(1) lookup
	allSessions := make(map[string]bool)
	if sessions, err := t.ListSessions(); err == nil {
		for _, s := range sessions {
			allSessions[s] = true
		}
	}

	// Discover warbands
	warbands, err := mgr.DiscoverRigs()
	if err != nil {
		return fmt.Errorf("discovering warbands: %w", err)
	}

	// Pre-fetch agent relics across all warband-specific relics DBs.
	allAgentRelics := make(map[string]*relics.Issue)
	allBannerRelics := make(map[string]*relics.Issue)

	// Fetch encampment-level agent relics (Warchief, Shaman) from encampment relics
	townRelicsPath := relics.GetTownRelicsPath(townRoot)
	townRelicsClient := relics.New(townRelicsPath)
	townAgentRelics, _ := townRelicsClient.ListAgentRelics()
	for id, issue := range townAgentRelics {
		allAgentRelics[id] = issue
	}

	// Fetch hook relics from encampment relics
	var townHookIDs []string
	for _, issue := range townAgentRelics {
		hookID := issue.BannerBead
		if hookID == "" {
			fields := relics.ParseAgentFields(issue.Description)
			if fields != nil {
				hookID = fields.BannerBead
			}
		}
		if hookID != "" {
			townHookIDs = append(townHookIDs, hookID)
		}
	}
	if len(townHookIDs) > 0 {
		townBannerRelics, _ := townRelicsClient.ShowMultiple(townHookIDs)
		for id, issue := range townBannerRelics {
			allBannerRelics[id] = issue
		}
	}

	// Fetch warband-level agent relics
	for _, r := range warbands {
		rigRelicsPath := filepath.Join(r.Path, "warchief", "warband")
		rigRelics := relics.New(rigRelicsPath)
		rigAgentRelics, _ := rigRelics.ListAgentRelics()
		if rigAgentRelics == nil {
			continue
		}
		for id, issue := range rigAgentRelics {
			allAgentRelics[id] = issue
		}

		var hookIDs []string
		for _, issue := range rigAgentRelics {
			// Use the BannerBead field from the database column; fall back for legacy relics.
			hookID := issue.BannerBead
			if hookID == "" {
				fields := relics.ParseAgentFields(issue.Description)
				if fields != nil {
					hookID = fields.BannerBead
				}
			}
			if hookID != "" {
				hookIDs = append(hookIDs, hookID)
			}
		}

		if len(hookIDs) == 0 {
			continue
		}
		bannerRelics, _ := rigRelics.ShowMultiple(hookIDs)
		for id, issue := range bannerRelics {
			allBannerRelics[id] = issue
		}
	}

	// Create drums router for inbox lookups
	mailRouter := drums.NewRouter(townRoot)

	// Load overseer config
	var overseerInfo *OverseerInfo
	if overseerConfig, err := config.LoadOrDetectOverseer(townRoot); err == nil && overseerConfig != nil {
		overseerInfo = &OverseerInfo{
			Name:     overseerConfig.Name,
			Email:    overseerConfig.Email,
			Username: overseerConfig.Username,
			Source:   overseerConfig.Source,
		}
		// Get overseer drums count
		if wardrums, err := mailRouter.GetMailbox("overseer"); err == nil {
			_, unread, _ := wardrums.Count()
			overseerInfo.UnreadMail = unread
		}
	}

	// Build status - parallel fetch global agents and warbands
	status := TownStatus{
		Name:     townConfig.Name,
		Location: townRoot,
		Overseer: overseerInfo,
		Warbands:     make([]RigStatus, len(warbands)),
	}

	var wg sync.WaitGroup

	// Fetch global agents in parallel with warband discovery
	wg.Add(1)
	go func() {
		defer wg.Done()
		status.Agents = discoverGlobalAgents(allSessions, allAgentRelics, allBannerRelics, mailRouter, statusFast)
	}()

	// Process all warbands in parallel
	rigActiveHooks := make([]int, len(warbands)) // Track hooks per warband for thread safety
	for i, r := range warbands {
		wg.Add(1)
		go func(idx int, r *warband.Warband) {
			defer wg.Done()

			rs := RigStatus{
				Name:         r.Name,
				Raiders:     r.Raiders,
				RaiderCount: len(r.Raiders),
				HasWitness:   r.HasWitness,
				HasForge:  r.HasForge,
			}

			// Count clan workers
			crewGit := git.NewGit(r.Path)
			crewMgr := clan.NewManager(r, crewGit)
			if workers, err := crewMgr.List(); err == nil {
				for _, w := range workers {
					rs.Clans = append(rs.Clans, w.Name)
				}
				rs.CrewCount = len(workers)
			}

			// Discover hooks for all agents in this warband
			rs.Hooks = discoverRigHooks(r, rs.Clans)
			activeHooks := 0
			for _, hook := range rs.Hooks {
				if hook.HasWork {
					activeHooks++
				}
			}
			rigActiveHooks[idx] = activeHooks

			// Discover runtime state for all agents in this warband
			rs.Agents = discoverRigAgents(allSessions, r, rs.Clans, allAgentRelics, allBannerRelics, mailRouter, statusFast)

			// Get MQ summary if warband has a forge
			rs.MQ = getMQSummary(r)

			status.Warbands[idx] = rs
		}(i, r)
	}

	wg.Wait()

	// Aggregate summary (after parallel work completes)
	for i, rs := range status.Warbands {
		status.Summary.RaiderCount += rs.RaiderCount
		status.Summary.CrewCount += rs.CrewCount
		status.Summary.ActiveHooks += rigActiveHooks[i]
		if rs.HasWitness {
			status.Summary.WitnessCount++
		}
		if rs.HasForge {
			status.Summary.ForgeCount++
		}
	}
	status.Summary.RigCount = len(warbands)

	// Output
	if statusJSON {
		return outputStatusJSON(status)
	}
	if err := outputStatusText(status); err != nil {
		return err
	}

	// Show rl daemon warning at the end if there were issues
	if bdWarning != "" {
		fmt.Printf("%s %s\n", style.Warning.Render("⚠"), bdWarning)
		fmt.Printf("  Run 'bd daemon killall && rl daemon --start' to restart daemons\n")
	}

	return nil
}

func outputStatusJSON(status TownStatus) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(status)
}

func outputStatusText(status TownStatus) error {
	// Header
	fmt.Printf("%s %s\n", style.Bold.Render("Encampment:"), status.Name)
	fmt.Printf("%s\n\n", style.Dim.Render(status.Location))

	// Overseer info
	if status.Overseer != nil {
		overseerDisplay := status.Overseer.Name
		if status.Overseer.Email != "" {
			overseerDisplay = fmt.Sprintf("%s <%s>", status.Overseer.Name, status.Overseer.Email)
		} else if status.Overseer.Username != "" && status.Overseer.Username != status.Overseer.Name {
			overseerDisplay = fmt.Sprintf("%s (@%s)", status.Overseer.Name, status.Overseer.Username)
		}
		fmt.Printf("👤 %s %s\n", style.Bold.Render("Overseer:"), overseerDisplay)
		if status.Overseer.UnreadMail > 0 {
			fmt.Printf("   📬 %d unread\n", status.Overseer.UnreadMail)
		}
		fmt.Println()
	}

	// Role icons - uses centralized emojis from constants package
	roleIcons := map[string]string{
		constants.RoleWarchief:    constants.EmojiWarchief,
		constants.RoleShaman:   constants.EmojiShaman,
		constants.RoleWitness:  constants.EmojiWitness,
		constants.RoleForge: constants.EmojiForge,
		constants.RoleCrew:     constants.EmojiCrew,
		constants.RoleRaider:  constants.EmojiRaider,
		// Legacy names for backwards compatibility
		"coordinator":  constants.EmojiWarchief,
		"health-check": constants.EmojiShaman,
	}

	// Global Agents (Warchief, Shaman)
	for _, agent := range status.Agents {
		icon := roleIcons[agent.Role]
		if icon == "" {
			icon = roleIcons[agent.Name]
		}
		if statusVerbose {
			fmt.Printf("%s %s\n", icon, style.Bold.Render(capitalizeFirst(agent.Name)))
			renderAgentDetails(agent, "   ", nil, status.Location)
			fmt.Println()
		} else {
			// Compact: icon + name on one line
			renderAgentCompact(agent, icon+" ", nil, status.Location)
		}
	}
	if !statusVerbose && len(status.Agents) > 0 {
		fmt.Println()
	}

	if len(status.Warbands) == 0 {
		fmt.Printf("%s\n", style.Dim.Render("No warbands registered. Use 'hd warband add' to add one."))
		return nil
	}

	// Warbands
	for _, r := range status.Warbands {
		// Warband header with separator
		fmt.Printf("─── %s ───────────────────────────────────────────\n\n", style.Bold.Render(r.Name+"/"))

		// Group agents by role
		var witnesses, refineries, clans, raiders []AgentRuntime
		for _, agent := range r.Agents {
			switch agent.Role {
			case "witness":
				witnesses = append(witnesses, agent)
			case "forge":
				refineries = append(refineries, agent)
			case "clan":
				clans = append(clans, agent)
			case "raider":
				raiders = append(raiders, agent)
			}
		}

		// Witness
		if len(witnesses) > 0 {
			if statusVerbose {
				fmt.Printf("%s %s\n", roleIcons["witness"], style.Bold.Render("Witness"))
				for _, agent := range witnesses {
					renderAgentDetails(agent, "   ", r.Hooks, status.Location)
				}
				fmt.Println()
			} else {
				for _, agent := range witnesses {
					renderAgentCompact(agent, roleIcons["witness"]+" ", r.Hooks, status.Location)
				}
			}
		}

		// Forge
		if len(refineries) > 0 {
			if statusVerbose {
				fmt.Printf("%s %s\n", roleIcons["forge"], style.Bold.Render("Forge"))
				for _, agent := range refineries {
					renderAgentDetails(agent, "   ", r.Hooks, status.Location)
				}
				// MQ summary (shown under forge)
				if r.MQ != nil {
					mqStr := formatMQSummary(r.MQ)
					if mqStr != "" {
						fmt.Printf("   MQ: %s\n", mqStr)
					}
				}
				fmt.Println()
			} else {
				for _, agent := range refineries {
					// Compact: include MQ on same line if present
					mqSuffix := ""
					if r.MQ != nil {
						mqStr := formatMQSummaryCompact(r.MQ)
						if mqStr != "" {
							mqSuffix = "  " + mqStr
						}
					}
					renderAgentCompactWithSuffix(agent, roleIcons["forge"]+" ", r.Hooks, status.Location, mqSuffix)
				}
			}
		}

		// Clan
		if len(clans) > 0 {
			if statusVerbose {
				fmt.Printf("%s %s (%d)\n", roleIcons["clan"], style.Bold.Render("Clan"), len(clans))
				for _, agent := range clans {
					renderAgentDetails(agent, "   ", r.Hooks, status.Location)
				}
				fmt.Println()
			} else {
				fmt.Printf("%s %s (%d)\n", roleIcons["clan"], style.Bold.Render("Clan"), len(clans))
				for _, agent := range clans {
					renderAgentCompact(agent, "   ", r.Hooks, status.Location)
				}
			}
		}

		// Raiders
		if len(raiders) > 0 {
			if statusVerbose {
				fmt.Printf("%s %s (%d)\n", roleIcons["raider"], style.Bold.Render("Raiders"), len(raiders))
				for _, agent := range raiders {
					renderAgentDetails(agent, "   ", r.Hooks, status.Location)
				}
				fmt.Println()
			} else {
				fmt.Printf("%s %s (%d)\n", roleIcons["raider"], style.Bold.Render("Raiders"), len(raiders))
				for _, agent := range raiders {
					renderAgentCompact(agent, "   ", r.Hooks, status.Location)
				}
			}
		}

		// No agents
		if len(witnesses) == 0 && len(refineries) == 0 && len(clans) == 0 && len(raiders) == 0 {
			fmt.Printf("   %s\n", style.Dim.Render("(no agents)"))
		}
		fmt.Println()
	}

	return nil
}

// renderAgentDetails renders full agent bead details
func renderAgentDetails(agent AgentRuntime, indent string, hooks []AgentHookInfo, townRoot string) { //nolint:unparam // indent kept for future customization
	// Line 1: Agent bead ID + status
	// Per gt-zecmc: derive status from tmux (observable reality), not bead state.
	// "Discover, don't track" - agent liveness is observable from tmux session.
	sessionExists := agent.Running

	var statusStr string
	var stateInfo string

	if sessionExists {
		statusStr = style.Success.Render("running")
	} else {
		statusStr = style.Error.Render("stopped")
	}

	// Show non-observable states that represent intentional agent decisions.
	// These can't be discovered from tmux and are legitimately recorded in relics.
	beadState := agent.State
	switch beadState {
	case "stuck":
		// Agent escalated - needs help
		stateInfo = style.Warning.Render(" [stuck]")
	case "awaiting-gate":
		// Agent waiting for external trigger (phase gate)
		stateInfo = style.Dim.Render(" [awaiting-gate]")
	case "muted", "paused", "degraded":
		// Other intentional non-observable states
		stateInfo = style.Dim.Render(fmt.Sprintf(" [%s]", beadState))
	// Ignore observable states: "running", "idle", "dead", "done", "stopped", ""
	// These should be derived from tmux, not bead.
	}

	// Build agent bead ID using canonical naming: prefix-warband-role-name
	agentBeadID := "hd-" + agent.Name
	if agent.Address != "" && agent.Address != agent.Name {
		// Use address for full path agents like horde/clan/joe → gt-horde-clan-joe
		addr := strings.TrimSuffix(agent.Address, "/") // Remove trailing slash for global agents
		parts := strings.Split(addr, "/")
		if len(parts) == 1 {
			// Global agent: warchief/, shaman/ → hq-warchief, hq-shaman
			agentBeadID = relics.AgentBeadIDWithPrefix(relics.TownRelicsPrefix, "", parts[0], "")
		} else if len(parts) >= 2 {
			warband := parts[0]
			prefix := relics.GetPrefixForRig(townRoot, warband)
			if parts[1] == "clan" && len(parts) >= 3 {
				agentBeadID = relics.CrewBeadIDWithPrefix(prefix, warband, parts[2])
			} else if parts[1] == "witness" {
				agentBeadID = relics.WitnessBeadIDWithPrefix(prefix, warband)
			} else if parts[1] == "forge" {
				agentBeadID = relics.ForgeBeadIDWithPrefix(prefix, warband)
			} else if len(parts) == 2 {
				// raider: warband/name
				agentBeadID = relics.RaiderBeadIDWithPrefix(prefix, warband, parts[1])
			}
		}
	}

	fmt.Printf("%s%s %s%s\n", indent, style.Dim.Render(agentBeadID), statusStr, stateInfo)

	// Line 2: Hook bead (pinned work)
	hookStr := style.Dim.Render("(none)")
	bannerBead := agent.BannerBead
	hookTitle := agent.WorkTitle

	// Fall back to hooks array if agent bead doesn't have hook info
	if bannerBead == "" && hooks != nil {
		for _, h := range hooks {
			if h.Agent == agent.Address && h.HasWork {
				bannerBead = h.Totem
				hookTitle = h.Title
				break
			}
		}
	}

	if bannerBead != "" {
		if hookTitle != "" {
			hookStr = fmt.Sprintf("%s → %s", bannerBead, truncateWithEllipsis(hookTitle, 40))
		} else {
			hookStr = bannerBead
		}
	} else if hookTitle != "" {
		// Has title but no totem ID
		hookStr = truncateWithEllipsis(hookTitle, 50)
	}

	fmt.Printf("%s  hook: %s\n", indent, hookStr)

	// Line 3: Drums (if any unread)
	if agent.UnreadMail > 0 {
		mailStr := fmt.Sprintf("📬 %d unread", agent.UnreadMail)
		if agent.FirstSubject != "" {
			mailStr = fmt.Sprintf("📬 %d unread → %s", agent.UnreadMail, truncateWithEllipsis(agent.FirstSubject, 35))
		}
		fmt.Printf("%s  drums: %s\n", indent, mailStr)
	}
}

// formatMQSummary formats the MQ status for verbose display
func formatMQSummary(mq *MQSummary) string {
	if mq == nil {
		return ""
	}
	mqParts := []string{}
	if mq.Pending > 0 {
		mqParts = append(mqParts, fmt.Sprintf("%d pending", mq.Pending))
	}
	if mq.InFlight > 0 {
		mqParts = append(mqParts, style.Warning.Render(fmt.Sprintf("%d in-flight", mq.InFlight)))
	}
	if mq.Blocked > 0 {
		mqParts = append(mqParts, style.Dim.Render(fmt.Sprintf("%d blocked", mq.Blocked)))
	}
	if len(mqParts) == 0 {
		return ""
	}
	// Add state indicator
	stateIcon := "○" // idle
	switch mq.State {
	case "processing":
		stateIcon = style.Success.Render("●")
	case "blocked":
		stateIcon = style.Error.Render("○")
	}
	// Add health warning if stale
	healthSuffix := ""
	if mq.Health == "stale" {
		healthSuffix = style.Error.Render(" [stale]")
	}
	return fmt.Sprintf("%s %s%s", stateIcon, strings.Join(mqParts, ", "), healthSuffix)
}

// formatMQSummaryCompact formats MQ status for compact single-line display
func formatMQSummaryCompact(mq *MQSummary) string {
	if mq == nil {
		return ""
	}
	// Very compact: "MQ:12" or "MQ:12 [stale]"
	total := mq.Pending + mq.InFlight + mq.Blocked
	if total == 0 {
		return ""
	}
	healthSuffix := ""
	if mq.Health == "stale" {
		healthSuffix = style.Error.Render("[stale]")
	}
	return fmt.Sprintf("MQ:%d%s", total, healthSuffix)
}

// renderAgentCompactWithSuffix renders a single-line agent status with an extra suffix
func renderAgentCompactWithSuffix(agent AgentRuntime, indent string, hooks []AgentHookInfo, _ string, suffix string) {
	// Build status indicator (gt-zecmc: use tmux state, not bead state)
	statusIndicator := buildStatusIndicator(agent)

	// Get hook info
	bannerBead := agent.BannerBead
	hookTitle := agent.WorkTitle
	if bannerBead == "" && hooks != nil {
		for _, h := range hooks {
			if h.Agent == agent.Address && h.HasWork {
				bannerBead = h.Totem
				hookTitle = h.Title
				break
			}
		}
	}

	// Build hook suffix
	hookSuffix := ""
	if bannerBead != "" {
		if hookTitle != "" {
			hookSuffix = style.Dim.Render(" → ") + truncateWithEllipsis(hookTitle, 30)
		} else {
			hookSuffix = style.Dim.Render(" → ") + bannerBead
		}
	} else if hookTitle != "" {
		hookSuffix = style.Dim.Render(" → ") + truncateWithEllipsis(hookTitle, 30)
	}

	// Drums indicator
	mailSuffix := ""
	if agent.UnreadMail > 0 {
		mailSuffix = fmt.Sprintf(" 📬%d", agent.UnreadMail)
	}

	// Print single line: name + status + hook + drums + suffix
	fmt.Printf("%s%-12s %s%s%s%s\n", indent, agent.Name, statusIndicator, hookSuffix, mailSuffix, suffix)
}

// renderAgentCompact renders a single-line agent status
func renderAgentCompact(agent AgentRuntime, indent string, hooks []AgentHookInfo, _ string) {
	// Build status indicator (gt-zecmc: use tmux state, not bead state)
	statusIndicator := buildStatusIndicator(agent)

	// Get hook info
	bannerBead := agent.BannerBead
	hookTitle := agent.WorkTitle
	if bannerBead == "" && hooks != nil {
		for _, h := range hooks {
			if h.Agent == agent.Address && h.HasWork {
				bannerBead = h.Totem
				hookTitle = h.Title
				break
			}
		}
	}

	// Build hook suffix
	hookSuffix := ""
	if bannerBead != "" {
		if hookTitle != "" {
			hookSuffix = style.Dim.Render(" → ") + truncateWithEllipsis(hookTitle, 30)
		} else {
			hookSuffix = style.Dim.Render(" → ") + bannerBead
		}
	} else if hookTitle != "" {
		hookSuffix = style.Dim.Render(" → ") + truncateWithEllipsis(hookTitle, 30)
	}

	// Drums indicator
	mailSuffix := ""
	if agent.UnreadMail > 0 {
		mailSuffix = fmt.Sprintf(" 📬%d", agent.UnreadMail)
	}

	// Print single line: name + status + hook + drums
	fmt.Printf("%s%-12s %s%s%s\n", indent, agent.Name, statusIndicator, hookSuffix, mailSuffix)
}

// buildStatusIndicator creates the visual status indicator for an agent.
// Per gt-zecmc: uses tmux state (observable reality), not bead state.
// Non-observable states (stuck, awaiting-gate, muted, etc.) are shown as suffixes.
func buildStatusIndicator(agent AgentRuntime) string {
	sessionExists := agent.Running

	// Base indicator from tmux state
	var indicator string
	if sessionExists {
		indicator = style.Success.Render("●")
	} else {
		indicator = style.Error.Render("○")
	}

	// Add non-observable state suffix if present
	beadState := agent.State
	switch beadState {
	case "stuck":
		indicator += style.Warning.Render(" stuck")
	case "awaiting-gate":
		indicator += style.Dim.Render(" gate")
	case "muted", "paused", "degraded":
		indicator += style.Dim.Render(" " + beadState)
	// Ignore observable states: running, idle, dead, done, stopped, ""
	}

	return indicator
}

// formatHookInfo formats the hook bead and title for display
func formatHookInfo(bannerBead, title string, maxLen int) string {
	if bannerBead == "" {
		return ""
	}
	if title == "" {
		return fmt.Sprintf(" → %s", bannerBead)
	}
	title = truncateWithEllipsis(title, maxLen)
	return fmt.Sprintf(" → %s", title)
}

// truncateWithEllipsis shortens a string to maxLen, adding "..." if truncated
func truncateWithEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// capitalizeFirst capitalizes the first letter of a string
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}

// discoverRigHooks finds all hook attachments for agents in a warband.
// It scans raiders, clan workers, witness, and forge for handoff relics.
func discoverRigHooks(r *warband.Warband, clans []string) []AgentHookInfo {
	var hooks []AgentHookInfo

	// Create relics instance for the warband
	b := relics.New(r.Path)

	// Check raiders
	for _, name := range r.Raiders {
		hook := getAgentHook(b, name, r.Name+"/"+name, "raider")
		hooks = append(hooks, hook)
	}

	// Check clan workers
	for _, name := range clans {
		hook := getAgentHook(b, name, r.Name+"/clan/"+name, "clan")
		hooks = append(hooks, hook)
	}

	// Check witness
	if r.HasWitness {
		hook := getAgentHook(b, "witness", r.Name+"/witness", "witness")
		hooks = append(hooks, hook)
	}

	// Check forge
	if r.HasForge {
		hook := getAgentHook(b, "forge", r.Name+"/forge", "forge")
		hooks = append(hooks, hook)
	}

	return hooks
}

// discoverGlobalAgents checks runtime state for encampment-level agents (Warchief, Shaman).
// Uses parallel fetching for performance. If skipMail is true, drums lookups are skipped.
// allSessions is a preloaded map of tmux sessions for O(1) lookup.
// allAgentRelics is a preloaded map of agent relics for O(1) lookup.
// allBannerRelics is a preloaded map of hook relics for O(1) lookup.
func discoverGlobalAgents(allSessions map[string]bool, allAgentRelics map[string]*relics.Issue, allBannerRelics map[string]*relics.Issue, mailRouter *drums.Router, skipMail bool) []AgentRuntime {
	// Get session names dynamically
	warchiefSession := getWarchiefSessionName()
	shamanSession := getShamanSessionName()

	// Define agents to discover
	// Note: Warchief and Shaman are encampment-level agents with hq- prefix bead IDs
	agentDefs := []struct {
		name    string
		address string
		session string
		role    string
		beadID  string
	}{
		{"warchief", "warchief/", warchiefSession, "coordinator", relics.WarchiefBeadIDTown()},
		{"shaman", "shaman/", shamanSession, "health-check", relics.ShamanBeadIDTown()},
	}

	agents := make([]AgentRuntime, len(agentDefs))
	var wg sync.WaitGroup

	for i, def := range agentDefs {
		wg.Add(1)
		go func(idx int, d struct {
			name    string
			address string
			session string
			role    string
			beadID  string
		}) {
			defer wg.Done()

			agent := AgentRuntime{
				Name:    d.name,
				Address: d.address,
				Session: d.session,
				Role:    d.role,
			}

			// Check tmux session from preloaded map (O(1))
			agent.Running = allSessions[d.session]

			// Look up agent bead from preloaded map (O(1))
			if issue, ok := allAgentRelics[d.beadID]; ok {
				// Prefer SQLite columns over description parsing
				// BannerBead column is authoritative (cleared by unsling)
				agent.BannerBead = issue.BannerBead
				agent.State = issue.AgentState
				if agent.BannerBead != "" {
					agent.HasWork = true
					// Get hook title from preloaded map
					if pinnedIssue, ok := allBannerRelics[agent.BannerBead]; ok {
						agent.WorkTitle = pinnedIssue.Title
					}
				}
				// Fallback to description for legacy relics without SQLite columns
				if agent.State == "" {
					fields := relics.ParseAgentFields(issue.Description)
					if fields != nil {
						agent.State = fields.AgentState
					}
				}
			}

			// Get drums info (skip if --fast)
			if !skipMail {
				populateMailInfo(&agent, mailRouter)
			}

			agents[idx] = agent
		}(i, def)
	}

	wg.Wait()
	return agents
}

// populateMailInfo fetches unread drums count and first subject for an agent
func populateMailInfo(agent *AgentRuntime, router *drums.Router) {
	if router == nil {
		return
	}
	wardrums, err := router.GetMailbox(agent.Address)
	if err != nil {
		return
	}
	_, unread, _ := wardrums.Count()
	agent.UnreadMail = unread
	if unread > 0 {
		if messages, err := wardrums.ListUnread(); err == nil && len(messages) > 0 {
			agent.FirstSubject = messages[0].Subject
		}
	}
}

// agentDef defines an agent to discover
type agentDef struct {
	name    string
	address string
	session string
	role    string
	beadID  string
}

// discoverRigAgents checks runtime state for all agents in a warband.
// Uses parallel fetching for performance. If skipMail is true, drums lookups are skipped.
// allSessions is a preloaded map of tmux sessions for O(1) lookup.
// allAgentRelics is a preloaded map of agent relics for O(1) lookup.
// allBannerRelics is a preloaded map of hook relics for O(1) lookup.
func discoverRigAgents(allSessions map[string]bool, r *warband.Warband, clans []string, allAgentRelics map[string]*relics.Issue, allBannerRelics map[string]*relics.Issue, mailRouter *drums.Router, skipMail bool) []AgentRuntime {
	// Build list of all agents to discover
	var defs []agentDef
	townRoot := filepath.Dir(r.Path)
	prefix := relics.GetPrefixForRig(townRoot, r.Name)

	// Witness
	if r.HasWitness {
		defs = append(defs, agentDef{
			name:    "witness",
			address: r.Name + "/witness",
			session: witnessSessionName(r.Name),
			role:    "witness",
			beadID:  relics.WitnessBeadIDWithPrefix(prefix, r.Name),
		})
	}

	// Forge
	if r.HasForge {
		defs = append(defs, agentDef{
			name:    "forge",
			address: r.Name + "/forge",
			session: fmt.Sprintf("gt-%s-forge", r.Name),
			role:    "forge",
			beadID:  relics.ForgeBeadIDWithPrefix(prefix, r.Name),
		})
	}

	// Raiders
	for _, name := range r.Raiders {
		defs = append(defs, agentDef{
			name:    name,
			address: r.Name + "/" + name,
			session: fmt.Sprintf("gt-%s-%s", r.Name, name),
			role:    "raider",
			beadID:  relics.RaiderBeadIDWithPrefix(prefix, r.Name, name),
		})
	}

	// Clan
	for _, name := range clans {
		defs = append(defs, agentDef{
			name:    name,
			address: r.Name + "/clan/" + name,
			session: crewSessionName(r.Name, name),
			role:    "clan",
			beadID:  relics.CrewBeadIDWithPrefix(prefix, r.Name, name),
		})
	}

	if len(defs) == 0 {
		return nil
	}

	// Fetch all agents in parallel
	agents := make([]AgentRuntime, len(defs))
	var wg sync.WaitGroup

	for i, def := range defs {
		wg.Add(1)
		go func(idx int, d agentDef) {
			defer wg.Done()

			agent := AgentRuntime{
				Name:    d.name,
				Address: d.address,
				Session: d.session,
				Role:    d.role,
			}

			// Check tmux session from preloaded map (O(1))
			agent.Running = allSessions[d.session]

			// Look up agent bead from preloaded map (O(1))
			if issue, ok := allAgentRelics[d.beadID]; ok {
				// Prefer SQLite columns over description parsing
				// BannerBead column is authoritative (cleared by unsling)
				agent.BannerBead = issue.BannerBead
				agent.State = issue.AgentState
				if agent.BannerBead != "" {
					agent.HasWork = true
					// Get hook title from preloaded map
					if pinnedIssue, ok := allBannerRelics[agent.BannerBead]; ok {
						agent.WorkTitle = pinnedIssue.Title
					}
				}
				// Fallback to description for legacy relics without SQLite columns
				if agent.State == "" {
					fields := relics.ParseAgentFields(issue.Description)
					if fields != nil {
						agent.State = fields.AgentState
					}
				}
			}

			// Get drums info (skip if --fast)
			if !skipMail {
				populateMailInfo(&agent, mailRouter)
			}

			agents[idx] = agent
		}(i, def)
	}

	wg.Wait()
	return agents
}

// getMQSummary queries relics for merge-request issues and returns a summary.
// Returns nil if the warband has no forge or no MQ issues.
func getMQSummary(r *warband.Warband) *MQSummary {
	if !r.HasForge {
		return nil
	}

	// Create relics instance for the warband
	b := relics.New(r.RelicsPath())

	// Query for all open merge-request type issues
	opts := relics.ListOptions{
		Type:     "merge-request",
		Status:   "open",
		Priority: -1, // No priority filter
	}
	openMRs, err := b.List(opts)
	if err != nil {
		return nil
	}

	// Query for in-progress merge-requests
	opts.Status = "in_progress"
	inProgressMRs, err := b.List(opts)
	if err != nil {
		return nil
	}

	// Count pending (open with no blockers) vs blocked
	pending := 0
	blocked := 0
	for _, mr := range openMRs {
		if len(mr.BlockedBy) > 0 || mr.BlockedByCount > 0 {
			blocked++
		} else {
			pending++
		}
	}

	// Determine queue state
	state := "idle"
	if len(inProgressMRs) > 0 {
		state = "processing"
	} else if pending > 0 {
		state = "idle" // Has work but not processing yet
	} else if blocked > 0 {
		state = "blocked" // Only blocked items, nothing processable
	}

	// Determine queue health
	health := "empty"
	total := pending + len(inProgressMRs) + blocked
	if total > 0 {
		health = "healthy"
		// Check for potential issues
		if pending > 10 && len(inProgressMRs) == 0 {
			// Large queue but nothing processing - may be stuck
			health = "stale"
		}
	}

	// Only return summary if there's something to show
	if pending == 0 && len(inProgressMRs) == 0 && blocked == 0 {
		return nil
	}

	return &MQSummary{
		Pending:  pending,
		InFlight: len(inProgressMRs),
		Blocked:  blocked,
		State:    state,
		Health:   health,
	}
}

// getAgentHook retrieves hook status for a specific agent.
func getAgentHook(b *relics.Relics, role, agentAddress, roleType string) AgentHookInfo {
	hook := AgentHookInfo{
		Agent: agentAddress,
		Role:  roleType,
	}

	// Find handoff bead for this role
	handoff, err := b.FindHandoffBead(role)
	if err != nil || handoff == nil {
		return hook
	}

	// Check for attachment
	attachment := relics.ParseAttachmentFields(handoff)
	if attachment != nil && attachment.AttachedMolecule != "" {
		hook.HasWork = true
		hook.Totem = attachment.AttachedMolecule
		hook.Title = handoff.Title
	} else if handoff.Description != "" {
		// Has content but no totem - still has work
		hook.HasWork = true
		hook.Title = handoff.Title
	}

	return hook
}
