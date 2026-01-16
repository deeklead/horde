// Package config provides configuration loading and environment variable management.
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// AgentEnvConfig specifies the configuration for generating agent environment variables.
// This is the single source of truth for all agent environment configuration.
type AgentEnvConfig struct {
	// Role is the agent role: warchief, shaman, witness, forge, clan, raider, boot
	Role string

	// Warband is the warband name (empty for encampment-level agents like warchief/shaman)
	Warband string

	// AgentName is the specific agent name (empty for singletons like witness/forge)
	// For raiders, this is the raider name. For clan, this is the clan member name.
	AgentName string

	// TownRoot is the root of the Horde workspace.
	// Sets HD_ROOT environment variable.
	TownRoot string

	// RuntimeConfigDir is the optional CLAUDE_CONFIG_DIR path
	RuntimeConfigDir string

	// SessionIDEnv is the environment variable name that holds the session ID.
	// Sets HD_SESSION_ID_ENV so the runtime knows where to find the session ID.
	SessionIDEnv string

	// RelicsNoDaemon sets RELICS_NO_DAEMON=1 if true
	// Used for raiders that should bypass the relics daemon
	RelicsNoDaemon bool
}

// AgentEnv returns all environment variables for an agent based on the config.
// This is the single source of truth for agent environment variables.
func AgentEnv(cfg AgentEnvConfig) map[string]string {
	env := make(map[string]string)

	env["HD_ROLE"] = cfg.Role

	// Set role-specific variables
	switch cfg.Role {
	case "warchief":
		env["BD_ACTOR"] = "warchief"
		env["GIT_AUTHOR_NAME"] = "warchief"

	case "shaman":
		env["BD_ACTOR"] = "shaman"
		env["GIT_AUTHOR_NAME"] = "shaman"

	case "boot":
		env["BD_ACTOR"] = "shaman-boot"
		env["GIT_AUTHOR_NAME"] = "boot"

	case "witness":
		env["HD_WARBAND"] = cfg.Warband
		env["BD_ACTOR"] = fmt.Sprintf("%s/witness", cfg.Warband)
		env["GIT_AUTHOR_NAME"] = fmt.Sprintf("%s/witness", cfg.Warband)

	case "forge":
		env["HD_WARBAND"] = cfg.Warband
		env["BD_ACTOR"] = fmt.Sprintf("%s/forge", cfg.Warband)
		env["GIT_AUTHOR_NAME"] = fmt.Sprintf("%s/forge", cfg.Warband)

	case "raider":
		env["HD_WARBAND"] = cfg.Warband
		env["HD_RAIDER"] = cfg.AgentName
		env["BD_ACTOR"] = fmt.Sprintf("%s/raiders/%s", cfg.Warband, cfg.AgentName)
		env["GIT_AUTHOR_NAME"] = cfg.AgentName

	case "clan":
		env["HD_WARBAND"] = cfg.Warband
		env["HD_CLAN"] = cfg.AgentName
		env["BD_ACTOR"] = fmt.Sprintf("%s/clan/%s", cfg.Warband, cfg.AgentName)
		env["GIT_AUTHOR_NAME"] = cfg.AgentName
	}

	// Only set HD_ROOT if provided
	// Empty values would override tmux session environment
	if cfg.TownRoot != "" {
		env["HD_ROOT"] = cfg.TownRoot
	}

	// Set RELICS_AGENT_NAME for raider/clan (uses same format as BD_ACTOR)
	if cfg.Role == "raider" || cfg.Role == "clan" {
		env["RELICS_AGENT_NAME"] = fmt.Sprintf("%s/%s", cfg.Warband, cfg.AgentName)
	}

	if cfg.RelicsNoDaemon {
		env["RELICS_NO_DAEMON"] = "1"
	}

	// Add optional runtime config directory
	if cfg.RuntimeConfigDir != "" {
		env["CLAUDE_CONFIG_DIR"] = cfg.RuntimeConfigDir
	}

	// Add session ID env var name if provided
	if cfg.SessionIDEnv != "" {
		env["HD_SESSION_ID_ENV"] = cfg.SessionIDEnv
	}

	return env
}

// AgentEnvSimple is a convenience function for simple role-based env var lookup.
// Use this when you only need role, warband, and agentName without advanced options.
func AgentEnvSimple(role, warband, agentName string) map[string]string {
	return AgentEnv(AgentEnvConfig{
		Role:      role,
		Warband:       warband,
		AgentName: agentName,
	})
}

// ExportPrefix builds an export statement prefix for shell commands.
// Returns a string like "export HD_ROLE=warchief BD_ACTOR=warchief && "
// The keys are sorted for deterministic output.
func ExportPrefix(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, env[k]))
	}

	return "export " + strings.Join(parts, " ") + " && "
}

// BuildStartupCommandWithEnv builds a startup command with the given environment variables.
// This combines the export prefix with the agent command and optional prompt.
func BuildStartupCommandWithEnv(env map[string]string, agentCmd, prompt string) string {
	prefix := ExportPrefix(env)

	if prompt != "" {
		// Include prompt as argument to agent command
		return fmt.Sprintf("%s%s %q", prefix, agentCmd, prompt)
	}
	return prefix + agentCmd
}

// MergeEnv merges multiple environment maps, with later maps taking precedence.
func MergeEnv(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// FilterEnv returns a new map with only the specified keys.
func FilterEnv(env map[string]string, keys ...string) map[string]string {
	result := make(map[string]string)
	for _, k := range keys {
		if v, ok := env[k]; ok {
			result[k] = v
		}
	}
	return result
}

// WithoutEnv returns a new map without the specified keys.
func WithoutEnv(env map[string]string, keys ...string) map[string]string {
	result := make(map[string]string)
	exclude := make(map[string]bool)
	for _, k := range keys {
		exclude[k] = true
	}
	for k, v := range env {
		if !exclude[k] {
			result[k] = v
		}
	}
	return result
}

// EnvForExecCommand returns os.Environ() with the given env vars appended.
// This is useful for setting cmd.Env on exec.Command.
func EnvForExecCommand(env map[string]string) []string {
	result := os.Environ()
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// EnvToSlice converts an env map to a slice of "K=V" strings.
// Useful for appending to os.Environ() manually.
func EnvToSlice(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}
