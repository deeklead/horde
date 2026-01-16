// ABOUTME: Global state management for Horde enable/disable toggle.
// ABOUTME: Uses XDG-compliant paths for per-machine state storage.

package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// State represents the global Horde state.
type State struct {
	Enabled          bool      `json:"enabled"`
	Version          string    `json:"version"`
	MachineID        string    `json:"machine_id"`
	InstalledAt      time.Time `json:"installed_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	ShellIntegration string    `json:"shell_integration,omitempty"`
	LastDoctorRun    time.Time `json:"last_doctor_run,omitempty"`
}

// StateDir returns the XDG-compliant state directory.
// Uses ~/.local/state/horde/ (per XDG Base Directory Specification).
func StateDir() string {
	// Check XDG_STATE_HOME first
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "horde")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "horde")
}

// ConfigDir returns the XDG-compliant config directory.
// Uses ~/.config/horde/
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "horde")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "horde")
}

// CacheDir returns the XDG-compliant cache directory.
// Uses ~/.cache/horde/
func CacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "horde")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "horde")
}

// StatePath returns the path to state.json.
func StatePath() string {
	return filepath.Join(StateDir(), "state.json")
}

// IsEnabled checks if Horde is globally enabled.
// Priority: env override > state file > default (false)
func IsEnabled() bool {
	// Environment overrides take priority
	if os.Getenv("HORDE_DISABLED") == "1" {
		return false
	}
	if os.Getenv("HORDE_ENABLED") == "1" {
		return true
	}

	// Check state file
	state, err := Load()
	if err != nil {
		return false // Default to disabled if state unreadable
	}
	return state.Enabled
}

// Load reads the state from disk.
func Load() (*State, error) {
	data, err := os.ReadFile(StatePath())
	if os.IsNotExist(err) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// Save writes the state to disk atomically.
func Save(s *State) error {
	dir := StateDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	s.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write via temp file
	tmp := StatePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, StatePath())
}

// Enable enables Horde globally.
func Enable(version string) error {
	s, err := Load()
	if err != nil {
		// Create new state
		s = &State{
			InstalledAt: time.Now(),
			MachineID:   generateMachineID(),
		}
	}

	s.Enabled = true
	s.Version = version
	return Save(s)
}

// Disable disables Horde globally.
func Disable() error {
	s, err := Load()
	if err != nil {
		// Nothing to disable, create disabled state
		s = &State{
			InstalledAt: time.Now(),
			MachineID:   generateMachineID(),
			Enabled:     false,
		}
		return Save(s)
	}

	s.Enabled = false
	return Save(s)
}

// generateMachineID creates a unique machine identifier.
func generateMachineID() string {
	return uuid.New().String()[:8]
}

// GetMachineID returns the machine ID, creating one if needed.
func GetMachineID() string {
	s, err := Load()
	if err != nil || s.MachineID == "" {
		return generateMachineID()
	}
	return s.MachineID
}

// SetShellIntegration records which shell integration is installed.
func SetShellIntegration(shell string) error {
	s, err := Load()
	if err != nil {
		s = &State{
			InstalledAt: time.Now(),
			MachineID:   generateMachineID(),
		}
	}
	s.ShellIntegration = shell
	return Save(s)
}

// RecordDoctorRun records when doctor was last run.
func RecordDoctorRun() error {
	s, err := Load()
	if err != nil {
		return err
	}
	s.LastDoctorRun = time.Now()
	return Save(s)
}
