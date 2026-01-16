package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/workspace"
)

// filterHDEnv removes HD_* and BD_* environment variables to isolate test subprocess.
// This prevents tests from inheriting the parent workspace's Horde configuration.
func filterHDEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "HD_") || strings.HasPrefix(e, "BD_") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// TestQuerySessionEvents_FindsEventsFromAllLocations verifies that querySessionEvents
// finds session.ended events from both encampment-level and warband-level relics databases.
//
// Bug: Events created by warband-level agents (raiders, witness, etc.) are stored in
// the warband's .relics database. Events created by encampment-level agents (warchief, shaman)
// are stored in the encampment's .relics database. querySessionEvents must query ALL
// relics locations to find all events.
//
// This test:
// 1. Creates a encampment with a warband
// 2. Creates session.ended events in both encampment and warband relics
// 3. Verifies querySessionEvents finds events from both locations
func TestQuerySessionEvents_FindsEventsFromAllLocations(t *testing.T) {
	// Skip if hd and rl are not installed
	if _, err := exec.LookPath("hd"); err != nil {
		t.Skip("hd not installed, skipping integration test")
	}
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping integration test")
	}

	// Skip when running inside a Horde workspace - this integration test
	// creates a separate workspace and the subprocesses can interact with
	// the parent workspace's daemon, causing hangs.
	if os.Getenv("HD_ENCAMPMENT_ROOT") != "" || os.Getenv("BD_ACTOR") != "" {
		t.Skip("skipping integration test inside Horde workspace (use 'go test' outside workspace)")
	}

	// Create a temporary directory structure
	tmpDir := t.TempDir()
	townRoot := filepath.Join(tmpDir, "test-encampment")

	// Create encampment directory
	if err := os.MkdirAll(townRoot, 0755); err != nil {
		t.Fatalf("creating encampment directory: %v", err)
	}

	// Initialize a git repo (required for hd install)
	gitInitCmd := exec.Command("git", "init")
	gitInitCmd.Dir = townRoot
	if out, err := gitInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	// Use hd install to set up the encampment
	// Clear GT environment variables to isolate test from parent workspace
	gtInstallCmd := exec.Command("hd", "install")
	gtInstallCmd.Dir = townRoot
	gtInstallCmd.Env = filterHDEnv(os.Environ())
	if out, err := gtInstallCmd.CombinedOutput(); err != nil {
		t.Fatalf("hd install: %v\n%s", err, out)
	}

	// Create a bare repo to use as the warband source
	bareRepo := filepath.Join(tmpDir, "bare-repo.git")
	bareInitCmd := exec.Command("git", "init", "--bare", bareRepo)
	if out, err := bareInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	// Create a temporary clone to add initial content (bare repos need content)
	tempClone := filepath.Join(tmpDir, "temp-clone")
	cloneCmd := exec.Command("git", "clone", bareRepo, tempClone)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone bare: %v\n%s", err, out)
	}

	// Add initial commit to bare repo
	initFileCmd := exec.Command("bash", "-c", "echo 'test' > README.md && git add . && git commit -m 'init'")
	initFileCmd.Dir = tempClone
	if out, err := initFileCmd.CombinedOutput(); err != nil {
		t.Fatalf("initial commit: %v\n%s", err, out)
	}
	pushCmd := exec.Command("git", "push", "origin", "main")
	pushCmd.Dir = tempClone
	// Try main first, fall back to master
	if _, err := pushCmd.CombinedOutput(); err != nil {
		pushCmd2 := exec.Command("git", "push", "origin", "master")
		pushCmd2.Dir = tempClone
		if out, err := pushCmd2.CombinedOutput(); err != nil {
			t.Fatalf("git push: %v\n%s", err, out)
		}
	}

	// Add warband using hd warband add
	rigAddCmd := exec.Command("hd", "warband", "add", "testrig", bareRepo, "--prefix=tr")
	rigAddCmd.Dir = townRoot
	rigAddCmd.Env = filterHDEnv(os.Environ())
	if out, err := rigAddCmd.CombinedOutput(); err != nil {
		t.Fatalf("hd warband add: %v\n%s", err, out)
	}

	// Find the warband path
	rigPath := filepath.Join(townRoot, "testrig")

	// Verify warband has its own .relics
	rigRelicsPath := filepath.Join(rigPath, ".relics")
	if _, err := os.Stat(rigRelicsPath); os.IsNotExist(err) {
		t.Fatalf("warband .relics not created at %s", rigRelicsPath)
	}

	// Create a session.ended event in ENCAMPMENT relics (simulating warchief/shaman)
	townEventPayload := `{"cost_usd":1.50,"session_id":"hq-warchief","role":"warchief","ended_at":"2026-01-12T10:00:00Z"}`
	townEventCmd := exec.Command("rl", "create",
		"--type=event",
		"--title=Encampment session ended",
		"--event-category=session.ended",
		"--event-payload="+townEventPayload,
		"--json",
	)
	townEventCmd.Dir = townRoot
	townEventCmd.Env = filterHDEnv(os.Environ())
	townOut, err := townEventCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("creating encampment event: %v\n%s", err, townOut)
	}
	t.Logf("Created encampment event: %s", string(townOut))

	// Create a session.ended event in WARBAND relics (simulating raider)
	rigEventPayload := `{"cost_usd":2.50,"session_id":"hd-testrig-toast","role":"raider","warband":"testrig","worker":"toast","ended_at":"2026-01-12T11:00:00Z"}`
	rigEventCmd := exec.Command("rl", "create",
		"--type=event",
		"--title=Warband session ended",
		"--event-category=session.ended",
		"--event-payload="+rigEventPayload,
		"--json",
	)
	rigEventCmd.Dir = rigPath
	rigEventCmd.Env = filterHDEnv(os.Environ())
	rigOut, err := rigEventCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("creating warband event: %v\n%s", err, rigOut)
	}
	t.Logf("Created warband event: %s", string(rigOut))

	// Verify events are in separate databases by querying each directly
	townListCmd := exec.Command("rl", "list", "--type=event", "--all", "--json")
	townListCmd.Dir = townRoot
	townListCmd.Env = filterHDEnv(os.Environ())
	townListOut, err := townListCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("listing encampment events: %v\n%s", err, townListOut)
	}

	rigListCmd := exec.Command("rl", "list", "--type=event", "--all", "--json")
	rigListCmd.Dir = rigPath
	rigListCmd.Env = filterHDEnv(os.Environ())
	rigListOut, err := rigListCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("listing warband events: %v\n%s", err, rigListOut)
	}

	var townEvents, rigEvents []struct{ ID string }
	json.Unmarshal(townListOut, &townEvents)
	json.Unmarshal(rigListOut, &rigEvents)

	t.Logf("Encampment relics has %d events", len(townEvents))
	t.Logf("Warband relics has %d events", len(rigEvents))

	// Both should have events (they're in separate DBs)
	if len(townEvents) == 0 {
		t.Error("Expected encampment relics to have events")
	}
	if len(rigEvents) == 0 {
		t.Error("Expected warband relics to have events")
	}

	// Save current directory and change to encampment root for query
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Errorf("restoring directory: %v", err)
		}
	}()

	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("changing to encampment root: %v", err)
	}

	// Verify workspace discovery works
	foundTownRoot, wsErr := workspace.FindFromCwdOrError()
	if wsErr != nil {
		t.Fatalf("workspace.FindFromCwdOrError failed: %v", wsErr)
	}
	if foundTownRoot != townRoot {
		t.Errorf("workspace.FindFromCwdOrError returned %s, expected %s", foundTownRoot, townRoot)
	}

	// Call querySessionEvents - this should find events from ALL locations
	entries := querySessionEvents()

	t.Logf("querySessionEvents returned %d entries", len(entries))

	// We created 2 session.ended events (one encampment, one warband)
	// The fix should find BOTH
	if len(entries) < 2 {
		t.Errorf("querySessionEvents found %d entries, expected at least 2 (one from encampment, one from warband)", len(entries))
		t.Log("This indicates the bug: querySessionEvents only queries encampment-level relics, missing warband-level events")
	}

	// Verify we found both the warchief and raider sessions
	var foundWarchief, foundRaider bool
	for _, e := range entries {
		t.Logf("  Entry: session=%s role=%s cost=$%.2f", e.SessionID, e.Role, e.CostUSD)
		if e.Role == "warchief" {
			foundWarchief = true
		}
		if e.Role == "raider" {
			foundRaider = true
		}
	}

	if !foundWarchief {
		t.Error("Missing warchief session from encampment relics")
	}
	if !foundRaider {
		t.Error("Missing raider session from warband relics")
	}
}
