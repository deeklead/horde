package config

import (
	"testing"
)

func TestAgentEnv_Warchief(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:     "warchief",
		TownRoot: "/encampment",
	})

	assertEnv(t, env, "GT_ROLE", "warchief")
	assertEnv(t, env, "BD_ACTOR", "warchief")
	assertEnv(t, env, "GIT_AUTHOR_NAME", "warchief")
	assertEnv(t, env, "GT_ROOT", "/encampment")
	assertNotSet(t, env, "GT_RIG")
	assertNotSet(t, env, "RELICS_NO_DAEMON")
}

func TestAgentEnv_Witness(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:     "witness",
		Warband:      "myrig",
		TownRoot: "/encampment",
	})

	assertEnv(t, env, "GT_ROLE", "witness")
	assertEnv(t, env, "GT_RIG", "myrig")
	assertEnv(t, env, "BD_ACTOR", "myrig/witness")
	assertEnv(t, env, "GIT_AUTHOR_NAME", "myrig/witness")
	assertEnv(t, env, "GT_ROOT", "/encampment")
}

func TestAgentEnv_Raider(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:          "raider",
		Warband:           "myrig",
		AgentName:     "Toast",
		TownRoot:      "/encampment",
		RelicsNoDaemon: true,
	})

	assertEnv(t, env, "GT_ROLE", "raider")
	assertEnv(t, env, "GT_RIG", "myrig")
	assertEnv(t, env, "GT_RAIDER", "Toast")
	assertEnv(t, env, "BD_ACTOR", "myrig/raiders/Toast")
	assertEnv(t, env, "GIT_AUTHOR_NAME", "Toast")
	assertEnv(t, env, "RELICS_AGENT_NAME", "myrig/Toast")
	assertEnv(t, env, "RELICS_NO_DAEMON", "1")
}

func TestAgentEnv_Crew(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:          "clan",
		Warband:           "myrig",
		AgentName:     "emma",
		TownRoot:      "/encampment",
		RelicsNoDaemon: true,
	})

	assertEnv(t, env, "GT_ROLE", "clan")
	assertEnv(t, env, "GT_RIG", "myrig")
	assertEnv(t, env, "GT_CREW", "emma")
	assertEnv(t, env, "BD_ACTOR", "myrig/clan/emma")
	assertEnv(t, env, "GIT_AUTHOR_NAME", "emma")
	assertEnv(t, env, "RELICS_AGENT_NAME", "myrig/emma")
	assertEnv(t, env, "RELICS_NO_DAEMON", "1")
}

func TestAgentEnv_Forge(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:          "forge",
		Warband:           "myrig",
		TownRoot:      "/encampment",
		RelicsNoDaemon: true,
	})

	assertEnv(t, env, "GT_ROLE", "forge")
	assertEnv(t, env, "GT_RIG", "myrig")
	assertEnv(t, env, "BD_ACTOR", "myrig/forge")
	assertEnv(t, env, "GIT_AUTHOR_NAME", "myrig/forge")
	assertEnv(t, env, "RELICS_NO_DAEMON", "1")
}

func TestAgentEnv_Shaman(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:     "shaman",
		TownRoot: "/encampment",
	})

	assertEnv(t, env, "GT_ROLE", "shaman")
	assertEnv(t, env, "BD_ACTOR", "shaman")
	assertEnv(t, env, "GIT_AUTHOR_NAME", "shaman")
	assertEnv(t, env, "GT_ROOT", "/encampment")
	assertNotSet(t, env, "GT_RIG")
	assertNotSet(t, env, "RELICS_NO_DAEMON")
}

func TestAgentEnv_Boot(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:     "boot",
		TownRoot: "/encampment",
	})

	assertEnv(t, env, "GT_ROLE", "boot")
	assertEnv(t, env, "BD_ACTOR", "shaman-boot")
	assertEnv(t, env, "GIT_AUTHOR_NAME", "boot")
	assertEnv(t, env, "GT_ROOT", "/encampment")
	assertNotSet(t, env, "GT_RIG")
	assertNotSet(t, env, "RELICS_NO_DAEMON")
}

func TestAgentEnv_WithRuntimeConfigDir(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:             "raider",
		Warband:              "myrig",
		AgentName:        "Toast",
		TownRoot:         "/encampment",
		RuntimeConfigDir: "/home/user/.config/claude",
	})

	assertEnv(t, env, "CLAUDE_CONFIG_DIR", "/home/user/.config/claude")
}

func TestAgentEnv_WithoutRuntimeConfigDir(t *testing.T) {
	t.Parallel()
	env := AgentEnv(AgentEnvConfig{
		Role:      "raider",
		Warband:       "myrig",
		AgentName: "Toast",
		TownRoot:  "/encampment",
	})

	assertNotSet(t, env, "CLAUDE_CONFIG_DIR")
}

