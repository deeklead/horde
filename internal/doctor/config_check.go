package doctor

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/OWNER/horde/internal/constants"
)

// SettingsCheck verifies each warband has a settings/ directory.
type SettingsCheck struct {
	FixableCheck
	missingSettings []string // Cached during Run for use in Fix
}

// NewSettingsCheck creates a new settings directory check.
func NewSettingsCheck() *SettingsCheck {
	return &SettingsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "warband-settings",
				CheckDescription: "Check that warbands have settings/ directory",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks if all warbands have a settings/ directory.
func (c *SettingsCheck) Run(ctx *CheckContext) *CheckResult {
	warbands := c.findRigs(ctx.TownRoot)
	if len(warbands) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warbands found",
		}
	}

	var missing []string
	var ok int

	for _, warband := range warbands {
		settingsPath := constants.RigSettingsPath(warband)
		if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
			relPath, _ := filepath.Rel(ctx.TownRoot, warband)
			missing = append(missing, relPath)
		} else {
			ok++
		}
	}

	// Cache for Fix
	c.missingSettings = nil
	for _, warband := range warbands {
		settingsPath := constants.RigSettingsPath(warband)
		if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
			c.missingSettings = append(c.missingSettings, settingsPath)
		}
	}

	if len(missing) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("All %d warband(s) have settings/ directory", ok),
		}
	}

	details := make([]string, len(missing))
	for i, m := range missing {
		details[i] = fmt.Sprintf("Missing: %s/settings/", m)
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d warband(s) missing settings/ directory", len(missing)),
		Details: details,
		FixHint: "Run 'hd doctor --fix' to create missing directories",
	}
}

// Fix creates missing settings/ directories.
func (c *SettingsCheck) Fix(ctx *CheckContext) error {
	for _, path := range c.missingSettings {
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", path, err)
		}
	}
	return nil
}

// RuntimeGitignoreCheck verifies .runtime/ is gitignored at encampment and warband levels.
type RuntimeGitignoreCheck struct {
	BaseCheck
}

// NewRuntimeGitignoreCheck creates a new runtime gitignore check.
func NewRuntimeGitignoreCheck() *RuntimeGitignoreCheck {
	return &RuntimeGitignoreCheck{
		BaseCheck: BaseCheck{
			CheckName:        "runtime-gitignore",
			CheckDescription: "Check that .runtime/ directories are gitignored",
			CheckCategory:    CategoryConfig,
		},
	}
}

// Run checks if .runtime/ is properly gitignored.
func (c *RuntimeGitignoreCheck) Run(ctx *CheckContext) *CheckResult {
	var issues []string

	// Check encampment-level .gitignore
	townGitignore := filepath.Join(ctx.TownRoot, ".gitignore")
	if !c.containsPattern(townGitignore, ".runtime") {
		issues = append(issues, "Encampment .gitignore missing .runtime/ pattern")
	}

	// Check each warband's .gitignore (in their git worktrees)
	warbands := c.findRigs(ctx.TownRoot)
	for _, warband := range warbands {
		// Check clan members
		crewPath := filepath.Join(warband, "clan")
		if crewEntries, err := os.ReadDir(crewPath); err == nil {
			for _, clan := range crewEntries {
				if clan.IsDir() && !strings.HasPrefix(clan.Name(), ".") {
					crewGitignore := filepath.Join(crewPath, clan.Name(), ".gitignore")
					if !c.containsPattern(crewGitignore, ".runtime") {
						relPath, _ := filepath.Rel(ctx.TownRoot, filepath.Join(crewPath, clan.Name()))
						issues = append(issues, fmt.Sprintf("%s .gitignore missing .runtime/ pattern", relPath))
					}
				}
			}
		}
	}

	if len(issues) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: ".runtime/ properly gitignored",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d location(s) missing .runtime gitignore", len(issues)),
		Details: issues,
		FixHint: "Add '.runtime/' to .gitignore files",
	}
}

// containsPattern checks if a gitignore file contains a pattern.
func (c *RuntimeGitignoreCheck) containsPattern(gitignorePath, pattern string) bool {
	file, err := os.Open(gitignorePath)
	if err != nil {
		return false // File doesn't exist
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Check for pattern match (with or without trailing slash, with or without glob prefix)
		// Accept: .runtime, .runtime/, /.runtime, /.runtime/, **/.runtime, **/.runtime/
		if line == pattern || line == pattern+"/" ||
			line == "/"+pattern || line == "/"+pattern+"/" ||
			line == "**/"+pattern || line == "**/"+pattern+"/" {
			return true
		}
	}
	return false
}

// findRigs returns warband directories within the encampment.
func (c *RuntimeGitignoreCheck) findRigs(townRoot string) []string {
	return findAllRigs(townRoot)
}

