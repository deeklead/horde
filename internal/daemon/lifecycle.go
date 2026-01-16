package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/tmux"
)

// RelicsMessage represents a message from hd drums inbox --json.
type RelicsMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Timestamp string `json:"timestamp"`
	Read      bool   `json:"read"`
	Priority  string `json:"priority"`
	Type      string `json:"type"`
}

// MaxLifecycleMessageAge is the maximum age of a lifecycle message before it's ignored.
// Messages older than this are considered stale and deleted without execution.
const MaxLifecycleMessageAge = 6 * time.Hour

// ProcessLifecycleRequests checks for and processes lifecycle requests from the shaman inbox.
func (d *Daemon) ProcessLifecycleRequests() {
	// Get drums for shaman identity (using hd drums, not rl drums)
	cmd := exec.Command("hd", "drums", "inbox", "--identity", "shaman/", "--json")
	cmd.Dir = d.config.TownRoot

	output, err := cmd.Output()
	if err != nil {
		d.logger.Printf("Warning: failed to fetch shaman inbox: %v", err)
		return
	}

	if len(output) == 0 || string(output) == "[]" || string(output) == "[]\n" {
		return
	}

	var messages []RelicsMessage
	if err := json.Unmarshal(output, &messages); err != nil {
		d.logger.Printf("Error parsing drums: %v", err)
		return
	}

	for _, msg := range messages {
		if msg.Read {
			continue // Already processed
		}

		request := d.parseLifecycleRequest(&msg)
		if request == nil {
			continue // Not a lifecycle request
		}

		// Check message age - ignore stale lifecycle requests
		if msgTime, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
			age := time.Since(msgTime)
			if age > MaxLifecycleMessageAge {
				d.logger.Printf("Ignoring stale lifecycle request from %s (age: %v, max: %v) - deleting",
					request.From, age.Round(time.Minute), MaxLifecycleMessageAge)
				if err := d.closeMessage(msg.ID); err != nil {
					d.logger.Printf("Warning: failed to delete stale message %s: %v", msg.ID, err)
				}
				continue
			}
		}

		d.logger.Printf("Processing lifecycle request from %s: %s", request.From, request.Action)

		// CRITICAL: Delete message FIRST, before executing action.
		// This prevents stale messages from being reprocessed on every heartbeat.
		// "Claim then execute" pattern: claim by deleting, then execute.
		// Even if action fails, the message is gone - sender must re-request.
		if err := d.closeMessage(msg.ID); err != nil {
			d.logger.Printf("Warning: failed to delete message %s before execution: %v", msg.ID, err)
			// Continue anyway - better to attempt action than leave stale message
		}

		if err := d.executeLifecycleAction(request); err != nil {
			d.logger.Printf("Error executing lifecycle action: %v", err)
			continue
		}
	}
}

// LifecycleBody is the structured body format for lifecycle requests.
// Claude should send drums with JSON body: {"action": "cycle"} or {"action": "shutdown"}
type LifecycleBody struct {
	Action string `json:"action"`
}

// parseLifecycleRequest extracts a lifecycle request from a message.
// Uses structured body parsing instead of keyword matching on subject.
func (d *Daemon) parseLifecycleRequest(msg *RelicsMessage) *LifecycleRequest {
	// Gate: subject must start with "LIFECYCLE:"
	subject := strings.ToLower(msg.Subject)
	if !strings.HasPrefix(subject, "lifecycle:") {
		return nil
	}

	// Parse structured body for action
	var body LifecycleBody
	if err := json.Unmarshal([]byte(msg.Body), &body); err != nil {
		// Fallback: check for simple action strings in body
		bodyLower := strings.ToLower(strings.TrimSpace(msg.Body))
		switch {
		case bodyLower == "restart" || bodyLower == "action: restart":
			body.Action = "restart"
		case bodyLower == "shutdown" || bodyLower == "action: shutdown" || bodyLower == "stop":
			body.Action = "shutdown"
		case bodyLower == "cycle" || bodyLower == "action: cycle":
			body.Action = "cycle"
		default:
			d.logger.Printf("Lifecycle request with unparseable body: %q", msg.Body)
			return nil
		}
	}

	// Map action string to enum
	var action LifecycleAction
	switch strings.ToLower(body.Action) {
	case "restart":
		action = ActionRestart
	case "shutdown", "stop":
		action = ActionShutdown
	case "cycle":
		action = ActionCycle
	default:
		d.logger.Printf("Unknown lifecycle action: %q", body.Action)
		return nil
	}

	return &LifecycleRequest{
		From:      msg.From,
		Action:    action,
		Timestamp: time.Now(),
	}
}

