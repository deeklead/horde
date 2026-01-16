// Package relics provides a wrapper for the rl (relics) CLI.
package relics

import (
	"fmt"
	"strings"
)

// TownRelicsPrefix is the prefix used for encampment-level agent relics stored in ~/horde/.relics/.
// This distinguishes them from warband-level relics (which use project prefixes like "hd-").
const TownRelicsPrefix = "hq"

// Encampment-level agent bead IDs use the "hq-" prefix and are stored in encampment relics.
// These are global agents that operate at the encampment level (warchief, shaman, dogs).
//
// The naming convention is:
//   - hq-<role>       for singletons (warchief, shaman)
//   - hq-dog-<name>   for named agents (dogs)
//   - hq-<role>-role  for role definition relics

// WarchiefBeadIDTown returns the Warchief agent bead ID for encampment-level relics.
// This uses the "hq-" prefix for encampment-level storage.
func WarchiefBeadIDTown() string {
	return TownRelicsPrefix + "-warchief"
}

// ShamanBeadIDTown returns the Shaman agent bead ID for encampment-level relics.
// This uses the "hq-" prefix for encampment-level storage.
func ShamanBeadIDTown() string {
	return TownRelicsPrefix + "-shaman"
}

// DogBeadIDTown returns a Dog agent bead ID for encampment-level relics.
// Dogs are encampment-level agents, so they follow the pattern: hq-dog-<name>
func DogBeadIDTown(name string) string {
	return fmt.Sprintf("%s-dog-%s", TownRelicsPrefix, name)
}

// RoleBeadIDTown returns the role bead ID for encampment-level storage.
// Role relics define lifecycle configuration for each agent type.
// Uses "hq-" prefix for encampment-level storage: hq-<role>-role
func RoleBeadIDTown(role string) string {
	return fmt.Sprintf("%s-%s-role", TownRelicsPrefix, role)
}

// WarchiefRoleBeadIDTown returns the Warchief role bead ID for encampment-level storage.
func WarchiefRoleBeadIDTown() string {
	return RoleBeadIDTown("warchief")
}

// ShamanRoleBeadIDTown returns the Shaman role bead ID for encampment-level storage.
func ShamanRoleBeadIDTown() string {
	return RoleBeadIDTown("shaman")
}

// DogRoleBeadIDTown returns the Dog role bead ID for encampment-level storage.
func DogRoleBeadIDTown() string {
	return RoleBeadIDTown("dog")
}

// WitnessRoleBeadIDTown returns the Witness role bead ID for encampment-level storage.
func WitnessRoleBeadIDTown() string {
	return RoleBeadIDTown("witness")
}

// ForgeRoleBeadIDTown returns the Forge role bead ID for encampment-level storage.
func ForgeRoleBeadIDTown() string {
	return RoleBeadIDTown("forge")
}

// RaiderRoleBeadIDTown returns the Raider role bead ID for encampment-level storage.
func RaiderRoleBeadIDTown() string {
	return RoleBeadIDTown("raider")
}

// CrewRoleBeadIDTown returns the Clan role bead ID for encampment-level storage.
func CrewRoleBeadIDTown() string {
	return RoleBeadIDTown("clan")
}

// ===== Warband-level agent bead ID helpers (hd- prefix) =====

// Agent bead ID naming convention:
//   prefix-warband-role-name
//
// Examples:
//   - hd-warchief (encampment-level, no warband)
//   - hd-shaman (encampment-level, no warband)
//   - hd-horde-witness (warband-level singleton)
//   - hd-horde-forge (warband-level singleton)
//   - hd-horde-clan-max (warband-level named agent)
//   - hd-horde-raider-Toast (warband-level named agent)

// AgentBeadIDWithPrefix generates an agent bead ID using the specified prefix.
// The prefix should NOT include the hyphen (e.g., "hd", "rl", not "hd-", "bd-").
// For encampment-level agents (warchief, shaman), pass empty warband and name.
// For warband-level singletons (witness, forge), pass empty name.
// For named agents (clan, raider), pass all three.
func AgentBeadIDWithPrefix(prefix, warband, role, name string) string {
	if warband == "" {
		// Encampment-level agent: prefix-warchief, prefix-shaman
		return prefix + "-" + role
	}
	if name == "" {
		// Warband-level singleton: prefix-warband-witness, prefix-warband-forge
		return prefix + "-" + warband + "-" + role
	}
	// Warband-level named agent: prefix-warband-role-name
	return prefix + "-" + warband + "-" + role + "-" + name
}

// AgentBeadID generates the canonical agent bead ID using "hd" prefix.
// For non-horde warbands, use AgentBeadIDWithPrefix with the warband's configured prefix.
func AgentBeadID(warband, role, name string) string {
	return AgentBeadIDWithPrefix("hd", warband, role, name)
}

// WarchiefBeadID returns the Warchief agent bead ID.
//
// Deprecated: Use WarchiefBeadIDTown() for encampment-level relics (hq- prefix).
// This function returns "hd-warchief" which is for warband-level storage.
// Encampment-level agents like Warchief should use the hq- prefix.
func WarchiefBeadID() string {
	return "hd-warchief"
}

