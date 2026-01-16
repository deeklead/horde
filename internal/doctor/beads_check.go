package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/deeklead/horde/internal/relics"
)

// RelicsDatabaseCheck verifies that the relics database is properly initialized.
// It detects when issues.db is empty or missing critical columns, and can
// auto-fix by triggering a re-import from the JSONL file.
type RelicsDatabaseCheck struct {
	FixableCheck
}

// NewRelicsDatabaseCheck creates a new relics database check.
func NewRelicsDatabaseCheck() *RelicsDatabaseCheck {
	return &RelicsDatabaseCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "relics-database",
				CheckDescription: "Verify relics database is properly initialized",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks if the relics database is properly initialized.
func (c *RelicsDatabaseCheck) Run(ctx *CheckContext) *CheckResult {
	// Check encampment-level relics
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "No .relics directory found at encampment root",
			FixHint: "Run 'bd init' to initialize relics",
		}
	}

	// Check if issues.db exists and has content
	issuesDB := filepath.Join(relicsDir, "issues.db")
	issuesJSONL := filepath.Join(relicsDir, "issues.jsonl")

	dbInfo, dbErr := os.Stat(issuesDB)
	jsonlInfo, jsonlErr := os.Stat(issuesJSONL)

	// If no database file, that's OK - relics will create it
	if os.IsNotExist(dbErr) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No issues.db file (will be created on first use)",
		}
	}

	// If database file is empty but JSONL has content, this is the bug
	if dbErr == nil && dbInfo.Size() == 0 {
		if jsonlErr == nil && jsonlInfo.Size() > 0 {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusError,
				Message: "issues.db is empty but issues.jsonl has content",
				Details: []string{
					"This can cause 'table issues has no column named pinned' errors",
					"The database needs to be rebuilt from the JSONL file",
				},
				FixHint: "Run 'hd doctor --fix' or delete issues.db and run 'bd sync --from-main'",
			}
		}
	}

	// Also check warband-level relics if a warband is specified
	// Follows redirect if present (warband root may redirect to warchief/warband/.relics)
	if ctx.RigName != "" {
		rigRelicsDir := relics.ResolveRelicsDir(ctx.RigPath())
		if _, err := os.Stat(rigRelicsDir); err == nil {
			rigDB := filepath.Join(rigRelicsDir, "issues.db")
			rigJSONL := filepath.Join(rigRelicsDir, "issues.jsonl")

			rigDBInfo, rigDBErr := os.Stat(rigDB)
			rigJSONLInfo, rigJSONLErr := os.Stat(rigJSONL)

			if rigDBErr == nil && rigDBInfo.Size() == 0 {
				if rigJSONLErr == nil && rigJSONLInfo.Size() > 0 {
					return &CheckResult{
						Name:    c.Name(),
						Status:  StatusError,
						Message: "Warband issues.db is empty but issues.jsonl has content",
						Details: []string{
							"Warband: " + ctx.RigName,
							"This can cause 'table issues has no column named pinned' errors",
						},
						FixHint: "Run 'hd doctor --fix' or delete the warband's issues.db",
					}
				}
			}
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "Relics database is properly initialized",
	}
}

