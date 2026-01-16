package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func realPath(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("realpath: %v", err)
	}
	return real
}

func TestFindWithPrimaryMarker(t *testing.T) {
	// Create temp workspace structure
	root := realPath(t, t.TempDir())
	warchiefDir := filepath.Join(root, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(warchiefDir, "encampment.json")
	if err := os.WriteFile(townFile, []byte(`{"type":"encampment"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Create nested directory
	nested := filepath.Join(root, "some", "deep", "path")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	// Find from nested should return root
	found, err := Find(nested)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != root {
		t.Errorf("Find = %q, want %q", found, root)
	}
}

func TestFindWithSecondaryMarker(t *testing.T) {
	// Create temp workspace with just warchief/ directory
	root := realPath(t, t.TempDir())
	warchiefDir := filepath.Join(root, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create nested directory
	nested := filepath.Join(root, "warbands", "test")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	// Find from nested should return root
	found, err := Find(nested)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != root {
		t.Errorf("Find = %q, want %q", found, root)
	}
}

func TestFindNotFound(t *testing.T) {
	// Create temp dir with no markers
	dir := t.TempDir()

	found, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != "" {
		t.Errorf("Find = %q, want empty string", found)
	}
}

func TestFindOrErrorNotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := FindOrError(dir)
	if err != ErrNotFound {
		t.Errorf("FindOrError = %v, want ErrNotFound", err)
	}
}

func TestFindAtRoot(t *testing.T) {
	// Create workspace at temp root level
	root := realPath(t, t.TempDir())
	warchiefDir := filepath.Join(root, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(warchiefDir, "encampment.json")
	if err := os.WriteFile(townFile, []byte(`{"type":"encampment"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Find from root should return root
	found, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != root {
		t.Errorf("Find = %q, want %q", found, root)
	}
}

func TestIsWorkspace(t *testing.T) {
	root := t.TempDir()

	// Not a workspace initially
	is, err := IsWorkspace(root)
	if err != nil {
		t.Fatalf("IsWorkspace: %v", err)
	}
	if is {
		t.Error("expected not a workspace initially")
	}

	// Add primary marker (warchief/encampment.json)
	warchiefDir := filepath.Join(root, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(warchiefDir, "encampment.json")
	if err := os.WriteFile(townFile, []byte(`{"type":"encampment"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Now is a workspace
	is, err = IsWorkspace(root)
	if err != nil {
		t.Fatalf("IsWorkspace: %v", err)
	}
	if !is {
		t.Error("expected to be a workspace")
	}
}

func TestFindFromSymlinkedDir(t *testing.T) {
	root := realPath(t, t.TempDir())
	warchiefDir := filepath.Join(root, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(warchiefDir, "encampment.json")
	if err := os.WriteFile(townFile, []byte(`{"type":"encampment"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	linkTarget := filepath.Join(root, "actual")
	if err := os.MkdirAll(linkTarget, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	linkName := filepath.Join(root, "linked")
	if err := os.Symlink(linkTarget, linkName); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	found, err := Find(linkName)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != root {
		t.Errorf("Find = %q, want %q", found, root)
	}
}

func TestFindPreservesSymlinkPath(t *testing.T) {
	realRoot := t.TempDir()
	resolved, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	symRoot := filepath.Join(t.TempDir(), "symlink-workspace")
	if err := os.Symlink(resolved, symRoot); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	warchiefDir := filepath.Join(symRoot, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(warchiefDir, "encampment.json")
	if err := os.WriteFile(townFile, []byte(`{}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	subdir := filepath.Join(symRoot, "warbands", "project", "raiders", "worker")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	townRoot, err := Find(subdir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if townRoot != symRoot {
		t.Errorf("Find returned %q, want %q (symlink path preserved)", townRoot, symRoot)
	}

	relPath, err := filepath.Rel(townRoot, subdir)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}

	if relPath != "warbands/project/raiders/worker" {
		t.Errorf("Rel = %q, want 'warbands/project/raiders/worker'", relPath)
	}
}

func TestFindSkipsNestedWorkspaceInWorktree(t *testing.T) {
	root := realPath(t, t.TempDir())

	if err := os.MkdirAll(filepath.Join(root, "warchief"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "warchief", "encampment.json"), []byte(`{"name":"outer"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	raiderDir := filepath.Join(root, "myrig", "raiders", "worker")
	if err := os.MkdirAll(filepath.Join(raiderDir, "warchief"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(raiderDir, "warchief", "encampment.json"), []byte(`{"name":"inner"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	found, err := Find(raiderDir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if found != root {
		t.Errorf("Find = %q, want %q (should skip nested workspace in raiders/)", found, root)
	}

	rel, _ := filepath.Rel(found, raiderDir)
	if rel != "myrig/raiders/worker" {
		t.Errorf("Rel = %q, want 'myrig/raiders/worker'", rel)
	}
}

func TestFindSkipsNestedWorkspaceInCrew(t *testing.T) {
	root := realPath(t, t.TempDir())

	if err := os.MkdirAll(filepath.Join(root, "warchief"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "warchief", "encampment.json"), []byte(`{"name":"outer"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	crewDir := filepath.Join(root, "myrig", "clan", "worker")
	if err := os.MkdirAll(filepath.Join(crewDir, "warchief"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(crewDir, "warchief", "encampment.json"), []byte(`{"name":"inner"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	found, err := Find(crewDir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if found != root {
		t.Errorf("Find = %q, want %q (should skip nested workspace in clan/)", found, root)
	}
}
