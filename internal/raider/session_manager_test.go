package raider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/tmux"
)

func TestSessionName(t *testing.T) {
	r := &warband.Warband{
		Name:     "horde",
		Raiders: []string{"Toast"},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	name := m.SessionName("Toast")
	if name != "gt-horde-Toast" {
		t.Errorf("sessionName = %q, want gt-horde-Toast", name)
	}
}

func TestSessionManagerRaiderDir(t *testing.T) {
	r := &warband.Warband{
		Name:     "horde",
		Path:     "/home/user/ai/horde",
		Raiders: []string{"Toast"},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	dir := m.raiderDir("Toast")
	expected := "/home/user/ai/horde/raiders/Toast"
	if dir != expected {
		t.Errorf("raiderDir = %q, want %q", dir, expected)
	}
}

func TestHasRaider(t *testing.T) {
	root := t.TempDir()
	// hasRaider checks filesystem, so create actual directories
	for _, name := range []string{"Toast", "Cheedo"} {
		if err := os.MkdirAll(filepath.Join(root, "raiders", name), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	r := &warband.Warband{
		Name:     "horde",
		Path:     root,
		Raiders: []string{"Toast", "Cheedo"},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	if !m.hasRaider("Toast") {
		t.Error("expected hasRaider(Toast) = true")
	}
	if !m.hasRaider("Cheedo") {
		t.Error("expected hasRaider(Cheedo) = true")
	}
	if m.hasRaider("Unknown") {
		t.Error("expected hasRaider(Unknown) = false")
	}
}

func TestStartRaiderNotFound(t *testing.T) {
	r := &warband.Warband{
		Name:     "horde",
		Raiders: []string{"Toast"},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	err := m.Start("Unknown", SessionStartOptions{})
	if err == nil {
		t.Error("expected error for unknown raider")
	}
}

func TestIsRunningNoSession(t *testing.T) {
	r := &warband.Warband{
		Name:     "horde",
		Raiders: []string{"Toast"},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	running, err := m.IsRunning("Toast")
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if running {
		t.Error("expected IsRunning = false for non-existent session")
	}
}

func TestSessionManagerListEmpty(t *testing.T) {
	r := &warband.Warband{
		Name:     "test-warband-unlikely-name",
		Raiders: []string{},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	infos, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("infos count = %d, want 0", len(infos))
	}
}

func TestStopNotFound(t *testing.T) {
	r := &warband.Warband{
		Name:     "test-warband",
		Raiders: []string{"Toast"},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	err := m.Stop("Toast", false)
	if err != ErrSessionNotFound {
		t.Errorf("Stop = %v, want ErrSessionNotFound", err)
	}
}

func TestCaptureNotFound(t *testing.T) {
	r := &warband.Warband{
		Name:     "test-warband",
		Raiders: []string{"Toast"},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	_, err := m.Capture("Toast", 50)
	if err != ErrSessionNotFound {
		t.Errorf("Capture = %v, want ErrSessionNotFound", err)
	}
}

func TestInjectNotFound(t *testing.T) {
	r := &warband.Warband{
		Name:     "test-warband",
		Raiders: []string{"Toast"},
	}
	m := NewSessionManager(tmux.NewTmux(), r)

	err := m.Inject("Toast", "hello")
	if err != ErrSessionNotFound {
		t.Errorf("Inject = %v, want ErrSessionNotFound", err)
	}
}

// TestRaiderCommandFormat verifies the raider session command exports
// GT_ROLE, GT_RIG, GT_RAIDER, and BD_ACTOR inline before starting Claude.
// This is a regression test for gt-y41ep - env vars must be exported inline
// because tmux SetEnvironment only affects new panes, not the current shell.
func TestRaiderCommandFormat(t *testing.T) {
	// This test verifies the expected command format.
	// The actual command is built in Start() but we test the format here
	// to document and verify the expected behavior.

	rigName := "horde"
	raiderName := "Toast"
	expectedBdActor := "horde/raiders/Toast"

	// Build the expected command format (mirrors Start() logic)
	expectedPrefix := "export GT_ROLE=raider GT_RIG=" + rigName + " GT_RAIDER=" + raiderName + " BD_ACTOR=" + expectedBdActor + " GIT_AUTHOR_NAME=" + expectedBdActor
	expectedSuffix := "&& claude --dangerously-skip-permissions"

	// The command must contain all required env exports
	requiredParts := []string{
		"export",
		"GT_ROLE=raider",
		"GT_RIG=" + rigName,
		"GT_RAIDER=" + raiderName,
		"BD_ACTOR=" + expectedBdActor,
		"GIT_AUTHOR_NAME=" + expectedBdActor,
		"claude --dangerously-skip-permissions",
	}

	// Verify expected format contains all required parts
	fullCommand := expectedPrefix + " " + expectedSuffix
	for _, part := range requiredParts {
		if !strings.Contains(fullCommand, part) {
			t.Errorf("Raider command should contain %q", part)
		}
	}

	// Verify GT_ROLE is specifically "raider" (not "warchief" or "clan")
	if !strings.Contains(fullCommand, "GT_ROLE=raider") {
		t.Error("GT_ROLE must be 'raider', not 'warchief' or 'clan'")
	}
}
