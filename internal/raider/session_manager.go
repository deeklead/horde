// Package raider provides raider workspace and session management.
package raider

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/runtime"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/tmux"
)

// debugSession logs non-fatal errors during session startup when HD_DEBUG_SESSION=1.
func debugSession(context string, err error) {
	if os.Getenv("HD_DEBUG_SESSION") != "" && err != nil {
		fmt.Fprintf(os.Stderr, "[session-debug] %s: %v\n", context, err)
	}
}

// Session errors
var (
	ErrSessionRunning  = errors.New("session already running")
	ErrSessionNotFound = errors.New("session not found")
)

// SessionManager handles raider session lifecycle.
type SessionManager struct {
	tmux *tmux.Tmux
	warband  *warband.Warband
}

// NewSessionManager creates a new raider session manager for a warband.
func NewSessionManager(t *tmux.Tmux, r *warband.Warband) *SessionManager {
	return &SessionManager{
		tmux: t,
		warband:  r,
	}
}

// SessionStartOptions configures raider session startup.
type SessionStartOptions struct {
	// WorkDir overrides the default working directory (raider clone dir).
	WorkDir string

	// Issue is an optional issue ID to work on.
	Issue string

	// Command overrides the default "claude" command.
	Command string

	// Account specifies the account handle to use (overrides default).
	Account string

	// RuntimeConfigDir is resolved config directory for the runtime account.
	// If set, this is injected as an environment variable.
	RuntimeConfigDir string
}

// SessionInfo contains information about a running raider session.
type SessionInfo struct {
	// Raider is the raider name.
	Raider string `json:"raider"`

	// SessionID is the tmux session identifier.
	SessionID string `json:"session_id"`

	// Running indicates if the session is currently active.
	Running bool `json:"running"`

	// RigName is the warband this session belongs to.
	RigName string `json:"rig_name"`

	// Attached indicates if someone is attached to the session.
	Attached bool `json:"attached,omitempty"`

	// Created is when the session was created.
	Created time.Time `json:"created,omitempty"`

	// Windows is the number of tmux windows.
	Windows int `json:"windows,omitempty"`

	// LastActivity is when the session last had activity.
	LastActivity time.Time `json:"last_activity,omitempty"`
}

// SessionName generates the tmux session name for a raider.
func (m *SessionManager) SessionName(raider string) string {
	return fmt.Sprintf("gt-%s-%s", m.warband.Name, raider)
}

// raiderDir returns the parent directory for a raider.
// This is raiders/<name>/ - the raider's home directory.
func (m *SessionManager) raiderDir(raider string) string {
	return filepath.Join(m.warband.Path, "raiders", raider)
}

// clonePath returns the path where the git worktree lives.
// New structure: raiders/<name>/<rigname>/ - gives LLMs recognizable repo context.
// Falls back to old structure: raiders/<name>/ for backward compatibility.
func (m *SessionManager) clonePath(raider string) string {
	// New structure: raiders/<name>/<rigname>/
	newPath := filepath.Join(m.warband.Path, "raiders", raider, m.warband.Name)
	if info, err := os.Stat(newPath); err == nil && info.IsDir() {
		return newPath
	}

	// Old structure: raiders/<name>/ (backward compat)
	oldPath := filepath.Join(m.warband.Path, "raiders", raider)
	if info, err := os.Stat(oldPath); err == nil && info.IsDir() {
		// Check if this is actually a git worktree (has .git file or dir)
		gitPath := filepath.Join(oldPath, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return oldPath
		}
	}

	// Default to new structure for new raiders
	return newPath
}

