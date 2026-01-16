package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

var nudgeMessageFlag string
var nudgeForceFlag bool

func init() {
	rootCmd.AddCommand(nudgeCmd)
	nudgeCmd.Flags().StringVarP(&nudgeMessageFlag, "message", "m", "", "Message to send")
	nudgeCmd.Flags().BoolVarP(&nudgeForceFlag, "force", "f", false, "Send even if target has DND enabled")
}

var nudgeCmd = &cobra.Command{
	Use:     "signal <target> [message]",
	GroupID: GroupComm,
	Short:   "Send a message to a raider or shaman session reliably",
	Long: `Sends a message to a raider's or shaman's Claude Code session.

Uses a reliable delivery pattern:
1. Sends text in literal mode (-l flag)
2. Waits 500ms for paste to complete
3. Sends Enter as a separate command

This is the ONLY way to send messages to Claude sessions.
Do not use raw tmux send-keys elsewhere.

Role shortcuts (expand to session names):
  warchief     Maps to gt-warchief
  shaman    Maps to gt-shaman
  witness   Maps to gt-<warband>-witness (uses current warband)
  forge  Maps to gt-<warband>-forge (uses current warband)

Channel syntax:
  channel:<name>  Nudges all members of a named channel defined in
                  ~/horde/config/messaging.json under "nudge_channels".
                  Patterns like "horde/raiders/*" are expanded.

DND (Do Not Disturb):
  If the target has DND enabled (gt dnd on), the signal is skipped.
  Use --force to override DND and send anyway.

Examples:
  hd signal greenplace/furiosa "Check your drums and start working"
  hd signal greenplace/alpha -m "What's your status?"
  hd signal warchief "Status update requested"
  hd signal witness "Check raider health"
  hd signal shaman session-started
  hd signal channel:workers "New priority work available"`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runNudge,
}

func runNudge(cmd *cobra.Command, args []string) error {
	target := args[0]

	// Get message from -m flag or positional arg
	var message string
	if nudgeMessageFlag != "" {
		message = nudgeMessageFlag
	} else if len(args) >= 2 {
		message = args[1]
	} else {
		return fmt.Errorf("message required: use -m flag or provide as second argument")
	}

	// Handle channel syntax: channel:<name>
	if strings.HasPrefix(target, "channel:") {
		channelName := strings.TrimPrefix(target, "channel:")
		return runNudgeChannel(channelName, message)
	}

	// Identify sender for message prefix
	sender := "unknown"
	if roleInfo, err := GetRole(); err == nil {
		switch roleInfo.Role {
		case RoleWarchief:
			sender = "warchief"
		case RoleCrew:
			sender = fmt.Sprintf("%s/clan/%s", roleInfo.Warband, roleInfo.Raider)
		case RoleRaider:
			sender = fmt.Sprintf("%s/%s", roleInfo.Warband, roleInfo.Raider)
		case RoleWitness:
			sender = fmt.Sprintf("%s/witness", roleInfo.Warband)
		case RoleForge:
			sender = fmt.Sprintf("%s/forge", roleInfo.Warband)
		case RoleShaman:
			sender = "shaman"
		default:
			sender = string(roleInfo.Role)
		}
	}

	// Prefix message with sender
	message = fmt.Sprintf("[from %s] %s", sender, message)

	// Check DND status for target (unless force flag or channel target)
	townRoot, _ := workspace.FindFromCwd()
	if townRoot != "" && !nudgeForceFlag && !strings.HasPrefix(target, "channel:") {
		shouldSend, level, _ := shouldNudgeTarget(townRoot, target, nudgeForceFlag)
		if !shouldSend {
			fmt.Printf("%s Target has DND enabled (%s) - signal skipped\n", style.Dim.Render("○"), level)
			fmt.Printf("  Use %s to override\n", style.Bold.Render("--force"))
			return nil
		}
	}

	t := tmux.NewTmux()

	// Expand role shortcuts to session names
	// These shortcuts let users type "warchief" instead of "hd-warchief"
	switch target {
	case "warchief":
		target = session.WarchiefSessionName()
	case "witness", "forge":
		// These need the current warband
		roleInfo, err := GetRole()
		if err != nil {
			return fmt.Errorf("cannot determine warband for %s shortcut: %w", target, err)
		}
		if roleInfo.Warband == "" {
			return fmt.Errorf("cannot determine warband for %s shortcut (not in a warband context)", target)
		}
		if target == "witness" {
			target = session.WitnessSessionName(roleInfo.Warband)
		} else {
			target = session.ForgeSessionName(roleInfo.Warband)
		}
	}

	// Special case: "shaman" target maps to the Shaman session
	if target == "shaman" {
		shamanSession := session.ShamanSessionName()
		// Check if Shaman session exists
		exists, err := t.HasSession(shamanSession)
		if err != nil {
			return fmt.Errorf("checking shaman session: %w", err)
		}
		if !exists {
			// Shaman not running - this is not an error, just log and return
			fmt.Printf("%s Shaman not running, signal skipped\n", style.Dim.Render("○"))
			return nil
		}

		if err := t.SignalSession(shamanSession, message); err != nil {
			return fmt.Errorf("nudging shaman: %w", err)
		}

		fmt.Printf("%s Nudged shaman\n", style.Bold.Render("✓"))

		// Log signal event
		if townRoot, err := workspace.FindFromCwd(); err == nil && townRoot != "" {
			_ = LogNudge(townRoot, "shaman", message)
		}
		_ = events.LogFeed(events.TypeNudge, sender, events.NudgePayload("", "shaman", message))
		return nil
	}

	// Check if target is warband/raider format or raw session name
	if strings.Contains(target, "/") {
		// Parse warband/raider format
		rigName, raiderName, err := parseAddress(target)
		if err != nil {
			return err
		}

		var sessionName string

		// Check if this is a clan address (raiderName starts with "clan/")
		if strings.HasPrefix(raiderName, "clan/") {
			// Extract clan name and use clan session naming
			crewName := strings.TrimPrefix(raiderName, "clan/")
			sessionName = crewSessionName(rigName, crewName)
		} else {
			// Regular raider - use session manager
			mgr, _, err := getSessionManager(rigName)
			if err != nil {
				return err
			}
			sessionName = mgr.SessionName(raiderName)
		}

		// Send signal using the reliable SignalSession
		if err := t.SignalSession(sessionName, message); err != nil {
			return fmt.Errorf("nudging session: %w", err)
		}

		fmt.Printf("%s Nudged %s/%s\n", style.Bold.Render("✓"), rigName, raiderName)

		// Log signal event
		if townRoot, err := workspace.FindFromCwd(); err == nil && townRoot != "" {
			_ = LogNudge(townRoot, target, message)
		}
		_ = events.LogFeed(events.TypeNudge, sender, events.NudgePayload(rigName, target, message))
	} else {
		// Raw session name (legacy)
		exists, err := t.HasSession(target)
		if err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if !exists {
			return fmt.Errorf("session %q not found", target)
		}

		if err := t.SignalSession(target, message); err != nil {
			return fmt.Errorf("nudging session: %w", err)
		}

		fmt.Printf("✓ Nudged %s\n", target)

		// Log signal event
		if townRoot, err := workspace.FindFromCwd(); err == nil && townRoot != "" {
			_ = LogNudge(townRoot, target, message)
		}
		_ = events.LogFeed(events.TypeNudge, sender, events.NudgePayload("", target, message))
	}

	return nil
}

