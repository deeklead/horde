package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

var handoffCmd = &cobra.Command{
	Use:     "handoff [bead-or-role]",
	GroupID: GroupWork,
	Short:   "Hand off to a fresh session, work continues from hook",
	Long: `End watch. Hand off to a fresh agent session.

This is the canonical way to end any agent session. It handles all roles:

  - Warchief, Clan, Witness, Forge, Shaman: Respawns with fresh Claude instance
  - Raiders: Calls 'hd done --status DEFERRED' (Witness handles lifecycle)

When run without arguments, hands off the current session.
When given a bead ID (gt-xxx, hq-xxx), hooks that work first, then restarts.
When given a role name, hands off that role's session (and switches to it).

Examples:
  hd handoff                          # Hand off current session
  hd handoff gt-abc                   # Hook bead, then restart
  hd handoff gt-abc -s "Fix it"       # Hook with context, then restart
  hd handoff -s "Context" -m "Notes"  # Hand off with custom message
  hd handoff -c                       # Collect state into handoff message
  hd handoff clan                     # Hand off clan session
  hd handoff warchief                    # Hand off warchief session

The --collect (-c) flag gathers current state (bannered work, inbox, ready relics,
in-progress items) and includes it in the handoff drums. This provides context
for the next session without manual summarization.

Any totem on the hook will be auto-continued by the new session.
The SessionStart hook runs 'hd rally' to restore context.`,
	RunE: runHandoff,
}

var (
	handoffWatch   bool
	handoffDryRun  bool
	handoffSubject string
	handoffMessage string
	handoffCollect bool
)

func init() {
	handoffCmd.Flags().BoolVarP(&handoffWatch, "watch", "w", true, "Switch to new session (for remote handoff)")
	handoffCmd.Flags().BoolVarP(&handoffDryRun, "dry-run", "n", false, "Show what would be done without executing")
	handoffCmd.Flags().StringVarP(&handoffSubject, "subject", "s", "", "Subject for handoff drums (optional)")
	handoffCmd.Flags().StringVarP(&handoffMessage, "message", "m", "", "Message body for handoff drums (optional)")
	handoffCmd.Flags().BoolVarP(&handoffCollect, "collect", "c", false, "Auto-collect state (status, inbox, relics) into handoff message")
	rootCmd.AddCommand(handoffCmd)
}