func TestAgentEnvSimple(t *testing.T) {
	t.Parallel()
	env := AgentEnvSimple("raider", "myrig", "Toast")

	assertEnv(t, env, "GT_ROLE", "raider")
	assertEnv(t, env, "GT_RIG", "myrig")
	assertEnv(t, env, "GT_RAIDER", "Toast")
	// Simple doesn't set TownRoot, so key should be absent
	// (not empty string which would override tmux session environment)
	assertNotSet(t, env, "GT_ROOT")
}

func TestAgentEnv_EmptyTownRootOmitted(t *testing.T) {
	t.Parallel()
	// Regression test: empty TownRoot should NOT create keys in the map.
	// If it was set to empty string, ExportPrefix would generate "export GT_ROOT= ..."
	// which overrides tmux session environment where it's correctly set.
	env := AgentEnv(AgentEnvConfig{
		Role:      "raider",
		Warband:       "myrig",
		AgentName: "Toast",
		TownRoot:  "", // explicitly empty
	})

	// Key should be absent, not empty string
	assertNotSet(t, env, "GT_ROOT")

	// Other keys should still be set
	assertEnv(t, env, "GT_ROLE", "raider")
	assertEnv(t, env, "GT_RIG", "myrig")
}

func TestExportPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name:     "empty",
			env:      map[string]string{},
			expected: "",
		},
		{
			name:     "single var",
			env:      map[string]string{"FOO": "bar"},
			expected: "export FOO=bar && ",
		},
		{
			name: "multiple vars sorted",
			env: map[string]string{
				"ZZZ": "last",
				"AAA": "first",
				"MMM": "middle",
			},
			expected: "export AAA=first MMM=middle ZZZ=last && ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExportPrefix(tt.env)
			if result != tt.expected {
				t.Errorf("ExportPrefix() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestBuildStartupCommandWithEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		env      map[string]string
		agentCmd string
		prompt   string
		expected string
	}{
		{
			name:     "no env no prompt",
			env:      map[string]string{},
			agentCmd: "claude",
			prompt:   "",
			expected: "claude",
		},
		{
			name:     "env no prompt",
			env:      map[string]string{"GT_ROLE": "raider"},
			agentCmd: "claude",
			prompt:   "",
			expected: "export GT_ROLE=raider && claude",
		},
		{
			name:     "env with prompt",
			env:      map[string]string{"GT_ROLE": "raider"},
			agentCmd: "claude",
			prompt:   "hd rally",
			expected: `export GT_ROLE=raider && claude "hd rally"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildStartupCommandWithEnv(tt.env, tt.agentCmd, tt.prompt)
			if result != tt.expected {
				t.Errorf("BuildStartupCommandWithEnv() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMergeEnv(t *testing.T) {
	t.Parallel()
	a := map[string]string{"A": "1", "B": "2"}
	b := map[string]string{"B": "override", "C": "3"}

	result := MergeEnv(a, b)

	assertEnv(t, result, "A", "1")
	assertEnv(t, result, "B", "override")
	assertEnv(t, result, "C", "3")
}

func TestFilterEnv(t *testing.T) {
	t.Parallel()
	env := map[string]string{"A": "1", "B": "2", "C": "3"}

	result := FilterEnv(env, "A", "C")

	assertEnv(t, result, "A", "1")
	assertNotSet(t, result, "B")
	assertEnv(t, result, "C", "3")
}

func TestWithoutEnv(t *testing.T) {
	t.Parallel()
	env := map[string]string{"A": "1", "B": "2", "C": "3"}

	result := WithoutEnv(env, "B")

	assertEnv(t, result, "A", "1")
	assertNotSet(t, result, "B")
	assertEnv(t, result, "C", "3")
}

func TestEnvToSlice(t *testing.T) {
	t.Parallel()
	env := map[string]string{"A": "1", "B": "2"}

	result := EnvToSlice(env)

	if len(result) != 2 {
		t.Errorf("EnvToSlice() returned %d items, want 2", len(result))
	}

	// Check both entries exist (order not guaranteed)
	found := make(map[string]bool)
	for _, s := range result {
		found[s] = true
	}
	if !found["A=1"] || !found["B=2"] {
		t.Errorf("EnvToSlice() = %v, want [A=1, B=2]", result)
	}
}

// Helper functions

func assertEnv(t *testing.T, env map[string]string, key, expected string) {
	t.Helper()
	if got := env[key]; got != expected {
		t.Errorf("env[%q] = %q, want %q", key, got, expected)
	}
}

func assertNotSet(t *testing.T, env map[string]string, key string) {
	t.Helper()
	if _, ok := env[key]; ok {
		t.Errorf("env[%q] should not be set, but is %q", key, env[key])
	}
}
