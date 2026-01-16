package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/config"
)

// mockEnvReader implements SessionEnvReader for testing.
type mockEnvReader struct {
	sessions    []string
	sessionEnvs map[string]map[string]string
	listErr     error
	envErrs     map[string]error
}

func (m *mockEnvReader) ListSessions() ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.sessions, nil
}

func (m *mockEnvReader) GetAllEnvironment(session string) (map[string]string, error) {
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

// testTownRoot is the encampment root used in tests.
// Tests use this fixed path so expected values match what the check generates.
const testTownRoot = "/encampment"

// expectedEnv generates expected env vars matching what the check generates.
func expectedEnv(role, warband, agentName string) map[string]string {
	return config.AgentEnv(config.AgentEnvConfig{
		Role:      role,
		Warband:       warband,
		AgentName: agentName,
		TownRoot:  testTownRoot,
	})
}

// testCtx returns a CheckContext with the test encampment root.
func testCtx() *CheckContext {
	return &CheckContext{TownRoot: testTownRoot}
}

func TestEnvVarsCheck_NoSessions(t *testing.T) {
	reader := &mockEnvReader{
		sessions: []string{},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
	if result.Message != "No Horde sessions running" {
		t.Errorf("Message = %q, want %q", result.Message, "No Horde sessions running")
	}
}

func TestEnvVarsCheck_ListSessionsError(t *testing.T) {
	reader := &mockEnvReader{
		listErr: errors.New("tmux not running"),
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	// No tmux server is valid (Horde can be down)
	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
	if result.Message != "No tmux sessions running" {
		t.Errorf("Message = %q, want %q", result.Message, "No tmux sessions running")
	}
}

func TestEnvVarsCheck_NonHordeSessions(t *testing.T) {
	reader := &mockEnvReader{
		sessions: []string{"other-session", "my-dev"},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
	if result.Message != "No Horde sessions running" {
		t.Errorf("Message = %q, want %q", result.Message, "No Horde sessions running")
	}
}

func TestEnvVarsCheck_WarchiefCorrect(t *testing.T) {
	expected := expectedEnv("warchief", "", "")
	reader := &mockEnvReader{
		sessions: []string{"hq-warchief"},
		sessionEnvs: map[string]map[string]string{
			"hq-warchief": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
}

func TestEnvVarsCheck_WarchiefMissing(t *testing.T) {
	reader := &mockEnvReader{
		sessions: []string{"hq-warchief"},
		sessionEnvs: map[string]map[string]string{
			"hq-warchief": {}, // Missing all env vars
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
}

func TestEnvVarsCheck_WitnessCorrect(t *testing.T) {
	expected := expectedEnv("witness", "myrig", "")
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-witness"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-witness": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
}

func TestEnvVarsCheck_WitnessMismatch(t *testing.T) {
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-witness"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-witness": {
				"GT_ROLE": "witness",
				"GT_RIG":  "wrongrig", // Wrong warband
			},
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
}

func TestEnvVarsCheck_ForgeCorrect(t *testing.T) {
	expected := expectedEnv("forge", "myrig", "")
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-forge"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-forge": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
}

func TestEnvVarsCheck_RaiderCorrect(t *testing.T) {
	expected := expectedEnv("raider", "myrig", "Toast")
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-Toast"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-Toast": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
}

func TestEnvVarsCheck_RaiderMissing(t *testing.T) {
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-Toast"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-Toast": {
				"GT_ROLE": "raider",
				// Missing GT_RIG, GT_RAIDER, BD_ACTOR, GIT_AUTHOR_NAME
			},
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
}

func TestEnvVarsCheck_CrewCorrect(t *testing.T) {
	expected := expectedEnv("clan", "myrig", "worker1")
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-clan-worker1"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-clan-worker1": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
}

func TestEnvVarsCheck_MultipleSessions(t *testing.T) {
	warchiefEnv := expectedEnv("warchief", "", "")
	witnessEnv := expectedEnv("witness", "rig1", "")
	raiderEnv := expectedEnv("raider", "rig1", "Toast")

	reader := &mockEnvReader{
		sessions: []string{"hq-warchief", "gt-rig1-witness", "gt-rig1-Toast"},
		sessionEnvs: map[string]map[string]string{
			"hq-warchief":        warchiefEnv,
			"gt-rig1-witness": witnessEnv,
			"gt-rig1-Toast":   raiderEnv,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
	if result.Message != "All 3 session(s) have correct environment variables" {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestEnvVarsCheck_MixedCorrectAndMismatch(t *testing.T) {
	warchiefEnv := expectedEnv("warchief", "", "")

	reader := &mockEnvReader{
		sessions: []string{"hq-warchief", "gt-rig1-witness"},
		sessionEnvs: map[string]map[string]string{
			"hq-warchief": warchiefEnv,
			"gt-rig1-witness": {
				"GT_ROLE": "witness",
				// Missing GT_RIG and other vars
			},
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
}

func TestEnvVarsCheck_ShamanCorrect(t *testing.T) {
	expected := expectedEnv("shaman", "", "")
	reader := &mockEnvReader{
		sessions: []string{"hq-shaman"},
		sessionEnvs: map[string]map[string]string{
			"hq-shaman": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
}

func TestEnvVarsCheck_ShamanMissing(t *testing.T) {
	reader := &mockEnvReader{
		sessions: []string{"hq-shaman"},
		sessionEnvs: map[string]map[string]string{
			"hq-shaman": {}, // Missing all env vars
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
}

func TestEnvVarsCheck_GetEnvError(t *testing.T) {
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-witness"},
		envErrs: map[string]error{
			"gt-myrig-witness": errors.New("session not found"),
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
}

func TestEnvVarsCheck_HyphenatedRig(t *testing.T) {
	// Test warband name with hyphens: "foo-bar"
	expected := expectedEnv("witness", "foo-bar", "")
	reader := &mockEnvReader{
		sessions: []string{"gt-foo-bar-witness"},
		sessionEnvs: map[string]map[string]string{
			"gt-foo-bar-witness": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", result.Status)
	}
}

func TestEnvVarsCheck_RelicsDirWarning(t *testing.T) {
	// RELICS_DIR being set breaks prefix-based routing
	expected := expectedEnv("witness", "myrig", "")
	expected["RELICS_DIR"] = "/some/path/.relics" // This shouldn't be set!
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-witness"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-witness": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
	if !strings.Contains(result.Message, "RELICS_DIR") {
		t.Errorf("Message should mention RELICS_DIR, got: %q", result.Message)
	}
	if !strings.Contains(result.FixHint, "hd shutdown") {
		t.Errorf("FixHint should mention restart, got: %q", result.FixHint)
	}
}

func TestEnvVarsCheck_RelicsDirEmptyIsOK(t *testing.T) {
	// Empty RELICS_DIR should not warn
	expected := expectedEnv("witness", "myrig", "")
	expected["RELICS_DIR"] = "" // Empty is fine
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-witness"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-witness": expected,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK for empty RELICS_DIR", result.Status)
	}
}

func TestEnvVarsCheck_RelicsDirMultipleSessions(t *testing.T) {
	// Multiple sessions, only one has RELICS_DIR
	witnessEnv := expectedEnv("witness", "myrig", "")
	raiderEnv := expectedEnv("raider", "myrig", "Toast")
	raiderEnv["RELICS_DIR"] = "/bad/path" // This shouldn't be set!

	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-witness", "gt-myrig-Toast"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-witness": witnessEnv,
			"gt-myrig-Toast":   raiderEnv,
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
	if !strings.Contains(result.Message, "1 session") {
		t.Errorf("Message should mention 1 session with RELICS_DIR, got: %q", result.Message)
	}
}

func TestEnvVarsCheck_RelicsDirWithOtherMismatches(t *testing.T) {
	// Session has RELICS_DIR AND other mismatches - both should be reported
	reader := &mockEnvReader{
		sessions: []string{"gt-myrig-witness"},
		sessionEnvs: map[string]map[string]string{
			"gt-myrig-witness": {
				"GT_ROLE":   "witness",
				"GT_RIG":    "wrongrig", // Mismatch
				"RELICS_DIR": "/bad/path",
			},
		},
	}
	check := NewEnvVarsCheckWithReader(reader)
	result := check.Run(testCtx())

	if result.Status != StatusWarning {
		t.Errorf("Status = %v, want StatusWarning", result.Status)
	}
	// RELICS_DIR takes priority in message
	if !strings.Contains(result.Message, "RELICS_DIR") {
		t.Errorf("Message should prioritize RELICS_DIR, got: %q", result.Message)
	}
	// But details should include both
	detailsStr := strings.Join(result.Details, "\n")
	if !strings.Contains(detailsStr, "RELICS_DIR") {
		t.Errorf("Details should mention RELICS_DIR")
	}
	if !strings.Contains(detailsStr, "Other env var issues") {
		t.Errorf("Details should mention other issues")
	}
}
