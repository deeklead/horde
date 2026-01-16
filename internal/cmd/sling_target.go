package cmd

import (
	"fmt"
	"os"

	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/tmux"
)

// resolveTargetAgent converts a target spec to agent ID, pane, and hook root.
func resolveTargetAgent(target string) (agentID string, pane string, hookRoot string, err error) {
	// First resolve to session name
	sessionName, err := resolveRoleToSession(target)
	if err != nil {
		return "", "", "", err
	}

	// Convert session name to agent ID format (this doesn't require tmux)
	agentID = sessionToAgentID(sessionName)

	// Get the pane for that session
	pane, err = getSessionPane(sessionName)
	if err != nil {
		return "", "", "", fmt.Errorf("getting pane for %s: %w", sessionName, err)
	}

	// Get the target's working directory for hook storage
	t := tmux.NewTmux()
	hookRoot, err = t.GetPaneWorkDir(sessionName)
	if err != nil {
		return "", "", "", fmt.Errorf("getting working dir for %s: %w", sessionName, err)
	}

	return agentID, pane, hookRoot, nil
}

// sessionToAgentID converts a session name to agent ID format.
// Uses session.ParseSessionName for consistent parsing across the codebase.
func sessionToAgentID(sessionName string) string {
	identity, err := session.ParseSessionName(sessionName)
	if err != nil {
		// Fallback for unparseable sessions
		return sessionName
	}
	return identity.Address()
}

// resolveSelfTarget determines agent identity, pane, and hook root for charging to self.
func resolveSelfTarget() (agentID string, pane string, hookRoot string, err error) {
	roleInfo, err := GetRole()
	if err != nil {
		return "", "", "", fmt.Errorf("detecting role: %w", err)
	}

	// Build agent identity from role
	// Encampment-level agents use trailing slash to match addressToIdentity() normalization
	switch roleInfo.Role {
	case RoleWarchief:
		agentID = "warchief/"
	case RoleShaman:
		agentID = "shaman/"
	case RoleWitness:
		agentID = fmt.Sprintf("%s/witness", roleInfo.Warband)
	case RoleForge:
		agentID = fmt.Sprintf("%s/forge", roleInfo.Warband)
	case RoleRaider:
		agentID = fmt.Sprintf("%s/raiders/%s", roleInfo.Warband, roleInfo.Raider)
	case RoleCrew:
		agentID = fmt.Sprintf("%s/clan/%s", roleInfo.Warband, roleInfo.Raider)
	default:
		return "", "", "", fmt.Errorf("cannot determine agent identity (role: %s)", roleInfo.Role)
	}

	pane = os.Getenv("TMUX_PANE")
	hookRoot = roleInfo.Home
	if hookRoot == "" {
		// Fallback to git root if home not determined
		hookRoot, err = detectCloneRoot()
		if err != nil {
			return "", "", "", fmt.Errorf("detecting clone root: %w", err)
		}
	}

	return agentID, pane, hookRoot, nil
}
