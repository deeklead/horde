package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/lock"
	"github.com/deeklead/horde/internal/state"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

var primeHookMode bool
var primeDryRun bool
var rallyState bool
var rallyStateJSON bool
var primeExplain bool

// Role represents a detected agent role.
type Role string

const (
	RoleWarchief    Role = "warchief"
	RoleShaman   Role = "shaman"
	RoleBoot     Role = "boot"
	RoleWitness  Role = "witness"
	RoleForge Role = "forge"
	RoleRaider  Role = "raider"
	RoleCrew     Role = "clan"
	RoleUnknown  Role = "unknown"
)

var primeCmd = &cobra.Command{
	Use:     "rally",
	GroupID: GroupDiag,
	Short:   "Output role context for current directory",
	Long: `Detect the agent role from the current directory and output context.

Role detection:
  - Encampment root, warchief/, or <warband>/warchief/ → Warchief context
  - <warband>/witness/warband/ → Witness context
  - <warband>/forge/warband/ → Forge context
  - <warband>/raiders/<name>/ → Raider context

This command is typically used in shell prompts or agent initialization.

HOOK MODE (--hook):
  When called as an LLM runtime hook, use --hook to enable session ID handling.
  This reads session metadata from stdin and persists it for the session.

  Claude Code integration (in .claude/settings.json):
    "SessionStart": [{"hooks": [{"type": "command", "command": "hd rally --hook"}]}]

  Claude Code sends JSON on stdin:
    {"session_id": "uuid", "transcript_path": "/path", "source": "startup|resume"}

  Other agents can set GT_SESSION_ID environment variable instead.`,
	RunE: runPrime,
}

func init() {
	primeCmd.Flags().BoolVar(&primeHookMode, "banner", false,
		"Hook mode: read session ID from stdin JSON (for LLM runtime hooks)")
	primeCmd.Flags().BoolVar(&primeDryRun, "dry-run", false,
		"Show what would be injected without side effects (no marker removal, no rl rally, no drums)")
	primeCmd.Flags().BoolVar(&rallyState, "state", false,
		"Show detected session state only (normal/post-handoff/crash/autonomous)")
	primeCmd.Flags().BoolVar(&rallyStateJSON, "json", false,
		"Output state as JSON (requires --state)")
	primeCmd.Flags().BoolVar(&primeExplain, "explain", false,
		"Show why each section was included")
	rootCmd.AddCommand(primeCmd)
}

// RoleContext is an alias for RoleInfo for backward compatibility.
// New code should use RoleInfo directly.
type RoleContext = RoleInfo

