//go:build integration

// Package cmd contains integration tests for banner slot verification.
//
// Run with: go test -tags=integration ./internal/cmd -run TestHookSlot -v
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/deeklead/horde/internal/relics"
)

// setupHookTestTown creates a minimal Horde with a raider for testing hooks.
// Returns townRoot and the path to the raider's worktree.
func setupHookTestTown(t *testing.T) (townRoot, raiderDir string) {
	t.Helper()

	townRoot = t.TempDir()

	// Create encampment-level .relics directory
	townRelicsDir := filepath.Join(townRoot, ".relics")
	if err := os.MkdirAll(townRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir encampment .relics: %v", err)
	}

	// Create routes.jsonl
	routes := []relics.Route{
		{Prefix: "hq-", Path: "."},                     // Encampment-level relics
		{Prefix: "hd-", Path: "horde/warchief/warband"},     // Horde warband
	}
	if err := relics.WriteRoutes(townRelicsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	// Create horde warband structure
	gasRigPath := filepath.Join(townRoot, "horde", "warchief", "warband")
	if err := os.MkdirAll(gasRigPath, 0755); err != nil {
		t.Fatalf("mkdir horde: %v", err)
	}

	// Create horde .relics directory with its own config
	gasRelicsDir := filepath.Join(gasRigPath, ".relics")
	if err := os.MkdirAll(gasRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir horde .relics: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gasRelicsDir, "config.yaml"), []byte("prefix: gt\n"), 0644); err != nil {
		t.Fatalf("write horde config: %v", err)
	}

	// Create raider worktree with redirect
	raiderDir = filepath.Join(townRoot, "horde", "raiders", "toast")
	if err := os.MkdirAll(raiderDir, 0755); err != nil {
		t.Fatalf("mkdir raiders: %v", err)
	}

	// Create redirect file for raider -> warchief/warband/.relics
	raiderRelicsDir := filepath.Join(raiderDir, ".relics")
	if err := os.MkdirAll(raiderRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir raider .relics: %v", err)
	}
	redirectContent := "../../warchief/warband/.relics"
	if err := os.WriteFile(filepath.Join(raiderRelicsDir, "redirect"), []byte(redirectContent), 0644); err != nil {
		t.Fatalf("write redirect: %v", err)
	}

	return townRoot, raiderDir
}

// initRelicsDB initializes the relics database by running rl init.
func initRelicsDB(t *testing.T, dir string) {
	t.Helper()

	cmd := exec.Command("rl", "--no-daemon", "init")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init failed: %v\n%s", err, output)
	}
}

// TestHookSlot_BasicHook verifies that a bead can be bannered to an agent.
func TestHookSlot_BasicHook(t *testing.T) {
	// Skip if rl is not available
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	townRoot, raiderDir := setupHookTestTown(t)
	_ = townRoot // Not used directly but shows test context

	// Initialize relics in the warband
	rigDir := filepath.Join(raiderDir, "..", "..", "warchief", "warband")
	initRelicsDB(t, rigDir)

	b := relics.New(rigDir)

	// Create a test bead
	issue, err := b.Create(relics.CreateOptions{
		Title:    "Test task for hooking",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create bead: %v", err)
	}
	t.Logf("Created bead: %s", issue.ID)

	// Hook the bead to the raider
	agentID := "horde/raiders/toast"
	status := relics.StatusHooked
	if err := b.Update(issue.ID, relics.UpdateOptions{
		Status:   &status,
		Assignee: &agentID,
	}); err != nil {
		t.Fatalf("hook bead: %v", err)
	}

	// Verify the bead is bannered
	hookedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: agentID,
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("list bannered relics: %v", err)
	}

	if len(hookedRelics) != 1 {
		t.Errorf("expected 1 bannered bead, got %d", len(hookedRelics))
	}

	if len(hookedRelics) > 0 && hookedRelics[0].ID != issue.ID {
		t.Errorf("bannered bead ID = %s, want %s", hookedRelics[0].ID, issue.ID)
	}
}

