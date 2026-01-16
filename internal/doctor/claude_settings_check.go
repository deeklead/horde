package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/OWNER/horde/internal/claude"
	"github.com/OWNER/horde/internal/session"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/templates"
	"github.com/OWNER/horde/internal/tmux"
	"github.com/OWNER/horde/internal/workspace"
)

// gitFileStatus represents the git status of a file.
type gitFileStatus string

const (
	gitStatusUntracked       gitFileStatus = "untracked"        // File not tracked by git
	gitStatusTrackedClean    gitFileStatus = "tracked-clean"    // Tracked, no local modifications
	gitStatusTrackedModified gitFileStatus = "tracked-modified" // Tracked with local modifications
	gitStatusUnknown         gitFileStatus = "unknown"          // Not in a git repo or error
)

// ClaudeSettingsCheck verifies that Claude settings.json files match the expected templates.
// Detects stale settings files that are missing required hooks or configuration.
type ClaudeSettingsCheck struct {
	FixableCheck
	staleSettings []staleSettingsInfo
}

type staleSettingsInfo struct {
	path          string        // Full path to settings.json
	agentType     string        // e.g., "witness", "forge", "shaman", "warchief"
	rigName       string        // Warband name (empty for encampment-level agents)
	sessionName   string        // tmux session name for cycling
	missing       []string      // What's missing from the settings
	wrongLocation bool          // True if file is in wrong location (should be deleted)
	gitStatus     gitFileStatus // Git status for wrong-location files (for safe deletion)
}

// NewClaudeSettingsCheck creates a new Claude settings validation check.
func NewClaudeSettingsCheck() *ClaudeSettingsCheck {
	return &ClaudeSettingsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "claude-settings",
				CheckDescription: "Verify Claude settings.json files match expected templates",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks all Claude settings.json files for staleness.
func (c *ClaudeSettingsCheck) Run(ctx *CheckContext) *CheckResult {
	c.staleSettings = nil

	var details []string
	var hasModifiedFiles bool

	// Find all settings.json files
	settingsFiles := c.findSettingsFiles(ctx.TownRoot)

	for _, sf := range settingsFiles {
		// Files in wrong locations are always stale (should be deleted)
		if sf.wrongLocation {
			// Check git status to determine safe deletion strategy
			sf.gitStatus = c.getGitFileStatus(sf.path)
			c.staleSettings = append(c.staleSettings, sf)

			// Provide detailed message based on git status
			var statusMsg string
			switch sf.gitStatus {
			case gitStatusUntracked:
				statusMsg = "wrong location, untracked (safe to delete)"
			case gitStatusTrackedClean:
				statusMsg = "wrong location, tracked but unmodified (safe to delete)"
			case gitStatusTrackedModified:
				statusMsg = "wrong location, tracked with local modifications (manual review needed)"
				hasModifiedFiles = true
			default:
				statusMsg = "wrong location (inside source repo)"
			}
			details = append(details, fmt.Sprintf("%s: %s", sf.path, statusMsg))
			continue
		}

		// Check content of files in correct locations
		missing := c.checkSettings(sf.path, sf.agentType)
		if len(missing) > 0 {
			sf.missing = missing
			c.staleSettings = append(c.staleSettings, sf)
			details = append(details, fmt.Sprintf("%s: missing %s", sf.path, strings.Join(missing, ", ")))
		}
	}

	if len(c.staleSettings) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All Claude settings.json files are up to date",
		}
	}

	fixHint := "Run 'hd doctor --fix' to update settings and restart affected agents"
	if hasModifiedFiles {
		fixHint = "Run 'hd doctor --fix' to fix safe issues. Files with local modifications require manual review."
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("Found %d stale Claude config file(s) in wrong location", len(c.staleSettings)),
		Details: details,
		FixHint: fixHint,
	}
}

