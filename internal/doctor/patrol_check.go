package doctor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/templates"
)

// PatrolMoleculesExistCheck verifies that scout totems exist for each warband.
type PatrolMoleculesExistCheck struct {
	FixableCheck
	missingMols map[string][]string // warband -> missing totem titles
}

// NewPatrolMoleculesExistCheck creates a new scout totems exist check.
func NewPatrolMoleculesExistCheck() *PatrolMoleculesExistCheck {
	return &PatrolMoleculesExistCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "scout-totems-exist",
				CheckDescription: "Check if scout totems exist for each warband",
				CheckCategory:    CategoryPatrol,
			},
		},
	}
}

// patrolMolecules are the required scout totem titles.
var patrolMolecules = []string{
	"Shaman Scout",
	"Witness Scout",
	"Forge Scout",
}

// Run checks if scout totems exist.
func (c *PatrolMoleculesExistCheck) Run(ctx *CheckContext) *CheckResult {
	c.missingMols = make(map[string][]string)

	warbands, err := discoverRigs(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to discover warbands",
			Details: []string{err.Error()},
		}
	}

	if len(warbands) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warbands configured",
		}
	}

	var details []string
	for _, rigName := range warbands {
		rigPath := filepath.Join(ctx.TownRoot, rigName)
		missing := c.checkPatrolMolecules(rigPath)
		if len(missing) > 0 {
			c.missingMols[rigName] = missing
			details = append(details, fmt.Sprintf("%s: missing %v", rigName, missing))
		}
	}

	if len(details) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d warband(s) missing scout totems", len(c.missingMols)),
			Details: details,
			FixHint: "Run 'hd doctor --fix' to create missing scout totems",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("All %d warband(s) have scout totems", len(warbands)),
	}
}

// checkPatrolMolecules returns missing scout totem titles for a warband.
func (c *PatrolMoleculesExistCheck) checkPatrolMolecules(rigPath string) []string {
	// List totems using bd
	cmd := exec.Command("rl", "list", "--type=totem")
	cmd.Dir = rigPath
	output, err := cmd.Output()
	if err != nil {
		return patrolMolecules // Can't check, assume all missing
	}

	outputStr := string(output)
	var missing []string
	for _, mol := range patrolMolecules {
		if !strings.Contains(outputStr, mol) {
			missing = append(missing, mol)
		}
	}
	return missing
}

// Fix creates missing scout totems.
func (c *PatrolMoleculesExistCheck) Fix(ctx *CheckContext) error {
	for rigName, missing := range c.missingMols {
		rigPath := filepath.Join(ctx.TownRoot, rigName)
		for _, mol := range missing {
			desc := getPatrolMoleculeDesc(mol)
			cmd := exec.Command("rl", "create", //nolint:gosec // G204: args are constructed internally
				"--type=totem",
				"--title="+mol,
				"--description="+desc,
				"--priority=2",
			)
			cmd.Dir = rigPath
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("creating %s in %s: %w", mol, rigName, err)
			}
		}
	}
	return nil
}

func getPatrolMoleculeDesc(title string) string {
	switch title {
	case "Shaman Scout":
		return "Warchief's daemon scout loop for handling callbacks, health checks, and cleanup."
	case "Witness Scout":
		return "Per-warband worker monitor scout loop with progressive nudging."
	case "Forge Scout":
		return "Merge queue processor scout loop with verification gates."
	default:
		return "Scout totem"
	}
}

// PatrolHooksWiredCheck verifies that hooks trigger scout execution.
type PatrolHooksWiredCheck struct {
	FixableCheck
}

// NewPatrolHooksWiredCheck creates a new scout hooks wired check.
func NewPatrolHooksWiredCheck() *PatrolHooksWiredCheck {
	return &PatrolHooksWiredCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "scout-hooks-wired",
				CheckDescription: "Check if hooks trigger scout execution",
				CheckCategory:    CategoryPatrol,
			},
		},
	}
}

// Run checks if scout hooks are wired.
func (c *PatrolHooksWiredCheck) Run(ctx *CheckContext) *CheckResult {
	daemonConfigPath := config.DaemonPatrolConfigPath(ctx.TownRoot)
	relPath, _ := filepath.Rel(ctx.TownRoot, daemonConfigPath)

	if _, err := os.Stat(daemonConfigPath); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%s not found", relPath),
			FixHint: "Run 'hd doctor --fix' to create default config, or 'hd daemon start' to start the daemon",
		}
	}

	cfg, err := config.LoadDaemonPatrolConfig(daemonConfigPath)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to read daemon config",
			Details: []string{err.Error()},
		}
	}

	if len(cfg.Patrols) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("Daemon configured with %d scout(s)", len(cfg.Patrols)),
		}
	}

	if cfg.Heartbeat != nil && cfg.Heartbeat.Enabled {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Daemon heartbeat enabled (triggers patrols)",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("Configure patrols in %s or run 'hd daemon start'", relPath),
		FixHint: "Run 'hd doctor --fix' to create default config",
	}
}

