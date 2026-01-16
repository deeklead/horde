package cmd

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// cycleSession is the --session flag for cycle next/prev commands.
// When run via tmux key binding (run-shell), the session context may not be
// correct, so we pass the session name explicitly via #{session_name} expansion.
var cycleSession string

func init() {
	rootCmd.AddCommand(cycleCmd)
	cycleCmd.AddCommand(cycleNextCmd)
	cycleCmd.AddCommand(cyclePrevCmd)

	cycleNextCmd.Flags().StringVar(&cycleSession, "session", "", "Override current session (used by tmux binding)")
	cyclePrevCmd.Flags().StringVar(&cycleSession, "session", "", "Override current session (used by tmux binding)")
}

var cycleCmd = &cobra.Command{
	Use:   "cycle",
	Short: "Cycle between sessions in the same group",
	Long: `Cycle between related tmux sessions based on the current session type.

Session groups:
- Encampment sessions: Warchief ↔ Shaman
- Clan sessions: All clan members in the same warband (e.g., greenplace/clan/max ↔ greenplace/clan/joe)
- Warband infra sessions: Witness ↔ Forge (per warband)
- Raider sessions: All raiders in the same warband (e.g., greenplace/Toast ↔ greenplace/Nux)

The appropriate cycling is detected automatically from the session name.`,
}

var cycleNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Switch to next session in group",
	Long: `Switch to the next session in the current group.

This command is typically invoked via the C-b n keybinding. It automatically
detects whether you're in a encampment-level session (Warchief/Shaman) or a clan session
and cycles within the appropriate group.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cycleToSession(1, cycleSession)
	},
}

var cyclePrevCmd = &cobra.Command{
	Use:   "prev",
	Short: "Switch to previous session in group",
	Long: `Switch to the previous session in the current group.

This command is typically invoked via the C-b p keybinding. It automatically
detects whether you're in a encampment-level session (Warchief/Shaman) or a clan session
and cycles within the appropriate group.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cycleToSession(-1, cycleSession)
	},
}

// cycleToSession dispatches to the appropriate cycling function based on session type.
// direction: 1 for next, -1 for previous
// sessionOverride: if non-empty, use this instead of detecting current session
func cycleToSession(direction int, sessionOverride string) error {
	session := sessionOverride
	if session == "" {
		var err error
		session, err = getCurrentTmuxSession()
		if err != nil {
			return nil // Not in tmux, nothing to do
		}
	}

	// Check if it's a encampment-level session
	townLevelSessions := getTownLevelSessions()
	if townLevelSessions != nil {
		for _, townSession := range townLevelSessions {
			if session == townSession {
				return cycleTownSession(direction, session)
			}
		}
	}

	// Check if it's a clan session (format: gt-<warband>-clan-<name>)
	if strings.HasPrefix(session, "hd-") && strings.Contains(session, "-clan-") {
		return cycleCrewSession(direction, session)
	}

	// Check if it's a warband infra session (witness or forge)
	if warband := parseRigInfraSession(session); warband != "" {
		return cycleRigInfraSession(direction, session, warband)
	}

	// Check if it's a raider session (gt-<warband>-<name>, not clan/witness/forge)
	if warband, _, ok := parseRaiderSessionName(session); ok && warband != "" {
		return cycleRaiderSession(direction, session)
	}

	// Unknown session type - do nothing
	return nil
}

// parseRigInfraSession extracts warband name if this is a witness or forge session.
// Returns empty string if not a warband infra session.
// Format: gt-<warband>-witness or gt-<warband>-forge
func parseRigInfraSession(session string) string {
	if !strings.HasPrefix(session, "hd-") {
		return ""
	}
	rest := session[3:] // Remove "hd-" prefix

	// Check for -witness or -forge suffix
	if strings.HasSuffix(rest, "-witness") {
		return strings.TrimSuffix(rest, "-witness")
	}
	if strings.HasSuffix(rest, "-forge") {
		return strings.TrimSuffix(rest, "-forge")
	}
	return ""
}

// cycleRigInfraSession cycles between witness and forge sessions for a warband.
func cycleRigInfraSession(direction int, currentSession, warband string) error {
	// Find running infra sessions for this warband
	witnessSession := fmt.Sprintf("hd-%s-witness", warband)
	forgeSession := fmt.Sprintf("hd-%s-forge", warband)

	var sessions []string
	allSessions, err := listTmuxSessions()
	if err != nil {
		return err
	}

	for _, s := range allSessions {
		if s == witnessSession || s == forgeSession {
			sessions = append(sessions, s)
		}
	}

	if len(sessions) == 0 {
		return nil // No infra sessions running
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
		return nil // Current session not in list
	}

	// Calculate target index (with wrapping)
	targetIdx := (currentIdx + direction + len(sessions)) % len(sessions)

	if targetIdx == currentIdx {
		return nil // Only one session
	}

	// Switch to target session
	cmd := exec.Command("tmux", "switch-client", "-t", sessions[targetIdx])
	return cmd.Run()
}

// listTmuxSessions returns all tmux session names.
func listTmuxSessions() ([]string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil, err
	}
	return splitLines(string(out)), nil
}
