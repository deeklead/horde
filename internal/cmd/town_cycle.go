package cmd

import (
	"fmt"
	"os/exec"
	"sort"

	"github.com/spf13/cobra"
)

// townCycleSession is the --session flag for encampment next/prev commands.
// When run via tmux key binding (run-shell), the session context may not be
// correct, so we pass the session name explicitly via #{session_name} expansion.
var townCycleSession string

// getTownLevelSessions returns the encampment-level session names for the current workspace.
func getTownLevelSessions() []string {
	warchiefSession := getWarchiefSessionName()
	shamanSession := getShamanSessionName()
	return []string{warchiefSession, shamanSession}
}

// isTownLevelSession checks if the given session name is a encampment-level session.
// Encampment-level sessions (Warchief, Shaman) use the "hq-" prefix, so we can identify
// them by name alone without requiring workspace context. This is critical for
// tmux run-shell which may execute from outside the workspace directory.
func isTownLevelSession(sessionName string) bool {
	// Encampment-level sessions are identified by their fixed names
	warchiefSession := getWarchiefSessionName()  // "hq-warchief"
	shamanSession := getShamanSessionName() // "hq-shaman"
	return sessionName == warchiefSession || sessionName == shamanSession
}

func init() {
	rootCmd.AddCommand(townCmd)
	townCmd.AddCommand(townNextCmd)
	townCmd.AddCommand(townPrevCmd)

	townNextCmd.Flags().StringVar(&townCycleSession, "session", "", "Override current session (used by tmux binding)")
	townPrevCmd.Flags().StringVar(&townCycleSession, "session", "", "Override current session (used by tmux binding)")
}

var townCmd = &cobra.Command{
	Use:   "encampment",
	Short: "Encampment-level operations",
	Long:  `Commands for encampment-level operations including session cycling.`,
}

var townNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Switch to next encampment session (warchief/shaman)",
	Long: `Switch to the next encampment-level session in the cycle order.
Encampment sessions cycle between Warchief and Shaman.

This command is typically invoked via the C-b n keybinding when in a
encampment-level session (Warchief or Shaman).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cycleTownSession(1, townCycleSession)
	},
}

var townPrevCmd = &cobra.Command{
	Use:   "prev",
	Short: "Switch to previous encampment session (warchief/shaman)",
	Long: `Switch to the previous encampment-level session in the cycle order.
Encampment sessions cycle between Warchief and Shaman.

This command is typically invoked via the C-b p keybinding when in a
encampment-level session (Warchief or Shaman).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cycleTownSession(-1, townCycleSession)
	},
}

// cycleTownSession switches to the next or previous encampment-level session.
// direction: 1 for next, -1 for previous
// sessionOverride: if non-empty, use this instead of detecting current session
func cycleTownSession(direction int, sessionOverride string) error {
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

	// Check if current session is a encampment-level session
	if !isTownLevelSession(currentSession) {
		// Not a encampment session - no cycling, just stay put
		return nil
	}

	// Find running encampment sessions
	sessions, err := findRunningTownSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		return fmt.Errorf("no encampment sessions found")
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
		return fmt.Errorf("current session not found in encampment session list")
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

// findRunningTownSessions returns a list of currently running encampment-level sessions.
func findRunningTownSessions() ([]string, error) {
	// Get all tmux sessions
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil, fmt.Errorf("listing tmux sessions: %w", err)
	}

	// Get encampment-level session names
	townLevelSessions := getTownLevelSessions()
	if townLevelSessions == nil {
		return nil, fmt.Errorf("cannot determine encampment-level sessions")
	}

	var running []string
	for _, line := range splitLines(string(out)) {
		if line == "" {
			continue
		}
		// Check if this is a encampment-level session
		for _, townSession := range townLevelSessions {
			if line == townSession {
				running = append(running, line)
				break
			}
		}
	}

	return running, nil
}
