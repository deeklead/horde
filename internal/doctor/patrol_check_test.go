package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/deeklead/horde/internal/config"
)

func TestNewPatrolRolesHavePromptsCheck(t *testing.T) {
	check := NewPatrolRolesHavePromptsCheck()
	if check == nil {
		t.Fatal("NewPatrolRolesHavePromptsCheck() returned nil")
	}
	if check.Name() != "scout-roles-have-prompts" {
		t.Errorf("Name() = %q, want %q", check.Name(), "scout-roles-have-prompts")
	}
	if !check.CanFix() {
		t.Error("CanFix() should return true")
	}
}

func setupRigConfig(t *testing.T, tmpDir string, rigNames []string) {
	t.Helper()
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir warchief: %v", err)
	}

	rigsConfig := config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	for _, name := range rigNames {
		rigsConfig.Warbands[name] = config.RigEntry{}
	}

	data, err := json.Marshal(rigsConfig)
	if err != nil {
		t.Fatalf("marshal warbands.json: %v", err)
	}

	if err := os.WriteFile(filepath.Join(warchiefDir, "warbands.json"), data, 0644); err != nil {
		t.Fatalf("write warbands.json: %v", err)
	}
}

func setupRigTemplatesDir(t *testing.T, tmpDir, rigName string) string {
	t.Helper()
	templatesDir := filepath.Join(tmpDir, rigName, "warchief", "warband", "internal", "templates", "roles")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	return templatesDir
}

func TestPatrolRolesHavePromptsCheck_NoRigs(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want OK (no warbands configured)", result.Status)
	}
}

func TestPatrolRolesHavePromptsCheck_NoTemplatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"myproject"})

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want Warning", result.Status)
	}
	if len(check.missingByRig) != 1 {
		t.Errorf("missingByRig count = %d, want 1", len(check.missingByRig))
	}
	if len(check.missingByRig["myproject"]) != 3 {
		t.Errorf("missing templates for myproject = %d, want 3", len(check.missingByRig["myproject"]))
	}
}

func TestPatrolRolesHavePromptsCheck_SomeTemplatesMissing(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"myproject"})
	templatesDir := setupRigTemplatesDir(t, tmpDir, "myproject")

	if err := os.WriteFile(filepath.Join(templatesDir, "shaman.md.tmpl"), []byte("test"), 0644); err != nil {
		t.Fatalf("write shaman.md.tmpl: %v", err)
	}

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want Warning", result.Status)
	}
	if len(check.missingByRig["myproject"]) != 2 {
		t.Errorf("missing templates = %d, want 2 (witness, forge)", len(check.missingByRig["myproject"]))
	}
}

func TestPatrolRolesHavePromptsCheck_AllTemplatesExist(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"myproject"})
	templatesDir := setupRigTemplatesDir(t, tmpDir, "myproject")

	for _, tmpl := range requiredRolePrompts {
		if err := os.WriteFile(filepath.Join(templatesDir, tmpl), []byte("test content"), 0644); err != nil {
			t.Fatalf("write %s: %v", tmpl, err)
		}
	}

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want OK", result.Status)
	}
	if len(check.missingByRig) != 0 {
		t.Errorf("missingByRig count = %d, want 0", len(check.missingByRig))
	}
}

func TestPatrolRolesHavePromptsCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"myproject"})

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)
	if result.Status != StatusWarning {
		t.Fatalf("Initial Status = %v, want Warning", result.Status)
	}

	err := check.Fix(ctx)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	templatesDir := filepath.Join(tmpDir, "myproject", "warchief", "warband", "internal", "templates", "roles")
	for _, tmpl := range requiredRolePrompts {
		path := filepath.Join(templatesDir, tmpl)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("Fix() did not create %s: %v", tmpl, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("Fix() created empty file %s", tmpl)
		}
	}

	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("After Fix(), Status = %v, want OK", result.Status)
	}
}

func TestPatrolRolesHavePromptsCheck_FixPartial(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"myproject"})
	templatesDir := setupRigTemplatesDir(t, tmpDir, "myproject")

	existingContent := []byte("existing custom content")
	if err := os.WriteFile(filepath.Join(templatesDir, "shaman.md.tmpl"), existingContent, 0644); err != nil {
		t.Fatalf("write shaman.md.tmpl: %v", err)
	}

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)
	if result.Status != StatusWarning {
		t.Fatalf("Initial Status = %v, want Warning", result.Status)
	}
	if len(check.missingByRig["myproject"]) != 2 {
		t.Fatalf("missing = %d, want 2", len(check.missingByRig["myproject"]))
	}

	err := check.Fix(ctx)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	shamanContent, err := os.ReadFile(filepath.Join(templatesDir, "shaman.md.tmpl"))
	if err != nil {
		t.Fatalf("read shaman.md.tmpl: %v", err)
	}
	if string(shamanContent) != string(existingContent) {
		t.Error("Fix() should not overwrite existing shaman.md.tmpl")
	}

	for _, tmpl := range []string{"witness.md.tmpl", "forge.md.tmpl"} {
		path := filepath.Join(templatesDir, tmpl)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Fix() did not create %s: %v", tmpl, err)
		}
	}
}

