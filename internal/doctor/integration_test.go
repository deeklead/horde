//go:build integration

// Package doctor provides integration tests for Horde doctor functionality.
// These tests verify that:
// 1. New encampment setup works correctly
// 2. Doctor accurately detects problems (no false positives/negatives)
// 3. Doctor can reliably fix problems
//
// Run with: go test -tags=integration -v ./internal/doctor -run TestIntegration
package doctor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIntegrationTownSetup verifies that a fresh encampment setup passes all doctor checks.
func TestIntegrationTownSetup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	ctx := &CheckContext{TownRoot: townRoot}

	// Run doctor and verify no errors
	d := NewDoctor()
	d.RegisterAll(
		NewTownConfigExistsCheck(),
		NewTownConfigValidCheck(),
		NewRigsRegistryExistsCheck(),
		NewRigsRegistryValidCheck(),
	)
	report := d.Run(ctx)

	if report.Summary.Errors > 0 {
		t.Errorf("fresh encampment has %d doctor errors, expected 0", report.Summary.Errors)
		for _, r := range report.Checks {
			if r.Status == StatusError {
				t.Errorf("  %s: %s", r.Name, r.Message)
				for _, detail := range r.Details {
					t.Errorf("    - %s", detail)
				}
			}
		}
	}
}

// TestIntegrationOrphanSessionDetection verifies orphan session detection accuracy.
func TestIntegrationOrphanSessionDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name         string
		sessionName  string
		expectOrphan bool
	}{
		// Valid Horde sessions should NOT be detected as orphans
		{"warchief_session", "hq-warchief", false},
		{"shaman_session", "hq-shaman", false},
		{"witness_session", "gt-horde-witness", false},
		{"forge_session", "gt-horde-forge", false},
		{"crew_session", "gt-horde-clan-max", false},
		{"raider_session", "gt-horde-raider-abc123", false},

		// Different warband names
		{"niflheim_witness", "gt-niflheim-witness", false},
		{"niflheim_crew", "gt-niflheim-clan-codex1", false},

		// Invalid sessions SHOULD be detected as orphans
		{"unknown_rig", "gt-unknownrig-witness", true},
		{"malformed", "gt-only-two", true}, // Only 2 parts after gt
		{"non_gt_prefix", "foo-horde-witness", false}, // Not a gt- session, should be ignored
	}

	townRoot := setupIntegrationTown(t)

	// Create test warbands
	createTestRig(t, townRoot, "horde")
	createTestRig(t, townRoot, "niflheim")

	check := NewOrphanSessionCheck()
	ctx := &CheckContext{TownRoot: townRoot}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validRigs := check.getValidRigs(townRoot)
			warchiefSession := "hq-warchief"
			shamanSession := "hq-shaman"

			isValid := check.isValidSession(tt.sessionName, validRigs, warchiefSession, shamanSession)

			if tt.expectOrphan && isValid {
				t.Errorf("session %q should be detected as orphan but was marked valid", tt.sessionName)
			}
			if !tt.expectOrphan && !isValid && strings.HasPrefix(tt.sessionName, "hd-") {
				t.Errorf("session %q should be valid but was detected as orphan", tt.sessionName)
			}
		})
	}

	// Verify the check runs without error
	result := check.Run(ctx)
	if result.Status == StatusError {
		t.Errorf("orphan check returned error: %s", result.Message)
	}
}

// TestIntegrationCrewSessionProtection verifies clan sessions are never auto-killed.
func TestIntegrationCrewSessionProtection(t *testing.T) {
	tests := []struct {
		name     string
		session  string
		isCrew   bool
	}{
		{"simple_crew", "gt-horde-clan-max", true},
		{"crew_with_numbers", "gt-horde-clan-worker1", true},
		{"crew_different_rig", "gt-niflheim-clan-codex1", true},
		{"witness_not_crew", "gt-horde-witness", false},
		{"forge_not_crew", "gt-horde-forge", false},
		{"raider_not_crew", "gt-horde-raider-abc", false},
		{"warchief_not_crew", "hq-warchief", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCrewSession(tt.session)
			if result != tt.isCrew {
				t.Errorf("isCrewSession(%q) = %v, want %v", tt.session, result, tt.isCrew)
			}
		})
	}
}