// executeLifecycleAction performs the requested lifecycle action.
func (d *Daemon) executeLifecycleAction(request *LifecycleRequest) error {
	// Determine session name from sender identity
	sessionName := d.identityToSession(request.From)
	if sessionName == "" {
		return fmt.Errorf("unknown agent identity: %s", request.From)
	}

	d.logger.Printf("Executing %s for session %s", request.Action, sessionName)

	// Check agent bead state (ZFC: trust what agent reports) - gt-39ttg
	agentBeadID := d.identityToAgentBeadID(request.From)
	if agentBeadID != "" {
		if beadState, err := d.getAgentBeadState(agentBeadID); err == nil {
			d.logger.Printf("Agent bead %s reports state: %s", agentBeadID, beadState)
		}
	}

	// Check if session exists (tmux detection still needed for lifecycle actions)
	running, err := d.tmux.HasSession(sessionName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}

	switch request.Action {
	case ActionShutdown:
		if running {
			if err := d.tmux.KillSession(sessionName); err != nil {
				return fmt.Errorf("killing session: %w", err)
			}
			d.logger.Printf("Killed session %s", sessionName)
		}
		return nil

	case ActionCycle, ActionRestart:
		if running {
			// Kill the session first
			if err := d.tmux.KillSession(sessionName); err != nil {
				return fmt.Errorf("killing session: %w", err)
			}
			d.logger.Printf("Killed session %s for restart", sessionName)

			// Wait a moment
			time.Sleep(constants.ShutdownNotifyDelay)
		}

		// Restart the session
		if err := d.restartSession(sessionName, request.From); err != nil {
			return fmt.Errorf("restarting session: %w", err)
		}
		d.logger.Printf("Restarted session %s", sessionName)
		return nil

	default:
		return fmt.Errorf("unknown action: %s", request.Action)
	}
}

// ParsedIdentity holds the components extracted from an agent identity string.
// This is used to look up the appropriate role bead for lifecycle config.
type ParsedIdentity struct {
	RoleType  string // warchief, shaman, witness, forge, clan, raider
	RigName   string // Empty for encampment-level agents (warchief, shaman)
	AgentName string // Empty for singletons (warchief, shaman, witness, forge)
}

// parseIdentity extracts role type, warband name, and agent name from an identity string.
// This is the ONLY place where identity string patterns are parsed.
// All other functions should use the extracted components to look up role relics.
func parseIdentity(identity string) (*ParsedIdentity, error) {
	switch identity {
	case "warchief":
		return &ParsedIdentity{RoleType: "warchief"}, nil
	case "shaman":
		return &ParsedIdentity{RoleType: "shaman"}, nil
	}

	// Pattern: <warband>-witness → witness role
	if strings.HasSuffix(identity, "-witness") {
		rigName := strings.TrimSuffix(identity, "-witness")
		return &ParsedIdentity{RoleType: "witness", RigName: rigName}, nil
	}

	// Pattern: <warband>-forge → forge role
	if strings.HasSuffix(identity, "-forge") {
		rigName := strings.TrimSuffix(identity, "-forge")
		return &ParsedIdentity{RoleType: "forge", RigName: rigName}, nil
	}

	// Pattern: <warband>-clan-<name> → clan role
	if strings.Contains(identity, "-clan-") {
		parts := strings.SplitN(identity, "-clan-", 2)
		if len(parts) == 2 {
			return &ParsedIdentity{RoleType: "clan", RigName: parts[0], AgentName: parts[1]}, nil
		}
	}

	// Pattern: <warband>-raider-<name> → raider role
	if strings.Contains(identity, "-raider-") {
		parts := strings.SplitN(identity, "-raider-", 2)
		if len(parts) == 2 {
			return &ParsedIdentity{RoleType: "raider", RigName: parts[0], AgentName: parts[1]}, nil
		}
	}

	// Pattern: <warband>/raiders/<name> → raider role (slash format)
	if strings.Contains(identity, "/raiders/") {
		parts := strings.Split(identity, "/raiders/")
		if len(parts) == 2 {
			return &ParsedIdentity{RoleType: "raider", RigName: parts[0], AgentName: parts[1]}, nil
		}
	}

	return nil, fmt.Errorf("unknown identity format: %s", identity)
}

