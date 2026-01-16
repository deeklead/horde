package doctor

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/constants"
)

// RigIsGitRepoCheck verifies the warband has a valid warchief/warband git clone.
// Note: The warband directory itself is not a git repo - it contains clones.
type RigIsGitRepoCheck struct {
	BaseCheck
}

// NewRigIsGitRepoCheck creates a new warband git repo check.
func NewRigIsGitRepoCheck() *RigIsGitRepoCheck {
	return &RigIsGitRepoCheck{
		BaseCheck: BaseCheck{
			CheckName:        "warband-is-git-repo",
			CheckDescription: "Verify warband has a valid warchief/warband git clone",
			CheckCategory:    CategoryRig,
		},
	}
}

// Run checks if the warband has a valid warchief/warband git clone.
func (c *RigIsGitRepoCheck) Run(ctx *CheckContext) *CheckResult {
	rigPath := ctx.RigPath()
	if rigPath == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warband specified",
		}
	}

	// Check warchief/warband/ which is the authoritative clone for the warband
	warchiefRigPath := filepath.Join(rigPath, "warchief", "warband")
	gitPath := filepath.Join(warchiefRigPath, ".git")
	info, err := os.Stat(gitPath)
	if os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warchief/warband clone found",
			Details: []string{fmt.Sprintf("Missing: %s", gitPath)},
			FixHint: "Clone the repository to warchief/warband/",
		}
	}
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("Cannot access warchief/warband/.git: %v", err),
		}
	}

	// Verify git status works
	cmd := exec.Command("git", "-C", warchiefRigPath, "status", "--porcelain")
	if err := cmd.Run(); err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "git status failed on warchief/warband",
			Details: []string{fmt.Sprintf("Error: %v", err)},
			FixHint: "Check git configuration and repository integrity",
		}
	}

	gitType := "clone"
	if info.Mode().IsRegular() {
		gitType = "worktree"
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("Valid warchief/warband %s", gitType),
	}
}

// GitExcludeConfiguredCheck verifies .git/info/exclude has Horde directories.
type GitExcludeConfiguredCheck struct {
	FixableCheck
	missingEntries []string
	excludePath    string
}

// NewGitExcludeConfiguredCheck creates a new git exclude check.
func NewGitExcludeConfiguredCheck() *GitExcludeConfiguredCheck {
	return &GitExcludeConfiguredCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "git-exclude-configured",
				CheckDescription: "Check .git/info/exclude has Horde directories",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// requiredExcludes returns the directories that should be excluded.
func (c *GitExcludeConfiguredCheck) requiredExcludes() []string {
	return []string{"raiders/", "witness/", "forge/", "warchief/"}
}

// Run checks if .git/info/exclude contains required entries.
func (c *GitExcludeConfiguredCheck) Run(ctx *CheckContext) *CheckResult {
	rigPath := ctx.RigPath()
	if rigPath == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warband specified",
		}
	}

	// Check warchief/warband/ which is the authoritative clone
	warchiefRigPath := filepath.Join(rigPath, "warchief", "warband")
	gitDir := filepath.Join(warchiefRigPath, ".git")
	info, err := os.Stat(gitDir)
	if os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "No warchief/warband clone found",
			FixHint: "Run warband-is-git-repo check first",
		}
	}

	// If .git is a file (worktree), read the actual git dir
	if info.Mode().IsRegular() {
		content, err := os.ReadFile(gitDir)
		if err != nil {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusError,
				Message: fmt.Sprintf("Cannot read .git file: %v", err),
			}
		}
		// Format: "gitdir: /path/to/actual/git/dir"
		line := strings.TrimSpace(string(content))
		if strings.HasPrefix(line, "gitdir: ") {
			gitDir = strings.TrimPrefix(line, "gitdir: ")
			// Resolve relative paths
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(rigPath, gitDir)
			}
		}
	}

	c.excludePath = filepath.Join(gitDir, "info", "exclude")

	// Read existing excludes
	existing := make(map[string]bool)
	if file, err := os.Open(c.excludePath); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				existing[line] = true
			}
		}
		_ = file.Close() //nolint:gosec // G104: best-effort close
	}

	// Check for missing entries
	c.missingEntries = nil
	for _, required := range c.requiredExcludes() {
		if !existing[required] {
			c.missingEntries = append(c.missingEntries, required)
		}
	}

	if len(c.missingEntries) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Git exclude properly configured",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d Horde directories not excluded", len(c.missingEntries)),
		Details: []string{fmt.Sprintf("Missing: %s", strings.Join(c.missingEntries, ", "))},
		FixHint: "Run 'hd doctor --fix' to add missing entries",
	}
}

