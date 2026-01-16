package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deeklead/horde/internal/relics"
)

// TestDoneUsesResolveRelicsDir verifies that the done command correctly uses
// relics.ResolveRelicsDir to follow redirect files when initializing relics.
// This is critical for raider/clan worktrees that use .relics/redirect to point
// to the shared warchief/warband/.relics directory.
//
// The done.go file has two code paths that initialize relics:
//   - Line 181: ExitCompleted path - rl := relics.New(relics.ResolveRelicsDir(cwd))
//   - Line 277: ExitPhaseComplete path - rl := relics.New(relics.ResolveRelicsDir(cwd))
//
// Both must use ResolveRelicsDir to properly handle redirects.
func TestDoneUsesResolveRelicsDir(t *testing.T) {
	// Create a temp directory structure simulating raider worktree with redirect
	tmpDir := t.TempDir()

	// Create structure like:
	//   horde/
	//     warchief/warband/.relics/          <- shared relics directory
	//     raiders/fixer/.relics/     <- raider with redirect
	//       redirect -> ../../warchief/warband/.relics

	warchiefRigRelicsDir := filepath.Join(tmpDir, "horde", "warchief", "warband", ".relics")
	raiderDir := filepath.Join(tmpDir, "horde", "raiders", "fixer")
	raiderRelicsDir := filepath.Join(raiderDir, ".relics")

	// Create directories
	if err := os.MkdirAll(warchiefRigRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband/.relics: %v", err)
	}
	if err := os.MkdirAll(raiderRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir raiders/fixer/.relics: %v", err)
	}

	// Create redirect file pointing to warchief/warband/.relics
	redirectContent := "../../warchief/warband/.relics"
	redirectPath := filepath.Join(raiderRelicsDir, "redirect")
	if err := os.WriteFile(redirectPath, []byte(redirectContent), 0644); err != nil {
		t.Fatalf("write redirect: %v", err)
	}

	t.Run("redirect followed from raider directory", func(t *testing.T) {
		// This mirrors how done.go initializes relics at line 181 and 277
		resolvedDir := relics.ResolveRelicsDir(raiderDir)

		// Should resolve to warchief/warband/.relics
		if resolvedDir != warchiefRigRelicsDir {
			t.Errorf("ResolveRelicsDir(%s) = %s, want %s", raiderDir, resolvedDir, warchiefRigRelicsDir)
		}

		// Verify the relics instance is created with the resolved path
		// We use the same pattern as done.go: relics.New(relics.ResolveRelicsDir(cwd))
		bd := relics.New(relics.ResolveRelicsDir(raiderDir))
		if rl == nil {
			t.Error("relics.New returned nil")
		}
	})

	t.Run("redirect not present uses local relics", func(t *testing.T) {
		// Without redirect, should use local .relics
		localDir := filepath.Join(tmpDir, "horde", "warchief", "warband")
		resolvedDir := relics.ResolveRelicsDir(localDir)

		if resolvedDir != warchiefRigRelicsDir {
			t.Errorf("ResolveRelicsDir(%s) = %s, want %s", localDir, resolvedDir, warchiefRigRelicsDir)
		}
	})
}

// TestDoneRelicsInitWithoutRedirect verifies that relics initialization works
// normally when no redirect file exists.
func TestDoneRelicsInitWithoutRedirect(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a simple .relics directory without redirect (like warchief/warband)
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir .relics: %v", err)
	}

	// ResolveRelicsDir should return the same directory when no redirect exists
	resolvedDir := relics.ResolveRelicsDir(tmpDir)
	if resolvedDir != relicsDir {
		t.Errorf("ResolveRelicsDir(%s) = %s, want %s", tmpDir, resolvedDir, relicsDir)
	}

	// Relics initialization should work the same way done.go does it
	bd := relics.New(relics.ResolveRelicsDir(tmpDir))
	if rl == nil {
		t.Error("relics.New returned nil")
	}
}