// getRoleConfigForIdentity looks up the role bead for an identity and returns its config.
// Falls back to default config if role bead doesn't exist or has no config.
func (d *Daemon) getRoleConfigForIdentity(identity string) (*relics.RoleConfig, *ParsedIdentity, error) {
	parsed, err := parseIdentity(identity)
	if err != nil {
		return nil, nil, err
	}

	// Look up role bead
	b := relics.New(d.config.TownRoot)

	roleBeadID := relics.RoleBeadIDTown(parsed.RoleType)
	roleConfig, err := b.GetRoleConfig(roleBeadID)
	if err != nil {
		d.logger.Printf("Warning: failed to get role config for %s: %v", roleBeadID, err)
	}

	// Backward compatibility: fall back to legacy role bead IDs.
	if roleConfig == nil {
		legacyRoleBeadID := relics.RoleBeadID(parsed.RoleType) // gt-<role>-role
		if legacyRoleBeadID != roleBeadID {
			legacyCfg, legacyErr := b.GetRoleConfig(legacyRoleBeadID)
			if legacyErr != nil {
				d.logger.Printf("Warning: failed to get legacy role config for %s: %v", legacyRoleBeadID, legacyErr)
			} else if legacyCfg != nil {
				roleConfig = legacyCfg
			}
		}
	}

	// Return parsed identity even if config is nil (caller can use defaults)
	return roleConfig, parsed, nil
}

// identityToSession converts a relics identity to a tmux session name.
// Uses role bead config if available, falls back to hardcoded patterns.
func (d *Daemon) identityToSession(identity string) string {
	config, parsed, err := d.getRoleConfigForIdentity(identity)
	if err != nil {
		return ""
	}

	// If role bead has session_pattern, use it
	if config != nil && config.SessionPattern != "" {
		return relics.ExpandRolePattern(config.SessionPattern, d.config.TownRoot, parsed.RigName, parsed.AgentName, parsed.RoleType)
	}

	// Fallback: use default patterns based on role type
	switch parsed.RoleType {
	case "warchief":
		return session.WarchiefSessionName()
	case "shaman":
		return session.ShamanSessionName()
	case "witness", "forge":
		return fmt.Sprintf("hd-%s-%s", parsed.RigName, parsed.RoleType)
	case "clan":
		return fmt.Sprintf("hd-%s-clan-%s", parsed.RigName, parsed.AgentName)
	case "raider":
		return fmt.Sprintf("hd-%s-%s", parsed.RigName, parsed.AgentName)
	default:
		return ""
	}
}

// restartSession starts a new session for the given agent.
// Uses role bead config if available, falls back to hardcoded defaults.
func (d *Daemon) restartSession(sessionName, identity string) error {
	// Get role config for this identity
	config, parsed, err := d.getRoleConfigForIdentity(identity)
	if err != nil {
		return fmt.Errorf("parsing identity: %w", err)
	}

	// Check warband operational state for warband-level agents (witness, forge, clan, raider)
	// Encampment-level agents (warchief, shaman) are not affected by warband state
	if parsed.RigName != "" {
		if operational, reason := d.isRigOperational(parsed.RigName); !operational {
			d.logger.Printf("Skipping session restart for %s: %s", identity, reason)
			return fmt.Errorf("cannot restart session: %s", reason)
		}
	}

	// Determine working directory
	workDir := d.getWorkDir(config, parsed)
	if workDir == "" {
		return fmt.Errorf("cannot determine working directory for %s", identity)
	}

	// Determine if pre-sync is needed
	needsPreSync := d.getNeedsPreSync(config, parsed)

	// Pre-sync workspace for agents with git worktrees
	if needsPreSync {
		d.logger.Printf("Pre-syncing workspace for %s at %s", identity, workDir)
		d.syncWorkspace(workDir)
	}

	// Create session
	// Use EnsureSessionFresh to handle zombie sessions that exist but have dead Claude
	if err := d.tmux.EnsureSessionFresh(sessionName, workDir); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	// Set environment variables
	d.setSessionEnvironment(sessionName, config, parsed)

	// Apply theme (non-fatal: theming failure doesn't affect operation)
	d.applySessionTheme(sessionName, parsed)

	// Get and send startup command
	startCmd := d.getStartCommand(config, parsed)
	if err := d.tmux.SendKeys(sessionName, startCmd); err != nil {
		return fmt.Errorf("sending startup command: %w", err)
	}

	// Wait for Claude to start, then accept bypass permissions warning if it appears.
	// This ensures automated role starts aren't blocked by the warning dialog.
	if err := d.tmux.WaitForCommand(sessionName, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		// Non-fatal - Claude might still start
	}
	_ = d.tmux.AcceptBypassPermissionsWarning(sessionName)
	time.Sleep(constants.ShutdownNotifyDelay)

	// GUPP: Horde Universal Propulsion Principle
	// Send startup signal for predecessor discovery via /resume
	recipient := identityToBDActor(identity)
	_ = session.StartupNudge(d.tmux, sessionName, session.StartupNudgeConfig{
		Recipient: recipient,
		Sender:    "shaman",
		Topic:     "lifecycle-restart",
	}) // Non-fatal

	// Send propulsion signal to trigger autonomous execution.
	// Wait for beacon to be fully processed (needs to be separate prompt)
	time.Sleep(2 * time.Second)
	_ = d.tmux.SignalSession(sessionName, session.PropulsionNudgeForRole(parsed.RoleType, workDir)) // Non-fatal

	return nil
}