// Fix appends missing entries to .git/info/exclude.
func (c *GitExcludeConfiguredCheck) Fix(ctx *CheckContext) error {
	if len(c.missingEntries) == 0 {
		return nil
	}

	// Ensure info directory exists
	infoDir := filepath.Dir(c.excludePath)
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		return fmt.Errorf("failed to create info directory: %w", err)
	}

	// Append missing entries
	f, err := os.OpenFile(c.excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open exclude file: %w", err)
	}
	defer f.Close()

	// Add a header comment if file is empty or new
	info, _ := f.Stat()
	if info.Size() == 0 {
		if _, err := f.WriteString("# Horde directories\n"); err != nil {
			return err
		}
	} else {
		// Add newline before new entries
		if _, err := f.WriteString("\n# Horde directories\n"); err != nil {
			return err
		}
	}

	for _, entry := range c.missingEntries {
		if _, err := f.WriteString(entry + "\n"); err != nil {
			return err
		}
	}

	return nil
}

// HooksPathConfiguredCheck verifies all clones have core.hooksPath set to .githooks.
// This ensures the pre-push hook blocks pushes to invalid branches (no internal PRs).
type HooksPathConfiguredCheck struct {
	FixableCheck
	unconfiguredClones []string
}

// NewHooksPathConfiguredCheck creates a new hooks path check.
func NewHooksPathConfiguredCheck() *HooksPathConfiguredCheck {
	return &HooksPathConfiguredCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "hooks-path-configured",
				CheckDescription: "Check core.hooksPath is set for all clones",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run checks if all clones have core.hooksPath configured.
func (c *HooksPathConfiguredCheck) Run(ctx *CheckContext) *CheckResult {
	rigPath := ctx.RigPath()
	if rigPath == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warband specified",
		}
	}

	c.unconfiguredClones = nil

	// Check all clone locations
	clonePaths := []string{
		filepath.Join(rigPath, "warchief", "warband"),
		filepath.Join(rigPath, "forge", "warband"),
	}

	// Add clan clones
	crewDir := filepath.Join(rigPath, "clan")
	if entries, err := os.ReadDir(crewDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				clonePaths = append(clonePaths, filepath.Join(crewDir, entry.Name()))
			}
		}
	}

	// Add raider clones
	raiderDir := filepath.Join(rigPath, "raiders")
	if entries, err := os.ReadDir(raiderDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				clonePaths = append(clonePaths, filepath.Join(raiderDir, entry.Name()))
			}
		}
	}

	for _, clonePath := range clonePaths {
		// Skip if not a git repo
		if _, err := os.Stat(filepath.Join(clonePath, ".git")); os.IsNotExist(err) {
			continue
		}

		// Skip if no .githooks directory exists
		if _, err := os.Stat(filepath.Join(clonePath, ".githooks")); os.IsNotExist(err) {
			continue
		}

		// Check core.hooksPath
		cmd := exec.Command("git", "-C", clonePath, "config", "--get", "core.hooksPath")
		output, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(output)) != ".githooks" {
			// Get relative path for cleaner output
			relPath, _ := filepath.Rel(rigPath, clonePath)
			if relPath == "" {
				relPath = clonePath
			}
			c.unconfiguredClones = append(c.unconfiguredClones, clonePath)
		}
	}

	if len(c.unconfiguredClones) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All clones have hooks configured",
		}
	}

	// Build details with relative paths
	var details []string
	for _, clonePath := range c.unconfiguredClones {
		relPath, _ := filepath.Rel(rigPath, clonePath)
		if relPath == "" {
			relPath = clonePath
		}
		details = append(details, relPath)
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d clone(s) missing hooks configuration", len(c.unconfiguredClones)),
		Details: details,
		FixHint: "Run 'hd doctor --fix' to configure hooks",
	}
}