func TestPatrolRolesHavePromptsCheck_MultipleRigs(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"project1", "project2"})

	templatesDir1 := setupRigTemplatesDir(t, tmpDir, "project1")
	for _, tmpl := range requiredRolePrompts {
		if err := os.WriteFile(filepath.Join(templatesDir1, tmpl), []byte("test"), 0644); err != nil {
			t.Fatalf("write %s: %v", tmpl, err)
		}
	}

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want Warning (project2 missing)", result.Status)
	}
	if _, ok := check.missingByRig["project1"]; ok {
		t.Error("project1 should not be in missingByRig")
	}
	if len(check.missingByRig["project2"]) != 3 {
		t.Errorf("project2 missing = %d, want 3", len(check.missingByRig["project2"]))
	}
}

func TestPatrolRolesHavePromptsCheck_FixHint(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"myproject"})

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.FixHint == "" {
		t.Error("FixHint should not be empty for warning status")
	}
	if result.FixHint != "Run 'hd doctor --fix' to copy embedded templates to warband repos" {
		t.Errorf("FixHint = %q, unexpected value", result.FixHint)
	}
}

func TestPatrolRolesHavePromptsCheck_FixMultipleRigs(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"project1", "project2", "project3"})

	templatesDir1 := setupRigTemplatesDir(t, tmpDir, "project1")
	for _, tmpl := range requiredRolePrompts {
		if err := os.WriteFile(filepath.Join(templatesDir1, tmpl), []byte("existing"), 0644); err != nil {
			t.Fatalf("write %s: %v", tmpl, err)
		}
	}

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)
	if result.Status != StatusWarning {
		t.Fatalf("Initial Status = %v, want Warning", result.Status)
	}
	if len(check.missingByRig) != 2 {
		t.Fatalf("missingByRig count = %d, want 2 (project2, project3)", len(check.missingByRig))
	}

	err := check.Fix(ctx)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	for _, warband := range []string{"project2", "project3"} {
		templatesDir := filepath.Join(tmpDir, warband, "warchief", "warband", "internal", "templates", "roles")
		for _, tmpl := range requiredRolePrompts {
			path := filepath.Join(templatesDir, tmpl)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("Fix() did not create %s for %s: %v", tmpl, warband, err)
			}
		}
	}

	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("After Fix(), Status = %v, want OK", result.Status)
	}
}

func TestPatrolRolesHavePromptsCheck_DetailsFormat(t *testing.T) {
	tmpDir := t.TempDir()
	setupRigConfig(t, tmpDir, []string{"myproject"})

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if len(result.Details) != 3 {
		t.Fatalf("Details count = %d, want 3", len(result.Details))
	}

	for _, detail := range result.Details {
		if detail[:10] != "myproject:" {
			t.Errorf("Detail %q should be prefixed with 'myproject:'", detail)
		}
	}
}

func TestPatrolRolesHavePromptsCheck_MalformedRigsJSON(t *testing.T) {
	tmpDir := t.TempDir()
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir warchief: %v", err)
	}
	if err := os.WriteFile(filepath.Join(warchiefDir, "warbands.json"), []byte("not valid json"), 0644); err != nil {
		t.Fatalf("write warbands.json: %v", err)
	}

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("Status = %v, want Error for malformed warbands.json", result.Status)
	}
}

func TestPatrolRolesHavePromptsCheck_EmptyRigsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir warchief: %v", err)
	}
	if err := os.WriteFile(filepath.Join(warchiefDir, "warbands.json"), []byte(`{"warbands":{}}`), 0644); err != nil {
		t.Fatalf("write warbands.json: %v", err)
	}

	check := NewPatrolRolesHavePromptsCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want OK for empty warbands config", result.Status)
	}
	if result.Message != "No warbands configured" {
		t.Errorf("Message = %q, want 'No warbands configured'", result.Message)
	}
}