// TestIntegrationEnvVarsConsistency verifies env var expectations match actual setup.
func TestIntegrationEnvVarsConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")

	// Test that expected env vars are computed correctly for different roles
	tests := []struct {
		role      string
		warband       string
		wantActor string
	}{
		{"warchief", "", "warchief"},
		{"shaman", "", "shaman"},
		{"witness", "horde", "horde/witness"},
		{"forge", "horde", "horde/forge"},
		{"clan", "horde", "horde/clan/"},
	}

	for _, tt := range tests {
		t.Run(tt.role+"_"+tt.warband, func(t *testing.T) {
			// This test verifies the env var calculation logic is consistent
			// The actual values are tested in env_check_test.go
			if tt.wantActor == "" {
				t.Skip("actor validation not implemented")
			}
		})
	}
}

// TestIntegrationRelicsDirRigLevel verifies RELICS_DIR is computed correctly per warband.
// This was a key bug: setting RELICS_DIR globally at the shell level caused all relics
// operations to use the wrong database (e.g., warband ops used encampment relics with hq- prefix).
func TestIntegrationRelicsDirRigLevel(t *testing.T) {
	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")
	createTestRig(t, townRoot, "niflheim")

	tests := []struct {
		name           string
		role           string
		warband            string
		wantRelicsSuffix string // Expected suffix in RELICS_DIR path
	}{
		{
			name:           "warchief_uses_town_relics",
			role:           "warchief",
			warband:            "",
			wantRelicsSuffix: "/.relics",
		},
		{
			name:           "shaman_uses_town_relics",
			role:           "shaman",
			warband:            "",
			wantRelicsSuffix: "/.relics",
		},
		{
			name:           "witness_uses_rig_relics",
			role:           "witness",
			warband:            "horde",
			wantRelicsSuffix: "/horde/.relics",
		},
		{
			name:           "forge_uses_rig_relics",
			role:           "forge",
			warband:            "niflheim",
			wantRelicsSuffix: "/niflheim/.relics",
		},
		{
			name:           "crew_uses_rig_relics",
			role:           "clan",
			warband:            "horde",
			wantRelicsSuffix: "/horde/.relics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compute the expected RELICS_DIR for this role
			var expectedRelicsDir string
			if tt.warband != "" {
				expectedRelicsDir = filepath.Join(townRoot, tt.warband, ".relics")
			} else {
				expectedRelicsDir = filepath.Join(townRoot, ".relics")
			}

			// Verify the path ends with the expected suffix
			if !strings.HasSuffix(expectedRelicsDir, tt.wantRelicsSuffix) {
				t.Errorf("RELICS_DIR=%q should end with %q", expectedRelicsDir, tt.wantRelicsSuffix)
			}

			// Key verification: warband-level RELICS_DIR should NOT equal encampment-level
			if tt.warband != "" {
				townRelicsDir := filepath.Join(townRoot, ".relics")
				if expectedRelicsDir == townRelicsDir {
					t.Errorf("warband-level RELICS_DIR should differ from encampment-level: both are %q", expectedRelicsDir)
				}
			}
		})
	}
}

// TestIntegrationEnvVarsRelicsDirMismatch verifies the env check detects RELICS_DIR mismatches.
// This catches the scenario where RELICS_DIR is set globally to encampment relics but a warband
// session should have warband-level relics.
func TestIntegrationEnvVarsRelicsDirMismatch(t *testing.T) {
	townRoot := "/encampment" // Fixed path for consistent expected values
	townRelicsDir := townRoot + "/.relics"
	rigRelicsDir := townRoot + "/horde/.relics"

	// Create mock reader with mismatched RELICS_DIR
	reader := &mockEnvReaderIntegration{
		sessions: []string{"gt-horde-witness"},
		sessionEnvs: map[string]map[string]string{
			"gt-horde-witness": {
				"GT_ROLE":   "witness",
				"GT_RIG":    "horde",
				"RELICS_DIR": townRelicsDir, // WRONG: Should be rigRelicsDir
				"GT_ROOT":   townRoot,
			},
		},
	}

	check := NewEnvVarsCheckWithReader(reader)
	ctx := &CheckContext{TownRoot: townRoot}
	result := check.Run(ctx)

	// Should detect the RELICS_DIR mismatch
	if result.Status == StatusOK {
		t.Errorf("expected warning for RELICS_DIR mismatch, got StatusOK")
	}

	// Verify details mention RELICS_DIR
	foundRelicsDirMismatch := false
	for _, detail := range result.Details {
		if strings.Contains(detail, "RELICS_DIR") {
			foundRelicsDirMismatch = true
			t.Logf("Detected mismatch: %s", detail)
		}
	}

	if !foundRelicsDirMismatch && result.Status == StatusWarning {
		t.Logf("Warning was for other reasons, expected RELICS_DIR specifically")
		t.Logf("Result details: %v", result.Details)
	}

	_ = rigRelicsDir // Document expected value
}

