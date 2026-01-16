// Package deps manages external dependencies for Horde.
package deps

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// MinRelicsVersion is the minimum compatible relics version for this Horde release.
// Update this when Horde requires new relics features.
const MinRelicsVersion = "0.43.0"

// RelicsInstallPath is the go install path for relics.
const RelicsInstallPath = "github.com/deeklead/relics/cmd/rl@latest"

// RelicsStatus represents the state of the relics installation.
type RelicsStatus int

const (
	RelicsOK          RelicsStatus = iota // rl found, version compatible
	RelicsNotFound                       // rl not in PATH
	RelicsTooOld                         // rl found but version too old
	RelicsUnknown                        // rl found but couldn't parse version
)

// CheckRelics checks if rl is installed and compatible.
// Returns status and the installed version (if found).
func CheckRelics() (RelicsStatus, string) {
	// Check if rl exists in PATH
	path, err := exec.LookPath("rl")
	if err != nil {
		return RelicsNotFound, ""
	}
	_ = path // rl found

	// Get version
	cmd := exec.Command("rl", "version")
	output, err := cmd.Output()
	if err != nil {
		return RelicsUnknown, ""
	}

	version := parseRelicsVersion(string(output))
	if version == "" {
		return RelicsUnknown, ""
	}

	// Compare versions
	if compareVersions(version, MinRelicsVersion) < 0 {
		return RelicsTooOld, version
	}

	return RelicsOK, version
}

// EnsureRelics checks for rl and installs it if missing or outdated.
// Returns nil if rl is available and compatible.
// If autoInstall is true, will attempt to install rl when missing.
func EnsureRelics(autoInstall bool) error {
	status, version := CheckRelics()

	switch status {
	case RelicsOK:
		return nil

	case RelicsNotFound:
		if !autoInstall {
			return fmt.Errorf("relics (bd) not found in PATH\n\nInstall with: go install %s", RelicsInstallPath)
		}
		return installRelics()

	case RelicsTooOld:
		return fmt.Errorf("relics version %s is too old (minimum: %s)\n\nUpgrade with: go install %s",
			version, MinRelicsVersion, RelicsInstallPath)

	case RelicsUnknown:
		// Found rl but couldn't determine version - proceed with warning
		return nil
	}

	return nil
}

// installRelics runs go install to install the latest relics.
func installRelics() error {
	fmt.Printf("   relics (bd) not found. Installing...\n")

	cmd := exec.Command("go", "install", RelicsInstallPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install relics: %s\n%s", err, string(output))
	}

	// Verify installation
	status, version := CheckRelics()
	if status == RelicsNotFound {
		return fmt.Errorf("relics installed but not in PATH - ensure $GOPATH/bin is in your PATH")
	}
	if status == RelicsTooOld {
		return fmt.Errorf("installed relics %s but minimum required is %s", version, MinRelicsVersion)
	}

	fmt.Printf("   ✓ Installed relics %s\n", version)
	return nil
}

// parseRelicsVersion extracts version from "bd version X.Y.Z ..." output.
func parseRelicsVersion(output string) string {
	// Match patterns like "bd version 0.43.0" or "bd version 0.43.0 (dev: ...)"
	re := regexp.MustCompile(`rl version (\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// compareVersions compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	aParts := parseVersion(a)
	bParts := parseVersion(b)

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

// parseVersion parses "X.Y.Z" into [3]int.
func parseVersion(v string) [3]int {
	var parts [3]int
	split := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(split); i++ {
		parts[i], _ = strconv.Atoi(split[i])
	}
	return parts
}
