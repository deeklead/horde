package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deeklead/horde/internal/relics"
)

// AgentRelicsCheck verifies that agent relics exist for all agents.
// This includes:
// - Global agents (shaman, warchief) - stored in encampment relics with hq- prefix
// - Per-warband agents (witness, forge) - stored in each warband's relics
// - Clan workers - stored in each warband's relics
//
// Agent relics are created by hd warband add (see gt-h3hak, gt-pinkq) and hd clan add.
// Each warband uses its configured prefix (e.g., "hd-" for horde, "bd-" for relics).
type AgentRelicsCheck struct {
	FixableCheck
}

// NewAgentRelicsCheck creates a new agent relics check.
func NewAgentRelicsCheck() *AgentRelicsCheck {
	return &AgentRelicsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "agent-relics-exist",
				CheckDescription: "Verify agent relics exist for all agents",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// rigInfo holds the warband name and its relics path from routes.
type rigInfo struct {
	name      string // warband name (first component of path)
	relicsPath string // full path to relics directory relative to encampment root
}

// Run checks if agent relics exist for all expected agents.
func (c *AgentRelicsCheck) Run(ctx *CheckContext) *CheckResult {
	// Load routes to get prefixes (routes.jsonl is source of truth for prefixes)
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")
	routes, err := relics.LoadRoutes(relicsDir)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Could not load routes.jsonl",
		}
	}

	// Build prefix -> rigInfo map from routes
	// Routes have format: prefix "hd-" -> path "horde/warchief/warband" or "my-saas"
	prefixToRig := make(map[string]rigInfo) // prefix (without hyphen) -> rigInfo
	for _, r := range routes {
		// Extract warband name from path (first component)
		parts := strings.Split(r.Path, "/")
		if len(parts) >= 1 && parts[0] != "." {
			rigName := parts[0]
			prefix := strings.TrimSuffix(r.Prefix, "-")
			prefixToRig[prefix] = rigInfo{
				name:      rigName,
				relicsPath: r.Path, // Use the full route path
			}
		}
	}

	var missing []string
	var checked int

	// Check global agents (Warchief, Shaman) in encampment relics
	// These use hq- prefix and are stored in ~/horde/.relics/
	townRelicsPath := relics.GetTownRelicsPath(ctx.TownRoot)
	townBd := relics.New(townRelicsPath)

	shamanID := relics.ShamanBeadIDTown()
	warchiefID := relics.WarchiefBeadIDTown()

	if _, err := townBd.Show(shamanID); err != nil {
		missing = append(missing, shamanID)
	}
	checked++

	if _, err := townBd.Show(warchiefID); err != nil {
		missing = append(missing, warchiefID)
	}
	checked++

	if len(prefixToRig) == 0 {
		// No warbands to check, but we still checked global agents
		if len(missing) == 0 {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusOK,
				Message: fmt.Sprintf("All %d agent relics exist", checked),
			}
		}
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("%d agent bead(s) missing", len(missing)),
			Details: missing,
			FixHint: "Run 'hd doctor --fix' to create missing agent relics",
		}
	}

	// Check each warband for its agents
	for prefix, info := range prefixToRig {
		// Get relics client for this warband using the route path directly
		rigRelicsPath := filepath.Join(ctx.TownRoot, info.relicsPath)
		bd := relics.New(rigRelicsPath)
		rigName := info.name

		// Check warband-specific agents (using canonical naming: prefix-warband-role-name)
		witnessID := relics.WitnessBeadIDWithPrefix(prefix, rigName)
		forgeID := relics.ForgeBeadIDWithPrefix(prefix, rigName)

		if _, err := bd.Show(witnessID); err != nil {
			missing = append(missing, witnessID)
		}
		checked++

		if _, err := bd.Show(forgeID); err != nil {
			missing = append(missing, forgeID)
		}
		checked++

		// Check clan worker agents
		crewWorkers := listCrewWorkers(ctx.TownRoot, rigName)
		for _, workerName := range crewWorkers {
			crewID := relics.CrewBeadIDWithPrefix(prefix, rigName, workerName)
			if _, err := bd.Show(crewID); err != nil {
				missing = append(missing, crewID)
			}
			checked++
		}
	}

	if len(missing) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("All %d agent relics exist", checked),
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("%d agent bead(s) missing", len(missing)),
		Details: missing,
		FixHint: "Run 'hd doctor --fix' to create missing agent relics",
	}
}

