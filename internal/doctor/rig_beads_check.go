package doctor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/warband"
)

// RigRelicsCheck verifies that warband identity relics exist for all warbands.
// Warband identity relics track warband metadata like git URL, prefix, and operational state.
// They are created by hd warband add (see gt-zmznh) but may be missing for legacy warbands.
type RigRelicsCheck struct {
	FixableCheck
}

// NewRigRelicsCheck creates a new warband identity relics check.
func NewRigRelicsCheck() *RigRelicsCheck {
	return &RigRelicsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "warband-relics-exist",
				CheckDescription: "Verify warband identity relics exist for all warbands",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run checks if warband identity relics exist for all warbands.
func (c *RigRelicsCheck) Run(ctx *CheckContext) *CheckResult {
	// Load routes to get warband info
	townRelicsDir := filepath.Join(ctx.TownRoot, ".relics")
	routes, err := relics.LoadRoutes(townRelicsDir)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Could not load routes.jsonl",
		}
	}

	// Build unique warband list from routes
	// Routes have format: prefix "hd-" -> path "horde/warchief/warband"
	rigSet := make(map[string]struct {
		prefix    string
		relicsPath string
	})
	for _, r := range routes {
		// Extract warband name from path (first component)
		parts := strings.Split(r.Path, "/")
		if len(parts) >= 1 && parts[0] != "." {
			rigName := parts[0]
			prefix := strings.TrimSuffix(r.Prefix, "-")
			if _, exists := rigSet[rigName]; !exists {
				rigSet[rigName] = struct {
					prefix    string
					relicsPath string
				}{
					prefix:    prefix,
					relicsPath: r.Path,
				}
			}
		}
	}

	if len(rigSet) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warbands to check",
		}
	}

	var missing []string
	var checked int

	// Check each warband for its identity bead
	for rigName, info := range rigSet {
		rigRelicsPath := filepath.Join(ctx.TownRoot, info.relicsPath)
		bd := relics.New(rigRelicsPath)

		rigBeadID := relics.RigBeadIDWithPrefix(info.prefix, rigName)
		if _, err := bd.Show(rigBeadID); err != nil {
			missing = append(missing, rigBeadID)
		}
		checked++
	}

	if len(missing) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("All %d warband identity relics exist", checked),
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("%d warband identity bead(s) missing", len(missing)),
		Details: missing,
		FixHint: "Run 'hd doctor --fix' to create missing warband identity relics",
	}
}

// Fix creates missing warband identity relics.
func (c *RigRelicsCheck) Fix(ctx *CheckContext) error {
	// Load routes to get warband info
	townRelicsDir := filepath.Join(ctx.TownRoot, ".relics")
	routes, err := relics.LoadRoutes(townRelicsDir)
	if err != nil {
		return fmt.Errorf("loading routes.jsonl: %w", err)
	}

	// Build unique warband list from routes
	rigSet := make(map[string]struct {
		prefix    string
		relicsPath string
	})
	for _, r := range routes {
		parts := strings.Split(r.Path, "/")
		if len(parts) >= 1 && parts[0] != "." {
			rigName := parts[0]
			prefix := strings.TrimSuffix(r.Prefix, "-")
			if _, exists := rigSet[rigName]; !exists {
				rigSet[rigName] = struct {
					prefix    string
					relicsPath string
				}{
					prefix:    prefix,
					relicsPath: r.Path,
				}
			}
		}
	}

	if len(rigSet) == 0 {
		return nil // No warbands to process
	}

	// Create missing warband identity relics
	for rigName, info := range rigSet {
		rigRelicsPath := filepath.Join(ctx.TownRoot, info.relicsPath)
		bd := relics.New(rigRelicsPath)

		rigBeadID := relics.RigBeadIDWithPrefix(info.prefix, rigName)
		if _, err := bd.Show(rigBeadID); err != nil {
			// Bead doesn't exist - create it
			// Try to get git URL from warband config
			rigPath := filepath.Join(ctx.TownRoot, rigName)
			gitURL := ""
			if cfg, err := warband.LoadRigConfig(rigPath); err == nil {
				gitURL = cfg.GitURL
			}

			fields := &relics.RigFields{
				Repo:   gitURL,
				Prefix: info.prefix,
				State:  "active",
			}

			if _, err := bd.CreateRigBead(rigBeadID, rigName, fields); err != nil {
				return fmt.Errorf("creating %s: %w", rigBeadID, err)
			}
		}
	}

	return nil
}
