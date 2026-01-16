package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/style"
)

// runBatchSling handles charging multiple relics to a warband.
// Each bead gets its own freshly spawned raider.
func runBatchSling(beadIDs []string, rigName string, townRelicsDir string) error {
	// Validate all relics exist before spawning any raiders
	for _, beadID := range beadIDs {
		if err := verifyBeadExists(beadID); err != nil {
			return fmt.Errorf("bead '%s' not found", beadID)
		}
	}

	if slingDryRun {
		fmt.Printf("%s Batch charging %d relics to warband '%s':\n", style.Bold.Render("🎯"), len(beadIDs), rigName)
		for _, beadID := range beadIDs {
			fmt.Printf("  Would muster raider for: %s\n", beadID)
		}
		return nil
	}

	fmt.Printf("%s Batch charging %d relics to warband '%s'...\n", style.Bold.Render("🎯"), len(beadIDs), rigName)

	// Track results for summary
	type slingResult struct {
		beadID  string
		raider string
		success bool
		errMsg  string
	}
	results := make([]slingResult, 0, len(beadIDs))

	// Muster a raider for each bead and charge it
	for i, beadID := range beadIDs {
		fmt.Printf("\n[%d/%d] Charging %s...\n", i+1, len(beadIDs), beadID)

		// Check bead status
		info, err := getBeadInfo(beadID)
		if err != nil {
			results = append(results, slingResult{beadID: beadID, success: false, errMsg: err.Error()})
			fmt.Printf("  %s Could not get bead info: %v\n", style.Dim.Render("✗"), err)
			continue
		}

		if info.Status == "pinned" && !slingForce {
			results = append(results, slingResult{beadID: beadID, success: false, errMsg: "already pinned"})
			fmt.Printf("  %s Already pinned (use --force to re-charge)\n", style.Dim.Render("✗"))
			continue
		}

		// Muster a fresh raider
		spawnOpts := SlingSpawnOptions{
			Force:    slingForce,
			Account:  slingAccount,
			Create:   slingCreate,
			BannerBead: beadID, // Set atomically at muster time
			Agent:    slingAgent,
		}
		spawnInfo, err := SpawnRaiderForSling(rigName, spawnOpts)
		if err != nil {
			results = append(results, slingResult{beadID: beadID, success: false, errMsg: err.Error()})
			fmt.Printf("  %s Failed to muster raider: %v\n", style.Dim.Render("✗"), err)
			continue
		}

		targetAgent := spawnInfo.AgentID()
		hookWorkDir := spawnInfo.ClonePath

		// Auto-raid: check if issue is already tracked
		if !slingNoRaid {
			existingRaid := isTrackedByRaid(beadID)
			if existingRaid == "" {
				raidID, err := createAutoRaid(beadID, info.Title)
				if err != nil {
					fmt.Printf("  %s Could not create auto-raid: %v\n", style.Dim.Render("Warning:"), err)
				} else {
					fmt.Printf("  %s Created raid 🚚 %s\n", style.Bold.Render("→"), raidID)
				}
			} else {
				fmt.Printf("  %s Already tracked by raid %s\n", style.Dim.Render("○"), existingRaid)
			}
		}

		// Hook the bead. See: https://github.com/deeklead/horde/issues/148
		townRoot := filepath.Dir(townRelicsDir)
		hookCmd := exec.Command("rl", "--no-daemon", "update", beadID, "--status=bannered", "--assignee="+targetAgent)
		hookCmd.Dir = relics.ResolveHookDir(townRoot, beadID, hookWorkDir)
		hookCmd.Stderr = os.Stderr
		if err := hookCmd.Run(); err != nil {
			results = append(results, slingResult{beadID: beadID, raider: spawnInfo.RaiderName, success: false, errMsg: "hook failed"})
			fmt.Printf("  %s Failed to hook bead: %v\n", style.Dim.Render("✗"), err)
			continue
		}

		fmt.Printf("  %s Work attached to %s\n", style.Bold.Render("✓"), spawnInfo.RaiderName)

		// Log charge event
		actor := detectActor()
		_ = events.LogFeed(events.TypeSling, actor, events.SlingPayload(beadID, targetAgent))

		// Update agent bead state
		updateAgentBannerBead(targetAgent, beadID, hookWorkDir, townRelicsDir)

		// Auto-summon totem-raider-work totem to raider agent bead
		if err := attachRaiderWorkMolecule(targetAgent, hookWorkDir, townRoot); err != nil {
			fmt.Printf("  %s Could not summon work totem: %v\n", style.Dim.Render("Warning:"), err)
		}

		// Store args if provided
		if slingArgs != "" {
			if err := storeArgsInBead(beadID, slingArgs); err != nil {
				fmt.Printf("  %s Could not store args: %v\n", style.Dim.Render("Warning:"), err)
			}
		}

		// Signal the raider
		if spawnInfo.Pane != "" {
			if err := injectStartPrompt(spawnInfo.Pane, beadID, slingSubject, slingArgs); err != nil {
				fmt.Printf("  %s Could not signal (agent will discover via hd rally)\n", style.Dim.Render("○"))
			} else {
				fmt.Printf("  %s Start prompt sent\n", style.Bold.Render("▶"))
			}
		}

		results = append(results, slingResult{beadID: beadID, raider: spawnInfo.RaiderName, success: true})
	}

	// Wake witness and forge once at the end
	wakeRigAgents(rigName)

	// Print summary
	successCount := 0
	for _, r := range results {
		if r.success {
			successCount++
		}
	}

	fmt.Printf("\n%s Batch charge complete: %d/%d succeeded\n", style.Bold.Render("📊"), successCount, len(beadIDs))
	if successCount < len(beadIDs) {
		for _, r := range results {
			if !r.success {
				fmt.Printf("  %s %s: %s\n", style.Dim.Render("✗"), r.beadID, r.errMsg)
			}
		}
	}

	return nil
}