// Fix creates the daemon scout config with defaults.
func (c *PatrolHooksWiredCheck) Fix(ctx *CheckContext) error {
	return config.EnsureDaemonPatrolConfig(ctx.TownRoot)
}

// PatrolNotStuckCheck detects wisps that have been in_progress too long.
type PatrolNotStuckCheck struct {
	BaseCheck
	stuckThreshold time.Duration
}

// DefaultStuckThreshold is the fallback when no role bead config exists.
// Per ZFC: "Let agents decide thresholds. 'Stuck' is a judgment call."
const DefaultStuckThreshold = 1 * time.Hour

// NewPatrolNotStuckCheck creates a new scout not stuck check.
func NewPatrolNotStuckCheck() *PatrolNotStuckCheck {
	return &PatrolNotStuckCheck{
		BaseCheck: BaseCheck{
			CheckName:        "scout-not-stuck",
			CheckDescription: "Check for stuck scout wisps (>1h in_progress)",
			CheckCategory:    CategoryPatrol,
		},
		stuckThreshold: DefaultStuckThreshold,
	}
}

// loadStuckThreshold loads the stuck threshold from the Shaman's role bead.
// Returns the default if no config exists.
func loadStuckThreshold(townRoot string) time.Duration {
	bd := relics.NewWithRelicsDir(townRoot, relics.ResolveRelicsDir(townRoot))
	roleConfig, err := bd.GetRoleConfig(relics.RoleBeadIDTown("shaman"))
	if err != nil || roleConfig == nil || roleConfig.StuckThreshold == "" {
		return DefaultStuckThreshold
	}
	if d, err := time.ParseDuration(roleConfig.StuckThreshold); err == nil {
		return d
	}
	return DefaultStuckThreshold
}

// Run checks for stuck scout wisps.
func (c *PatrolNotStuckCheck) Run(ctx *CheckContext) *CheckResult {
	// Load threshold from role bead (ZFC: agent-controlled)
	c.stuckThreshold = loadStuckThreshold(ctx.TownRoot)

	warbands, err := discoverRigs(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to discover warbands",
			Details: []string{err.Error()},
		}
	}

	if len(warbands) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warbands configured",
		}
	}

	var stuckWisps []string
	for _, rigName := range warbands {
		// Check main relics database for wisps (issues with Wisp=true)
		// Follows redirect if present (warband root may redirect to warchief/warband/.relics)
		rigPath := filepath.Join(ctx.TownRoot, rigName)
		relicsDir := relics.ResolveRelicsDir(rigPath)
		relicsPath := filepath.Join(relicsDir, "issues.jsonl")
		stuck := c.checkStuckWisps(relicsPath, rigName)
		stuckWisps = append(stuckWisps, stuck...)
	}

	thresholdStr := c.stuckThreshold.String()
	if len(stuckWisps) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d stuck scout wisp(s) found (>%s)", len(stuckWisps), thresholdStr),
			Details: stuckWisps,
			FixHint: "Manual review required - wisps may need to be burned or sessions restarted",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "No stuck scout wisps found",
	}
}

// checkStuckWisps returns descriptions of stuck wisps in a warband.
func (c *PatrolNotStuckCheck) checkStuckWisps(issuesPath string, rigName string) []string {
	file, err := os.Open(issuesPath)
	if err != nil {
		return nil // No issues file
	}
	defer file.Close()

	var stuck []string
	cutoff := time.Now().Add(-c.stuckThreshold)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var issue struct {
			ID        string    `json:"id"`
			Title     string    `json:"title"`
			Status    string    `json:"status"`
			UpdatedAt time.Time `json:"updated_at"`
		}
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			continue
		}

		// Check for in_progress issues older than threshold
		if issue.Status == "in_progress" && !issue.UpdatedAt.IsZero() && issue.UpdatedAt.Before(cutoff) {
			stuck = append(stuck, fmt.Sprintf("%s: %s (%s) - stale since %s",
				rigName, issue.ID, issue.Title, issue.UpdatedAt.Format("2006-01-02 15:04")))
		}
	}

	return stuck
}

// PatrolPluginsAccessibleCheck verifies plugin directories exist and are readable.
type PatrolPluginsAccessibleCheck struct {
	FixableCheck
	missingDirs []string
}

// NewPatrolPluginsAccessibleCheck creates a new scout plugins accessible check.
func NewPatrolPluginsAccessibleCheck() *PatrolPluginsAccessibleCheck {
	return &PatrolPluginsAccessibleCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "scout-plugins-accessible",
				CheckDescription: "Check if plugin directories exist and are readable",
				CheckCategory:    CategoryPatrol,
			},
		},
	}
}

