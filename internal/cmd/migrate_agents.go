package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/workspace"
)

var (
	migrateAgentsDryRun bool
	migrateAgentsForce  bool
)

var migrateAgentsCmd = &cobra.Command{
	Use:     "migrate-agents",
	GroupID: GroupDiag,
	Short:   "Migrate agent relics to two-level architecture",
	Long: `Migrate agent relics from the old single-tier to the two-level architecture.

This command migrates encampment-level agent relics (Warchief, Shaman) from warband relics
with gt-* prefix to encampment relics with hq-* prefix:

  OLD (warband relics):    gt-warchief, gt-shaman
  NEW (encampment relics):   hq-warchief, hq-shaman

Warband-level agents (Witness, Forge, Raiders) remain in warband relics unchanged.

The migration:
1. Detects old gt-warchief/gt-shaman relics in warband relics
2. Creates new hq-warchief/hq-shaman relics in encampment relics
3. Copies agent state (banner_bead, agent_state, etc.)
4. Adds migration note to old relics (preserves them)

Safety:
- Dry-run mode by default (use --execute to apply changes)
- Old relics are preserved with migration notes
- Validates new relics exist before marking migration complete
- Skips if new relics already exist (idempotent)

Examples:
  hd migrate-agents              # Dry-run: show what would be migrated
  hd migrate-agents --execute    # Apply the migration
  hd migrate-agents --force      # Re-migrate even if new relics exist`,
	RunE: runMigrateAgents,
}

func init() {
	migrateAgentsCmd.Flags().BoolVar(&migrateAgentsDryRun, "dry-run", true, "Show what would be migrated without making changes (default)")
	migrateAgentsCmd.Flags().BoolVar(&migrateAgentsForce, "force", false, "Re-migrate even if new relics already exist")
	// Add --execute as inverse of --dry-run for clarity
	migrateAgentsCmd.Flags().BoolP("execute", "x", false, "Actually apply the migration (opposite of --dry-run)")
	rootCmd.AddCommand(migrateAgentsCmd)
}

// migrationResult holds the result of a single bead migration.
type migrationResult struct {
	OldID      string
	NewID      string
	Status     string // "migrated", "skipped", "error"
	Message    string
	OldFields  *relics.AgentFields
	WasDryRun  bool
}

func runMigrateAgents(cmd *cobra.Command, args []string) error {
	// Handle --execute flag
	if execute, _ := cmd.Flags().GetBool("execute"); execute {
		migrateAgentsDryRun = false
	}

	// Find encampment root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Get encampment relics path
	townRelicsDir := filepath.Join(townRoot, ".relics")

	// Load routes to find warband relics
	routes, err := relics.LoadRoutes(townRelicsDir)
	if err != nil {
		return fmt.Errorf("loading routes.jsonl: %w", err)
	}

	// Find the first warband with gt- prefix (where global agents are currently stored)
	var sourceRigPath string
	for _, r := range routes {
		if strings.TrimSuffix(r.Prefix, "-") == "hd" && r.Path != "." {
			sourceRigPath = r.Path
			break
		}
	}

	if sourceRigPath == "" {
		fmt.Println("No warband with gt- prefix found. Nothing to migrate.")
		return nil
	}

	// Source relics (warband relics where old agent relics are)
	sourceRelicsDir := filepath.Join(townRoot, sourceRigPath, ".relics")
	sourceBd := relics.New(sourceRelicsDir)

	// Target relics (encampment relics where new agent relics should go)
	targetBd := relics.NewWithRelicsDir(townRoot, townRelicsDir)

	// Agents to migrate: encampment-level agents only
	agentsToMigrate := []struct {
		oldID   string
		newID   string
		desc    string
	}{
		{
			oldID: relics.WarchiefBeadID(),  // gt-warchief
			newID: relics.WarchiefBeadIDTown(), // hq-warchief
			desc:  "Warchief - global coordinator, handles cross-warband communication and escalations.",
		},
		{
			oldID: relics.ShamanBeadID(),  // gt-shaman
			newID: relics.ShamanBeadIDTown(), // hq-shaman
			desc:  "Shaman (daemon beacon) - receives mechanical heartbeats, runs encampment plugins and monitoring.",
		},
	}

	// Also migrate role relics
	rolesToMigrate := []string{"warchief", "shaman", "witness", "forge", "raider", "clan", "dog"}

	if migrateAgentsDryRun {
		fmt.Println("🔍 DRY RUN: Showing what would be migrated")
		fmt.Println("   Use --execute to apply changes")
		fmt.Println()
	} else {
		fmt.Println("🚀 Migrating agent relics to two-level architecture")
		fmt.Println()
	}

	var results []migrationResult

	// Migrate agent relics
	fmt.Println("Agent Relics:")
	for _, agent := range agentsToMigrate {
		result := migrateAgentBead(sourceBd, targetBd, agent.oldID, agent.newID, agent.desc, migrateAgentsDryRun, migrateAgentsForce)
		results = append(results, result)
		printMigrationResult(result)
	}

	// Migrate role relics
	fmt.Println("\nRole Relics:")
	for _, role := range rolesToMigrate {
		oldID := "hd-" + role + "-role"
		newID := relics.RoleBeadIDTown(role) // hq-<role>-role
		result := migrateRoleBead(sourceBd, targetBd, oldID, newID, role, migrateAgentsDryRun, migrateAgentsForce)
		results = append(results, result)
		printMigrationResult(result)
	}

	// Summary
	fmt.Println()
	printMigrationSummary(results, migrateAgentsDryRun)

	return nil
}

