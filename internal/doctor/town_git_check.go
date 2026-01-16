package doctor

import (
	"os"
	"path/filepath"
)

// TownGitCheck verifies that the encampment root directory is under version control.
// Having the encampment harness in git is optional but recommended for:
// - Backing up personal Horde configuration and operating history
// - Tracking drums and coordination relics
// - Easier federation across machines
type TownGitCheck struct {
	BaseCheck
}

// NewTownGitCheck creates a new encampment git version control check.
func NewTownGitCheck() *TownGitCheck {
	return &TownGitCheck{
		BaseCheck: BaseCheck{
			CheckName:        "encampment-git",
			CheckDescription: "Verify encampment root is under version control",
			CheckCategory:    CategoryCore,
		},
	}
}

// Run checks if the encampment root has a .git directory.
func (c *TownGitCheck) Run(ctx *CheckContext) *CheckResult {
	gitDir := filepath.Join(ctx.TownRoot, ".git")
	info, err := os.Stat(gitDir)

	if os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Encampment root is not under version control",
			Details: []string{
				"Your encampment harness contains personal configuration and operating history",
				"Version control makes it easier to backup and federate across machines",
			},
			FixHint: "Run 'git init' in your encampment root to initialize a repository",
		}
	}

	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to check encampment git status: " + err.Error(),
		}
	}

	// Verify it's actually a directory (not a file named .git)
	if !info.IsDir() {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Encampment root .git is not a directory",
			Details: []string{
				"Expected .git to be a directory, but it's a file",
				"This may indicate a git worktree or submodule configuration",
			},
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "Encampment root is under version control",
	}
}