// getWorkDir determines the working directory for an agent.
// Uses role bead config if available, falls back to hardcoded defaults.
func (d *Daemon) getWorkDir(config *relics.RoleConfig, parsed *ParsedIdentity) string {
	// If role bead has work_dir_pattern, use it
	if config != nil && config.WorkDirPattern != "" {
		return relics.ExpandRolePattern(config.WorkDirPattern, d.config.TownRoot, parsed.RigName, parsed.AgentName, parsed.RoleType)
	}

	// Fallback: use default patterns based on role type
	switch parsed.RoleType {
	case "warchief":
		return d.config.TownRoot
	case "shaman":
		return d.config.TownRoot
	case "witness":
		return filepath.Join(d.config.TownRoot, parsed.RigName)
	case "forge":
		return filepath.Join(d.config.TownRoot, parsed.RigName, "forge", "warband")
	case "clan":
		return filepath.Join(d.config.TownRoot, parsed.RigName, "clan", parsed.AgentName)
	case "raider":
		// New structure: raiders/<name>/<rigname>/ (for LLM ergonomics)
		// Old structure: raiders/<name>/ (for backward compat)
		newPath := filepath.Join(d.config.TownRoot, parsed.RigName, "raiders", parsed.AgentName, parsed.RigName)
		if _, err := os.Stat(newPath); err == nil {
			return newPath
		}
		return filepath.Join(d.config.TownRoot, parsed.RigName, "raiders", parsed.AgentName)
	default:
		return ""
	}
}

// getNeedsPreSync determines if a workspace needs git sync before starting.
// Uses role bead config if available, falls back to hardcoded defaults.
func (d *Daemon) getNeedsPreSync(config *relics.RoleConfig, parsed *ParsedIdentity) bool {
	// If role bead has explicit config, use it
	if config != nil {
		return config.NeedsPreSync
	}

	// Fallback: roles with persistent git clones need pre-sync
	switch parsed.RoleType {
	case "forge", "clan", "raider":
		return true
	default:
		return false
	}
}

// getStartCommand determines the startup command for an agent.
// Uses role bead config if available, then role-based agent selection, then hardcoded defaults.
func (d *Daemon) getStartCommand(roleConfig *relics.RoleConfig, parsed *ParsedIdentity) string {
	// If role bead has explicit config, use it
	if roleConfig != nil && roleConfig.StartCommand != "" {
		// Expand any patterns in the command
		return relics.ExpandRolePattern(roleConfig.StartCommand, d.config.TownRoot, parsed.RigName, parsed.AgentName, parsed.RoleType)
	}

	rigPath := ""
	if parsed != nil && parsed.RigName != "" {
		rigPath = filepath.Join(d.config.TownRoot, parsed.RigName)
	}

	// Use role-based agent resolution for per-role model selection
	runtimeConfig := config.ResolveRoleAgentConfig(parsed.RoleType, d.config.TownRoot, rigPath)

	// Build default command using the role-resolved runtime config
	defaultCmd := "exec " + runtimeConfig.BuildCommand()
	if runtimeConfig.Session != nil && runtimeConfig.Session.SessionIDEnv != "" {
		defaultCmd = config.PrependEnv(defaultCmd, map[string]string{"HD_SESSION_ID_ENV": runtimeConfig.Session.SessionIDEnv})
	}

	// Raiders and clan need environment variables set in the command
	if parsed.RoleType == "raider" {
		var sessionIDEnv string
		if runtimeConfig.Session != nil {
			sessionIDEnv = runtimeConfig.Session.SessionIDEnv
		}
		envVars := config.AgentEnv(config.AgentEnvConfig{
			Role:         "raider",
			Warband:          parsed.RigName,
			AgentName:    parsed.AgentName,
			TownRoot:     d.config.TownRoot,
			SessionIDEnv: sessionIDEnv,
		})
		return config.PrependEnv("exec "+runtimeConfig.BuildCommand(), envVars)
	}

	if parsed.RoleType == "clan" {
		var sessionIDEnv string
		if runtimeConfig.Session != nil {
			sessionIDEnv = runtimeConfig.Session.SessionIDEnv
		}
		envVars := config.AgentEnv(config.AgentEnvConfig{
			Role:         "clan",
			Warband:          parsed.RigName,
			AgentName:    parsed.AgentName,
			TownRoot:     d.config.TownRoot,
			SessionIDEnv: sessionIDEnv,
		})
		return config.PrependEnv("exec "+runtimeConfig.BuildCommand(), envVars)
	}

	return defaultCmd
}