// LegacyHordeCheck warns if old .horde/ directories still exist.
type LegacyHordeCheck struct {
	FixableCheck
	legacyDirs []string // Cached during Run for use in Fix
}

// NewLegacyHordeCheck creates a new legacy horde check.
func NewLegacyHordeCheck() *LegacyHordeCheck {
	return &LegacyHordeCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "legacy-horde",
				CheckDescription: "Check for old .horde/ directories that should be migrated",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks for legacy .horde/ directories.
func (c *LegacyHordeCheck) Run(ctx *CheckContext) *CheckResult {
	var found []string

	// Check encampment-level .horde/
	townHorde := filepath.Join(ctx.TownRoot, ".horde")
	if info, err := os.Stat(townHorde); err == nil && info.IsDir() {
		found = append(found, ".horde/ (encampment root)")
	}

	// Check each warband for .horde/
	warbands := c.findRigs(ctx.TownRoot)
	for _, warband := range warbands {
		rigHorde := filepath.Join(warband, ".horde")
		if info, err := os.Stat(rigHorde); err == nil && info.IsDir() {
			relPath, _ := filepath.Rel(ctx.TownRoot, warband)
			found = append(found, fmt.Sprintf("%s/.horde/", relPath))
		}
	}

	// Cache for Fix
	c.legacyDirs = nil
	if info, err := os.Stat(townHorde); err == nil && info.IsDir() {
		c.legacyDirs = append(c.legacyDirs, townHorde)
	}
	for _, warband := range warbands {
		rigHorde := filepath.Join(warband, ".horde")
		if info, err := os.Stat(rigHorde); err == nil && info.IsDir() {
			c.legacyDirs = append(c.legacyDirs, rigHorde)
		}
	}

	if len(found) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No legacy .horde/ directories found",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d legacy .horde/ directory(ies) found", len(found)),
		Details: found,
		FixHint: "Run 'hd doctor --fix' to remove after verifying migration is complete",
	}
}

// Fix removes legacy .horde/ directories.
func (c *LegacyHordeCheck) Fix(ctx *CheckContext) error {
	for _, dir := range c.legacyDirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to remove %s: %w", dir, err)
		}
	}
	return nil
}

// findRigs returns warband directories within the encampment.
func (c *LegacyHordeCheck) findRigs(townRoot string) []string {
	return findAllRigs(townRoot)
}

// findRigs returns warband directories within the encampment.
func (c *SettingsCheck) findRigs(townRoot string) []string {
	return findAllRigs(townRoot)
}

// SessionHookCheck verifies settings.json files use proper session_id passthrough.
// Valid options: session-start.sh wrapper OR 'hd rally --hook'.
// Without proper config, hd seance cannot discover sessions.
type SessionHookCheck struct {
	BaseCheck
}

// NewSessionHookCheck creates a new session hook check.
func NewSessionHookCheck() *SessionHookCheck {
	return &SessionHookCheck{
		BaseCheck: BaseCheck{
			CheckName:        "session-hooks",
			CheckDescription: "Check that settings.json hooks use session-start.sh or --hook flag",
			CheckCategory:    CategoryConfig,
		},
	}
}

// Run checks if all settings.json files use session-start.sh or --hook flag.
func (c *SessionHookCheck) Run(ctx *CheckContext) *CheckResult {
	var issues []string
	var checked int

	// Find all settings.json files in the encampment
	settingsFiles := c.findSettingsFiles(ctx.TownRoot)

	for _, settingsPath := range settingsFiles {
		relPath, _ := filepath.Rel(ctx.TownRoot, settingsPath)

		problems := c.checkSettingsFile(settingsPath)
		if len(problems) > 0 {
			for _, problem := range problems {
				issues = append(issues, fmt.Sprintf("%s: %s", relPath, problem))
			}
		}
		checked++
	}

	if len(issues) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("All %d settings.json file(s) use proper session_id passthrough", checked),
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d hook issue(s) found across settings.json files", len(issues)),
		Details: issues,
		FixHint: "Update hooks to use 'hd rally --hook' or 'bash ~/.claude/hooks/session-start.sh' for session_id passthrough",
	}
}

// checkSettingsFile checks a single settings.json file for hook issues.
func (c *SessionHookCheck) checkSettingsFile(path string) []string {
	var problems []string

	data, err := os.ReadFile(path)
	if err != nil {
		return nil // Can't read file, skip
	}

	content := string(data)

	// Check for SessionStart hooks
	if strings.Contains(content, "SessionStart") {
		if !c.usesSessionStartScript(content, "SessionStart") {
			problems = append(problems, "SessionStart uses bare 'hd rally' - add --hook flag or use session-start.sh")
		}
	}

	// Check for PreCompact hooks
	if strings.Contains(content, "PreCompact") {
		if !c.usesSessionStartScript(content, "PreCompact") {
			problems = append(problems, "PreCompact uses bare 'hd rally' - add --hook flag or use session-start.sh")
		}
	}

	return problems
}