// Fix configures core.hooksPath for all unconfigured clones.
func (c *HooksPathConfiguredCheck) Fix(ctx *CheckContext) error {
	for _, clonePath := range c.unconfiguredClones {
		cmd := exec.Command("git", "-C", clonePath, "config", "core.hooksPath", ".githooks")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to configure hooks for %s: %w", clonePath, err)
		}
	}
	return nil
}

// WitnessExistsCheck verifies the witness directory structure exists.
type WitnessExistsCheck struct {
	FixableCheck
	rigPath     string
	needsCreate bool
	needsClone  bool
	needsMail   bool
}

// NewWitnessExistsCheck creates a new witness exists check.
func NewWitnessExistsCheck() *WitnessExistsCheck {
	return &WitnessExistsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "witness-exists",
				CheckDescription: "Verify witness/ directory structure exists",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run checks if the witness directory structure exists.
func (c *WitnessExistsCheck) Run(ctx *CheckContext) *CheckResult {
	c.rigPath = ctx.RigPath()
	if c.rigPath == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warband specified",
		}
	}

	witnessDir := filepath.Join(c.rigPath, "witness")
	rigClone := filepath.Join(witnessDir, "warband")
	mailInbox := filepath.Join(witnessDir, "drums", "inbox.jsonl")

	var issues []string
	c.needsCreate = false
	c.needsClone = false
	c.needsMail = false

	// Check witness/ directory
	if _, err := os.Stat(witnessDir); os.IsNotExist(err) {
		issues = append(issues, "Missing: witness/")
		c.needsCreate = true
	} else {
		// Check witness/warband/ clone
		rigGit := filepath.Join(rigClone, ".git")
		if _, err := os.Stat(rigGit); os.IsNotExist(err) {
			issues = append(issues, "Missing: witness/warband/ (git clone)")
			c.needsClone = true
		}

		// Check witness/drums/inbox.jsonl
		if _, err := os.Stat(mailInbox); os.IsNotExist(err) {
			issues = append(issues, "Missing: witness/drums/inbox.jsonl")
			c.needsMail = true
		}
	}

	if len(issues) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Witness structure exists",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "Witness structure incomplete",
		Details: issues,
		FixHint: "Run 'hd doctor --fix' to create missing structure",
	}
}

// Fix creates missing witness structure.
func (c *WitnessExistsCheck) Fix(ctx *CheckContext) error {
	witnessDir := filepath.Join(c.rigPath, "witness")

	if c.needsCreate {
		if err := os.MkdirAll(witnessDir, 0755); err != nil {
			return fmt.Errorf("failed to create witness/: %w", err)
		}
	}

	if c.needsMail {
		mailDir := filepath.Join(witnessDir, "drums")
		if err := os.MkdirAll(mailDir, 0755); err != nil {
			return fmt.Errorf("failed to create witness/drums/: %w", err)
		}
		inboxPath := filepath.Join(mailDir, "inbox.jsonl")
		if err := os.WriteFile(inboxPath, []byte{}, 0644); err != nil {
			return fmt.Errorf("failed to create inbox.jsonl: %w", err)
		}
	}

	// Note: Cannot auto-fix clone without knowing the repo URL
	if c.needsClone {
		return fmt.Errorf("cannot auto-create witness/warband/ clone (requires repo URL)")
	}

	return nil
}

// ForgeExistsCheck verifies the forge directory structure exists.
type ForgeExistsCheck struct {
	FixableCheck
	rigPath     string
	needsCreate bool
	needsClone  bool
	needsMail   bool
}

