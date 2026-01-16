package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewHookAttachmentValidCheck(t *testing.T) {
	check := NewHookAttachmentValidCheck()

	if check.Name() != "hook-attachment-valid" {
		t.Errorf("expected name 'hook-attachment-valid', got %q", check.Name())
	}

	if check.Description() != "Verify attached totems exist and are not closed" {
		t.Errorf("unexpected description: %q", check.Description())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestHookAttachmentValidCheck_NoRelicsDir(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewHookAttachmentValidCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	// No relics dir means nothing to check, should be OK
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no relics dir, got %v", result.Status)
	}
}

func TestHookAttachmentValidCheck_EmptyRelicsDir(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewHookAttachmentValidCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	// Empty relics dir means no pinned relics, should be OK
	// Note: This may error if rl CLI is not available, but should still handle gracefully
	if result.Status != StatusOK && result.Status != StatusError {
		t.Errorf("expected StatusOK or graceful error, got %v", result.Status)
	}
}

func TestHookAttachmentValidCheck_FormatInvalid(t *testing.T) {
	check := NewHookAttachmentValidCheck()

	tests := []struct {
		inv      invalidAttachment
		expected string
	}{
		{
			inv: invalidAttachment{
				pinnedBeadID: "hq-123",
				moleculeID:   "hd-456",
				reason:       "not_found",
			},
			expected: "hq-123: attached totem gt-456 not found",
		},
		{
			inv: invalidAttachment{
				pinnedBeadID: "hq-123",
				moleculeID:   "hd-789",
				reason:       "closed",
			},
			expected: "hq-123: attached totem gt-789 is closed",
		},
	}

	for _, tt := range tests {
		result := check.formatInvalid(tt.inv)
		if result != tt.expected {
			t.Errorf("formatInvalid() = %q, want %q", result, tt.expected)
		}
	}
}

func TestHookAttachmentValidCheck_FindRigRelicsDirs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create encampment-level .relics (should be excluded)
	townRelics := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(townRelics, 0755); err != nil {
		t.Fatal(err)
	}

	// Create warband-level .relics
	rigRelics := filepath.Join(tmpDir, "myrig", ".relics")
	if err := os.MkdirAll(rigRelics, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewHookAttachmentValidCheck()
	dirs := check.findRigRelicsDirs(tmpDir)

	// Should find the warband-level relics but not encampment-level
	found := false
	for _, dir := range dirs {
		if dir == townRelics {
			t.Error("findRigRelicsDirs should not include encampment-level .relics")
		}
		if dir == rigRelics {
			found = true
		}
	}

	if !found && len(dirs) > 0 {
		t.Logf("Found dirs: %v", dirs)
	}
}

// Tests for HookSingletonCheck

func TestNewHookSingletonCheck(t *testing.T) {
	check := NewHookSingletonCheck()

	if check.Name() != "hook-singleton" {
		t.Errorf("expected name 'hook-singleton', got %q", check.Name())
	}

	if check.Description() != "Ensure each agent has at most one handoff bead" {
		t.Errorf("unexpected description: %q", check.Description())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestHookSingletonCheck_NoRelicsDir(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewHookSingletonCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	// No relics dir means nothing to check, should be OK
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no relics dir, got %v", result.Status)
	}
}

func TestHookSingletonCheck_EmptyRelicsDir(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewHookSingletonCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	// Empty relics dir means no pinned relics, should be OK
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when empty relics dir, got %v", result.Status)
	}
}

func TestHookSingletonCheck_FormatDuplicate(t *testing.T) {
	check := NewHookSingletonCheck()

	tests := []struct {
		dup      duplicateHandoff
		expected string
	}{
		{
			dup: duplicateHandoff{
				title:   "Warchief Handoff",
				beadIDs: []string{"hq-123", "hq-456"},
			},
			expected: `"Warchief Handoff" has 2 relics: hq-123, hq-456`,
		},
		{
			dup: duplicateHandoff{
				title:   "Witness Handoff",
				beadIDs: []string{"hd-1", "hd-2", "hd-3"},
			},
			expected: `"Witness Handoff" has 3 relics: gt-1, gt-2, gt-3`,
		},
	}

	for _, tt := range tests {
		result := check.formatDuplicate(tt.dup)
		if result != tt.expected {
			t.Errorf("formatDuplicate() = %q, want %q", result, tt.expected)
		}
	}
}

// Tests for OrphanedAttachmentsCheck

func TestNewOrphanedAttachmentsCheck(t *testing.T) {
	check := NewOrphanedAttachmentsCheck()

	if check.Name() != "orphaned-attachments" {
		t.Errorf("expected name 'orphaned-attachments', got %q", check.Name())
	}

	if check.Description() != "Detect handoff relics for non-existent agents" {
		t.Errorf("unexpected description: %q", check.Description())
	}

	// This check is not auto-fixable (uses BaseCheck, not FixableCheck)
	if check.CanFix() {
		t.Error("expected CanFix to return false")
	}
}

func TestOrphanedAttachmentsCheck_NoRelicsDir(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewOrphanedAttachmentsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	// No relics dir means nothing to check, should be OK
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no relics dir, got %v", result.Status)
	}
}

func TestOrphanedAttachmentsCheck_FormatOrphan(t *testing.T) {
	check := NewOrphanedAttachmentsCheck()

	tests := []struct {
		orph     orphanedHandoff
		expected string
	}{
		{
			orph: orphanedHandoff{
				beadID: "hq-123",
				agent:  "horde/nux",
			},
			expected: `hq-123: agent "horde/nux" no longer exists`,
		},
		{
			orph: orphanedHandoff{
				beadID: "hd-456",
				agent:  "horde/clan/joe",
			},
			expected: `gt-456: agent "horde/clan/joe" no longer exists`,
		},
	}

	for _, tt := range tests {
		result := check.formatOrphan(tt.orph)
		if result != tt.expected {
			t.Errorf("formatOrphan() = %q, want %q", result, tt.expected)
		}
	}
}

func TestOrphanedAttachmentsCheck_AgentExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some agent directories
	raiderDir := filepath.Join(tmpDir, "horde", "raiders", "nux")
	if err := os.MkdirAll(raiderDir, 0755); err != nil {
		t.Fatal(err)
	}

	crewDir := filepath.Join(tmpDir, "horde", "clan", "joe")
	if err := os.MkdirAll(crewDir, 0755); err != nil {
		t.Fatal(err)
	}

	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatal(err)
	}

	witnessDir := filepath.Join(tmpDir, "horde", "witness")
	if err := os.MkdirAll(witnessDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewOrphanedAttachmentsCheck()

	tests := []struct {
		agent    string
		expected bool
	}{
		// Existing agents
		{"horde/nux", true},
		{"horde/clan/joe", true},
		{"warchief", true},
		{"horde-witness", true},

		// Non-existent agents
		{"horde/deleted", false},
		{"horde/clan/gone", false},
		{"otherrig-witness", false},
	}

	for _, tt := range tests {
		result := check.agentExists(tt.agent, tmpDir)
		if result != tt.expected {
			t.Errorf("agentExists(%q) = %v, want %v", tt.agent, result, tt.expected)
		}
	}
}