func runHandoff(cmd *cobra.Command, args []string) error {
	// Check if we're a raider - raiders use hd done instead
	// HD_RAIDER is set by the session manager when starting raider sessions
	if raiderName := os.Getenv("HD_RAIDER"); raiderName != "" {
		fmt.Printf("%s Raider detected (%s) - using hd done for handoff\n",
			style.Bold.Render("🐾"), raiderName)
		// Raiders don't respawn themselves - Witness handles lifecycle
		// Call hd done with DEFERRED exit type to preserve work state
		doneCmd := exec.Command("hd", "done", "--exit", "DEFERRED")
		doneCmd.Stdout = os.Stdout
		doneCmd.Stderr = os.Stderr
		return doneCmd.Run()
	}

	// If --collect flag is set, auto-collect state into the message
	if handoffCollect {
		collected := collectHandoffState()
		if handoffMessage == "" {
			handoffMessage = collected
		} else {
			handoffMessage = handoffMessage + "\n\n---\n" + collected
		}
		if handoffSubject == "" {
			handoffSubject = "Session handoff with context"
		}
	}

	t := tmux.NewTmux()

	// Verify we're in tmux
	if !tmux.IsInsideTmux() {
		return fmt.Errorf("not running in tmux - cannot hand off")
	}

	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return fmt.Errorf("TMUX_PANE not set - cannot hand off")
	}

	// Get current session name
	currentSession, err := getCurrentTmuxSession()
	if err != nil {
		return fmt.Errorf("getting session name: %w", err)
	}

	// Determine target session and check for bead hook
	targetSession := currentSession
	if len(args) > 0 {
		arg := args[0]

		// Check if arg is a bead ID (gt-xxx, hq-xxx, bd-xxx, etc.)
		if looksLikeBeadID(arg) {
			// Hook the bead first
			if err := bannerBeadForHandoff(arg); err != nil {
				return fmt.Errorf("hooking bead: %w", err)
			}
			// Update subject if not set
			if handoffSubject == "" {
				handoffSubject = fmt.Sprintf("🪝 HOOKED: %s", arg)
			}
		} else {
			// User specified a role to hand off
			targetSession, err = resolveRoleToSession(arg)
			if err != nil {
				return fmt.Errorf("resolving role: %w", err)
			}
		}
	}

	// Build the restart command
	restartCmd, err := buildRestartCommand(targetSession)
	if err != nil {
		return err
	}

	// If handing off a different session, we need to find its pane and respawn there
	if targetSession != currentSession {
		return handoffRemoteSession(t, targetSession, restartCmd)
	}

	// Handing off ourselves - print feedback then respawn
	fmt.Printf("%s Handing off %s...\n", style.Bold.Render("🤝"), currentSession)

	// Log handoff event (both encampmentlog and events feed)
	if townRoot, err := workspace.FindFromCwd(); err == nil && townRoot != "" {
		agent := sessionToGTRole(currentSession)
		if agent == "" {
			agent = currentSession
		}
		_ = LogHandoff(townRoot, agent, handoffSubject)
		// Also log to activity feed
		_ = events.LogFeed(events.TypeHandoff, agent, events.HandoffPayload(handoffSubject, true))
	}

	// Dry run mode - show what would happen (BEFORE any side effects)
	if handoffDryRun {
		if handoffSubject != "" || handoffMessage != "" {
			fmt.Printf("Would send handoff drums: subject=%q (auto-bannered)\n", handoffSubject)
		}
		fmt.Printf("Would execute: tmux clear-history -t %s\n", pane)
		fmt.Printf("Would execute: tmux respawn-pane -k -t %s %s\n", pane, restartCmd)
		return nil
	}

	// If subject/message provided, send handoff drums to self first
	// The drums is auto-bannered so the next session picks it up
	if handoffSubject != "" || handoffMessage != "" {
		beadID, err := sendHandoffMail(handoffSubject, handoffMessage)
		if err != nil {
			style.PrintWarning("could not send handoff drums: %v", err)
			// Continue anyway - the respawn is more important
		} else {
			fmt.Printf("%s Sent handoff drums %s (auto-bannered)\n", style.Bold.Render("📬"), beadID)
		}
	}

	// NOTE: reportAgentState("stopped") removed (gt-zecmc)
	// Agent liveness is observable from tmux - no need to record it in bead.
	// "Discover, don't track" principle: reality is truth, state is derived.

	// Clear scrollback history before respawn (resets copy-mode from [0/N] to [0/0])
	if err := t.ClearHistory(pane); err != nil {
		// Non-fatal - continue with respawn even if clear fails
		style.PrintWarning("could not clear history: %v", err)
	}

	// Write handoff marker for successor detection (prevents handoff loop bug).
	// The marker is cleared by hd rally after it outputs the warning.
	// This tells the new session "you're post-handoff, don't re-run /handoff"
	if cwd, err := os.Getwd(); err == nil {
		runtimeDir := filepath.Join(cwd, constants.DirRuntime)
		_ = os.MkdirAll(runtimeDir, 0755)
		markerPath := filepath.Join(runtimeDir, constants.FileHandoffMarker)
		_ = os.WriteFile(markerPath, []byte(currentSession), 0644)
	}

	// Use exec to respawn the pane - this kills us and restarts
	return t.RespawnPane(pane, restartCmd)
}