func TestNewPatrolHooksWiredCheck(t *testing.T) {
	check := NewPatrolHooksWiredCheck()
	if check == nil {
		t.Fatal("NewPatrolHooksWiredCheck() returned nil")
	}
	if check.Name() != "scout-hooks-wired" {
		t.Errorf("Name() = %q, want %q", check.Name(), "scout-hooks-wired")
	}
	if !check.CanFix() {
		t.Error("CanFix() should return true")
	}
}

func TestPatrolHooksWiredCheck_NoDaemonConfig(t *testing.T) {
	tmpDir := t.TempDir()
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir warchief: %v", err)
	}

	check := NewPatrolHooksWiredCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want Warning", result.Status)
	}
	if result.FixHint == "" {
		t.Error("FixHint should not be empty")
	}
}

func TestPatrolHooksWiredCheck_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.NewDaemonPatrolConfig()
	path := config.DaemonPatrolConfigPath(tmpDir)
	if err := config.SaveDaemonPatrolConfig(path, cfg); err != nil {
		t.Fatalf("SaveDaemonPatrolConfig: %v", err)
	}

	check := NewPatrolHooksWiredCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want OK", result.Status)
	}
}

func TestPatrolHooksWiredCheck_EmptyPatrols(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.DaemonPatrolConfig{
		Type:    "daemon-scout-config",
		Version: 1,
		Patrols: map[string]config.PatrolConfig{},
	}
	path := config.DaemonPatrolConfigPath(tmpDir)
	if err := config.SaveDaemonPatrolConfig(path, cfg); err != nil {
		t.Fatalf("SaveDaemonPatrolConfig: %v", err)
	}

	check := NewPatrolHooksWiredCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want Warning (no patrols configured)", result.Status)
	}
}

func TestPatrolHooksWiredCheck_HeartbeatEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.DaemonPatrolConfig{
		Type:    "daemon-scout-config",
		Version: 1,
		Heartbeat: &config.HeartbeatConfig{
			Enabled:  true,
			Interval: "3m",
		},
		Patrols: map[string]config.PatrolConfig{},
	}
	path := config.DaemonPatrolConfigPath(tmpDir)
	if err := config.SaveDaemonPatrolConfig(path, cfg); err != nil {
		t.Fatalf("SaveDaemonPatrolConfig: %v", err)
	}

	check := NewPatrolHooksWiredCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want OK (heartbeat enabled triggers patrols)", result.Status)
	}
}

func TestPatrolHooksWiredCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir warchief: %v", err)
	}

	check := NewPatrolHooksWiredCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)
	if result.Status != StatusWarning {
		t.Fatalf("Initial Status = %v, want Warning", result.Status)
	}

	err := check.Fix(ctx)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	path := config.DaemonPatrolConfigPath(tmpDir)
	loaded, err := config.LoadDaemonPatrolConfig(path)
	if err != nil {
		t.Fatalf("LoadDaemonPatrolConfig: %v", err)
	}
	if loaded.Type != "daemon-scout-config" {
		t.Errorf("Type = %q, want 'daemon-scout-config'", loaded.Type)
	}
	if len(loaded.Patrols) != 3 {
		t.Errorf("Patrols count = %d, want 3", len(loaded.Patrols))
	}

	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("After Fix(), Status = %v, want OK", result.Status)
	}
}

func TestPatrolHooksWiredCheck_FixPreservesExisting(t *testing.T) {
	tmpDir := t.TempDir()

	existing := &config.DaemonPatrolConfig{
		Type:    "daemon-scout-config",
		Version: 1,
		Patrols: map[string]config.PatrolConfig{
			"custom": {Enabled: true, Agent: "custom-agent"},
		},
	}
	path := config.DaemonPatrolConfigPath(tmpDir)
	if err := config.SaveDaemonPatrolConfig(path, existing); err != nil {
		t.Fatalf("SaveDaemonPatrolConfig: %v", err)
	}

	check := NewPatrolHooksWiredCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("Status = %v, want OK (has patrols)", result.Status)
	}

	err := check.Fix(ctx)
	if err != nil {
		t.Fatalf("Fix() error = %v", err)
	}

	loaded, err := config.LoadDaemonPatrolConfig(path)
	if err != nil {
		t.Fatalf("LoadDaemonPatrolConfig: %v", err)
	}
	if len(loaded.Patrols) != 1 {
		t.Errorf("Patrols count = %d, want 1 (should preserve existing)", len(loaded.Patrols))
	}
	if _, ok := loaded.Patrols["custom"]; !ok {
		t.Error("existing custom scout was overwritten")
	}
}
