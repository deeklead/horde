package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deeklead/horde/internal/workspace"
)

// findMailWorkDir returns the encampment root for all drums operations.
//
// Two-level relics architecture:
// - Encampment relics (~/horde/.relics/): ALL drums and coordination
// - Clone relics (<warband>/clan/*/.relics/): Project issues only
//
// Drums ALWAYS uses encampment relics, regardless of sender or recipient address.
// This ensures messages are visible to all agents in the encampment.
func findMailWorkDir() (string, error) {
	return workspace.FindFromCwdOrError()
}

// findLocalRelicsDir finds the nearest .relics directory by walking up from CWD.
// Used for project work (totems, issue creation) that uses clone relics.
//
// Priority:
//  1. RELICS_DIR environment variable (set by session manager for raiders)
//  2. Walk up from CWD looking for .relics directory
//
// Raiders use redirect-based relics access, so their worktree doesn't have a full
// .relics directory. The session manager sets RELICS_DIR to the correct location.
func findLocalRelicsDir() (string, error) {
	// Check RELICS_DIR environment variable first (set by session manager for raiders).
	// This is important for raiders that use redirect-based relics access.
	if relicsDir := os.Getenv("RELICS_DIR"); relicsDir != "" {
		// RELICS_DIR points directly to the .relics directory, return its parent
		if _, err := os.Stat(relicsDir); err == nil {
			return filepath.Dir(relicsDir), nil
		}
	}

	// Fallback: walk up from CWD
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	path := cwd
	for {
		if _, err := os.Stat(filepath.Join(path, ".relics")); err == nil {
			return path, nil
		}

		parent := filepath.Dir(path)
		if parent == path {
			break // Reached root
		}
		path = parent
	}

	return "", fmt.Errorf("no .relics directory found")
}

// detectSender determines the current context's address.
// Priority:
//  1. HD_ROLE env var → use the role-based identity (agent session)
//  2. No HD_ROLE → try cwd-based detection (witness/forge/raider/clan directories)
//  3. No match → return "overseer" (human at terminal)
//
// All Horde agents run in tmux sessions with HD_ROLE set at muster.
// However, cwd-based detection is also tried to support running commands
// from agent directories without HD_ROLE set (e.g., debugging sessions).
func detectSender() string {
	// Check HD_ROLE first (authoritative for agent sessions)
	role := os.Getenv("HD_ROLE")
	if role != "" {
		// Agent session - build address from role and context
		return detectSenderFromRole(role)
	}

	// No HD_ROLE - try cwd-based detection, defaults to overseer if not in agent directory
	return detectSenderFromCwd()
}

// detectSenderFromRole builds an address from the HD_ROLE and related env vars.
// HD_ROLE can be either a simple role name ("clan", "raider") or a full address
// ("greenplace/clan/joe") depending on how the session was started.
//
// If HD_ROLE is a simple name but required env vars (HD_WARBAND, HD_RAIDER, etc.)
// are missing, falls back to cwd-based detection. This could return "overseer"
// if cwd doesn't match any known agent path - a misconfigured agent session.
func detectSenderFromRole(role string) string {
	warband := os.Getenv("HD_WARBAND")

	// Check if role is already a full address (contains /)
	if strings.Contains(role, "/") {
		// HD_ROLE is already a full address, use it directly
		return role
	}

	// HD_ROLE is a simple role name, build the full address
	switch role {
	case "warchief":
		return "warchief/"
	case "shaman":
		return "shaman/"
	case "raider":
		raider := os.Getenv("HD_RAIDER")
		if warband != "" && raider != "" {
			return fmt.Sprintf("%s/%s", warband, raider)
		}
		// Fallback to cwd detection for raiders
		return detectSenderFromCwd()
	case "clan":
		clan := os.Getenv("HD_CLAN")
		if warband != "" && clan != "" {
			return fmt.Sprintf("%s/clan/%s", warband, clan)
		}
		// Fallback to cwd detection for clan
		return detectSenderFromCwd()
	case "witness":
		if warband != "" {
			return fmt.Sprintf("%s/witness", warband)
		}
		return detectSenderFromCwd()
	case "forge":
		if warband != "" {
			return fmt.Sprintf("%s/forge", warband)
		}
		return detectSenderFromCwd()
	default:
		// Unknown role, try cwd detection
		return detectSenderFromCwd()
	}
}

// detectSenderFromCwd is the legacy cwd-based detection for edge cases.
func detectSenderFromCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "overseer"
	}

	// If in a warband's raiders directory, extract address (format: warband/raiders/name)
	if strings.Contains(cwd, "/raiders/") {
		parts := strings.Split(cwd, "/raiders/")
		if len(parts) >= 2 {
			rigPath := parts[0]
			raiderPath := strings.Split(parts[1], "/")[0]
			rigName := filepath.Base(rigPath)
			return fmt.Sprintf("%s/raiders/%s", rigName, raiderPath)
		}
	}

	// If in a warband's clan directory, extract address (format: warband/clan/name)
	if strings.Contains(cwd, "/clan/") {
		parts := strings.Split(cwd, "/clan/")
		if len(parts) >= 2 {
			rigPath := parts[0]
			crewName := strings.Split(parts[1], "/")[0]
			rigName := filepath.Base(rigPath)
			return fmt.Sprintf("%s/clan/%s", rigName, crewName)
		}
	}

	// If in a warband's forge directory, extract address (format: warband/forge)
	if strings.Contains(cwd, "/forge") {
		parts := strings.Split(cwd, "/forge")
		if len(parts) >= 1 {
			rigName := filepath.Base(parts[0])
			return fmt.Sprintf("%s/forge", rigName)
		}
	}

	// If in a warband's witness directory, extract address (format: warband/witness)
	if strings.Contains(cwd, "/witness") {
		parts := strings.Split(cwd, "/witness")
		if len(parts) >= 1 {
			rigName := filepath.Base(parts[0])
			return fmt.Sprintf("%s/witness", rigName)
		}
	}

	// Default to overseer (human)
	return "overseer"
}
