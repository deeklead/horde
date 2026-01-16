// Package relics provides redirect resolution for relics databases.
package relics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveRelicsDir returns the actual relics directory, following any redirect.
// If workDir/.relics/redirect exists, it reads the redirect path and resolves it
// relative to workDir (not the .relics directory). Otherwise, returns workDir/.relics.
//
// This is essential for clan workers and raiders that use shared relics via redirect.
// The redirect file contains a relative path like "../../warchief/warband/.relics".
//
// Example: if we're at clan/max/ and .relics/redirect contains "../../warchief/warband/.relics",
// the redirect is resolved from clan/max/ (not clan/max/.relics/), giving us
// warchief/warband/.relics at the warband root level.
//
// Circular redirect detection: If the resolved path equals the original relics directory,
// this indicates an errant redirect file that should be removed. The function logs a
// warning and returns the original relics directory.
func ResolveRelicsDir(workDir string) string {
	relicsDir := filepath.Join(workDir, ".relics")
	redirectPath := filepath.Join(relicsDir, "redirect")

	// Check for redirect file
	data, err := os.ReadFile(redirectPath) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		// No redirect, use local .relics
		return relicsDir
	}

	// Read and clean the redirect path
	redirectTarget := strings.TrimSpace(string(data))
	if redirectTarget == "" {
		return relicsDir
	}

	// Resolve relative to workDir (the redirect is written from the perspective
	// of being inside workDir, not inside workDir/.relics)
	// e.g., redirect contains "../../warchief/warband/.relics"
	// from clan/max/, this resolves to warchief/warband/.relics
	resolved := filepath.Join(workDir, redirectTarget)

	// Clean the path to resolve .. components
	resolved = filepath.Clean(resolved)

	// Detect circular redirects: if resolved path equals original relics dir,
	// this is an errant redirect file (e.g., redirect in warchief/warband/.relics pointing to itself)
	if resolved == relicsDir {
		fmt.Fprintf(os.Stderr, "Warning: circular redirect detected in %s (points to itself), ignoring redirect\n", redirectPath)
		// Remove the errant redirect file to prevent future warnings
		if err := os.Remove(redirectPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not remove errant redirect file: %v\n", err)
		}
		return relicsDir
	}

	// Follow redirect chains (e.g., clan/.relics -> warband/.relics -> warchief/warband/.relics)
	// This is intentional for the warband-level redirect architecture.
	// Limit depth to prevent infinite loops from misconfigured redirects.
	return resolveRelicsDirWithDepth(resolved, 3)
}

// resolveRelicsDirWithDepth follows redirect chains with a depth limit.
func resolveRelicsDirWithDepth(relicsDir string, maxDepth int) string {
	if maxDepth <= 0 {
		fmt.Fprintf(os.Stderr, "Warning: redirect chain too deep at %s, stopping\n", relicsDir)
		return relicsDir
	}

	redirectPath := filepath.Join(relicsDir, "redirect")
	data, err := os.ReadFile(redirectPath) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		// No redirect, this is the final destination
		return relicsDir
	}

	redirectTarget := strings.TrimSpace(string(data))
	if redirectTarget == "" {
		return relicsDir
	}

	// Resolve relative to parent of relicsDir (the workDir)
	workDir := filepath.Dir(relicsDir)
	resolved := filepath.Clean(filepath.Join(workDir, redirectTarget))

	// Detect circular redirect
	if resolved == relicsDir {
		fmt.Fprintf(os.Stderr, "Warning: circular redirect detected in %s, stopping\n", redirectPath)
		return relicsDir
	}

	// Recursively follow
	return resolveRelicsDirWithDepth(resolved, maxDepth-1)
}