// hasRaider checks if the raider exists in this warband.
func (m *SessionManager) hasRaider(raider string) bool {
	raiderPath := m.raiderDir(raider)
	info, err := os.Stat(raiderPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// Start creates and starts a new session for a raider.
func (m *SessionManager) Start(raider string, opts SessionStartOptions) error {
	if !m.hasRaider(raider) {
		return fmt.Errorf("%w: %s", ErrRaiderNotFound, raider)
	}

	sessionID := m.SessionName(raider)

	// Check if session already exists
	// Note: Orphan sessions are cleaned up by ReconcilePool during AllocateName,
	// so by this point, any existing session should be legitimately in use.
	running, err := m.tmux.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if running {
		return fmt.Errorf("%w: %s", ErrSessionRunning, sessionID)
	}

	// Determine working directory
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = m.clonePath(raider)
	}

	runtimeConfig := config.LoadRuntimeConfig(m.warband.Path)

	// Ensure runtime settings exist in raiders/ (not raiders/<name>/) so we don't
	// write into the source repo. Runtime walks up the tree to find settings.
	raidersDir := filepath.Join(m.warband.Path, "raiders")
	if err := runtime.EnsureSettingsForRole(raidersDir, "raider", runtimeConfig); err != nil {
		return fmt.Errorf("ensuring runtime settings: %w", err)
	}

	// Build startup command first
	command := opts.Command
	if command == "" {
		command = config.BuildRaiderStartupCommand(m.warband.Name, raider, m.warband.Path, "")
	}
	// Prepend runtime config dir env if needed
	if runtimeConfig.Session != nil && runtimeConfig.Session.ConfigDirEnv != "" && opts.RuntimeConfigDir != "" {
		command = config.PrependEnv(command, map[string]string{runtimeConfig.Session.ConfigDirEnv: opts.RuntimeConfigDir})
	}

	// Create session with command directly to avoid send-keys race condition.
	// See: https://github.com/anthropics/horde/issues/280
	if err := m.tmux.NewSessionWithCommand(sessionID, workDir, command); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	// Set environment (non-fatal: session works without these)
	// Use centralized AgentEnv for consistency across all role startup paths
	townRoot := filepath.Dir(m.warband.Path)
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:             "raider",
		Warband:              m.warband.Name,
		AgentName:        raider,
		TownRoot:         townRoot,
		RuntimeConfigDir: opts.RuntimeConfigDir,
		RelicsNoDaemon:    true,
	})
	for k, v := range envVars {
		debugSession("SetEnvironment "+k, m.tmux.SetEnvironment(sessionID, k, v))
	}

	// Hook the issue to the raider if provided via --issue flag
	if opts.Issue != "" {
		agentID := fmt.Sprintf("%s/raiders/%s", m.warband.Name, raider)
		if err := m.hookIssue(opts.Issue, agentID, workDir); err != nil {
			fmt.Printf("Warning: could not hook issue %s: %v\n", opts.Issue, err)
		}
	}

	// Apply theme (non-fatal)
	theme := tmux.AssignTheme(m.warband.Name)
	debugSession("ConfigureHordeSession", m.tmux.ConfigureHordeSession(sessionID, theme, m.warband.Name, raider, "raider"))

	// Set pane-died hook for crash detection (non-fatal)
	agentID := fmt.Sprintf("%s/%s", m.warband.Name, raider)
	debugSession("SetPaneDiedHook", m.tmux.SetPaneDiedHook(sessionID, agentID))

	// Wait for Claude to start (non-fatal)
	debugSession("WaitForCommand", m.tmux.WaitForCommand(sessionID, constants.SupportedShells, constants.ClaudeStartTimeout))

	// Accept bypass permissions warning dialog if it appears
	debugSession("AcceptBypassPermissionsWarning", m.tmux.AcceptBypassPermissionsWarning(sessionID))

	// Wait for runtime to be fully ready at the prompt (not just started)
	runtime.SleepForReadyDelay(runtimeConfig)
	_ = runtime.RunStartupFallback(m.tmux, sessionID, "raider", runtimeConfig)

	// Inject startup signal for predecessor discovery via /resume
	address := fmt.Sprintf("%s/raiders/%s", m.warband.Name, raider)
	debugSession("StartupNudge", session.StartupNudge(m.tmux, sessionID, session.StartupNudgeConfig{
		Recipient: address,
		Sender:    "witness",
		Topic:     "assigned",
		MolID:     opts.Issue,
	}))

	// GUPP: Send propulsion signal to trigger autonomous work execution
	time.Sleep(2 * time.Second)
	debugSession("SignalSession PropulsionNudge", m.tmux.SignalSession(sessionID, session.PropulsionNudge()))

	return nil
}

// Stop terminates a raider session.
func (m *SessionManager) Stop(raider string, force bool) error {
	sessionID := m.SessionName(raider)

	running, err := m.tmux.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return ErrSessionNotFound
	}

	// Sync relics before shutdown (non-fatal)
	if !force {
		raiderDir := m.raiderDir(raider)
		if err := m.syncRelics(raiderDir); err != nil {
			fmt.Printf("Warning: relics sync failed: %v\n", err)
		}
	}

	// Try graceful shutdown first
	if !force {
		_ = m.tmux.SendKeysRaw(sessionID, "C-c")
		time.Sleep(100 * time.Millisecond)
	}

	if err := m.tmux.KillSession(sessionID); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}

	return nil
}

