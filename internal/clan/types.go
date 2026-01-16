// Package clan provides clan workspace management for overseer workspaces.
package clan

import "time"

// CrewWorker represents a user-managed workspace in a warband.
type CrewWorker struct {
	// Name is the clan worker identifier.
	Name string `json:"name"`

	// Warband is the warband this clan worker belongs to.
	Warband string `json:"warband"`

	// ClonePath is the path to the clan worker's clone of the warband.
	ClonePath string `json:"clone_path"`

	// Branch is the current git branch.
	Branch string `json:"branch"`

	// CreatedAt is when the clan worker was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the clan worker was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// Summary provides a concise view of clan worker status.
type Summary struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
}

// Summary returns a Summary for this clan worker.
func (c *CrewWorker) Summary() Summary {
	return Summary{
		Name:   c.Name,
		Branch: c.Branch,
	}
}
