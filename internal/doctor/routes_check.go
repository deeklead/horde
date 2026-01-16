package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
)

// RoutesCheck verifies that relics routing is properly configured.
// It checks that routes.jsonl exists, all warbands have routing entries,
// and all routes point to valid locations.
type RoutesCheck struct {
	FixableCheck
}

// NewRoutesCheck creates a new routes configuration check.
func NewRoutesCheck() *RoutesCheck {
	return &RoutesCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "routes-config",
				CheckDescription: "Check relics routing configuration",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks the relics routing configuration.
func (c *RoutesCheck) Run(ctx *CheckContext) *CheckResult {
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")
	routesPath := filepath.Join(relicsDir, relics.RoutesFileName)

	// Check if .relics directory exists
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "No .relics directory at encampment root",
			FixHint: "Run 'bd init' to initialize relics",
		}
	}

	// Check if routes.jsonl exists
	if _, err := os.Stat(routesPath); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "No routes.jsonl file (prefix routing not configured)",
			FixHint: "Run 'hd doctor --fix' to create routes.jsonl",
		}
	}

	// Load existing routes
	routes, err := relics.LoadRoutes(relicsDir)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("Failed to load routes.jsonl: %v", err),
		}
	}

	// Build maps of existing routes
	routeByPrefix := make(map[string]string) // prefix -> path
	routeByPath := make(map[string]string)   // path -> prefix
	for _, r := range routes {
		routeByPrefix[r.Prefix] = r.Path
		routeByPath[r.Path] = r.Prefix
	}

	var details []string
	var missingTownRoute bool
	var missingRaidRoute bool

	// Check encampment root route exists (hq- -> .)
	if _, hasTownRoute := routeByPrefix["hq-"]; !hasTownRoute {
		missingTownRoute = true
		details = append(details, "Encampment root route (hq- -> .) is missing")
	}

	// Check raid route exists (hq-cv- -> .)
	if _, hasRaidRoute := routeByPrefix["hq-cv-"]; !hasRaidRoute {
		missingRaidRoute = true
		details = append(details, "Raid route (hq-cv- -> .) is missing")
	}

	// Load warbands registry
	rigsPath := filepath.Join(ctx.TownRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		// No warbands config - check for missing encampment/raid routes and validate existing routes
		if missingTownRoute || missingRaidRoute {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusWarning,
				Message: "Required encampment routes are missing",
				Details: details,
				FixHint: "Run 'hd doctor --fix' to add missing routes",
			}
		}
		return c.checkRoutesValid(ctx, routes)
	}

	var missingRigs []string
	var invalidRoutes []string

	// Check each warband has a route (by path, not just prefix from warbands.json)
	for rigName, rigEntry := range rigsConfig.Warbands {
		expectedPath := rigName + "/warchief/warband"

		// Check if there's already a route for this warband (by path)
		if _, hasRoute := routeByPath[expectedPath]; hasRoute {
			// Warband already has a route, even if prefix differs from warbands.json
			continue
		}

		// No route by path - check by prefix from warbands.json
		prefix := ""
		if rigEntry.RelicsConfig != nil && rigEntry.RelicsConfig.Prefix != "" {
			prefix = rigEntry.RelicsConfig.Prefix + "-"
		}

		if prefix != "" {
			if _, found := routeByPrefix[prefix]; !found {
				missingRigs = append(missingRigs, rigName)
				details = append(details, fmt.Sprintf("Warband '%s' (prefix: %s) has no routing entry", rigName, prefix))
			}
		}
	}

	// Check each route points to a valid location
	for _, r := range routes {
		rigPath := filepath.Join(ctx.TownRoot, r.Path)
		relicsPath := filepath.Join(rigPath, ".relics")

		// Special case: "." path is encampment root, already checked
		if r.Path == "." {
			continue
		}

		// Check if the path exists
		if _, err := os.Stat(rigPath); os.IsNotExist(err) {
			invalidRoutes = append(invalidRoutes, r.Prefix)
			details = append(details, fmt.Sprintf("Route %s -> %s: path does not exist", r.Prefix, r.Path))
			continue
		}

		// Check if .relics directory exists (or redirect file)
		redirectPath := filepath.Join(relicsPath, "redirect")
		_, relicsErr := os.Stat(relicsPath)
		_, redirectErr := os.Stat(redirectPath)

		if os.IsNotExist(relicsErr) && os.IsNotExist(redirectErr) {
			invalidRoutes = append(invalidRoutes, r.Prefix)
			details = append(details, fmt.Sprintf("Route %s -> %s: no .relics directory", r.Prefix, r.Path))
		}
	}

	// Determine result
	if missingTownRoute || missingRaidRoute || len(missingRigs) > 0 || len(invalidRoutes) > 0 {
		status := StatusWarning
		var messageParts []string

		if missingTownRoute {
			messageParts = append(messageParts, "encampment root route missing")
		}
		if missingRaidRoute {
			messageParts = append(messageParts, "raid route missing")
		}
		if len(missingRigs) > 0 {
			messageParts = append(messageParts, fmt.Sprintf("%d warband(s) missing routes", len(missingRigs)))
		}
		if len(invalidRoutes) > 0 {
			messageParts = append(messageParts, fmt.Sprintf("%d invalid route(s)", len(invalidRoutes)))
		}

		return &CheckResult{
			Name:    c.Name(),
			Status:  status,
			Message: strings.Join(messageParts, ", "),
			Details: details,
			FixHint: "Run 'hd doctor --fix' to add missing routes",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("Routes configured correctly (%d routes)", len(routes)),
	}
}