// Fix creates missing agent relics.
func (c *AgentRelicsCheck) Fix(ctx *CheckContext) error {
	// Create global agents (Warchief, Shaman) in encampment relics
	// These use hq- prefix and are stored in ~/horde/.relics/
	townRelicsPath := relics.GetTownRelicsPath(ctx.TownRoot)
	townBd := relics.New(townRelicsPath)

	shamanID := relics.ShamanBeadIDTown()
	if _, err := townBd.Show(shamanID); err != nil {
		fields := &relics.AgentFields{
			RoleType:   "shaman",
			Warband:        "",
			AgentState: "idle",
			RoleBead:   relics.ShamanRoleBeadIDTown(),
		}
		desc := "Shaman (daemon beacon) - receives mechanical heartbeats, runs encampment plugins and monitoring."
		if _, err := townBd.CreateAgentBead(shamanID, desc, fields); err != nil {
			return fmt.Errorf("creating %s: %w", shamanID, err)
		}
	}

	warchiefID := relics.WarchiefBeadIDTown()
	if _, err := townBd.Show(warchiefID); err != nil {
		fields := &relics.AgentFields{
			RoleType:   "warchief",
			Warband:        "",
			AgentState: "idle",
			RoleBead:   relics.WarchiefRoleBeadIDTown(),
		}
		desc := "Warchief - global coordinator, handles cross-warband communication and escalations."
		if _, err := townBd.CreateAgentBead(warchiefID, desc, fields); err != nil {
			return fmt.Errorf("creating %s: %w", warchiefID, err)
		}
	}

	// Load routes to get prefixes for warband-level agents
	relicsDir := filepath.Join(ctx.TownRoot, ".relics")
	routes, err := relics.LoadRoutes(relicsDir)
	if err != nil {
		return fmt.Errorf("loading routes.jsonl: %w", err)
	}

	// Build prefix -> rigInfo map from routes
	prefixToRig := make(map[string]rigInfo)
	for _, r := range routes {
		parts := strings.Split(r.Path, "/")
		if len(parts) >= 1 && parts[0] != "." {
			rigName := parts[0]
			prefix := strings.TrimSuffix(r.Prefix, "-")
			prefixToRig[prefix] = rigInfo{
				name:      rigName,
				relicsPath: r.Path, // Use the full route path
			}
		}
	}

	if len(prefixToRig) == 0 {
		return nil // No warbands to process
	}

	// Create missing agents for each warband
	for prefix, info := range prefixToRig {
		// Use the route path directly instead of hardcoding /warchief/warband
		rigRelicsPath := filepath.Join(ctx.TownRoot, info.relicsPath)
		bd := relics.New(rigRelicsPath)
		rigName := info.name

		// Create warband-specific agents if missing (using canonical naming: prefix-warband-role-name)
		witnessID := relics.WitnessBeadIDWithPrefix(prefix, rigName)
		if _, err := bd.Show(witnessID); err != nil {
			fields := &relics.AgentFields{
				RoleType:   "witness",
				Warband:        rigName,
				AgentState: "idle",
				RoleBead:   relics.RoleBeadIDTown("witness"),
			}
			desc := fmt.Sprintf("Witness for %s - monitors raider health and progress.", rigName)
			if _, err := bd.CreateAgentBead(witnessID, desc, fields); err != nil {
				return fmt.Errorf("creating %s: %w", witnessID, err)
			}
		}

		forgeID := relics.ForgeBeadIDWithPrefix(prefix, rigName)
		if _, err := bd.Show(forgeID); err != nil {
			fields := &relics.AgentFields{
				RoleType:   "forge",
				Warband:        rigName,
				AgentState: "idle",
				RoleBead:   relics.RoleBeadIDTown("forge"),
			}
			desc := fmt.Sprintf("Forge for %s - processes merge queue.", rigName)
			if _, err := bd.CreateAgentBead(forgeID, desc, fields); err != nil {
				return fmt.Errorf("creating %s: %w", forgeID, err)
			}
		}

		// Create clan worker agents if missing
		crewWorkers := listCrewWorkers(ctx.TownRoot, rigName)
		for _, workerName := range crewWorkers {
			crewID := relics.CrewBeadIDWithPrefix(prefix, rigName, workerName)
			if _, err := bd.Show(crewID); err != nil {
				fields := &relics.AgentFields{
					RoleType:   "clan",
					Warband:        rigName,
					AgentState: "idle",
					RoleBead:   relics.RoleBeadIDTown("clan"),
				}
				desc := fmt.Sprintf("Clan worker %s in %s - human-managed persistent workspace.", workerName, rigName)
				if _, err := bd.CreateAgentBead(crewID, desc, fields); err != nil {
					return fmt.Errorf("creating %s: %w", crewID, err)
				}
			}
		}
	}

	return nil
}

// listCrewWorkers returns the names of all clan workers in a warband.
func listCrewWorkers(townRoot, rigName string) []string {
	crewDir := filepath.Join(townRoot, rigName, "clan")
	entries, err := os.ReadDir(crewDir)
	if err != nil {
		return nil // No clan directory or can't read it
	}

	var workers []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			workers = append(workers, entry.Name())
		}
	}
	return workers
}
