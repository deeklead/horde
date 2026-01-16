package raider

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/OWNER/horde/internal/git"
	"github.com/OWNER/horde/internal/warband"
)

func TestStateIsActive(t *testing.T) {
	tests := []struct {
		state  State
		active bool
	}{
		{StateWorking, true},
		{StateDone, false},
		{StateStuck, false},
		// Legacy active state is treated as active
		{StateActive, true},
	}

	for _, tt := range tests {
		if got := tt.state.IsActive(); got != tt.active {
			t.Errorf("%s.IsActive() = %v, want %v", tt.state, got, tt.active)
		}
	}
}

func TestStateIsWorking(t *testing.T) {
	tests := []struct {
		state   State
		working bool
	}{
		{StateActive, false},
		{StateWorking, true},
		{StateDone, false},
		{StateStuck, false},
	}

	for _, tt := range tests {
		if got := tt.state.IsWorking(); got != tt.working {
			t.Errorf("%s.IsWorking() = %v, want %v", tt.state, got, tt.working)
		}
	}
}

func TestRaiderSummary(t *testing.T) {
	p := &Raider{
		Name:  "Toast",
		State: StateWorking,
		Issue: "gt-abc",
	}

	summary := p.Summary()
	if summary.Name != "Toast" {
		t.Errorf("Name = %q, want Toast", summary.Name)
	}
	if summary.State != StateWorking {
		t.Errorf("State = %v, want StateWorking", summary.State)
	}
	if summary.Issue != "gt-abc" {
		t.Errorf("Issue = %q, want gt-abc", summary.Issue)
	}
}

func TestListEmpty(t *testing.T) {
	root := t.TempDir()
	r := &warband.Warband{
		Name: "test-warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	raiders, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(raiders) != 0 {
		t.Errorf("raiders count = %d, want 0", len(raiders))
	}
}

