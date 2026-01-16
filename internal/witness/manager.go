package witness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OWNER/horde/internal/agent"
	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/claude"
	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/constants"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/session"
	"github.com/OWNER/horde/internal/tmux"
	"github.com/OWNER/horde/internal/workspace"
)

// Common errors
var (
	ErrNotRunning     = errors.New("witness not running")
	ErrAlreadyRunning = errors.New("witness already running")
)

// Manager handles witness lifecycle and monitoring operations.
type Manager struct {
	warband          *warband.Warband
	workDir      string
	stateManager *agent.StateManager[Witness]
}

// NewManager creates a new witness manager for a warband.
func NewManager(r *warband.Warband) *Manager {
	return &Manager{
		warband:     r,
		workDir: r.Path,
		stateManager: agent.NewStateManager[Witness](r.Path, "witness.json", func() *Witness {
			return &Witness{
				RigName: r.Name,
				State:   StateStopped,
			}
		}),
	}
}

// stateFile returns the path to the witness state file.
func (m *Manager) stateFile() string {
	return m.stateManager.StateFile()
}

// loadState loads witness state from disk.
func (m *Manager) loadState() (*Witness, error) {
	return m.stateManager.Load()
}

// saveState persists witness state to disk using atomic write.
func (m *Manager) saveState(w *Witness) error {
	return m.stateManager.Save(w)
}

// SessionName returns the tmux session name for this witness.
func (m *Manager) SessionName() string {
	return fmt.Sprintf("gt-%s-witness", m.warband.Name)
}

// Status returns the current witness status.
// ZFC-compliant: trusts agent-reported state, no PID inference.
// The daemon reads agent bead state for liveness checks.
func (m *Manager) Status() (*Witness, error) {
	w, err := m.loadState()
	if err != nil {
		return nil, err
	}

	// Update monitored raiders list (still useful for display)
	w.MonitoredRaiders = m.warband.Raiders

	return w, nil
}

// witnessDir returns the working directory for the witness.
// Prefers witness/warband/, falls back to witness/, then warband root.
func (m *Manager) witnessDir() string {
	witnessRigDir := filepath.Join(m.warband.Path, "witness", "warband")
	if _, err := os.Stat(witnessRigDir); err == nil {
		return witnessRigDir
	}

	witnessDir := filepath.Join(m.warband.Path, "witness")
	if _, err := os.Stat(witnessDir); err == nil {
		return witnessDir
	}

	return m.warband.Path
}

// Start starts the witness.
// If foreground is true, only updates state (no tmux session - deprecated).
// Otherwise, spawns a Claude agent in a tmux session.
// agentOverride optionally specifies a different agent alias to use.
// envOverrides are KEY=VALUE pairs that override all other env var sources.
func (m *Manager) Start(foreground bool, agentOverride string, envOverrides []string) error {
	w, err := m.loadState()
	if err != nil {
		return err
	}

	t := tmux.NewTmux()
	sessionID := m.SessionName()

	if foreground {
		// Foreground mode is deprecated - scout logic moved to totem-witness-scout
		// Just check tmux session (no PID inference per ZFC)
		if running, _ := t.HasSession(sessionID); running && t.IsClaudeRunning(sessionID) {
			return ErrAlreadyRunning
		}

		now := time.Now()
		w.State = StateRunning
		w.StartedAt = &now
		w.PID = 0 // No longer track PID (ZFC)
		w.MonitoredRaiders = m.warband.Raiders

		return m.saveState(w)
	}

	// Background mode: check if session already exists
	running, _ := t.HasSession(sessionID)
	if running {
		// Session exists - check if Claude is actually running (healthy vs zombie)
		if t.IsClaudeRunning(sessionID) {
			// Healthy - Claude is running
			return ErrAlreadyRunning
		}
		// Zombie - tmux alive but Claude dead. Kill and recreate.
		if err := t.KillSession(sessionID); err != nil {
			return fmt.Errorf("killing zombie session: %w", err)
		}
	}

	// Note: No PID check per ZFC - tmux session is the source of truth

	// Working directory
	witnessDir := m.witnessDir()

	// Ensure Claude settings exist in witness/ (not witness/warband/) so we don't
	// write into the source repo. Claude walks up the tree to find settings.
	witnessParentDir := filepath.Join(m.warband.Path, "witness")
	if err := claude.EnsureSettingsForRole(witnessParentDir, "witness"); err != nil {
		return fmt.Errorf("ensuring Claude settings: %w", err)
	}

	roleConfig, err := m.roleConfig()
	if err != nil {
		return err
	}

	townRoot := m.townRoot()

	// Build startup command first
	// NOTE: No hd rally injection needed - SessionStart hook handles it automatically
	// Export GT_ROLE and BD_ACTOR in the command since tmux SetEnvironment only affects new panes
	// Pass m.warband.Path so warband agent settings are honored (not encampment-level defaults)
	command, err := buildWitnessStartCommand(m.warband.Path, m.warband.Name, townRoot, agentOverride, roleConfig)
	if err != nil {
		return err
	}

	// Create session with command directly to avoid send-keys race condition.
	// See: https://github.com/anthropics/horde/issues/280
	if err := t.NewSessionWithCommand(sessionID, witnessDir, command); err != nil {
		return fmt.Errorf("creating tmux session: %w", err)
	}

	// Set environment variables (non-fatal: session works without these)
	// Use centralized AgentEnv for consistency across all role startup paths
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:     "witness",
		Warband:      m.warband.Name,
		TownRoot: townRoot,
	})
	for k, v := range envVars {
		_ = t.SetEnvironment(sessionID, k, v)
	}
	// Apply role config env vars if present (non-fatal).
	for key, value := range roleConfigEnvVars(roleConfig, townRoot, m.warband.Name) {
		_ = t.SetEnvironment(sessionID, key, value)
	}
	// Apply CLI env overrides (highest priority, non-fatal).
	for _, override := range envOverrides {
		if key, value, ok := strings.Cut(override, "="); ok {
			_ = t.SetEnvironment(sessionID, key, value)
		}
	}

	// Apply Horde theming (non-fatal: theming failure doesn't affect operation)
	theme := tmux.AssignTheme(m.warband.Name)
	_ = t.ConfigureHordeSession(sessionID, theme, m.warband.Name, "witness", "witness")

	// Update state to running
	now := time.Now()
	w.State = StateRunning
	w.StartedAt = &now
	w.PID = 0 // Claude agent doesn't have a PID we track
	w.MonitoredRaiders = m.warband.Raiders
	if err := m.saveState(w); err != nil {
		_ = t.KillSession(sessionID) // best-effort cleanup on state save failure
		return fmt.Errorf("saving state: %w", err)
	}

	// Wait for Claude to start (non-fatal).
	if err := t.WaitForCommand(sessionID, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		// Non-fatal - try to continue anyway
	}

	// Accept bypass permissions warning dialog if it appears.
	_ = t.AcceptBypassPermissionsWarning(sessionID)

	time.Sleep(constants.ShutdownNotifyDelay)

	// Inject startup signal for predecessor discovery via /resume
	address := fmt.Sprintf("%s/witness", m.warband.Name)
	_ = session.StartupNudge(t, sessionID, session.StartupNudgeConfig{
		Recipient: address,
		Sender:    "shaman",
		Topic:     "scout",
	}) // Non-fatal

	// GUPP: Horde Universal Propulsion Principle
	// Send the propulsion signal to trigger autonomous scout execution.
	// Wait for beacon to be fully processed (needs to be separate prompt)
	time.Sleep(2 * time.Second)
	_ = t.SignalSession(sessionID, session.PropulsionNudgeForRole("witness", witnessDir)) // Non-fatal

	return nil
}

