package warchief

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OWNER/horde/internal/claude"
	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/constants"
	"github.com/OWNER/horde/internal/session"
	"github.com/OWNER/horde/internal/tmux"
)

// Common errors
var (
	ErrNotRunning     = errors.New("warchief not running")
	ErrAlreadyRunning = errors.New("warchief already running")
)

// Manager handles warchief lifecycle operations.
type Manager struct {
	townRoot string
}

// NewManager creates a new warchief manager for a encampment.
func NewManager(townRoot string) *Manager {
	return &Manager{
		townRoot: townRoot,
	}
}

// SessionName returns the tmux session name for the warchief.
// This is a package-level function for convenience.
func SessionName() string {
	return session.WarchiefSessionName()
}

// SessionName returns the tmux session name for the warchief.
func (m *Manager) SessionName() string {
	return SessionName()
}

// warchiefDir returns the working directory for the warchief.
func (m *Manager) warchiefDir() string {
	return filepath.Join(m.townRoot, "warchief")
}

// Start starts the warchief session.
// agentOverride optionally specifies a different agent alias to use.
func (m *Manager) Start(agentOverride string) error {
	t := tmux.NewTmux()
	sessionID := m.SessionName()

	// Check if session already exists
	running, _ := t.HasSession(sessionID)
	if running {
		// Session exists - check if Claude is actually running (healthy vs zombie)
		if t.IsClaudeRunning(sessionID) {
			return ErrAlreadyRunning
		}
		// Zombie - tmux alive but Claude dead. Kill and recreate.
		if err := t.KillSession(sessionID); err != nil {
			return fmt.Errorf("killing zombie session: %w", err)
		}
	}

	// Ensure warchief directory exists (for Claude settings)
	warchiefDir := m.warchiefDir()
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		return fmt.Errorf("creating warchief directory: %w", err)
	}

	// Ensure Claude settings exist
	if err := claude.EnsureSettingsForRole(warchiefDir, "warchief"); err != nil {
		return fmt.Errorf("ensuring Claude settings: %w", err)
	}

	// Build startup beacon with explicit instructions (matches hd handoff behavior)
	// This ensures the agent has clear context immediately, not after nudges arrive
	beacon := session.FormatStartupNudge(session.StartupNudgeConfig{
		Recipient: "warchief",
		Sender:    "human",
		Topic:     "cold-start",
	})

	// Build startup command WITH the beacon prompt - the startup hook handles 'hd rally' automatically
	// Export GT_ROLE and BD_ACTOR in the command since tmux SetEnvironment only affects new panes
	startupCmd, err := config.BuildAgentStartupCommandWithAgentOverride("warchief", "", m.townRoot, "", beacon, agentOverride)
	if err != nil {
		return fmt.Errorf("building startup command: %w", err)
	}

	// Create session in townRoot (not warchiefDir) to match hd handoff behavior
	// This ensures Warchief works from the encampment root where all tools work correctly
	// See: https://github.com/anthropics/horde/issues/280
	if err := t.NewSessionWithCommand(sessionID, m.townRoot, startupCmd); err != nil {
		return fmt.Errorf("creating tmux session: %w", err)
	}

	// Set environment variables (non-fatal: session works without these)
	// Use centralized AgentEnv for consistency across all role startup paths
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:     "warchief",
		TownRoot: m.townRoot,
	})
	for k, v := range envVars {
		_ = t.SetEnvironment(sessionID, k, v)
	}

	// Apply Warchief theming (non-fatal: theming failure doesn't affect operation)
	theme := tmux.WarchiefTheme()
	_ = t.ConfigureHordeSession(sessionID, theme, "", "Warchief", "coordinator")

	// Wait for Claude to start (non-fatal)
	if err := t.WaitForCommand(sessionID, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		// Non-fatal - try to continue anyway
	}

	// Accept bypass permissions warning dialog if it appears.
	_ = t.AcceptBypassPermissionsWarning(sessionID)

	time.Sleep(constants.ShutdownNotifyDelay)

	// Startup beacon with instructions is now included in the initial command,
	// so no separate signal needed. The agent starts with full context immediately.

	return nil
}

// Stop stops the warchief session.
func (m *Manager) Stop() error {
	t := tmux.NewTmux()
	sessionID := m.SessionName()

	// Check if session exists
	running, err := t.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return ErrNotRunning
	}

	// Try graceful shutdown first (best-effort interrupt)
	_ = t.SendKeysRaw(sessionID, "C-c")
	time.Sleep(100 * time.Millisecond)

	// Kill the session
	if err := t.KillSession(sessionID); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}

	return nil
}

// IsRunning checks if the warchief session is active.
func (m *Manager) IsRunning() (bool, error) {
	t := tmux.NewTmux()
	return t.HasSession(m.SessionName())
}

// Status returns information about the warchief session.
func (m *Manager) Status() (*tmux.SessionInfo, error) {
	t := tmux.NewTmux()
	sessionID := m.SessionName()

	running, err := t.HasSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return nil, ErrNotRunning
	}

	return t.GetSessionInfo(sessionID)
}