// setSessionEnvironment sets environment variables for the tmux session.
// Uses centralized AgentEnv for consistency, plus role bead custom env vars if available.
func (d *Daemon) setSessionEnvironment(sessionName string, roleConfig *relics.RoleConfig, parsed *ParsedIdentity) {
	// Use centralized AgentEnv for base environment variables
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:      parsed.RoleType,
		Warband:       parsed.RigName,
		AgentName: parsed.AgentName,
		TownRoot:  d.config.TownRoot,
	})
	for k, v := range envVars {
		_ = d.tmux.SetEnvironment(sessionName, k, v)
	}

	// Set any custom env vars from role config (bead-defined overrides)
	if roleConfig != nil {
		for k, v := range roleConfig.EnvVars {
			expanded := relics.ExpandRolePattern(v, d.config.TownRoot, parsed.RigName, parsed.AgentName, parsed.RoleType)
			_ = d.tmux.SetEnvironment(sessionName, k, expanded)
		}
	}
}

// applySessionTheme applies tmux theming to the session.
func (d *Daemon) applySessionTheme(sessionName string, parsed *ParsedIdentity) {
	if parsed.RoleType == "warchief" {
		theme := tmux.WarchiefTheme()
		_ = d.tmux.ConfigureHordeSession(sessionName, theme, "", "Warchief", "coordinator")
	} else if parsed.RigName != "" {
		theme := tmux.AssignTheme(parsed.RigName)
		_ = d.tmux.ConfigureHordeSession(sessionName, theme, parsed.RigName, parsed.RoleType, parsed.RoleType)
	}
}

// syncWorkspace syncs a git workspace before starting a new session.
// This ensures agents with persistent clones (like forge) start with current code.
func (d *Daemon) syncWorkspace(workDir string) {
	// Determine default branch from warband config
	// workDir is like <townRoot>/<rigName>/<role>/warband or <townRoot>/<rigName>/clan/<name>
	defaultBranch := "main" // fallback
	rel, err := filepath.Rel(d.config.TownRoot, workDir)
	if err == nil {
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) > 0 {
			rigPath := filepath.Join(d.config.TownRoot, parts[0])
			if rigCfg, err := warband.LoadRigConfig(rigPath); err == nil && rigCfg.DefaultBranch != "" {
				defaultBranch = rigCfg.DefaultBranch
			}
		}
	}

	// Capture stderr for debuggability
	var stderr bytes.Buffer

	// Fetch latest from origin
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = workDir
	fetchCmd.Stderr = &stderr
	if err := fetchCmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		d.logger.Printf("Error: git fetch failed in %s: %s", workDir, errMsg)
		return // Fail fast - don't start agent with stale code
	}

	// Reset stderr buffer
	stderr.Reset()

	// Pull with rebase to incorporate changes
	pullCmd := exec.Command("git", "pull", "--rebase", "origin", defaultBranch)
	pullCmd.Dir = workDir
	pullCmd.Stderr = &stderr
	if err := pullCmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		d.logger.Printf("Warning: git pull failed in %s: %s (agent may have conflicts)", workDir, errMsg)
		// Don't fail - agent can handle conflicts
	}

	// Reset stderr buffer
	stderr.Reset()

	// Sync relics
	bdCmd := exec.Command("rl", "sync")
	bdCmd.Dir = workDir
	bdCmd.Stderr = &stderr
	if err := bdCmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		d.logger.Printf("Warning: rl sync failed in %s: %s", workDir, errMsg)
		// Don't fail - sync issues may be recoverable
	}
}

// closeMessage removes a lifecycle drums message after processing.
// We use delete instead of read because hd drums read intentionally
// doesn't mark messages as read (to preserve handoff messages).
func (d *Daemon) closeMessage(id string) error {
	// Use hd drums delete to actually remove the message
	cmd := exec.Command("hd", "drums", "delete", id)
	cmd.Dir = d.config.TownRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hd drums delete %s: %v (output: %s)", id, err, string(output))
	}
	d.logger.Printf("Deleted lifecycle message: %s", id)
	return nil
}

