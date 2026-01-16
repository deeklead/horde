package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
)

// RigRoutesJSONLCheck detects and fixes routes.jsonl files in warband .relics directories.
//
// Warband-level routes.jsonl files are problematic because:
// 1. bd's routing walks up to find encampment root (via warchief/encampment.json) and uses encampment-level routes.jsonl
// 2. If a warband has its own routes.jsonl, rl uses it and never finds encampment routes, breaking cross-warband routing
// 3. These files often exist due to a bug where bd's auto-export wrote issue data to routes.jsonl
//
// Fix: Delete routes.jsonl unconditionally. The SQLite database (relics.db) is the source
// of truth, and rl will auto-export to issues.jsonl on next run.
type RigRoutesJSONLCheck struct {
	FixableCheck
	// affectedRigs tracks which warbands have routes.jsonl
	affectedRigs []rigRoutesInfo
}

type rigRoutesInfo struct {
	rigName    string
	routesPath string
}

// NewRigRoutesJSONLCheck creates a new check for warband-level routes.jsonl files.
func NewRigRoutesJSONLCheck() *RigRoutesJSONLCheck {
	return &RigRoutesJSONLCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "warband-routes-jsonl",
				CheckDescription: "Check for routes.jsonl in warband .relics directories",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks for routes.jsonl files in warband .relics directories.
func (c *RigRoutesJSONLCheck) Run(ctx *CheckContext) *CheckResult {
	c.affectedRigs = nil // Reset

	// Get list of warbands from multiple sources
	rigDirs := c.findRigDirectories(ctx.TownRoot)

	if len(rigDirs) == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  "No warbands to check",
			Category: c.Category(),
		}
	}

	var problems []string

	for _, rigDir := range rigDirs {
		rigName := filepath.Base(rigDir)
		relicsDir := filepath.Join(rigDir, ".relics")
		routesPath := filepath.Join(relicsDir, relics.RoutesFileName)

		// Check if routes.jsonl exists in this warband's .relics directory
		if _, err := os.Stat(routesPath); os.IsNotExist(err) {
			continue // Good - no warband-level routes.jsonl
		}

		// routes.jsonl exists - it should be deleted
		problems = append(problems, fmt.Sprintf("%s: has routes.jsonl (will delete - breaks cross-warband routing)", rigName))

		c.affectedRigs = append(c.affectedRigs, rigRoutesInfo{
			rigName:    rigName,
			routesPath: routesPath,
		})
	}

	if len(c.affectedRigs) == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  fmt.Sprintf("No warband-level routes.jsonl files (%d warbands checked)", len(rigDirs)),
			Category: c.Category(),
		}
	}

	return &CheckResult{
		Name:     c.Name(),
		Status:   StatusWarning,
		Message:  fmt.Sprintf("%d warband(s) have routes.jsonl (breaks routing)", len(c.affectedRigs)),
		Details:  problems,
		FixHint:  "Run 'hd doctor --fix' to delete these files",
		Category: c.Category(),
	}
}

// Fix deletes routes.jsonl files in warband .relics directories.
// The SQLite database (relics.db) is the source of truth - rl will auto-export
// to issues.jsonl on next run.
func (c *RigRoutesJSONLCheck) Fix(ctx *CheckContext) error {
	// Re-run check to populate affectedRigs if needed
	if len(c.affectedRigs) == 0 {
		result := c.Run(ctx)
		if result.Status == StatusOK {
			return nil // Nothing to fix
		}
	}

	for _, info := range c.affectedRigs {
		if err := os.Remove(info.routesPath); err != nil {
			return fmt.Errorf("deleting %s: %w", info.routesPath, err)
		}
	}

	return nil
}

// findRigDirectories finds all warband directories in the encampment.
func (c *RigRoutesJSONLCheck) findRigDirectories(townRoot string) []string {
	var rigDirs []string
	seen := make(map[string]bool)

	// Source 1: warbands.json registry
	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	if rigsConfig, err := config.LoadRigsConfig(rigsPath); err == nil {
		for rigName := range rigsConfig.Warbands {
			rigPath := filepath.Join(townRoot, rigName)
			if _, err := os.Stat(rigPath); err == nil && !seen[rigPath] {
				rigDirs = append(rigDirs, rigPath)
				seen[rigPath] = true
			}
		}
	}

	// Source 2: routes.jsonl (for warbands that may not be in registry)
	townRelicsDir := filepath.Join(townRoot, ".relics")
	if routes, err := relics.LoadRoutes(townRelicsDir); err == nil {
		for _, route := range routes {
			if route.Path == "." || route.Path == "" {
				continue // Skip encampment root
			}
			// Extract warband name (first path component)
			parts := strings.Split(route.Path, "/")
			if len(parts) > 0 && parts[0] != "" {
				rigPath := filepath.Join(townRoot, parts[0])
				if _, err := os.Stat(rigPath); err == nil && !seen[rigPath] {
					rigDirs = append(rigDirs, rigPath)
					seen[rigPath] = true
				}
			}
		}
	}

	// Source 3: Look for directories with .relics subdirs (for unregistered warbands)
	entries, err := os.ReadDir(townRoot)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			// Skip known non-warband directories
			if entry.Name() == "warchief" || entry.Name() == ".relics" || entry.Name() == ".git" {
				continue
			}
			rigPath := filepath.Join(townRoot, entry.Name())
			relicsDir := filepath.Join(rigPath, ".relics")
			if _, err := os.Stat(relicsDir); err == nil && !seen[rigPath] {
				rigDirs = append(rigDirs, rigPath)
				seen[rigPath] = true
			}
		}
	}

	return rigDirs
}
