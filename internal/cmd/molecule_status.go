package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// Note: Agent field parsing is now in internal/relics/fields.go (AgentFields, ParseAgentFieldsFromDescription)

// buildAgentBeadID constructs the agent bead ID from an agent identity.
// Uses canonical naming: prefix-warband-role-name
// Encampment-level agents use hq- prefix; warband-level agents use warband's prefix.
// Examples:
//   - "warchief" -> "hq-warchief"
//   - "shaman" -> "hq-shaman"
//   - "horde/witness" -> "gt-horde-witness"
//   - "horde/forge" -> "gt-horde-forge"
//   - "horde/nux" (raider) -> "gt-horde-raider-nux"
//   - "horde/clan/max" -> "gt-horde-clan-max"
//
// If role is unknown, it tries to infer from the identity string.
// townRoot is needed to look up the warband's configured prefix.
func buildAgentBeadID(identity string, role Role, townRoot string) string {
	parts := strings.Split(identity, "/")

	// Helper to get prefix for a warband
	getPrefix := func(warband string) string {
		return config.GetRigPrefix(townRoot, warband)
	}

	// If role is unknown or empty, try to infer from identity
	if role == RoleUnknown || role == Role("") {
		switch {
		case identity == "warchief":
			return relics.WarchiefBeadIDTown()
		case identity == "shaman":
			return relics.ShamanBeadIDTown()
		case len(parts) == 2 && parts[1] == "witness":
			return relics.WitnessBeadIDWithPrefix(getPrefix(parts[0]), parts[0])
		case len(parts) == 2 && parts[1] == "forge":
			return relics.ForgeBeadIDWithPrefix(getPrefix(parts[0]), parts[0])
		case len(parts) == 2:
			// Assume warband/name is a raider
			return relics.RaiderBeadIDWithPrefix(getPrefix(parts[0]), parts[0], parts[1])
		case len(parts) == 3 && parts[1] == "clan":
			// warband/clan/name - clan member
			return relics.CrewBeadIDWithPrefix(getPrefix(parts[0]), parts[0], parts[2])
		case len(parts) == 3 && parts[1] == "raiders":
			// warband/raiders/name - explicit raider
			return relics.RaiderBeadIDWithPrefix(getPrefix(parts[0]), parts[0], parts[2])
		default:
			return ""
		}
	}

	switch role {
	case RoleWarchief:
		return relics.WarchiefBeadIDTown()
	case RoleShaman:
		return relics.ShamanBeadIDTown()
	case RoleWitness:
		if len(parts) >= 1 {
			return relics.WitnessBeadIDWithPrefix(getPrefix(parts[0]), parts[0])
		}
		return ""
	case RoleForge:
		if len(parts) >= 1 {
			return relics.ForgeBeadIDWithPrefix(getPrefix(parts[0]), parts[0])
		}
		return ""
	case RoleRaider:
		// Handle both 2-part (warband/name) and 3-part (warband/raiders/name) formats
		if len(parts) == 3 && parts[1] == "raiders" {
			return relics.RaiderBeadIDWithPrefix(getPrefix(parts[0]), parts[0], parts[2])
		}
		if len(parts) >= 2 {
			return relics.RaiderBeadIDWithPrefix(getPrefix(parts[0]), parts[0], parts[1])
		}
		return ""
	case RoleCrew:
		if len(parts) >= 3 && parts[1] == "clan" {
			return relics.CrewBeadIDWithPrefix(getPrefix(parts[0]), parts[0], parts[2])
		}
		return ""
	default:
		return ""
	}
}

// MoleculeProgressInfo contains progress information for a totem instance.
type MoleculeProgressInfo struct {
	RootID       string   `json:"root_id"`
	RootTitle    string   `json:"root_title"`
	MoleculeID   string   `json:"molecule_id,omitempty"`
	TotalSteps   int      `json:"total_steps"`
	DoneSteps    int      `json:"done_steps"`
	InProgress   int      `json:"in_progress_steps"`
	ReadySteps   []string `json:"ready_steps"`
	BlockedSteps []string `json:"blocked_steps"`
	Percent      int      `json:"percent_complete"`
	Complete     bool     `json:"complete"`
}