func runPrime(cmd *cobra.Command, args []string) error {
	// Validate flag combinations: --state is exclusive (except --json)
	if rallyState && (primeHookMode || primeDryRun || primeExplain) {
		return fmt.Errorf("--state cannot be combined with other flags (except --json)")
	}
	// --json requires --state
	if rallyStateJSON && !rallyState {
		return fmt.Errorf("--json requires --state")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}

	// "Discover, Don't Track" principle:
	// - If we're in a workspace, proceed - the workspace's existence IS the enable signal
	// - If we're NOT in a workspace, check the global enabled state
	// This ensures a missing/stale state file doesn't break workspace users
	if townRoot == "" {
		// Not in a workspace - check global enabled state
		// (This matters for hooks that might run from random directories)
		if !state.IsEnabled() {
			return nil // Silent exit - not in workspace and not enabled
		}
		return fmt.Errorf("not in a Horde workspace")
	}

	// Handle hook mode: read session ID from stdin and persist it
	if primeHookMode {
		sessionID, source := readHookSessionID()
		if !primeDryRun {
			persistSessionID(townRoot, sessionID)
			if cwd != townRoot {
				persistSessionID(cwd, sessionID)
			}
		}
		// Set environment for this process (affects event emission below)
		_ = os.Setenv("GT_SESSION_ID", sessionID)
		_ = os.Setenv("CLAUDE_SESSION_ID", sessionID) // Legacy compatibility
		// Output session beacon
		explain(true, "Session beacon: hook mode enabled, session ID from stdin")
		fmt.Printf("[session:%s]\n", sessionID)
		if source != "" {
			fmt.Printf("[source:%s]\n", source)
		}
	}

	// Check for handoff marker (prevents handoff loop bug)
	// In dry-run mode, use the non-mutating version
	if primeDryRun {
		checkHandoffMarkerDryRun(cwd)
	} else {
		checkHandoffMarker(cwd)
	}

	// Get role using env-aware detection
	roleInfo, err := GetRoleWithContext(cwd, townRoot)
	if err != nil {
		return fmt.Errorf("detecting role: %w", err)
	}

	// Warn prominently if there's a role/cwd mismatch
	if roleInfo.Mismatch {
		fmt.Printf("\n%s\n", style.Bold.Render("⚠️  ROLE/LOCATION MISMATCH"))
		fmt.Printf("You are %s (from $GT_ROLE) but your cwd suggests %s.\n",
			style.Bold.Render(string(roleInfo.Role)),
			style.Bold.Render(string(roleInfo.CwdRole)))
		fmt.Printf("Expected home: %s\n", roleInfo.Home)
		fmt.Printf("Actual cwd:    %s\n", cwd)
		fmt.Println()
		fmt.Println("This can cause commands to misbehave. Either:")
		fmt.Println("  1. cd to your home directory, OR")
		fmt.Println("  2. Use absolute paths for gt/bd commands")
		fmt.Println()
	}

	// Build RoleContext for compatibility with existing code
	ctx := RoleContext{
		Role:     roleInfo.Role,
		Warband:      roleInfo.Warband,
		Raider:  roleInfo.Raider,
		TownRoot: townRoot,
		WorkDir:  cwd,
	}

	// --state mode: output state only and exit
	if rallyState {
		outputState(ctx, rallyStateJSON)
		return nil
	}

	// Check and acquire identity lock for worker roles
	if !primeDryRun {
		if err := acquireIdentityLock(ctx); err != nil {
			return err
		}
	}

	// Ensure relics redirect exists for worktree-based roles
	// Skip if there's a role/location mismatch to avoid creating bad redirects
	if !roleInfo.Mismatch && !primeDryRun {
		ensureRelicsRedirect(ctx)
	}

	// Emit session_start event for seance discovery
	if !primeDryRun {
		emitSessionEvent(ctx)
	}

	// Output session metadata for seance discovery
	explain(true, "Session metadata: always included for seance discovery")
	outputSessionMetadata(ctx)

	// Output context
	explain(true, fmt.Sprintf("Role context: detected role is %s", ctx.Role))
	if err := outputPrimeContext(ctx); err != nil {
		return err
	}

	// Output handoff content if present
	outputHandoffContent(ctx)

	// Output attachment status (for autonomous work detection)
	outputAttachmentStatus(ctx)

	// Check for charged work on hook (from hd charge)
	// If found, we're in autonomous mode - skip normal startup directive
	hasSlungWork := checkSlungWork(ctx)
	explain(hasSlungWork, "Autonomous mode: bannered/in-progress work detected")

	// Output totem context if working on a totem step
	outputMoleculeContext(ctx)

	// Output previous session checkpoint for crash recovery
	outputCheckpointContext(ctx)

	// Run rl rally to output relics workflow context
	if !primeDryRun {
		runBdPrime(cwd)
	} else {
		explain(true, "bd rally: skipped in dry-run mode")
	}

	// Run hd drums check --inject to inject any pending drums
	if !primeDryRun {
		runMailCheckInject(cwd)
	} else {
		explain(true, "hd drums check --inject: skipped in dry-run mode")
	}

	// For Warchief, check for pending escalations
	if ctx.Role == RoleWarchief {
		checkPendingEscalations(ctx)
	}

	// Output startup directive for roles that should announce themselves
	// Skip if in autonomous mode (charged work provides its own directive)
	if !hasSlungWork {
		explain(true, "Startup directive: normal mode (no bannered work)")
		outputStartupDirective(ctx)
	}

	return nil
}