// migrateAgentBead migrates a single agent bead from source to target.
func migrateAgentBead(sourceBd, targetBd *relics.Relics, oldID, newID, desc string, dryRun, force bool) migrationResult {
	result := migrationResult{
		OldID:     oldID,
		NewID:     newID,
		WasDryRun: dryRun,
	}

	// Check if old bead exists
	oldIssue, oldFields, err := sourceBd.GetAgentBead(oldID)
	if err != nil {
		result.Status = "skipped"
		result.Message = "old bead not found"
		return result
	}
	result.OldFields = oldFields

	// Check if new bead already exists
	if _, err := targetBd.Show(newID); err == nil {
		if !force {
			result.Status = "skipped"
			result.Message = "new bead already exists (use --force to re-migrate)"
			return result
		}
	}

	if dryRun {
		result.Status = "would migrate"
		result.Message = fmt.Sprintf("would copy state from %s", oldIssue.ID)
		return result
	}

	// Create new bead in encampment relics
	newFields := &relics.AgentFields{
		RoleType:          oldFields.RoleType,
		Warband:               oldFields.Warband,
		AgentState:        oldFields.AgentState,
		BannerBead:          oldFields.BannerBead,
		RoleBead:          relics.RoleBeadIDTown(oldFields.RoleType), // Update to hq- role
		CleanupStatus:     oldFields.CleanupStatus,
		ActiveMR:          oldFields.ActiveMR,
		NotificationLevel: oldFields.NotificationLevel,
	}

	_, err = targetBd.CreateAgentBead(newID, desc, newFields)
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("failed to create: %v", err)
		return result
	}

	// Add migration label to old bead
	migrationLabel := fmt.Sprintf("migrated-to:%s", newID)
	if err := sourceBd.Update(oldID, relics.UpdateOptions{AddLabels: []string{migrationLabel}}); err != nil {
		// Non-fatal: just log it
		result.Message = fmt.Sprintf("created but couldn't add migration label: %v", err)
	}

	result.Status = "migrated"
	result.Message = "successfully migrated"
	return result
}

// migrateRoleBead migrates a role definition bead.
func migrateRoleBead(sourceBd, targetBd *relics.Relics, oldID, newID, role string, dryRun, force bool) migrationResult {
	result := migrationResult{
		OldID:     oldID,
		NewID:     newID,
		WasDryRun: dryRun,
	}

	// Check if old bead exists
	oldIssue, err := sourceBd.Show(oldID)
	if err != nil {
		result.Status = "skipped"
		result.Message = "old bead not found"
		return result
	}

	// Check if new bead already exists
	if _, err := targetBd.Show(newID); err == nil {
		if !force {
			result.Status = "skipped"
			result.Message = "new bead already exists (use --force to re-migrate)"
			return result
		}
	}

	if dryRun {
		result.Status = "would migrate"
		result.Message = fmt.Sprintf("would copy from %s", oldIssue.ID)
		return result
	}

	// Create new role bead in encampment relics
	// Role relics are simple - just copy the description
	_, err = targetBd.CreateWithID(newID, relics.CreateOptions{
		Title:       fmt.Sprintf("Role: %s", role),
		Type:        "role",
		Description: oldIssue.Title, // Use old title as description
	})
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("failed to create: %v", err)
		return result
	}

	// Add migration label to old bead
	migrationLabel := fmt.Sprintf("migrated-to:%s", newID)
	if err := sourceBd.Update(oldID, relics.UpdateOptions{AddLabels: []string{migrationLabel}}); err != nil {
		// Non-fatal
		result.Message = fmt.Sprintf("created but couldn't add migration label: %v", err)
	}

	result.Status = "migrated"
	result.Message = "successfully migrated"
	return result
}

func printMigrationResult(r migrationResult) {
	var icon string
	switch r.Status {
	case "migrated", "would migrate":
		icon = "  ✓"
	case "skipped":
		icon = "  ⊘"
	case "error":
		icon = "  ✗"
	}
	fmt.Printf("%s %s → %s: %s\n", icon, r.OldID, r.NewID, r.Message)
}

func printMigrationSummary(results []migrationResult, dryRun bool) {
	var migrated, skipped, errors int
	for _, r := range results {
		switch r.Status {
		case "migrated", "would migrate":
			migrated++
		case "skipped":
			skipped++
		case "error":
			errors++
		}
	}

	if dryRun {
		fmt.Printf("Summary (dry-run): %d would migrate, %d skipped, %d errors\n", migrated, skipped, errors)
		if migrated > 0 {
			fmt.Println("\nRun with --execute to apply these changes.")
		}
	} else {
		fmt.Printf("Summary: %d migrated, %d skipped, %d errors\n", migrated, skipped, errors)
	}
}