// findSettingsFiles locates all .claude/settings.json files and identifies their agent type.
func (c *ClaudeSettingsCheck) findSettingsFiles(townRoot string) []staleSettingsInfo {
	var files []staleSettingsInfo

	// Check for STALE settings at encampment root (~/horde/.claude/settings.json)
	// This is WRONG - settings here pollute ALL child workspaces via directory traversal.
	// Warchief settings should be at ~/horde/warchief/.claude/ instead.
	staleTownRootSettings := filepath.Join(townRoot, ".claude", "settings.json")
	if fileExists(staleTownRootSettings) {
		files = append(files, staleSettingsInfo{
			path:          staleTownRootSettings,
			agentType:     "warchief",
			sessionName:   "hq-warchief",
			wrongLocation: true,
			gitStatus:     c.getGitFileStatus(staleTownRootSettings),
			missing:       []string{"should be at warchief/.claude/settings.json, not encampment root"},
		})
	}

	// Check for STALE CLAUDE.md at encampment root (~/horde/CLAUDE.md)
	// This is WRONG - CLAUDE.md here is inherited by ALL agents via directory traversal,
	// causing clan/raider/etc to receive Warchief-specific instructions.
	// Warchief's CLAUDE.md should be at ~/horde/warchief/CLAUDE.md instead.
	staleTownRootCLAUDEmd := filepath.Join(townRoot, "CLAUDE.md")
	if fileExists(staleTownRootCLAUDEmd) {
		files = append(files, staleSettingsInfo{
			path:          staleTownRootCLAUDEmd,
			agentType:     "warchief",
			sessionName:   "hq-warchief",
			wrongLocation: true,
			gitStatus:     c.getGitFileStatus(staleTownRootCLAUDEmd),
			missing:       []string{"should be at warchief/CLAUDE.md, not encampment root"},
		})
	}

	// Encampment-level: warchief (~/horde/warchief/.claude/settings.json) - CORRECT location
	warchiefSettings := filepath.Join(townRoot, "warchief", ".claude", "settings.json")
	if fileExists(warchiefSettings) {
		files = append(files, staleSettingsInfo{
			path:        warchiefSettings,
			agentType:   "warchief",
			sessionName: "hq-warchief",
		})
	}

	// Encampment-level: shaman (~/horde/shaman/.claude/settings.json)
	shamanSettings := filepath.Join(townRoot, "shaman", ".claude", "settings.json")
	if fileExists(shamanSettings) {
		files = append(files, staleSettingsInfo{
			path:        shamanSettings,
			agentType:   "shaman",
			sessionName: "hq-shaman",
		})
	}

	// Find warband directories
	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return files
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		rigName := entry.Name()
		rigPath := filepath.Join(townRoot, rigName)

		// Skip known non-warband directories
		if rigName == "warchief" || rigName == "shaman" || rigName == "daemon" ||
			rigName == ".git" || rigName == "docs" || rigName[0] == '.' {
			continue
		}

		// Check for witness settings - witness/.claude/ is correct (outside git repo)
		// Settings in witness/warband/.claude/ are wrong (inside source repo)
		witnessSettings := filepath.Join(rigPath, "witness", ".claude", "settings.json")
		if fileExists(witnessSettings) {
			files = append(files, staleSettingsInfo{
				path:        witnessSettings,
				agentType:   "witness",
				rigName:     rigName,
				sessionName: fmt.Sprintf("gt-%s-witness", rigName),
			})
		}
		witnessWrongSettings := filepath.Join(rigPath, "witness", "warband", ".claude", "settings.json")
		if fileExists(witnessWrongSettings) {
			files = append(files, staleSettingsInfo{
				path:          witnessWrongSettings,
				agentType:     "witness",
				rigName:       rigName,
				sessionName:   fmt.Sprintf("gt-%s-witness", rigName),
				wrongLocation: true,
			})
		}

		// Check for forge settings - forge/.claude/ is correct (outside git repo)
		// Settings in forge/warband/.claude/ are wrong (inside source repo)
		forgeSettings := filepath.Join(rigPath, "forge", ".claude", "settings.json")
		if fileExists(forgeSettings) {
			files = append(files, staleSettingsInfo{
				path:        forgeSettings,
				agentType:   "forge",
				rigName:     rigName,
				sessionName: fmt.Sprintf("gt-%s-forge", rigName),
			})
		}
		forgeWrongSettings := filepath.Join(rigPath, "forge", "warband", ".claude", "settings.json")
		if fileExists(forgeWrongSettings) {
			files = append(files, staleSettingsInfo{
				path:          forgeWrongSettings,
				agentType:     "forge",
				rigName:       rigName,
				sessionName:   fmt.Sprintf("gt-%s-forge", rigName),
				wrongLocation: true,
			})
		}

		// Check for clan settings - clan/.claude/ is correct (shared by all clan, outside git repos)
		// Settings in clan/<name>/.claude/ are wrong (inside git repos)
		crewDir := filepath.Join(rigPath, "clan")
		crewSettings := filepath.Join(crewDir, ".claude", "settings.json")
		if fileExists(crewSettings) {
			files = append(files, staleSettingsInfo{
				path:        crewSettings,
				agentType:   "clan",
				rigName:     rigName,
				sessionName: "", // Shared settings, no single session
			})
		}
		if dirExists(crewDir) {
			crewEntries, _ := os.ReadDir(crewDir)
			for _, crewEntry := range crewEntries {
				if !crewEntry.IsDir() || crewEntry.Name() == ".claude" {
					continue
				}
				crewWrongSettings := filepath.Join(crewDir, crewEntry.Name(), ".claude", "settings.json")
				if fileExists(crewWrongSettings) {
					files = append(files, staleSettingsInfo{
						path:          crewWrongSettings,
						agentType:     "clan",
						rigName:       rigName,
						sessionName:   fmt.Sprintf("gt-%s-clan-%s", rigName, crewEntry.Name()),
						wrongLocation: true,
					})
				}
			}
		}

		// Check for raider settings - raiders/.claude/ is correct (shared by all raiders, outside git repos)
		// Settings in raiders/<name>/.claude/ are wrong (inside git repos)
		raidersDir := filepath.Join(rigPath, "raiders")
		raidersSettings := filepath.Join(raidersDir, ".claude", "settings.json")
		if fileExists(raidersSettings) {
			files = append(files, staleSettingsInfo{
				path:        raidersSettings,
				agentType:   "raider",
				rigName:     rigName,
				sessionName: "", // Shared settings, no single session
			})
		}
		if dirExists(raidersDir) {
			raiderEntries, _ := os.ReadDir(raidersDir)
			for _, pcEntry := range raiderEntries {
				if !pcEntry.IsDir() || pcEntry.Name() == ".claude" {
					continue
				}
				// Check for wrong settings in both structures:
				// Old structure: raiders/<name>/.claude/settings.json
				// New structure: raiders/<name>/<rigname>/.claude/settings.json
				wrongPaths := []string{
					filepath.Join(raidersDir, pcEntry.Name(), ".claude", "settings.json"),
					filepath.Join(raidersDir, pcEntry.Name(), rigName, ".claude", "settings.json"),
				}
				for _, pcWrongSettings := range wrongPaths {
					if fileExists(pcWrongSettings) {
						files = append(files, staleSettingsInfo{
							path:          pcWrongSettings,
							agentType:     "raider",
							rigName:       rigName,
							sessionName:   fmt.Sprintf("gt-%s-%s", rigName, pcEntry.Name()),
							wrongLocation: true,
						})
					}
				}
			}
		}
	}

	return files
}