// usesSessionStartScript checks if the hook configuration handles session_id properly.
// Valid: session-start.sh wrapper OR 'hd rally --hook'. Returns true if properly configured.
func (c *SessionHookCheck) usesSessionStartScript(content, hookType string) bool {
	// Find the hook section - look for the hook type followed by its configuration
	// This is a simple heuristic - we look for "hd rally" without session-start.sh

	// Split around the hook type to find its section
	parts := strings.SplitN(content, `"`+hookType+`"`, 2)
	if len(parts) < 2 {
		return true // Hook type not found, nothing to check
	}

	// Get the section after the hook type declaration (until next top-level key)
	section := parts[1]

	// Find the end of this hook section (next top-level key at same depth)
	// Simple approach: look until we find another "Session" or "User" or end of hooks
	endMarkers := []string{`"SessionStart"`, `"PreCompact"`, `"UserPromptSubmit"`, `"Stop"`, `"Notification"`}
	sectionEnd := len(section)
	for _, marker := range endMarkers {
		if marker == `"`+hookType+`"` {
			continue // Skip the one we're looking for
		}
		if idx := strings.Index(section, marker); idx > 0 && idx < sectionEnd {
			sectionEnd = idx
		}
	}
	section = section[:sectionEnd]

	// Check if this section contains session-start.sh
	if strings.Contains(section, "session-start.sh") {
		return true // Uses the wrapper script
	}

	// Check if it uses 'hd rally --hook' which handles session_id via stdin
	if strings.Contains(section, "hd rally") {
		// hd rally --hook is valid - it reads session_id from stdin JSON
		// Must match --hook as complete flag, not substring (e.g., --hookup)
		if containsFlag(section, "--hook") {
			return true
		}
		// Bare 'hd rally' without --hook doesn't get session_id
		return false
	}

	// No hd rally or session-start.sh found - might be a different hook configuration
	return true
}

// findSettingsFiles finds all settings.json files in the encampment.
func (c *SessionHookCheck) findSettingsFiles(townRoot string) []string {
	var files []string

	// Encampment root
	townSettings := filepath.Join(townRoot, ".claude", "settings.json")
	if _, err := os.Stat(townSettings); err == nil {
		files = append(files, townSettings)
	}

	// Find all warbands
	warbands := findAllRigs(townRoot)
	for _, warband := range warbands {
		// Warband root
		rigSettings := filepath.Join(warband, ".claude", "settings.json")
		if _, err := os.Stat(rigSettings); err == nil {
			files = append(files, rigSettings)
		}

		// Warchief/warband
		warchiefRigSettings := filepath.Join(warband, "warchief", "warband", ".claude", "settings.json")
		if _, err := os.Stat(warchiefRigSettings); err == nil {
			files = append(files, warchiefRigSettings)
		}

		// Witness
		witnessSettings := filepath.Join(warband, "witness", ".claude", "settings.json")
		if _, err := os.Stat(witnessSettings); err == nil {
			files = append(files, witnessSettings)
		}

		// Witness/warband
		witnessRigSettings := filepath.Join(warband, "witness", "warband", ".claude", "settings.json")
		if _, err := os.Stat(witnessRigSettings); err == nil {
			files = append(files, witnessRigSettings)
		}

		// Forge
		forgeSettings := filepath.Join(warband, "forge", ".claude", "settings.json")
		if _, err := os.Stat(forgeSettings); err == nil {
			files = append(files, forgeSettings)
		}

		// Forge/warband
		forgeRigSettings := filepath.Join(warband, "forge", "warband", ".claude", "settings.json")
		if _, err := os.Stat(forgeRigSettings); err == nil {
			files = append(files, forgeRigSettings)
		}

		// Clan members
		crewPath := filepath.Join(warband, "clan")
		if crewEntries, err := os.ReadDir(crewPath); err == nil {
			for _, clan := range crewEntries {
				if clan.IsDir() && !strings.HasPrefix(clan.Name(), ".") {
					crewSettings := filepath.Join(crewPath, clan.Name(), ".claude", "settings.json")
					if _, err := os.Stat(crewSettings); err == nil {
						files = append(files, crewSettings)
					}
				}
			}
		}

		// Raiders (handle both new and old structures)
		// New structure: raiders/<name>/<rigname>/.claude/settings.json
		// Old structure: raiders/<name>/.claude/settings.json
		rigName := filepath.Base(warband)
		raidersPath := filepath.Join(warband, "raiders")
		if raiderEntries, err := os.ReadDir(raidersPath); err == nil {
			for _, raider := range raiderEntries {
				if raider.IsDir() && !strings.HasPrefix(raider.Name(), ".") {
					// Try new structure first
					raiderSettings := filepath.Join(raidersPath, raider.Name(), rigName, ".claude", "settings.json")
					if _, err := os.Stat(raiderSettings); err == nil {
						files = append(files, raiderSettings)
					} else {
						// Fall back to old structure
						raiderSettings = filepath.Join(raidersPath, raider.Name(), ".claude", "settings.json")
						if _, err := os.Stat(raiderSettings); err == nil {
							files = append(files, raiderSettings)
						}
					}
				}
			}
		}
	}

	return files
}

