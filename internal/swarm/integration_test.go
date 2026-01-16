package swarm

import (
	"testing"

	"github.com/OWNER/horde/internal/warband"
)

func TestGetWorkerBranch(t *testing.T) {
	r := &warband.Warband{
		Name: "test-warband",
		Path: "/tmp/test-warband",
	}
	m := NewManager(r)

	branch := m.GetWorkerBranch("sw-1", "Toast", "task-123")
	expected := "sw-1/Toast/task-123"
	if branch != expected {
		t.Errorf("branch = %q, want %q", branch, expected)
	}
}

// Note: Integration tests that require git operations and relics
// are covered by the E2E test (gt-kc7yj.4).
