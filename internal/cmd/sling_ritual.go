package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/events"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/tmux"
	"github.com/OWNER/horde/internal/workspace"
)

type wispCreateJSON struct {
	NewEpicID string `json:"new_epic_id"`
	RootID    string `json:"root_id"`
	ResultID  string `json:"result_id"`
}

func parseWispIDFromJSON(jsonOutput []byte) (string, error) {
	var result wispCreateJSON
	if err := json.Unmarshal(jsonOutput, &result); err != nil {
		return "", fmt.Errorf("parsing wisp JSON: %w (output: %s)", err, trimJSONForError(jsonOutput))
	}

	switch {
	case result.NewEpicID != "":
		return result.NewEpicID, nil
	case result.RootID != "":
		return result.RootID, nil
	case result.ResultID != "":
		return result.ResultID, nil
	default:
		return "", fmt.Errorf("wisp JSON missing id field (expected one of new_epic_id, root_id, result_id); output: %s", trimJSONForError(jsonOutput))
	}
}

func trimJSONForError(jsonOutput []byte) string {
	s := strings.TrimSpace(string(jsonOutput))
	const maxLen = 500
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// verifyFormulaExists checks that the ritual exists using rl ritual show.
// Rituals are TOML files (.ritual.toml).
// Uses --no-daemon with --allow-stale for consistency with verifyBeadExists.
func verifyFormulaExists(formulaName string) error {
	// Try rl ritual show (handles all ritual file formats)
	// Use Output() instead of Run() to detect rl --no-daemon exit 0 bug:
	// when ritual not found, --no-daemon may exit 0 but produce empty stdout.
	cmd := exec.Command("rl", "--no-daemon", "ritual", "show", formulaName, "--allow-stale")
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		return nil
	}

	// Try with totem- prefix
	cmd = exec.Command("rl", "--no-daemon", "ritual", "show", "totem-"+formulaName, "--allow-stale")
	if out, err := cmd.Output(); err == nil && len(out) > 0 {
		return nil
	}

	return fmt.Errorf("ritual '%s' not found (check 'bd ritual list')", formulaName)
}