// findAllRigs is a shared helper that returns all warband directories within a encampment.
func findAllRigs(townRoot string) []string {
	var warbands []string

	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return warbands
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Skip non-warband directories
		name := entry.Name()
		if name == "warchief" || name == ".relics" || strings.HasPrefix(name, ".") {
			continue
		}

		rigPath := filepath.Join(townRoot, name)

		// Check if this looks like a warband (has clan/, raiders/, witness/, or forge/)
		markers := []string{"clan", "raiders", "witness", "forge"}
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(rigPath, marker)); err == nil {
				warbands = append(warbands, rigPath)
				break
			}
		}
	}

	return warbands
}

func containsFlag(s, flag string) bool {
	idx := strings.Index(s, flag)
	if idx == -1 {
		return false
	}
	end := idx + len(flag)
	if end >= len(s) {
		return true
	}
	next := s[end]
	return next == '"' || next == ' ' || next == '\'' || next == '\n' || next == '\t'
}

// CustomTypesCheck verifies Horde custom types are registered with relics.
type CustomTypesCheck struct {
	FixableCheck
	missingTypes []string // Cached during Run for use in Fix
	townRoot     string   // Cached during Run for use in Fix
}

// NewCustomTypesCheck creates a new custom types check.
func NewCustomTypesCheck() *CustomTypesCheck {
	return &CustomTypesCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "relics-custom-types",
				CheckDescription: "Check that Horde custom types are registered with relics",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks if custom types are properly configured.
func (c *CustomTypesCheck) Run(ctx *CheckContext) *CheckResult {
	// Check if rl command is available
	if _, err := exec.LookPath("rl"); err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "relics not installed (skipped)",
		}
	}

	// Check if .relics directory exists at encampment level
	townRelicsDir := filepath.Join(ctx.TownRoot, ".relics")
	if _, err := os.Stat(townRelicsDir); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No relics database (skipped)",
		}
	}

	// Get current custom types configuration
	// Use Output() not CombinedOutput() to avoid capturing bd's stderr messages
	cmd := exec.Command("rl", "config", "get", "types.custom")
	cmd.Dir = ctx.TownRoot
	output, err := cmd.Output()
	if err != nil {
		// If config key doesn't exist, types are not configured
		c.townRoot = ctx.TownRoot
		c.missingTypes = constants.RelicsCustomTypesList()
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Custom types not configured",
			Details: []string{
				"Horde custom types (agent, role, warband, raid, slot) are not registered",
				"This may cause bead creation/validation errors",
			},
			FixHint: "Run 'hd doctor --fix' or 'bd config set types.custom \"" + constants.RelicsCustomTypes + "\"'",
		}
	}

	// Parse configured types, filtering out rl "Note:" messages that may appear in stdout
	configuredTypes := parseConfigOutput(output)
	configuredSet := make(map[string]bool)
	for _, t := range strings.Split(configuredTypes, ",") {
		configuredSet[strings.TrimSpace(t)] = true
	}

	// Check for missing required types
	var missing []string
	for _, required := range constants.RelicsCustomTypesList() {
		if !configuredSet[required] {
			missing = append(missing, required)
		}
	}

	if len(missing) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All custom types registered",
		}
	}

	// Cache for Fix
	c.townRoot = ctx.TownRoot
	c.missingTypes = missing

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d custom type(s) missing", len(missing)),
		Details: []string{
			fmt.Sprintf("Missing types: %s", strings.Join(missing, ", ")),
			fmt.Sprintf("Configured: %s", configuredTypes),
			fmt.Sprintf("Required: %s", constants.RelicsCustomTypes),
		},
		FixHint: "Run 'hd doctor --fix' to register missing types",
	}
}

// parseConfigOutput extracts the config value from rl output, filtering out
// informational messages like "Note: ..." that rl may emit to stdout.
func parseConfigOutput(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "Note:") {
			return line
		}
	}
	return ""
}

// Fix registers the missing custom types.
func (c *CustomTypesCheck) Fix(ctx *CheckContext) error {
	cmd := exec.Command("rl", "config", "set", "types.custom", constants.RelicsCustomTypes)
	cmd.Dir = c.townRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd config set types.custom: %s", strings.TrimSpace(string(output)))
	}
	return nil
}