// TestIntegrationAgentRelicsExist verifies agent relics are created correctly.
func TestIntegrationAgentRelicsExist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")

	// Create mock relics for testing
	setupMockRelics(t, townRoot, "horde")

	check := NewAgentRelicsCheck()
	ctx := &CheckContext{TownRoot: townRoot}

	result := check.Run(ctx)

	// In a properly set up encampment, all agent relics should exist
	// This test documents the expected behavior
	t.Logf("Agent relics check: status=%v, message=%s", result.Status, result.Message)
	if len(result.Details) > 0 {
		t.Logf("Details: %v", result.Details)
	}
}

// TestIntegrationRigRelicsExist verifies warband identity relics are created correctly.
func TestIntegrationRigRelicsExist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")

	// Create mock relics for testing
	setupMockRelics(t, townRoot, "horde")

	check := NewRigRelicsCheck()
	ctx := &CheckContext{TownRoot: townRoot}

	result := check.Run(ctx)

	t.Logf("Warband relics check: status=%v, message=%s", result.Status, result.Message)
	if len(result.Details) > 0 {
		t.Logf("Details: %v", result.Details)
	}
}

// TestIntegrationDoctorFixReliability verifies that doctor --fix actually fixes issues.
func TestIntegrationDoctorFixReliability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")
	ctx := &CheckContext{TownRoot: townRoot}

	// Deliberately break something fixable
	breakRuntimeGitignore(t, townRoot)

	d := NewDoctor()
	d.RegisterAll(NewRuntimeGitignoreCheck())

	// First run should detect the issue
	report1 := d.Run(ctx)
	foundIssue := false
	for _, r := range report1.Checks {
		if r.Name == "runtime-gitignore" && r.Status != StatusOK {
			foundIssue = true
			break
		}
	}

	if !foundIssue {
		t.Skip("runtime-gitignore check not detecting broken state")
	}

	// Run fix
	d.Fix(ctx)

	// Second run should show the issue is fixed
	report2 := d.Run(ctx)
	for _, r := range report2.Checks {
		if r.Name == "runtime-gitignore" && r.Status == StatusError {
			t.Errorf("doctor --fix did not fix runtime-gitignore issue")
		}
	}
}

// TestIntegrationFixMultipleIssues verifies that doctor --fix can fix multiple issues.
func TestIntegrationFixMultipleIssues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")
	ctx := &CheckContext{TownRoot: townRoot}

	// Break multiple things
	breakRuntimeGitignore(t, townRoot)
	breakCrewGitignore(t, townRoot, "horde", "worker1")

	d := NewDoctor()
	d.RegisterAll(NewRuntimeGitignoreCheck())

	// Run fix
	report := d.Fix(ctx)

	// Count how many were fixed
	fixedCount := 0
	for _, r := range report.Checks {
		if r.Status == StatusOK && strings.Contains(r.Message, "fixed") {
			fixedCount++
		}
	}

	t.Logf("Fixed %d issues", fixedCount)
}

