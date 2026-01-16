//go:build integration

// Package cmd contains integration tests for relics routing and redirects.
//
// Run with: go test -tags=integration ./internal/cmd -run TestRelicsRouting -v
package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/deeklead/horde/internal/relics"
)

// setupRoutingTestTown creates a minimal Horde with multiple warbands for testing routing.
// Returns townRoot.
func setupRoutingTestTown(t *testing.T) string {
	t.Helper()

	townRoot := t.TempDir()

	// Create encampment-level .relics directory
	townRelicsDir := filepath.Join(townRoot, ".relics")
	if err := os.MkdirAll(townRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir encampment .relics: %v", err)
	}

	// Create routes.jsonl with multiple warbands
	routes := []relics.Route{
		{Prefix: "hq-", Path: "."},                      // Encampment-level relics
		{Prefix: "hd-", Path: "horde/warchief/warband"},      // Horde warband
		{Prefix: "tr-", Path: "testrig/warchief/warband"},      // Test warband
	}
	if err := relics.WriteRoutes(townRelicsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	// Create horde warband structure
	gasRigPath := filepath.Join(townRoot, "horde", "warchief", "warband")
	if err := os.MkdirAll(gasRigPath, 0755); err != nil {
		t.Fatalf("mkdir horde: %v", err)
	}

	// Create horde .relics directory with its own config
	gasRelicsDir := filepath.Join(gasRigPath, ".relics")
	if err := os.MkdirAll(gasRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir horde .relics: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gasRelicsDir, "config.yaml"), []byte("prefix: gt\n"), 0644); err != nil {
		t.Fatalf("write horde config: %v", err)
	}

	// Create testrig structure
	testRigPath := filepath.Join(townRoot, "testrig", "warchief", "warband")
	if err := os.MkdirAll(testRigPath, 0755); err != nil {
		t.Fatalf("mkdir testrig: %v", err)
	}

	// Create testrig .relics directory
	testRelicsDir := filepath.Join(testRigPath, ".relics")
	if err := os.MkdirAll(testRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir testrig .relics: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testRelicsDir, "config.yaml"), []byte("prefix: tr\n"), 0644); err != nil {
		t.Fatalf("write testrig config: %v", err)
	}

	// Create raiders directory with redirect
	raidersDir := filepath.Join(townRoot, "horde", "raiders", "rictus")
	if err := os.MkdirAll(raidersDir, 0755); err != nil {
		t.Fatalf("mkdir raiders: %v", err)
	}

	// Create redirect file for raider -> warchief/warband/.relics
	// Path: horde/raiders/rictus -> ../../warchief/warband/.relics -> horde/warchief/warband/.relics
	raiderRelicsDir := filepath.Join(raidersDir, ".relics")
	if err := os.MkdirAll(raiderRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir raider .relics: %v", err)
	}
	redirectContent := "../../warchief/warband/.relics"
	if err := os.WriteFile(filepath.Join(raiderRelicsDir, "redirect"), []byte(redirectContent), 0644); err != nil {
		t.Fatalf("write redirect: %v", err)
	}

	// Create clan directory with redirect
	// Path: horde/clan/max -> ../../warchief/warband/.relics -> horde/warchief/warband/.relics
	crewDir := filepath.Join(townRoot, "horde", "clan", "max")
	if err := os.MkdirAll(crewDir, 0755); err != nil {
		t.Fatalf("mkdir clan: %v", err)
	}

	crewRelicsDir := filepath.Join(crewDir, ".relics")
	if err := os.MkdirAll(crewRelicsDir, 0755); err != nil {
		t.Fatalf("mkdir clan .relics: %v", err)
	}
	crewRedirect := "../../warchief/warband/.relics"
	if err := os.WriteFile(filepath.Join(crewRelicsDir, "redirect"), []byte(crewRedirect), 0644); err != nil {
		t.Fatalf("write clan redirect: %v", err)
	}

	return townRoot
}