// MoleculeStatusInfo contains status information for an agent's work.
type MoleculeStatusInfo struct {
	Target           string                `json:"target"`
	Role             string                `json:"role"`
	AgentBeadID      string                `json:"agent_bead_id,omitempty"` // The agent bead if found
	HasWork          bool                  `json:"has_work"`
	PinnedBead       *relics.Issue          `json:"pinned_bead,omitempty"`
	AttachedMolecule string                `json:"attached_molecule,omitempty"`
	AttachedAt       string                `json:"attached_at,omitempty"`
	AttachedArgs     string                `json:"attached_args,omitempty"`
	IsWisp           bool                  `json:"is_wisp"`
	Progress         *MoleculeProgressInfo `json:"progress,omitempty"`
	NextAction       string                `json:"next_action,omitempty"`
}

// MoleculeCurrentInfo contains info about what an agent should be working on.
type MoleculeCurrentInfo struct {
	Identity      string `json:"identity"`
	HandoffID     string `json:"handoff_id,omitempty"`
	HandoffTitle  string `json:"handoff_title,omitempty"`
	MoleculeID    string `json:"molecule_id,omitempty"`
	MoleculeTitle string `json:"molecule_title,omitempty"`
	StepsComplete int    `json:"steps_complete"`
	StepsTotal    int    `json:"steps_total"`
	CurrentStepID string `json:"current_step_id,omitempty"`
	CurrentStep   string `json:"current_step,omitempty"`
	Status        string `json:"status"` // "working", "naked", "complete", "blocked"
}

func runMoleculeProgress(cmd *cobra.Command, args []string) error {
	rootID := args[0]

	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Get the root issue
	root, err := b.Show(rootID)
	if err != nil {
		return fmt.Errorf("getting root issue: %w", err)
	}

	// Find all children of the root issue
	children, err := b.List(relics.ListOptions{
		Parent:   rootID,
		Status:   "all",
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("listing children: %w", err)
	}

	if len(children) == 0 {
		return fmt.Errorf("no steps found for %s (not a totem root?)", rootID)
	}

	// Build progress info
	progress := MoleculeProgressInfo{
		RootID:    rootID,
		RootTitle: root.Title,
	}

	// Try to find totem ID from first child's description
	for _, child := range children {
		if molID := extractMoleculeID(child.Description); molID != "" {
			progress.MoleculeID = molID
			break
		}
	}

	// Build set of closed issue IDs for dependency checking
	closedIDs := make(map[string]bool)
	for _, child := range children {
		if child.Status == "closed" {
			closedIDs[child.ID] = true
		}
	}

	// Categorize steps
	for _, child := range children {
		progress.TotalSteps++

		switch child.Status {
		case "closed":
			progress.DoneSteps++
		case "in_progress":
			progress.InProgress++
		case "open":
			// Check if all dependencies are closed
			allDepsClosed := true
			for _, depID := range child.DependsOn {
				if !closedIDs[depID] {
					allDepsClosed = false
					break
				}
			}

			if len(child.DependsOn) == 0 || allDepsClosed {
				progress.ReadySteps = append(progress.ReadySteps, child.ID)
			} else {
				progress.BlockedSteps = append(progress.BlockedSteps, child.ID)
			}
		}
	}

	// Calculate completion percentage
	if progress.TotalSteps > 0 {
		progress.Percent = (progress.DoneSteps * 100) / progress.TotalSteps
	}
	progress.Complete = progress.DoneSteps == progress.TotalSteps

	// JSON output
	if moleculeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(progress)
	}

	// Human-readable output
	fmt.Printf("\n%s %s\n\n", style.Bold.Render("🧬 Totem Progress:"), root.Title)
	fmt.Printf("  Root: %s\n", rootID)
	if progress.MoleculeID != "" {
		fmt.Printf("  Totem: %s\n", progress.MoleculeID)
	}
	fmt.Println()

	// Progress bar
	barWidth := 20
	filled := (progress.Percent * barWidth) / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("  [%s] %d%% (%d/%d)\n\n", bar, progress.Percent, progress.DoneSteps, progress.TotalSteps)

	// Step status
	fmt.Printf("  Done:        %d\n", progress.DoneSteps)
	fmt.Printf("  In Progress: %d\n", progress.InProgress)
	fmt.Printf("  Ready:       %d", len(progress.ReadySteps))
	if len(progress.ReadySteps) > 0 {
		fmt.Printf(" (%s)", strings.Join(progress.ReadySteps, ", "))
	}
	fmt.Println()
	fmt.Printf("  Blocked:     %d\n", len(progress.BlockedSteps))

	if progress.Complete {
		fmt.Printf("\n  %s\n", style.Bold.Render("✓ Totem complete!"))
	}

	return nil
}

