// Package session provides raider session lifecycle management.
package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Prefix is the common prefix for warband-level Horde tmux sessions.
const Prefix = "hd-"

// HQPrefix is the prefix for encampment-level services (Warchief, Shaman).
const HQPrefix = "hq-"

// WarchiefSessionName returns the session name for the Warchief agent.
// One warchief per machine - multi-encampment requires containers/VMs for isolation.
func WarchiefSessionName() string {
	return HQPrefix + "warchief"
}

// ShamanSessionName returns the session name for the Shaman agent.
// One shaman per machine - multi-encampment requires containers/VMs for isolation.
func ShamanSessionName() string {
	return HQPrefix + "shaman"
}

// WitnessSessionName returns the session name for a warband's Witness agent.
func WitnessSessionName(warband string) string {
	return fmt.Sprintf("%s%s-witness", Prefix, warband)
}

// ForgeSessionName returns the session name for a warband's Forge agent.
func ForgeSessionName(warband string) string {
	return fmt.Sprintf("%s%s-forge", Prefix, warband)
}

// CrewSessionName returns the session name for a clan worker in a warband.
func CrewSessionName(warband, name string) string {
	return fmt.Sprintf("%s%s-clan-%s", Prefix, warband, name)
}

// RaiderSessionName returns the session name for a raider in a warband.
func RaiderSessionName(warband, name string) string {
	return fmt.Sprintf("%s%s-%s", Prefix, warband, name)
}

// PropulsionNudge generates the GUPP (Horde Universal Propulsion Principle) signal.
// This is sent after the beacon to trigger autonomous work execution.
// The agent receives this as user input, triggering the propulsion principle:
// "If work is on your hook, YOU RUN IT."
func PropulsionNudge() string {
	return "Run `hd hook` to check your hook and begin work."
}

// PropulsionNudgeForRole generates a role-specific GUPP signal.
// Different roles have different startup flows:
// - raider/clan: Check hook for charged work
// - witness/forge: Start scout cycle
// - shaman: Start heartbeat scout
// - warchief: Check drums for coordination work
//
// The workDir parameter is used to locate .runtime/session_id for including
// session ID in the message (for Claude Code /resume picker discovery).
func PropulsionNudgeForRole(role, workDir string) string {
	var msg string
	switch role {
	case "raider", "clan":
		msg = PropulsionNudge()
	case "witness":
		msg = "Run `hd rally` to check scout status and begin work."
	case "forge":
		msg = "Run `hd rally` to check MQ status and begin scout."
	case "shaman":
		msg = "Run `hd rally` to check scout status and begin heartbeat cycle."
	case "warchief":
		msg = "Run `hd rally` to check drums and begin coordination."
	default:
		msg = PropulsionNudge()
	}

	// Append session ID if available (for /resume picker visibility)
	if sessionID := readSessionID(workDir); sessionID != "" {
		msg = fmt.Sprintf("%s [session:%s]", msg, sessionID)
	}
	return msg
}

// readSessionID reads the session ID from .runtime/session_id if it exists.
// Returns empty string if the file doesn't exist or can't be read.
func readSessionID(workDir string) string {
	if workDir == "" {
		return ""
	}
	sessionPath := filepath.Join(workDir, ".runtime", "session_id")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
