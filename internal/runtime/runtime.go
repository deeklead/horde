// Package runtime provides helpers for runtime-specific integration.
package runtime

import (
	"os"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/claude"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/opencode"
	"github.com/deeklead/horde/internal/tmux"
)

// EnsureSettingsForRole installs runtime hook settings when supported.
func EnsureSettingsForRole(workDir, role string, rc *config.RuntimeConfig) error {
	if rc == nil {
		rc = config.DefaultRuntimeConfig()
	}

	if rc.Hooks == nil {
		return nil
	}

	switch rc.Hooks.Provider {
	case "claude":
		return claude.EnsureSettingsForRoleAt(workDir, role, rc.Hooks.Dir, rc.Hooks.SettingsFile)
	case "opencode":
		return opencode.EnsurePluginAt(workDir, rc.Hooks.Dir, rc.Hooks.SettingsFile)
	default:
		return nil
	}
}

// SessionIDFromEnv returns the runtime session ID, if present.
// It checks GT_SESSION_ID_ENV first, then falls back to CLAUDE_SESSION_ID.
func SessionIDFromEnv() string {
	if envName := os.Getenv("GT_SESSION_ID_ENV"); envName != "" {
		if sessionID := os.Getenv(envName); sessionID != "" {
			return sessionID
		}
	}
	return os.Getenv("CLAUDE_SESSION_ID")
}

// SleepForReadyDelay sleeps for the runtime's configured readiness delay.
func SleepForReadyDelay(rc *config.RuntimeConfig) {
	if rc == nil || rc.Tmux == nil {
		return
	}
	if rc.Tmux.ReadyDelayMs <= 0 {
		return
	}
	time.Sleep(time.Duration(rc.Tmux.ReadyDelayMs) * time.Millisecond)
}

// StartupFallbackCommands returns commands that approximate Claude hooks when hooks are unavailable.
func StartupFallbackCommands(role string, rc *config.RuntimeConfig) []string {
	if rc == nil {
		rc = config.DefaultRuntimeConfig()
	}
	if rc.Hooks != nil && rc.Hooks.Provider != "" && rc.Hooks.Provider != "none" {
		return nil
	}

	role = strings.ToLower(role)
	command := "hd rally"
	if isAutonomousRole(role) {
		command += " && hd drums check --inject"
	}
	command += " && hd signal shaman session-started"

	return []string{command}
}

// RunStartupFallback sends the startup fallback commands via tmux.
func RunStartupFallback(t *tmux.Tmux, sessionID, role string, rc *config.RuntimeConfig) error {
	commands := StartupFallbackCommands(role, rc)
	for _, cmd := range commands {
		if err := t.SignalSession(sessionID, cmd); err != nil {
			return err
		}
	}
	return nil
}

// isAutonomousRole returns true if the given role should automatically
// inject drums check on startup. Autonomous roles (raider, witness,
// forge, shaman) operate without human prompting and need drums injection
// to receive work assignments.
//
// Non-autonomous roles (warchief, clan) are human-guided and should not
// have automatic drums injection to avoid confusion.
func isAutonomousRole(role string) bool {
	switch role {
	case "raider", "witness", "forge", "shaman":
		return true
	default:
		return false
	}
}