// extractMoleculeID extracts the totem ID from an issue's description.
func extractMoleculeID(description string) string {
	lines := strings.Split(description, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "instantiated_from:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "instantiated_from:"))
		}
	}
	return ""
}

func runMoleculeStatus(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Find encampment root
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	// Determine target agent
	var target string
	var roleCtx RoleContext

	if len(args) > 0 {
		// Explicit target provided
		target = args[0]
	} else {
		// Use cwd-based detection for status display
		// This ensures we show the hook for the agent whose directory we're in,
		// not the agent from the GT_ROLE env var (which might be different if
		// we cd'd into another warband's clan/raider directory)
		roleCtx = detectRole(cwd, townRoot)
		target = buildAgentIdentity(roleCtx)
		if target == "" {
			return fmt.Errorf("cannot determine agent identity (role: %s)", roleCtx.Role)
		}
	}

	// Find relics directory
	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Build status info
	status := MoleculeStatusInfo{
		Target: target,
		Role:   string(roleCtx.Role),
	}

	// Try to find agent bead and read banner slot
	// This is the preferred method - agent relics have a banner_bead field
	agentBeadID := buildAgentBeadID(target, roleCtx.Role, townRoot)
	var bannerBead *relics.Issue

	if agentBeadID != "" {
		// Try to fetch the agent bead
		agentBead, err := b.Show(agentBeadID)
		if err == nil && agentBead != nil && agentBead.Type == "agent" {
			status.AgentBeadID = agentBeadID

			// Read banner_bead from the agent bead's database field (not description!)
			// The banner_bead column is updated by `rl slot set` in UpdateAgentState.
			// IMPORTANT: Don't use ParseAgentFieldsFromDescription - the description
			// field may contain stale data, causing the wrong issue to be bannered.
			if agentBead.BannerBead != "" {
				// Fetch the bead on the hook
				bannerBead, err = b.Show(agentBead.BannerBead)
				if err != nil {
					// Hook bead referenced but not found - report error but continue
					bannerBead = nil
				}
			}
		}
		// If agent bead not found or not an agent type, fall through to legacy approach
	}

	// If we found a hook bead via agent bead, use it
	if bannerBead != nil {
		status.HasWork = true
		status.PinnedBead = bannerBead

		// Check for attached totem
		attachment := relics.ParseAttachmentFields(bannerBead)
		if attachment != nil {
			status.AttachedMolecule = attachment.AttachedMolecule
			status.AttachedAt = attachment.AttachedAt
			status.AttachedArgs = attachment.AttachedArgs

			// Check if it's a wisp
			status.IsWisp = strings.Contains(bannerBead.Description, "wisp: true") ||
				strings.Contains(bannerBead.Description, "is_wisp: true")

			// Get progress if there's an attached totem
			if attachment.AttachedMolecule != "" {
				progress, _ := getMoleculeProgressInfo(b, attachment.AttachedMolecule)
				status.Progress = progress
				status.NextAction = determineNextAction(status)
			}
		}
	} else {
		// FALLBACK: Query for bannered relics (work on agent's hook)
		// First try status=bannered (work that's been charged but not yet claimed)
		hookedRelics, err := b.List(relics.ListOptions{
			Status:   relics.StatusHooked,
			Assignee: target,
			Priority: -1,
		})
		if err != nil {
			return fmt.Errorf("listing bannered relics: %w", err)
		}

		// If no bannered relics found, also check in_progress relics assigned to this agent.
		// This handles the case where work was claimed (status changed to in_progress)
		// but the session was interrupted before completion. The hook should persist.
		if len(hookedRelics) == 0 {
			inProgressRelics, err := b.List(relics.ListOptions{
				Status:   "in_progress",
				Assignee: target,
				Priority: -1,
			})
			if err == nil && len(inProgressRelics) > 0 {
				// Use the first in_progress bead (should typically be only one)
				hookedRelics = inProgressRelics
			}
		}

		// For encampment-level roles (warchief, shaman), scan all warbands if nothing found locally
		if len(hookedRelics) == 0 && isTownLevelRole(target) {
			hookedRelics = scanAllRigsForHookedRelics(townRoot, target)
		}

		status.HasWork = len(hookedRelics) > 0

		if len(hookedRelics) > 0 {
			// Take the first bannered bead
			status.PinnedBead = hookedRelics[0]

			// Check for attached totem
			attachment := relics.ParseAttachmentFields(hookedRelics[0])
			if attachment != nil {
				status.AttachedMolecule = attachment.AttachedMolecule
				status.AttachedAt = attachment.AttachedAt
				status.AttachedArgs = attachment.AttachedArgs

				// Check if it's a wisp
				status.IsWisp = strings.Contains(hookedRelics[0].Description, "wisp: true") ||
					strings.Contains(hookedRelics[0].Description, "is_wisp: true")

				// Get progress if there's an attached totem
				if attachment.AttachedMolecule != "" {
					progress, _ := getMoleculeProgressInfo(b, attachment.AttachedMolecule)
					status.Progress = progress
					status.NextAction = determineNextAction(status)
				}
			}
		}
	}

	// Determine next action if no work is charged
	if !status.HasWork {
		status.NextAction = "Check inbox for work assignments: hd drums inbox"
	} else if status.AttachedMolecule == "" {
		status.NextAction = "Summon a totem to start work: hd mol summon <bead-id> <totem-id>"
	}

	// JSON output
	if moleculeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	// Human-readable output
	return outputMoleculeStatus(status)
}

