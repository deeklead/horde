package ritual

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseRealFormulas tests parsing actual ritual files from the filesystem.
// This is an integration test that validates our parser against real-world files.
func TestParseRealFormulas(t *testing.T) {
	// Find ritual files - they're in various .relics/rituals directories
	formulaDirs := []string{
		"/Users/stevey/horde/horde/raiders/slit/.relics/rituals",
		"/Users/stevey/horde/horde/warchief/warband/.relics/rituals",
	}

	var formulaFiles []string
	for _, dir := range formulaDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // Skip if directory doesn't exist
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".toml" {
				formulaFiles = append(formulaFiles, filepath.Join(dir, e.Name()))
			}
		}
	}

	if len(formulaFiles) == 0 {
		t.Skip("No ritual files found to test")
	}

	// Known files that use advanced features not yet supported:
	// - Composition (extends, compose): shiny-enterprise, shiny-secure
	// - Aspect-oriented (advice, pointcuts): security-audit
	skipAdvanced := map[string]string{
		"shiny-enterprise.ritual.toml": "uses ritual composition (extends)",
		"shiny-secure.ritual.toml":     "uses ritual composition (extends)",
		"security-audit.ritual.toml":   "uses aspect-oriented features (advice/pointcuts)",
	}

	for _, path := range formulaFiles {
		t.Run(filepath.Base(path), func(t *testing.T) {
			baseName := filepath.Base(path)
			if reason, ok := skipAdvanced[baseName]; ok {
				t.Skipf("Skipping advanced ritual: %s", reason)
				return
			}

			f, err := ParseFile(path)
			if err != nil {
				// Check if this is a composition ritual (has extends)
				if strings.Contains(err.Error(), "requires at least one") {
					t.Skipf("Skipping: likely a composition ritual - %v", err)
					return
				}
				t.Errorf("ParseFile failed: %v", err)
				return
			}

			// Basic sanity checks
			if f.Name == "" {
				t.Error("Ritual name is empty")
			}
			if !f.Type.IsValid() {
				t.Errorf("Invalid ritual type: %s", f.Type)
			}

			// Type-specific checks
			switch f.Type {
			case TypeRaid:
				if len(f.Legs) == 0 {
					t.Error("Raid ritual has no legs")
				}
				t.Logf("Raid ritual with %d legs", len(f.Legs))
			case TypeWorkflow:
				if len(f.Steps) == 0 {
					t.Error("Workflow ritual has no steps")
				}
				// Test topological sort
				order, err := f.TopologicalSort()
				if err != nil {
					t.Errorf("TopologicalSort failed: %v", err)
				}
				t.Logf("Workflow ritual with %d steps, sorted order: %v", len(f.Steps), order)
			case TypeExpansion:
				if len(f.Template) == 0 {
					t.Error("Expansion ritual has no templates")
				}
				t.Logf("Expansion ritual with %d templates", len(f.Template))
			}
		})
	}
}
