package deps

import "testing"

func TestParseRelicsVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"bd version 0.43.0 (dev: main@3e1378e122c6)", "0.43.0"},
		{"bd version 0.43.0", "0.43.0"},
		{"bd version 1.2.3", "1.2.3"},
		{"bd version 10.20.30 (release)", "10.20.30"},
		{"some other output", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := parseRelicsVersion(tt.input)
		if result != tt.expected {
			t.Errorf("parseRelicsVersion(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"0.43.0", "0.43.0", 0},
		{"0.43.0", "0.42.0", 1},
		{"0.42.0", "0.43.0", -1},
		{"1.0.0", "0.99.99", 1},
		{"0.43.1", "0.43.0", 1},
		{"0.43.0", "0.43.1", -1},
	}

	for _, tt := range tests {
		result := compareVersions(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestCheckRelics(t *testing.T) {
	// This test depends on whether rl is installed in the test environment
	status, version := CheckRelics()

	// We expect rl to be installed in dev environment
	if status == RelicsNotFound {
		t.Skip("bd not installed, skipping integration test")
	}

	if status == RelicsOK && version == "" {
		t.Error("CheckRelics returned RelicsOK but empty version")
	}

	t.Logf("CheckRelics: status=%d, version=%s", status, version)
}
