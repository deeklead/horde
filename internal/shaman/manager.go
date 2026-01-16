package shaman

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
	ErrNotRunning     = errors.New("shaman not running")
	ErrAlreadyRunning = errors.New("shaman already running")
)

// Manager handles shaman lifecycle operations.
type Manager struct {
	townRoot string
}

// NewManager creates a new shaman manager for a encampment.
func NewManager(townRoot string) *Manager {
	return &Manager{
		townRoot: townRoot,
	}
}

// SessionName returns the tmux session name for the shaman.
// This is a package-level function for convenience.
func SessionName() string {
	return session.ShamanSessionName()
}

// SessionName returns the tmux session name for the shaman.
func (m *Manager) SessionName() string {
	return SessionName()
}

// shamanDir returns the working directory for the shaman.
func (m *Manager) shamanDir() string {
	return filepath.Join(m.townRoot, "shaman")
}

// Start starts the shaman session.
// agentOverride allows specifying an alternate agent alias (e.g., for testing).
// Restarts are handled by daemon via ensureShamanRunning on each heartbeat.
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

	// Ensure shaman directory exists
	shamanDir := m.shamanDir()
	if err := os.MkdirAll(shamanDir, 0755); err != nil {
		return fmt.Errorf("creating shaman directory: %w", err)
	}

	// Ensure Claude settings exist
	if err := claude.EnsureSettingsForRole(shamanDir, "shaman"); err != nil {
		return fmt.Errorf("ensuring Claude settings: %w", err)
	}

	// Build startup command first
	// Restarts are handled by daemon via ensureShamanRunning on each heartbeat
	startupCmd, err := config.BuildAgentStartupCommandWithAgentOverride("shaman", "", m.townRoot, "", "", agentOverride)
	if err != nil {
		return fmt.Errorf("building startup command: %w", err)
	}

	// Create session with command directly to avoid send-keys race condition.
	// See: https://github.com/anthropics/horde/issues/280
	if err := t.NewSessionWithCommand(sessionID, shamanDir, startupCmd); err != nil {
		return fmt.Errorf("creating tmux session: %w", err)
	}

	// Set environment variables (non-fatal: session works without these)
	// Use centralized AgentEnv for consistency across all role startup paths
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:     "shaman",
		TownRoot: m.townRoot,
	})
	for k, v := range envVars {
		_ = t.SetEnvironment(sessionID, k, v)
	}

	// Apply Shaman theming (non-fatal: theming failure doesn't affect operation)
	theme := tmux.ShamanTheme()
	_ = t.ConfigureHordeSession(sessionID, theme, "", "Shaman", "health-check")

	// Wait for Claude to start (non-fatal)
	if err := t.WaitForCommand(sessionID, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		// Non-fatal - try to continue anyway
	}

	// Accept bypass permissions warning dialog if it appears.
	_ = t.AcceptBypassPermissionsWarning(sessionID)

	time.Sleep(constants.ShutdownNotifyDelay)

	// Inject startup signal for predecessor discovery via /resume
	_ = session.StartupNudge(t, sessionID, session.StartupNudgeConfig{
		Recipient: "shaman",
		Sender:    "daemon",
		Topic:     "scout",
	}) // Non-fatal

	// GUPP: Horde Universal Propulsion Principle
	// Send the propulsion signal to trigger autonomous scout execution.
	// Wait for beacon to be fully processed (needs to be separate prompt)
	time.Sleep(2 * time.Second)
	_ = t.SignalSession(sessionID, session.PropulsionNudgeForRole("shaman", shamanDir)) // Non-fatal

	return nil
}

// Stop stops the shaman session.
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

// IsRunning checks if the shaman session is active.
func (m *Manager) IsRunning() (bool, error) {
	t := tmux.NewTmux()
	return t.HasSession(m.SessionName())
}

// Status returns information about the shaman session.
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
