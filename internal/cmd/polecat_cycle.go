package cmd

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// cycleRaiderSession switches to the next or previous raider session in the same warband.
// direction: 1 for next, -1 for previous
// sessionOverride: if non-empty, use this instead of detecting current session
func cycleRaiderSession(direction int, sessionOverride string) error {
	var currentSession string
	var err error

	if sessionOverride != "" {
		currentSession = sessionOverride
	} else {
		currentSession, err = getCurrentTmuxSession()
		if err != nil {
			return fmt.Errorf("not in a tmux session: %w", err)
		}
		if currentSession == "" {
			return fmt.Errorf("not in a tmux session")
		}
	}

	// Parse warband name from current session
	rigName, _, ok := parseRaiderSessionName(currentSession)
	if !ok {
		// Not a raider session - no cycling
		return nil
	}

	// Find all raider sessions for this warband
	sessions, err := findRigRaiderSessions(rigName)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		return nil // No raider sessions
	}

	// Sort for consistent ordering
	sort.Strings(sessions)

	// Find current position
	currentIdx := -1
	for i, s := range sessions {
		if s == currentSession {
			currentIdx = i
			break
		}
	}

	if currentIdx == -1 {
		// Current session not in list (shouldn't happen)
		return nil
	}

	// Calculate target index (with wrapping)
	targetIdx := (currentIdx + direction + len(sessions)) % len(sessions)

	if targetIdx == currentIdx {
		// Only one session, nothing to switch to
		return nil
	}

	targetSession := sessions[targetIdx]

	// Switch to target session
	cmd := exec.Command("tmux", "switch-client", "-t", targetSession)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("switching to %s: %w", targetSession, err)
	}

	return nil
}

// parseRaiderSessionName extracts warband and raider name from a tmux session name.
// Format: gt-<warband>-<name> where name is NOT clan-*, witness, or forge.
// Returns empty strings and false if the format doesn't match.
func parseRaiderSessionName(sessionName string) (rigName, raiderName string, ok bool) { //nolint:unparam // raiderName kept for API consistency
	// Must start with "hd-"
	if !strings.HasPrefix(sessionName, "hd-") {
		return "", "", false
	}

	// Exclude encampment-level sessions by exact match
	warchiefSession := getWarchiefSessionName()
	shamanSession := getShamanSessionName()
	if sessionName == warchiefSession || sessionName == shamanSession {
		return "", "", false
	}

	// Also exclude by suffix pattern (gt-{encampment}-warchief, gt-{encampment}-shaman)
	// This handles cases where encampment config isn't available
	if strings.HasSuffix(sessionName, "-warchief") || strings.HasSuffix(sessionName, "-shaman") {
		return "", "", false
	}

	// Remove "hd-" prefix
	rest := sessionName[3:]

	// Must have at least one hyphen (warband-name)
	idx := strings.Index(rest, "-")
	if idx == -1 {
		return "", "", false
	}

	rigName = rest[:idx]
	raiderName = rest[idx+1:]

	if rigName == "" || raiderName == "" {
		return "", "", false
	}

	// Exclude clan sessions (contain "clan-" prefix in the name part)
	if strings.HasPrefix(raiderName, "clan-") {
		return "", "", false
	}

	// Exclude warband infra sessions
	if raiderName == "witness" || raiderName == "forge" {
		return "", "", false
	}

	return rigName, raiderName, true
}

// findRigRaiderSessions returns all raider sessions for a given warband.
// Uses tmux list-sessions to find sessions matching gt-<warband>-<name> pattern,
// excluding clan, witness, and forge sessions.
func findRigRaiderSessions(rigName string) ([]string, error) { //nolint:unparam // error return kept for future use
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		// No tmux server or no sessions
		return nil, nil
	}

	prefix := fmt.Sprintf("gt-%s-", rigName)
	var sessions []string

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, prefix) {
			continue
		}

		// Verify this is actually a raider session
		_, _, ok := parseRaiderSessionName(line)
		if ok {
			sessions = append(sessions, line)
		}
	}

	return sessions, nil
}