// runNudgeChannel nudges all members of a named channel.
func runNudgeChannel(channelName, message string) error {
	// Find encampment root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("cannot find encampment root: %w", err)
	}

	// Load messaging config
	msgConfigPath := config.MessagingConfigPath(townRoot)
	msgConfig, err := config.LoadMessagingConfig(msgConfigPath)
	if err != nil {
		return fmt.Errorf("loading messaging config: %w", err)
	}

	// Look up channel
	patterns, ok := msgConfig.NudgeChannels[channelName]
	if !ok {
		return fmt.Errorf("signal channel %q not found in messaging config", channelName)
	}

	if len(patterns) == 0 {
		return fmt.Errorf("signal channel %q has no members", channelName)
	}

	// Identify sender for message prefix
	sender := "unknown"
	if roleInfo, err := GetRole(); err == nil {
		switch roleInfo.Role {
		case RoleWarchief:
			sender = "warchief"
		case RoleCrew:
			sender = fmt.Sprintf("%s/clan/%s", roleInfo.Warband, roleInfo.Raider)
		case RoleRaider:
			sender = fmt.Sprintf("%s/%s", roleInfo.Warband, roleInfo.Raider)
		case RoleWitness:
			sender = fmt.Sprintf("%s/witness", roleInfo.Warband)
		case RoleForge:
			sender = fmt.Sprintf("%s/forge", roleInfo.Warband)
		case RoleShaman:
			sender = "shaman"
		default:
			sender = string(roleInfo.Role)
		}
	}

	// Prefix message with sender
	prefixedMessage := fmt.Sprintf("[from %s] %s", sender, message)

	// Get all running sessions for pattern matching
	agents, err := getAgentSessions(true)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	// Resolve patterns to session names
	var targets []string
	seenTargets := make(map[string]bool)

	for _, pattern := range patterns {
		resolved := resolveNudgePattern(pattern, agents)
		for _, sessionName := range resolved {
			if !seenTargets[sessionName] {
				seenTargets[sessionName] = true
				targets = append(targets, sessionName)
			}
		}
	}

	if len(targets) == 0 {
		fmt.Printf("%s No sessions match channel %q patterns\n", style.WarningPrefix, channelName)
		return nil
	}

	// Send nudges
	t := tmux.NewTmux()
	var succeeded, failed int
	var failures []string

	fmt.Printf("Nudging channel %q (%d target(s))...\n\n", channelName, len(targets))

	for i, sessionName := range targets {
		if err := t.SignalSession(sessionName, prefixedMessage); err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: %v", sessionName, err))
			fmt.Printf("  %s %s\n", style.ErrorPrefix, sessionName)
		} else {
			succeeded++
			fmt.Printf("  %s %s\n", style.SuccessPrefix, sessionName)
		}

		// Small delay between nudges
		if i < len(targets)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	fmt.Println()

	// Log signal event
	_ = events.LogFeed(events.TypeNudge, sender, events.NudgePayload("", "channel:"+channelName, message))

	if failed > 0 {
		fmt.Printf("%s Channel signal complete: %d succeeded, %d failed\n",
			style.WarningPrefix, succeeded, failed)
		for _, f := range failures {
			fmt.Printf("  %s\n", style.Dim.Render(f))
		}
		return fmt.Errorf("%d signal(s) failed", failed)
	}

	fmt.Printf("%s Channel signal complete: %d target(s) nudged\n", style.SuccessPrefix, succeeded)
	return nil
}