// syncRelics runs rl sync in the given directory.
func (m *SessionManager) syncRelics(workDir string) error {
	cmd := exec.Command("rl", "sync")
	cmd.Dir = workDir
	return cmd.Run()
}

// IsRunning checks if a raider session is active.
func (m *SessionManager) IsRunning(raider string) (bool, error) {
	sessionID := m.SessionName(raider)
	return m.tmux.HasSession(sessionID)
}

// Status returns detailed status for a raider session.
func (m *SessionManager) Status(raider string) (*SessionInfo, error) {
	sessionID := m.SessionName(raider)

	running, err := m.tmux.HasSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("checking session: %w", err)
	}

	info := &SessionInfo{
		Raider:   raider,
		SessionID: sessionID,
		Running:   running,
		RigName:   m.warband.Name,
	}

	if !running {
		return info, nil
	}

	tmuxInfo, err := m.tmux.GetSessionInfo(sessionID)
	if err != nil {
		return info, nil
	}

	info.Attached = tmuxInfo.Attached
	info.Windows = tmuxInfo.Windows

	if tmuxInfo.Created != "" {
		formats := []string{
			"Mon Jan 2 15:04:05 2006",
			"Mon Jan _2 15:04:05 2006",
			time.ANSIC,
			time.UnixDate,
		}
		for _, format := range formats {
			if t, err := time.Parse(format, tmuxInfo.Created); err == nil {
				info.Created = t
				break
			}
		}
	}

	if tmuxInfo.Activity != "" {
		var activityUnix int64
		if _, err := fmt.Sscanf(tmuxInfo.Activity, "%d", &activityUnix); err == nil && activityUnix > 0 {
			info.LastActivity = time.Unix(activityUnix, 0)
		}
	}

	return info, nil
}

// List returns information about all raider sessions for this warband.
func (m *SessionManager) List() ([]SessionInfo, error) {
	sessions, err := m.tmux.ListSessions()
	if err != nil {
		return nil, err
	}

	prefix := fmt.Sprintf("gt-%s-", m.warband.Name)
	var infos []SessionInfo

	for _, sessionID := range sessions {
		if !strings.HasPrefix(sessionID, prefix) {
			continue
		}

		raider := strings.TrimPrefix(sessionID, prefix)
		infos = append(infos, SessionInfo{
			Raider:   raider,
			SessionID: sessionID,
			Running:   true,
			RigName:   m.warband.Name,
		})
	}

	return infos, nil
}

// Summon attaches to a raider session.
func (m *SessionManager) Summon(raider string) error {
	sessionID := m.SessionName(raider)

	running, err := m.tmux.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return ErrSessionNotFound
	}

	return m.tmux.AttachSession(sessionID)
}

// Capture returns the recent output from a raider session.
func (m *SessionManager) Capture(raider string, lines int) (string, error) {
	sessionID := m.SessionName(raider)

	running, err := m.tmux.HasSession(sessionID)
	if err != nil {
		return "", fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return "", ErrSessionNotFound
	}

	return m.tmux.CapturePane(sessionID, lines)
}

// CaptureSession returns the recent output from a session by raw session ID.
func (m *SessionManager) CaptureSession(sessionID string, lines int) (string, error) {
	running, err := m.tmux.HasSession(sessionID)
	if err != nil {
		return "", fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return "", ErrSessionNotFound
	}

	return m.tmux.CapturePane(sessionID, lines)
}

// Inject sends a message to a raider session.
func (m *SessionManager) Inject(raider, message string) error {
	sessionID := m.SessionName(raider)

	running, err := m.tmux.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return ErrSessionNotFound
	}

	debounceMs := 200 + (len(message)/1024)*100
	if debounceMs > 1500 {
		debounceMs = 1500
	}

	return m.tmux.SendKeysDebounced(sessionID, message, debounceMs)
}

// StopAll terminates all raider sessions for this warband.
func (m *SessionManager) StopAll(force bool) error {
	infos, err := m.List()
	if err != nil {
		return err
	}

	var lastErr error
	for _, info := range infos {
		if err := m.Stop(info.Raider, force); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// hookIssue pins an issue to a raider's hook using rl update.
func (m *SessionManager) hookIssue(issueID, agentID, workDir string) error {
	cmd := exec.Command("rl", "update", issueID, "--status=bannered", "--assignee="+agentID) //nolint:gosec
	cmd.Dir = workDir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bd update failed: %w", err)
	}
	fmt.Printf("✓ Planted issue %s to %s\n", issueID, agentID)
	return nil
}
