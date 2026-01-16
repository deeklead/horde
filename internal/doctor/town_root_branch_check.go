package doctor

import (
	"fmt"
	"os/exec"
	"strings"
)

// TownRootBranchCheck verifies that the encampment root directory is on the main branch.
// The encampment root should always stay on main to avoid confusion and broken hd commands.
// Accidental branch switches can happen when git commands run in the wrong directory.
type TownRootBranchCheck struct {
	FixableCheck
	currentBranch string // Cached during Run for use in Fix
}

// NewTownRootBranchCheck creates a new encampment root branch check.
func NewTownRootBranchCheck() *TownRootBranchCheck {
	return &TownRootBranchCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "encampment-root-branch",
				CheckDescription: "Verify encampment root is on main branch",
				CheckCategory:    CategoryCore,
			},
		},
	}
}

// Run checks if the encampment root is on the main branch.
func (c *TownRootBranchCheck) Run(ctx *CheckContext) *CheckResult {
	// Get current branch
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = ctx.TownRoot
	out, err := cmd.Output()
	if err != nil {
		// Not a git repo - skip this check (handled by encampment-git check)
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Encampment root is not a git repository (skipped)",
		}
	}

	branch := strings.TrimSpace(string(out))
	c.currentBranch = branch

	// Empty branch means detached HEAD
	if branch == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Encampment root is in detached HEAD state",
			Details: []string{
				"The encampment root should be on the main branch",
				"Detached HEAD can cause hd commands to fail",
			},
			FixHint: "Run 'hd doctor --fix' or manually: cd ~/horde && git checkout main",
		}
	}

	// Accept main or master
	if branch == "main" || branch == "master" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("Encampment root is on %s branch", branch),
		}
	}

	// On wrong branch - this is the problem we're trying to prevent
	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("Encampment root is on wrong branch: %s", branch),
		Details: []string{
			"The encampment root (~/horde) must stay on main branch",
			fmt.Sprintf("Currently on: %s", branch),
			"This can cause hd commands to fail (missing warbands.json, etc.)",
			"The branch switch was likely accidental (git command in wrong dir)",
		},
		FixHint: "Run 'hd doctor --fix' or manually: cd ~/horde && git checkout main",
	}
}

// Fix switches the encampment root back to main branch.
func (c *TownRootBranchCheck) Fix(ctx *CheckContext) error {
	// Only fix if we're not already on main
	if c.currentBranch == "main" || c.currentBranch == "master" {
		return nil
	}

	// Check for uncommitted changes that would block checkout
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = ctx.TownRoot
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("cannot switch to main: uncommitted changes in encampment root (stash or commit first)")
	}

	// Switch to main
	cmd = exec.Command("git", "checkout", "main")
	cmd.Dir = ctx.TownRoot
	if err := cmd.Run(); err != nil {
		// Try master if main doesn't exist
		cmd = exec.Command("git", "checkout", "master")
		cmd.Dir = ctx.TownRoot
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to checkout main: %w", err)
		}
	}

	return nil
}