// resolveNudgePattern resolves a signal channel pattern to session names.
// Patterns can be:
//   - Literal: "horde/witness" → gt-horde-witness
//   - Wildcard: "horde/raiders/*" → all raider sessions in horde
//   - Role: "*/witness" → all witness sessions
//   - Special: "warchief", "shaman" → gt-{encampment}-warchief, gt-{encampment}-shaman
// townName is used to generate the correct session names for warchief/shaman.
func resolveNudgePattern(pattern string, agents []*AgentSession) []string {
	var results []string

	// Handle special cases
	switch pattern {
	case "warchief":
		return []string{session.WarchiefSessionName()}
	case "shaman":
		return []string{session.ShamanSessionName()}
	}

	// Parse pattern
	if !strings.Contains(pattern, "/") {
		// Unknown pattern format
		return nil
	}

	parts := strings.SplitN(pattern, "/", 2)
	rigPattern := parts[0]
	targetPattern := parts[1]

	for _, agent := range agents {
		// Match warband pattern
		if rigPattern != "*" && rigPattern != agent.Warband {
			continue
		}

		// Match target pattern
		if strings.HasPrefix(targetPattern, "raiders/") {
			// raiders/* or raiders/<name>
			if agent.Type != AgentRaider {
				continue
			}
			suffix := strings.TrimPrefix(targetPattern, "raiders/")
			if suffix != "*" && suffix != agent.AgentName {
				continue
			}
		} else if strings.HasPrefix(targetPattern, "clan/") {
			// clan/* or clan/<name>
			if agent.Type != AgentCrew {
				continue
			}
			suffix := strings.TrimPrefix(targetPattern, "clan/")
			if suffix != "*" && suffix != agent.AgentName {
				continue
			}
		} else if targetPattern == "witness" {
			if agent.Type != AgentWitness {
				continue
			}
		} else if targetPattern == "forge" {
			if agent.Type != AgentForge {
				continue
			}
		} else {
			// Assume it's a raider name (legacy short format)
			if agent.Type != AgentRaider || agent.AgentName != targetPattern {
				continue
			}
		}

		results = append(results, agent.Name)
	}

	return results
}

// shouldNudgeTarget checks if a signal should be sent based on the target's notification level.
// Returns (shouldSend bool, level string, err error).
// If force is true, always returns true.
// If the agent bead cannot be found, returns true (fail-open for backward compatibility).
func shouldNudgeTarget(townRoot, targetAddress string, force bool) (bool, string, error) { //nolint:unparam // error return kept for future use
	if force {
		return true, "", nil
	}

	// Try to determine agent bead ID from address
	agentBeadID := addressToAgentBeadID(targetAddress)
	if agentBeadID == "" {
		// Can't determine agent bead, allow the signal
		return true, "", nil
	}

	bd := relics.New(townRoot)
	level, err := bd.GetAgentNotificationLevel(agentBeadID)
	if err != nil {
		// Agent bead might not exist, allow the signal
		return true, "", nil
	}

	// Allow signal if level is not muted
	return level != relics.NotifyMuted, level, nil
}

// addressToAgentBeadID converts a target address to an agent bead ID.
// Examples:
//   - "warchief" -> "hd-{encampment}-warchief"
//   - "shaman" -> "hd-{encampment}-shaman"
//   - "horde/witness" -> "hd-horde-witness"
//   - "horde/alpha" -> "hd-horde-raider-alpha"
//
// Returns empty string if the address cannot be converted.
func addressToAgentBeadID(address string) string {
	// Handle special cases
	switch address {
	case "warchief":
		return session.WarchiefSessionName()
	case "shaman":
		return session.ShamanSessionName()
	}

	// Parse warband/role format
	if !strings.Contains(address, "/") {
		return ""
	}

	parts := strings.SplitN(address, "/", 2)
	if len(parts) != 2 {
		return ""
	}

	warband := parts[0]
	role := parts[1]

	switch role {
	case "witness":
		return fmt.Sprintf("hd-%s-witness", warband)
	case "forge":
		return fmt.Sprintf("hd-%s-forge", warband)
	default:
		// Assume raider
		if strings.HasPrefix(role, "clan/") {
			crewName := strings.TrimPrefix(role, "clan/")
			return fmt.Sprintf("hd-%s-clan-%s", warband, crewName)
		}
		return fmt.Sprintf("hd-%s-raider-%s", warband, role)
	}
}
