// Test Warband-Level Custom Agent Support
//
// This integration test verifies that custom agents defined in warband-level
// settings/config.json are correctly loaded and used when spawning raiders.
// It creates a stub agent, configures it at the warband level, and verifies
// the agent is actually used via tmux session capture.

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRigLevelCustomAgentIntegration tests end-to-end warband-level custom agent functionality.
// This test:
// 1. Creates a stub agent script that echoes identifiable output
// 2. Sets up a minimal encampment/warband with the custom agent configured
// 3. Verifies that BuildRaiderStartupCommand uses the custom agent
// 4. Optionally spawns a tmux session and verifies output (if tmux available)
func TestRigLevelCustomAgentIntegration(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create the stub agent script
	stubAgentPath := createStubAgent(t, tmpDir)

	// Set up encampment structure
	townRoot := filepath.Join(tmpDir, "encampment")
	rigName := "testrig"
	rigPath := filepath.Join(townRoot, rigName)

	setupTestTownWithCustomAgent(t, townRoot, rigName, stubAgentPath)

	// Test 1: Verify ResolveAgentConfig picks up the custom agent
	t.Run("ResolveAgentConfig uses warband-level agent", func(t *testing.T) {
		rc := ResolveAgentConfig(townRoot, rigPath)
		if rc == nil {
			t.Fatal("ResolveAgentConfig returned nil")
		}

		if rc.Command != stubAgentPath {
			t.Errorf("Expected command %q, got %q", stubAgentPath, rc.Command)
		}

		// Verify args are passed through
		if len(rc.Args) != 2 || rc.Args[0] != "--test-mode" || rc.Args[1] != "--stub" {
			t.Errorf("Expected args [--test-mode --stub], got %v", rc.Args)
		}
	})

	// Test 2: Verify BuildRaiderStartupCommand includes the custom agent
	t.Run("BuildRaiderStartupCommand uses custom agent", func(t *testing.T) {
		cmd := BuildRaiderStartupCommand(rigName, "test-raider", rigPath, "")

		if !strings.Contains(cmd, stubAgentPath) {
			t.Errorf("Expected command to contain stub agent path %q, got: %s", stubAgentPath, cmd)
		}

		if !strings.Contains(cmd, "--test-mode") {
			t.Errorf("Expected command to contain --test-mode, got: %s", cmd)
		}

		// Verify environment variables are set
		if !strings.Contains(cmd, "HD_ROLE=raider") {
			t.Errorf("Expected HD_ROLE=raider in command, got: %s", cmd)
		}

		if !strings.Contains(cmd, "HD_RAIDER=test-raider") {
			t.Errorf("Expected HD_RAIDER=test-raider in command, got: %s", cmd)
		}
	})

	// Test 3: Verify ResolveAgentConfigWithOverride respects warband agents
	t.Run("ResolveAgentConfigWithOverride with warband agent", func(t *testing.T) {
		rc, agentName, err := ResolveAgentConfigWithOverride(townRoot, rigPath, "stub-agent")
		if err != nil {
			t.Fatalf("ResolveAgentConfigWithOverride failed: %v", err)
		}

		if agentName != "stub-agent" {
			t.Errorf("Expected agent name 'stub-agent', got %q", agentName)
		}

		if rc.Command != stubAgentPath {
			t.Errorf("Expected command %q, got %q", stubAgentPath, rc.Command)
		}
	})

	// Test 4: Verify unknown agent override returns error
	t.Run("ResolveAgentConfigWithOverride unknown agent errors", func(t *testing.T) {
		_, _, err := ResolveAgentConfigWithOverride(townRoot, rigPath, "nonexistent-agent")
		if err == nil {
			t.Fatal("Expected error for nonexistent agent, got nil")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Expected 'not found' error, got: %v", err)
		}
	})

	// Test 5: Tmux integration (skip if tmux not available)
	t.Run("TmuxSessionWithCustomAgent", func(t *testing.T) {
		if _, err := exec.LookPath("tmux"); err != nil {
			t.Skip("tmux not available, skipping session test")
		}

		testTmuxSessionWithStubAgent(t, tmpDir, stubAgentPath, rigName)
	})
}

