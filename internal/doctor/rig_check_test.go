package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRelicsRedirectCheck(t *testing.T) {
	check := NewRelicsRedirectCheck()

	if check.Name() != "relics-redirect" {
		t.Errorf("expected name 'relics-redirect', got %q", check.Name())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestRelicsRedirectCheck_NoRigSpecified(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: ""}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no warband specified, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "skipping") {
		t.Errorf("expected message about skipping, got %q", result.Message)
	}
}

func TestRelicsRedirectCheck_NoRelicsAtAll(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(rigDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError when no relics exist (fixable), got %v", result.Status)
	}
}

func TestRelicsRedirectCheck_LocalRelicsOnly(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create local relics at warband root (no warchief/warband/.relics)
	localRelics := filepath.Join(rigDir, ".relics")
	if err := os.MkdirAll(localRelics, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for local relics (no redirect needed), got %v", result.Status)
	}
	if !strings.Contains(result.Message, "local relics") {
		t.Errorf("expected message about local relics, got %q", result.Message)
	}
}

func TestRelicsRedirectCheck_TrackedRelicsMissingRedirect(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create tracked relics at warchief/warband/.relics
	trackedRelics := filepath.Join(rigDir, "warchief", "warband", ".relics")
	if err := os.MkdirAll(trackedRelics, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing redirect, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "Missing") {
		t.Errorf("expected message about missing redirect, got %q", result.Message)
	}
}