// getCurrentTmuxSession returns the current tmux session name.
func getCurrentTmuxSession() (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveRoleToSession converts a role name or path to a tmux session name.
// Accepts:
//   - Role shortcuts: "clan", "witness", "forge", "warchief", "shaman"
//   - Full paths: "<warband>/clan/<name>", "<warband>/witness", "<warband>/forge"
//   - Direct session names (passed through)
//
// For role shortcuts that need context (clan, witness, forge), it auto-detects from environment.
func resolveRoleToSession(role string) (string, error) {
	// First, check if it's a path format (contains /)
	if strings.Contains(role, "/") {
		return resolvePathToSession(role)
	}

	switch strings.ToLower(role) {
	case "warchief", "may":
		return getWarchiefSessionName(), nil

	case "shaman", "dea":
		return getShamanSessionName(), nil

	case "clan":
		// Try to get warband and clan name from environment or cwd
		warband := os.Getenv("HD_WARBAND")
		crewName := os.Getenv("HD_CLAN")
		if warband == "" || crewName == "" {
			// Try to detect from cwd
			detected, err := detectCrewFromCwd()
			if err == nil {
				warband = detected.rigName
				crewName = detected.crewName
			}
		}
		if warband == "" || crewName == "" {
			return "", fmt.Errorf("cannot determine clan identity - run from clan directory or specify HD_WARBAND/HD_CLAN")
		}
		return fmt.Sprintf("gt-%s-clan-%s", warband, crewName), nil

	case "witness", "wit":
		warband := os.Getenv("HD_WARBAND")
		if warband == "" {
			return "", fmt.Errorf("cannot determine warband - set HD_WARBAND or run from warband context")
		}
		return fmt.Sprintf("gt-%s-witness", warband), nil

	case "forge", "ref":
		warband := os.Getenv("HD_WARBAND")
		if warband == "" {
			return "", fmt.Errorf("cannot determine warband - set HD_WARBAND or run from warband context")
		}
		return fmt.Sprintf("gt-%s-forge", warband), nil

	default:
		// Assume it's a direct session name (e.g., gt-horde-clan-max)
		return role, nil
	}
}

// resolvePathToSession converts a path like "<warband>/clan/<name>" to a session name.
// Supported formats:
//   - <warband>/clan/<name> -> gt-<warband>-clan-<name>
//   - <warband>/witness -> gt-<warband>-witness
//   - <warband>/forge -> gt-<warband>-forge
//   - <warband>/raiders/<name> -> gt-<warband>-<name> (explicit raider)
//   - <warband>/<name> -> gt-<warband>-<name> (raider shorthand, if name isn't a known role)
func resolvePathToSession(path string) (string, error) {
	parts := strings.Split(path, "/")

	// Handle <warband>/clan/<name> format
	if len(parts) == 3 && parts[1] == "clan" {
		warband := parts[0]
		name := parts[2]
		return fmt.Sprintf("gt-%s-clan-%s", warband, name), nil
	}

	// Handle <warband>/raiders/<name> format (explicit raider path)
	if len(parts) == 3 && parts[1] == "raiders" {
		warband := parts[0]
		name := strings.ToLower(parts[2]) // normalize raider name
		return fmt.Sprintf("gt-%s-%s", warband, name), nil
	}

	// Handle <warband>/<role-or-raider> format
	if len(parts) == 2 {
		warband := parts[0]
		second := parts[1]
		secondLower := strings.ToLower(second)

		// Check for known roles first
		switch secondLower {
		case "witness":
			return fmt.Sprintf("gt-%s-witness", warband), nil
		case "forge":
			return fmt.Sprintf("gt-%s-forge", warband), nil
		case "clan":
			// Just "<warband>/clan" without a name - need more info
			return "", fmt.Errorf("clan path requires name: %s/clan/<name>", warband)
		case "raiders":
			// Just "<warband>/raiders" without a name - need more info
			return "", fmt.Errorf("raiders path requires name: %s/raiders/<name>", warband)
		default:
			// Not a known role - check if it's a clan member before assuming raider.
			// Clan members exist at <townRoot>/<warband>/clan/<name>.
			// This fixes: hd charge gt-375 horde/max failing because max is clan, not raider.
			townRoot := detectTownRootFromCwd()
			if townRoot != "" {
				crewPath := filepath.Join(townRoot, warband, "clan", second)
				if info, err := os.Stat(crewPath); err == nil && info.IsDir() {
					return fmt.Sprintf("gt-%s-clan-%s", warband, second), nil
				}
			}
			// Not a clan member - treat as raider name (e.g., horde/nux)
			return fmt.Sprintf("gt-%s-%s", warband, secondLower), nil
		}
	}

	return "", fmt.Errorf("cannot parse path '%s' - expected <warband>/<raider>, <warband>/clan/<name>, <warband>/witness, or <warband>/forge", path)
}

// claudeEnvVars lists the Claude-related environment variables to propagate
// during handoff. These vars aren't inherited by tmux respawn-pane's fresh shell.
var claudeEnvVars = []string{
	// Claude API and config
	"ANTHROPIC_API_KEY",
	"CLAUDE_CODE_USE_BEDROCK",
	// AWS vars for Bedrock
	"AWS_PROFILE",
	"AWS_REGION",
}

// buildRestartCommand creates the command to run when respawning a session's pane.
// This needs to be the actual command to execute (e.g., claude), not a session summon command.
// The command includes a cd to the correct working directory for the role.
func buildRestartCommand(sessionName string) (string, error) {
	// Detect encampment root from current directory
	townRoot := detectTownRootFromCwd()
	if townRoot == "" {
		return "", fmt.Errorf("cannot detect encampment root - run from within a Horde workspace")
	}

	// Determine the working directory for this session type
	workDir, err := sessionWorkDir(sessionName, townRoot)
	if err != nil {
		return "", err
	}

	// Parse the session name to get the identity (used for HD_ROLE and beacon)
	identity, err := session.ParseSessionName(sessionName)
	if err != nil {
		return "", fmt.Errorf("cannot parse session name %q: %w", sessionName, err)
	}
	gtRole := identity.GTRole()

	// Build startup beacon for predecessor discovery via /resume
	// Use FormatStartupNudge instead of bare "hd rally" which confuses agents
	// The SessionStart hook handles context injection (gt rally --hook)
	beacon := session.FormatStartupNudge(session.StartupNudgeConfig{
		Recipient: identity.Address(),
		Sender:    "self",
		Topic:     "handoff",
	})

	// For respawn-pane, we:
	// 1. cd to the right directory (role's canonical home)
	// 2. export HD_ROLE and BD_ACTOR so role detection works correctly
	// 3. export Claude-related env vars (not inherited by fresh shell)
	// 4. run claude with the startup beacon (triggers immediate context loading)
	// Use exec to ensure clean process replacement.
	runtimeCmd := config.GetRuntimeCommandWithPrompt("", beacon)

	// Build environment exports - role vars first, then Claude vars
	var exports []string
	if gtRole != "" {
		runtimeConfig := config.LoadRuntimeConfig("")
		exports = append(exports, "HD_ROLE="+gtRole)
		exports = append(exports, "BD_ACTOR="+gtRole)
		exports = append(exports, "GIT_AUTHOR_NAME="+gtRole)
		if runtimeConfig.Session != nil && runtimeConfig.Session.SessionIDEnv != "" {
			exports = append(exports, "HD_SESSION_ID_ENV="+runtimeConfig.Session.SessionIDEnv)
		}
	}

	// Add Claude-related env vars from current environment
	for _, name := range claudeEnvVars {
		if val := os.Getenv(name); val != "" {
			// Shell-escape the value in case it contains special chars
			exports = append(exports, fmt.Sprintf("%s=%q", name, val))
		}
	}

	if len(exports) > 0 {
		return fmt.Sprintf("cd %s && export %s && exec %s", workDir, strings.Join(exports, " "), runtimeCmd), nil
	}
	return fmt.Sprintf("cd %s && exec %s", workDir, runtimeCmd), nil
}

// sessionWorkDir returns the correct working directory for a session.
// This is the canonical home for each role type.
func sessionWorkDir(sessionName, townRoot string) (string, error) {
	// Get session names for comparison
	warchiefSession := getWarchiefSessionName()
	shamanSession := getShamanSessionName()

	switch {
	case sessionName == warchiefSession:
		return townRoot, nil

	case sessionName == shamanSession:
		return townRoot + "/shaman", nil

	case strings.Contains(sessionName, "-clan-"):
		// gt-<warband>-clan-<name> -> <townRoot>/<warband>/clan/<name>
		parts := strings.Split(sessionName, "-")
		if len(parts) < 4 {
			return "", fmt.Errorf("invalid clan session name: %s", sessionName)
		}
		// Find the index of "clan" to split warband name (may contain dashes)
		for i, p := range parts {
			if p == "clan" && i > 1 && i < len(parts)-1 {
				warband := strings.Join(parts[1:i], "-")
				name := strings.Join(parts[i+1:], "-")
				return fmt.Sprintf("%s/%s/clan/%s", townRoot, warband, name), nil
			}
		}
		return "", fmt.Errorf("cannot parse clan session name: %s", sessionName)

	case strings.HasSuffix(sessionName, "-witness"):
		// gt-<warband>-witness -> <townRoot>/<warband>/witness
		// Note: witness doesn't have a /warband worktree like forge does
		warband := strings.TrimPrefix(sessionName, "hd-")
		warband = strings.TrimSuffix(warband, "-witness")
		return fmt.Sprintf("%s/%s/witness", townRoot, warband), nil

	case strings.HasSuffix(sessionName, "-forge"):
		// gt-<warband>-forge -> <townRoot>/<warband>/forge/warband
		warband := strings.TrimPrefix(sessionName, "hd-")
		warband = strings.TrimSuffix(warband, "-forge")
		return fmt.Sprintf("%s/%s/forge/warband", townRoot, warband), nil

	default:
		// Assume raider: gt-<warband>-<name> -> <townRoot>/<warband>/raiders/<name>
		// Use session.ParseSessionName to determine warband and name
		identity, err := session.ParseSessionName(sessionName)
		if err != nil {
			return "", fmt.Errorf("unknown session type: %s (%w)", sessionName, err)
		}
		if identity.Role != session.RoleRaider {
			return "", fmt.Errorf("unknown session type: %s (role %s, try specifying role explicitly)", sessionName, identity.Role)
		}
		return fmt.Sprintf("%s/%s/raiders/%s", townRoot, identity.Warband, identity.Name), nil
	}
}

// sessionToGTRole converts a session name to a HD_ROLE value.
// Uses session.ParseSessionName for consistent parsing across the codebase.
func sessionToGTRole(sessionName string) string {
	identity, err := session.ParseSessionName(sessionName)
	if err != nil {
		return ""
	}
	return identity.GTRole()
}

// detectTownRootFromCwd walks up from the current directory to find the encampment root.
func detectTownRootFromCwd() string {
	// Use workspace.FindFromCwd which handles both primary (warchief/encampment.json)
	// and secondary (warchief/ directory) markers
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return ""
	}
	return townRoot
}