func initRelicsDBWithPrefix(t *testing.T, dir, prefix string) {
	t.Helper()

	cmd := exec.Command("rl", "--no-daemon", "init", "--quiet", "--prefix", prefix)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init failed in %s: %v\n%s", dir, err, output)
	}

	// Create empty issues.jsonl to prevent rl auto-export from corrupting routes.jsonl.
	// Without this, rl create writes issue data to routes.jsonl (the first .jsonl file
	// it finds), corrupting the routing configuration. This mirrors what hd install does.
	issuesPath := filepath.Join(dir, ".relics", "issues.jsonl")
	if err := os.WriteFile(issuesPath, []byte(""), 0644); err != nil {
		t.Fatalf("create issues.jsonl in %s: %v", dir, err)
	}
}

func createTestIssue(t *testing.T, dir, title string) *relics.Issue {
	t.Helper()

	args := []string{"--no-daemon", "create", "--json", "--title", title, "--type", "task",
		"--description", "Integration test issue"}
	cmd := exec.Command("rl", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		combinedCmd := exec.Command("rl", args...)
		combinedCmd.Dir = dir
		combinedOutput, _ := combinedCmd.CombinedOutput()
		t.Fatalf("create issue in %s: %v\n%s", dir, err, combinedOutput)
	}

	var issue relics.Issue
	if err := json.Unmarshal(output, &issue); err != nil {
		t.Fatalf("parse create output in %s: %v", dir, err)
	}
	if issue.ID == "" {
		t.Fatalf("create issue in %s returned empty ID", dir)
	}
	return &issue
}

func hasIssueID(issues []*relics.Issue, id string) bool {
	for _, issue := range issues {
		if issue.ID == id {
			return true
		}
	}
	return false
}

// TestRelicsRoutingFromTownRoot verifies that rl show routes to correct warband
// based on issue ID prefix when run from encampment root.
func TestRelicsRoutingFromTownRoot(t *testing.T) {
	// Skip if rl is not available
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping routing test")
	}

	townRoot := setupRoutingTestTown(t)

	initRelicsDBWithPrefix(t, townRoot, "hq")

	hordeRigPath := filepath.Join(townRoot, "horde", "warchief", "warband")
	testrigRigPath := filepath.Join(townRoot, "testrig", "warchief", "warband")
	initRelicsDBWithPrefix(t, hordeRigPath, "hd")
	initRelicsDBWithPrefix(t, testrigRigPath, "tr")

	townIssue := createTestIssue(t, townRoot, "Encampment-level routing test")
	hordeIssue := createTestIssue(t, hordeRigPath, "Horde routing test")
	testrigIssue := createTestIssue(t, testrigRigPath, "Testrig routing test")

	tests := []struct {
		id    string
		title string
	}{
		{townIssue.ID, townIssue.Title},
		{hordeIssue.ID, hordeIssue.Title},
		{testrigIssue.ID, testrigIssue.Title},
	}

	townRelics := relics.New(townRoot)
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			issue, err := townRelics.Show(tc.id)
			if err != nil {
				t.Fatalf("bd show %s failed: %v", tc.id, err)
			}
			if issue.ID != tc.id {
				t.Errorf("issue.ID = %s, want %s", issue.ID, tc.id)
			}
			if issue.Title != tc.title {
				t.Errorf("issue.Title = %q, want %q", issue.Title, tc.title)
			}
		})
	}
}

// TestRelicsRedirectResolution verifies that redirect files are followed correctly.
func TestRelicsRedirectResolution(t *testing.T) {
	townRoot := setupRoutingTestTown(t)

	tests := []struct {
		name     string
		workDir  string
		expected string // Expected resolved path (relative to townRoot)
	}{
		{
			name:     "raider redirect",
			workDir:  "horde/raiders/rictus",
			expected: "horde/warchief/warband/.relics",
		},
		{
			name:     "clan redirect",
			workDir:  "horde/clan/max",
			expected: "horde/warchief/warband/.relics",
		},
		{
			name:     "no redirect (warchief/warband)",
			workDir:  "horde/warchief/warband",
			expected: "horde/warchief/warband/.relics",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fullWorkDir := filepath.Join(townRoot, tc.workDir)
			resolved := relics.ResolveRelicsDir(fullWorkDir)

			expectedFull := filepath.Join(townRoot, tc.expected)
			if resolved != expectedFull {
				t.Errorf("ResolveRelicsDir(%s) = %s, want %s", tc.workDir, resolved, expectedFull)
			}
		})
	}
}

