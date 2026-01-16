package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deeklead/horde/internal/ritual"
)

func TestNewFormulaCheck(t *testing.T) {
	check := NewFormulaCheck()
	if check.Name() != "rituals" {
		t.Errorf("Name() = %q, want %q", check.Name(), "rituals")
	}
	if !check.CanFix() {
		t.Error("FormulaCheck should be fixable")
	}
}

func TestFormulaCheck_Run_AllOK(t *testing.T) {
	tmpDir := t.TempDir()

	// Provision rituals fresh
	_, err := ritual.ProvisionFormulas(tmpDir)
	if err != nil {
		t.Fatalf("ProvisionFormulas() error: %v", err)
	}

	check := NewFormulaCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want %v", result.Status, StatusOK)
	}
}

func TestFormulaCheck_Run_Missing(t *testing.T) {
	tmpDir := t.TempDir()

	// Provision rituals
	_, err := ritual.ProvisionFormulas(tmpDir)
	if err != nil {
		t.Fatalf("ProvisionFormulas() error: %v", err)
	}

	// Delete a ritual
	formulasDir := filepath.Join(tmpDir, ".relics", "rituals")
	formulaPath := filepath.Join(formulasDir, "totem-shaman-scout.ritual.toml")
	if err := os.Remove(formulaPath); err != nil {
		t.Fatal(err)
	}

	check := NewFormulaCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want %v", result.Status, StatusWarning)
	}
	if result.FixHint == "" {
		t.Error("should have FixHint")
	}
}

func TestFormulaCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()

	// Provision rituals
	_, err := ritual.ProvisionFormulas(tmpDir)
	if err != nil {
		t.Fatalf("ProvisionFormulas() error: %v", err)
	}

	// Delete a ritual
	formulasDir := filepath.Join(tmpDir, ".relics", "rituals")
	formulaPath := filepath.Join(formulasDir, "totem-shaman-scout.ritual.toml")
	if err := os.Remove(formulaPath); err != nil {
		t.Fatal(err)
	}

	check := NewFormulaCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	// Run fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix() error: %v", err)
	}

	// Verify ritual was restored
	if _, err := os.Stat(formulaPath); os.IsNotExist(err) {
		t.Error("ritual should have been restored")
	}

	// Re-run check - should be OK now
	result := check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("after fix, Status = %v, want %v", result.Status, StatusOK)
	}
}