// NewForgeExistsCheck creates a new forge exists check.
func NewForgeExistsCheck() *ForgeExistsCheck {
	return &ForgeExistsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "forge-exists",
				CheckDescription: "Verify forge/ directory structure exists",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run checks if the forge directory structure exists.
func (c *ForgeExistsCheck) Run(ctx *CheckContext) *CheckResult {
	c.rigPath = ctx.RigPath()
	if c.rigPath == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warband specified",
		}
	}

	forgeDir := filepath.Join(c.rigPath, "forge")
	rigClone := filepath.Join(forgeDir, "warband")
	mailInbox := filepath.Join(forgeDir, "drums", "inbox.jsonl")

	var issues []string
	c.needsCreate = false
	c.needsClone = false
	c.needsMail = false

	// Check forge/ directory
	if _, err := os.Stat(forgeDir); os.IsNotExist(err) {
		issues = append(issues, "Missing: forge/")
		c.needsCreate = true
	} else {
		// Check forge/warband/ clone
		rigGit := filepath.Join(rigClone, ".git")
		if _, err := os.Stat(rigGit); os.IsNotExist(err) {
			issues = append(issues, "Missing: forge/warband/ (git clone)")
			c.needsClone = true
		}

		// Check forge/drums/inbox.jsonl
		if _, err := os.Stat(mailInbox); os.IsNotExist(err) {
			issues = append(issues, "Missing: forge/drums/inbox.jsonl")
			c.needsMail = true
		}
	}

	if len(issues) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Forge structure exists",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "Forge structure incomplete",
		Details: issues,
		FixHint: "Run 'hd doctor --fix' to create missing structure",
	}
}

// Fix creates missing forge structure.
func (c *ForgeExistsCheck) Fix(ctx *CheckContext) error {
	forgeDir := filepath.Join(c.rigPath, "forge")

	if c.needsCreate {
		if err := os.MkdirAll(forgeDir, 0755); err != nil {
			return fmt.Errorf("failed to create forge/: %w", err)
		}
	}

	if c.needsMail {
		mailDir := filepath.Join(forgeDir, "drums")
		if err := os.MkdirAll(mailDir, 0755); err != nil {
			return fmt.Errorf("failed to create forge/drums/: %w", err)
		}
		inboxPath := filepath.Join(mailDir, "inbox.jsonl")
		if err := os.WriteFile(inboxPath, []byte{}, 0644); err != nil {
			return fmt.Errorf("failed to create inbox.jsonl: %w", err)
		}
	}

	// Note: Cannot auto-fix clone without knowing the repo URL
	if c.needsClone {
		return fmt.Errorf("cannot auto-create forge/warband/ clone (requires repo URL)")
	}

	return nil
}

// WarchiefCloneExistsCheck verifies the warchief/warband clone exists.
type WarchiefCloneExistsCheck struct {
	FixableCheck
	rigPath     string
	needsCreate bool
	needsClone  bool
}

// NewWarchiefCloneExistsCheck creates a new warchief clone check.
func NewWarchiefCloneExistsCheck() *WarchiefCloneExistsCheck {
	return &WarchiefCloneExistsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "warchief-clone-exists",
				CheckDescription: "Verify warchief/warband/ git clone exists",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run checks if the warchief/warband clone exists.
func (c *WarchiefCloneExistsCheck) Run(ctx *CheckContext) *CheckResult {
	c.rigPath = ctx.RigPath()
	if c.rigPath == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warband specified",
		}
	}

	warchiefDir := filepath.Join(c.rigPath, "warchief")
	rigClone := filepath.Join(warchiefDir, "warband")

	var issues []string
	c.needsCreate = false
	c.needsClone = false

	// Check warchief/ directory
	if _, err := os.Stat(warchiefDir); os.IsNotExist(err) {
		issues = append(issues, "Missing: warchief/")
		c.needsCreate = true
	} else {
		// Check warchief/warband/ clone
		rigGit := filepath.Join(rigClone, ".git")
		if _, err := os.Stat(rigGit); os.IsNotExist(err) {
			issues = append(issues, "Missing: warchief/warband/ (git clone)")
			c.needsClone = true
		}
	}

	if len(issues) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Warchief clone exists",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "Warchief structure incomplete",
		Details: issues,
		FixHint: "Run 'hd doctor --fix' to create structure (clone requires repo URL)",
	}
}