// handoffRemoteSession respawns a different session and optionally switches to it.
func handoffRemoteSession(t *tmux.Tmux, targetSession, restartCmd string) error {
	// Check if target session exists
	exists, err := t.HasSession(targetSession)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !exists {
		return fmt.Errorf("session '%s' not found - is the agent running?", targetSession)
	}

	// Get the pane ID for the target session
	targetPane, err := getSessionPane(targetSession)
	if err != nil {
		return fmt.Errorf("getting target pane: %w", err)
	}

	fmt.Printf("%s Handing off %s...\n", style.Bold.Render("🤝"), targetSession)

	// Dry run mode
	if handoffDryRun {
		fmt.Printf("Would execute: tmux clear-history -t %s\n", targetPane)
		fmt.Printf("Would execute: tmux respawn-pane -k -t %s %s\n", targetPane, restartCmd)
		if handoffWatch {
			fmt.Printf("Would execute: tmux switch-client -t %s\n", targetSession)
		}
		return nil
	}

	// Clear scrollback history before respawn (resets copy-mode from [0/N] to [0/0])
	if err := t.ClearHistory(targetPane); err != nil {
		// Non-fatal - continue with respawn even if clear fails
		style.PrintWarning("could not clear history: %v", err)
	}

	// Respawn the remote session's pane
	if err := t.RespawnPane(targetPane, restartCmd); err != nil {
		return fmt.Errorf("respawning pane: %w", err)
	}

	// If --watch, switch to that session
	if handoffWatch {
		fmt.Printf("Switching to %s...\n", targetSession)
		// Use tmux switch-client to move our view to the target session
		if err := exec.Command("tmux", "switch-client", "-t", targetSession).Run(); err != nil {
			// Non-fatal - they can manually switch
			fmt.Printf("Note: Could not auto-switch (use: tmux switch-client -t %s)\n", targetSession)
		}
	}

	return nil
}