// TestRelicsCircularRedirectDetection verifies that circular redirects are detected.
func TestRelicsCircularRedirectDetection(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a relics directory with a redirect pointing to itself
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create redirect file pointing to itself (circular)
	redirectContent := ".relics" // Points to current .relics (circular)
	if err := os.WriteFile(filepath.Join(relicsDir, "redirect"), []byte(redirectContent), 0644); err != nil {
		t.Fatalf("write redirect: %v", err)
	}

	// ResolveRelicsDir should detect the circular redirect and return the original
	resolved := relics.ResolveRelicsDir(tmpDir)
	if resolved != relicsDir {
		t.Errorf("expected circular redirect to return original relics dir, got %s", resolved)
	}

	// The redirect file should have been removed
	redirectPath := filepath.Join(relicsDir, "redirect")
	if _, err := os.Stat(redirectPath); !os.IsNotExist(err) {
		t.Error("circular redirect file should have been removed")
	}
}

// TestRelicsPrefixConflictDetection verifies that duplicate prefixes are detected.
func TestRelicsPrefixConflictDetection(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create routes with a duplicate prefix
	routes := []relics.Route{
		{Prefix: "hd-", Path: "horde/warchief/warband"},
		{Prefix: "hd-", Path: "other/warchief/warband"}, // Duplicate!
		{Prefix: "bd-", Path: "relics/warchief/warband"},
	}
	if err := relics.WriteRoutes(relicsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	// FindConflictingPrefixes should detect the duplicate
	conflicts, err := relics.FindConflictingPrefixes(relicsDir)
	if err != nil {
		t.Fatalf("FindConflictingPrefixes: %v", err)
	}

	if len(conflicts) == 0 {
		t.Error("expected to find conflicts, got none")
	}

	if paths, ok := conflicts["hd-"]; !ok {
		t.Error("expected conflict for prefix 'gt-'")
	} else if len(paths) != 2 {
		t.Errorf("expected 2 conflicting paths for 'gt-', got %d", len(paths))
	}
}

// TestRelicsListFromRaiderDirectory verifies that rl list works from raider directories.
func TestRelicsListFromRaiderDirectory(t *testing.T) {
	// Skip if rl is not available
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	townRoot := setupRoutingTestTown(t)
	raiderDir := filepath.Join(townRoot, "horde", "raiders", "rictus")

	rigPath := filepath.Join(townRoot, "horde", "warchief", "warband")
	initRelicsDBWithPrefix(t, rigPath, "hd")

	issue := createTestIssue(t, rigPath, "Raider list redirect test")

	issues, err := relics.New(raiderDir).List(relics.ListOptions{
		Status:   "open",
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("bd list from raider dir failed: %v", err)
	}

	if !hasIssueID(issues, issue.ID) {
		t.Errorf("bd list from raider dir missing issue %s", issue.ID)
	}
}

// TestRelicsListFromCrewDirectory verifies that rl list works from clan directories.
func TestRelicsListFromCrewDirectory(t *testing.T) {
	// Skip if rl is not available
	if _, err := exec.LookPath("rl"); err != nil {
		t.Skip("bd not installed, skipping test")
	}

	townRoot := setupRoutingTestTown(t)
	crewDir := filepath.Join(townRoot, "horde", "clan", "max")

	rigPath := filepath.Join(townRoot, "horde", "warchief", "warband")
	initRelicsDBWithPrefix(t, rigPath, "hd")

	issue := createTestIssue(t, rigPath, "Clan list redirect test")

	issues, err := relics.New(crewDir).List(relics.ListOptions{
		Status:   "open",
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("bd list from clan dir failed: %v", err)
	}
	if !hasIssueID(issues, issue.ID) {
		t.Errorf("bd list from clan dir missing issue %s", issue.ID)
	}
}

// TestRelicsRoutesLoading verifies that routes.jsonl is loaded correctly.
func TestRelicsRoutesLoading(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create routes.jsonl with various entries
	routesContent := `{"prefix": "hq-", "path": "."}
{"prefix": "hd-", "path": "horde/warchief/warband"}
# Comment line should be ignored
{"prefix": "bd-", "path": "relics/warchief/warband"}

{"prefix": "tr-", "path": "testrig/warchief/warband"}
`
	if err := os.WriteFile(filepath.Join(relicsDir, "routes.jsonl"), []byte(routesContent), 0644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	routes, err := relics.LoadRoutes(relicsDir)
	if err != nil {
		t.Fatalf("LoadRoutes: %v", err)
	}

	if len(routes) != 4 {
		t.Errorf("expected 4 routes, got %d", len(routes))
	}

	// Verify specific routes
	expectedPrefixes := map[string]string{
		"hq-": ".",
		"hd-": "horde/warchief/warband",
		"bd-": "relics/warchief/warband",
		"tr-": "testrig/warchief/warband",
	}

	for _, r := range routes {
		if expected, ok := expectedPrefixes[r.Prefix]; ok {
			if r.Path != expected {
				t.Errorf("route %s: path = %q, want %q", r.Prefix, r.Path, expected)
			}
		} else {
			t.Errorf("unexpected prefix: %s", r.Prefix)
		}
	}
}

// TestRelicsAppendRoute verifies that routes can be appended and updated.
func TestRelicsAppendRoute(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Append first route
	route1 := relics.Route{Prefix: "hd-", Path: "horde/warchief/warband"}
	if err := relics.AppendRoute(tmpDir, route1); err != nil {
		t.Fatalf("AppendRoute 1: %v", err)
	}

	// Append second route
	route2 := relics.Route{Prefix: "bd-", Path: "relics/warchief/warband"}
	if err := relics.AppendRoute(tmpDir, route2); err != nil {
		t.Fatalf("AppendRoute 2: %v", err)
	}

	// Verify both routes exist
	routes, err := relics.LoadRoutes(relicsDir)
	if err != nil {
		t.Fatalf("LoadRoutes: %v", err)
	}
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}

	// Update existing route (same prefix, different path)
	route1Updated := relics.Route{Prefix: "hd-", Path: "newpath/warchief/warband"}
	if err := relics.AppendRoute(tmpDir, route1Updated); err != nil {
		t.Fatalf("AppendRoute update: %v", err)
	}

	// Verify update
	routes, _ = relics.LoadRoutes(relicsDir)
	if len(routes) != 2 {
		t.Errorf("expected 2 routes after update, got %d", len(routes))
	}

	for _, r := range routes {
		if r.Prefix == "hd-" && r.Path != "newpath/warchief/warband" {
			t.Errorf("route update failed: got path %q", r.Path)
		}
	}
}

// TestRelicsRemoveRoute verifies that routes can be removed.
func TestRelicsRemoveRoute(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create initial routes
	routes := []relics.Route{
		{Prefix: "hd-", Path: "horde/warchief/warband"},
		{Prefix: "bd-", Path: "relics/warchief/warband"},
	}
	if err := relics.WriteRoutes(relicsDir, routes); err != nil {
		t.Fatalf("WriteRoutes: %v", err)
	}

	// Remove one route
	if err := relics.RemoveRoute(tmpDir, "hd-"); err != nil {
		t.Fatalf("RemoveRoute: %v", err)
	}

	// Verify removal
	remaining, _ := relics.LoadRoutes(relicsDir)
	if len(remaining) != 1 {
		t.Errorf("expected 1 route after removal, got %d", len(remaining))
	}
	if remaining[0].Prefix != "bd-" {
		t.Errorf("wrong route remaining: %s", remaining[0].Prefix)
	}
}

// TestSlingCrossRigRoutingResolution verifies that charge can resolve warband paths
// for cross-warband bead hooking using ExtractPrefix and GetRigPathForPrefix.
// This is the fix for https://github.com/deeklead/horde/issues/148
func TestSlingCrossRigRoutingResolution(t *testing.T) {
	townRoot := setupRoutingTestTown(t)

	tests := []struct {
		beadID       string
		expectedPath string // Relative to townRoot, or "." for encampment-level
	}{
		{"gt-totem-abc", "horde/warchief/warband"},
		{"tr-task-xyz", "testrig/warchief/warband"},
		{"hq-cv-123", "."}, // Encampment-level relics
	}

	for _, tc := range tests {
		t.Run(tc.beadID, func(t *testing.T) {
			// Step 1: Extract prefix from bead ID
			prefix := relics.ExtractPrefix(tc.beadID)
			if prefix == "" {
				t.Fatalf("ExtractPrefix(%q) returned empty", tc.beadID)
			}

			// Step 2: Resolve warband path from prefix
			rigPath := relics.GetRigPathForPrefix(townRoot, prefix)
			if rigPath == "" {
				t.Fatalf("GetRigPathForPrefix(%q, %q) returned empty", townRoot, prefix)
			}

			// Step 3: Verify the path is correct
			var expectedFull string
			if tc.expectedPath == "." {
				expectedFull = townRoot
			} else {
				expectedFull = filepath.Join(townRoot, tc.expectedPath)
			}

			if rigPath != expectedFull {
				t.Errorf("GetRigPathForPrefix resolved to %q, want %q", rigPath, expectedFull)
			}

			// Step 4: Verify the .relics directory exists at that path
			relicsDir := filepath.Join(rigPath, ".relics")
			if _, err := os.Stat(relicsDir); os.IsNotExist(err) {
				t.Errorf(".relics directory doesn't exist at resolved path: %s", relicsDir)
			}
		})
	}
}

// TestSlingCrossRigUnknownPrefix verifies behavior for unknown prefixes.
func TestSlingCrossRigUnknownPrefix(t *testing.T) {
	townRoot := setupRoutingTestTown(t)

	// An unknown prefix should return empty string
	unknownBeadID := "xx-unknown-123"
	prefix := relics.ExtractPrefix(unknownBeadID)
	if prefix != "xx-" {
		t.Fatalf("ExtractPrefix(%q) = %q, want %q", unknownBeadID, prefix, "xx-")
	}

	rigPath := relics.GetRigPathForPrefix(townRoot, prefix)
	if rigPath != "" {
		t.Errorf("GetRigPathForPrefix for unknown prefix returned %q, want empty", rigPath)
	}
}

// TestRelicsGetPrefixForRig verifies prefix lookup by warband name.
func TestRelicsGetPrefixForRig(t *testing.T) {
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create routes
	routes := []relics.Route{
		{Prefix: "hd-", Path: "horde/warchief/warband"},
		{Prefix: "bd-", Path: "relics/warchief/warband"},
		{Prefix: "hq-", Path: "."},
	}
	if err := relics.WriteRoutes(relicsDir, routes); err != nil {
		t.Fatalf("WriteRoutes: %v", err)
	}

	tests := []struct {
		rigName  string
		expected string
	}{
		{"horde", "hd"},
		{"relics", "rl"},
		{"unknown", "hd"}, // Default
		{"", "hd"},        // Empty -> default
	}

	for _, tc := range tests {
		t.Run(tc.rigName, func(t *testing.T) {
			result := relics.GetPrefixForRig(tmpDir, tc.rigName)
			if result != tc.expected {
				t.Errorf("GetPrefixForRig(%q) = %q, want %q", tc.rigName, result, tc.expected)
			}
		})
	}
}