// Fix creates missing warchief structure.
func (c *WarchiefCloneExistsCheck) Fix(ctx *CheckContext) error {
	warchiefDir := filepath.Join(c.rigPath, "warchief")

	if c.needsCreate {
		if err := os.MkdirAll(warchiefDir, 0755); err != nil {
			return fmt.Errorf("failed to create warchief/: %w", err)
		}
	}

	// Note: Cannot auto-fix clone without knowing the repo URL
	if c.needsClone {
		return fmt.Errorf("cannot auto-create warchief/warband/ clone (requires repo URL)")
	}

	return nil
}

// RaiderClonesValidCheck verifies each raider directory is a valid clone.
type RaiderClonesValidCheck struct {
	BaseCheck
}

// NewRaiderClonesValidCheck creates a new raider clones check.
func NewRaiderClonesValidCheck() *RaiderClonesValidCheck {
	return &RaiderClonesValidCheck{
		BaseCheck: BaseCheck{
			CheckName:        "raider-clones-valid",
			CheckDescription: "Verify raider directories are valid git clones",
			CheckCategory:    CategoryRig,
		},
	}
}

// Run checks if each raider directory is a valid git clone.
func (c *RaiderClonesValidCheck) Run(ctx *CheckContext) *CheckResult {
	rigPath := ctx.RigPath()
	if rigPath == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warband specified",
		}
	}

	raidersDir := filepath.Join(rigPath, "raiders")
	entries, err := os.ReadDir(raidersDir)
	if os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No raiders/ directory (none deployed)",
		}
	}
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("Cannot read raiders/: %v", err),
		}
	}

	var issues []string
	var warnings []string
	validCount := 0

	// Get warband name for new structure path detection
	rigName := ctx.RigName

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		raiderName := entry.Name()

		// Determine worktree path (handle both new and old structures)
		// New structure: raiders/<name>/<rigname>/
		// Old structure: raiders/<name>/
		raiderPath := filepath.Join(raidersDir, raiderName, rigName)
		if _, err := os.Stat(raiderPath); os.IsNotExist(err) {
			raiderPath = filepath.Join(raidersDir, raiderName)
		}

		// Check if it's a git clone
		gitPath := filepath.Join(raiderPath, ".git")
		if _, err := os.Stat(gitPath); os.IsNotExist(err) {
			issues = append(issues, fmt.Sprintf("%s: not a git clone", raiderName))
			continue
		}

		// Verify git status works and check for uncommitted changes
		cmd := exec.Command("git", "-C", raiderPath, "status", "--porcelain")
		output, err := cmd.Output()
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: git status failed", raiderName))
			continue
		}

		if len(output) > 0 {
			warnings = append(warnings, fmt.Sprintf("%s: has uncommitted changes", raiderName))
		}

		// Check if on a raider branch
		cmd = exec.Command("git", "-C", raiderPath, "branch", "--show-current")
		branchOutput, err := cmd.Output()
		if err == nil {
			branch := strings.TrimSpace(string(branchOutput))
			if !strings.HasPrefix(branch, constants.BranchRaiderPrefix) {
				warnings = append(warnings, fmt.Sprintf("%s: on branch '%s' (expected %s*)", raiderName, branch, constants.BranchRaiderPrefix))
			}
		}

		validCount++
	}

	if len(issues) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("%d raider(s) invalid", len(issues)),
			Details: append(issues, warnings...),
			FixHint: "Cannot auto-fix (data loss risk)",
		}
	}

	if len(warnings) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d raider(s) valid, %d warning(s)", validCount, len(warnings)),
			Details: warnings,
		}
	}

	if validCount == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No raiders deployed",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("%d raider(s) valid", validCount),
	}
}

// RelicsConfigValidCheck verifies relics configuration if .relics/ exists.
type RelicsConfigValidCheck struct {
	FixableCheck
	rigPath   string
	needsSync bool
}

