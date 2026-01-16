package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRigRoutesJSONLCheck_Run(t *testing.T) {
	t.Run("no warbands returns OK", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create minimal encampment structure
		if err := os.MkdirAll(filepath.Join(tmpDir, "warchief"), 0755); err != nil {
			t.Fatal(err)
		}

		check := NewRigRoutesJSONLCheck()
		ctx := &CheckContext{TownRoot: tmpDir}
		result := check.Run(ctx)

		if result.Status != StatusOK {
			t.Errorf("expected StatusOK, got %v: %s", result.Status, result.Message)
		}
	})

	t.Run("warband without routes.jsonl returns OK", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create warband with .relics but no routes.jsonl
		rigRelics := filepath.Join(tmpDir, "myrig", ".relics")
		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatal(err)
		}

		check := NewRigRoutesJSONLCheck()
		ctx := &CheckContext{TownRoot: tmpDir}
		result := check.Run(ctx)

		if result.Status != StatusOK {
			t.Errorf("expected StatusOK, got %v: %s", result.Status, result.Message)
		}
	})

	t.Run("warband with routes.jsonl warns", func(t *testing.T) {
		tmpDir := t.TempDir()
		rigRelics := filepath.Join(tmpDir, "myrig", ".relics")
		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatal(err)
		}

		// Create routes.jsonl (any content - will be deleted)
		if err := os.WriteFile(filepath.Join(rigRelics, "routes.jsonl"), []byte(`{"prefix":"x-","path":"."}`+"\n"), 0644); err != nil {
			t.Fatal(err)
		}

		check := NewRigRoutesJSONLCheck()
		ctx := &CheckContext{TownRoot: tmpDir}
		result := check.Run(ctx)

		if result.Status != StatusWarning {
			t.Errorf("expected StatusWarning, got %v: %s", result.Status, result.Message)
		}
		if len(result.Details) == 0 {
			t.Error("expected details about the issue")
		}
	})

	t.Run("multiple warbands with routes.jsonl reports all", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create two warbands with routes.jsonl
		for _, rigName := range []string{"rig1", "rig2"} {
			rigRelics := filepath.Join(tmpDir, rigName, ".relics")
			if err := os.MkdirAll(rigRelics, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(rigRelics, "routes.jsonl"), []byte(`{"prefix":"x-","path":"."}`+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}

		check := NewRigRoutesJSONLCheck()
		ctx := &CheckContext{TownRoot: tmpDir}
		result := check.Run(ctx)

		if result.Status != StatusWarning {
			t.Errorf("expected StatusWarning, got %v", result.Status)
		}
		if len(result.Details) != 2 {
			t.Errorf("expected 2 details, got %d: %v", len(result.Details), result.Details)
		}
	})
}

func TestRigRoutesJSONLCheck_Fix(t *testing.T) {
	t.Run("deletes routes.jsonl unconditionally", func(t *testing.T) {
		tmpDir := t.TempDir()
		rigRelics := filepath.Join(tmpDir, "myrig", ".relics")
		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatal(err)
		}

		// Create routes.jsonl with any content
		routesPath := filepath.Join(rigRelics, "routes.jsonl")
		if err := os.WriteFile(routesPath, []byte(`{"id":"test-abc123","title":"Test Issue"}`+"\n"), 0644); err != nil {
			t.Fatal(err)
		}

		check := NewRigRoutesJSONLCheck()
		ctx := &CheckContext{TownRoot: tmpDir}

		// Run check first to populate affectedRigs
		result := check.Run(ctx)
		if result.Status != StatusWarning {
			t.Fatalf("expected StatusWarning, got %v", result.Status)
		}

		// Fix
		if err := check.Fix(ctx); err != nil {
			t.Fatalf("Fix() error: %v", err)
		}

		// Verify routes.jsonl is gone
		if _, err := os.Stat(routesPath); !os.IsNotExist(err) {
			t.Error("routes.jsonl should have been deleted")
		}
	})

	t.Run("fix is idempotent", func(t *testing.T) {
		tmpDir := t.TempDir()
		rigRelics := filepath.Join(tmpDir, "myrig", ".relics")
		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatal(err)
		}

		check := NewRigRoutesJSONLCheck()
		ctx := &CheckContext{TownRoot: tmpDir}

		// First run - should pass (no routes.jsonl)
		result := check.Run(ctx)
		if result.Status != StatusOK {
			t.Fatalf("expected StatusOK, got %v", result.Status)
		}

		// Fix should be no-op
		if err := check.Fix(ctx); err != nil {
			t.Fatalf("Fix() error on clean state: %v", err)
		}
	})
}

func TestRigRoutesJSONLCheck_FindRigDirectories(t *testing.T) {
	t.Run("finds warbands from multiple sources", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create warchief directory
		if err := os.MkdirAll(filepath.Join(tmpDir, "warchief"), 0755); err != nil {
			t.Fatal(err)
		}

		// Create encampment-level .relics with routes.jsonl
		townRelics := filepath.Join(tmpDir, ".relics")
		if err := os.MkdirAll(townRelics, 0755); err != nil {
			t.Fatal(err)
		}
		routes := `{"prefix":"rig1-","path":"rig1/warchief/warband"}` + "\n"
		if err := os.WriteFile(filepath.Join(townRelics, "routes.jsonl"), []byte(routes), 0644); err != nil {
			t.Fatal(err)
		}

		// Create rig1 (from routes.jsonl)
		if err := os.MkdirAll(filepath.Join(tmpDir, "rig1", ".relics"), 0755); err != nil {
			t.Fatal(err)
		}

		// Create rig2 (unregistered but has .relics)
		if err := os.MkdirAll(filepath.Join(tmpDir, "rig2", ".relics"), 0755); err != nil {
			t.Fatal(err)
		}

		check := NewRigRoutesJSONLCheck()
		warbands := check.findRigDirectories(tmpDir)

		if len(warbands) != 2 {
			t.Errorf("expected 2 warbands, got %d: %v", len(warbands), warbands)
		}
	})

	t.Run("excludes warchief and .relics directories", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create directories that should be excluded
		if err := os.MkdirAll(filepath.Join(tmpDir, "warchief", ".relics"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(tmpDir, ".relics"), 0755); err != nil {
			t.Fatal(err)
		}

		check := NewRigRoutesJSONLCheck()
		warbands := check.findRigDirectories(tmpDir)

		if len(warbands) != 0 {
			t.Errorf("expected 0 warbands (warchief and .relics should be excluded), got %d: %v", len(warbands), warbands)
		}
	})
}
