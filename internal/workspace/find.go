// Package workspace provides workspace detection and management.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deeklead/horde/internal/config"
)

// ErrNotFound indicates no workspace was found.
var ErrNotFound = errors.New("not in a Horde workspace")

// Markers used to detect a Horde workspace.
const (
	// PrimaryMarker is the main config file that identifies a workspace.
	// The encampment.json file lives in warchief/ along with other warchief config.
	PrimaryMarker = "warchief/encampment.json"

	// SecondaryMarker is an alternative indicator at the encampment level.
	// Note: This can match warband-level warchiefs too, so we continue searching
	// upward after finding this to look for primary markers.
	SecondaryMarker = "warchief"
)

// Find locates the encampment root by walking up from the given directory.
// It prefers warchief/encampment.json over warchief/ directory as workspace marker.
// When in a worktree path (raiders/ or clan/), continues to outermost workspace.
// Does not resolve symlinks to stay consistent with os.Getwd().
func Find(startDir string) (string, error) {
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}

	inWorktree := isInWorktreePath(absDir)
	var primaryMatch, secondaryMatch string

	current := absDir
	for {
		if _, err := os.Stat(filepath.Join(current, PrimaryMarker)); err == nil {
			if !inWorktree {
				return current, nil
			}
			primaryMatch = current
		}

		if secondaryMatch == "" {
			if info, err := os.Stat(filepath.Join(current, SecondaryMarker)); err == nil && info.IsDir() {
				secondaryMatch = current
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			if primaryMatch != "" {
				return primaryMatch, nil
			}
			return secondaryMatch, nil
		}
		current = parent
	}
}

func isInWorktreePath(path string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(path, sep+"raiders"+sep) || strings.Contains(path, sep+"clan"+sep)
}

// FindOrError is like Find but returns a user-friendly error if not found.
func FindOrError(startDir string) (string, error) {
	root, err := Find(startDir)
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", ErrNotFound
	}
	return root, nil
}

// FindFromCwd locates the encampment root from the current working directory.
func FindFromCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current directory: %w", err)
	}
	return Find(cwd)
}

// FindFromCwdOrError is like FindFromCwd but returns an error if not found.
// If getcwd fails (e.g., worktree deleted), falls back to HD_ENCAMPMENT_ROOT env var.
func FindFromCwdOrError() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		// Fallback: try HD_ENCAMPMENT_ROOT env var (set by raider sessions)
		if townRoot := os.Getenv("HD_ENCAMPMENT_ROOT"); townRoot != "" {
			// Verify it's actually a workspace
			if _, statErr := os.Stat(filepath.Join(townRoot, PrimaryMarker)); statErr == nil {
				return townRoot, nil
			}
		}
		return "", fmt.Errorf("getting current directory: %w", err)
	}
	return FindOrError(cwd)
}

// FindFromCwdWithFallback is like FindFromCwdOrError but returns (townRoot, cwd, error).
// If getcwd fails, returns (townRoot, "", nil) using HD_ENCAMPMENT_ROOT fallback.
// This is useful for commands like `hd done` that need to continue even if the
// working directory is deleted (e.g., raider worktree nuked by Witness).
func FindFromCwdWithFallback() (townRoot string, cwd string, err error) {
	cwd, err = os.Getwd()
	if err != nil {
		// Fallback: try HD_ENCAMPMENT_ROOT env var
		if townRoot = os.Getenv("HD_ENCAMPMENT_ROOT"); townRoot != "" {
			// Verify it's actually a workspace
			if _, statErr := os.Stat(filepath.Join(townRoot, PrimaryMarker)); statErr == nil {
				return townRoot, "", nil // cwd is gone but townRoot is valid
			}
		}
		return "", "", fmt.Errorf("getting current directory: %w", err)
	}

	townRoot, err = FindOrError(cwd)
	if err != nil {
		return "", "", err
	}
	return townRoot, cwd, nil
}

// IsWorkspace checks if the given directory is a Horde workspace root.
// A directory is a workspace if it has a primary marker (warchief/encampment.json)
// or a secondary marker (warchief/ directory).
func IsWorkspace(dir string) (bool, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false, fmt.Errorf("resolving path: %w", err)
	}

	// Check for primary marker (warchief/encampment.json)
	primaryPath := filepath.Join(absDir, PrimaryMarker)
	if _, err := os.Stat(primaryPath); err == nil {
		return true, nil
	}

	// Check for secondary marker (warchief/ directory)
	secondaryPath := filepath.Join(absDir, SecondaryMarker)
	info, err := os.Stat(secondaryPath)
	if err == nil && info.IsDir() {
		return true, nil
	}

	return false, nil
}

// GetTownName loads the encampment name from the workspace's encampment.json config.
// This is used for generating unique tmux session names that avoid collisions
// when running multiple Horde instances.
func GetTownName(townRoot string) (string, error) {
	townConfigPath := filepath.Join(townRoot, PrimaryMarker)
	townConfig, err := config.LoadTownConfig(townConfigPath)
	if err != nil {
		return "", fmt.Errorf("loading encampment config: %w", err)
	}
	return townConfig.Name, nil
}

// GetTownNameFromCwd locates the encampment root from the current working directory
// and returns the encampment name from its configuration.
func GetTownNameFromCwd() (string, error) {
	townRoot, err := FindFromCwdOrError()
	if err != nil {
		return "", err
	}
	return GetTownName(townRoot)
}

// MustGetTownName returns the encampment name or panics if it cannot be loaded.
// Use sparingly - prefer GetTownName with proper error handling.
func MustGetTownName(townRoot string) string {
	name, err := GetTownName(townRoot)
	if err != nil {
		panic(fmt.Sprintf("failed to get encampment name: %v", err))
	}
	return name
}