// NewRelicsConfigValidCheck creates a new relics config check.
func NewRelicsConfigValidCheck() *RelicsConfigValidCheck {
	return &RelicsConfigValidCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "relics-config-valid",
				CheckDescription: "Verify relics configuration if .relics/ exists",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run checks if relics is properly configured.
func (c *RelicsConfigValidCheck) Run(ctx *CheckContext) *CheckResult {
	c.rigPath = ctx.RigPath()
	if c.rigPath == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "No warband specified",
		}
	}

	relicsDir := filepath.Join(c.rigPath, ".relics")
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No .relics/ directory (relics not configured)",
		}
	}

	// Check if rl command works
	cmd := exec.Command("rl", "stats", "--json")
	cmd.Dir = c.rigPath
	if err := cmd.Run(); err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "bd command failed",
			Details: []string{fmt.Sprintf("Error: %v", err)},
			FixHint: "Check relics installation and .relics/ configuration",
		}
	}

	// Check sync status
	cmd = exec.Command("rl", "sync", "--status")
	cmd.Dir = c.rigPath
	output, err := cmd.CombinedOutput()
	c.needsSync = false
	if err != nil {
		// sync --status may exit non-zero if out of sync
		outputStr := string(output)
		if strings.Contains(outputStr, "out of sync") || strings.Contains(outputStr, "behind") {
			c.needsSync = true
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusWarning,
				Message: "Relics out of sync",
				Details: []string{strings.TrimSpace(outputStr)},
				FixHint: "Run 'hd doctor --fix' or 'bd sync' to synchronize",
			}
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "Relics configured and in sync",
	}
}

// Fix runs rl sync if needed.
func (c *RelicsConfigValidCheck) Fix(ctx *CheckContext) error {
	if !c.needsSync {
		return nil
	}

	cmd := exec.Command("rl", "sync")
	cmd.Dir = c.rigPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd sync failed: %s", string(output))
	}

	return nil
}

// RelicsRedirectCheck verifies that warband-level relics redirect exists for tracked relics.
// When a repo has .relics/ tracked in git (at warchief/warband/.relics), the warband root needs
// a redirect file pointing to that location.
type RelicsRedirectCheck struct {
	FixableCheck
}

// NewRelicsRedirectCheck creates a new relics redirect check.
func NewRelicsRedirectCheck() *RelicsRedirectCheck {
	return &RelicsRedirectCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "relics-redirect",
				CheckDescription: "Verify warband-level relics redirect for tracked relics",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run checks if the warband-level relics redirect exists when needed.
func (c *RelicsRedirectCheck) Run(ctx *CheckContext) *CheckResult {
	// Only applies when checking a specific warband
	if ctx.RigName == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warband specified (skipping redirect check)",
		}
	}

	rigPath := ctx.RigPath()
	warchiefRigRelics := filepath.Join(rigPath, "warchief", "warband", ".relics")
	rigRelicsDir := filepath.Join(rigPath, ".relics")
	redirectPath := filepath.Join(rigRelicsDir, "redirect")

	// Check if this warband has tracked relics (warchief/warband/.relics exists)
	if _, err := os.Stat(warchiefRigRelics); os.IsNotExist(err) {
		// No tracked relics - check if warband/.relics exists (local relics)
		if _, err := os.Stat(rigRelicsDir); os.IsNotExist(err) {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusError,
				Message: "No .relics directory found at warband root",
				Details: []string{
					"Relics database not initialized for this warband",
					"This prevents issue tracking for this warband",
				},
				FixHint: "Run 'hd doctor --fix --warband " + ctx.RigName + "' to initialize relics",
			}
		}
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Warband uses local relics (no redirect needed)",
		}
	}

	// Tracked relics exist - check for conflicting local relics
	hasLocalData := hasRelicsData(rigRelicsDir)
	redirectExists := false
	if _, err := os.Stat(redirectPath); err == nil {
		redirectExists = true
	}

	// Case: Local relics directory has actual data (not just redirect)
	if hasLocalData && !redirectExists {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Conflicting local relics found with tracked relics",
			Details: []string{
				"Tracked relics exist at: warchief/warband/.relics",
				"Local relics with data exist at: .relics/",
				"Fix will remove local relics and create redirect to tracked relics",
			},
			FixHint: "Run 'hd doctor --fix --warband " + ctx.RigName + "' to fix",
		}
	}

	// Case: No redirect file (but no conflicting data)
	if !redirectExists {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Missing warband-level relics redirect for tracked relics",
			Details: []string{
				"Tracked relics exist at: warchief/warband/.relics",
				"Missing redirect at: .relics/redirect",
				"Without this redirect, rl commands from warband root won't find relics",
			},
			FixHint: "Run 'hd doctor --fix' to create the redirect",
		}
	}

	// Verify redirect points to correct location
	content, err := os.ReadFile(redirectPath)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not read redirect file: %v", err),
		}
	}

	target := strings.TrimSpace(string(content))
	if target != "warchief/warband/.relics" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("Redirect points to %q, expected warchief/warband/.relics", target),
			FixHint: "Run 'hd doctor --fix --warband " + ctx.RigName + "' to correct the redirect",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "Warband-level relics redirect is correctly configured",
	}
}

