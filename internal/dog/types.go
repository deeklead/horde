// Package dog manages Dogs - Shaman's helper workers for infrastructure tasks.
// Dogs are reusable workers with multi-warband worktrees, managed by the Shaman.
// Unlike raiders (single-warband, ephemeral), dogs handle cross-warband infrastructure work.
package dog

import (
	"time"
)

// State represents a dog's operational state.
type State string

const (
	// StateIdle means the dog is available for work.
	StateIdle State = "idle"
	// StateWorking means the dog is executing a task.
	StateWorking State = "working"
)

// Dog represents a Shaman helper worker.
type Dog struct {
	Name       string            // Dog name (e.g., "alpha")
	State      State             // Current state
	Path       string            // Path to kennel dir (~/horde/shaman/dogs/<name>)
	Worktrees  map[string]string // Warband name -> worktree path
	LastActive time.Time         // Last activity timestamp
	Work       string            // Current work assignment (bead ID or totem)
	CreatedAt  time.Time         // When dog was added to kennel
}

// DogState is the persistent state stored in .dog.json.
type DogState struct {
	Name       string            `json:"name"`
	State      State             `json:"state"`
	LastActive time.Time         `json:"last_active"`
	Work       string            `json:"work,omitempty"`       // Current work assignment
	Worktrees  map[string]string `json:"worktrees,omitempty"`  // Warband -> path (for verification)
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}