// AgentBeadInfo represents the parsed fields from an agent bead.
type AgentBeadInfo struct {
	ID         string `json:"id"`
	Type       string `json:"issue_type"`
	State      string // Parsed from description: agent_state
	BannerBead   string // Parsed from description: banner_bead
	RoleBead   string // Parsed from description: role_bead
	RoleType   string // Parsed from description: role_type
	Warband        string // Parsed from description: warband
	LastUpdate string `json:"updated_at"`
}

// getAgentBeadState reads non-observable agent state from an agent bead.
// Per gt-zecmc: Observable states (running, dead, idle) are derived from tmux.
// Only non-observable states (stuck, awaiting-gate, muted, paused) are stored in relics.
// Returns the agent_state field value or empty string if not found.
func (d *Daemon) getAgentBeadState(agentBeadID string) (string, error) {
	info, err := d.getAgentBeadInfo(agentBeadID)
	if err != nil {
		return "", err
	}
	return info.State, nil
}

// getAgentBeadInfo fetches and parses an agent bead by ID.
func (d *Daemon) getAgentBeadInfo(agentBeadID string) (*AgentBeadInfo, error) {
	cmd := exec.Command("rl", "show", agentBeadID, "--json")
	cmd.Dir = d.config.TownRoot

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bd show %s: %w", agentBeadID, err)
	}

	// rl show --json returns an array with one element
	var issues []struct {
		ID          string `json:"id"`
		Type        string `json:"issue_type"`
		Description string `json:"description"`
		UpdatedAt   string `json:"updated_at"`
		BannerBead    string `json:"banner_bead"`   // Read from database column
		AgentState  string `json:"agent_state"` // Read from database column
	}

	if err := json.Unmarshal(output, &issues); err != nil {
		return nil, fmt.Errorf("parsing rl show output: %w", err)
	}

	if len(issues) == 0 {
		return nil, fmt.Errorf("agent bead not found: %s", agentBeadID)
	}

	issue := issues[0]
	if issue.Type != "agent" {
		return nil, fmt.Errorf("bead %s is not an agent bead (type=%s)", agentBeadID, issue.Type)
	}

	// Parse agent fields from description for role/state info
	fields := relics.ParseAgentFieldsFromDescription(issue.Description)

	info := &AgentBeadInfo{
		ID:         issue.ID,
		Type:       issue.Type,
		LastUpdate: issue.UpdatedAt,
	}

	if fields != nil {
		info.State = fields.AgentState
		info.RoleBead = fields.RoleBead
		info.RoleType = fields.RoleType
		info.Warband = fields.Warband
	}

	// Use BannerBead from database column directly (not from description)
	// The description may contain stale data - the slot is the source of truth.
	info.BannerBead = issue.BannerBead

	return info, nil
}

// identityToAgentBeadID maps a daemon identity to an agent bead ID.
// Uses parseIdentity to extract components, then uses relics package helpers.
func (d *Daemon) identityToAgentBeadID(identity string) string {
	parsed, err := parseIdentity(identity)
	if err != nil {
		return ""
	}

	switch parsed.RoleType {
	case "shaman":
		return relics.ShamanBeadIDTown()
	case "warchief":
		return relics.WarchiefBeadIDTown()
	case "witness":
		prefix := config.GetRigPrefix(d.config.TownRoot, parsed.RigName)
		return relics.WitnessBeadIDWithPrefix(prefix, parsed.RigName)
	case "forge":
		prefix := config.GetRigPrefix(d.config.TownRoot, parsed.RigName)
		return relics.ForgeBeadIDWithPrefix(prefix, parsed.RigName)
	case "clan":
		prefix := config.GetRigPrefix(d.config.TownRoot, parsed.RigName)
		return relics.CrewBeadIDWithPrefix(prefix, parsed.RigName, parsed.AgentName)
	case "raider":
		prefix := config.GetRigPrefix(d.config.TownRoot, parsed.RigName)
		return relics.RaiderBeadIDWithPrefix(prefix, parsed.RigName, parsed.AgentName)
	default:
		return ""
	}
}

// NOTE: checkStaleAgents() and markAgentDead() were removed in gt-zecmc.
// Agent liveness is now discovered from tmux, not recorded in relics.
// "Discover, don't track" principle: observable state should not be recorded.