func TestRelicsRedirectCheck_TrackedRelicsCorrectRedirect(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create tracked relics at warchief/warband/.relics
	trackedRelics := filepath.Join(rigDir, "warchief", "warband", ".relics")
	if err := os.MkdirAll(trackedRelics, 0755); err != nil {
		t.Fatal(err)
	}

	// Create warband-level .relics with correct redirect
	rigRelics := filepath.Join(rigDir, ".relics")
	if err := os.MkdirAll(rigRelics, 0755); err != nil {
		t.Fatal(err)
	}
	redirectPath := filepath.Join(rigRelics, "redirect")
	if err := os.WriteFile(redirectPath, []byte("warchief/warband/.relics\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for correct redirect, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "correctly configured") {
		t.Errorf("expected message about correct config, got %q", result.Message)
	}
}

func TestRelicsRedirectCheck_TrackedRelicsWrongRedirect(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create tracked relics at warchief/warband/.relics
	trackedRelics := filepath.Join(rigDir, "warchief", "warband", ".relics")
	if err := os.MkdirAll(trackedRelics, 0755); err != nil {
		t.Fatal(err)
	}

	// Create warband-level .relics with wrong redirect
	rigRelics := filepath.Join(rigDir, ".relics")
	if err := os.MkdirAll(rigRelics, 0755); err != nil {
		t.Fatal(err)
	}
	redirectPath := filepath.Join(rigRelics, "redirect")
	if err := os.WriteFile(redirectPath, []byte("wrong/path\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for wrong redirect (fixable), got %v", result.Status)
	}
	if !strings.Contains(result.Message, "wrong/path") {
		t.Errorf("expected message to contain wrong path, got %q", result.Message)
	}
}

func TestRelicsRedirectCheck_FixWrongRedirect(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create tracked relics at warchief/warband/.relics
	trackedRelics := filepath.Join(rigDir, "warchief", "warband", ".relics")
	if err := os.MkdirAll(trackedRelics, 0755); err != nil {
		t.Fatal(err)
	}

	// Create warband-level .relics with wrong redirect
	rigRelics := filepath.Join(rigDir, ".relics")
	if err := os.MkdirAll(rigRelics, 0755); err != nil {
		t.Fatal(err)
	}
	redirectPath := filepath.Join(rigRelics, "redirect")
	if err := os.WriteFile(redirectPath, []byte("wrong/path\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Apply fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify redirect was corrected
	content, err := os.ReadFile(redirectPath)
	if err != nil {
		t.Fatalf("redirect file not found: %v", err)
	}
	if string(content) != "warchief/warband/.relics\n" {
		t.Errorf("redirect content = %q, want 'warchief/warband/.relics\\n'", string(content))
	}

	// Verify check now passes
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v", result.Status)
	}
}

func TestRelicsRedirectCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create tracked relics at warchief/warband/.relics
	trackedRelics := filepath.Join(rigDir, "warchief", "warband", ".relics")
	if err := os.MkdirAll(trackedRelics, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Apply fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify redirect file was created
	redirectPath := filepath.Join(rigDir, ".relics", "redirect")
	content, err := os.ReadFile(redirectPath)
	if err != nil {
		t.Fatalf("redirect file not created: %v", err)
	}

	expected := "warchief/warband/.relics\n"
	if string(content) != expected {
		t.Errorf("redirect content = %q, want %q", string(content), expected)
	}

	// Verify check now passes
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v", result.Status)
	}
}

func TestRelicsRedirectCheck_FixNoOp_LocalRelics(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create only local relics (no tracked relics)
	localRelics := filepath.Join(rigDir, ".relics")
	if err := os.MkdirAll(localRelics, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Fix should be a no-op
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify no redirect was created
	redirectPath := filepath.Join(rigDir, ".relics", "redirect")
	if _, err := os.Stat(redirectPath); !os.IsNotExist(err) {
		t.Error("redirect file should not be created for local relics")
	}
}

func TestRelicsRedirectCheck_FixInitRelics(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create warband directory (no relics at all)
	if err := os.MkdirAll(rigDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create warchief/warbands.json with prefix for the warband
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatal(err)
	}
	rigsJSON := `{
		"version": 1,
		"warbands": {
			"testrig": {
				"git_url": "https://example.com/test.git",
				"relics": {
					"prefix": "tr"
				}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(warchiefDir, "warbands.json"), []byte(rigsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Apply fix - this will run 'bd init' if available, otherwise create config.yaml
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify .relics directory was created
	relicsDir := filepath.Join(rigDir, ".relics")
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		t.Fatal(".relics directory not created")
	}

	// Verify relics was initialized (either by rl init or fallback)
	// rl init creates config.yaml, fallback creates config.yaml with prefix
	configPath := filepath.Join(relicsDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config.yaml not created")
	}

	// Verify check now passes (local relics exist)
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v", result.Status)
	}
}

func TestRelicsRedirectCheck_ConflictingLocalRelics(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create tracked relics at warchief/warband/.relics
	trackedRelics := filepath.Join(rigDir, "warchief", "warband", ".relics")
	if err := os.MkdirAll(trackedRelics, 0755); err != nil {
		t.Fatal(err)
	}
	// Add some content to tracked relics
	if err := os.WriteFile(filepath.Join(trackedRelics, "issues.jsonl"), []byte(`{"id":"tr-1"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create conflicting local relics with actual data
	localRelics := filepath.Join(rigDir, ".relics")
	if err := os.MkdirAll(localRelics, 0755); err != nil {
		t.Fatal(err)
	}
	// Add data to local relics (this is the conflict)
	if err := os.WriteFile(filepath.Join(localRelics, "issues.jsonl"), []byte(`{"id":"local-1"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRelics, "config.yaml"), []byte("prefix: local\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Check should detect conflicting relics
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Errorf("expected StatusError for conflicting relics, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "Conflicting") {
		t.Errorf("expected message about conflicting relics, got %q", result.Message)
	}
}

func TestRelicsRedirectCheck_FixConflictingLocalRelics(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"
	rigDir := filepath.Join(tmpDir, rigName)

	// Create tracked relics at warchief/warband/.relics
	trackedRelics := filepath.Join(rigDir, "warchief", "warband", ".relics")
	if err := os.MkdirAll(trackedRelics, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trackedRelics, "issues.jsonl"), []byte(`{"id":"tr-1"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create conflicting local relics with actual data
	localRelics := filepath.Join(rigDir, ".relics")
	if err := os.MkdirAll(localRelics, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRelics, "issues.jsonl"), []byte(`{"id":"local-1"}`), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsRedirectCheck()
	ctx := &CheckContext{TownRoot: tmpDir, RigName: rigName}

	// Verify fix is needed
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before fix, got %v", result.Status)
	}

	// Apply fix - should remove conflicting local relics and create redirect
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify local issues.jsonl was removed
	if _, err := os.Stat(filepath.Join(localRelics, "issues.jsonl")); !os.IsNotExist(err) {
		t.Error("local issues.jsonl should have been removed")
	}

	// Verify redirect was created
	redirectPath := filepath.Join(localRelics, "redirect")
	content, err := os.ReadFile(redirectPath)
	if err != nil {
		t.Fatalf("redirect file not created: %v", err)
	}
	if string(content) != "warchief/warband/.relics\n" {
		t.Errorf("redirect content = %q, want 'warchief/warband/.relics\\n'", string(content))
	}

	// Verify check now passes
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v", result.Status)
	}
}
