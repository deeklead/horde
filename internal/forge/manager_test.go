package forge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/deeklead/horde/internal/warband"
)

func setupTestManager(t *testing.T) (*Manager, string) {
	t.Helper()

	// Create temp directory structure
	tmpDir := t.TempDir()
	rigPath := filepath.Join(tmpDir, "testrig")
	if err := os.MkdirAll(filepath.Join(rigPath, ".runtime"), 0755); err != nil {
		t.Fatalf("mkdir .runtime: %v", err)
	}

	r := &warband.Warband{
		Name: "testrig",
		Path: rigPath,
	}

	return NewManager(r), rigPath
}

func TestManager_GetMR(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create a test MR in the pending queue
	mr := &MergeRequest{
		ID:       "hd-mr-abc123",
		Branch:   "raider/Toast/gt-xyz",
		Worker:   "Toast",
		IssueID:  "hd-xyz",
		Status:   MROpen,
		Error:    "test failure",
	}

	if err := mgr.RegisterMR(mr); err != nil {
		t.Fatalf("RegisterMR: %v", err)
	}

	t.Run("find existing MR", func(t *testing.T) {
		found, err := mgr.GetMR("hd-mr-abc123")
		if err != nil {
			t.Errorf("GetMR() unexpected error: %v", err)
		}
		if found == nil {
			t.Fatal("GetMR() returned nil")
		}
		if found.ID != mr.ID {
			t.Errorf("GetMR() ID = %s, want %s", found.ID, mr.ID)
		}
	})

	t.Run("MR not found", func(t *testing.T) {
		_, err := mgr.GetMR("nonexistent-mr")
		if err != ErrMRNotFound {
			t.Errorf("GetMR() error = %v, want %v", err, ErrMRNotFound)
		}
	})
}

func TestManager_Retry(t *testing.T) {
	t.Run("retry failed MR clears error", func(t *testing.T) {
		mgr, _ := setupTestManager(t)

		// Create a failed MR
		mr := &MergeRequest{
			ID:       "hd-mr-failed",
			Branch:   "raider/Toast/gt-xyz",
			Worker:   "Toast",
			Status:   MROpen,
			Error:    "merge conflict",
		}

		if err := mgr.RegisterMR(mr); err != nil {
			t.Fatalf("RegisterMR: %v", err)
		}

		// Retry without processing
		err := mgr.Retry("hd-mr-failed", false)
		if err != nil {
			t.Errorf("Retry() unexpected error: %v", err)
		}

		// Verify error was cleared
		found, _ := mgr.GetMR("hd-mr-failed")
		if found.Error != "" {
			t.Errorf("Retry() error not cleared, got %s", found.Error)
		}
	})

	t.Run("retry non-failed MR fails", func(t *testing.T) {
		mgr, _ := setupTestManager(t)

		// Create a successful MR (no error)
		mr := &MergeRequest{
			ID:     "hd-mr-success",
			Branch: "raider/Toast/gt-abc",
			Worker: "Toast",
			Status: MROpen,
			Error:  "", // No error
		}

		if err := mgr.RegisterMR(mr); err != nil {
			t.Fatalf("RegisterMR: %v", err)
		}

		err := mgr.Retry("hd-mr-success", false)
		if err != ErrMRNotFailed {
			t.Errorf("Retry() error = %v, want %v", err, ErrMRNotFailed)
		}
	})

	t.Run("retry nonexistent MR fails", func(t *testing.T) {
		mgr, _ := setupTestManager(t)

		err := mgr.Retry("nonexistent", false)
		if err != ErrMRNotFound {
			t.Errorf("Retry() error = %v, want %v", err, ErrMRNotFound)
		}
	})
}

func TestManager_RegisterMR(t *testing.T) {
	mgr, rigPath := setupTestManager(t)

	mr := &MergeRequest{
		ID:           "hd-mr-new",
		Branch:       "raider/Cheedo/gt-123",
		Worker:       "Cheedo",
		IssueID:      "hd-123",
		TargetBranch: "main",
		CreatedAt:    time.Now(),
		Status:       MROpen,
	}

	if err := mgr.RegisterMR(mr); err != nil {
		t.Fatalf("RegisterMR: %v", err)
	}

	// Verify it was saved to disk
	stateFile := filepath.Join(rigPath, ".runtime", "forge.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}

	var ref Forge
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}

	if ref.PendingMRs == nil {
		t.Fatal("PendingMRs is nil")
	}

	saved, ok := ref.PendingMRs["hd-mr-new"]
	if !ok {
		t.Fatal("MR not found in PendingMRs")
	}

	if saved.Worker != "Cheedo" {
		t.Errorf("saved MR worker = %s, want Cheedo", saved.Worker)
	}
}