// buildAgentIdentity constructs the agent identity string from role context.
// Encampment-level agents (warchief, shaman) use trailing slash to match the format
// used when setting assignee on bannered relics (see resolveSelfTarget in charge.go).
func buildAgentIdentity(ctx RoleContext) string {
	switch ctx.Role {
	case RoleWarchief:
		return "warchief/"
	case RoleShaman:
		return "shaman/"
	case RoleWitness:
		return ctx.Warband + "/witness"
	case RoleForge:
		return ctx.Warband + "/forge"
	case RoleRaider:
		return ctx.Warband + "/raiders/" + ctx.Raider
	case RoleCrew:
		return ctx.Warband + "/clan/" + ctx.Raider
	default:
		return ""
	}
}

// getMoleculeProgressInfo gets progress info for a totem instance.
func getMoleculeProgressInfo(b *relics.Relics, moleculeRootID string) (*MoleculeProgressInfo, error) {
	// Get the totem root issue
	root, err := b.Show(moleculeRootID)
	if err != nil {
		return nil, fmt.Errorf("getting totem root: %w", err)
	}

	// Find all children of the root issue
	children, err := b.List(relics.ListOptions{
		Parent:   moleculeRootID,
		Status:   "all",
		Priority: -1,
	})
	if err != nil {
		return nil, fmt.Errorf("listing children: %w", err)
	}

	if len(children) == 0 {
		// No children - might be a simple issue, not a totem
		return nil, nil
	}

	// Build progress info
	progress := &MoleculeProgressInfo{
		RootID:    moleculeRootID,
		RootTitle: root.Title,
	}

	// Try to find totem ID from first child's description
	for _, child := range children {
		if molID := extractMoleculeID(child.Description); molID != "" {
			progress.MoleculeID = molID
			break
		}
	}

	// Build set of closed issue IDs for dependency checking
	closedIDs := make(map[string]bool)
	for _, child := range children {
		if child.Status == "closed" {
			closedIDs[child.ID] = true
		}
	}

	// Categorize steps
	for _, child := range children {
		progress.TotalSteps++

		switch child.Status {
		case "closed":
			progress.DoneSteps++
		case "in_progress":
			progress.InProgress++
		case "open":
			// Check if all dependencies are closed
			allDepsClosed := true
			for _, depID := range child.DependsOn {
				if !closedIDs[depID] {
					allDepsClosed = false
					break
				}
			}

			if len(child.DependsOn) == 0 || allDepsClosed {
				progress.ReadySteps = append(progress.ReadySteps, child.ID)
			} else {
				progress.BlockedSteps = append(progress.BlockedSteps, child.ID)
			}
		}
	}

	// Calculate completion percentage
	if progress.TotalSteps > 0 {
		progress.Percent = (progress.DoneSteps * 100) / progress.TotalSteps
	}
	progress.Complete = progress.DoneSteps == progress.TotalSteps

	return progress, nil
}