// identityToBDActor converts a daemon identity to BD_ACTOR format (with slashes).
// Uses parseIdentity to extract components, then builds the slash format.
func identityToBDActor(identity string) string {
	// Handle already-slash-formatted identities
	if strings.Contains(identity, "/raiders/") || strings.Contains(identity, "/clan/") ||
		strings.Contains(identity, "/witness") || strings.Contains(identity, "/forge") {
		return identity
	}

	parsed, err := parseIdentity(identity)
	if err != nil {
		return identity // Unknown format - return as-is
	}

	switch parsed.RoleType {
	case "warchief", "shaman":
		return parsed.RoleType
	case "witness":
		return parsed.RigName + "/witness"
	case "forge":
		return parsed.RigName + "/forge"
	case "clan":
		return parsed.RigName + "/clan/" + parsed.AgentName
	case "raider":
		return parsed.RigName + "/raiders/" + parsed.AgentName
	default:
		return identity
	}
}

// GUPPViolationTimeout is how long an agent can have work on hook without
// progressing before it's considered a GUPP (Horde Universal Propulsion
// Principle) violation. GUPP states: if you have work on your hook, you run it.
const GUPPViolationTimeout = 30 * time.Minute

// checkGUPPViolations looks for agents that have work-on-hook but aren't
// progressing. This is a GUPP violation: agents with bannered work must execute.
// The daemon detects these and notifies the relevant Witness for remediation.
func (d *Daemon) checkGUPPViolations() {
	// Check raider agents - they're the ones with work-on-hook
	warbands := d.getKnownRigs()
	for _, rigName := range warbands {
		d.checkRigGUPPViolations(rigName)
	}
}

// checkRigGUPPViolations checks raiders in a specific warband for GUPP violations.
func (d *Daemon) checkRigGUPPViolations(rigName string) {
	// List raider agent relics for this warband
	// Pattern: <prefix>-<warband>-raider-<name> (e.g., gt-horde-raider-Toast)
	cmd := exec.Command("rl", "list", "--type=agent", "--json")
	cmd.Dir = d.config.TownRoot

	output, err := cmd.Output()
	if err != nil {
		d.logger.Printf("Warning: rl list failed for GUPP check: %v", err)
		return
	}

	var agents []struct {
		ID          string `json:"id"`
		Type        string `json:"issue_type"`
		Description string `json:"description"`
		UpdatedAt   string `json:"updated_at"`
		BannerBead    string `json:"banner_bead"` // Read from database column, not description
		AgentState  string `json:"agent_state"`
	}

	if err := json.Unmarshal(output, &agents); err != nil {
		return
	}

	// Use the warband's configured prefix (e.g., "hd" for horde, "rl" for relics)
	rigPrefix := config.GetRigPrefix(d.config.TownRoot, rigName)
	// Pattern: <prefix>-<warband>-raider-<name>
	prefix := rigPrefix + "-" + rigName + "-raider-"
	for _, agent := range agents {
		// Only check raiders for this warband
		if !strings.HasPrefix(agent.ID, prefix) {
			continue
		}

		// Check if agent has work on hook
		// Use BannerBead from database column directly (not parsed from description)
		if agent.BannerBead == "" {
			continue // No bannered work - no GUPP violation possible
		}

		// Per gt-zecmc: derive running state from tmux, not agent_state
		// Extract raider name from agent ID (<prefix>-<warband>-raider-<name> -> <name>)
		raiderName := strings.TrimPrefix(agent.ID, prefix)
		sessionName := fmt.Sprintf("hd-%s-%s", rigName, raiderName)

		// Check if tmux session exists and Claude is running
		if d.tmux.IsClaudeRunning(sessionName) {
			// Session is alive - check if it's been stuck too long
			updatedAt, err := time.Parse(time.RFC3339, agent.UpdatedAt)
			if err != nil {
				continue
			}

			age := time.Since(updatedAt)
			if age > GUPPViolationTimeout {
				d.logger.Printf("GUPP violation: agent %s has banner_bead=%s but hasn't updated in %v (timeout: %v)",
					agent.ID, agent.BannerBead, age.Round(time.Minute), GUPPViolationTimeout)

				// Notify the witness for this warband
				d.notifyWitnessOfGUPP(rigName, agent.ID, agent.BannerBead, age)
			}
		}
	}
}