// cleanRelicsRuntimeFiles removes gitignored runtime files from a .relics directory
// while preserving tracked files (rituals/, README.md, config.yaml, .gitignore).
// This is safe to call even if the directory doesn't exist.
func cleanRelicsRuntimeFiles(relicsDir string) error {
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		return nil // Nothing to clean
	}

	// Runtime files/patterns that are gitignored and safe to remove
	runtimePatterns := []string{
		// SQLite databases
		"*.db", "*.db-*", "*.db?*",
		// Daemon runtime
		"daemon.lock", "daemon.log", "daemon.pid", "bd.sock",
		// Sync state
		"sync-state.json", "last-touched", "metadata.json",
		// Version tracking
		".local_version",
		// Redirect file (we're about to recreate it)
		"redirect",
		// Merge artifacts
		"relics.base.*", "relics.left.*", "relics.right.*",
		// JSONL files (tracked but will be redirected, safe to remove in worktrees)
		"issues.jsonl", "interactions.jsonl",
		// Runtime directories
		"mq",
	}

	var firstErr error
	for _, pattern := range runtimePatterns {
		matches, err := filepath.Glob(filepath.Join(relicsDir, pattern))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, match := range matches {
			if err := os.RemoveAll(match); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// SetupRedirect creates a .relics/redirect file for a worktree to point to the warband's shared relics.
// This is used by clan, raiders, and forge worktrees to share the warband's relics database.
//
// Parameters:
//   - townRoot: the encampment root directory (e.g., ~/horde)
//   - worktreePath: the worktree directory (e.g., <warband>/clan/<name> or <warband>/forge/warband)
//
// The function:
//  1. Computes the relative path from worktree to warband-level .relics
//  2. Cleans up runtime files (preserving tracked files like rituals/)
//  3. Creates the redirect file
//
// Safety: This function refuses to create redirects in the canonical relics location
// (warchief/warband) to prevent circular redirect chains.
func SetupRedirect(townRoot, worktreePath string) error {
	// Get warband root from worktree path
	// worktreePath = <encampment>/<warband>/clan/<name> or <encampment>/<warband>/forge/warband etc.
	relPath, err := filepath.Rel(townRoot, worktreePath)
	if err != nil {
		return fmt.Errorf("computing relative path: %w", err)
	}
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid worktree path: must be at least 2 levels deep from encampment root")
	}

	// Safety check: prevent creating redirect in canonical relics location (warchief/warband)
	// This would create a circular redirect chain since warband/.relics redirects to warchief/warband/.relics
	if len(parts) >= 2 && parts[1] == "warchief" {
		return fmt.Errorf("cannot create redirect in canonical relics location (warchief/warband)")
	}

	rigRoot := filepath.Join(townRoot, parts[0])
	rigRelicsPath := filepath.Join(rigRoot, ".relics")
	warchiefRelicsPath := filepath.Join(rigRoot, "warchief", "warband", ".relics")

	// Check warband-level .relics first, fall back to warchief/warband/.relics (tracked relics architecture)
	usesWarchiefFallback := false
	if _, err := os.Stat(rigRelicsPath); os.IsNotExist(err) {
		// No warband/.relics - check for warchief/warband/.relics (tracked relics architecture)
		if _, err := os.Stat(warchiefRelicsPath); os.IsNotExist(err) {
			return fmt.Errorf("no relics found at %s or %s", rigRelicsPath, warchiefRelicsPath)
		}
		// Using warchief fallback - warn user to run rl doctor
		fmt.Fprintf(os.Stderr, "Warning: warband .relics not found at %s, using %s\n", rigRelicsPath, warchiefRelicsPath)
		fmt.Fprintf(os.Stderr, "  Run 'bd doctor' to fix warband relics configuration\n")
		usesWarchiefFallback = true
	}

	// Clean up runtime files in .relics/ but preserve tracked files (rituals/, README.md, etc.)
	worktreeRelicsDir := filepath.Join(worktreePath, ".relics")
	if err := cleanRelicsRuntimeFiles(worktreeRelicsDir); err != nil {
		return fmt.Errorf("cleaning runtime files: %w", err)
	}

	// Create .relics directory if it doesn't exist
	if err := os.MkdirAll(worktreeRelicsDir, 0755); err != nil {
		return fmt.Errorf("creating .relics dir: %w", err)
	}

	// Compute relative path from worktree to warband root
	// e.g., clan/<name> (depth 2) -> ../../.relics
	//       forge/warband (depth 2) -> ../../.relics
	depth := len(parts) - 1 // subtract 1 for warband name itself
	upPath := strings.Repeat("../", depth)

	var redirectPath string
	if usesWarchiefFallback {
		// Direct redirect to warchief/warband/.relics since warband/.relics doesn't exist
		redirectPath = upPath + "warchief/warband/.relics"
	} else {
		redirectPath = upPath + ".relics"

		// Check if warband-level relics has a redirect (tracked relics case).
		// If so, redirect directly to the final destination to avoid chains.
		// The rl CLI doesn't support redirect chains, so we must skip intermediate hops.
		rigRedirectPath := filepath.Join(rigRelicsPath, "redirect")
		if data, err := os.ReadFile(rigRedirectPath); err == nil {
			rigRedirectTarget := strings.TrimSpace(string(data))
			if rigRedirectTarget != "" {
				// Warband has redirect (e.g., "warchief/warband/.relics" for tracked relics).
				// Redirect worktree directly to the final destination.
				redirectPath = upPath + rigRedirectTarget
			}
		}
	}

	// Create redirect file
	redirectFile := filepath.Join(worktreeRelicsDir, "redirect")
	if err := os.WriteFile(redirectFile, []byte(redirectPath+"\n"), 0644); err != nil {
		return fmt.Errorf("creating redirect file: %w", err)
	}

	return nil
}
