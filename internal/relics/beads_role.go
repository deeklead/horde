// Package relics provides role bead management.
package relics

import (
	"errors"
	"fmt"
)

// Role bead ID naming convention:
// Role relics are stored in encampment relics (~/.relics/) with hq- prefix.
//
// Canonical format: hq-<role>-role
//
// Examples:
//   - hq-warchief-role
//   - hq-shaman-role
//   - hq-witness-role
//   - hq-forge-role
//   - hq-clan-role
//   - hq-raider-role
//
// Use RoleBeadIDTown() to get canonical role bead IDs.
// The legacy RoleBeadID() function returns gt-<role>-role for backward compatibility.

// RoleBeadID returns the role bead ID for a given role type.
// Role relics define lifecycle configuration for each agent type.
// Deprecated: Use RoleBeadIDTown() for encampment-level relics with hq- prefix.
// Role relics are global templates and should use hq-<role>-role, not gt-<role>-role.
func RoleBeadID(roleType string) string {
	return "hd-" + roleType + "-role"
}

// DogRoleBeadID returns the Dog role bead ID.
func DogRoleBeadID() string {
	return RoleBeadID("dog")
}

// WarchiefRoleBeadID returns the Warchief role bead ID.
func WarchiefRoleBeadID() string {
	return RoleBeadID("warchief")
}

// ShamanRoleBeadID returns the Shaman role bead ID.
func ShamanRoleBeadID() string {
	return RoleBeadID("shaman")
}

// WitnessRoleBeadID returns the Witness role bead ID.
func WitnessRoleBeadID() string {
	return RoleBeadID("witness")
}

// ForgeRoleBeadID returns the Forge role bead ID.
func ForgeRoleBeadID() string {
	return RoleBeadID("forge")
}

// CrewRoleBeadID returns the Clan role bead ID.
func CrewRoleBeadID() string {
	return RoleBeadID("clan")
}

// RaiderRoleBeadID returns the Raider role bead ID.
func RaiderRoleBeadID() string {
	return RoleBeadID("raider")
}

// GetRoleConfig looks up a role bead and returns its parsed RoleConfig.
// Returns nil, nil if the role bead doesn't exist or has no config.
func (b *Relics) GetRoleConfig(roleBeadID string) (*RoleConfig, error) {
	issue, err := b.Show(roleBeadID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if !HasLabel(issue, "gt:role") {
		return nil, fmt.Errorf("bead %s is not a role bead (missing gt:role label)", roleBeadID)
	}

	return ParseRoleConfig(issue.Description), nil
}

// HasLabel checks if an issue has a specific label.
func HasLabel(issue *Issue, label string) bool {
	for _, l := range issue.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// RoleBeadDef defines a role bead's metadata.
// Used by hd install and hd doctor to create missing role relics.
type RoleBeadDef struct {
	ID    string // e.g., "hq-witness-role"
	Title string // e.g., "Witness Role"
	Desc  string // Description of the role
}

// AllRoleBeadDefs returns all role bead definitions.
// This is the single source of truth for role relics used by both
// hd install (initial creation) and hd doctor --fix (repair).
func AllRoleBeadDefs() []RoleBeadDef {
	return []RoleBeadDef{
		{
			ID:    WarchiefRoleBeadIDTown(),
			Title: "Warchief Role",
			Desc:  "Role definition for Warchief agents. Global coordinator for cross-warband work.",
		},
		{
			ID:    ShamanRoleBeadIDTown(),
			Title: "Shaman Role",
			Desc:  "Role definition for Shaman agents. Daemon beacon for heartbeats and monitoring.",
		},
		{
			ID:    DogRoleBeadIDTown(),
			Title: "Dog Role",
			Desc:  "Role definition for Dog agents. Encampment-level workers for cross-warband tasks.",
		},
		{
			ID:    WitnessRoleBeadIDTown(),
			Title: "Witness Role",
			Desc:  "Role definition for Witness agents. Per-warband worker monitor with progressive nudging.",
		},
		{
			ID:    ForgeRoleBeadIDTown(),
			Title: "Forge Role",
			Desc:  "Role definition for Forge agents. Merge queue processor with verification gates.",
		},
		{
			ID:    RaiderRoleBeadIDTown(),
			Title: "Raider Role",
			Desc:  "Role definition for Raider agents. Ephemeral workers for batch work dispatch.",
		},
		{
			ID:    CrewRoleBeadIDTown(),
			Title: "Clan Role",
			Desc:  "Role definition for Clan agents. Persistent user-managed workspaces.",
		},
	}
}