func detectRole(cwd, townRoot string) RoleInfo {
	ctx := RoleInfo{
		Role:     RoleUnknown,
		TownRoot: townRoot,
		WorkDir:  cwd,
		Source:   "cwd",
	}

	// Get relative path from encampment root
	relPath, err := filepath.Rel(townRoot, cwd)
	if err != nil {
		return ctx
	}

	// Normalize and split path
	relPath = filepath.ToSlash(relPath)
	parts := strings.Split(relPath, "/")

	// Check for warchief role
	// At encampment root, or in warchief/ or warchief/warband/
	if relPath == "." || relPath == "" {
		ctx.Role = RoleWarchief
		return ctx
	}
	if len(parts) >= 1 && parts[0] == "warchief" {
		ctx.Role = RoleWarchief
		return ctx
	}

	// Check for boot role: shaman/dogs/boot/
	// Must check before shaman since boot is under shaman directory
	if len(parts) >= 3 && parts[0] == "shaman" && parts[1] == "dogs" && parts[2] == "boot" {
		ctx.Role = RoleBoot
		return ctx
	}

	// Check for shaman role: shaman/
	if len(parts) >= 1 && parts[0] == "shaman" {
		ctx.Role = RoleShaman
		return ctx
	}

	// At this point, first part should be a warband name
	if len(parts) < 1 {
		return ctx
	}
	rigName := parts[0]
	ctx.Warband = rigName

	// Check for warchief: <warband>/warchief/ or <warband>/warchief/warband/
	if len(parts) >= 2 && parts[1] == "warchief" {
		ctx.Role = RoleWarchief
		return ctx
	}

	// Check for witness: <warband>/witness/warband/
	if len(parts) >= 2 && parts[1] == "witness" {
		ctx.Role = RoleWitness
		return ctx
	}

	// Check for forge: <warband>/forge/warband/
	if len(parts) >= 2 && parts[1] == "forge" {
		ctx.Role = RoleForge
		return ctx
	}

	// Check for raider: <warband>/raiders/<name>/
	if len(parts) >= 3 && parts[1] == "raiders" {
		ctx.Role = RoleRaider
		ctx.Raider = parts[2]
		return ctx
	}

	// Check for clan: <warband>/clan/<name>/
	if len(parts) >= 3 && parts[1] == "clan" {
		ctx.Role = RoleCrew
		ctx.Raider = parts[2] // Use Raider field for clan member name
		return ctx
	}

	// Default: could be warband root - treat as unknown
	return ctx
}

// runBdPrime runs `rl rally` and outputs the result.
// This provides relics workflow context to the agent.
func runBdPrime(workDir string) {
	cmd := exec.Command("rl", "rally")
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Skip if rl rally fails (relics might not be available)
		// But log stderr if present for debugging
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			fmt.Fprintf(os.Stderr, "bd rally: %s\n", errMsg)
		}
		return
	}

	output := strings.TrimSpace(stdout.String())
	if output != "" {
		fmt.Println()
		fmt.Println(output)
	}
}

// runMailCheckInject runs `hd drums check --inject` and outputs the result.
// This injects any pending drums into the agent's context.
func runMailCheckInject(workDir string) {
	cmd := exec.Command("hd", "drums", "check", "--inject")
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Skip if drums check fails, but log stderr for debugging
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			fmt.Fprintf(os.Stderr, "hd drums check: %s\n", errMsg)
		}
		return
	}

	output := strings.TrimSpace(stdout.String())
	if output != "" {
		fmt.Println()
		fmt.Println(output)
	}
}