// determineNextAction suggests the next action based on status.
func determineNextAction(status MoleculeStatusInfo) string {
	if status.Progress == nil {
		return ""
	}

	if status.Progress.Complete {
		return "Totem complete! Close the bead: rl close " + status.PinnedBead.ID
	}

	if status.Progress.InProgress > 0 {
		return "Continue working on in-progress steps"
	}

	if len(status.Progress.ReadySteps) > 0 {
		return fmt.Sprintf("Start next ready step: rl update %s --status=in_progress", status.Progress.ReadySteps[0])
	}

	if len(status.Progress.BlockedSteps) > 0 {
		return "All remaining steps are blocked - waiting on dependencies"
	}

	return ""
}

// outputMoleculeStatus outputs human-readable status.
func outputMoleculeStatus(status MoleculeStatusInfo) error {
	// Header with hook icon
	fmt.Printf("\n%s Hook Status: %s\n", style.Bold.Render("🪝"), status.Target)
	if status.Role != "" && status.Role != "unknown" {
		fmt.Printf("Role: %s\n", status.Role)
	}
	fmt.Println()

	if !status.HasWork {
		fmt.Printf("%s\n", style.Dim.Render("Nothing on hook - no work charged"))
		fmt.Printf("\n%s %s\n", style.Bold.Render("Next:"), status.NextAction)
		return nil
	}

	// Show bannered bead info
	if status.PinnedBead == nil {
		fmt.Printf("%s\n", style.Dim.Render("Work indicated but no bead found"))
		return nil
	}

	// AUTONOMOUS MODE banner - bannered work triggers autonomous execution
	fmt.Println(style.Bold.Render("🚀 AUTONOMOUS MODE - Work on hook triggers immediate execution"))
	fmt.Println()

	// Check if the bannered bead is already closed (someone closed it externally)
	if status.PinnedBead.Status == "closed" {
		fmt.Printf("%s Planted bead %s is already closed!\n", style.Bold.Render("⚠"), status.PinnedBead.ID)
		fmt.Printf("   Title: %s\n", status.PinnedBead.Title)
		fmt.Printf("   This work was completed elsewhere. Clear your hook with: hd unsling\n")
		return nil
	}

	// Check if this is a drums bead - display drums-specific format
	if status.PinnedBead.Type == "message" {
		sender := extractMailSender(status.PinnedBead.Labels)
		fmt.Printf("%s %s (drums)\n", style.Bold.Render("🪝 Hook:"), status.PinnedBead.ID)
		if sender != "" {
			fmt.Printf("   From: %s\n", sender)
		}
		fmt.Printf("   Subject: %s\n", status.PinnedBead.Title)
		fmt.Printf("   Run: hd drums read %s\n", status.PinnedBead.ID)
		return nil
	}

	fmt.Printf("%s %s: %s\n", style.Bold.Render("🪝 Planted:"), status.PinnedBead.ID, status.PinnedBead.Title)

	// Show attached totem
	if status.AttachedMolecule != "" {
		molType := "Totem"
		if status.IsWisp {
			molType = "Wisp"
		}
		fmt.Printf("%s %s: %s\n", style.Bold.Render("🧬 "+molType+":"), status.AttachedMolecule, "")
		if status.AttachedAt != "" {
			fmt.Printf("   Attached: %s\n", status.AttachedAt)
		}
		if status.AttachedArgs != "" {
			fmt.Printf("   %s %s\n", style.Bold.Render("Args:"), status.AttachedArgs)
		}
	} else {
		fmt.Printf("%s\n", style.Dim.Render("No totem attached (bannered bead still triggers autonomous work)"))
	}

	// Show progress if available
	if status.Progress != nil {
		fmt.Println()

		// Progress bar
		barWidth := 20
		filled := (status.Progress.Percent * barWidth) / 100
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		fmt.Printf("Progress: [%s] %d%% (%d/%d steps)\n",
			bar, status.Progress.Percent, status.Progress.DoneSteps, status.Progress.TotalSteps)

		// Step breakdown
		fmt.Printf("  Done:        %d\n", status.Progress.DoneSteps)
		fmt.Printf("  In Progress: %d\n", status.Progress.InProgress)
		fmt.Printf("  Ready:       %d", len(status.Progress.ReadySteps))
		if len(status.Progress.ReadySteps) > 0 && len(status.Progress.ReadySteps) <= 3 {
			fmt.Printf(" (%s)", strings.Join(status.Progress.ReadySteps, ", "))
		}
		fmt.Println()
		fmt.Printf("  Blocked:     %d\n", len(status.Progress.BlockedSteps))

		if status.Progress.Complete {
			fmt.Printf("\n%s\n", style.Bold.Render("✓ Totem complete!"))
		}
	}

	// Next action hint
	if status.NextAction != "" {
		fmt.Printf("\n%s %s\n", style.Bold.Render("Next:"), status.NextAction)
	}

	return nil
}

