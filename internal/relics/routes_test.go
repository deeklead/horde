package relics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetPrefixForRig(t *testing.T) {
	// Create a temporary directory with routes.jsonl
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	routesContent := `{"prefix": "hd-", "path": "horde/warchief/warband"}
{"prefix": "bd-", "path": "relics/warchief/warband"}
{"prefix": "hq-", "path": "."}
`
	if err := os.WriteFile(filepath.Join(relicsDir, "routes.jsonl"), []byte(routesContent), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		warband      string
		expected string
	}{
		{"horde", "hd"},
		{"relics", "rl"},
		{"unknown", "hd"}, // default
		{"", "hd"},        // empty warband -> default
	}

	for _, tc := range tests {
		t.Run(tc.warband, func(t *testing.T) {
			result := GetPrefixForRig(tmpDir, tc.warband)
			if result != tc.expected {
				t.Errorf("GetPrefixForRig(%q, %q) = %q, want %q", tmpDir, tc.warband, result, tc.expected)
			}
		})
	}
}

func TestGetPrefixForRig_NoRoutesFile(t *testing.T) {
	tmpDir := t.TempDir()
	// No routes.jsonl file

	result := GetPrefixForRig(tmpDir, "anything")
	if result != "hd" {
		t.Errorf("Expected default 'gt' when no routes file, got %q", result)
	}
}

func TestExtractPrefix(t *testing.T) {
	tests := []struct {
		beadID   string
		expected string
	}{
		{"ap-qtsup.16", "ap-"},
		{"hq-cv-abc", "hq-"},
		{"gt-totem-xyz", "hd-"},
		{"bd-123", "bd-"},
		{"", ""},
		{"nohyphen", ""},
		{"-startswithhyphen", ""}, // Leading hyphen = invalid prefix
		{"-", ""},                 // Just hyphen = invalid
		{"a-", "a-"},              // Trailing hyphen is valid
	}

	for _, tc := range tests {
		t.Run(tc.beadID, func(t *testing.T) {
			result := ExtractPrefix(tc.beadID)
			if result != tc.expected {
				t.Errorf("ExtractPrefix(%q) = %q, want %q", tc.beadID, result, tc.expected)
			}
		})
	}
}

func TestGetRigPathForPrefix(t *testing.T) {
	// Create a temporary directory with routes.jsonl
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	routesContent := `{"prefix": "ap-", "path": "ai_platform/warchief/warband"}
{"prefix": "hd-", "path": "horde/warchief/warband"}
{"prefix": "hq-", "path": "."}
`
	if err := os.WriteFile(filepath.Join(relicsDir, "routes.jsonl"), []byte(routesContent), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		prefix   string
		expected string
	}{
		{"ap-", filepath.Join(tmpDir, "ai_platform/warchief/warband")},
		{"hd-", filepath.Join(tmpDir, "horde/warchief/warband")},
		{"hq-", tmpDir}, // Encampment-level relics return townRoot
		{"unknown-", ""}, // Unknown prefix returns empty
		{"", ""},         // Empty prefix returns empty
	}

	for _, tc := range tests {
		t.Run(tc.prefix, func(t *testing.T) {
			result := GetRigPathForPrefix(tmpDir, tc.prefix)
			if result != tc.expected {
				t.Errorf("GetRigPathForPrefix(%q, %q) = %q, want %q", tmpDir, tc.prefix, result, tc.expected)
			}
		})
	}
}

func TestGetRigPathForPrefix_NoRoutesFile(t *testing.T) {
	tmpDir := t.TempDir()
	// No routes.jsonl file

	result := GetRigPathForPrefix(tmpDir, "ap-")
	if result != "" {
		t.Errorf("Expected empty string when no routes file, got %q", result)
	}
}

func TestResolveHookDir(t *testing.T) {
	// Create a temporary directory with routes.jsonl
	tmpDir := t.TempDir()
	relicsDir := filepath.Join(tmpDir, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatal(err)
	}

	routesContent := `{"prefix": "ap-", "path": "ai_platform/warchief/warband"}
{"prefix": "hq-", "path": "."}
`
	if err := os.WriteFile(filepath.Join(relicsDir, "routes.jsonl"), []byte(routesContent), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		beadID      string
		hookWorkDir string
		expected    string
	}{
		{
			name:        "prefix resolution takes precedence over hookWorkDir",
			beadID:      "ap-test",
			hookWorkDir: "/custom/path",
			expected:    filepath.Join(tmpDir, "ai_platform/warchief/warband"),
		},
		{
			name:        "resolves warband path from prefix",
			beadID:      "ap-test",
			hookWorkDir: "",
			expected:    filepath.Join(tmpDir, "ai_platform/warchief/warband"),
		},
		{
			name:        "encampment-level bead returns townRoot",
			beadID:      "hq-test",
			hookWorkDir: "",
			expected:    tmpDir,
		},
		{
			name:        "unknown prefix uses hookWorkDir as fallback",
			beadID:      "xx-unknown",
			hookWorkDir: "/fallback/path",
			expected:    "/fallback/path",
		},
		{
			name:        "unknown prefix without hookWorkDir falls back to townRoot",
			beadID:      "xx-unknown",
			hookWorkDir: "",
			expected:    tmpDir,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ResolveHookDir(tmpDir, tc.beadID, tc.hookWorkDir)
			if result != tc.expected {
				t.Errorf("ResolveHookDir(%q, %q, %q) = %q, want %q",
					tmpDir, tc.beadID, tc.hookWorkDir, result, tc.expected)
			}
		})
	}
}

func TestAgentBeadIDsWithPrefix(t *testing.T) {
	tests := []struct {
		name     string
		fn       func() string
		expected string
	}{
		{"RaiderBeadIDWithPrefix rl relics obsidian",
			func() string { return RaiderBeadIDWithPrefix("rl", "relics", "obsidian") },
			"bd-relics-raider-obsidian"},
		{"RaiderBeadIDWithPrefix hd horde Toast",
			func() string { return RaiderBeadIDWithPrefix("hd", "horde", "Toast") },
			"gt-horde-raider-Toast"},
		{"WitnessBeadIDWithPrefix rl relics",
			func() string { return WitnessBeadIDWithPrefix("rl", "relics") },
			"bd-relics-witness"},
		{"ForgeBeadIDWithPrefix rl relics",
			func() string { return ForgeBeadIDWithPrefix("rl", "relics") },
			"bd-relics-forge"},
		{"CrewBeadIDWithPrefix rl relics max",
			func() string { return CrewBeadIDWithPrefix("rl", "relics", "max") },
			"bd-relics-clan-max"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.fn()
			if result != tc.expected {
				t.Errorf("got %q, want %q", result, tc.expected)
			}
		})
	}
}