// checkSlungWork checks for bannered work on the agent's hook.
// If found, displays AUTONOMOUS WORK MODE and tells the agent to execute immediately.
// Returns true if bannered work was found (caller should skip normal startup directive).
func checkSlungWork(ctx RoleContext) bool {
	// Determine agent identity
	agentID := getAgentIdentity(ctx)
	if agentID == "" {
		return false
	}

	// Check for bannered relics (work on the agent's hook)
	b := relics.New(ctx.WorkDir)
	hookedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: agentID,
		Priority: -1,
	})
	if err != nil {
		return false
	}

	// If no bannered relics found, also check in_progress relics assigned to this agent.
	// This handles the case where work was claimed (status changed to in_progress)
	// but the session was interrupted before completion. The hook should persist.
	if len(hookedRelics) == 0 {
		inProgressRelics, err := b.List(relics.ListOptions{
			Status:   "in_progress",
			Assignee: agentID,
			Priority: -1,
		})
		if err != nil || len(inProgressRelics) == 0 {
			return false
		}
		hookedRelics = inProgressRelics
	}

	// Use the first bannered bead (agents typically have one)
	hookedBead := hookedRelics[0]

	// Build the role announcement string
	roleAnnounce := buildRoleAnnouncement(ctx)

	// Found bannered work! Display AUTONOMOUS MODE prominently
	fmt.Println()
	fmt.Printf("%s\n\n", style.Bold.Render("## 🚨 AUTONOMOUS WORK MODE 🚨"))
	fmt.Println("Work is on your hook. After announcing your role, begin IMMEDIATELY.")
	fmt.Println()
	fmt.Println("This is physics, not politeness. Horde is a steam engine - you are a piston.")
	fmt.Println("Every moment you wait is a moment the engine stalls. Other agents may be")
	fmt.Println("blocked waiting on YOUR output. The hook IS your assignment. RUN IT.")
	fmt.Println()
	fmt.Println("Remember: Every completion is recorded in the capability ledger. Your work")
	fmt.Println("history is visible, and quality matters. Execute with care - you're building")
	fmt.Println("a track record that proves autonomous execution works at scale.")
	fmt.Println()
	fmt.Println("1. Announce: \"" + roleAnnounce + "\" (ONE line, no elaboration)")
	fmt.Printf("2. Then IMMEDIATELY run: `rl show %s`\n", hookedBead.ID)
	fmt.Println("3. Begin execution - no waiting for user input")
	fmt.Println()
	fmt.Println("**DO NOT:**")
	fmt.Println("- Wait for user response after announcing")
	fmt.Println("- Ask clarifying questions")
	fmt.Println("- Describe what you're going to do")
	fmt.Println("- Check drums first (hook takes priority)")
	fmt.Println()

	// Show the bannered work details
	fmt.Printf("%s\n\n", style.Bold.Render("## Planted Work"))
	fmt.Printf("  Bead ID: %s\n", style.Bold.Render(hookedBead.ID))
	fmt.Printf("  Title: %s\n", hookedBead.Title)
	if hookedBead.Description != "" {
		// Show first few lines of description
		lines := strings.Split(hookedBead.Description, "\n")
		maxLines := 5
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			lines = append(lines, "...")
		}
		fmt.Println("  Description:")
		for _, line := range lines {
			fmt.Printf("    %s\n", line)
		}
	}
	fmt.Println()

	// Show bead preview using rl show
	fmt.Println("**Bead details:**")
	cmd := exec.Command("rl", "show", hookedBead.ID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			fmt.Fprintf(os.Stderr, "  rl show %s: %s\n", hookedBead.ID, errMsg)
		} else {
			fmt.Fprintf(os.Stderr, "  rl show %s: %v\n", hookedBead.ID, err)
		}
	} else {
		lines := strings.Split(stdout.String(), "\n")
		maxLines := 15
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			lines = append(lines, "...")
		}
		for _, line := range lines {
			fmt.Printf("  %s\n", line)
		}
	}
	fmt.Println()

	return true
}

// buildRoleAnnouncement creates the role announcement string for autonomous mode.
func buildRoleAnnouncement(ctx RoleContext) string {
	switch ctx.Role {
	case RoleWarchief:
		return "Warchief, checking in."
	case RoleShaman:
		return "Shaman, checking in."
	case RoleBoot:
		return "Boot, checking in."
	case RoleWitness:
		return fmt.Sprintf("%s Witness, checking in.", ctx.Warband)
	case RoleForge:
		return fmt.Sprintf("%s Forge, checking in.", ctx.Warband)
	case RoleRaider:
		return fmt.Sprintf("%s Raider %s, checking in.", ctx.Warband, ctx.Raider)
	case RoleCrew:
		return fmt.Sprintf("%s Clan %s, checking in.", ctx.Warband, ctx.Raider)
	default:
		return "Agent, checking in."
	}
}