// Fix attempts to rebuild the database from JSONL.
func (c *RelicsDatabaseCheck) Fix(ctx *CheckContext) error {
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")
	issuesDB := filepath.Join(relicsDir, "issues.db")
	issuesJSONL := filepath.Join(relicsDir, "issues.jsonl")

	// Check if we need to fix encampment-level database
	dbInfo, dbErr := os.Stat(issuesDB)
	jsonlInfo, jsonlErr := os.Stat(issuesJSONL)

	if dbErr == nil && dbInfo.Size() == 0 && jsonlErr == nil && jsonlInfo.Size() > 0 {
		// Delete the empty database file
		if err := os.Remove(issuesDB); err != nil {
			return err
		}

		// Run rl sync to rebuild from JSONL
		cmd := exec.Command("rl", "sync", "--from-main")
		cmd.Dir = ctx.TownRoot
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	// Also fix warband-level if specified (follows redirect if present)
	if ctx.RigName != "" {
		rigRelicsDir := relics.ResolveRelicsDir(ctx.RigPath())
		rigDB := filepath.Join(rigRelicsDir, "issues.db")
		rigJSONL := filepath.Join(rigRelicsDir, "issues.jsonl")

		rigDBInfo, rigDBErr := os.Stat(rigDB)
		rigJSONLInfo, rigJSONLErr := os.Stat(rigJSONL)

		if rigDBErr == nil && rigDBInfo.Size() == 0 && rigJSONLErr == nil && rigJSONLInfo.Size() > 0 {
			if err := os.Remove(rigDB); err != nil {
				return err
			}

			cmd := exec.Command("rl", "sync", "--from-main")
			cmd.Dir = ctx.RigPath()
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return err
			}
		}
	}

	return nil
}

// PrefixConflictCheck detects duplicate prefixes across warbands in routes.jsonl.
// Duplicate prefixes break prefix-based routing.
type PrefixConflictCheck struct {
	BaseCheck
}

// NewPrefixConflictCheck creates a new prefix conflict check.
func NewPrefixConflictCheck() *PrefixConflictCheck {
	return &PrefixConflictCheck{
		BaseCheck: BaseCheck{
			CheckName:        "prefix-conflict",
			CheckDescription: "Check for duplicate relics prefixes across warbands",
			CheckCategory:    CategoryConfig,
		},
	}
}

// Run checks for duplicate prefixes in routes.jsonl.
func (c *PrefixConflictCheck) Run(ctx *CheckContext) *CheckResult {
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")

	// Check if routes.jsonl exists
	routesPath := filepath.Join(relicsDir, relics.RoutesFileName)
	if _, err := os.Stat(routesPath); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No routes.jsonl file (prefix routing not configured)",
		}
	}

	// Find conflicts
	conflicts, err := relics.FindConflictingPrefixes(relicsDir)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not check routes.jsonl: %v", err),
		}
	}

	if len(conflicts) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No prefix conflicts found",
		}
	}

	// Build details
	var details []string
	for prefix, paths := range conflicts {
		details = append(details, fmt.Sprintf("Prefix %q used by: %s", prefix, strings.Join(paths, ", ")))
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("%d prefix conflict(s) found in routes.jsonl", len(conflicts)),
		Details: details,
		FixHint: "Use 'bd rename-prefix <new-prefix>' in one of the conflicting warbands to resolve",
	}
}

// PrefixMismatchCheck detects when warbands.json has a different prefix than what
// routes.jsonl actually uses for a warband. This can happen when:
// - deriveRelicsPrefix() generates a different prefix than what's in the relics DB
// - Someone manually edited warbands.json with the wrong prefix
// - The relics were initialized before auto-derive existed with a different prefix
type PrefixMismatchCheck struct {
	FixableCheck
}

