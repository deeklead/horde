package cmd

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/forge"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/tmux"
	"github.com/OWNER/horde/internal/witness"
)

// RigDockedLabel is the label set on warband identity relics when docked.
const RigDockedLabel = "status:docked"

var rigDockCmd = &cobra.Command{
	Use:   "dock <warband>",
	Short: "Dock a warband (global, persistent shutdown)",
	Long: `Dock a warband to persistently disable it across all clones.

Docking a warband:
  - Stops the witness if running
  - Stops the forge if running
  - Sets status:docked label on the warband identity bead
  - Syncs via git so all clones see the docked status

This is a Level 2 (global/persistent) operation:
  - Affects all clones of this warband (via git sync)
  - Persists until explicitly undocked
  - The daemon respects this status and won't auto-restart agents

Use 'hd warband undock' to resume normal operation.

Examples:
  hd warband dock horde
  hd warband dock relics`,
	Args: cobra.ExactArgs(1),
	RunE: runRigDock,
}

var rigUndockCmd = &cobra.Command{
	Use:   "undock <warband>",
	Short: "Undock a warband (remove global docked status)",
	Long: `Undock a warband to remove the persistent docked status.

Undocking a warband:
  - Removes the status:docked label from the warband identity bead
  - Syncs via git so all clones see the undocked status
  - Allows the daemon to auto-restart agents
  - Does NOT automatically start agents (use 'hd warband start' for that)

Examples:
  hd warband undock horde
  hd warband undock relics`,
	Args: cobra.ExactArgs(1),
	RunE: runRigUndock,
}

func init() {
	rigCmd.AddCommand(rigDockCmd)
	rigCmd.AddCommand(rigUndockCmd)
}

func runRigDock(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	// Get warband
	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	// Get warband prefix for bead ID
	prefix := "hd" // default
	if r.Config != nil && r.Config.Prefix != "" {
		prefix = r.Config.Prefix
	}

	// Find the warband identity bead
	rigBeadID := relics.RigBeadIDWithPrefix(prefix, rigName)
	bd := relics.New(r.RelicsPath())

	// Check if warband bead exists, create if not
	rigBead, err := bd.Show(rigBeadID)
	if err != nil {
		// Warband identity bead doesn't exist (legacy warband) - create it
		fmt.Printf("  Creating warband identity bead %s...\n", rigBeadID)
		rigBead, err = bd.CreateRigBead(rigBeadID, rigName, &relics.RigFields{
			Repo:   r.GitURL,
			Prefix: prefix,
			State:  "active",
		})
		if err != nil {
			return fmt.Errorf("creating warband identity bead: %w", err)
		}
	}

	// Check if already docked
	for _, label := range rigBead.Labels {
		if label == RigDockedLabel {
			fmt.Printf("%s Warband %s is already docked\n", style.Dim.Render("•"), rigName)
			return nil
		}
	}

	fmt.Printf("Docking warband %s...\n", style.Bold.Render(rigName))

	var stoppedAgents []string

	t := tmux.NewTmux()

	// Stop witness if running
	witnessSession := fmt.Sprintf("gt-%s-witness", rigName)
	witnessRunning, _ := t.HasSession(witnessSession)
	if witnessRunning {
		fmt.Printf("  Stopping witness...\n")
		witMgr := witness.NewManager(r)
		if err := witMgr.Stop(); err != nil {
			fmt.Printf("  %s Failed to stop witness: %v\n", style.Warning.Render("!"), err)
		} else {
			stoppedAgents = append(stoppedAgents, "Witness stopped")
		}
	}

	// Stop forge if running
	forgeSession := fmt.Sprintf("gt-%s-forge", rigName)
	forgeRunning, _ := t.HasSession(forgeSession)
	if forgeRunning {
		fmt.Printf("  Stopping forge...\n")
		refMgr := forge.NewManager(r)
		if err := refMgr.Stop(); err != nil {
			fmt.Printf("  %s Failed to stop forge: %v\n", style.Warning.Render("!"), err)
		} else {
			stoppedAgents = append(stoppedAgents, "Forge stopped")
		}
	}

	// Set docked label on warband identity bead
	if err := bd.Update(rigBeadID, relics.UpdateOptions{
		AddLabels: []string{RigDockedLabel},
	}); err != nil {
		return fmt.Errorf("setting docked label: %w", err)
	}

	// Sync relics to propagate to other clones
	fmt.Printf("  Syncing relics...\n")
	syncCmd := exec.Command("rl", "sync")
	syncCmd.Dir = r.RelicsPath()
	if output, err := syncCmd.CombinedOutput(); err != nil {
		fmt.Printf("  %s rl sync warning: %v\n%s", style.Warning.Render("!"), err, string(output))
	}

	// Output
	fmt.Printf("%s Warband %s docked (global)\n", style.Success.Render("✓"), rigName)
	fmt.Printf("  Label added: %s\n", RigDockedLabel)
	for _, msg := range stoppedAgents {
		fmt.Printf("  %s\n", msg)
	}
	fmt.Printf("  Run '%s' to propagate to other clones\n", style.Dim.Render("bd sync"))

	return nil
}