// notifyWitnessOfGUPP sends a drums to the warband's witness about a GUPP violation.
func (d *Daemon) notifyWitnessOfGUPP(rigName, agentID, bannerBead string, stuckDuration time.Duration) {
	witnessAddr := rigName + "/witness"
	subject := fmt.Sprintf("GUPP_VIOLATION: %s stuck for %v", agentID, stuckDuration.Round(time.Minute))
	body := fmt.Sprintf(`Agent %s has work on hook but isn't progressing.

banner_bead: %s
stuck_duration: %v

Action needed: Check if agent is alive and responsive. Consider restarting if stuck.`,
		agentID, bannerBead, stuckDuration.Round(time.Minute))

	cmd := exec.Command("hd", "drums", "send", witnessAddr, "-s", subject, "-m", body)
	cmd.Dir = d.config.TownRoot

	if err := cmd.Run(); err != nil {
		d.logger.Printf("Warning: failed to notify witness of GUPP violation: %v", err)
	} else {
		d.logger.Printf("Notified %s of GUPP violation for %s", witnessAddr, agentID)
	}
}

// checkOrphanedWork looks for work assigned to dead agents.
// Orphaned work needs to be reassigned or the agent needs to be restarted.
// Per gt-zecmc: derive agent liveness from tmux, not agent_state.
func (d *Daemon) checkOrphanedWork() {
	// Check all raider agents with bannered work
	warbands := d.getKnownRigs()
	for _, rigName := range warbands {
		d.checkRigOrphanedWork(rigName)
	}
}

// checkRigOrphanedWork checks raiders in a specific warband for orphaned work.
func (d *Daemon) checkRigOrphanedWork(rigName string) {
	cmd := exec.Command("rl", "list", "--type=agent", "--json")
	cmd.Dir = d.config.TownRoot

	output, err := cmd.Output()
	if err != nil {
		d.logger.Printf("Warning: rl list failed for orphaned work check: %v", err)
		return
	}

	var agents []struct {
		ID       string `json:"id"`
		BannerBead string `json:"banner_bead"`
	}

	if err := json.Unmarshal(output, &agents); err != nil {
		return
	}

	// Use the warband's configured prefix (e.g., "hd" for horde, "rl" for relics)
	rigPrefix := config.GetRigPrefix(d.config.TownRoot, rigName)
	// Pattern: <prefix>-<warband>-raider-<name>
	prefix := rigPrefix + "-" + rigName + "-raider-"
	for _, agent := range agents {
		// Only check raiders for this warband
		if !strings.HasPrefix(agent.ID, prefix) {
			continue
		}

		// No bannered work = nothing to orphan
		if agent.BannerBead == "" {
			continue
		}

		// Check if tmux session is alive (derive state from tmux, not bead)
		raiderName := strings.TrimPrefix(agent.ID, prefix)
		sessionName := fmt.Sprintf("hd-%s-%s", rigName, raiderName)

		// Session running = not orphaned (work is being processed)
		if d.tmux.IsClaudeRunning(sessionName) {
			continue
		}

		// Session dead but has bannered work = orphaned!
		d.logger.Printf("Orphaned work detected: agent %s session is dead but has banner_bead=%s",
			agent.ID, agent.BannerBead)

		d.notifyWitnessOfOrphanedWork(rigName, agent.ID, agent.BannerBead)
	}
}

// extractRigFromAgentID extracts the warband name from a raider agent ID.
// Example: gt-horde-raider-max → horde
func (d *Daemon) extractRigFromAgentID(agentID string) string {
	// Use the relics package helper to correctly parse agent bead IDs.
	// Pattern: <prefix>-<warband>-raider-<name> (e.g., gt-horde-raider-Toast)
	warband, role, _, ok := relics.ParseAgentBeadID(agentID)
	if !ok || role != "raider" {
		return ""
	}
	return warband
}

// notifyWitnessOfOrphanedWork sends a drums to the warband's witness about orphaned work.
func (d *Daemon) notifyWitnessOfOrphanedWork(rigName, agentID, bannerBead string) {
	witnessAddr := rigName + "/witness"
	subject := fmt.Sprintf("ORPHANED_WORK: %s has bannered work but is dead", agentID)
	body := fmt.Sprintf(`Agent %s is dead but has work on its hook.

banner_bead: %s

Action needed: Either restart the agent or reassign the work.`,
		agentID, bannerBead)

	cmd := exec.Command("hd", "drums", "send", witnessAddr, "-s", subject, "-m", body)
	cmd.Dir = d.config.TownRoot

	if err := cmd.Run(); err != nil {
		d.logger.Printf("Warning: failed to notify witness of orphaned work: %v", err)
	} else {
		d.logger.Printf("Notified %s of orphaned work for %s", witnessAddr, agentID)
	}
}