func runMoleculeCurrent(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Find encampment root
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	// Determine target agent identity
	var target string
	var roleCtx RoleContext

	if len(args) > 0 {
		// Explicit target provided
		target = args[0]
	} else {
		// Use cwd-based detection for status display
		// This ensures we show the hook for the agent whose directory we're in,
		// not the agent from the GT_ROLE env var (which might be different if
		// we cd'd into another warband's clan/raider directory)
		roleCtx = detectRole(cwd, townRoot)
		target = buildAgentIdentity(roleCtx)
		if target == "" {
			return fmt.Errorf("cannot determine agent identity (role: %s)", roleCtx.Role)
		}
	}

	// Find relics directory
	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Extract role from target for handoff bead lookup
	parts := strings.Split(target, "/")
	role := parts[len(parts)-1]

	// Find handoff bead for this identity
	handoff, err := b.FindHandoffBead(role)
	if err != nil {
		return fmt.Errorf("finding handoff bead: %w", err)
	}

	// Build current info
	info := MoleculeCurrentInfo{
		Identity: target,
	}

	if handoff == nil {
		info.Status = "naked"
		return outputMoleculeCurrent(info)
	}

	info.HandoffID = handoff.ID
	info.HandoffTitle = handoff.Title

	// Check for attached totem
	attachment := relics.ParseAttachmentFields(handoff)
	if attachment == nil || attachment.AttachedMolecule == "" {
		info.Status = "naked"
		return outputMoleculeCurrent(info)
	}

	info.MoleculeID = attachment.AttachedMolecule

	// Get the totem root to find its title and children
	molRoot, err := b.Show(attachment.AttachedMolecule)
	if err != nil {
		// Totem not found - might be a template ID, still report what we have
		info.Status = "working"
		return outputMoleculeCurrent(info)
	}

	info.MoleculeTitle = molRoot.Title

	// Find all children (steps) of the totem root
	children, err := b.List(relics.ListOptions{
		Parent:   attachment.AttachedMolecule,
		Status:   "all",
		Priority: -1,
	})
	if err != nil {
		// No steps - just an issue, not a totem instance
		info.Status = "working"
		return outputMoleculeCurrent(info)
	}

	info.StepsTotal = len(children)

	// Build set of closed issue IDs for dependency checking
	closedIDs := make(map[string]bool)
	var inProgressSteps []*relics.Issue
	var readySteps []*relics.Issue

	for _, child := range children {
		switch child.Status {
		case "closed":
			info.StepsComplete++
			closedIDs[child.ID] = true
		case "in_progress":
			inProgressSteps = append(inProgressSteps, child)
		}
	}

	// Find ready steps (open with all deps closed)
	for _, child := range children {
		if child.Status == "open" {
			allDepsClosed := true
			for _, depID := range child.DependsOn {
				if !closedIDs[depID] {
					allDepsClosed = false
					break
				}
			}
			if len(child.DependsOn) == 0 || allDepsClosed {
				readySteps = append(readySteps, child)
			}
		}
	}

	// Determine current step and status
	if info.StepsComplete == info.StepsTotal && info.StepsTotal > 0 {
		info.Status = "complete"
	} else if len(inProgressSteps) > 0 {
		// First in-progress step is the current one
		info.Status = "working"
		info.CurrentStepID = inProgressSteps[0].ID
		info.CurrentStep = inProgressSteps[0].Title
	} else if len(readySteps) > 0 {
		// First ready step is the next to work on
		info.Status = "working"
		info.CurrentStepID = readySteps[0].ID
		info.CurrentStep = readySteps[0].Title
	} else if info.StepsTotal > 0 {
		// Has steps but none ready or in-progress -> blocked
		info.Status = "blocked"
	} else {
		info.Status = "working"
	}

	return outputMoleculeCurrent(info)
}

