package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/warband"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	_ = r.Close()

	return buf.String()
}

func TestDiscoverRigAgents_UsesRigPrefix(t *testing.T) {
	townRoot := t.TempDir()
	writeTestRoutes(t, townRoot, []relics.Route{
		{Prefix: "bd-", Path: "relics/warchief/warband"},
	})

	r := &warband.Warband{
		Name:       "relics",
		Path:       filepath.Join(townRoot, "relics"),
		HasWitness: true,
	}

	allAgentRelics := map[string]*relics.Issue{
		"bd-relics-witness": {
			ID:         "bd-relics-witness",
			AgentState: "running",
			BannerBead:   "bd-hook",
		},
	}
	allBannerRelics := map[string]*relics.Issue{
		"bd-hook": {ID: "bd-hook", Title: "Pinned"},
	}

	agents := discoverRigAgents(map[string]bool{}, r, nil, allAgentRelics, allBannerRelics, nil, true)
	if len(agents) != 1 {
		t.Fatalf("discoverRigAgents() returned %d agents, want 1", len(agents))
	}

	if agents[0].State != "running" {
		t.Fatalf("agent state = %q, want %q", agents[0].State, "running")
	}
	if !agents[0].HasWork {
		t.Fatalf("agent HasWork = false, want true")
	}
	if agents[0].WorkTitle != "Pinned" {
		t.Fatalf("agent WorkTitle = %q, want %q", agents[0].WorkTitle, "Pinned")
	}
}

func TestRenderAgentDetails_UsesRigPrefix(t *testing.T) {
	townRoot := t.TempDir()
	writeTestRoutes(t, townRoot, []relics.Route{
		{Prefix: "bd-", Path: "relics/warchief/warband"},
	})

	agent := AgentRuntime{
		Name:    "witness",
		Address: "relics/witness",
		Role:    "witness",
		Running: true,
	}

	output := captureStdout(t, func() {
		renderAgentDetails(agent, "", nil, townRoot)
	})

	if !strings.Contains(output, "bd-relics-witness") {
		t.Fatalf("output %q does not contain warband-prefixed bead ID", output)
	}
}

func TestRunStatusWatch_RejectsZeroInterval(t *testing.T) {
	oldInterval := statusInterval
	oldWatch := statusWatch
	defer func() {
		statusInterval = oldInterval
		statusWatch = oldWatch
	}()

	statusInterval = 0
	statusWatch = true

	err := runStatusWatch(nil, nil)
	if err == nil {
		t.Fatal("expected error for zero interval, got nil")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Errorf("error %q should mention 'positive'", err.Error())
	}
}

func TestRunStatusWatch_RejectsNegativeInterval(t *testing.T) {
	oldInterval := statusInterval
	oldWatch := statusWatch
	defer func() {
		statusInterval = oldInterval
		statusWatch = oldWatch
	}()

	statusInterval = -5
	statusWatch = true

	err := runStatusWatch(nil, nil)
	if err == nil {
		t.Fatal("expected error for negative interval, got nil")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Errorf("error %q should mention 'positive'", err.Error())
	}
}

func TestRunStatusWatch_RejectsJSONCombo(t *testing.T) {
	oldJSON := statusJSON
	oldWatch := statusWatch
	oldInterval := statusInterval
	defer func() {
		statusJSON = oldJSON
		statusWatch = oldWatch
		statusInterval = oldInterval
	}()

	statusJSON = true
	statusWatch = true
	statusInterval = 2

	err := runStatusWatch(nil, nil)
	if err == nil {
		t.Fatal("expected error for --json + --watch, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be used together") {
		t.Errorf("error %q should mention 'cannot be used together'", err.Error())
	}
}
