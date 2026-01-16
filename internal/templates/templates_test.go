package templates

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	tmpl, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if tmpl == nil {
		t.Fatal("New() returned nil")
	}
}

func TestRenderRole_Warchief(t *testing.T) {
	tmpl, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	data := RoleData{
		Role:          "warchief",
		TownRoot:      "/test/encampment",
		TownName:      "encampment",
		WorkDir:       "/test/encampment",
		DefaultBranch: "main",
		WarchiefSession:  "hd-encampment-warchief",
		ShamanSession: "hd-encampment-shaman",
	}

	output, err := tmpl.RenderRole("warchief", data)
	if err != nil {
		t.Fatalf("RenderRole() error = %v", err)
	}

	// Check for key content
	if !strings.Contains(output, "Warchief Context") {
		t.Error("output missing 'Warchief Context'")
	}
	if !strings.Contains(output, "/test/encampment") {
		t.Error("output missing encampment root")
	}
	if !strings.Contains(output, "global coordinator") {
		t.Error("output missing role description")
	}
}

func TestRenderRole_Raider(t *testing.T) {
	tmpl, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	data := RoleData{
		Role:          "raider",
		RigName:       "myrig",
		TownRoot:      "/test/encampment",
		TownName:      "encampment",
		WorkDir:       "/test/encampment/myrig/raiders/TestCat",
		DefaultBranch: "main",
		Raider:       "TestCat",
		WarchiefSession:  "hd-encampment-warchief",
		ShamanSession: "hd-encampment-shaman",
	}

	output, err := tmpl.RenderRole("raider", data)
	if err != nil {
		t.Fatalf("RenderRole() error = %v", err)
	}

	// Check for key content
	if !strings.Contains(output, "Raider Context") {
		t.Error("output missing 'Raider Context'")
	}
	if !strings.Contains(output, "TestCat") {
		t.Error("output missing raider name")
	}
	if !strings.Contains(output, "myrig") {
		t.Error("output missing warband name")
	}
}

func TestRenderRole_Shaman(t *testing.T) {
	tmpl, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	data := RoleData{
		Role:          "shaman",
		TownRoot:      "/test/encampment",
		TownName:      "encampment",
		WorkDir:       "/test/encampment",
		DefaultBranch: "main",
		WarchiefSession:  "hd-encampment-warchief",
		ShamanSession: "hd-encampment-shaman",
	}

	output, err := tmpl.RenderRole("shaman", data)
	if err != nil {
		t.Fatalf("RenderRole() error = %v", err)
	}

	// Check for key content
	if !strings.Contains(output, "Shaman Context") {
		t.Error("output missing 'Shaman Context'")
	}
	if !strings.Contains(output, "/test/encampment") {
		t.Error("output missing encampment root")
	}
	if !strings.Contains(output, "Scout Executor") {
		t.Error("output missing role description")
	}
	if !strings.Contains(output, "Startup Protocol: Propulsion") {
		t.Error("output missing startup protocol section")
	}
	if !strings.Contains(output, "totem-shaman-scout") {
		t.Error("output missing scout totem reference")
	}
}

func TestRenderRole_Forge_DefaultBranch(t *testing.T) {
	tmpl, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Test with custom default branch (e.g., "develop")
	data := RoleData{
		Role:          "forge",
		RigName:       "myrig",
		TownRoot:      "/test/encampment",
		TownName:      "encampment",
		WorkDir:       "/test/encampment/myrig/forge/warband",
		DefaultBranch: "develop",
		WarchiefSession:  "hd-encampment-warchief",
		ShamanSession: "hd-encampment-shaman",
	}

	output, err := tmpl.RenderRole("forge", data)
	if err != nil {
		t.Fatalf("RenderRole() error = %v", err)
	}

	// Check that the custom default branch is used in git commands
	if !strings.Contains(output, "origin/develop") {
		t.Error("output missing 'origin/develop' - DefaultBranch not being used for rebase")
	}
	if !strings.Contains(output, "git checkout develop") {
		t.Error("output missing 'git checkout develop' - DefaultBranch not being used for checkout")
	}
	if !strings.Contains(output, "git push origin develop") {
		t.Error("output missing 'git push origin develop' - DefaultBranch not being used for push")
	}

	// Verify it does NOT contain hardcoded "main" in git commands
	// (main may appear in other contexts like "main branch" descriptions, so we check specific patterns)
	if strings.Contains(output, "git rebase origin/main") {
		t.Error("output still contains hardcoded 'git rebase origin/main' - should use DefaultBranch")
	}
	if strings.Contains(output, "git checkout main") {
		t.Error("output still contains hardcoded 'git checkout main' - should use DefaultBranch")
	}
	if strings.Contains(output, "git push origin main") {
		t.Error("output still contains hardcoded 'git push origin main' - should use DefaultBranch")
	}
}