// NewPrefixMismatchCheck creates a new prefix mismatch check.
func NewPrefixMismatchCheck() *PrefixMismatchCheck {
	return &PrefixMismatchCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "prefix-mismatch",
				CheckDescription: "Check for prefix mismatches between warbands.json and routes.jsonl",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks for prefix mismatches between warbands.json and routes.jsonl.
func (c *PrefixMismatchCheck) Run(ctx *CheckContext) *CheckResult {
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")

	// Load routes.jsonl
	routes, err := relics.LoadRoutes(relicsDir)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("Could not load routes.jsonl: %v", err),
		}
	}
	if len(routes) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No routes configured (nothing to check)",
		}
	}

	// Load warbands.json
	rigsPath := filepath.Join(ctx.TownRoot, "warchief", "warbands.json")
	rigsConfig, err := loadRigsConfig(rigsPath)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warbands.json found (nothing to check)",
		}
	}

	// Build map of route path -> prefix from routes.jsonl
	routePrefixByPath := make(map[string]string)
	for _, r := range routes {
		// Normalize: strip trailing hyphen from prefix for comparison
		prefix := strings.TrimSuffix(r.Prefix, "-")
		routePrefixByPath[r.Path] = prefix
	}

	// Check each warband in warbands.json against routes.jsonl
	var mismatches []string
	mismatchData := make(map[string][2]string) // rigName -> [rigsJsonPrefix, routesPrefix]

	for rigName, rigEntry := range rigsConfig.Warbands {
		// Skip warbands without relics config
		if rigEntry.RelicsConfig == nil || rigEntry.RelicsConfig.Prefix == "" {
			continue
		}

		rigsJsonPrefix := rigEntry.RelicsConfig.Prefix
		expectedPath := rigName + "/warchief/warband"

		// Find the route for this warband
		routePrefix, hasRoute := routePrefixByPath[expectedPath]
		if !hasRoute {
			// No route for this warband - routes-config check handles this
			continue
		}

		// Compare prefixes (both should be without trailing hyphen)
		if rigsJsonPrefix != routePrefix {
			mismatches = append(mismatches, rigName)
			mismatchData[rigName] = [2]string{rigsJsonPrefix, routePrefix}
		}
	}

	if len(mismatches) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No prefix mismatches found",
		}
	}

	// Build details
	var details []string
	for _, rigName := range mismatches {
		data := mismatchData[rigName]
		details = append(details, fmt.Sprintf("Warband '%s': warbands.json says '%s', routes.jsonl uses '%s'",
			rigName, data[0], data[1]))
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d prefix mismatch(es) between warbands.json and routes.jsonl", len(mismatches)),
		Details: details,
		FixHint: "Run 'hd doctor --fix' to update warbands.json with correct prefixes",
	}
}

// Fix updates warbands.json to match the prefixes in routes.jsonl.
func (c *PrefixMismatchCheck) Fix(ctx *CheckContext) error {
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")

	// Load routes.jsonl
	routes, err := relics.LoadRoutes(relicsDir)
	if err != nil || len(routes) == 0 {
		return nil // Nothing to fix
	}

	// Load warbands.json
	rigsPath := filepath.Join(ctx.TownRoot, "warchief", "warbands.json")
	rigsConfig, err := loadRigsConfig(rigsPath)
	if err != nil {
		return nil // Nothing to fix
	}

	// Build map of route path -> prefix from routes.jsonl
	routePrefixByPath := make(map[string]string)
	for _, r := range routes {
		prefix := strings.TrimSuffix(r.Prefix, "-")
		routePrefixByPath[r.Path] = prefix
	}

	// Update each warband's prefix to match routes.jsonl
	modified := false
	for rigName, rigEntry := range rigsConfig.Warbands {
		expectedPath := rigName + "/warchief/warband"
		routePrefix, hasRoute := routePrefixByPath[expectedPath]
		if !hasRoute {
			continue
		}

		// Ensure RelicsConfig exists
		if rigEntry.RelicsConfig == nil {
			rigEntry.RelicsConfig = &rigsConfigRelicsConfig{}
		}

		if rigEntry.RelicsConfig.Prefix != routePrefix {
			rigEntry.RelicsConfig.Prefix = routePrefix
			rigsConfig.Warbands[rigName] = rigEntry
			modified = true
		}
	}

	if modified {
		return saveRigsConfig(rigsPath, rigsConfig)
	}

	return nil
}

// rigsConfigEntry is a local type for loading warbands.json without importing config package
// to avoid circular dependencies and keep the check self-contained.
type rigsConfigEntry struct {
	GitURL      string                 `json:"git_url"`
	LocalRepo   string                 `json:"local_repo,omitempty"`
	AddedAt     string                 `json:"added_at"` // Keep as string to preserve format
	RelicsConfig *rigsConfigRelicsConfig `json:"relics,omitempty"`
}