// getSessionPane returns the pane identifier for a session's main pane.
func getSessionPane(sessionName string) (string, error) {
	// Get the pane ID for the first pane in the session
	out, err := exec.Command("tmux", "list-panes", "-t", sessionName, "-F", "#{pane_id}").Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("no panes found in session")
	}
	return lines[0], nil
}

// sendHandoffMail sends a handoff drums to self and auto-hooks it.
// Returns the created bead ID and any error.
func sendHandoffMail(subject, message string) (string, error) {
	// Build subject with handoff prefix if not already present
	if subject == "" {
		subject = "🤝 HANDOFF: Session cycling"
	} else if !strings.Contains(subject, "HANDOFF") {
		subject = "🤝 HANDOFF: " + subject
	}

	// Default message if not provided
	if message == "" {
		message = "Context cycling. Check rl ready for pending work."
	}

	// Detect agent identity for self-drums
	agentID, _, _, err := resolveSelfTarget()
	if err != nil {
		return "", fmt.Errorf("detecting agent identity: %w", err)
	}

	// Detect encampment root for relics location
	townRoot := detectTownRootFromCwd()
	if townRoot == "" {
		return "", fmt.Errorf("cannot detect encampment root")
	}

	// Build labels for drums metadata (matches drums router format)
	labels := fmt.Sprintf("from:%s", agentID)

	// Create drums bead directly using rl create with --silent to get the ID
	// Drums goes to encampment-level relics (hq- prefix)
	args := []string{
		"create", subject,
		"--type", "message",
		"--assignee", agentID,
		"-d", message,
		"--priority", "2",
		"--labels", labels,
		"--actor", agentID,
		"--ephemeral", // Handoff drums is ephemeral
		"--silent",    // Output only the bead ID
	}

	cmd := exec.Command("rl", args...)
	cmd.Dir = townRoot // Run from encampment root for encampment-level relics
	cmd.Env = append(os.Environ(), "RELICS_DIR="+filepath.Join(townRoot, ".relics"))

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("creating handoff drums: %s", errMsg)
		}
		return "", fmt.Errorf("creating handoff drums: %w", err)
	}

	beadID := strings.TrimSpace(stdout.String())
	if beadID == "" {
		return "", fmt.Errorf("bd create did not return bead ID")
	}

	// Auto-hook the created drums bead
	hookCmd := exec.Command("rl", "update", beadID, "--status=bannered", "--assignee="+agentID)
	hookCmd.Dir = townRoot
	hookCmd.Env = append(os.Environ(), "RELICS_DIR="+filepath.Join(townRoot, ".relics"))
	hookCmd.Stderr = os.Stderr

	if err := hookCmd.Run(); err != nil {
		// Non-fatal: drums was created, just couldn't hook
		style.PrintWarning("created drums %s but failed to auto-hook: %v", beadID, err)
		return beadID, nil
	}

	return beadID, nil
}

