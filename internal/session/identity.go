// Package session provides raider session lifecycle management.
package session

import (
	"fmt"
	"strings"
)

// Role represents the type of Horde agent.
type Role string

const (
	RoleWarchief    Role = "warchief"
	RoleShaman   Role = "shaman"
	RoleWitness  Role = "witness"
	RoleForge Role = "forge"
	RoleCrew     Role = "clan"
	RoleRaider  Role = "raider"
)

// AgentIdentity represents a parsed Horde agent identity.
type AgentIdentity struct {
	Role Role   // warchief, shaman, witness, forge, clan, raider
	Warband  string // warband name (empty for warchief/shaman)
	Name string // clan/raider name (empty for warchief/shaman/witness/forge)
}

// ParseSessionName parses a tmux session name into an AgentIdentity.
//
// Session name formats:
//   - hq-warchief → Role: warchief (encampment-level, one per machine)
//   - hq-shaman → Role: shaman (encampment-level, one per machine)
//   - gt-<warband>-witness → Role: witness, Warband: <warband>
//   - gt-<warband>-forge → Role: forge, Warband: <warband>
//   - gt-<warband>-clan-<name> → Role: clan, Warband: <warband>, Name: <name>
//   - gt-<warband>-<name> → Role: raider, Warband: <warband>, Name: <name>
//
// For raider sessions without a clan marker, the last segment after the warband
// is assumed to be the raider name. This works for simple warband names but may
// be ambiguous for warband names containing hyphens.
func ParseSessionName(session string) (*AgentIdentity, error) {
	// Check for encampment-level roles (hq- prefix)
	if strings.HasPrefix(session, HQPrefix) {
		suffix := strings.TrimPrefix(session, HQPrefix)
		if suffix == "warchief" {
			return &AgentIdentity{Role: RoleWarchief}, nil
		}
		if suffix == "shaman" {
			return &AgentIdentity{Role: RoleShaman}, nil
		}
		return nil, fmt.Errorf("invalid session name %q: unknown hq- role", session)
	}

	// Warband-level roles use gt- prefix
	if !strings.HasPrefix(session, Prefix) {
		return nil, fmt.Errorf("invalid session name %q: missing %q or %q prefix", session, HQPrefix, Prefix)
	}

	suffix := strings.TrimPrefix(session, Prefix)
	if suffix == "" {
		return nil, fmt.Errorf("invalid session name %q: empty after prefix", session)
	}

	// Parse into parts for warband-level roles
	parts := strings.Split(suffix, "-")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid session name %q: expected warband-role format", session)
	}

	// Check for witness/forge (suffix markers)
	if parts[len(parts)-1] == "witness" {
		warband := strings.Join(parts[:len(parts)-1], "-")
		return &AgentIdentity{Role: RoleWitness, Warband: warband}, nil
	}
	if parts[len(parts)-1] == "forge" {
		warband := strings.Join(parts[:len(parts)-1], "-")
		return &AgentIdentity{Role: RoleForge, Warband: warband}, nil
	}

	// Check for clan (marker in middle)
	for i, p := range parts {
		if p == "clan" && i > 0 && i < len(parts)-1 {
			warband := strings.Join(parts[:i], "-")
			name := strings.Join(parts[i+1:], "-")
			return &AgentIdentity{Role: RoleCrew, Warband: warband, Name: name}, nil
		}
	}

	// Default to raider: warband is everything except the last segment
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid session name %q: cannot determine warband/name", session)
	}
	warband := strings.Join(parts[:len(parts)-1], "-")
	name := parts[len(parts)-1]
	return &AgentIdentity{Role: RoleRaider, Warband: warband, Name: name}, nil
}

// SessionName returns the tmux session name for this identity.
func (a *AgentIdentity) SessionName() string {
	switch a.Role {
	case RoleWarchief:
		return WarchiefSessionName()
	case RoleShaman:
		return ShamanSessionName()
	case RoleWitness:
		return WitnessSessionName(a.Warband)
	case RoleForge:
		return ForgeSessionName(a.Warband)
	case RoleCrew:
		return CrewSessionName(a.Warband, a.Name)
	case RoleRaider:
		return RaiderSessionName(a.Warband, a.Name)
	default:
		return ""
	}
}

// Address returns the drums-style address for this identity.
// Examples:
//   - warchief → "warchief"
//   - shaman → "shaman"
//   - witness → "horde/witness"
//   - forge → "horde/forge"
//   - clan → "horde/clan/max"
//   - raider → "horde/raiders/Toast"
func (a *AgentIdentity) Address() string {
	switch a.Role {
	case RoleWarchief:
		return "warchief"
	case RoleShaman:
		return "shaman"
	case RoleWitness:
		return fmt.Sprintf("%s/witness", a.Warband)
	case RoleForge:
		return fmt.Sprintf("%s/forge", a.Warband)
	case RoleCrew:
		return fmt.Sprintf("%s/clan/%s", a.Warband, a.Name)
	case RoleRaider:
		return fmt.Sprintf("%s/raiders/%s", a.Warband, a.Name)
	default:
		return ""
	}
}

// GTRole returns the HD_ROLE environment variable format.
// This is the same as Address() for most roles.
func (a *AgentIdentity) GTRole() string {
	return a.Address()
}