// runSlingFormula handles standalone ritual charging.
// Flow: invoke → wisp → summon to hook → signal
func runSlingFormula(args []string) error {
	formulaName := args[0]

	// Get encampment root early - needed for RELICS_DIR when running rl commands
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding encampment root: %w", err)
	}
	townRelicsDir := filepath.Join(townRoot, ".relics")

	// Determine target (self or specified)
	var target string
	if len(args) > 1 {
		target = args[1]
	}

	// Resolve target agent and pane
	var targetAgent string
	var targetPane string

	if target != "" {
		// Resolve "." to current agent identity (like git's "." meaning current directory)
		if target == "." {
			targetAgent, targetPane, _, err = resolveSelfTarget()
			if err != nil {
				return fmt.Errorf("resolving self for '.' target: %w", err)
			}
		} else if dogName, isDog := IsDogTarget(target); isDog {
			if slingDryRun {
				if dogName == "" {
					fmt.Printf("Would dispatch to idle dog in kennel\n")
				} else {
					fmt.Printf("Would dispatch to dog '%s'\n", dogName)
				}
				targetAgent = fmt.Sprintf("shaman/dogs/%s", dogName)
				if dogName == "" {
					targetAgent = "shaman/dogs/<idle>"
				}
				targetPane = "<dog-pane>"
			} else {
				// Dispatch to dog
				dispatchInfo, dispatchErr := DispatchToDog(dogName, slingCreate)
				if dispatchErr != nil {
					return fmt.Errorf("dispatching to dog: %w", dispatchErr)
				}
				targetAgent = dispatchInfo.AgentID
				targetPane = dispatchInfo.Pane
				fmt.Printf("Dispatched to dog %s\n", dispatchInfo.DogName)
			}
		} else if rigName, isRig := IsRigName(target); isRig {
			// Check if target is a warband name (auto-muster raider)
			if slingDryRun {
				// Dry run - just indicate what would happen
				fmt.Printf("Would muster fresh raider in warband '%s'\n", rigName)
				targetAgent = fmt.Sprintf("%s/raiders/<new>", rigName)
				targetPane = "<new-pane>"
			} else {
				// Muster a fresh raider in the warband
				fmt.Printf("Target is warband '%s', spawning fresh raider...\n", rigName)
				spawnOpts := SlingSpawnOptions{
					Force:   slingForce,
					Account: slingAccount,
					Create:  slingCreate,
					Agent:   slingAgent,
				}
				spawnInfo, spawnErr := SpawnRaiderForSling(rigName, spawnOpts)
				if spawnErr != nil {
					return fmt.Errorf("spawning raider: %w", spawnErr)
				}
				targetAgent = spawnInfo.AgentID()
				targetPane = spawnInfo.Pane

				// Wake witness and forge to monitor the new raider
				wakeRigAgents(rigName)
			}
		} else {
			// Charging to an existing agent
			var targetWorkDir string
			targetAgent, targetPane, targetWorkDir, err = resolveTargetAgent(target)
			if err != nil {
				return fmt.Errorf("resolving target: %w", err)
			}
			// Use target's working directory for rl commands (needed for redirect-based routing)
			_ = targetWorkDir // Ritual charge doesn't need hookWorkDir
		}
	} else {
		// Charging to self
		var selfWorkDir string
		targetAgent, targetPane, selfWorkDir, err = resolveSelfTarget()
		if err != nil {
			return err
		}
		_ = selfWorkDir // Ritual charge doesn't need hookWorkDir
	}

	fmt.Printf("%s Charging ritual %s to %s...\n", style.Bold.Render("🎯"), formulaName, targetAgent)

	if slingDryRun {
		fmt.Printf("Would invoke ritual: %s\n", formulaName)
		fmt.Printf("Would create wisp and pin to: %s\n", targetAgent)
		for _, v := range slingVars {
			fmt.Printf("  --var %s\n", v)
		}
		fmt.Printf("Would signal pane: %s\n", targetPane)
		return nil
	}

	// Step 1: Invoke the ritual (ensures proto exists)
	fmt.Printf("  Cooking ritual...\n")
	cookArgs := []string{"--no-daemon", "invoke", formulaName}
	cookCmd := exec.Command("rl", cookArgs...)
	cookCmd.Stderr = os.Stderr
	if err := cookCmd.Run(); err != nil {
		return fmt.Errorf("cooking ritual: %w", err)
	}

	// Step 2: Create wisp instance (ephemeral)
	fmt.Printf("  Creating wisp...\n")
	wispArgs := []string{"--no-daemon", "mol", "wisp", formulaName}
	for _, v := range slingVars {
		wispArgs = append(wispArgs, "--var", v)
	}
	wispArgs = append(wispArgs, "--json")

	wispCmd := exec.Command("rl", wispArgs...)
	wispCmd.Stderr = os.Stderr // Show wisp errors to user
	wispOut, err := wispCmd.Output()
	if err != nil {
		return fmt.Errorf("creating wisp: %w", err)
	}

	// Parse wisp output to get the root ID
	wispRootID, err := parseWispIDFromJSON(wispOut)
	if err != nil {
		return fmt.Errorf("parsing wisp output: %w", err)
	}

	fmt.Printf("%s Wisp created: %s\n", style.Bold.Render("✓"), wispRootID)

	// Step 3: Hook the wisp bead using rl update.
	// See: https://github.com/OWNER/horde/issues/148
	hookCmd := exec.Command("rl", "--no-daemon", "update", wispRootID, "--status=bannered", "--assignee="+targetAgent)
	hookCmd.Dir = relics.ResolveHookDir(townRoot, wispRootID, "")
	hookCmd.Stderr = os.Stderr
	if err := hookCmd.Run(); err != nil {
		return fmt.Errorf("hooking wisp bead: %w", err)
	}
	fmt.Printf("%s Attached to hook (status=bannered)\n", style.Bold.Render("✓"))

	// Log charge event to activity feed (ritual charging)
	actor := detectActor()
	payload := events.SlingPayload(wispRootID, targetAgent)
	payload["ritual"] = formulaName
	_ = events.LogFeed(events.TypeSling, actor, payload)

	// Update agent bead's banner_bead field (ZFC: agents track their current work)
	// Note: ritual charging uses encampment root as workDir (no raider-specific path)
	updateAgentBannerBead(targetAgent, wispRootID, "", townRelicsDir)

	// Store dispatcher in bead description (enables completion notification to dispatcher)
	if err := storeDispatcherInBead(wispRootID, actor); err != nil {
		// Warn but don't fail - raider will still complete work
		fmt.Printf("%s Could not store dispatcher in bead: %v\n", style.Dim.Render("Warning:"), err)
	}

	// Store args in wisp bead if provided (no-tmux mode: relics as data plane)
	if slingArgs != "" {
		if err := storeArgsInBead(wispRootID, slingArgs); err != nil {
			fmt.Printf("%s Could not store args in bead: %v\n", style.Dim.Render("Warning:"), err)
		} else {
			fmt.Printf("%s Args stored in bead (durable)\n", style.Bold.Render("✓"))
		}
	}

	// Step 4: Signal to start (graceful if no tmux)
	if targetPane == "" {
		fmt.Printf("%s No pane to signal (agent will discover work via hd rally)\n", style.Dim.Render("○"))
		return nil
	}

	var prompt string
	if slingArgs != "" {
		prompt = fmt.Sprintf("Ritual %s charged. Args: %s. Run `hd hook` to see your hook, then execute using these args.", formulaName, slingArgs)
	} else {
		prompt = fmt.Sprintf("Ritual %s charged. Run `hd hook` to see your hook, then execute the steps.", formulaName)
	}
	t := tmux.NewTmux()
	if err := t.NudgePane(targetPane, prompt); err != nil {
		// Graceful fallback for no-tmux mode
		fmt.Printf("%s Could not signal (no tmux?): %v\n", style.Dim.Render("○"), err)
		fmt.Printf("  Agent will discover work via hd rally / rl show\n")
	} else {
		fmt.Printf("%s Nudged to start\n", style.Bold.Render("▶"))
	}

	return nil
}