// looksLikeBeadID checks if a string looks like a bead ID.
// Bead IDs have format: prefix-xxxx where prefix is 1-5 lowercase letters and xxxx is alphanumeric.
// Examples: "gt-abc123", "bd-ka761", "hq-cv-abc", "relics-xyz", "ap-qtsup.16"
func looksLikeBeadID(s string) bool {
	// Find the first hyphen
	idx := strings.Index(s, "-")
	if idx < 1 || idx > 5 {
		// No hyphen, or prefix is empty/too long
		return false
	}

	// Check prefix is all lowercase letters
	prefix := s[:idx]
	for _, c := range prefix {
		if c < 'a' || c > 'z' {
			return false
		}
	}

	// Check there's something after the hyphen
	rest := s[idx+1:]
	if len(rest) == 0 {
		return false
	}

	// Check rest starts with alphanumeric and contains only alphanumeric, dots, hyphens
	first := rest[0]
	if !((first >= 'a' && first <= 'z') || (first >= '0' && first <= '9')) {
		return false
	}

	return true
}

// bannerBeadForHandoff attaches a bead to the current agent's hook.
func bannerBeadForHandoff(beadID string) error {
	// Verify the bead exists first
	verifyCmd := exec.Command("rl", "show", beadID, "--json")
	if err := verifyCmd.Run(); err != nil {
		return fmt.Errorf("bead '%s' not found", beadID)
	}

	// Determine agent identity
	agentID, _, _, err := resolveSelfTarget()
	if err != nil {
		return fmt.Errorf("detecting agent identity: %w", err)
	}

	fmt.Printf("%s Hooking %s...\n", style.Bold.Render("🪝"), beadID)

	if handoffDryRun {
		fmt.Printf("Would run: rl update %s --status=pinned --assignee=%s\n", beadID, agentID)
		return nil
	}

	// Pin the bead using rl update (discovery-based approach)
	pinCmd := exec.Command("rl", "update", beadID, "--status=pinned", "--assignee="+agentID)
	pinCmd.Stderr = os.Stderr
	if err := pinCmd.Run(); err != nil {
		return fmt.Errorf("pinning bead: %w", err)
	}

	fmt.Printf("%s Work attached to hook (pinned bead)\n", style.Bold.Render("✓"))
	return nil
}

