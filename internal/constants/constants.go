// Package constants defines shared constant values used throughout Horde.
// Centralizing these magic strings improves maintainability and consistency.
package constants

import "time"

// Timing constants for session management and tmux operations.
const (
	// ShutdownNotifyDelay is the pause after sending shutdown notification.
	ShutdownNotifyDelay = 500 * time.Millisecond

	// ClaudeStartTimeout is how long to wait for Claude to start in a session.
	// Increased to 60s because Claude can take 30s+ on slower machines.
	ClaudeStartTimeout = 60 * time.Second

	// ShellReadyTimeout is how long to wait for shell prompt after command.
	ShellReadyTimeout = 5 * time.Second

	// DefaultDebounceMs is the default debounce for SendKeys operations.
	// 500ms is required for Claude Code to reliably process paste before Enter.
	// See SignalSession comment: "Wait 500ms for paste to complete (tested, required)"
	DefaultDebounceMs = 500

	// DefaultDisplayMs is the default duration for tmux display-message.
	DefaultDisplayMs = 5000

	// PollInterval is the default polling interval for wait loops.
	PollInterval = 100 * time.Millisecond
)

// Directory names within a Horde workspace.
const (
	// DirWarchief is the directory containing warchief configuration and state.
	DirWarchief = "warchief"

	// DirRaiders is the directory containing raider worktrees.
	DirRaiders = "raiders"

	// DirCrew is the directory containing clan workspaces.
	DirCrew = "clan"

	// DirForge is the directory containing the forge clone.
	DirForge = "forge"

	// DirWitness is the directory containing witness state.
	DirWitness = "witness"

	// DirRig is the subdirectory containing the actual git clone.
	DirRig = "warband"

	// DirRelics is the relics database directory.
	DirRelics = ".relics"

	// DirRuntime is the runtime state directory (gitignored).
	DirRuntime = ".runtime"

	// DirSettings is the warband settings directory (git-tracked).
	DirSettings = "settings"
)

// File names for configuration and state.
const (
	// FileRigsJSON is the warband registry file in warchief/.
	FileRigsJSON = "warbands.json"

	// FileTownJSON is the encampment configuration file in warchief/.
	FileTownJSON = "encampment.json"

	// FileConfigJSON is the general config file.
	FileConfigJSON = "config.json"

	// FileAccountsJSON is the accounts configuration file in warchief/.
	FileAccountsJSON = "accounts.json"

	// FileHandoffMarker is the marker file indicating a handoff just occurred.
	// Written by hd handoff before respawn, cleared by hd rally after detection.
	// This prevents the handoff loop bug where agents re-run /handoff from context.
	FileHandoffMarker = "handoff_to_successor"
)

// Relics configuration constants.
const (
	// RelicsCustomTypes is the comma-separated list of custom issue types that
	// Horde registers with relics. These types were extracted from relics core
	// in v0.46.0 and now require explicit configuration.
	RelicsCustomTypes = "agent,role,warband,raid,slot,queue"
)

// RelicsCustomTypesList returns the custom types as a slice.
func RelicsCustomTypesList() []string {
	return []string{"agent", "role", "warband", "raid", "slot", "queue"}
}

// Git branch names.
const (
	// BranchMain is the default main branch name.
	BranchMain = "main"

	// BranchRelicsSync is the branch used for relics synchronization.
	BranchRelicsSync = "relics-sync"

	// BranchRaiderPrefix is the prefix for raider work branches.
	BranchRaiderPrefix = "raider/"

	// BranchIntegrationPrefix is the prefix for integration branches.
	BranchIntegrationPrefix = "integration/"
)

// Tmux session names.
// Warchief and Shaman use hq- prefix: hq-warchief, hq-shaman (encampment-level, one per machine).
// Warband-level services use hd- prefix: hd-<warband>-witness, hd-<warband>-forge, etc.
// Use session.WarchiefSessionName() and session.ShamanSessionName().
const (
	// SessionPrefix is the prefix for warband-level Horde tmux sessions.
	SessionPrefix = "hd-"

	// HQSessionPrefix is the prefix for encampment-level services (Warchief, Shaman).
	HQSessionPrefix = "hq-"
)

