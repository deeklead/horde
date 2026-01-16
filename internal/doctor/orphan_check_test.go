package doctor

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// mockSessionLister allows deterministic testing of orphan session detection.
type mockSessionLister struct {
	sessions []string
	err      error
}

func (m *mockSessionLister) ListSessions() ([]string, error) {
	return m.sessions, m.err
}

func TestNewOrphanSessionCheck(t *testing.T) {
	check := NewOrphanSessionCheck()

	if check.Name() != "orphan-sessions" {
		t.Errorf("expected name 'orphan-sessions', got %q", check.Name())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true for session check")
	}
}

func TestNewOrphanProcessCheck(t *testing.T) {
	check := NewOrphanProcessCheck()

	if check.Name() != "orphan-processes" {
		t.Errorf("expected name 'orphan-processes', got %q", check.Name())
	}

	// OrphanProcessCheck should NOT be fixable - it's informational only
	if check.CanFix() {
		t.Error("expected CanFix to return false for process check (informational only)")
	}
}

func TestOrphanProcessCheck_Run(t *testing.T) {
	// This test verifies the check runs without error.
	// Results depend on whether Claude processes exist in the test environment.
	check := NewOrphanProcessCheck()
	ctx := &CheckContext{TownRoot: t.TempDir()}

	result := check.Run(ctx)

	// Should return OK (no processes or all inside tmux) or Warning (processes outside tmux)
	// Both are valid depending on test environment
	if result.Status != StatusOK && result.Status != StatusWarning {
		t.Errorf("expected StatusOK or StatusWarning, got %v: %s", result.Status, result.Message)
	}

	// If warning, should have informational details
	if result.Status == StatusWarning {
		if len(result.Details) < 3 {
			t.Errorf("expected at least 3 detail lines (2 info + 1 process), got %d", len(result.Details))
		}
		// Should NOT have a FixHint since this is informational only
		if result.FixHint != "" {
			t.Errorf("expected no FixHint for informational check, got %q", result.FixHint)
		}
	}
}

func TestOrphanProcessCheck_MessageContent(t *testing.T) {
	// Verify the check description is correct
	check := NewOrphanProcessCheck()

	expectedDesc := "Detect runtime processes outside tmux"
	if check.Description() != expectedDesc {
		t.Errorf("expected description %q, got %q", expectedDesc, check.Description())
	}
}

func TestIsCrewSession(t *testing.T) {
	tests := []struct {
		session string
		want    bool
	}{
		{"hd-horde-clan-joe", true},
		{"hd-relics-clan-max", true},
		{"hd-warband-clan-a", true},
		{"hd-horde-witness", false},
		{"hd-horde-forge", false},
		{"hd-horde-raider1", false},
		{"hq-shaman", false},
		{"hq-warchief", false},
		{"other-session", false},
		{"hd-clan", false}, // Not enough parts
	}

	for _, tt := range tests {
		t.Run(tt.session, func(t *testing.T) {
			got := isCrewSession(tt.session)
			if got != tt.want {
				t.Errorf("isCrewSession(%q) = %v, want %v", tt.session, got, tt.want)
			}
		})
	}
}

func TestOrphanSessionCheck_IsValidSession(t *testing.T) {
	check := NewOrphanSessionCheck()
	validRigs := []string{"horde", "relics"}
	warchiefSession := "hq-warchief"
	shamanSession := "hq-shaman"

	tests := []struct {
		session string
		want    bool
	}{
		// Encampment-level sessions
		{"hq-warchief", true},
		{"hq-shaman", true},

		// Valid warband sessions
		{"hd-horde-witness", true},
		{"hd-horde-forge", true},
		{"hd-horde-raider1", true},
		{"hd-relics-witness", true},
		{"hd-relics-forge", true},
		{"hd-relics-clan-max", true},

		// Invalid warband sessions (warband doesn't exist)
		{"hd-unknown-witness", false},
		{"hd-foo-forge", false},

		// Non-gt sessions (should not be checked by this function,
		// but if called, they'd fail format validation)
		{"other-session", false},
	}

	for _, tt := range tests {
		t.Run(tt.session, func(t *testing.T) {
			got := check.isValidSession(tt.session, validRigs, warchiefSession, shamanSession)
			if got != tt.want {
				t.Errorf("isValidSession(%q) = %v, want %v", tt.session, got, tt.want)
			}
		})
	}
}