// outputMoleculeCurrent outputs the current info in the appropriate format.
func outputMoleculeCurrent(info MoleculeCurrentInfo) error {
	if moleculeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	// Human-readable output matching spec format
	fmt.Printf("Identity: %s\n", info.Identity)

	if info.HandoffID != "" {
		fmt.Printf("Handoff:  %s (%s)\n", info.HandoffID, info.HandoffTitle)
	} else {
		fmt.Printf("Handoff:  %s\n", style.Dim.Render("(none)"))
	}

	if info.MoleculeID != "" {
		if info.MoleculeTitle != "" {
			fmt.Printf("Totem: %s (%s)\n", info.MoleculeID, info.MoleculeTitle)
		} else {
			fmt.Printf("Totem: %s\n", info.MoleculeID)
		}
	} else {
		fmt.Printf("Totem: %s\n", style.Dim.Render("(none attached)"))
	}

	if info.StepsTotal > 0 {
		fmt.Printf("Progress: %d/%d steps complete\n", info.StepsComplete, info.StepsTotal)
	}

	if info.CurrentStepID != "" {
		fmt.Printf("Current:  %s - %s\n", info.CurrentStepID, info.CurrentStep)
	} else if info.Status == "naked" {
		fmt.Printf("Status:   %s\n", style.Dim.Render("naked - awaiting work assignment"))
	} else if info.Status == "complete" {
		fmt.Printf("Status:   %s\n", style.Bold.Render("complete - totem finished"))
	} else if info.Status == "blocked" {
		fmt.Printf("Status:   %s\n", style.Dim.Render("blocked - waiting on dependencies"))
	}

	return nil
}

// getGitRootForMolStatus returns the git root for hook file lookup.
func getGitRootForMolStatus() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isTownLevelRole returns true if the agent ID is a encampment-level role.
// Encampment-level roles (Warchief, Shaman) operate from the encampment root and may have
// pinned relics in any warband's relics directory.
// Accepts both "warchief" and "warchief/" formats for compatibility.
func isTownLevelRole(agentID string) bool {
	return agentID == "warchief" || agentID == "warchief/" ||
		agentID == "shaman" || agentID == "shaman/"
}

// extractMailSender extracts the sender from drums bead labels.
// Drums relics have a "from:X" label containing the sender address.
func extractMailSender(labels []string) string {
	for _, label := range labels {
		if strings.HasPrefix(label, "from:") {
			return strings.TrimPrefix(label, "from:")
		}
	}
	return ""
}

// scanAllRigsForHookedRelics scans all registered warbands for bannered relics
// assigned to the target agent. Used for encampment-level roles that may have
// work bannered in any warband.
func scanAllRigsForHookedRelics(townRoot, target string) []*relics.Issue {
	// Load routes from encampment relics
	townRelicsDir := filepath.Join(townRoot, ".relics")
	routes, err := relics.LoadRoutes(townRelicsDir)
	if err != nil {
		return nil
	}

	// Scan each warband's relics directory
	for _, route := range routes {
		rigRelicsDir := filepath.Join(townRoot, route.Path)
		if _, err := os.Stat(rigRelicsDir); os.IsNotExist(err) {
			continue
		}

		b := relics.New(rigRelicsDir)

		// First check for bannered relics
		hookedRelics, err := b.List(relics.ListOptions{
			Status:   relics.StatusHooked,
			Assignee: target,
			Priority: -1,
		})
		if err != nil {
			continue
		}

		if len(hookedRelics) > 0 {
			return hookedRelics
		}

		// Also check for in_progress relics (work that was claimed but session interrupted)
		inProgressRelics, err := b.List(relics.ListOptions{
			Status:   "in_progress",
			Assignee: target,
			Priority: -1,
		})
		if err != nil {
			continue
		}

		if len(inProgressRelics) > 0 {
			return inProgressRelics
		}
	}

	return nil
}