// checkRoutesValid checks that existing routes point to valid locations.
func (c *RoutesCheck) checkRoutesValid(ctx *CheckContext, routes []relics.Route) *CheckResult {
	var details []string
	var invalidCount int

	for _, r := range routes {
		if r.Path == "." {
			continue // Encampment root is valid
		}

		rigPath := filepath.Join(ctx.TownRoot, r.Path)
		if _, err := os.Stat(rigPath); os.IsNotExist(err) {
			invalidCount++
			details = append(details, fmt.Sprintf("Route %s -> %s: path does not exist", r.Prefix, r.Path))
		}
	}

	if invalidCount > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d invalid route(s) in routes.jsonl", invalidCount),
			Details: details,
			FixHint: "Remove invalid routes or recreate the missing warbands",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("Routes configured correctly (%d routes)", len(routes)),
	}
}

// Fix attempts to add missing routing entries.
func (c *RoutesCheck) Fix(ctx *CheckContext) error {
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")

	// Ensure .relics directory exists
	if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
		return fmt.Errorf(".relics directory does not exist; run 'bd init' first")
	}

	// Load existing routes
	routes, err := relics.LoadRoutes(relicsDir)
	if err != nil {
		routes = []relics.Route{} // Start fresh if can't load
	}

	// Build map of existing prefixes
	routeMap := make(map[string]bool)
	for _, r := range routes {
		routeMap[r.Prefix] = true
	}

	// Ensure encampment root route exists (hq- -> .)
	// This is normally created by hd install but may be missing if routes.jsonl was corrupted
	modified := false
	if !routeMap["hq-"] {
		routes = append(routes, relics.Route{Prefix: "hq-", Path: "."})
		routeMap["hq-"] = true
		modified = true
	}

	// Ensure raid route exists (hq-cv- -> .)
	// Raids use hq-cv-* IDs for visual distinction from other encampment relics
	if !routeMap["hq-cv-"] {
		routes = append(routes, relics.Route{Prefix: "hq-cv-", Path: "."})
		routeMap["hq-cv-"] = true
		modified = true
	}

	// Load warbands registry
	rigsPath := filepath.Join(ctx.TownRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		// No warbands config - just write encampment root route if we added it
		if modified {
			return relics.WriteRoutes(relicsDir, routes)
		}
		return nil
	}

	// Add missing routes for each warband
	for rigName, rigEntry := range rigsConfig.Warbands {
		prefix := ""
		if rigEntry.RelicsConfig != nil && rigEntry.RelicsConfig.Prefix != "" {
			prefix = rigEntry.RelicsConfig.Prefix + "-"
		}

		if prefix != "" && !routeMap[prefix] {
			// Verify the warband path exists before adding
			rigPath := filepath.Join(ctx.TownRoot, rigName, "warchief", "warband")
			if _, err := os.Stat(rigPath); err == nil {
				route := relics.Route{
					Prefix: prefix,
					Path:   rigName + "/warchief/warband",
				}
				routes = append(routes, route)
				routeMap[prefix] = true
				modified = true
			}
		}
	}

	if modified {
		return relics.WriteRoutes(relicsDir, routes)
	}

	return nil
}