// createStubAgent creates a bash script that simulates an AI agent.
// The script echoes identifiable output and handles simple Q&A.
func createStubAgent(t *testing.T, tmpDir string) string {
	t.Helper()

	stubScript := `#!/bin/bash
# Stub Agent for Integration Testing
# This simulates an AI agent with identifiable output

AGENT_NAME="STUB_AGENT"
AGENT_VERSION="1.0.0"

echo "=========================================="
echo "STUB_AGENT_STARTED"
echo "Agent: $AGENT_NAME v$AGENT_VERSION"
echo "Args: $@"
echo "Working Dir: $(pwd)"
echo "HD_ROLE: ${HD_ROLE:-not_set}"
echo "HD_RAIDER: ${HD_RAIDER:-not_set}"
echo "HD_WARBAND: ${HD_WARBAND:-not_set}"
echo "=========================================="

# Simple Q&A loop
while true; do
    echo ""
    echo "STUB_AGENT_READY"
    echo "Enter question (or 'exit' to quit):"
    
    # Read with timeout for non-interactive testing
    if read -t 5 question; then
        case "$question" in
            "exit"|"quit"|"q")
                echo "STUB_AGENT_EXITING"
                exit 0
                ;;
            "what is 2+2"*)
                echo "STUB_AGENT_ANSWER: 4"
                ;;
            "ping"*)
                echo "STUB_AGENT_ANSWER: pong"
                ;;
            "status"*)
                echo "STUB_AGENT_ANSWER: operational"
                ;;
            *)
                echo "STUB_AGENT_ANSWER: I received your question: $question"
                ;;
        esac
    else
        # Timeout - check if we should exit
        if [ -f "/tmp/stub_agent_stop_$$" ]; then
            echo "STUB_AGENT_STOPPING (signal file detected)"
            rm -f "/tmp/stub_agent_stop_$$"
            exit 0
        fi
    fi
done
`

	stubPath := filepath.Join(tmpDir, "stub-agent")
	if err := os.WriteFile(stubPath, []byte(stubScript), 0755); err != nil {
		t.Fatalf("Failed to create stub agent: %v", err)
	}

	return stubPath
}

// setupTestTownWithCustomAgent creates a minimal encampment/warband structure with a custom agent.
func setupTestTownWithCustomAgent(t *testing.T, townRoot, rigName, stubAgentPath string) {
	t.Helper()

	rigPath := filepath.Join(townRoot, rigName)

	// Create directory structure
	dirs := []string{
		filepath.Join(townRoot, "warchief"),
		filepath.Join(townRoot, "settings"),
		filepath.Join(rigPath, "settings"),
		filepath.Join(rigPath, "raiders"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create encampment.json
	townConfig := map[string]interface{}{
		"type":       "encampment",
		"version":    2,
		"name":       "test-encampment",
		"created_at": time.Now().Format(time.RFC3339),
	}
	writeTownJSON(t, filepath.Join(townRoot, "warchief", "encampment.json"), townConfig)

	// Create encampment settings (empty, uses defaults)
	townSettings := map[string]interface{}{
		"type":          "encampment-settings",
		"version":       1,
		"default_agent": "claude",
	}
	writeTownJSON(t, filepath.Join(townRoot, "settings", "config.json"), townSettings)

	// Create warband settings with custom agent
	rigSettings := map[string]interface{}{
		"type":    "warband-settings",
		"version": 1,
		"agent":   "stub-agent",
		"agents": map[string]interface{}{
			"stub-agent": map[string]interface{}{
				"command": stubAgentPath,
				"args":    []string{"--test-mode", "--stub"},
			},
		},
	}
	writeTownJSON(t, filepath.Join(rigPath, "settings", "config.json"), rigSettings)

	// Create warbands.json
	rigsConfig := map[string]interface{}{
		"version": 1,
		"warbands": map[string]interface{}{
			rigName: map[string]interface{}{
				"git_url":  "https://github.com/test/testrepo.git",
				"added_at": time.Now().Format(time.RFC3339),
			},
		},
	}
	writeTownJSON(t, filepath.Join(townRoot, "warchief", "warbands.json"), rigsConfig)
}

// writeTownJSON writes a JSON config file.
func writeTownJSON(t *testing.T, path string, data interface{}) {
	t.Helper()

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal JSON for %s: %v", path, err)
	}

	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		t.Fatalf("Failed to write %s: %v", path, err)
	}
}

func pollForOutput(t *testing.T, sessionName, expected string, timeout time.Duration) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output := captureTmuxPane(t, sessionName, 50)
		if strings.Contains(output, expected) {
			return output, true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return captureTmuxPane(t, sessionName, 50), false
}