func (m *Manager) roleConfig() (*relics.RoleConfig, error) {
	// Role relics use hq- prefix and live in encampment-level relics, not warband relics
	townRoot := m.townRoot()
	bd := relics.NewWithRelicsDir(townRoot, relics.ResolveRelicsDir(townRoot))
	roleConfig, err := bd.GetRoleConfig(relics.RoleBeadIDTown("witness"))
	if err != nil {
		return nil, fmt.Errorf("loading witness role config: %w", err)
	}
	return roleConfig, nil
}

func (m *Manager) townRoot() string {
	townRoot, err := workspace.Find(m.warband.Path)
	if err != nil || townRoot == "" {
		return m.warband.Path
	}
	return townRoot
}

func roleConfigEnvVars(roleConfig *relics.RoleConfig, townRoot, rigName string) map[string]string {
	if roleConfig == nil || len(roleConfig.EnvVars) == 0 {
		return nil
	}
	expanded := make(map[string]string, len(roleConfig.EnvVars))
	for key, value := range roleConfig.EnvVars {
		expanded[key] = relics.ExpandRolePattern(value, townRoot, rigName, "", "witness")
	}
	return expanded
}

func buildWitnessStartCommand(rigPath, rigName, townRoot, agentOverride string, roleConfig *relics.RoleConfig) (string, error) {
	if agentOverride != "" {
		roleConfig = nil
	}
	if roleConfig != nil && roleConfig.StartCommand != "" {
		return relics.ExpandRolePattern(roleConfig.StartCommand, townRoot, rigName, "", "witness"), nil
	}
	command, err := config.BuildAgentStartupCommandWithAgentOverride("witness", rigName, townRoot, rigPath, "", agentOverride)
	if err != nil {
		return "", fmt.Errorf("building startup command: %w", err)
	}
	return command, nil
}

// Stop stops the witness.
func (m *Manager) Stop() error {
	w, err := m.loadState()
	if err != nil {
		return err
	}

	// Check if tmux session exists
	t := tmux.NewTmux()
	sessionID := m.SessionName()
	sessionRunning, _ := t.HasSession(sessionID)

	// If neither state nor session indicates running, it's not running
	if w.State != StateRunning && !sessionRunning {
		return ErrNotRunning
	}

	// Kill tmux session if it exists (best-effort: may already be dead)
	if sessionRunning {
		_ = t.KillSession(sessionID)
	}

	// Note: No PID-based stop per ZFC - tmux session kill is sufficient

	w.State = StateStopped
	w.PID = 0

	return m.saveState(w)
}