func runRigUndock(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	// Get warband and encampment root
	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	// Get warband prefix for bead ID
	prefix := "hd" // default
	if r.Config != nil && r.Config.Prefix != "" {
		prefix = r.Config.Prefix
	}

	// Find the warband identity bead
	rigBeadID := relics.RigBeadIDWithPrefix(prefix, rigName)
	bd := relics.New(r.RelicsPath())

	// Check if warband bead exists, create if not
	rigBead, err := bd.Show(rigBeadID)
	if err != nil {
		// Warband identity bead doesn't exist (legacy warband) - can't be docked
		fmt.Printf("%s Warband %s has no identity bead and is not docked\n", style.Dim.Render("•"), rigName)
		return nil
	}

	// Check if actually docked
	isDocked := false
	for _, label := range rigBead.Labels {
		if label == RigDockedLabel {
			isDocked = true
			break
		}
	}
	if !isDocked {
		fmt.Printf("%s Warband %s is not docked\n", style.Dim.Render("•"), rigName)
		return nil
	}

	// Remove docked label from warband identity bead
	if err := bd.Update(rigBeadID, relics.UpdateOptions{
		RemoveLabels: []string{RigDockedLabel},
	}); err != nil {
		return fmt.Errorf("removing docked label: %w", err)
	}

	// Sync relics to propagate to other clones
	fmt.Printf("  Syncing relics...\n")
	syncCmd := exec.Command("rl", "sync")
	syncCmd.Dir = r.RelicsPath()
	if output, err := syncCmd.CombinedOutput(); err != nil {
		fmt.Printf("  %s rl sync warning: %v\n%s", style.Warning.Render("!"), err, string(output))
	}

	fmt.Printf("%s Warband %s undocked\n", style.Success.Render("✓"), rigName)
	fmt.Printf("  Label removed: %s\n", RigDockedLabel)
	fmt.Printf("  Daemon can now auto-restart agents\n")
	fmt.Printf("  Use '%s' to start agents immediately\n", style.Dim.Render("hd warband start "+rigName))

	return nil
}

// IsRigDocked checks if a warband is docked by checking for the status:docked label
// on the warband identity bead. This function is exported for use by the daemon.
func IsRigDocked(townRoot, rigName, prefix string) bool {
	// Construct the warband relics path
	rigPath := townRoot + "/" + rigName
	relicsPath := rigPath + "/warchief/warband"
	if _, err := exec.Command("test", "-d", relicsPath).CombinedOutput(); err != nil {
		relicsPath = rigPath
	}

	bd := relics.New(relicsPath)
	rigBeadID := relics.RigBeadIDWithPrefix(prefix, rigName)

	rigBead, err := bd.Show(rigBeadID)
	if err != nil {
		return false
	}

	for _, label := range rigBead.Labels {
		if label == RigDockedLabel {
			return true
		}
	}
	return false
}