// Run checks if plugin directories are accessible.
func (c *PatrolPluginsAccessibleCheck) Run(ctx *CheckContext) *CheckResult {
	c.missingDirs = nil

	// Check encampment-level plugins directory
	townPluginsDir := filepath.Join(ctx.TownRoot, "plugins")
	if _, err := os.Stat(townPluginsDir); os.IsNotExist(err) {
		c.missingDirs = append(c.missingDirs, townPluginsDir)
	}

	// Check warband-level plugins directories
	warbands, err := discoverRigs(ctx.TownRoot)
	if err == nil {
		for _, rigName := range warbands {
			rigPluginsDir := filepath.Join(ctx.TownRoot, rigName, "plugins")
			if _, err := os.Stat(rigPluginsDir); os.IsNotExist(err) {
				c.missingDirs = append(c.missingDirs, rigPluginsDir)
			}
		}
	}

	if len(c.missingDirs) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d plugin directory(ies) missing", len(c.missingDirs)),
			Details: c.missingDirs,
			FixHint: "Run 'hd doctor --fix' to create missing directories",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "All plugin directories accessible",
	}
}

// Fix creates missing plugin directories.
func (c *PatrolPluginsAccessibleCheck) Fix(ctx *CheckContext) error {
	for _, dir := range c.missingDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	return nil
}

// PatrolRolesHavePromptsCheck verifies that internal/templates/roles/*.md.tmpl exist for each warband.
// Checks at <encampment>/<warband>/warchief/warband/internal/templates/roles/*.md.tmpl
// Fix copies embedded templates to missing locations.
type PatrolRolesHavePromptsCheck struct {
	FixableCheck
	// missingByRig tracks missing templates per warband: rigName -> []missingFiles
	missingByRig map[string][]string
}

// NewPatrolRolesHavePromptsCheck creates a new scout roles have prompts check.
func NewPatrolRolesHavePromptsCheck() *PatrolRolesHavePromptsCheck {
	return &PatrolRolesHavePromptsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "scout-roles-have-prompts",
				CheckDescription: "Check if internal/templates/roles/*.md.tmpl exist for each scout role",
				CheckCategory:    CategoryPatrol,
			},
		},
	}
}

var requiredRolePrompts = []string{
	"shaman.md.tmpl",
	"witness.md.tmpl",
	"forge.md.tmpl",
}

func (c *PatrolRolesHavePromptsCheck) Run(ctx *CheckContext) *CheckResult {
	c.missingByRig = make(map[string][]string)

	warbands, err := discoverRigs(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to discover warbands",
			Details: []string{err.Error()},
		}
	}

	if len(warbands) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warbands configured",
		}
	}

	var missingPrompts []string
	for _, rigName := range warbands {
		// Check in warchief's clone (canonical for the warband)
		warchiefRig := filepath.Join(ctx.TownRoot, rigName, "warchief", "warband")
		templatesDir := filepath.Join(warchiefRig, "internal", "templates", "roles")

		var rigMissing []string
		for _, roleFile := range requiredRolePrompts {
			promptPath := filepath.Join(templatesDir, roleFile)
			if _, err := os.Stat(promptPath); os.IsNotExist(err) {
				missingPrompts = append(missingPrompts, fmt.Sprintf("%s: %s", rigName, roleFile))
				rigMissing = append(rigMissing, roleFile)
			}
		}
		if len(rigMissing) > 0 {
			c.missingByRig[rigName] = rigMissing
		}
	}

	if len(missingPrompts) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d role prompt template(s) missing", len(missingPrompts)),
			Details: missingPrompts,
			FixHint: "Run 'hd doctor --fix' to copy embedded templates to warband repos",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "All scout role prompt templates found",
	}
}

func (c *PatrolRolesHavePromptsCheck) Fix(ctx *CheckContext) error {
	allTemplates, err := templates.GetAllRoleTemplates()
	if err != nil {
		return fmt.Errorf("getting embedded templates: %w", err)
	}

	for rigName, missingFiles := range c.missingByRig {
		warchiefRig := filepath.Join(ctx.TownRoot, rigName, "warchief", "warband")
		templatesDir := filepath.Join(warchiefRig, "internal", "templates", "roles")

		if err := os.MkdirAll(templatesDir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", templatesDir, err)
		}

		for _, roleFile := range missingFiles {
			content, ok := allTemplates[roleFile]
			if !ok {
				continue
			}

			destPath := filepath.Join(templatesDir, roleFile)
			if err := os.WriteFile(destPath, content, 0644); err != nil {
				return fmt.Errorf("writing %s in %s: %w", roleFile, rigName, err)
			}
		}
	}

	return nil
}

// discoverRigs finds all registered warbands.
func discoverRigs(townRoot string) ([]string, error) {
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	data, err := os.ReadFile(rigsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No warbands configured
		}
		return nil, err
	}

	var rigsConfig config.RigsConfig
	if err := json.Unmarshal(data, &rigsConfig); err != nil {
		return nil, err
	}

	var warbands []string
	for name := range rigsConfig.Warbands {
		warbands = append(warbands, name)
	}
	return warbands, nil
}