type rigsConfigRelicsConfig struct {
	Repo   string `json:"repo"`
	Prefix string `json:"prefix"`
}

type rigsConfigFile struct {
	Version int                         `json:"version"`
	Warbands    map[string]rigsConfigEntry  `json:"warbands"`
}

func loadRigsConfig(path string) (*rigsConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg rigsConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func saveRigsConfig(path string, cfg *rigsConfigFile) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// beadShower is an interface for fetching bead information.
// Allows mocking in tests.
type beadShower interface {
	Show(id string) (*relics.Issue, error)
}

// labelAdder is an interface for adding labels to relics.
// Allows mocking in tests.
type labelAdder interface {
	AddLabel(townRoot, id, label string) error
}

// realLabelAdder implements labelAdder using rl command.
type realLabelAdder struct{}

func (r *realLabelAdder) AddLabel(townRoot, id, label string) error {
	cmd := exec.Command("rl", "label", "add", id, label)
	cmd.Dir = townRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adding %s label to %s: %s", label, id, strings.TrimSpace(string(output)))
	}
	return nil
}

// RoleLabelCheck verifies that role relics have the gt:role label.
// This label is required for GetRoleConfig to recognize role relics.
// Role relics created before the label migration may be missing this label.
type RoleLabelCheck struct {
	FixableCheck
	missingLabel []string // Role bead IDs missing gt:role label
	townRoot     string   // Cached for Fix

	// Injected dependencies for testing
	beadShower beadShower
	labelAdder labelAdder
}

// NewRoleLabelCheck creates a new role label check.
func NewRoleLabelCheck() *RoleLabelCheck {
	return &RoleLabelCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "role-bead-labels",
				CheckDescription: "Check that role relics have gt:role label",
				CheckCategory:    CategoryConfig,
			},
		},
		labelAdder: &realLabelAdder{},
	}
}

// roleBeadIDs returns the list of role bead IDs to check.
func roleBeadIDs() []string {
	return []string{
		relics.WarchiefRoleBeadIDTown(),
		relics.ShamanRoleBeadIDTown(),
		relics.DogRoleBeadIDTown(),
		relics.WitnessRoleBeadIDTown(),
		relics.ForgeRoleBeadIDTown(),
		relics.RaiderRoleBeadIDTown(),
		relics.CrewRoleBeadIDTown(),
	}
}

// Run checks if role relics have the gt:role label.
func (c *RoleLabelCheck) Run(ctx *CheckContext) *CheckResult {
	// Check if rl command is available (skip if testing with mock)
	if c.beadShower == nil {
		if _, err := exec.LookPath("rl"); err != nil {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusOK,
				Message: "relics not installed (skipped)",
			}
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

	// Use injected beadShower or create real one
	shower := c.beadShower
	if shower == nil {
		shower = relics.New(ctx.TownRoot)
	}

	var missingLabel []string
	for _, roleID := range roleBeadIDs() {
		issue, err := shower.Show(roleID)
		if err != nil {
			// Bead doesn't exist - that's OK, install will create it
			continue
		}

		// Check if it has the gt:role label
		if !relics.HasLabel(issue, "gt:role") {
			missingLabel = append(missingLabel, roleID)
		}
	}

	// Cache for Fix
	c.missingLabel = missingLabel
	c.townRoot = ctx.TownRoot

	if len(missingLabel) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All role relics have gt:role label",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d role bead(s) missing gt:role label", len(missingLabel)),
		Details: missingLabel,
		FixHint: "Run 'hd doctor --fix' to add missing labels",
	}
}

// Fix adds the gt:role label to role relics that are missing it.
func (c *RoleLabelCheck) Fix(ctx *CheckContext) error {
	for _, roleID := range c.missingLabel {
		if err := c.labelAdder.AddLabel(c.townRoot, roleID, "gt:role"); err != nil {
			return err
		}
	}
	return nil
}