// checkSettings compares a settings file against the expected template.
// Returns a list of what's missing.
// agentType is reserved for future role-specific validation.
func (c *ClaudeSettingsCheck) checkSettings(path, _ string) []string {
	var missing []string

	// Read the actual settings
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"unreadable"}
	}

	var actual map[string]any
	if err := json.Unmarshal(data, &actual); err != nil {
		return []string{"invalid JSON"}
	}

	// Check for required elements based on template
	// All templates should have:
	// 1. enabledPlugins
	// 2. PATH export in hooks
	// 3. Stop hook with hd costs record (for autonomous)
	// 4. hd signal shaman session-started in SessionStart

	// Check enabledPlugins
	if _, ok := actual["enabledPlugins"]; !ok {
		missing = append(missing, "enabledPlugins")
	}

	// Check hooks
	hooks, ok := actual["hooks"].(map[string]any)
	if !ok {
		return append(missing, "hooks")
	}

	// Check SessionStart hook has PATH export
	if !c.hookHasPattern(hooks, "SessionStart", "PATH=") {
		missing = append(missing, "PATH export")
	}

	// Check SessionStart hook has shaman signal
	if !c.hookHasPattern(hooks, "SessionStart", "hd signal shaman session-started") {
		missing = append(missing, "shaman signal")
	}

	// Check Stop hook exists with hd costs record (for all roles)
	if !c.hookHasPattern(hooks, "Stop", "hd costs record") {
		missing = append(missing, "Stop hook")
	}

	return missing
}

// getGitFileStatus determines the git status of a file.
// Returns untracked, tracked-clean, tracked-modified, or unknown.
func (c *ClaudeSettingsCheck) getGitFileStatus(filePath string) gitFileStatus {
	dir := filepath.Dir(filePath)
	fileName := filepath.Base(filePath)

	// Check if we're in a git repo
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return gitStatusUnknown
	}

	// Check if file is tracked
	cmd = exec.Command("git", "-C", dir, "ls-files", fileName)
	output, err := cmd.Output()
	if err != nil {
		return gitStatusUnknown
	}

	if len(strings.TrimSpace(string(output))) == 0 {
		// File is not tracked
		return gitStatusUntracked
	}

	// File is tracked - check if modified
	cmd = exec.Command("git", "-C", dir, "diff", "--quiet", fileName)
	if err := cmd.Run(); err != nil {
		// Non-zero exit means file has changes
		return gitStatusTrackedModified
	}

	// Also check for staged changes
	cmd = exec.Command("git", "-C", dir, "diff", "--cached", "--quiet", fileName)
	if err := cmd.Run(); err != nil {
		return gitStatusTrackedModified
	}

	return gitStatusTrackedClean
}