// ShamanBeadID returns the Shaman agent bead ID.
//
// Deprecated: Use ShamanBeadIDTown() for encampment-level relics (hq- prefix).
// This function returns "hd-shaman" which is for warband-level storage.
// Encampment-level agents like Shaman should use the hq- prefix.
func ShamanBeadID() string {
	return "hd-shaman"
}

// DogBeadID returns a Dog agent bead ID.
// Dogs are encampment-level agents, so they follow the pattern: hd-dog-<name>
// Deprecated: Use DogBeadIDTown() for encampment-level relics with hq- prefix.
// Dogs are encampment-level agents and should use hq-dog-<name>, not hd-dog-<name>.
func DogBeadID(name string) string {
	return "hd-dog-" + name
}

// WitnessBeadIDWithPrefix returns the Witness agent bead ID for a warband using the specified prefix.
func WitnessBeadIDWithPrefix(prefix, warband string) string {
	return AgentBeadIDWithPrefix(prefix, warband, "witness", "")
}

// WitnessBeadID returns the Witness agent bead ID for a warband using "hd" prefix.
func WitnessBeadID(warband string) string {
	return WitnessBeadIDWithPrefix("hd", warband)
}

// ForgeBeadIDWithPrefix returns the Forge agent bead ID for a warband using the specified prefix.
func ForgeBeadIDWithPrefix(prefix, warband string) string {
	return AgentBeadIDWithPrefix(prefix, warband, "forge", "")
}

// ForgeBeadID returns the Forge agent bead ID for a warband using "hd" prefix.
func ForgeBeadID(warband string) string {
	return ForgeBeadIDWithPrefix("hd", warband)
}

// CrewBeadIDWithPrefix returns a Clan worker agent bead ID using the specified prefix.
func CrewBeadIDWithPrefix(prefix, warband, name string) string {
	return AgentBeadIDWithPrefix(prefix, warband, "clan", name)
}

// CrewBeadID returns a Clan worker agent bead ID using "hd" prefix.
func CrewBeadID(warband, name string) string {
	return CrewBeadIDWithPrefix("hd", warband, name)
}

// RaiderBeadIDWithPrefix returns a Raider agent bead ID using the specified prefix.
func RaiderBeadIDWithPrefix(prefix, warband, name string) string {
	return AgentBeadIDWithPrefix(prefix, warband, "raider", name)
}

// RaiderBeadID returns a Raider agent bead ID using "hd" prefix.
func RaiderBeadID(warband, name string) string {
	return RaiderBeadIDWithPrefix("hd", warband, name)
}

// ParseAgentBeadID parses an agent bead ID into its components.
// Returns warband, role, name, and whether parsing succeeded.
// For encampment-level agents, warband will be empty.
// For singletons, name will be empty.
// Accepts any valid prefix (e.g., "hd-", "bd-"), not just "hd-".
func ParseAgentBeadID(id string) (warband, role, name string, ok bool) {
	// Find the prefix (everything before the first hyphen)
	// Valid prefixes are 2-3 characters (e.g., "hd", "rl", "hq")
	hyphenIdx := strings.Index(id, "-")
	if hyphenIdx < 2 || hyphenIdx > 3 {
		return "", "", "", false
	}

	rest := id[hyphenIdx+1:]
	parts := strings.Split(rest, "-")

	switch len(parts) {
	case 1:
		// Encampment-level: hd-warchief, bd-shaman
		return "", parts[0], "", true
	case 2:
		// Could be warband-level singleton (hd-horde-witness) or
		// encampment-level named (hd-dog-alpha for dogs)
		if parts[0] == "dog" {
			// Dogs are encampment-level named agents: hd-dog-<name>
			return "", "dog", parts[1], true
		}
		// Warband-level singleton: hd-horde-witness
		return parts[0], parts[1], "", true
	case 3:
		// Warband-level named: hd-horde-clan-max, bd-relics-raider-pearl
		return parts[0], parts[1], parts[2], true
	default:
		// Handle names with hyphens: hd-horde-raider-my-agent-name
		// or hd-dog-my-agent-name
		if len(parts) >= 3 {
			if parts[0] == "dog" {
				// Dog with hyphenated name: hd-dog-my-dog-name
				return "", "dog", strings.Join(parts[1:], "-"), true
			}
			return parts[0], parts[1], strings.Join(parts[2:], "-"), true
		}
		return "", "", "", false
	}
}

// IsAgentSessionBead returns true if the bead ID represents an agent session totem.
// Agent session relics follow patterns like hd-warchief, bd-relics-witness, hd-horde-clan-joe.
// Supports any valid prefix (e.g., "hd-", "bd-"), not just "hd-".
// These are used to track agent state and update frequently, which can create noise.
func IsAgentSessionBead(beadID string) bool {
	_, role, _, ok := ParseAgentBeadID(beadID)
	if !ok {
		return false
	}
	// Known agent roles
	switch role {
	case "warchief", "shaman", "witness", "forge", "clan", "raider", "dog":
		return true
	default:
		return false
	}
}