func TestRenderMessage_Spawn(t *testing.T) {
	tmpl, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	data := SpawnData{
		Issue:       "hd-123",
		Title:       "Test Issue",
		Priority:    1,
		Description: "Test description",
		Branch:      "feature/test",
		RigName:     "myrig",
		Raider:     "TestCat",
	}

	output, err := tmpl.RenderMessage("muster", data)
	if err != nil {
		t.Fatalf("RenderMessage() error = %v", err)
	}

	// Check for key content
	if !strings.Contains(output, "hd-123") {
		t.Error("output missing issue ID")
	}
	if !strings.Contains(output, "Test Issue") {
		t.Error("output missing issue title")
	}
}

func TestRenderMessage_Nudge(t *testing.T) {
	tmpl, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	data := NudgeData{
		Raider:    "TestCat",
		Reason:     "No progress for 30 minutes",
		NudgeCount: 2,
		MaxNudges:  3,
		Issue:      "hd-123",
		Status:     "in_progress",
	}

	output, err := tmpl.RenderMessage("signal", data)
	if err != nil {
		t.Fatalf("RenderMessage() error = %v", err)
	}

	// Check for key content
	if !strings.Contains(output, "TestCat") {
		t.Error("output missing raider name")
	}
	if !strings.Contains(output, "2/3") {
		t.Error("output missing signal count")
	}
}

func TestRoleNames(t *testing.T) {
	tmpl, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	names := tmpl.RoleNames()
	expected := []string{"warchief", "witness", "forge", "raider", "clan", "shaman"}

	if len(names) != len(expected) {
		t.Errorf("RoleNames() = %v, want %v", names, expected)
	}

	for i, name := range names {
		if name != expected[i] {
			t.Errorf("RoleNames()[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestGetAllRoleTemplates(t *testing.T) {
	templates, err := GetAllRoleTemplates()
	if err != nil {
		t.Fatalf("GetAllRoleTemplates() error = %v", err)
	}

	if len(templates) == 0 {
		t.Fatal("GetAllRoleTemplates() returned empty map")
	}

	expectedFiles := []string{
		"shaman.md.tmpl",
		"witness.md.tmpl",
		"forge.md.tmpl",
		"warchief.md.tmpl",
		"raider.md.tmpl",
		"clan.md.tmpl",
	}

	for _, file := range expectedFiles {
		content, ok := templates[file]
		if !ok {
			t.Errorf("GetAllRoleTemplates() missing %s", file)
			continue
		}
		if len(content) == 0 {
			t.Errorf("GetAllRoleTemplates()[%s] has empty content", file)
		}
	}
}

func TestGetAllRoleTemplates_ContentValidity(t *testing.T) {
	templates, err := GetAllRoleTemplates()
	if err != nil {
		t.Fatalf("GetAllRoleTemplates() error = %v", err)
	}

	for name, content := range templates {
		if !strings.HasSuffix(name, ".md.tmpl") {
			t.Errorf("unexpected file %s (should end with .md.tmpl)", name)
		}
		contentStr := string(content)
		if !strings.Contains(contentStr, "Context") {
			t.Errorf("%s doesn't contain 'Context' - may not be a valid role template", name)
		}
	}
}