func TestGetNotFound(t *testing.T) {
	root := t.TempDir()
	r := &warband.Warband{
		Name: "test-warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	_, err := m.Get("nonexistent")
	if err != ErrRaiderNotFound {
		t.Errorf("Get = %v, want ErrRaiderNotFound", err)
	}
}

func TestRemoveNotFound(t *testing.T) {
	root := t.TempDir()
	r := &warband.Warband{
		Name: "test-warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	err := m.Remove("nonexistent", false)
	if err != ErrRaiderNotFound {
		t.Errorf("Remove = %v, want ErrRaiderNotFound", err)
	}
}

func TestRaiderDir(t *testing.T) {
	r := &warband.Warband{
		Name: "test-warband",
		Path: "/home/user/ai/test-warband",
	}
	m := NewManager(r, git.NewGit(r.Path), nil)

	dir := m.raiderDir("Toast")
	expected := "/home/user/ai/test-warband/raiders/Toast"
	if dir != expected {
		t.Errorf("raiderDir = %q, want %q", dir, expected)
	}
}

func TestAssigneeID(t *testing.T) {
	r := &warband.Warband{
		Name: "test-warband",
		Path: "/home/user/ai/test-warband",
	}
	m := NewManager(r, git.NewGit(r.Path), nil)

	id := m.assigneeID("Toast")
	expected := "test-warband/Toast"
	if id != expected {
		t.Errorf("assigneeID = %q, want %q", id, expected)
	}
}

// Note: State persistence tests removed - state is now derived from relics assignee field.
// Integration tests should verify relics-based state management.

func TestGetReturnsWorkingWithoutRelics(t *testing.T) {
	// When relics is not available, Get should return StateWorking
	// (assume the raider is doing something if it exists)
	//
	// Skip if rl is installed - the test assumes rl is unavailable, but when bd
	// is present it queries relics and returns actual state instead of defaulting.
	if _, err := exec.LookPath("rl"); err == nil {
		t.Skip("skipping: rl is installed, test requires rl to be unavailable")
	}

	root := t.TempDir()
	raiderDir := filepath.Join(root, "raiders", "Test")
	if err := os.MkdirAll(raiderDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create warchief/warband directory for relics (but no actual relics)
	warchiefRigDir := filepath.Join(root, "warchief", "warband")
	if err := os.MkdirAll(warchiefRigDir, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband: %v", err)
	}

	r := &warband.Warband{
		Name: "test-warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	// Get should return raider with StateWorking (assume active if relics unavailable)
	raider, err := m.Get("Test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if raider.Name != "Test" {
		t.Errorf("Name = %q, want Test", raider.Name)
	}
	if raider.State != StateWorking {
		t.Errorf("State = %v, want StateWorking (relics not available)", raider.State)
	}
}

func TestListWithRaiders(t *testing.T) {
	root := t.TempDir()

	// Create some raider directories (state is now derived from relics, not state files)
	for _, name := range []string{"Toast", "Cheedo"} {
		raiderDir := filepath.Join(root, "raiders", name)
		if err := os.MkdirAll(raiderDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "raiders", ".claude"), 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	// Create warchief/warband for relics path
	warchiefRig := filepath.Join(root, "warchief", "warband")
	if err := os.MkdirAll(warchiefRig, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband: %v", err)
	}

	r := &warband.Warband{
		Name: "test-warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	raiders, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(raiders) != 2 {
		t.Errorf("raiders count = %d, want 2", len(raiders))
	}
}

// Note: TestSetState, TestAssignIssue, and TestClearIssue were removed.
// These operations now require a running relics instance and are tested
// via integration tests. The unit tests here focus on testing the basic
// raider lifecycle operations that don't require relics.

func TestSetStateWithoutRelics(t *testing.T) {
	// SetState should not error when relics is not available
	root := t.TempDir()
	raiderDir := filepath.Join(root, "raiders", "Test")
	if err := os.MkdirAll(raiderDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create warchief/warband for relics path
	warchiefRig := filepath.Join(root, "warchief", "warband")
	if err := os.MkdirAll(warchiefRig, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband: %v", err)
	}

	r := &warband.Warband{
		Name: "test-warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	// SetState should succeed (no-op when no issue assigned)
	err := m.SetState("Test", StateActive)
	if err != nil {
		t.Errorf("SetState: %v (expected no error when no relics/issue)", err)
	}
}

func TestClearIssueWithoutAssignment(t *testing.T) {
	// ClearIssue should not error when no issue is assigned
	root := t.TempDir()
	raiderDir := filepath.Join(root, "raiders", "Test")
	if err := os.MkdirAll(raiderDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create warchief/warband for relics path
	warchiefRig := filepath.Join(root, "warchief", "warband")
	if err := os.MkdirAll(warchiefRig, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband: %v", err)
	}

	r := &warband.Warband{
		Name: "test-warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	// ClearIssue should succeed even when no issue assigned
	err := m.ClearIssue("Test")
	if err != nil {
		t.Errorf("ClearIssue: %v (expected no error when no assignment)", err)
	}
}

// NOTE: TestInstallCLAUDETemplate tests were removed.
// We no longer write CLAUDE.md to worktrees - Horde context is injected
// ephemerally via SessionStart hook (gt rally) to prevent leaking internal
// architecture into project repos.

func TestAddWithOptions_HasAgentsMD(t *testing.T) {
	// This test verifies that AGENTS.md exists in raider worktrees after creation.
	// AGENTS.md is critical for raiders to "land the plane" properly.

	root := t.TempDir()

	// Create warchief/warband directory structure (this acts as repo base when no .repo.git)
	warchiefRig := filepath.Join(root, "warchief", "warband")
	if err := os.MkdirAll(warchiefRig, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband: %v", err)
	}

	// Initialize git repo in warchief/warband
	cmd := exec.Command("git", "init")
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	// Create AGENTS.md with test content
	agentsMDContent := []byte("# AGENTS.md\n\nTest content for raiders.\n")
	agentsMDPath := filepath.Join(warchiefRig, "AGENTS.md")
	if err := os.WriteFile(agentsMDPath, agentsMDContent, 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	// Commit AGENTS.md so it's part of the repo
	warchiefGit := git.NewGit(warchiefRig)
	if err := warchiefGit.Add("AGENTS.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := warchiefGit.Commit("Add AGENTS.md"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// AddWithOptions needs origin/main to exist. Add self as origin and create tracking ref.
	cmd = exec.Command("git", "remote", "add", "origin", warchiefRig)
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	// When using a local directory as remote, fetch doesn't create tracking branches.
	// Create origin/main manually since AddWithOptions expects origin/main by default.
	cmd = exec.Command("git", "update-ref", "refs/remotes/origin/main", "HEAD")
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git update-ref: %v\n%s", err, out)
	}

	// Create warband pointing to root
	r := &warband.Warband{
		Name: "warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	// Create raider via AddWithOptions
	raider, err := m.AddWithOptions("TestAgent", AddOptions{})
	if err != nil {
		t.Fatalf("AddWithOptions: %v", err)
	}

	// Verify AGENTS.md exists in the worktree
	worktreeAgentsMD := filepath.Join(raider.ClonePath, "AGENTS.md")
	if _, err := os.Stat(worktreeAgentsMD); os.IsNotExist(err) {
		t.Errorf("AGENTS.md does not exist in worktree at %s", worktreeAgentsMD)
	}

	// Verify content matches
	content, err := os.ReadFile(worktreeAgentsMD)
	if err != nil {
		t.Fatalf("read worktree AGENTS.md: %v", err)
	}
	if string(content) != string(agentsMDContent) {
		t.Errorf("AGENTS.md content = %q, want %q", string(content), string(agentsMDContent))
	}
}

func TestAddWithOptions_AgentsMDFallback(t *testing.T) {
	// This test verifies the fallback: if AGENTS.md is not in git,
	// it should be copied from warchief/warband.

	root := t.TempDir()

	// Create warchief/warband directory structure
	warchiefRig := filepath.Join(root, "warchief", "warband")
	if err := os.MkdirAll(warchiefRig, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband: %v", err)
	}

	// Initialize git repo in warchief/warband WITHOUT AGENTS.md in git
	cmd := exec.Command("git", "init")
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	// Create a dummy file and commit (repo needs at least one commit)
	dummyPath := filepath.Join(warchiefRig, "README.md")
	if err := os.WriteFile(dummyPath, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	warchiefGit := git.NewGit(warchiefRig)
	if err := warchiefGit.Add("README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := warchiefGit.Commit("Initial commit"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// AddWithOptions needs origin/main to exist. Add self as origin and create tracking ref.
	cmd = exec.Command("git", "remote", "add", "origin", warchiefRig)
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	// When using a local directory as remote, fetch doesn't create tracking branches.
	// Create origin/main manually since AddWithOptions expects origin/main by default.
	cmd = exec.Command("git", "update-ref", "refs/remotes/origin/main", "HEAD")
	cmd.Dir = warchiefRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git update-ref: %v\n%s", err, out)
	}

	// Now create AGENTS.md in warchief/warband (but NOT committed to git)
	// This simulates the fallback scenario
	agentsMDContent := []byte("# AGENTS.md\n\nFallback content.\n")
	agentsMDPath := filepath.Join(warchiefRig, "AGENTS.md")
	if err := os.WriteFile(agentsMDPath, agentsMDContent, 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	// Create warband pointing to root
	r := &warband.Warband{
		Name: "warband",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	// Create raider via AddWithOptions
	raider, err := m.AddWithOptions("TestFallback", AddOptions{})
	if err != nil {
		t.Fatalf("AddWithOptions: %v", err)
	}

	// Verify AGENTS.md exists in the worktree (via fallback copy)
	worktreeAgentsMD := filepath.Join(raider.ClonePath, "AGENTS.md")
	if _, err := os.Stat(worktreeAgentsMD); os.IsNotExist(err) {
		t.Errorf("AGENTS.md does not exist in worktree (fallback failed) at %s", worktreeAgentsMD)
	}

	// Verify content matches the fallback source
	content, err := os.ReadFile(worktreeAgentsMD)
	if err != nil {
		t.Fatalf("read worktree AGENTS.md: %v", err)
	}
	if string(content) != string(agentsMDContent) {
		t.Errorf("AGENTS.md content = %q, want %q", string(content), string(agentsMDContent))
	}
}
// TestReconcilePoolWith tests all permutations of directory and session existence.
// This is the core allocation policy logic.
//
// Truth table:
//   HasDir | HasSession | Result
//   -------|------------|------------------
//   false  | false      | available (not in-use)
//   true   | false      | in-use (normal finished raider)
//   false  | true       | orphan → kill session, available
//   true   | true       | in-use (normal working raider)
func TestReconcilePoolWith(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		namesWithDirs    []string
		namesWithSessions []string
		wantInUse        []string // names that should be marked in-use
		wantOrphans      []string // sessions that should be killed
	}{
		{
			name:             "no dirs, no sessions - all available",
			namesWithDirs:    []string{},
			namesWithSessions: []string{},
			wantInUse:        []string{},
			wantOrphans:      []string{},
		},
		{
			name:             "has dir, no session - in use",
			namesWithDirs:    []string{"toast"},
			namesWithSessions: []string{},
			wantInUse:        []string{"toast"},
			wantOrphans:      []string{},
		},
		{
			name:             "no dir, has session - orphan killed",
			namesWithDirs:    []string{},
			namesWithSessions: []string{"nux"},
			wantInUse:        []string{},
			wantOrphans:      []string{"nux"},
		},
		{
			name:             "has dir, has session - in use",
			namesWithDirs:    []string{"capable"},
			namesWithSessions: []string{"capable"},
			wantInUse:        []string{"capable"},
			wantOrphans:      []string{},
		},
		{
			name:             "mixed: one with dir, one orphan session",
			namesWithDirs:    []string{"toast"},
			namesWithSessions: []string{"toast", "nux"},
			wantInUse:        []string{"toast"},
			wantOrphans:      []string{"nux"},
		},
		{
			name:             "multiple dirs, no sessions",
			namesWithDirs:    []string{"toast", "nux", "capable"},
			namesWithSessions: []string{},
			wantInUse:        []string{"capable", "nux", "toast"},
			wantOrphans:      []string{},
		},
		{
			name:             "multiple orphan sessions",
			namesWithDirs:    []string{},
			namesWithSessions: []string{"slit", "rictus"},
			wantInUse:        []string{},
			wantOrphans:      []string{"rictus", "slit"},
		},
		{
			name:             "complex: dirs, valid sessions, orphan sessions",
			namesWithDirs:    []string{"toast", "capable"},
			namesWithSessions: []string{"toast", "nux", "slit"},
			wantInUse:        []string{"capable", "toast"},
			wantOrphans:      []string{"nux", "slit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for pool state
			tmpDir, err := os.MkdirTemp("", "reconcile-test-*")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			// Create warband and manager (nil tmux for unit test)
			r := &warband.Warband{
				Name: "testrig",
				Path: tmpDir,
			}
			m := NewManager(r, nil, nil)

			// Call ReconcilePoolWith
			m.ReconcilePoolWith(tt.namesWithDirs, tt.namesWithSessions)

			// Verify in-use names
			gotInUse := m.namePool.ActiveNames()
			sort.Strings(gotInUse)
			sort.Strings(tt.wantInUse)

			if len(gotInUse) != len(tt.wantInUse) {
				t.Errorf("in-use count: got %d, want %d", len(gotInUse), len(tt.wantInUse))
			}
			for i := range tt.wantInUse {
				if i >= len(gotInUse) || gotInUse[i] != tt.wantInUse[i] {
					t.Errorf("in-use names: got %v, want %v", gotInUse, tt.wantInUse)
					break
				}
			}

			// Verify orphans would be identified correctly
			// (actual killing requires tmux, tested separately)
			dirSet := make(map[string]bool)
			for _, name := range tt.namesWithDirs {
				dirSet[name] = true
			}
			var gotOrphans []string
			for _, name := range tt.namesWithSessions {
				if !dirSet[name] {
					gotOrphans = append(gotOrphans, name)
				}
			}
			sort.Strings(gotOrphans)
			sort.Strings(tt.wantOrphans)

			if len(gotOrphans) != len(tt.wantOrphans) {
				t.Errorf("orphan count: got %d, want %d", len(gotOrphans), len(tt.wantOrphans))
			}
			for i := range tt.wantOrphans {
				if i >= len(gotOrphans) || gotOrphans[i] != tt.wantOrphans[i] {
					t.Errorf("orphans: got %v, want %v", gotOrphans, tt.wantOrphans)
					break
				}
			}
		})
	}
}

// TestReconcilePoolWith_Allocation verifies that allocation respects reconciled state.
func TestReconcilePoolWith_Allocation(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "reconcile-alloc-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	r := &warband.Warband{
		Name: "testrig",
		Path: tmpDir,
	}
	m := NewManager(r, nil, nil)

	// Mark first few pool names as in-use via directories
	// (furiosa, nux, slit are first 3 in mad-max theme)
	m.ReconcilePoolWith([]string{"furiosa", "nux", "slit"}, []string{})

	// First allocation should skip in-use names
	name, err := m.namePool.Allocate()
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	// Should get "rictus" (4th in mad-max theme), not furiosa/nux/slit
	if name == "furiosa" || name == "nux" || name == "slit" {
		t.Errorf("allocated in-use name %q, should have skipped", name)
	}
	if name != "rictus" {
		t.Errorf("expected rictus (4th name), got %q", name)
	}
}

// TestReconcilePoolWith_OrphanDoesNotBlockAllocation verifies orphan sessions
// don't prevent name allocation (they're killed, freeing the name).
func TestReconcilePoolWith_OrphanDoesNotBlockAllocation(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "reconcile-orphan-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	r := &warband.Warband{
		Name: "testrig",
		Path: tmpDir,
	}
	m := NewManager(r, nil, nil)

	// furiosa has orphan session (no dir) - should NOT block allocation
	m.ReconcilePoolWith([]string{}, []string{"furiosa"})

	// furiosa should be available (orphan session killed, name freed)
	name, err := m.namePool.Allocate()
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	if name != "furiosa" {
		t.Errorf("expected furiosa (orphan freed), got %q", name)
	}
}
