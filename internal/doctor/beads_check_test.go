package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OWNER/horde/internal/relics"
)

func TestNewRelicsDatabaseCheck(t *testing.T) {
	check := NewRelicsDatabaseCheck()

	if check.Name() != "relics-database" {
		t.Errorf("expected name 'relics-database', got %q", check.Name())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestRelicsDatabaseCheck_NoRelicsDir(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewRelicsDatabaseCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning, got %v", result.Status)
	}
}

func TestRelicsDatabaseCheck_NoDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsDatabaseCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK, got %v", result.Status)
	}
}

func TestRelicsDatabaseCheck_EmptyDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create empty database
	dbPath := filepath.Join(relicsDir, "issues.db")
	if err := os.WriteFile(dbPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	// Create JSONL with content
	jsonlPath := filepath.Join(relicsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsDatabaseCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for empty db with content in jsonl, got %v", result.Status)
	}
}

func TestRelicsDatabaseCheck_PopulatedDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create database with content
	dbPath := filepath.Join(relicsDir, "issues.db")
	if err := os.WriteFile(dbPath, []byte("SQLite format 3"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewRelicsDatabaseCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for populated db, got %v", result.Status)
	}
}

func TestNewPrefixMismatchCheck(t *testing.T) {
	check := NewPrefixMismatchCheck()

	if check.Name() != "prefix-mismatch" {
		t.Errorf("expected name 'prefix-mismatch', got %q", check.Name())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestPrefixMismatchCheck_NoRoutes(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewPrefixMismatchCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for no routes, got %v", result.Status)
	}
}

func TestPrefixMismatchCheck_NoRigsJson(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create routes.jsonl
	routesPath := filepath.Join(relicsDir, "routes.jsonl")
	routesContent := `{"prefix":"hd-","path":"horde/warchief/warband"}`
	if err := os.WriteFile(routesPath, []byte(routesContent), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewPrefixMismatchCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no warbands.json, got %v", result.Status)
	}
}

func TestPrefixMismatchCheck_Matching(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create routes.jsonl with gt- prefix
	routesPath := filepath.Join(relicsDir, "routes.jsonl")
	routesContent := `{"prefix":"hd-","path":"horde/warchief/warband"}`
	if err := os.WriteFile(routesPath, []byte(routesContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create warbands.json with matching hd prefix
	rigsPath := filepath.Join(warchiefDir, "warbands.json")
	rigsContent := `{
		"version": 1,
		"warbands": {
			"horde": {
				"git_url": "https://github.com/example/horde",
				"relics": {
					"prefix": "hd"
				}
			}
		}
	}`
	if err := os.WriteFile(rigsPath, []byte(rigsContent), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewPrefixMismatchCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for matching prefixes, got %v: %s", result.Status, result.Message)
	}
}

func TestPrefixMismatchCheck_Mismatch(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create routes.jsonl with gt- prefix
	routesPath := filepath.Join(relicsDir, "routes.jsonl")
	routesContent := `{"prefix":"hd-","path":"horde/warchief/warband"}`
	if err := os.WriteFile(routesPath, []byte(routesContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create warbands.json with WRONG prefix (ga instead of gt)
	rigsPath := filepath.Join(warchiefDir, "warbands.json")
	rigsContent := `{
		"version": 1,
		"warbands": {
			"horde": {
				"git_url": "https://github.com/example/horde",
				"relics": {
					"prefix": "ga"
				}
			}
		}
	}`
	if err := os.WriteFile(rigsPath, []byte(rigsContent), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewPrefixMismatchCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning for prefix mismatch, got %v: %s", result.Status, result.Message)
	}

	if len(result.Details) != 1 {
		t.Errorf("expected 1 detail, got %d", len(result.Details))
	}
}

func TestPrefixMismatchCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	warchiefDir := filepath.Join(tmpDir, "warchief")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create routes.jsonl with gt- prefix
	routesPath := filepath.Join(relicsDir, "routes.jsonl")
	routesContent := `{"prefix":"hd-","path":"horde/warchief/warband"}`
	if err := os.WriteFile(routesPath, []byte(routesContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create warbands.json with WRONG prefix (ga instead of gt)
	rigsPath := filepath.Join(warchiefDir, "warbands.json")
	rigsContent := `{
		"version": 1,
		"warbands": {
			"horde": {
				"git_url": "https://github.com/example/horde",
				"relics": {
					"prefix": "ga"
				}
			}
		}
	}`
	if err := os.WriteFile(rigsPath, []byte(rigsContent), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewPrefixMismatchCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	// First verify there's a mismatch
	result := check.Run(ctx)
	if result.Status != StatusWarning {
		t.Fatalf("expected mismatch before fix, got %v", result.Status)
	}

	// Fix it
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix() failed: %v", err)
	}

	// Verify it's now fixed
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v: %s", result.Status, result.Message)
	}

	// Verify warbands.json was updated
	data, err := os.ReadFile(rigsPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("failed to load fixed warbands.json: %v (content: %s)", err, data)
	}
	if cfg.Warbands["horde"].RelicsConfig.Prefix != "hd" {
		t.Errorf("expected prefix 'gt' after fix, got %q", cfg.Warbands["horde"].RelicsConfig.Prefix)
	}
}

func TestNewRoleLabelCheck(t *testing.T) {
	check := NewRoleLabelCheck()

	if check.Name() != "role-bead-labels" {
		t.Errorf("expected name 'role-bead-labels', got %q", check.Name())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestRoleLabelCheck_NoRelicsDir(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewRoleLabelCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no .relics dir, got %v", result.Status)
	}
	if result.Message != "No relics database (skipped)" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

// mockBeadShower implements beadShower for testing
type mockBeadShower struct {
	relics map[string]*relics.Issue
}

func (m *mockBeadShower) Show(id string) (*relics.Issue, error) {
	if issue, ok := m.relics[id]; ok {
		return issue, nil
	}
	return nil, relics.ErrNotFound
}

// mockLabelAdder implements labelAdder for testing
type mockLabelAdder struct {
	calls []labelAddCall
}

type labelAddCall struct {
	townRoot string
	id       string
	label    string
}

func (m *mockLabelAdder) AddLabel(townRoot, id, label string) error {
	m.calls = append(m.calls, labelAddCall{townRoot, id, label})
	return nil
}

func TestRoleLabelCheck_AllRelicsHaveLabel(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock with all role relics having gt:role label
	mock := &mockBeadShower{
		relics: map[string]*relics.Issue{
			"hq-warchief-role":    {ID: "hq-warchief-role", Labels: []string{"gt:role"}},
			"hq-shaman-role":   {ID: "hq-shaman-role", Labels: []string{"gt:role"}},
			"hq-dog-role":      {ID: "hq-dog-role", Labels: []string{"gt:role"}},
			"hq-witness-role":  {ID: "hq-witness-role", Labels: []string{"gt:role"}},
			"hq-forge-role": {ID: "hq-forge-role", Labels: []string{"gt:role"}},
			"hq-raider-role":  {ID: "hq-raider-role", Labels: []string{"gt:role"}},
			"hq-clan-role":     {ID: "hq-clan-role", Labels: []string{"gt:role"}},
		},
	}

	check := NewRoleLabelCheck()
	check.beadShower = mock
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when all relics have label, got %v: %s", result.Status, result.Message)
	}
	if result.Message != "All role relics have gt:role label" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestRoleLabelCheck_MissingLabel(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock with witness-role missing the gt:role label (the regression case)
	mock := &mockBeadShower{
		relics: map[string]*relics.Issue{
			"hq-warchief-role":    {ID: "hq-warchief-role", Labels: []string{"gt:role"}},
			"hq-shaman-role":   {ID: "hq-shaman-role", Labels: []string{"gt:role"}},
			"hq-dog-role":      {ID: "hq-dog-role", Labels: []string{"gt:role"}},
			"hq-witness-role":  {ID: "hq-witness-role", Labels: []string{}}, // Missing gt:role!
			"hq-forge-role": {ID: "hq-forge-role", Labels: []string{"gt:role"}},
			"hq-raider-role":  {ID: "hq-raider-role", Labels: []string{"gt:role"}},
			"hq-clan-role":     {ID: "hq-clan-role", Labels: []string{"gt:role"}},
		},
	}

	check := NewRoleLabelCheck()
	check.beadShower = mock
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning when label missing, got %v", result.Status)
	}
	if result.Message != "1 role bead(s) missing gt:role label" {
		t.Errorf("unexpected message: %s", result.Message)
	}
	if len(result.Details) != 1 || result.Details[0] != "hq-witness-role" {
		t.Errorf("expected details to contain hq-witness-role, got %v", result.Details)
	}
}

func TestRoleLabelCheck_MultipleMissingLabels(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock with multiple relics missing the gt:role label
	mock := &mockBeadShower{
		relics: map[string]*relics.Issue{
			"hq-warchief-role":    {ID: "hq-warchief-role", Labels: []string{}},    // Missing
			"hq-shaman-role":   {ID: "hq-shaman-role", Labels: []string{}},   // Missing
			"hq-dog-role":      {ID: "hq-dog-role", Labels: []string{"gt:role"}},
			"hq-witness-role":  {ID: "hq-witness-role", Labels: []string{}},  // Missing
			"hq-forge-role": {ID: "hq-forge-role", Labels: []string{}}, // Missing
			"hq-raider-role":  {ID: "hq-raider-role", Labels: []string{"gt:role"}},
			"hq-clan-role":     {ID: "hq-clan-role", Labels: []string{"gt:role"}},
		},
	}

	check := NewRoleLabelCheck()
	check.beadShower = mock
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning, got %v", result.Status)
	}
	if result.Message != "4 role bead(s) missing gt:role label" {
		t.Errorf("unexpected message: %s", result.Message)
	}
	if len(result.Details) != 4 {
		t.Errorf("expected 4 details, got %d: %v", len(result.Details), result.Details)
	}
}

func TestRoleLabelCheck_BeadNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock with only some relics existing (others return ErrNotFound)
	mock := &mockBeadShower{
		relics: map[string]*relics.Issue{
			"hq-warchief-role":  {ID: "hq-warchief-role", Labels: []string{"gt:role"}},
			"hq-shaman-role": {ID: "hq-shaman-role", Labels: []string{"gt:role"}},
			// Other relics don't exist - should be skipped, not reported as errors
		},
	}

	check := NewRoleLabelCheck()
	check.beadShower = mock
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	// Should be OK - missing relics are not an error (install will create them)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when relics don't exist, got %v: %s", result.Status, result.Message)
	}
}

func TestRoleLabelCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock with witness-role missing the label
	mockShower := &mockBeadShower{
		relics: map[string]*relics.Issue{
			"hq-warchief-role":   {ID: "hq-warchief-role", Labels: []string{"gt:role"}},
			"hq-witness-role": {ID: "hq-witness-role", Labels: []string{}}, // Missing gt:role
		},
	}
	mockAdder := &mockLabelAdder{}

	check := NewRoleLabelCheck()
	check.beadShower = mockShower
	check.labelAdder = mockAdder
	ctx := &CheckContext{TownRoot: tmpDir}

	// First run to detect the issue
	result := check.Run(ctx)
	if result.Status != StatusWarning {
		t.Fatalf("expected StatusWarning, got %v", result.Status)
	}

	// Now fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix() failed: %v", err)
	}

	// Verify the correct rl label add command was called
	if len(mockAdder.calls) != 1 {
		t.Fatalf("expected 1 AddLabel call, got %d", len(mockAdder.calls))
	}
	call := mockAdder.calls[0]
	if call.townRoot != tmpDir {
		t.Errorf("expected townRoot %q, got %q", tmpDir, call.townRoot)
	}
	if call.id != "hq-witness-role" {
		t.Errorf("expected id 'hq-witness-role', got %q", call.id)
	}
	if call.label != "gt:role" {
		t.Errorf("expected label 'gt:role', got %q", call.label)
	}
}

func TestRoleLabelCheck_FixMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock with multiple relics missing the label
	mockShower := &mockBeadShower{
		relics: map[string]*relics.Issue{
			"hq-warchief-role":    {ID: "hq-warchief-role", Labels: []string{}},    // Missing
			"hq-shaman-role":   {ID: "hq-shaman-role", Labels: []string{"gt:role"}},
			"hq-witness-role":  {ID: "hq-witness-role", Labels: []string{}},  // Missing
			"hq-forge-role": {ID: "hq-forge-role", Labels: []string{}}, // Missing
		},
	}
	mockAdder := &mockLabelAdder{}

	check := NewRoleLabelCheck()
	check.beadShower = mockShower
	check.labelAdder = mockAdder
	ctx := &CheckContext{TownRoot: tmpDir}

	// First run to detect the issues
	result := check.Run(ctx)
	if result.Status != StatusWarning {
		t.Fatalf("expected StatusWarning, got %v", result.Status)
	}
	if len(result.Details) != 3 {
		t.Fatalf("expected 3 missing, got %d", len(result.Details))
	}

	// Now fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix() failed: %v", err)
	}

	// Verify all 3 relics got the label added
	if len(mockAdder.calls) != 3 {
		t.Fatalf("expected 3 AddLabel calls, got %d", len(mockAdder.calls))
	}

	// Verify each call has the correct label
	for _, call := range mockAdder.calls {
		if call.label != "gt:role" {
			t.Errorf("expected label 'gt:role', got %q", call.label)
		}
	}
}