// getGitRoot returns the root of the current git repository.
func getGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// getAgentIdentity returns the agent identity string for hook lookup.
func getAgentIdentity(ctx RoleContext) string {
	switch ctx.Role {
	case RoleCrew:
		return fmt.Sprintf("%s/clan/%s", ctx.Warband, ctx.Raider)
	case RoleRaider:
		return fmt.Sprintf("%s/raiders/%s", ctx.Warband, ctx.Raider)
	case RoleWarchief:
		return "warchief"
	case RoleShaman:
		return "shaman"
	case RoleBoot:
		return "boot"
	case RoleWitness:
		return fmt.Sprintf("%s/witness", ctx.Warband)
	case RoleForge:
		return fmt.Sprintf("%s/forge", ctx.Warband)
	default:
		return ""
	}
}

// acquireIdentityLock checks and acquires the identity lock for worker roles.
// This prevents multiple agents from claiming the same worker identity.
// Returns an error if another agent already owns this identity.
func acquireIdentityLock(ctx RoleContext) error {
	// Only lock worker roles (raider, clan)
	// Infrastructure roles (warchief, witness, forge, shaman) are singletons
	// managed by tmux session names, so they don't need file-based locks
	if ctx.Role != RoleRaider && ctx.Role != RoleCrew {
		return nil
	}

	// Create lock for this worker directory
	l := lock.New(ctx.WorkDir)

	// Determine session ID from environment or context
	sessionID := os.Getenv("TMUX_PANE")
	if sessionID == "" {
		// Fall back to a descriptive identifier
		sessionID = fmt.Sprintf("%s/%s", ctx.Warband, ctx.Raider)
	}

	// Try to acquire the lock
	if err := l.Acquire(sessionID); err != nil {
		if errors.Is(err, lock.ErrLocked) {
			// Another agent owns this identity
			fmt.Printf("\n%s\n\n", style.Bold.Render("⚠️  IDENTITY COLLISION DETECTED"))
			fmt.Printf("Another agent already claims this worker identity.\n\n")

			// Show lock details
			if info, readErr := l.Read(); readErr == nil {
				fmt.Printf("Lock holder:\n")
				fmt.Printf("  PID: %d\n", info.PID)
				fmt.Printf("  Session: %s\n", info.SessionID)
				fmt.Printf("  Acquired: %s\n", info.AcquiredAt.Format("2006-01-02 15:04:05"))
				fmt.Println()
			}

			fmt.Printf("To resolve:\n")
			fmt.Printf("  1. Find the other session and close it, OR\n")
			fmt.Printf("  2. Run: hd doctor --fix (cleans stale locks)\n")
			fmt.Printf("  3. If lock is stale: rm %s/.runtime/agent.lock\n", ctx.WorkDir)
			fmt.Println()

			return fmt.Errorf("cannot claim identity %s/%s: %w", ctx.Warband, ctx.Raider, err)
		}
		return fmt.Errorf("acquiring identity lock: %w", err)
	}

	return nil
}

