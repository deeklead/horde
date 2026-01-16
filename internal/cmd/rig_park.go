package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/forge"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/wisp"
	"github.com/deeklead/horde/internal/witness"
)

// RigStatusKey is the wisp config key for warband operational status.
const RigStatusKey = "status"

// RigStatusParked is the value indicating a warband is parked.
const RigStatusParked = "parked"

var rigParkCmd = &cobra.Command{
	Use:   "park <warband>...",
	Short: "Park one or more warbands (stops agents, daemon won't auto-restart)",
	Long: `Park warbands to temporarily disable them.

Parking a warband:
  - Stops the witness if running
  - Stops the forge if running
  - Sets status=parked in the wisp layer (local/ephemeral)
  - The daemon respects this status and won't auto-restart agents

This is a Level 1 (local/ephemeral) operation:
  - Only affects this encampment
  - Disappears on wisp cleanup
  - Use 'hd warband unpark' to resume normal operation

Examples:
  hd warband park horde
  hd warband park relics horde warchief`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRigPark,
}

var rigUnparkCmd = &cobra.Command{
	Use:   "unpark <warband>...",
	Short: "Unpark one or more warbands (allow daemon to auto-restart agents)",
	Long: `Unpark warbands to resume normal operation.

Unparking a warband:
  - Removes the parked status from the wisp layer
  - Allows the daemon to auto-restart agents
  - Does NOT automatically start agents (use 'hd warband start' for that)

Examples:
  hd warband unpark horde
  hd warband unpark relics horde warchief`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRigUnpark,
}

func init() {
	rigCmd.AddCommand(rigParkCmd)
	rigCmd.AddCommand(rigUnparkCmd)
}

func runRigPark(cmd *cobra.Command, args []string) error {
	var errs []error

	for _, rigName := range args {
		if err := parkOneRig(rigName); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rigName, err))
		}
	}

	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Printf("%s %v\n", style.Error.Render("✗"), err)
		}
		return fmt.Errorf("failed to park %d warband(s)", len(errs))
	}

	return nil
}

func parkOneRig(rigName string) error {
	// Get warband and encampment root
	townRoot, r, err := getRig(rigName)
	if err != nil {
		return err
	}

	fmt.Printf("Parking warband %s...\n", style.Bold.Render(rigName))

	var stoppedAgents []string

	t := tmux.NewTmux()

	// Stop witness if running
	witnessSession := fmt.Sprintf("hd-%s-witness", rigName)
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
	forgeSession := fmt.Sprintf("hd-%s-forge", rigName)
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

	// Set parked status in wisp layer
	wispCfg := wisp.NewConfig(townRoot, rigName)
	if err := wispCfg.Set(RigStatusKey, RigStatusParked); err != nil {
		return fmt.Errorf("setting parked status: %w", err)
	}

	// Output
	fmt.Printf("%s Warband %s parked (local only)\n", style.Success.Render("✓"), rigName)
	for _, msg := range stoppedAgents {
		fmt.Printf("  %s\n", msg)
	}
	fmt.Printf("  Daemon will not auto-restart\n")

	return nil
}

func runRigUnpark(cmd *cobra.Command, args []string) error {
	var errs []error

	for _, rigName := range args {
		if err := unparkOneRig(rigName); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rigName, err))
		}
	}

	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Printf("%s %v\n", style.Error.Render("✗"), err)
		}
		return fmt.Errorf("failed to unpark %d warband(s)", len(errs))
	}

	return nil
}

func unparkOneRig(rigName string) error {
	// Get warband and encampment root
	townRoot, _, err := getRig(rigName)
	if err != nil {
		return err
	}

	// Remove parked status from wisp layer
	wispCfg := wisp.NewConfig(townRoot, rigName)
	if err := wispCfg.Unset(RigStatusKey); err != nil {
		return fmt.Errorf("clearing parked status: %w", err)
	}

	fmt.Printf("%s Warband %s unparked\n", style.Success.Render("✓"), rigName)
	fmt.Printf("  Daemon can now auto-restart agents\n")
	fmt.Printf("  Use '%s' to start agents immediately\n", style.Dim.Render("hd warband start "+rigName))

	return nil
}

// IsRigParked checks if a warband is parked in the wisp layer.
// This function is exported for use by the daemon.
func IsRigParked(townRoot, rigName string) bool {
	wispCfg := wisp.NewConfig(townRoot, rigName)
	return wispCfg.GetString(RigStatusKey) == RigStatusParked
}