// Agent role names.
const (
	// RoleWarchief is the warchief agent role.
	RoleWarchief = "warchief"

	// RoleWitness is the witness agent role.
	RoleWitness = "witness"

	// RoleForge is the forge agent role.
	RoleForge = "forge"

	// RoleRaider is the raider agent role.
	RoleRaider = "raider"

	// RoleCrew is the clan agent role.
	RoleCrew = "clan"

	// RoleShaman is the shaman agent role.
	RoleShaman = "shaman"
)

// Role emojis - centralized for easy customization.
// These match the Horde visual identity (see ~/Desktop/Horde/ prompts).
const (
	// EmojiWarchief is the warchief emoji (fox conductor).
	EmojiWarchief = "🎩"

	// EmojiShaman is the shaman emoji (wolf in the engine room).
	EmojiShaman = "🐺"

	// EmojiWitness is the witness emoji (watchful owl).
	EmojiWitness = "🦉"

	// EmojiForge is the forge emoji (industrial).
	EmojiForge = "🏭"

	// EmojiCrew is the clan emoji (established worker).
	EmojiCrew = "👷"

	// EmojiRaider is the raider emoji (transient worker).
	EmojiRaider = "😺"
)

// RoleEmoji returns the emoji for a given role name.
func RoleEmoji(role string) string {
	switch role {
	case RoleWarchief:
		return EmojiWarchief
	case RoleShaman:
		return EmojiShaman
	case RoleWitness:
		return EmojiWitness
	case RoleForge:
		return EmojiForge
	case RoleCrew:
		return EmojiCrew
	case RoleRaider:
		return EmojiRaider
	default:
		return "❓"
	}
}

// SupportedShells lists shell binaries that Horde can detect and work with.
// Used to identify if a tmux pane is at a shell prompt vs running a command.
var SupportedShells = []string{"bash", "zsh", "sh", "fish", "tcsh", "ksh"}

// Path helpers construct common paths.

// WarchiefRigsPath returns the path to warbands.json within a encampment root.
func WarchiefRigsPath(townRoot string) string {
	return townRoot + "/" + DirWarchief + "/" + FileRigsJSON
}

// WarchiefTownPath returns the path to encampment.json within a encampment root.
func WarchiefTownPath(townRoot string) string {
	return townRoot + "/" + DirWarchief + "/" + FileTownJSON
}

// RigWarchiefPath returns the path to warchief/warband within a warband.
func RigWarchiefPath(rigPath string) string {
	return rigPath + "/" + DirWarchief + "/" + DirRig
}

// RigRelicsPath returns the path to warchief/warband/.relics within a warband.
func RigRelicsPath(rigPath string) string {
	return rigPath + "/" + DirWarchief + "/" + DirRig + "/" + DirRelics
}

// RigRaidersPath returns the path to raiders/ within a warband.
func RigRaidersPath(rigPath string) string {
	return rigPath + "/" + DirRaiders
}

// RigCrewPath returns the path to clan/ within a warband.
func RigCrewPath(rigPath string) string {
	return rigPath + "/" + DirCrew
}

// WarchiefConfigPath returns the path to warchief/config.json within a encampment root.
func WarchiefConfigPath(townRoot string) string {
	return townRoot + "/" + DirWarchief + "/" + FileConfigJSON
}

// TownRuntimePath returns the path to .runtime/ at the encampment root.
func TownRuntimePath(townRoot string) string {
	return townRoot + "/" + DirRuntime
}

// RigRuntimePath returns the path to .runtime/ within a warband.
func RigRuntimePath(rigPath string) string {
	return rigPath + "/" + DirRuntime
}

// RigSettingsPath returns the path to settings/ within a warband.
func RigSettingsPath(rigPath string) string {
	return rigPath + "/" + DirSettings
}

// WarchiefAccountsPath returns the path to warchief/accounts.json within a encampment root.
func WarchiefAccountsPath(townRoot string) string {
	return townRoot + "/" + DirWarchief + "/" + FileAccountsJSON
}