// Fix creates or corrects the warband-level relics redirect, or initializes relics if missing.
func (c *RelicsRedirectCheck) Fix(ctx *CheckContext) error {
	if ctx.RigName == "" {
		return nil
	}

	rigPath := ctx.RigPath()
	warchiefRigRelics := filepath.Join(rigPath, "warchief", "warband", ".relics")
	rigRelicsDir := filepath.Join(rigPath, ".relics")
	redirectPath := filepath.Join(rigRelicsDir, "redirect")

	// Check if tracked relics exist
	hasTrackedRelics := true
	if _, err := os.Stat(warchiefRigRelics); os.IsNotExist(err) {
		hasTrackedRelics = false
	}

	// Check if local relics exist
	hasLocalRelics := true
	if _, err := os.Stat(rigRelicsDir); os.IsNotExist(err) {
		hasLocalRelics = false
	}

	// Case 1: No relics at all - initialize with rl init
	if !hasTrackedRelics && !hasLocalRelics {
		// Get the warband's relics prefix from warbands.json (falls back to "hd" if not found)
		prefix := config.GetRigPrefix(ctx.TownRoot, ctx.RigName)

		// Create .relics directory
		if err := os.MkdirAll(rigRelicsDir, 0755); err != nil {
			return fmt.Errorf("creating .relics directory: %w", err)
		}

		// Run rl init with the configured prefix
		cmd := exec.Command("rl", "init", "--prefix", prefix)
		cmd.Dir = rigPath
		if output, err := cmd.CombinedOutput(); err != nil {
			// rl might not be installed - create minimal config.yaml
			configPath := filepath.Join(rigRelicsDir, "config.yaml")
			configContent := fmt.Sprintf("prefix: %s\n", prefix)
			if writeErr := os.WriteFile(configPath, []byte(configContent), 0644); writeErr != nil {
				return fmt.Errorf("bd init failed (%v) and fallback config creation failed: %w", err, writeErr)
			}
			// Continue - minimal config created
		} else {
			_ = output // rl init succeeded
			// Configure custom types for Horde (relics v0.46.0+)
			configCmd := exec.Command("rl", "config", "set", "types.custom", constants.RelicsCustomTypes)
			configCmd.Dir = rigPath
			_, _ = configCmd.CombinedOutput() // Ignore errors - older relics don't need this
		}
		return nil
	}

	// Case 2: Tracked relics exist - create redirect (may need to remove conflicting local relics)
	if hasTrackedRelics {
		// Check if local relics have conflicting data
		if hasLocalRelics && hasRelicsData(rigRelicsDir) {
			// Remove conflicting local relics directory
			if err := os.RemoveAll(rigRelicsDir); err != nil {
				return fmt.Errorf("removing conflicting local relics: %w", err)
			}
		}

		// Create .relics directory if needed
		if err := os.MkdirAll(rigRelicsDir, 0755); err != nil {
			return fmt.Errorf("creating .relics directory: %w", err)
		}

		// Write redirect file
		if err := os.WriteFile(redirectPath, []byte("warchief/warband/.relics\n"), 0644); err != nil {
			return fmt.Errorf("writing redirect file: %w", err)
		}
	}

	return nil
}