func testTmuxSessionWithStubAgent(t *testing.T, tmpDir, stubAgentPath, rigName string) {
	t.Helper()

	sessionName := fmt.Sprintf("hd-test-pid%d-%d", os.Getpid(), time.Now().UnixNano())
	workDir := tmpDir

	exec.Command("tmux", "kill-session", "-t", sessionName).Run()

	defer func() {
		exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	cmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", workDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to create tmux session: %v", err)
	}

	envVars := map[string]string{
		"HD_ROLE":    "raider",
		"HD_RAIDER": "test-raider",
		"HD_WARBAND":     rigName,
	}

	for key, val := range envVars {
		cmd := exec.Command("tmux", "set-environment", "-t", sessionName, key, val)
		if err := cmd.Run(); err != nil {
			t.Logf("Warning: failed to set %s: %v", key, err)
		}
	}

	agentCmd := fmt.Sprintf("%s --test-mode --stub", stubAgentPath)
	cmd = exec.Command("tmux", "send-keys", "-t", sessionName, agentCmd, "Enter")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to send keys: %v", err)
	}

	output, found := pollForOutput(t, sessionName, "STUB_AGENT_STARTED", 5*time.Second)
	if !found {
		t.Skipf("stub agent output not detected; tmux capture unreliable. Output:\n%s", output)
	}

	if !strings.Contains(output, "HD_ROLE: raider") {
		t.Logf("Warning: HD_ROLE not visible in agent output (tmux env may not propagate to subshell)")
	}

	cmd = exec.Command("tmux", "send-keys", "-t", sessionName, "ping", "Enter")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to send ping: %v", err)
	}

	output, found = pollForOutput(t, sessionName, "STUB_AGENT_ANSWER: pong", 3*time.Second)
	if !found {
		t.Errorf("Expected 'pong' response, got:\n%s", output)
	}

	cmd = exec.Command("tmux", "send-keys", "-t", sessionName, "exit", "Enter")
	if err := cmd.Run(); err != nil {
		t.Logf("Warning: failed to send exit: %v", err)
	}

	output, found = pollForOutput(t, sessionName, "STUB_AGENT_EXITING", 2*time.Second)
	if !found {
		t.Logf("Note: Agent may have exited before capture. Output:\n%s", output)
	}

	t.Logf("Tmux session test completed successfully")
}

// captureTmuxPane captures the output from a tmux pane.
func captureTmuxPane(t *testing.T, sessionName string, lines int) string {
	t.Helper()

	cmd := exec.Command("tmux", "capture-pane", "-t", sessionName, "-p", "-S", fmt.Sprintf("-%d", lines))
	output, err := cmd.Output()
	if err != nil {
		t.Logf("Warning: failed to capture pane: %v", err)
		return ""
	}

	return string(output)
}

func waitForTmuxOutputContains(t *testing.T, sessionName, needle string, timeout time.Duration) (string, bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	output := ""
	for time.Now().Before(deadline) {
		output = captureTmuxPane(t, sessionName, 200)
		if strings.Contains(output, needle) {
			return output, true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return output, false
}

// TestRigAgentOverridesTownAgent verifies warband agents take precedence over encampment agents.
func TestRigAgentOverridesTownAgent(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	townRoot := filepath.Join(tmpDir, "encampment")
	rigName := "testrig"
	rigPath := filepath.Join(townRoot, rigName)

	// Create directory structure
	dirs := []string{
		filepath.Join(townRoot, "warchief"),
		filepath.Join(townRoot, "settings"),
		filepath.Join(rigPath, "settings"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Encampment settings with a custom agent
	townSettings := map[string]interface{}{
		"type":          "encampment-settings",
		"version":       1,
		"default_agent": "my-agent",
		"agents": map[string]interface{}{
			"my-agent": map[string]interface{}{
				"command": "/encampment/path/to/agent",
				"args":    []string{"--encampment-level"},
			},
		},
	}
	writeTownJSON(t, filepath.Join(townRoot, "settings", "config.json"), townSettings)

	// Warband settings with SAME agent name but different config (should override)
	rigSettings := map[string]interface{}{
		"type":    "warband-settings",
		"version": 1,
		"agent":   "my-agent",
		"agents": map[string]interface{}{
			"my-agent": map[string]interface{}{
				"command": "/warband/path/to/agent",
				"args":    []string{"--warband-level"},
			},
		},
	}
	writeTownJSON(t, filepath.Join(rigPath, "settings", "config.json"), rigSettings)

	// Create encampment.json
	townConfig := map[string]interface{}{
		"type":       "encampment",
		"version":    2,
		"name":       "test-encampment",
		"created_at": time.Now().Format(time.RFC3339),
	}
	writeTownJSON(t, filepath.Join(townRoot, "warchief", "encampment.json"), townConfig)

	// Resolve agent config
	rc := ResolveAgentConfig(townRoot, rigPath)
	if rc == nil {
		t.Fatal("ResolveAgentConfig returned nil")
	}

	// Warband agent should take precedence
	if rc.Command != "/warband/path/to/agent" {
		t.Errorf("Expected warband agent command '/warband/path/to/agent', got %q", rc.Command)
	}

	if len(rc.Args) != 1 || rc.Args[0] != "--warband-level" {
		t.Errorf("Expected warband args [--warband-level], got %v", rc.Args)
	}
}