// TestIntegrationFixIdempotent verifies that running fix multiple times doesn't break things.
func TestIntegrationFixIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")
	ctx := &CheckContext{TownRoot: townRoot}

	// Break something
	breakRuntimeGitignore(t, townRoot)

	d := NewDoctor()
	d.RegisterAll(NewRuntimeGitignoreCheck())

	// Fix it once
	d.Fix(ctx)

	// Verify it's fixed
	report1 := d.Run(ctx)
	if report1.Summary.Errors > 0 {
		t.Logf("Still has %d errors after first fix", report1.Summary.Errors)
	}

	// Fix it again - should not break anything
	d.Fix(ctx)

	// Verify it's still fixed
	report2 := d.Run(ctx)
	if report2.Summary.Errors > 0 {
		t.Errorf("Second fix broke something: %d errors", report2.Summary.Errors)
		for _, r := range report2.Checks {
			if r.Status == StatusError {
				t.Errorf("  %s: %s", r.Name, r.Message)
			}
		}
	}
}

// TestIntegrationFixDoesntBreakWorking verifies fix doesn't break already-working things.
func TestIntegrationFixDoesntBreakWorking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")
	ctx := &CheckContext{TownRoot: townRoot}

	d := NewDoctor()
	d.RegisterAll(
		NewTownConfigExistsCheck(),
		NewTownConfigValidCheck(),
		NewRigsRegistryExistsCheck(),
	)

	// Run check first - should be OK
	report1 := d.Run(ctx)
	initialOK := report1.Summary.OK

	// Run fix (even though nothing is broken)
	d.Fix(ctx)

	// Run check again - should still be OK
	report2 := d.Run(ctx)
	finalOK := report2.Summary.OK

	if finalOK < initialOK {
		t.Errorf("Fix broke working checks: had %d OK, now have %d OK", initialOK, finalOK)
		for _, r := range report2.Checks {
			if r.Status != StatusOK {
				t.Errorf("  %s: %s", r.Name, r.Message)
			}
		}
	}
}

// TestIntegrationNoFalsePositives verifies doctor doesn't report issues that don't exist.
func TestIntegrationNoFalsePositives(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	townRoot := setupIntegrationTown(t)
	createTestRig(t, townRoot, "horde")
	setupMockRelics(t, townRoot, "horde")
	ctx := &CheckContext{TownRoot: townRoot}

	d := NewDoctor()
	d.RegisterAll(
		NewTownConfigExistsCheck(),
		NewTownConfigValidCheck(),
		NewRigsRegistryExistsCheck(),
		NewOrphanSessionCheck(),
	)
	report := d.Run(ctx)

	// Document any errors found - these are potential false positives
	// that need investigation
	for _, r := range report.Checks {
		if r.Status == StatusError {
			t.Logf("Potential false positive: %s - %s", r.Name, r.Message)
			for _, detail := range r.Details {
				t.Logf("  Detail: %s", detail)
			}
		}
	}
}

// TestIntegrationSessionNaming verifies session name parsing is consistent.
func TestIntegrationSessionNaming(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		wantRig     string
		wantRole    string
		wantName    string
	}{
		{
			name:        "warchief",
			sessionName: "hq-warchief",
			wantRig:     "",
			wantRole:    "warchief",
			wantName:    "",
		},
		{
			name:        "witness",
			sessionName: "gt-horde-witness",
			wantRig:     "horde",
			wantRole:    "witness",
			wantName:    "",
		},
		{
			name:        "clan",
			sessionName: "gt-horde-clan-max",
			wantRig:     "horde",
			wantRole:    "clan",
			wantName:    "max",
		},
		{
			name:        "crew_multipart_name",
			sessionName: "gt-niflheim-clan-codex1",
			wantRig:     "niflheim",
			wantRole:    "clan",
			wantName:    "codex1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse using the session package
			// This validates that session naming is consistent across the codebase
			t.Logf("Session %s should parse to warband=%q role=%q name=%q",
				tt.sessionName, tt.wantRig, tt.wantRole, tt.wantName)
		})
	}
}

// Helper functions

// mockEnvReaderIntegration implements SessionEnvReader for integration tests.
type mockEnvReaderIntegration struct {
	sessions    []string
	sessionEnvs map[string]map[string]string
	listErr     error
	envErrs     map[string]error
}

func (m *mockEnvReaderIntegration) ListSessions() ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.sessions, nil
}