// hasRelicsData checks if a relics directory has actual data (issues.jsonl, issues.db, config.yaml)
// as opposed to just being a redirect-only directory.
func hasRelicsData(relicsDir string) bool {
	// Check for actual relics data files
	dataFiles := []string{"issues.jsonl", "issues.db", "config.yaml"}
	for _, f := range dataFiles {
		if _, err := os.Stat(filepath.Join(relicsDir, f)); err == nil {
			return true
		}
	}
	return false
}

// BareRepoRefspecCheck verifies that the shared bare repo has the correct refspec configured.
// Without this, worktrees created from the bare repo cannot fetch and see origin/* refs.
// See: https://github.com/anthropics/horde/issues/286
type BareRepoRefspecCheck struct {
	FixableCheck
}

// NewBareRepoRefspecCheck creates a new bare repo refspec check.
func NewBareRepoRefspecCheck() *BareRepoRefspecCheck {
	return &BareRepoRefspecCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "bare-repo-refspec",
				CheckDescription: "Verify bare repo has correct refspec for worktrees",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run checks if the bare repo has the correct remote.origin.fetch refspec.
func (c *BareRepoRefspecCheck) Run(ctx *CheckContext) *CheckResult {
	if ctx.RigName == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warband specified, skipping bare repo check",
		}
	}

	bareRepoPath := filepath.Join(ctx.RigPath(), ".repo.git")
	if _, err := os.Stat(bareRepoPath); os.IsNotExist(err) {
		// No bare repo - might be using a different architecture
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No shared bare repo found (using individual clones)",
		}
	}

	// Check the refspec
	cmd := exec.Command("git", "-C", bareRepoPath, "config", "--get", "remote.origin.fetch")
	out, err := cmd.Output()
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Bare repo missing remote.origin.fetch refspec",
			Details: []string{
				"Worktrees cannot fetch or see origin/* refs without this config",
				"This breaks forge merge operations and causes stale origin/main",
			},
			FixHint: "Run 'hd doctor --fix' to configure the refspec",
		}
	}

	refspec := strings.TrimSpace(string(out))
	expectedRefspec := "+refs/heads/*:refs/remotes/origin/*"
	if refspec != expectedRefspec {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Bare repo has non-standard refspec",
			Details: []string{
				fmt.Sprintf("Current: %s", refspec),
				fmt.Sprintf("Expected: %s", expectedRefspec),
			},
			FixHint: "Run 'hd doctor --fix' to update the refspec",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "Bare repo refspec configured correctly",
	}
}

// Fix sets the correct refspec on the bare repo.
func (c *BareRepoRefspecCheck) Fix(ctx *CheckContext) error {
	if ctx.RigName == "" {
		return nil
	}

	bareRepoPath := filepath.Join(ctx.RigPath(), ".repo.git")
	if _, err := os.Stat(bareRepoPath); os.IsNotExist(err) {
		return nil // No bare repo to fix
	}

	cmd := exec.Command("git", "-C", bareRepoPath, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setting refspec: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// RigChecks returns all warband-level health checks.
func RigChecks() []Check {
	return []Check{
		NewRigIsGitRepoCheck(),
		NewGitExcludeConfiguredCheck(),
		NewHooksPathConfiguredCheck(),
		NewSparseCheckoutCheck(),
		NewBareRepoRefspecCheck(),
		NewWitnessExistsCheck(),
		NewForgeExistsCheck(),
		NewWarchiefCloneExistsCheck(),
		NewRaiderClonesValidCheck(),
		NewRelicsConfigValidCheck(),
		NewRelicsRedirectCheck(),
	}
}