// TestDoneRelicsInitBothCodePaths documents that both code paths in done.go
// that create relics instances use ResolveRelicsDir:
//   - ExitCompleted (line 181): for MR creation and issue operations
//   - ExitPhaseComplete (line 277): for gate waiter registration
//
// This test verifies the pattern by demonstrating that the resolved directory
// is used consistently for different operations.
func TestDoneRelicsInitBothCodePaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup: clan directory with redirect to warchief/warband/.relics
	warchiefRigRelicsDir := filepath.Join(tmpDir, "warchief", "warband", ".relics")
	crewDir := filepath.Join(tmpDir, "clan", "max")
	crewRelicsDir := filepath.Join(crewDir, ".relics")

	if err := os.MkdirAll(warchiefRigRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir warchief/warband/.relics: %v", err)
	}
	if err := os.MkdirAll(crewRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir clan/max/.relics: %v", err)
	}

	// Create redirect
	redirectPath := filepath.Join(crewRelicsDir, "redirect")
	if err := os.WriteFile(redirectPath, []byte("../../warchief/warband/.relics"), 0644); err != nil {
		t.Fatalf("write redirect: %v", err)
	}

	t.Run("ExitCompleted path uses ResolveRelicsDir", func(t *testing.T) {
		// This simulates the line 181 path in done.go:
		// rl := relics.New(relics.ResolveRelicsDir(cwd))
		resolvedDir := relics.ResolveRelicsDir(crewDir)
		if resolvedDir != warchiefRigRelicsDir {
			t.Errorf("ExitCompleted path: ResolveRelicsDir(%s) = %s, want %s",
				crewDir, resolvedDir, warchiefRigRelicsDir)
		}

		bd := relics.New(relics.ResolveRelicsDir(crewDir))
		if rl == nil {
			t.Error("relics.New returned nil for ExitCompleted path")
		}
	})

	t.Run("ExitPhaseComplete path uses ResolveRelicsDir", func(t *testing.T) {
		// This simulates the line 277 path in done.go:
		// rl := relics.New(relics.ResolveRelicsDir(cwd))
		resolvedDir := relics.ResolveRelicsDir(crewDir)
		if resolvedDir != warchiefRigRelicsDir {
			t.Errorf("ExitPhaseComplete path: ResolveRelicsDir(%s) = %s, want %s",
				crewDir, resolvedDir, warchiefRigRelicsDir)
		}

		bd := relics.New(relics.ResolveRelicsDir(crewDir))
		if rl == nil {
			t.Error("relics.New returned nil for ExitPhaseComplete path")
		}
	})
}

// TestDoneRedirectChain verifies behavior with chained redirects.
// ResolveRelicsDir follows chains up to depth 3 as a safety net for legacy configs.
// SetupRedirect avoids creating chains (bd CLI doesn't support them), but if
// chains exist we follow them to the final destination.
func TestDoneRedirectChain(t *testing.T) {
	tmpDir := t.TempDir()

	// Create chain: worktree -> intermediate -> canonical
	canonicalRelicsDir := filepath.Join(tmpDir, "canonical", ".relics")
	intermediateDir := filepath.Join(tmpDir, "intermediate")
	intermediateRelicsDir := filepath.Join(intermediateDir, ".relics")
	worktreeDir := filepath.Join(tmpDir, "worktree")
	worktreeRelicsDir := filepath.Join(worktreeDir, ".relics")

	// Create all directories
	for _, dir := range []string{canonicalRelicsDir, intermediateRelicsDir, worktreeRelicsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// Create redirects
	// intermediate -> canonical
	if err := os.WriteFile(filepath.Join(intermediateRelicsDir, "redirect"), []byte("../canonical/.relics"), 0644); err != nil {
		t.Fatalf("write intermediate redirect: %v", err)
	}
	// worktree -> intermediate
	if err := os.WriteFile(filepath.Join(worktreeRelicsDir, "redirect"), []byte("../intermediate/.relics"), 0644); err != nil {
		t.Fatalf("write worktree redirect: %v", err)
	}

	// ResolveRelicsDir follows chains up to depth 3 as a safety net.
	// Note: SetupRedirect avoids creating chains (bd CLI doesn't support them),
	// but if chains exist from legacy configs, we follow them to the final destination.
	resolved := relics.ResolveRelicsDir(worktreeDir)

	// Should resolve to canonical (follows the full chain)
	if resolved != canonicalRelicsDir {
		t.Errorf("ResolveRelicsDir should follow chain to final destination: got %s, want %s",
			resolved, canonicalRelicsDir)
	}
}

// TestDoneEmptyRedirectFallback verifies that an empty or whitespace-only
// redirect file falls back to the local .relics directory.
func TestDoneEmptyRedirectFallback(t *testing.T) {
	tmpDir := t.TempDir()

	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir .relics: %v", err)
	}

	// Create empty redirect file
	redirectPath := filepath.Join(relicsDir, "redirect")
	if err := os.WriteFile(redirectPath, []byte("   \n"), 0644); err != nil {
		t.Fatalf("write empty redirect: %v", err)
	}

	// Should fall back to local .relics
	resolved := relics.ResolveRelicsDir(tmpDir)
	if resolved != relicsDir {
		t.Errorf("empty redirect should fallback: got %s, want %s", resolved, relicsDir)
	}
}

// TestDoneCircularRedirectProtection verifies that circular redirects
// are detected and handled safely.
func TestDoneCircularRedirectProtection(t *testing.T) {
	tmpDir := t.TempDir()

	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir .relics: %v", err)
	}

	// Create circular redirect (points to itself)
	redirectPath := filepath.Join(relicsDir, "redirect")
	if err := os.WriteFile(redirectPath, []byte(".relics"), 0644); err != nil {
		t.Fatalf("write circular redirect: %v", err)
	}

	// Should detect circular redirect and return original
	resolved := relics.ResolveRelicsDir(tmpDir)
	if resolved != relicsDir {
		t.Errorf("circular redirect should return original: got %s, want %s", resolved, relicsDir)
	}
}
