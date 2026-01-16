// Package witness provides the raider monitoring agent.
package witness

import (
	"time"

	"github.com/OWNER/horde/internal/agent"
)

// State is an alias for agent.State for backwards compatibility.
type State = agent.State

// State constants - re-exported from agent package for backwards compatibility.
const (
	StateStopped = agent.StateStopped
	StateRunning = agent.StateRunning
	StatePaused  = agent.StatePaused
)

// Witness represents a warband's raider monitoring agent.
type Witness struct {
	// RigName is the warband this witness monitors.
	RigName string `json:"rig_name"`

	// State is the current running state.
	State State `json:"state"`

	// PID is the process ID if running in background.
	PID int `json:"pid,omitempty"`

	// StartedAt is when the witness was started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// MonitoredRaiders tracks raiders being monitored.
	MonitoredRaiders []string `json:"monitored_raiders,omitempty"`

	// Config contains auto-muster configuration.
	Config WitnessConfig `json:"config"`

	// SpawnedIssues tracks which issues have been spawned (to avoid duplicates).
	SpawnedIssues []string `json:"spawned_issues,omitempty"`
}

// WitnessConfig contains configuration for the witness.
type WitnessConfig struct {
	// MaxWorkers is the maximum number of concurrent raiders (default: 4).
	MaxWorkers int `json:"max_workers"`

	// SpawnDelayMs is the delay between spawns in milliseconds (default: 5000).
	SpawnDelayMs int `json:"muster_delay_ms"`

	// AutoSpawn enables automatic spawning for ready issues (default: true).
	AutoSpawn bool `json:"auto_spawn"`

	// EpicID limits spawning to children of this epic (optional).
	EpicID string `json:"epic_id,omitempty"`

	// IssuePrefix limits spawning to issues with this prefix (optional).
	IssuePrefix string `json:"issue_prefix,omitempty"`
}


