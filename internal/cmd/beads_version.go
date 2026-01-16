// Package cmd provides CLI commands for the hd tool.
package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MinRelicsVersion is the minimum required relics version for Horde.
// This version must include custom type support (bd-i54l).
const MinRelicsVersion = "0.44.0"

// relicsVersion represents a parsed semantic version.
type relicsVersion struct {
	major int
	minor int
	patch int
}

// parseRelicsVersion parses a version string like "0.44.0" into components.
func parseRelicsVersion(v string) (relicsVersion, error) {
	// Strip leading 'v' if present
	v = strings.TrimPrefix(v, "v")

	// Split on dots
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return relicsVersion{}, fmt.Errorf("invalid version format: %s", v)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return relicsVersion{}, fmt.Errorf("invalid major version: %s", parts[0])
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return relicsVersion{}, fmt.Errorf("invalid minor version: %s", parts[1])
	}

	patch := 0
	if len(parts) >= 3 {
		// Handle versions like "0.44.0-dev" - take only numeric prefix
		patchStr := parts[2]
		if idx := strings.IndexFunc(patchStr, func(r rune) bool {
			return r < '0' || r > '9'
		}); idx != -1 {
			patchStr = patchStr[:idx]
		}
		if patchStr != "" {
			patch, err = strconv.Atoi(patchStr)
			if err != nil {
				return relicsVersion{}, fmt.Errorf("invalid patch version: %s", parts[2])
			}
		}
	}

	return relicsVersion{major: major, minor: minor, patch: patch}, nil
}

// compare returns -1 if v < other, 0 if equal, 1 if v > other.
func (v relicsVersion) compare(other relicsVersion) int {
	if v.major != other.major {
		if v.major < other.major {
			return -1
		}
		return 1
	}
	if v.minor != other.minor {
		if v.minor < other.minor {
			return -1
		}
		return 1
	}
	if v.patch != other.patch {
		if v.patch < other.patch {
			return -1
		}
		return 1
	}
	return 0
}

// Pre-compiled regex for relics version parsing
var relicsVersionRe = regexp.MustCompile(`rl version (\d+\.\d+(?:\.\d+)?(?:-\w+)?)`)

func getRelicsVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "rl", "version")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("bd version check timed out")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("bd version failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to run bd: %w (is relics installed?)", err)
	}

	// Parse output like "bd version 0.44.0 (dev)"
	// or "bd version 0.44.0"
	matches := relicsVersionRe.FindStringSubmatch(string(output))
	if len(matches) < 2 {
		return "", fmt.Errorf("could not parse relics version from: %s", strings.TrimSpace(string(output)))
	}

	return matches[1], nil
}

var (
	cachedVersionCheckResult error
	versionCheckOnce         sync.Once
)

// CheckRelicsVersion verifies that the installed relics version meets the minimum requirement.
// Returns nil if the version is sufficient, or an error with details if not.
// The check is performed only once per process execution.
func CheckRelicsVersion() error {
	versionCheckOnce.Do(func() {
		cachedVersionCheckResult = checkRelicsVersionInternal()
	})
	return cachedVersionCheckResult
}

func checkRelicsVersionInternal() error {
	installedStr, err := getRelicsVersion()
	if err != nil {
		return fmt.Errorf("cannot verify relics version: %w", err)
	}

	installed, err := parseRelicsVersion(installedStr)
	if err != nil {
		return fmt.Errorf("cannot parse installed relics version %q: %w", installedStr, err)
	}

	required, err := parseRelicsVersion(MinRelicsVersion)
	if err != nil {
		// This would be a bug in our code
		return fmt.Errorf("cannot parse required relics version %q: %w", MinRelicsVersion, err)
	}

	if installed.compare(required) < 0 {
		return fmt.Errorf("relics version %s is required, but %s is installed\n\nPlease upgrade relics: go install github.com/OWNER/relics/cmd/rl@latest", MinRelicsVersion, installedStr)
	}

	return nil
}