// TestHookSlot_Singleton verifies that only one bead can be bannered per agent.
func TestHookSlot_Singleton(t *testing.T) {
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	townRoot, raiderDir := setupHookTestTown(t)
	_ = townRoot

	rigDir := filepath.Join(raiderDir, "..", "..", "warchief", "warband")
	initRelicsDB(t, rigDir)

	b := relics.New(rigDir)
	agentID := "horde/raiders/toast"
	status := relics.StatusHooked

	// Create and hook first bead
	issue1, err := b.Create(relics.CreateOptions{
		Title:    "First task",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create first bead: %v", err)
	}

	if err := b.Update(issue1.ID, relics.UpdateOptions{
		Status:   &status,
		Assignee: &agentID,
	}); err != nil {
		t.Fatalf("hook first bead: %v", err)
	}

	// Create second bead
	issue2, err := b.Create(relics.CreateOptions{
		Title:    "Second task",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create second bead: %v", err)
	}

	// Hook second bead to same agent
	if err := b.Update(issue2.ID, relics.UpdateOptions{
		Status:   &status,
		Assignee: &agentID,
	}); err != nil {
		t.Fatalf("hook second bead: %v", err)
	}

	// Query bannered relics - both should be bannered (bd allows multiple)
	// The singleton constraint is enforced by hd hook, not rl itself
	hookedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: agentID,
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("list bannered relics: %v", err)
	}

	t.Logf("Found %d bannered relics for agent %s", len(hookedRelics), agentID)
	for _, h := range hookedRelics {
		t.Logf("  - %s: %s", h.ID, h.Title)
	}

	// The test documents actual behavior: rl allows multiple bannered relics
	// The hd hook command enforces singleton behavior
	if len(hookedRelics) != 2 {
		t.Errorf("expected 2 bannered relics (bd allows multiple), got %d", len(hookedRelics))
	}
}

// TestHookSlot_Unhook verifies that a bead can be unhooked by changing status.
func TestHookSlot_Unhook(t *testing.T) {
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	townRoot, raiderDir := setupHookTestTown(t)
	_ = townRoot

	rigDir := filepath.Join(raiderDir, "..", "..", "warchief", "warband")
	initRelicsDB(t, rigDir)

	b := relics.New(rigDir)
	agentID := "horde/raiders/toast"

	// Create and hook a bead
	issue, err := b.Create(relics.CreateOptions{
		Title:    "Task to unhook",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create bead: %v", err)
	}

	status := relics.StatusHooked
	if err := b.Update(issue.ID, relics.UpdateOptions{
		Status:   &status,
		Assignee: &agentID,
	}); err != nil {
		t.Fatalf("hook bead: %v", err)
	}

	// Unhook by setting status back to open
	openStatus := "open"
	if err := b.Update(issue.ID, relics.UpdateOptions{
		Status: &openStatus,
	}); err != nil {
		t.Fatalf("unhook bead: %v", err)
	}

	// Verify no bannered relics remain
	hookedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: agentID,
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("list bannered relics: %v", err)
	}

	if len(hookedRelics) != 0 {
		t.Errorf("expected 0 bannered relics after unhook, got %d", len(hookedRelics))
	}
}

// TestHookSlot_DifferentAgents verifies that different agents can have different hooks.
func TestHookSlot_DifferentAgents(t *testing.T) {
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	townRoot, raiderDir := setupHookTestTown(t)

	// Create second raider directory
	raider2Dir := filepath.Join(townRoot, "horde", "raiders", "nux")
	if err := os.MkdirAll(raider2Dir, 0755); err != nil {
		t.Fatalf("mkdir raider2: %v", err)
	}

	rigDir := filepath.Join(raiderDir, "..", "..", "warchief", "warband")
	initRelicsDB(t, rigDir)

	b := relics.New(rigDir)
	agent1 := "horde/raiders/toast"
	agent2 := "horde/raiders/nux"
	status := relics.StatusHooked

	// Create and hook bead to first agent
	issue1, err := b.Create(relics.CreateOptions{
		Title:    "Toast's task",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create bead 1: %v", err)
	}

	if err := b.Update(issue1.ID, relics.UpdateOptions{
		Status:   &status,
		Assignee: &agent1,
	}); err != nil {
		t.Fatalf("hook bead to agent1: %v", err)
	}

	// Create and hook bead to second agent
	issue2, err := b.Create(relics.CreateOptions{
		Title:    "Nux's task",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create bead 2: %v", err)
	}

	if err := b.Update(issue2.ID, relics.UpdateOptions{
		Status:   &status,
		Assignee: &agent2,
	}); err != nil {
		t.Fatalf("hook bead to agent2: %v", err)
	}

	// Verify each agent has exactly one hook
	agent1Hooks, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: agent1,
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("list agent1 hooks: %v", err)
	}

	agent2Hooks, err := b.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: agent2,
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("list agent2 hooks: %v", err)
	}

	if len(agent1Hooks) != 1 {
		t.Errorf("agent1 should have 1 hook, got %d", len(agent1Hooks))
	}
	if len(agent2Hooks) != 1 {
		t.Errorf("agent2 should have 1 hook, got %d", len(agent2Hooks))
	}

	// Verify correct assignment
	if len(agent1Hooks) > 0 && agent1Hooks[0].ID != issue1.ID {
		t.Errorf("agent1 hook ID = %s, want %s", agent1Hooks[0].ID, issue1.ID)
	}
	if len(agent2Hooks) > 0 && agent2Hooks[0].ID != issue2.ID {
		t.Errorf("agent2 hook ID = %s, want %s", agent2Hooks[0].ID, issue2.ID)
	}
}

// TestHookSlot_HookPersistence verifies that hooks persist across relics object recreation.
func TestHookSlot_HookPersistence(t *testing.T) {
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	townRoot, raiderDir := setupHookTestTown(t)
	_ = townRoot

	rigDir := filepath.Join(raiderDir, "..", "..", "warchief", "warband")
	initRelicsDB(t, rigDir)

	agentID := "horde/raiders/toast"
	status := relics.StatusHooked

	// Create first relics instance and hook a bead
	b1 := relics.New(rigDir)
	issue, err := b1.Create(relics.CreateOptions{
		Title:    "Persistent task",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create bead: %v", err)
	}

	if err := b1.Update(issue.ID, relics.UpdateOptions{
		Status:   &status,
		Assignee: &agentID,
	}); err != nil {
		t.Fatalf("hook bead: %v", err)
	}

	// Create new relics instance (simulates session restart)
	b2 := relics.New(rigDir)

	// Verify hook persists
	hookedRelics, err := b2.List(relics.ListOptions{
		Status:   relics.StatusHooked,
		Assignee: agentID,
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("list bannered relics with new instance: %v", err)
	}

	if len(hookedRelics) != 1 {
		t.Errorf("expected hook to persist, got %d bannered relics", len(hookedRelics))
	}

	if len(hookedRelics) > 0 && hookedRelics[0].ID != issue.ID {
		t.Errorf("persisted hook ID = %s, want %s", hookedRelics[0].ID, issue.ID)
	}
}

// TestHookSlot_StatusTransitions tests valid status transitions for bannered relics.
func TestHookSlot_StatusTransitions(t *testing.T) {
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	townRoot, raiderDir := setupHookTestTown(t)
	_ = townRoot

	rigDir := filepath.Join(raiderDir, "..", "..", "warchief", "warband")
	initRelicsDB(t, rigDir)

	b := relics.New(rigDir)
	agentID := "horde/raiders/toast"

	// Create a bead
	issue, err := b.Create(relics.CreateOptions{
		Title:    "Status transition test",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create bead: %v", err)
	}

	// Test transitions: open -> bannered -> open -> bannered -> closed
	transitions := []struct {
		name   string
		status string
	}{
		{"banner", relics.StatusHooked},
		{"unhook", "open"},
		{"rehook", relics.StatusHooked},
	}

	for _, trans := range transitions {
		t.Run(trans.name, func(t *testing.T) {
			status := trans.status
			opts := relics.UpdateOptions{Status: &status}
			if trans.status == relics.StatusHooked {
				opts.Assignee = &agentID
			}

			if err := b.Update(issue.ID, opts); err != nil {
				t.Errorf("transition to %s failed: %v", trans.status, err)
			}

			// Verify status
			updated, err := b.Show(issue.ID)
			if err != nil {
				t.Errorf("show after %s: %v", trans.name, err)
				return
			}
			if updated.Status != trans.status {
				t.Errorf("status after %s = %s, want %s", trans.name, updated.Status, trans.status)
			}
		})
	}

	// Finally close the bead
	if err := b.Close(issue.ID); err != nil {
		t.Errorf("close bannered bead: %v", err)
	}

	// Verify it's closed
	closed, err := b.Show(issue.ID)
	if err != nil {
		t.Fatalf("show closed bead: %v", err)
	}
	if closed.Status != "closed" {
		t.Errorf("final status = %s, want closed", closed.Status)
	}
}
