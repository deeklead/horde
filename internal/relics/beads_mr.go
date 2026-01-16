// Package relics provides merge request and gate utilities.
package relics

import (
	"fmt"
	"strings"
)

// FindMRForBranch searches for an existing merge-request bead for the given branch.
// Returns the MR bead if found, nil if not found.
// This enables idempotent `hd done` - if an MR already exists, we skip creation.
func (b *Relics) FindMRForBranch(branch string) (*Issue, error) {
	// List all merge-request relics (open status only - closed MRs are already processed)
	issues, err := b.List(ListOptions{
		Status: "open",
		Label:  "gt:merge-request",
	})
	if err != nil {
		return nil, err
	}

	// Search for one matching this branch
	// MR description format: "branch: <branch>\ntarget: ..."
	branchPrefix := "branch: " + branch + "\n"
	for _, issue := range issues {
		if strings.HasPrefix(issue.Description, branchPrefix) {
			return issue, nil
		}
	}

	return nil, nil
}

// AddGateWaiter registers an agent as a waiter on a gate bead.
// When the gate closes, the waiter will receive a wake notification via hd gate wake.
// The waiter is typically the raider's address (e.g., "horde/raiders/Toast").
func (b *Relics) AddGateWaiter(gateID, waiter string) error {
	// Use rl gate add-waiter to register the waiter on the gate
	// This adds the waiter to the gate's native waiters field
	_, err := b.run("gate", "add-waiter", gateID, waiter)
	if err != nil {
		return fmt.Errorf("adding gate waiter: %w", err)
	}
	return nil
}
