package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

// beadInfo holds status and assignee for a bead.
type beadInfo struct {
	Title    string `json:"title"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

// verifyBeadExists checks that the bead exists using rl show.
// Uses bd's native prefix-based routing via routes.jsonl - do NOT set RELICS_DIR
// as that overrides routing and breaks resolution of warband-level relics.
//
// Uses --no-daemon with --allow-stale to avoid daemon socket timing issues
// while still finding relics when database is out of sync with JSONL.
// For existence checks, stale data is acceptable - we just need to know it exists.
func verifyBeadExists(beadID string) error {
	cmd := exec.Command("rl", "--no-daemon", "show", beadID, "--json", "--allow-stale")
	// Run from encampment root so rl can find routes.jsonl for prefix-based routing.
	// Do NOT set RELICS_DIR - that overrides routing and breaks warband bead resolution.
	if townRoot, err := workspace.FindFromCwd(); err == nil {
		cmd.Dir = townRoot
	}
	// Use Output() instead of Run() to detect rl --no-daemon exit 0 bug:
	// when issue not found, --no-daemon exits 0 but produces empty stdout.
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("bead '%s' not found (bd show failed)", beadID)
	}
	if len(out) == 0 {
		return fmt.Errorf("bead '%s' not found", beadID)
	}
	return nil
}

// getBeadInfo returns status and assignee for a bead.
// Uses bd's native prefix-based routing via routes.jsonl.
// Uses --no-daemon with --allow-stale for consistency with verifyBeadExists.
func getBeadInfo(beadID string) (*beadInfo, error) {
	cmd := exec.Command("rl", "--no-daemon", "show", beadID, "--json", "--allow-stale")
	// Run from encampment root so rl can find routes.jsonl for prefix-based routing.
	if townRoot, err := workspace.FindFromCwd(); err == nil {
		cmd.Dir = townRoot
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bead '%s' not found", beadID)
	}
	// Handle rl --no-daemon exit 0 bug: when issue not found,
	// --no-daemon exits 0 but produces empty stdout (error goes to stderr).
	if len(out) == 0 {
		return nil, fmt.Errorf("bead '%s' not found", beadID)
	}
	// rl show --json returns an array (issue + dependents), take first element
	var infos []beadInfo
	if err := json.Unmarshal(out, &infos); err != nil {
		return nil, fmt.Errorf("parsing bead info: %w", err)
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("bead '%s' not found", beadID)
	}
	return &infos[0], nil
}

// storeArgsInBead stores args in the bead's description using attached_args field.
// This enables no-tmux mode where agents discover args via hd rally / rl show.
func storeArgsInBead(beadID, args string) error {
	// Get the bead to preserve existing description content
	showCmd := exec.Command("rl", "--no-daemon", "show", beadID, "--json", "--allow-stale")
	out, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("fetching bead: %w", err)
	}
	// Handle rl --no-daemon exit 0 bug: empty stdout means not found
	if len(out) == 0 {
		return fmt.Errorf("bead not found")
	}

	// Parse the bead
	var issues []relics.Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return fmt.Errorf("parsing bead: %w", err)
	}
	if len(issues) == 0 {
		return fmt.Errorf("bead not found")
	}
	issue := &issues[0]

	// Get or create attachment fields
	fields := relics.ParseAttachmentFields(issue)
	if fields == nil {
		fields = &relics.AttachmentFields{}
	}

	// Set the args
	fields.AttachedArgs = args

	// Update the description
	newDesc := relics.SetAttachmentFields(issue, fields)

	// Update the bead
	updateCmd := exec.Command("rl", "--no-daemon", "update", beadID, "--description="+newDesc)
	updateCmd.Stderr = os.Stderr
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("updating bead description: %w", err)
	}

	return nil
}

// storeDispatcherInBead stores the dispatcher agent ID in the bead's description.
// This enables raiders to notify the dispatcher when work is complete.
func storeDispatcherInBead(beadID, dispatcher string) error {
	if dispatcher == "" {
		return nil
	}

	// Get the bead to preserve existing description content
	showCmd := exec.Command("rl", "show", beadID, "--json")
	out, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("fetching bead: %w", err)
	}

	// Parse the bead
	var issues []relics.Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return fmt.Errorf("parsing bead: %w", err)
	}
	if len(issues) == 0 {
		return fmt.Errorf("bead not found")
	}
	issue := &issues[0]

	// Get or create attachment fields
	fields := relics.ParseAttachmentFields(issue)
	if fields == nil {
		fields = &relics.AttachmentFields{}
	}

	// Set the dispatcher
	fields.DispatchedBy = dispatcher

	// Update the description
	newDesc := relics.SetAttachmentFields(issue, fields)

	// Update the bead
	updateCmd := exec.Command("rl", "update", beadID, "--description="+newDesc)
	updateCmd.Stderr = os.Stderr
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("updating bead description: %w", err)
	}

	return nil
}

// injectStartPrompt sends a prompt to the target pane to start working.
// Uses the reliable signal pattern: literal mode + 500ms debounce + separate Enter.
func injectStartPrompt(pane, beadID, subject, args string) error {
	if pane == "" {
		return fmt.Errorf("no target pane")
	}

	// Skip signal during tests to prevent agent self-interruption
	if os.Getenv("HD_TEST_NO_NUDGE") != "" {
		return nil
	}

	// Build the prompt to inject
	var prompt string
	if args != "" {
		// Args provided - include them prominently in the prompt
		if subject != "" {
			prompt = fmt.Sprintf("Work charged: %s (%s). Args: %s. Start working now - use these args to guide your execution.", beadID, subject, args)
		} else {
			prompt = fmt.Sprintf("Work charged: %s. Args: %s. Start working now - use these args to guide your execution.", beadID, args)
		}
	} else if subject != "" {
		prompt = fmt.Sprintf("Work charged: %s (%s). Start working on it now - no questions, just begin.", beadID, subject)
	} else {
		prompt = fmt.Sprintf("Work charged: %s. Start working on it now - run `hd hook` to see the hook, then begin.", beadID)
	}

	// Use the reliable signal pattern (same as hd signal / tmux.SignalSession)
	t := tmux.NewTmux()
	return t.NudgePane(pane, prompt)
}

// getSessionFromPane extracts session name from a pane target.
// Pane targets can be:
// - "%9" (pane ID) - need to query tmux for session
// - "gt-warband-name:0.0" (session:window.pane) - extract session name
func getSessionFromPane(pane string) string {
	if strings.HasPrefix(pane, "%") {
		// Pane ID format - query tmux for the session
		cmd := exec.Command("tmux", "display-message", "-t", pane, "-p", "#{session_name}")
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	// Session:window.pane format - extract session name
	if idx := strings.Index(pane, ":"); idx > 0 {
		return pane[:idx]
	}
	return pane
}

// ensureAgentReady waits for an agent to be ready before nudging an existing session.
// Uses a pragmatic approach: wait for the pane to leave a shell, then (Claude-only)
// accept the bypass permissions warning and give it a moment to finish initializing.
func ensureAgentReady(sessionName string) error {
	t := tmux.NewTmux()

	// If an agent is already running, assume it's ready (session was started earlier)
	if t.IsAgentRunning(sessionName) {
		return nil
	}

	// Agent not running yet - wait for it to start (shell → program transition)
	if err := t.WaitForCommand(sessionName, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		return fmt.Errorf("waiting for agent to start: %w", err)
	}

	// Claude-only: accept bypass permissions warning if present
	if t.IsClaudeRunning(sessionName) {
		_ = t.AcceptBypassPermissionsWarning(sessionName)

		// PRAGMATIC APPROACH: fixed delay rather than prompt detection.
		// Claude startup takes ~5-8 seconds on typical machines.
		time.Sleep(8 * time.Second)
	} else {
		time.Sleep(1 * time.Second)
	}

	return nil
}

// detectCloneRoot finds the root of the current git clone.
func detectCloneRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// detectActor returns the current agent's actor string for event logging.
func detectActor() string {
	roleInfo, err := GetRole()
	if err != nil {
		return "unknown"
	}
	return roleInfo.ActorString()
}

// agentIDToBeadID converts an agent ID to its corresponding agent bead ID.
// Uses canonical naming: prefix-warband-role-name
// Encampment-level agents (Warchief, Shaman) use hq- prefix and are stored in encampment relics.
// Warband-level agents use the warband's configured prefix (default "hd-").
// townRoot is needed to look up the warband's configured prefix.
func agentIDToBeadID(agentID, townRoot string) string {
	// Handle simple cases (encampment-level agents with hq- prefix)
	if agentID == "warchief" {
		return relics.WarchiefBeadIDTown()
	}
	if agentID == "shaman" {
		return relics.ShamanBeadIDTown()
	}

	// Parse path-style agent IDs
	parts := strings.Split(agentID, "/")
	if len(parts) < 2 {
		return ""
	}

	warband := parts[0]
	prefix := relics.GetPrefixForRig(townRoot, warband)

	switch {
	case len(parts) == 2 && parts[1] == "witness":
		return relics.WitnessBeadIDWithPrefix(prefix, warband)
	case len(parts) == 2 && parts[1] == "forge":
		return relics.ForgeBeadIDWithPrefix(prefix, warband)
	case len(parts) == 3 && parts[1] == "clan":
		return relics.CrewBeadIDWithPrefix(prefix, warband, parts[2])
	case len(parts) == 3 && parts[1] == "raiders":
		return relics.RaiderBeadIDWithPrefix(prefix, warband, parts[2])
	default:
		return ""
	}
}

// updateAgentBannerBead updates the agent bead's state and hook when work is charged.
// This enables the witness to see that each agent is working.
//
// We run from the raider's workDir (which redirects to the warband's relics database)
// WITHOUT setting RELICS_DIR, so the redirect mechanism works for gt-* agent relics.
//
// For warband-level relics (same database), we set the banner_bead slot directly.
// For cross-database scenarios (agent in warband db, hook bead in encampment db),
// the slot set may fail - this is handled gracefully with a warning.
// The work is still correctly attached via `rl update <bead> --assignee=<agent>`.
func updateAgentBannerBead(agentID, beadID, workDir, townRelicsDir string) {
	_ = townRelicsDir // Not used - RELICS_DIR breaks redirect mechanism

	// Determine the directory to run rl commands from:
	// - If workDir is provided (raider's clone path), use it for redirect-based routing
	// - Otherwise fall back to encampment root
	bdWorkDir := workDir
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		// Not in a Horde workspace - can't update agent bead
		fmt.Fprintf(os.Stderr, "Warning: couldn't find encampment root to update agent hook: %v\n", err)
		return
	}
	if bdWorkDir == "" {
		bdWorkDir = townRoot
	}

	// Convert agent ID to agent bead ID
	// Format examples (canonical: prefix-warband-role-name):
	//   greenplace/clan/max -> gt-greenplace-clan-max
	//   greenplace/raiders/Toast -> gt-greenplace-raider-Toast
	//   warchief -> hq-warchief
	//   greenplace/witness -> gt-greenplace-witness
	agentBeadID := agentIDToBeadID(agentID, townRoot)
	if agentBeadID == "" {
		return
	}

	// Run from workDir WITHOUT RELICS_DIR to enable redirect-based routing.
	// Set banner_bead to the charged work (gt-zecmc: removed agent_state update).
	// Agent liveness is observable from tmux - no need to record it in bead.
	// For cross-database scenarios, slot set may fail gracefully (warning only).
	bd := relics.New(bdWorkDir)
	if err := bd.SetBannerBead(agentBeadID, beadID); err != nil {
		// Log warning instead of silent ignore - helps debug cross-relics issues
		fmt.Fprintf(os.Stderr, "Warning: couldn't set agent %s hook: %v\n", agentBeadID, err)
		return
	}
}

// wakeRigAgents wakes the witness and forge for a warband after raider dispatch.
// This ensures the scout agents are ready to monitor and merge.
func wakeRigAgents(rigName string) {
	// Boot the warband (idempotent - no-op if already running)
	bootCmd := exec.Command("hd", "warband", "boot", rigName)
	_ = bootCmd.Run() // Ignore errors - warband might already be running

	// Signal witness and forge to clear any backoff
	t := tmux.NewTmux()
	witnessSession := fmt.Sprintf("gt-%s-witness", rigName)
	forgeSession := fmt.Sprintf("gt-%s-forge", rigName)

	// Silent nudges - sessions might not exist yet
	_ = t.SignalSession(witnessSession, "Raider dispatched - check for work")
	_ = t.SignalSession(forgeSession, "Raider dispatched - check for merge requests")
}

// isRaiderTarget checks if the target string refers to a raider.
// Returns true if the target format is "warband/raiders/name".
// This is used to determine if we should respawn a dead raider
// instead of failing when charging work.
func isRaiderTarget(target string) bool {
	parts := strings.Split(target, "/")
	return len(parts) >= 3 && parts[1] == "raiders"
}

// attachRaiderWorkMolecule attaches the totem-raider-work totem to a raider's agent bead.
// This ensures all raiders have the standard work totem attached for guidance.
// The totem is attached by storing it in the agent bead's description using attachment fields.
//
// Per issue #288: hd charge should auto-summon totem-raider-work when charging to raiders.
func attachRaiderWorkMolecule(targetAgent, hookWorkDir, townRoot string) error {
	// Parse the raider name from targetAgent (format: "warband/raiders/name")
	parts := strings.Split(targetAgent, "/")
	if len(parts) != 3 || parts[1] != "raiders" {
		return fmt.Errorf("invalid raider agent format: %s", targetAgent)
	}
	rigName := parts[0]
	raiderName := parts[2]

	// Get the raider's agent bead ID
	// Format: "<prefix>-<warband>-raider-<name>" (e.g., "gt-horde-raider-Toast")
	prefix := config.GetRigPrefix(townRoot, rigName)
	agentBeadID := relics.RaiderBeadIDWithPrefix(prefix, rigName, raiderName)

	// Resolve the warband directory for running rl commands.
	// Use ResolveHookDir to ensure we run rl from the correct warband directory
	// (not from the raider's worktree, which doesn't have a .relics directory).
	// This fixes issue #197: raider fails to hook when charging with totem.
	rigDir := relics.ResolveHookDir(townRoot, prefix+"-"+raiderName, hookWorkDir)

	b := relics.New(rigDir)

	// Check if totem is already attached (avoid duplicate summon)
	attachment, err := b.GetAttachment(agentBeadID)
	if err == nil && attachment != nil && attachment.AttachedMolecule != "" {
		// Already has a totem attached - skip
		return nil
	}

	// Invoke the totem-raider-work ritual to ensure the proto exists
	// This is safe to run multiple times - cooking is idempotent
	cookCmd := exec.Command("rl", "--no-daemon", "invoke", "totem-raider-work")
	cookCmd.Dir = rigDir
	cookCmd.Stderr = os.Stderr
	if err := cookCmd.Run(); err != nil {
		return fmt.Errorf("cooking totem-raider-work ritual: %w", err)
	}

	// Summon the totem to the raider's agent bead
	// The totem ID is the ritual name "totem-raider-work"
	moleculeID := "totem-raider-work"
	_, err = b.AttachMolecule(agentBeadID, moleculeID)
	if err != nil {
		return fmt.Errorf("attaching totem %s to %s: %w", moleculeID, agentBeadID, err)
	}

	fmt.Printf("%s Attached %s to %s\n", style.Bold.Render("✓"), moleculeID, agentBeadID)
	return nil
}