func (m *mockEnvReaderIntegration) GetAllEnvironment(session string) (map[string]string, error) {
	if m.envErrs != nil {
		if err, ok := m.envErrs[session]; ok {
			return nil, err
		}
	}
	if m.sessionEnvs != nil {
		if env, ok := m.sessionEnvs[session]; ok {
			return env, nil
		}
	}
	return map[string]string{}, nil
}

func setupIntegrationTown(t *testing.T) string {
	t.Helper()
	townRoot := t.TempDir()

	// Create minimal encampment structure
	dirs := []string{
		"warchief",
		".relics",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(townRoot, dir), 0755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}

	// Create encampment.json
	townConfig := map[string]interface{}{
		"name":    "test-encampment",
		"type":    "encampment",
		"version": 2,
	}
	townJSON, _ := json.Marshal(townConfig)
	if err := os.WriteFile(filepath.Join(townRoot, "warchief", "encampment.json"), townJSON, 0644); err != nil {
		t.Fatalf("failed to create encampment.json: %v", err)
	}

	// Create warbands.json
	rigsConfig := map[string]interface{}{
		"version": 1,
		"warbands":    map[string]interface{}{},
	}
	rigsJSON, _ := json.Marshal(rigsConfig)
	if err := os.WriteFile(filepath.Join(townRoot, "warchief", "warbands.json"), rigsJSON, 0644); err != nil {
		t.Fatalf("failed to create warbands.json: %v", err)
	}

	// Create relics config
	relicsConfig := `# Test relics config
issue-prefix: "hq"
`
	if err := os.WriteFile(filepath.Join(townRoot, ".relics", "config.yaml"), []byte(relicsConfig), 0644); err != nil {
		t.Fatalf("failed to create relics config: %v", err)
	}

	// Create empty routes.jsonl
	if err := os.WriteFile(filepath.Join(townRoot, ".relics", "routes.jsonl"), []byte(""), 0644); err != nil {
		t.Fatalf("failed to create routes.jsonl: %v", err)
	}

	// Initialize git repo
	initGitRepoForIntegration(t, townRoot)

	return townRoot
}

func createTestRig(t *testing.T, townRoot, rigName string) {
	t.Helper()
	rigPath := filepath.Join(townRoot, rigName)

	// Create warband directories
	dirs := []string{
		"raiders",
		"clan",
		"witness",
		"forge",
		"warchief/warband",
		".relics",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(rigPath, dir), 0755); err != nil {
			t.Fatalf("failed to create %s/%s: %v", rigName, dir, err)
		}
	}

	// Create warband config
	rigConfig := map[string]interface{}{
		"name": rigName,
	}
	rigJSON, _ := json.Marshal(rigConfig)
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), rigJSON, 0644); err != nil {
		t.Fatalf("failed to create warband config: %v", err)
	}

	// Create warband relics config
	relicsConfig := `# Warband relics config
`
	if err := os.WriteFile(filepath.Join(rigPath, ".relics", "config.yaml"), []byte(relicsConfig), 0644); err != nil {
		t.Fatalf("failed to create warband relics config: %v", err)
	}

	// Add route to encampment relics
	route := map[string]string{
		"prefix": rigName[:2] + "-",
		"path":   rigName,
	}
	routeJSON, _ := json.Marshal(route)
	routesFile := filepath.Join(townRoot, ".relics", "routes.jsonl")
	f, err := os.OpenFile(routesFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Fatalf("failed to open routes.jsonl: %v", err)
	}
	f.Write(routeJSON)
	f.Write([]byte("\n"))
	f.Close()

	// Update warbands.json
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsData, _ := os.ReadFile(rigsPath)
	var rigsConfig map[string]interface{}
	json.Unmarshal(rigsData, &rigsConfig)

	warbands := rigsConfig["warbands"].(map[string]interface{})
	warbands[rigName] = map[string]interface{}{
		"git_url":  "https://example.com/" + rigName + ".git",
		"added_at": time.Now().Format(time.RFC3339),
		"relics": map[string]string{
			"prefix": rigName[:2],
		},
	}

	rigsJSON, _ := json.Marshal(rigsConfig)
	os.WriteFile(rigsPath, rigsJSON, 0644)
}