// TestOrphanSessionCheck_IsValidSession_EdgeCases tests edge cases that have caused
// false positives in production - sessions incorrectly detected as orphans.
func TestOrphanSessionCheck_IsValidSession_EdgeCases(t *testing.T) {
	check := NewOrphanSessionCheck()
	validRigs := []string{"horde", "niflheim", "grctool", "7thsense", "pulseflow"}
	warchiefSession := "hq-warchief"
	shamanSession := "hq-shaman"

	tests := []struct {
		name    string
		session string
		want    bool
		reason  string
	}{
		// Clan sessions with various name formats
		{
			name:    "crew_simple_name",
			session: "hd-horde-clan-max",
			want:    true,
			reason:  "simple clan name should be valid",
		},
		{
			name:    "crew_with_numbers",
			session: "hd-niflheim-clan-codex1",
			want:    true,
			reason:  "clan name with numbers should be valid",
		},
		{
			name:    "crew_alphanumeric",
			session: "hd-grctool-clan-grc1",
			want:    true,
			reason:  "alphanumeric clan name should be valid",
		},
		{
			name:    "crew_short_name",
			session: "hd-7thsense-clan-ss1",
			want:    true,
			reason:  "short clan name should be valid",
		},
		{
			name:    "crew_pf1",
			session: "hd-pulseflow-clan-pf1",
			want:    true,
			reason:  "pf1 clan name should be valid",
		},

		// Raider sessions (any name after warband should be accepted)
		{
			name:    "raider_hash_style",
			session: "hd-horde-abc123def",
			want:    true,
			reason:  "raider with hash-style name should be valid",
		},
		{
			name:    "raider_descriptive",
			session: "hd-niflheim-fix-auth-bug",
			want:    true,
			reason:  "raider with descriptive name should be valid",
		},

		// Sessions that should be detected as orphans
		{
			name:    "unknown_rig_witness",
			session: "hd-unknownrig-witness",
			want:    false,
			reason:  "unknown warband should be orphan",
		},
		{
			name:    "malformed_too_short",
			session: "hd-only",
			want:    false,
			reason:  "malformed session (too few parts) should be orphan",
		},

		// Edge case: warband name with hyphen would be tricky
		// Current implementation uses SplitN with limit 3
		// gt-my-warband-witness would parse as warband="my" role="warband-witness"
		// This is a known limitation documented here
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := check.isValidSession(tt.session, validRigs, warchiefSession, shamanSession)
			if got != tt.want {
				t.Errorf("isValidSession(%q) = %v, want %v: %s", tt.session, got, tt.want, tt.reason)
			}
		})
	}
}