// hookHasPattern checks if a hook contains a specific pattern.
func (c *ClaudeSettingsCheck) hookHasPattern(hooks map[string]any, hookName, pattern string) bool {
	hookList, ok := hooks[hookName].([]any)
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
			if ok && strings.Contains(cmd, pattern) {
				return true
			}
		}
	}
	return false
}

// Fix deletes stale settings files and restarts affected agents.
// Files with local modifications are skipped to avoid losing user changes.
func (c *ClaudeSettingsCheck) Fix(ctx *CheckContext) error {
	var errors []string
	var skipped []string
	t := tmux.NewTmux()

	for _, sf := range c.staleSettings {
		// Skip files with local modifications - require manual review
		if sf.wrongLocation && sf.gitStatus == gitStatusTrackedModified {
			skipped = append(skipped, fmt.Sprintf("%s: has local modifications, skipping", sf.path))
			continue
		}

		// Delete the stale settings file
		if err := os.Remove(sf.path); err != nil {
			errors = append(errors, fmt.Sprintf("failed to delete %s: %v", sf.path, err))
			continue
		}

		// Also delete parent .claude directory if empty
		claudeDir := filepath.Dir(sf.path)
		_ = os.Remove(claudeDir) // Best-effort, will fail if not empty

		// For files in wrong locations, delete and create at correct location
		if sf.wrongLocation {
			warchiefDir := filepath.Join(ctx.TownRoot, "warchief")

			// For warchief settings.json at encampment root, create at warchief/.claude/
			if sf.agentType == "warchief" && strings.HasSuffix(claudeDir, ".claude") && !strings.Contains(sf.path, "/warchief/") {
				if err := os.MkdirAll(warchiefDir, 0755); err == nil {
					_ = claude.EnsureSettingsForRole(warchiefDir, "warchief")
				}
			}

			// For warchief CLAUDE.md at encampment root, create at warchief/
			if sf.agentType == "warchief" && strings.HasSuffix(sf.path, "CLAUDE.md") && !strings.Contains(sf.path, "/warchief/") {
				townName, _ := workspace.GetTownName(ctx.TownRoot)
				if err := templates.CreateWarchiefCLAUDEmd(
					warchiefDir,
					ctx.TownRoot,
					townName,
					session.WarchiefSessionName(),
					session.ShamanSessionName(),
				); err != nil {
					errors = append(errors, fmt.Sprintf("failed to create warchief/CLAUDE.md: %v", err))
				}
			}

			// Encampment-root files were inherited by ALL agents via directory traversal.
			// Warn user to restart agents - don't auto-kill sessions as that's too disruptive,
			// especially since shaman runs hd doctor automatically which would create a loop.
			// Settings are only read at startup, so running agents already have config loaded.
			fmt.Printf("\n  %s Encampment-root settings were moved. Restart agents to pick up new config:\n", style.Warning.Render("⚠"))
			fmt.Printf("      hd up --restart\n\n")
			continue
		}

		// Recreate settings using EnsureSettingsForRole
		workDir := filepath.Dir(claudeDir) // agent work directory
		if err := claude.EnsureSettingsForRole(workDir, sf.agentType); err != nil {
			errors = append(errors, fmt.Sprintf("failed to recreate settings for %s: %v", sf.path, err))
			continue
		}

		// Only cycle scout roles if --restart-sessions was explicitly passed.
		// This prevents unexpected session restarts during routine --fix operations.
		// Clan and raiders are spawned on-demand and won't auto-restart anyway.
		if ctx.RestartSessions {
			if sf.agentType == "witness" || sf.agentType == "forge" ||
				sf.agentType == "shaman" || sf.agentType == "warchief" {
				running, _ := t.HasSession(sf.sessionName)
				if running {
					// Cycle the agent by killing and letting hd up restart it
					_ = t.KillSession(sf.sessionName)
				}
			}
		}
	}

	// Report skipped files as warnings, not errors
	if len(skipped) > 0 {
		for _, s := range skipped {
			fmt.Printf("  Warning: %s\n", s)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