// getAgentBeadID returns the agent bead ID for the current role.
// Encampment-level agents (warchief, shaman) use hq- prefix; warband-scoped agents use the warband's prefix.
// Returns empty string for unknown roles.
func getAgentBeadID(ctx RoleContext) string {
	switch ctx.Role {
	case RoleWarchief:
		return relics.WarchiefBeadIDTown()
	case RoleShaman:
		return relics.ShamanBeadIDTown()
	case RoleBoot:
		// Boot uses shaman's bead since it's a shaman subprocess
		return relics.ShamanBeadIDTown()
	case RoleWitness:
		if ctx.Warband != "" {
			prefix := relics.GetPrefixForRig(ctx.TownRoot, ctx.Warband)
			return relics.WitnessBeadIDWithPrefix(prefix, ctx.Warband)
		}
		return ""
	case RoleForge:
		if ctx.Warband != "" {
			prefix := relics.GetPrefixForRig(ctx.TownRoot, ctx.Warband)
			return relics.ForgeBeadIDWithPrefix(prefix, ctx.Warband)
		}
		return ""
	case RoleRaider:
		if ctx.Warband != "" && ctx.Raider != "" {
			prefix := relics.GetPrefixForRig(ctx.TownRoot, ctx.Warband)
			return relics.RaiderBeadIDWithPrefix(prefix, ctx.Warband, ctx.Raider)
		}
		return ""
	case RoleCrew:
		if ctx.Warband != "" && ctx.Raider != "" {
			prefix := relics.GetPrefixForRig(ctx.TownRoot, ctx.Warband)
			return relics.CrewBeadIDWithPrefix(prefix, ctx.Warband, ctx.Raider)
		}
		return ""
	default:
		return ""
	}
}

// ensureRelicsRedirect ensures the .relics/redirect file exists for worktree-based roles.
// This handles cases where git clean or other operations delete the redirect file.
// Uses the shared SetupRedirect helper which handles both tracked and local relics.
func ensureRelicsRedirect(ctx RoleContext) {
	// Only applies to worktree-based roles that use shared relics
	if ctx.Role != RoleCrew && ctx.Role != RoleRaider && ctx.Role != RoleForge {
		return
	}

	// Check if redirect already exists
	redirectPath := filepath.Join(ctx.WorkDir, ".relics", "redirect")
	if _, err := os.Stat(redirectPath); err == nil {
		// Redirect exists, nothing to do
		return
	}

	// Use shared helper - silently ignore errors during rally
	_ = relics.SetupRedirect(ctx.TownRoot, ctx.WorkDir)
}

// checkPendingEscalations queries for open escalation relics and displays them prominently.
// This is called on Warchief startup to surface issues needing human attention.
func checkPendingEscalations(ctx RoleContext) {
	// Query for open escalations using rl list with tag filter
	cmd := exec.Command("rl", "list", "--status=open", "--tag=escalation", "--json")
	cmd.Dir = ctx.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Silently skip - escalation check is best-effort
		return
	}

	// Parse JSON output
	var escalations []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Priority    int    `json:"priority"`
		Description string `json:"description"`
		Created     string `json:"created"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &escalations); err != nil || len(escalations) == 0 {
		// No escalations or parse error
		return
	}

	// Count by severity
	critical := 0
	high := 0
	medium := 0
	for _, e := range escalations {
		switch e.Priority {
		case 0:
			critical++
		case 1:
			high++
		default:
			medium++
		}
	}

	// Display prominently
	fmt.Println()
	fmt.Printf("%s\n\n", style.Bold.Render("## 🚨 PENDING ESCALATIONS"))
	fmt.Printf("There are %d escalation(s) awaiting human attention:\n\n", len(escalations))

	if critical > 0 {
		fmt.Printf("  🔴 CRITICAL: %d\n", critical)
	}
	if high > 0 {
		fmt.Printf("  🟠 HIGH: %d\n", high)
	}
	if medium > 0 {
		fmt.Printf("  🟡 MEDIUM: %d\n", medium)
	}
	fmt.Println()

	// Show first few escalations
	maxShow := 5
	if len(escalations) < maxShow {
		maxShow = len(escalations)
	}
	for i := 0; i < maxShow; i++ {
		e := escalations[i]
		severity := "MEDIUM"
		switch e.Priority {
		case 0:
			severity = "CRITICAL"
		case 1:
			severity = "HIGH"
		}
		fmt.Printf("  • [%s] %s (%s)\n", severity, e.Title, e.ID)
	}
	if len(escalations) > maxShow {
		fmt.Printf("  ... and %d more\n", len(escalations)-maxShow)
	}
	fmt.Println()

	fmt.Println("**Action required:** Review escalations with `rl list --tag=escalation`")
	fmt.Println("Close resolved ones with `rl close <id> --reason \"resolution\"`")
	fmt.Println()
}