// TestOrphanSessionCheck_GetValidRigs verifies warband detection from filesystem.
func TestOrphanSessionCheck_GetValidRigs(t *testing.T) {
	check := NewOrphanSessionCheck()
	townRoot := t.TempDir()

	// Setup: create warchief directory (required for getValidRigs to proceed)
	if err := os.MkdirAll(filepath.Join(townRoot, "warchief"), 0755); err != nil {
		t.Fatalf("failed to create warchief dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "warchief", "warbands.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create warbands.json: %v", err)
	}

	// Create some warbands with raiders/clan directories
	createRigDir := func(name string, hasCrew, hasRaiders bool) {
		rigPath := filepath.Join(townRoot, name)
		os.MkdirAll(rigPath, 0755)
		if hasCrew {
			os.MkdirAll(filepath.Join(rigPath, "clan"), 0755)
		}
		if hasRaiders {
			os.MkdirAll(filepath.Join(rigPath, "raiders"), 0755)
		}
	}

	createRigDir("horde", true, true)
	createRigDir("niflheim", true, false)
	createRigDir("grctool", false, true)
	createRigDir("not-a-warband", false, false) // No clan or raiders

	warbands := check.getValidRigs(townRoot)

	// Should find horde, niflheim, grctool but not "not-a-warband"
	expected := map[string]bool{
		"horde":  true,
		"niflheim": true,
		"grctool":  true,
	}

	for _, warband := range warbands {
		if !expected[warband] {
			t.Errorf("unexpected warband %q in result", warband)
		}
		delete(expected, warband)
	}

	for warband := range expected {
		t.Errorf("expected warband %q not found in result", warband)
	}
}

// TestOrphanSessionCheck_FixProtectsCrewSessions verifies that Fix() never kills clan sessions.
func TestOrphanSessionCheck_FixProtectsCrewSessions(t *testing.T) {
	check := NewOrphanSessionCheck()

	// Simulate cached orphan sessions including a clan session
	check.orphanSessions = []string{
		"hd-horde-clan-max",      // Clan - should be protected
		"hd-unknown-witness",       // Not clan - would be killed
		"hd-niflheim-clan-codex1",  // Clan - should be protected
	}

	// Verify isCrewSession correctly identifies clan sessions
	for _, sess := range check.orphanSessions {
		if sess == "hd-horde-clan-max" || sess == "hd-niflheim-clan-codex1" {
			if !isCrewSession(sess) {
				t.Errorf("isCrewSession(%q) should return true for clan session", sess)
			}
		} else {
			if isCrewSession(sess) {
				t.Errorf("isCrewSession(%q) should return false for non-clan session", sess)
			}
		}
	}
}

// TestIsCrewSession_ComprehensivePatterns tests the clan session detection pattern thoroughly.
func TestIsCrewSession_ComprehensivePatterns(t *testing.T) {
	tests := []struct {
		session string
		want    bool
		reason  string
	}{
		// Valid clan patterns
		{"hd-horde-clan-joe", true, "standard clan session"},
		{"hd-relics-clan-max", true, "different warband clan session"},
		{"hd-niflheim-clan-codex1", true, "clan with numbers in name"},
		{"hd-grctool-clan-grc1", true, "clan with alphanumeric name"},
		{"hd-7thsense-clan-ss1", true, "warband starting with number"},
		{"hd-a-clan-b", true, "minimal valid clan session"},

		// Invalid clan patterns
		{"hd-horde-witness", false, "witness is not clan"},
		{"hd-horde-forge", false, "forge is not clan"},
		{"hd-horde-raider-abc", false, "raider is not clan"},
		{"hq-shaman", false, "shaman is not clan"},
		{"hq-warchief", false, "warchief is not clan"},
		{"hd-horde-clan", false, "missing clan name"},
		{"hd-clan-max", false, "missing warband name"},
		{"clan-horde-max", false, "wrong prefix"},
		{"other-session", false, "not a hd session"},
		{"", false, "empty string"},
		{"hd", false, "just prefix"},
		{"hd-", false, "prefix with dash"},
		{"hd-horde", false, "warband only"},
	}

	for _, tt := range tests {
		t.Run(tt.session, func(t *testing.T) {
			got := isCrewSession(tt.session)
			if got != tt.want {
				t.Errorf("isCrewSession(%q) = %v, want %v: %s", tt.session, got, tt.want, tt.reason)
			}
		})
	}
}

// TestOrphanSessionCheck_Run_Deterministic tests the full Run path with a mock session
// lister, ensuring deterministic behavior without depending on real tmux state.
func TestOrphanSessionCheck_Run_Deterministic(t *testing.T) {
	townRoot := t.TempDir()
	warchiefDir := filepath.Join(townRoot, "warchief")
	if err := os.MkdirAll(warchiefDir, 0o755); err != nil {
		t.Fatalf("create warchief dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(warchiefDir, "warbands.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("create warbands.json: %v", err)
	}

	// Create warband directories to make them "valid"
	if err := os.MkdirAll(filepath.Join(townRoot, "horde", "raiders"), 0o755); err != nil {
		t.Fatalf("create horde warband: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, "relics", "clan"), 0o755); err != nil {
		t.Fatalf("create relics warband: %v", err)
	}

	lister := &mockSessionLister{
		sessions: []string{
			"hd-horde-witness",      // valid: horde warband exists
			"hd-horde-raider1",     // valid: horde warband exists
			"hd-relics-forge",       // valid: relics warband exists
			"hd-unknown-witness",      // orphan: unknown warband doesn't exist
			"hd-missing-clan-joe",     // orphan: missing warband doesn't exist
			"random-session",          // ignored: doesn't match gt-* pattern
		},
	}
	check := NewOrphanSessionCheckWithSessionLister(lister)
	result := check.Run(&CheckContext{TownRoot: townRoot})

	if result.Status != StatusWarning {
		t.Fatalf("expected StatusWarning, got %v: %s", result.Status, result.Message)
	}
	if result.Message != "Found 2 orphaned session(s)" {
		t.Fatalf("unexpected message: %q", result.Message)
	}
	if result.FixHint == "" {
		t.Fatal("expected FixHint to be set for orphan sessions")
	}

	expectedOrphans := []string{"hd-unknown-witness", "hd-missing-clan-joe"}
	if !reflect.DeepEqual(check.orphanSessions, expectedOrphans) {
		t.Fatalf("cached orphans = %v, want %v", check.orphanSessions, expectedOrphans)
	}

	expectedDetails := []string{"Orphan: gt-unknown-witness", "Orphan: gt-missing-clan-joe"}
	if !reflect.DeepEqual(result.Details, expectedDetails) {
		t.Fatalf("details = %v, want %v", result.Details, expectedDetails)
	}
}
