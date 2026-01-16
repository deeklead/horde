package doctor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/constants"
)

// PrimingCheck verifies the priming subsystem is correctly configured.
// This ensures agents receive proper context on startup via the hd rally chain.
type PrimingCheck struct {
	FixableCheck
	issues []primingIssue
}

type primingIssue struct {
	location    string // e.g., "warchief", "horde/clan/max", "horde/witness"
	issueType   string // e.g., "no_hook", "no_prime", "large_claude_md", "missing_rally_md"
	description string
	fixable     bool
}

// NewPrimingCheck creates a new priming subsystem check.
func NewPrimingCheck() *PrimingCheck {
	return &PrimingCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "priming",
				CheckDescription: "Verify priming subsystem is correctly configured",
			},
		},
	}
}

// Run checks the priming configuration across all agent locations.
func (c *PrimingCheck) Run(ctx *CheckContext) *CheckResult {
	c.issues = nil

	var details []string

	// Check 1: hd binary in PATH
	if err := exec.Command("which", "hd").Run(); err != nil {
		c.issues = append(c.issues, primingIssue{
			location:    "system",
			issueType:   "gt_not_in_path",
			description: "hd binary not found in PATH",
			fixable:     false,
		})
		details = append(details, "hd binary not found in PATH")
	}

	// Check 2: Warchief priming (encampment-level)
	warchiefIssues := c.checkAgentPriming(ctx.TownRoot, "warchief", "warchief")
	for _, issue := range warchiefIssues {
		details = append(details, fmt.Sprintf("%s: %s", issue.location, issue.description))
	}
	c.issues = append(c.issues, warchiefIssues...)

	// Check 3: Shaman priming
	shamanPath := filepath.Join(ctx.TownRoot, "shaman")
	if dirExists(shamanPath) {
		shamanIssues := c.checkAgentPriming(ctx.TownRoot, "shaman", "shaman")
		for _, issue := range shamanIssues {
			details = append(details, fmt.Sprintf("%s: %s", issue.location, issue.description))
		}
		c.issues = append(c.issues, shamanIssues...)
	}

	// Check 4: Warband-level agents (witness, forge, clan, raiders)
	rigIssues := c.checkRigPriming(ctx.TownRoot)
	for _, issue := range rigIssues {
		details = append(details, fmt.Sprintf("%s: %s", issue.location, issue.description))
	}
	c.issues = append(c.issues, rigIssues...)

	if len(c.issues) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Priming subsystem is correctly configured",
		}
	}

	// Count fixable issues
	fixableCount := 0
	for _, issue := range c.issues {
		if issue.fixable {
			fixableCount++
		}
	}

	fixHint := ""
	if fixableCount > 0 {
		fixHint = fmt.Sprintf("Run 'hd doctor --fix' to fix %d issue(s)", fixableCount)
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("Found %d priming issue(s)", len(c.issues)),
		Details: details,
		FixHint: fixHint,
	}
}

// checkAgentPriming checks priming configuration for a specific agent.
func (c *PrimingCheck) checkAgentPriming(townRoot, agentDir, _ string) []primingIssue {
	var issues []primingIssue

	agentPath := filepath.Join(townRoot, agentDir)
	settingsPath := filepath.Join(agentPath, ".claude", "settings.json")

	// Check for SessionStart hook with hd rally
	if fileExists(settingsPath) {
		data, err := os.ReadFile(settingsPath)
		if err == nil {
			var settings map[string]any
			if err := json.Unmarshal(data, &settings); err == nil {
				if !c.hasGtPrimeHook(settings) {
					issues = append(issues, primingIssue{
						location:    agentDir,
						issueType:   "no_rally_hook",
						description: "SessionStart hook missing 'hd rally'",
						fixable:     false, // Requires template regeneration
					})
				}
			}
		}
	}

	// Check CLAUDE.md is minimal (bootstrap pointer, not full context)
	claudeMdPath := filepath.Join(agentPath, "CLAUDE.md")
	if fileExists(claudeMdPath) {
		lines := c.countLines(claudeMdPath)
		if lines > 30 {
			issues = append(issues, primingIssue{
				location:    agentDir,
				issueType:   "large_claude_md",
				description: fmt.Sprintf("CLAUDE.md has %d lines (should be <30 for bootstrap pointer)", lines),
				fixable:     false, // Requires manual review
			})
		}
	}

	// Check AGENTS.md is minimal (bootstrap pointer, not full context)
	agentsMdPath := filepath.Join(agentPath, "AGENTS.md")
	if fileExists(agentsMdPath) {
		lines := c.countLines(agentsMdPath)
		if lines > 20 {
			issues = append(issues, primingIssue{
				location:    agentDir,
				issueType:   "large_agents_md",
				description: fmt.Sprintf("AGENTS.md has %d lines (should be <20 for bootstrap pointer)", lines),
				fixable:     false, // Full context should come from hd rally templates
			})
		}
	}

	return issues
}