// collectHandoffState gathers current state for handoff context.
// Collects: inbox summary, ready relics, bannered work.
func collectHandoffState() string {
	var parts []string

	// Get bannered work
	hookOutput, err := exec.Command("hd", "banner").Output()
	if err == nil {
		hookStr := strings.TrimSpace(string(hookOutput))
		if hookStr != "" && !strings.Contains(hookStr, "Nothing on hook") {
			parts = append(parts, "## Planted Work\n"+hookStr)
		}
	}

	// Get inbox summary (first few messages)
	inboxOutput, err := exec.Command("hd", "drums", "inbox").Output()
	if err == nil {
		inboxStr := strings.TrimSpace(string(inboxOutput))
		if inboxStr != "" && !strings.Contains(inboxStr, "Inbox empty") {
			// Limit to first 10 lines for brevity
			lines := strings.Split(inboxStr, "\n")
			if len(lines) > 10 {
				lines = append(lines[:10], "... (more messages)")
			}
			parts = append(parts, "## Inbox\n"+strings.Join(lines, "\n"))
		}
	}

	// Get ready relics
	readyOutput, err := exec.Command("rl", "ready").Output()
	if err == nil {
		readyStr := strings.TrimSpace(string(readyOutput))
		if readyStr != "" && !strings.Contains(readyStr, "No issues ready") {
			// Limit to first 10 lines
			lines := strings.Split(readyStr, "\n")
			if len(lines) > 10 {
				lines = append(lines[:10], "... (more issues)")
			}
			parts = append(parts, "## Ready Work\n"+strings.Join(lines, "\n"))
		}
	}

	// Get in-progress relics
	inProgressOutput, err := exec.Command("rl", "list", "--status=in_progress").Output()
	if err == nil {
		ipStr := strings.TrimSpace(string(inProgressOutput))
		if ipStr != "" && !strings.Contains(ipStr, "No issues") {
			lines := strings.Split(ipStr, "\n")
			if len(lines) > 5 {
				lines = append(lines[:5], "... (more)")
			}
			parts = append(parts, "## In Progress\n"+strings.Join(lines, "\n"))
		}
	}

	if len(parts) == 0 {
		return "No active state to report."
	}

	return strings.Join(parts, "\n\n")
}