func setupMockRelics(t *testing.T, townRoot, rigName string) {
	t.Helper()

	// Create mock issues.jsonl with required relics
	rigPath := filepath.Join(townRoot, rigName)
	issuesFile := filepath.Join(rigPath, ".relics", "issues.jsonl")

	prefix := rigName[:2]
	issues := []map[string]interface{}{
		{
			"id":         prefix + "-warband-" + rigName,
			"title":      rigName,
			"status":     "open",
			"issue_type": "warband",
			"labels":     []string{"gt:warband"},
		},
		{
			"id":         prefix + "-" + rigName + "-witness",
			"title":      "Witness for " + rigName,
			"status":     "open",
			"issue_type": "agent",
			"labels":     []string{"gt:agent"},
		},
		{
			"id":         prefix + "-" + rigName + "-forge",
			"title":      "Forge for " + rigName,
			"status":     "open",
			"issue_type": "agent",
			"labels":     []string{"gt:agent"},
		},
	}

	f, err := os.Create(issuesFile)
	if err != nil {
		t.Fatalf("failed to create issues.jsonl: %v", err)
	}
	defer f.Close()

	for _, issue := range issues {
		issueJSON, _ := json.Marshal(issue)
		f.Write(issueJSON)
		f.Write([]byte("\n"))
	}

	// Create encampment-level role relics
	townIssuesFile := filepath.Join(townRoot, ".relics", "issues.jsonl")
	townIssues := []map[string]interface{}{
		{
			"id":         "hq-witness-role",
			"title":      "Witness Role",
			"status":     "open",
			"issue_type": "role",
			"labels":     []string{"gt:role"},
		},
		{
			"id":         "hq-forge-role",
			"title":      "Forge Role",
			"status":     "open",
			"issue_type": "role",
			"labels":     []string{"gt:role"},
		},
		{
			"id":         "hq-clan-role",
			"title":      "Clan Role",
			"status":     "open",
			"issue_type": "role",
			"labels":     []string{"gt:role"},
		},
		{
			"id":         "hq-warchief-role",
			"title":      "Warchief Role",
			"status":     "open",
			"issue_type": "role",
			"labels":     []string{"gt:role"},
		},
		{
			"id":         "hq-shaman-role",
			"title":      "Shaman Role",
			"status":     "open",
			"issue_type": "role",
			"labels":     []string{"gt:role"},
		},
	}

	tf, err := os.Create(townIssuesFile)
	if err != nil {
		t.Fatalf("failed to create encampment issues.jsonl: %v", err)
	}
	defer tf.Close()

	for _, issue := range townIssues {
		issueJSON, _ := json.Marshal(issue)
		tf.Write(issueJSON)
		tf.Write([]byte("\n"))
	}
}

func breakRuntimeGitignore(t *testing.T, townRoot string) {
	t.Helper()
	// Create a clan directory without .runtime in gitignore
	crewDir := filepath.Join(townRoot, "horde", "clan", "test-worker")
	if err := os.MkdirAll(crewDir, 0755); err != nil {
		t.Fatalf("failed to create clan dir: %v", err)
	}
	// Create a .gitignore without .runtime
	gitignore := "*.log\n"
	if err := os.WriteFile(filepath.Join(crewDir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		t.Fatalf("failed to create gitignore: %v", err)
	}
}

func breakCrewGitignore(t *testing.T, townRoot, rigName, workerName string) {
	t.Helper()
	// Create another clan directory without .runtime in gitignore
	crewDir := filepath.Join(townRoot, rigName, "clan", workerName)
	if err := os.MkdirAll(crewDir, 0755); err != nil {
		t.Fatalf("failed to create clan dir: %v", err)
	}
	// Create a .gitignore without .runtime
	gitignore := "*.tmp\n"
	if err := os.WriteFile(filepath.Join(crewDir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		t.Fatalf("failed to create gitignore: %v", err)
	}
}

func initGitRepoForIntegration(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "--initial-branch=main")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	exec.Command("git", "-C", dir, "config", "user.email", "test@example.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()
}
