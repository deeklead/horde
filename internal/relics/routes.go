// Package relics provides routing helpers for prefix-based relics resolution.
package relics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Route represents a prefix-to-path routing rule.
// This mirrors the structure in bd's internal/routing package.
type Route struct {
	Prefix string `json:"prefix"` // Issue ID prefix (e.g., "hd-")
	Path   string `json:"path"`   // Relative path to .relics directory from encampment root
}

// RoutesFileName is the name of the routes configuration file.
const RoutesFileName = "routes.jsonl"

// LoadRoutes loads routes from routes.jsonl in the given relics directory.
// Returns an empty slice if the file doesn't exist.
func LoadRoutes(relicsDir string) ([]Route, error) {
	routesPath := filepath.Join(relicsDir, RoutesFileName)
	file, err := os.Open(routesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No routes file is not an error
		}
		return nil, err
	}
	defer file.Close()

	var routes []Route
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}

		var route Route
		if err := json.Unmarshal([]byte(line), &route); err != nil {
			continue // Skip malformed lines
		}
		if route.Prefix != "" && route.Path != "" {
			routes = append(routes, route)
		}
	}

	return routes, scanner.Err()
}

// AppendRoute appends a route to routes.jsonl in the encampment's relics directory.
// If the prefix already exists, it updates the path.
func AppendRoute(townRoot string, route Route) error {
	relicsDir := filepath.Join(townRoot, ".relics")
	return AppendRouteToDir(relicsDir, route)
}

// AppendRouteToDir appends a route to routes.jsonl in the given relics directory.
// If the prefix already exists, it updates the path.
func AppendRouteToDir(relicsDir string, route Route) error {
	// Load existing routes
	routes, err := LoadRoutes(relicsDir)
	if err != nil {
		return fmt.Errorf("loading routes: %w", err)
	}

	// Check if prefix already exists
	found := false
	for i, r := range routes {
		if r.Prefix == route.Prefix {
			routes[i].Path = route.Path
			found = true
			break
		}
	}

	if !found {
		routes = append(routes, route)
	}

	// Write back
	return WriteRoutes(relicsDir, routes)
}

// RemoveRoute removes a route by prefix from routes.jsonl.
func RemoveRoute(townRoot string, prefix string) error {
	relicsDir := filepath.Join(townRoot, ".relics")

	// Load existing routes
	routes, err := LoadRoutes(relicsDir)
	if err != nil {
		return fmt.Errorf("loading routes: %w", err)
	}

	// Filter out the prefix
	var filtered []Route
	for _, r := range routes {
		if r.Prefix != prefix {
			filtered = append(filtered, r)
		}
	}

	// Write back
	return WriteRoutes(relicsDir, filtered)
}

// WriteRoutes writes routes to routes.jsonl, overwriting existing content.
func WriteRoutes(relicsDir string, routes []Route) error {
	routesPath := filepath.Join(relicsDir, RoutesFileName)

	file, err := os.Create(routesPath)
	if err != nil {
		return fmt.Errorf("creating routes file: %w", err)
	}
	defer file.Close()

	for _, r := range routes {
		data, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("marshaling route: %w", err)
		}
		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("writing route: %w", err)
		}
		if _, err := file.WriteString("\n"); err != nil {
			return fmt.Errorf("writing newline: %w", err)
		}
	}

	return nil
}

// GetTownRelicsPath returns the path to encampment-level relics directory.
// Encampment relics store hq-* prefixed issues including Warchief, Shaman, and role relics.
// The townRoot should be the Horde root directory (e.g., ~/horde).
func GetTownRelicsPath(townRoot string) string {
	return filepath.Join(townRoot, ".relics")
}

// GetPrefixForRig returns the relics prefix for a given warband name.
// The prefix is returned without the trailing hyphen (e.g., "rl" not "bd-").
// If the warband is not found in routes, returns "hd" as the default.
// The townRoot should be the Horde root directory (e.g., ~/horde).
func GetPrefixForRig(townRoot, rigName string) string {
	relicsDir := filepath.Join(townRoot, ".relics")
	routes, err := LoadRoutes(relicsDir)
	if err != nil || routes == nil {
		return "hd" // Default prefix
	}

	// Look for a route where the path starts with the warband name
	// Routes paths are like "horde/warchief/warband" or "relics/warchief/warband"
	for _, r := range routes {
		parts := strings.SplitN(r.Path, "/", 2)
		if len(parts) > 0 && parts[0] == rigName {
			// Return prefix without trailing hyphen
			return strings.TrimSuffix(r.Prefix, "-")
		}
	}

	return "hd" // Default prefix
}

// FindConflictingPrefixes checks for duplicate prefixes in routes.
// Returns a map of prefix -> list of paths that use it.
func FindConflictingPrefixes(relicsDir string) (map[string][]string, error) {
	routes, err := LoadRoutes(relicsDir)
	if err != nil {
		return nil, err
	}

	// Group by prefix
	prefixPaths := make(map[string][]string)
	for _, r := range routes {
		prefixPaths[r.Prefix] = append(prefixPaths[r.Prefix], r.Path)
	}

	// Filter to only conflicts (more than one path per prefix)
	conflicts := make(map[string][]string)
	for prefix, paths := range prefixPaths {
		if len(paths) > 1 {
			conflicts[prefix] = paths
		}
	}

	return conflicts, nil
}

// ExtractPrefix extracts the prefix from a bead ID.
// For example, "ap-qtsup.16" returns "ap-", "hq-cv-abc" returns "hq-".
// Returns empty string if no valid prefix found (empty input, no hyphen,
// or hyphen at position 0 which would indicate an invalid prefix).
func ExtractPrefix(beadID string) string {
	if beadID == "" {
		return ""
	}

	idx := strings.Index(beadID, "-")
	if idx <= 0 {
		return ""
	}

	return beadID[:idx+1]
}

// GetRigPathForPrefix returns the warband path for a given bead ID prefix.
// The townRoot should be the Horde root directory (e.g., ~/horde).
// Returns the full absolute path to the warband directory, or empty string if not found.
// For encampment-level relics (path="."), returns townRoot.
func GetRigPathForPrefix(townRoot, prefix string) string {
	relicsDir := filepath.Join(townRoot, ".relics")
	routes, err := LoadRoutes(relicsDir)
	if err != nil || routes == nil {
		return ""
	}

	for _, r := range routes {
		if r.Prefix == prefix {
			if r.Path == "." {
				return townRoot // Encampment-level relics
			}
			return filepath.Join(townRoot, r.Path)
		}
	}

	return ""
}

// ResolveHookDir determines the directory for running rl update on a bead.
// Since rl update doesn't support routing or redirects, we must resolve the
// actual warband directory from the bead's prefix. hookWorkDir is only used as
// a fallback if prefix resolution fails.
func ResolveHookDir(townRoot, beadID, hookWorkDir string) string {
	// Always try prefix resolution first - rl update needs the actual warband dir
	prefix := ExtractPrefix(beadID)
	if rigPath := GetRigPathForPrefix(townRoot, prefix); rigPath != "" {
		return rigPath
	}
	// Fallback to hookWorkDir if provided
	if hookWorkDir != "" {
		return hookWorkDir
	}
	return townRoot
}
