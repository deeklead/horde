package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/daemon"
)

// bdDoctorResult represents the JSON output from rl doctor --json.
type bdDoctorResult struct {
	Checks []bdDoctorCheck `json:"checks"`
}

// bdDoctorCheck represents a single check result from rl doctor.
type bdDoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	Fix     string `json:"fix,omitempty"`
}

// RepoFingerprintCheck verifies that relics databases have valid repository fingerprints.
// A missing or mismatched fingerprint can cause daemon startup failures and sync issues.
type RepoFingerprintCheck struct {
	FixableCheck
	needsMigration bool   // Cached during Run for use in Fix
	relicsDir       string // Relics directory that needs migration
}

// NewRepoFingerprintCheck creates a new repo fingerprint check.
func NewRepoFingerprintCheck() *RepoFingerprintCheck {
	return &RepoFingerprintCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "repo-fingerprint",
				CheckDescription: "Verify relics database has valid repository fingerprint",
				CheckCategory:    CategoryInfrastructure,
			},
		},
	}
}

// Run checks if relics databases have valid repo fingerprints.
func (c *RepoFingerprintCheck) Run(ctx *CheckContext) *CheckResult {
	// Reset cached state
	c.needsMigration = false
	c.relicsDir = ""

	// Check encampment-level relics
	townRelicsDir := filepath.Join(ctx.TownRoot, ".relics")
	if _, err := os.Stat(townRelicsDir); err == nil {
		result := c.checkRelicsDir(filepath.Dir(townRelicsDir), "encampment")
		if result.Status != StatusOK {
			return result
		}
	}

	// Check warband-level relics if specified
	if ctx.RigName != "" {
		rigRelicsDir := relics.ResolveRelicsDir(ctx.RigPath())
		if _, err := os.Stat(rigRelicsDir); err == nil {
			result := c.checkRelicsDir(filepath.Dir(rigRelicsDir), "warband "+ctx.RigName)
			if result.Status != StatusOK {
				return result
			}
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "Repository fingerprints verified",
	}
}

// checkRelicsDir checks a single relics directory for repo fingerprint using rl doctor.
func (c *RepoFingerprintCheck) checkRelicsDir(workDir, location string) *CheckResult {
	// Run rl doctor --json to get fingerprint status
	cmd := exec.Command("rl", "doctor", "--json")
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// rl doctor exits with non-zero if there are warnings, so ignore exit code
	_ = cmd.Run()

	// Parse JSON output
	var result bdDoctorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		// If we can't parse rl doctor output, skip this check
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("Skipped %s (bd doctor unavailable)", location),
		}
	}

	// Find the Repo Fingerprint check
	for _, check := range result.Checks {
		if check.Name == "Repo Fingerprint" {
			switch check.Status {
			case "ok":
				return &CheckResult{
					Name:    c.Name(),
					Status:  StatusOK,
					Message: fmt.Sprintf("Fingerprint verified in %s (%s)", location, check.Message),
				}
			case "warning":
				c.needsMigration = true
				c.relicsDir = filepath.Join(workDir, ".relics")
				return &CheckResult{
					Name:    c.Name(),
					Status:  StatusWarning,
					Message: fmt.Sprintf("Fingerprint issue in %s: %s", location, check.Message),
					Details: func() []string {
						if check.Detail != "" {
							return []string{check.Detail}
						}
						return nil
					}(),
					FixHint: "Run 'hd doctor --fix' or 'bd migrate --update-repo-id'",
				}
			case "error":
				c.needsMigration = true
				c.relicsDir = filepath.Join(workDir, ".relics")
				return &CheckResult{
					Name:    c.Name(),
					Status:  StatusError,
					Message: fmt.Sprintf("Fingerprint error in %s: %s", location, check.Message),
					Details: func() []string {
						if check.Detail != "" {
							return []string{check.Detail}
						}
						return nil
					}(),
					FixHint: "Run 'hd doctor --fix' or 'bd migrate --update-repo-id'",
				}
			}
		}
	}

	// Fingerprint check not found in rl doctor output - skip
	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("Fingerprint check not applicable for %s", location),
	}
}

// Fix runs rl migrate --update-repo-id and restarts the daemon.
func (c *RepoFingerprintCheck) Fix(ctx *CheckContext) error {
	if !c.needsMigration || c.relicsDir == "" {
		return nil
	}

	// Run rl migrate --update-repo-id
	cmd := exec.Command("rl", "migrate", "--update-repo-id")
	cmd.Dir = filepath.Dir(c.relicsDir) // Parent of .relics directory
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bd migrate --update-repo-id failed: %v: %s", err, stderr.String())
	}

	// Restart daemon if running
	running, _, err := daemon.IsRunning(ctx.TownRoot)
	if err == nil && running {
		// Stop daemon
		stopCmd := exec.Command("hd", "daemon", "stop")
		stopCmd.Dir = ctx.TownRoot
		_ = stopCmd.Run() // Ignore errors

		// Wait a moment
		time.Sleep(500 * time.Millisecond)

		// Start daemon
		startCmd := exec.Command("hd", "daemon", "run")
		startCmd.Dir = ctx.TownRoot
		startCmd.Stdin = nil
		startCmd.Stdout = nil
		startCmd.Stderr = nil
		if err := startCmd.Start(); err != nil {
			return fmt.Errorf("failed to restart daemon: %w", err)
		}

		// Wait for daemon to initialize
		time.Sleep(300 * time.Millisecond)
	}

	return nil
}
