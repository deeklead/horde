package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OWNER/horde/internal/relics"
)

func TestRoleRelicsCheck_Run(t *testing.T) {
	t.Run("no encampment relics returns warning", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create minimal encampment structure without .relics
		if err := os.MkdirAll(filepath.Join(tmpDir, "warchief"), 0755); err != nil {
			t.Fatal(err)
		}

		check := NewRoleRelicsCheck()
		ctx := &CheckContext{TownRoot: tmpDir}
		result := check.Run(ctx)

		// Without .relics directory, all role relics are "missing"
		expectedCount := len(relics.AllRoleBeadDefs())
		if result.Status != StatusWarning {
			t.Errorf("expected StatusWarning, got %v: %s", result.Status, result.Message)
		}
		if len(result.Details) != expectedCount {
			t.Errorf("expected %d missing role relics, got %d: %v", expectedCount, len(result.Details), result.Details)
		}
	})

	t.Run("check is fixable", func(t *testing.T) {
		check := NewRoleRelicsCheck()
		if !check.CanFix() {
			t.Error("RoleRelicsCheck should be fixable")
		}
	})
}

func TestRoleRelicsCheck_usesSharedDefs(t *testing.T) {
	// Verify the check uses relics.AllRoleBeadDefs()
	roleDefs := relics.AllRoleBeadDefs()

	if len(roleDefs) < 7 {
		t.Errorf("expected at least 7 role relics, got %d", len(roleDefs))
	}

	// Verify key roles are present
	expectedIDs := map[string]bool{
		"hq-warchief-role":    false,
		"hq-shaman-role":   false,
		"hq-witness-role":  false,
		"hq-forge-role": false,
	}

	for _, role := range roleDefs {
		if _, exists := expectedIDs[role.ID]; exists {
			expectedIDs[role.ID] = true
		}
	}

	for id, found := range expectedIDs {
		if !found {
			t.Errorf("expected role %s not found in AllRoleBeadDefs()", id)
		}
	}
}