// checkRigPriming checks priming for all warbands.
func (c *PrimingCheck) checkRigPriming(townRoot string) []primingIssue {
	var issues []primingIssue

	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return issues
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		rigName := entry.Name()
		rigPath := filepath.Join(townRoot, rigName)

		// Skip non-warband directories
		if rigName == "warchief" || rigName == "shaman" || rigName == "daemon" ||
			rigName == "docs" || rigName[0] == '.' {
			continue
		}

		// Check if this is actually a warband (has .relics directory)
		if !dirExists(filepath.Join(rigPath, ".relics")) {
			continue
		}

		// Check RALLY.md exists at warband level
		primeMdPath := filepath.Join(rigPath, ".relics", "RALLY.md")
		if !fileExists(primeMdPath) {
			issues = append(issues, primingIssue{
				location:    rigName,
				issueType:   "missing_rally_md",
				description: "Missing .relics/RALLY.md (Horde context fallback)",
				fixable:     true,
			})
		}

		// Check AGENTS.md is minimal at warband level (bootstrap pointer, not full context)
		agentsMdPath := filepath.Join(rigPath, "AGENTS.md")
		if fileExists(agentsMdPath) {
			lines := c.countLines(agentsMdPath)
			if lines > 20 {
				issues = append(issues, primingIssue{
					location:    rigName,
					issueType:   "large_agents_md",
					description: fmt.Sprintf("AGENTS.md has %d lines (should be <20 for bootstrap pointer)", lines),
					fixable:     false, // Requires manual review
				})
			}
		}

		// Check witness priming
		witnessPath := filepath.Join(rigPath, "witness")
		if dirExists(witnessPath) {
			witnessIssues := c.checkAgentPriming(townRoot, filepath.Join(rigName, "witness"), "witness")
			issues = append(issues, witnessIssues...)
		}

		// Check forge priming
		forgePath := filepath.Join(rigPath, "forge")
		if dirExists(forgePath) {
			forgeIssues := c.checkAgentPriming(townRoot, filepath.Join(rigName, "forge"), "forge")
			issues = append(issues, forgeIssues...)
		}

		// Check clan RALLY.md (shared settings, individual worktrees)
		crewDir := filepath.Join(rigPath, "clan")
		if dirExists(crewDir) {
			crewEntries, _ := os.ReadDir(crewDir)
			for _, crewEntry := range crewEntries {
				if !crewEntry.IsDir() || crewEntry.Name() == ".claude" {
					continue
				}
				crewPath := filepath.Join(crewDir, crewEntry.Name())
				// Check if relics redirect is set up (clan should redirect to warband)
				relicsDir := relics.ResolveRelicsDir(crewPath)
				primeMdPath := filepath.Join(relicsDir, "RALLY.md")
				if !fileExists(primeMdPath) {
					issues = append(issues, primingIssue{
						location:    fmt.Sprintf("%s/clan/%s", rigName, crewEntry.Name()),
						issueType:   "missing_rally_md",
						description: "Missing RALLY.md (Horde context fallback)",
						fixable:     true,
					})
				}
			}
		}

		// Check raider RALLY.md
		raidersDir := filepath.Join(rigPath, "raiders")
		if dirExists(raidersDir) {
			pcEntries, _ := os.ReadDir(raidersDir)
			for _, pcEntry := range pcEntries {
				if !pcEntry.IsDir() || pcEntry.Name() == ".claude" {
					continue
				}
				raiderPath := filepath.Join(raidersDir, pcEntry.Name())
				// Check if relics redirect is set up
				relicsDir := relics.ResolveRelicsDir(raiderPath)
				primeMdPath := filepath.Join(relicsDir, "RALLY.md")
				if !fileExists(primeMdPath) {
					issues = append(issues, primingIssue{
						location:    fmt.Sprintf("%s/raiders/%s", rigName, pcEntry.Name()),
						issueType:   "missing_rally_md",
						description: "Missing RALLY.md (Horde context fallback)",
						fixable:     true,
					})
				}
			}
		}
	}

	return issues
}

// hasGtPrimeHook checks if settings have a SessionStart hook that calls hd rally.
func (c *PrimingCheck) hasGtPrimeHook(settings map[string]any) bool {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}

	hookList, ok := hooks["SessionStart"].([]any)
	if !ok {
		return false
	}

	for _, hook := range hookList {
		hookMap, ok := hook.(map[string]any)
		if !ok {
			continue
		}
		innerHooks, ok := hookMap["hooks"].([]any)
		if !ok {
			continue
		}
		for _, inner := range innerHooks {
			innerMap, ok := inner.(map[string]any)
			if !ok {
				continue
			}
			cmd, ok := innerMap["command"].(string)
			if ok && strings.Contains(cmd, "hd rally") {
				return true
			}
		}
	}
	return false
}

// countLines counts the number of lines in a file.
func (c *PrimingCheck) countLines(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count
}

// Fix attempts to fix priming issues.
func (c *PrimingCheck) Fix(ctx *CheckContext) error {
	var errors []string

	for _, issue := range c.issues {
		if !issue.fixable {
			continue
		}

		switch issue.issueType {
		case "missing_rally_md":
			// Provision RALLY.md at the appropriate location
			var targetPath string

			// Parse the location to determine where to provision
			if strings.Contains(issue.location, "/clan/") || strings.Contains(issue.location, "/raiders/") {
				// Worker location - use relics.ProvisionPrimeMDForWorktree
				worktreePath := filepath.Join(ctx.TownRoot, issue.location)
				if err := relics.ProvisionPrimeMDForWorktree(worktreePath); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", issue.location, err))
				}
			} else {
				// Warband location - provision directly
				targetPath = filepath.Join(ctx.TownRoot, issue.location, constants.DirRelics)
				if err := relics.ProvisionPrimeMD(targetPath); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", issue.location, err))
				}
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return nil
}
