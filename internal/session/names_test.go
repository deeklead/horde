package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWarchiefSessionName(t *testing.T) {
	// Warchief session name is now fixed (one per machine), uses HQ prefix
	want := "hq-warchief"
	got := WarchiefSessionName()
	if got != want {
		t.Errorf("WarchiefSessionName() = %q, want %q", got, want)
	}
}

func TestShamanSessionName(t *testing.T) {
	// Shaman session name is now fixed (one per machine), uses HQ prefix
	want := "hq-shaman"
	got := ShamanSessionName()
	if got != want {
		t.Errorf("ShamanSessionName() = %q, want %q", got, want)
	}
}

func TestWitnessSessionName(t *testing.T) {
	tests := []struct {
		warband  string
		want string
	}{
		{"horde", "hd-horde-witness"},
		{"relics", "hd-relics-witness"},
		{"foo", "hd-foo-witness"},
	}
	for _, tt := range tests {
		t.Run(tt.warband, func(t *testing.T) {
			got := WitnessSessionName(tt.warband)
			if got != tt.want {
				t.Errorf("WitnessSessionName(%q) = %q, want %q", tt.warband, got, tt.want)
			}
		})
	}
}

func TestForgeSessionName(t *testing.T) {
	tests := []struct {
		warband  string
		want string
	}{
		{"horde", "hd-horde-forge"},
		{"relics", "hd-relics-forge"},
		{"foo", "hd-foo-forge"},
	}
	for _, tt := range tests {
		t.Run(tt.warband, func(t *testing.T) {
			got := ForgeSessionName(tt.warband)
			if got != tt.want {
				t.Errorf("ForgeSessionName(%q) = %q, want %q", tt.warband, got, tt.want)
			}
		})
	}
}

func TestCrewSessionName(t *testing.T) {
	tests := []struct {
		warband  string
		name string
		want string
	}{
		{"horde", "max", "hd-horde-clan-max"},
		{"relics", "alice", "hd-relics-clan-alice"},
		{"foo", "bar", "hd-foo-clan-bar"},
	}
	for _, tt := range tests {
		t.Run(tt.warband+"/"+tt.name, func(t *testing.T) {
			got := CrewSessionName(tt.warband, tt.name)
			if got != tt.want {
				t.Errorf("CrewSessionName(%q, %q) = %q, want %q", tt.warband, tt.name, got, tt.want)
			}
		})
	}
}

func TestRaiderSessionName(t *testing.T) {
	tests := []struct {
		warband  string
		name string
		want string
	}{
		{"horde", "Toast", "hd-horde-Toast"},
		{"horde", "Furiosa", "hd-horde-Furiosa"},
		{"relics", "worker1", "hd-relics-worker1"},
	}
	for _, tt := range tests {
		t.Run(tt.warband+"/"+tt.name, func(t *testing.T) {
			got := RaiderSessionName(tt.warband, tt.name)
			if got != tt.want {
				t.Errorf("RaiderSessionName(%q, %q) = %q, want %q", tt.warband, tt.name, got, tt.want)
			}
		})
	}
}

func TestPrefix(t *testing.T) {
	want := "hd-"
	if Prefix != want {
		t.Errorf("Prefix = %q, want %q", Prefix, want)
	}
}

func TestPropulsionNudgeForRole_WithSessionID(t *testing.T) {
	// Create temp directory with session_id file
	tmpDir := t.TempDir()
	runtimeDir := filepath.Join(tmpDir, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("creating runtime dir: %v", err)
	}

	sessionID := "test-session-abc123"
	if err := os.WriteFile(filepath.Join(runtimeDir, "session_id"), []byte(sessionID), 0644); err != nil {
		t.Fatalf("writing session_id: %v", err)
	}

	// Test that session ID is appended
	msg := PropulsionNudgeForRole("warchief", tmpDir)
	if !strings.Contains(msg, "[session:test-session-abc123]") {
		t.Errorf("PropulsionNudgeForRole(warchief, tmpDir) = %q, should contain [session:test-session-abc123]", msg)
	}
}

func TestPropulsionNudgeForRole_WithoutSessionID(t *testing.T) {
	// Use nonexistent directory
	msg := PropulsionNudgeForRole("warchief", "/nonexistent-dir-12345")
	if strings.Contains(msg, "[session:") {
		t.Errorf("PropulsionNudgeForRole(warchief, /nonexistent) = %q, should NOT contain session ID", msg)
	}
}

func TestPropulsionNudgeForRole_EmptyWorkDir(t *testing.T) {
	// Empty workDir should not crash and should not include session ID
	msg := PropulsionNudgeForRole("warchief", "")
	if strings.Contains(msg, "[session:") {
		t.Errorf("PropulsionNudgeForRole(warchief, \"\") = %q, should NOT contain session ID", msg)
	}
}

func TestPropulsionNudgeForRole_AllRoles(t *testing.T) {
	tests := []struct {
		role     string
		contains string
	}{
		{"raider", "hd hook"},
		{"clan", "hd hook"},
		{"witness", "hd rally"},
		{"forge", "hd rally"},
		{"shaman", "hd rally"},
		{"warchief", "hd rally"},
		{"unknown", "hd hook"},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			msg := PropulsionNudgeForRole(tt.role, "")
			if !strings.Contains(msg, tt.contains) {
				t.Errorf("PropulsionNudgeForRole(%q, \"\") = %q, should contain %q", tt.role, msg, tt.contains)
			}
		})
	}
}
