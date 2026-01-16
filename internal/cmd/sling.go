package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

var slingCmd = &cobra.Command{
	Use:     "charge <bead-or-ritual> [target]",
	GroupID: GroupWork,
	Short:   "Assign work to an agent (THE unified work dispatch command)",
	Long: `Charge work onto an agent's hook and start working immediately.

This is THE command for assigning work in Horde. It handles:
  - Existing agents (warchief, clan, witness, forge)
  - Auto-spawning raiders when target is a warband
  - Dispatching to dogs (Shaman's helper workers)
  - Ritual instantiation and wisp creation
  - Auto-raid creation for warmap visibility

Auto-Raid:
  When charging a single issue (not a ritual), charge automatically creates
  a raid to track the work unless --no-raid is specified. This ensures
  all work appears in 'hd raid list', even "swarm of one" assignments.

  hd charge gt-abc horde              # Creates "Work: <issue-title>" raid
  hd charge gt-abc horde --no-raid  # Skip auto-raid creation

Target Resolution:
  hd charge gt-abc                       # Self (current agent)
  hd charge gt-abc clan                  # Clan worker in current warband
  hd charge gp-abc greenplace               # Auto-muster raider in warband
  hd charge gt-abc greenplace/Toast         # Specific raider
  hd charge gt-abc warchief                 # Warchief
  hd charge gt-abc shaman/dogs           # Auto-dispatch to idle dog
  hd charge gt-abc shaman/dogs/alpha     # Specific dog

Spawning Options (when target is a warband):
  hd charge gp-abc greenplace --create               # Create raider if missing
  hd charge gp-abc greenplace --force                # Ignore unread drums
  hd charge gp-abc greenplace --account work         # Use specific Claude account

Natural Language Args:
  hd charge gt-abc --args "patch release"
  hd charge code-review --args "focus on security"

The --args string is stored in the bead and shown via hd rally. Since the
executor is an LLM, it interprets these instructions naturally.

Ritual Charging:
  hd charge totem-release warchief/           # Invoke + wisp + summon + signal
  hd charge towers-of-hanoi --var disks=3

Ritual-on-Bead (--on flag):
  hd charge totem-review --on gt-abc       # Apply ritual to existing work
  hd charge shiny --on gt-abc clan       # Apply ritual, charge to clan

Compare:
  hd hook <bead>      # Just summon (no action)
  hd charge <bead>     # Summon + start now (keep context)
  hd handoff <bead>   # Summon + restart (fresh context)

The propulsion principle: if it's on your hook, YOU RUN IT.

Batch Charging:
  hd charge gt-abc gt-def gt-ghi horde   # Charge multiple relics to a warband

  When multiple relics are provided with a warband target, each bead gets its own
  raider. This parallelizes work dispatch without running hd charge N times.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSling,
}

var (
	slingSubject  string
	slingMessage  string
	slingDryRun   bool
	slingOnTarget string   // --on flag: target bead when charging a ritual
	slingVars     []string // --var flag: ritual variables (key=value)
	slingArgs     string   // --args flag: natural language instructions for executor

	// Flags migrated for raider spawning (used by charge for work assignment)
	slingCreate   bool   // --create: create raider if it doesn't exist
	slingForce    bool   // --force: force muster even if raider has unread drums
	slingAccount  string // --account: Claude Code account handle to use
	slingAgent    string // --agent: override runtime agent for this charge/muster
	slingNoRaid bool   // --no-raid: skip auto-raid creation
)

func init() {
	slingCmd.Flags().StringVarP(&slingSubject, "subject", "s", "", "Context subject for the work")
	slingCmd.Flags().StringVarP(&slingMessage, "message", "m", "", "Context message for the work")
	slingCmd.Flags().BoolVarP(&slingDryRun, "dry-run", "n", false, "Show what would be done")
	slingCmd.Flags().StringVar(&slingOnTarget, "on", "", "Apply ritual to existing bead (implies wisp scaffolding)")
	slingCmd.Flags().StringArrayVar(&slingVars, "var", nil, "Ritual variable (key=value), can be repeated")
	slingCmd.Flags().StringVarP(&slingArgs, "args", "a", "", "Natural language instructions for the executor (e.g., 'patch release')")

	// Flags for raider spawning (when target is a warband)
	slingCmd.Flags().BoolVar(&slingCreate, "create", false, "Create raider if it doesn't exist")
	slingCmd.Flags().BoolVar(&slingForce, "force", false, "Force muster even if raider has unread drums")
	slingCmd.Flags().StringVar(&slingAccount, "account", "", "Claude Code account handle to use")
	slingCmd.Flags().StringVar(&slingAgent, "agent", "", "Override agent/runtime for this charge (e.g., claude, gemini, codex, or custom alias)")
	slingCmd.Flags().BoolVar(&slingNoRaid, "no-raid", false, "Skip auto-raid creation for single-issue charge")

	rootCmd.AddCommand(slingCmd)
}

func runSling(cmd *cobra.Command, args []string) error {
	// Raiders cannot charge - check early before writing anything
	if raiderName := os.Getenv("HD_RAIDER"); raiderName != "" {
		return fmt.Errorf("raiders cannot charge (use hd done for handoff)")
	}

	// Get encampment root early - needed for RELICS_DIR when running rl commands
	// This ensures hq-* relics are accessible even when running from raider worktree
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding encampment root: %w", err)
	}
	townRelicsDir := filepath.Join(townRoot, ".relics")

	// --var is only for standalone ritual mode, not ritual-on-bead mode
	if slingOnTarget != "" && len(slingVars) > 0 {
		return fmt.Errorf("--var cannot be used with --on (ritual-on-bead mode doesn't support variables)")
	}

	// Batch mode detection: multiple relics with warband target
	// Pattern: hd charge gt-abc gt-def gt-ghi horde
	// When len(args) > 2 and last arg is a warband, charge each bead to its own raider
	if len(args) > 2 {
		lastArg := args[len(args)-1]
		if rigName, isRig := IsRigName(lastArg); isRig {
			return runBatchSling(args[:len(args)-1], rigName, townRelicsDir)
		}
	}

	// Determine mode based on flags and argument types
	var beadID string
	var formulaName string

	if slingOnTarget != "" {
		// Ritual-on-bead mode: hd charge <ritual> --on <bead>
		formulaName = args[0]
		beadID = slingOnTarget
		// Verify both exist
		if err := verifyBeadExists(beadID); err != nil {
			return err
		}
		if err := verifyFormulaExists(formulaName); err != nil {
			return err
		}
	} else {
		// Could be bead mode or standalone ritual mode
		firstArg := args[0]

		// Try as bead first
		if err := verifyBeadExists(firstArg); err == nil {
			// It's a verified bead
			beadID = firstArg
		} else {
			// Not a verified bead - try as standalone ritual
			if err := verifyFormulaExists(firstArg); err == nil {
				// Standalone ritual mode: hd charge <ritual> [target]
				return runSlingFormula(args)
			}
			// Not a ritual either - check if it looks like a bead ID (routing issue workaround).
			// Accept it and let the actual rl update fail later if the bead doesn't exist.
			// This fixes: hd charge bd-ka761 relics/clan/dave failing with 'not a valid bead or ritual'
			if looksLikeBeadID(firstArg) {
				beadID = firstArg
			} else {
				// Neither bead nor ritual
				return fmt.Errorf("'%s' is not a valid bead or ritual", firstArg)
			}
		}
	}

	// Determine target agent (self or specified)
	var targetAgent string
	var targetPane string
	var hookWorkDir string // Working directory for running rl hook commands

	if len(args) > 1 {
		target := args[1]

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
					Force:    slingForce,
					Account:  slingAccount,
					Create:   slingCreate,
					BannerBead: beadID, // Set atomically at muster time
					Agent:    slingAgent,
				}
				spawnInfo, spawnErr := SpawnRaiderForSling(rigName, spawnOpts)
				if spawnErr != nil {
					return fmt.Errorf("spawning raider: %w", spawnErr)
				}
				targetAgent = spawnInfo.AgentID()
				targetPane = spawnInfo.Pane
				hookWorkDir = spawnInfo.ClonePath // Run rl commands from raider's worktree

				// Wake witness and forge to monitor the new raider
				wakeRigAgents(rigName)
			}
		} else {
			// Charging to an existing agent
			var targetWorkDir string
			targetAgent, targetPane, targetWorkDir, err = resolveTargetAgent(target)
			if err != nil {
				// Check if this is a dead raider (no active session)
				// If so, muster a fresh raider instead of failing
				if isRaiderTarget(target) {
					// Extract warband name from raider target (format: warband/raiders/name)
					parts := strings.Split(target, "/")
					if len(parts) >= 3 && parts[1] == "raiders" {
						rigName := parts[0]
						fmt.Printf("Target raider has no active session, spawning fresh raider in warband '%s'...\n", rigName)
						spawnOpts := SlingSpawnOptions{
							Force:    slingForce,
							Account:  slingAccount,
							Create:   slingCreate,
							BannerBead: beadID,
							Agent:    slingAgent,
						}
						spawnInfo, spawnErr := SpawnRaiderForSling(rigName, spawnOpts)
						if spawnErr != nil {
							return fmt.Errorf("spawning raider to replace dead raider: %w", spawnErr)
						}
						targetAgent = spawnInfo.AgentID()
						targetPane = spawnInfo.Pane
						hookWorkDir = spawnInfo.ClonePath

						// Wake witness and forge to monitor the new raider
						wakeRigAgents(rigName)
					} else {
						return fmt.Errorf("resolving target: %w", err)
					}
				} else {
					return fmt.Errorf("resolving target: %w", err)
				}
			}
			// Use target's working directory for rl commands (needed for redirect-based routing)
			if targetWorkDir != "" {
				hookWorkDir = targetWorkDir
			}
		}
	} else {
		// Charging to self
		var selfWorkDir string
		targetAgent, targetPane, selfWorkDir, err = resolveSelfTarget()
		if err != nil {
			return err
		}
		// Use self's working directory for rl commands
		if selfWorkDir != "" {
			hookWorkDir = selfWorkDir
		}
	}

	// Display what we're doing
	if formulaName != "" {
		fmt.Printf("%s Charging ritual %s on %s to %s...\n", style.Bold.Render("🎯"), formulaName, beadID, targetAgent)
	} else {
		fmt.Printf("%s Charging %s to %s...\n", style.Bold.Render("🎯"), beadID, targetAgent)
	}

	// Check if bead is already pinned (guard against accidental re-charge)
	info, err := getBeadInfo(beadID)
	if err != nil {
		return fmt.Errorf("checking bead status: %w", err)
	}
	if info.Status == "pinned" && !slingForce {
		assignee := info.Assignee
		if assignee == "" {
			assignee = "(unknown)"
		}
		return fmt.Errorf("bead %s is already pinned to %s\nUse --force to re-charge", beadID, assignee)
	}

	// Auto-raid: check if issue is already tracked by a raid
	// If not, create one for warmap visibility (unless --no-raid is set)
	if !slingNoRaid && formulaName == "" {
		existingRaid := isTrackedByRaid(beadID)
		if existingRaid == "" {
			if slingDryRun {
				fmt.Printf("Would create raid 'Work: %s'\n", info.Title)
				fmt.Printf("Would add tracking relation to %s\n", beadID)
			} else {
				raidID, err := createAutoRaid(beadID, info.Title)
				if err != nil {
					// Log warning but don't fail - raid is optional
					fmt.Printf("%s Could not create auto-raid: %v\n", style.Dim.Render("Warning:"), err)
				} else {
					fmt.Printf("%s Created raid 🚚 %s\n", style.Bold.Render("→"), raidID)
					fmt.Printf("  Tracking: %s\n", beadID)
				}
			}
		} else {
			fmt.Printf("%s Already tracked by raid %s\n", style.Dim.Render("○"), existingRaid)
		}
	}

	if slingDryRun {
		if formulaName != "" {
			fmt.Printf("Would instantiate ritual %s:\n", formulaName)
			fmt.Printf("  1. rl invoke %s\n", formulaName)
			fmt.Printf("  2. rl mol wisp %s --var feature=\"%s\" --var issue=\"%s\"\n", formulaName, info.Title, beadID)
			fmt.Printf("  3. rl mol bond <wisp-root> %s\n", beadID)
			fmt.Printf("  4. rl update <compound-root> --status=bannered --assignee=%s\n", targetAgent)
		} else {
			fmt.Printf("Would run: rl update %s --status=bannered --assignee=%s\n", beadID, targetAgent)
		}
		if slingSubject != "" {
			fmt.Printf("  subject (in signal): %s\n", slingSubject)
		}
		if slingMessage != "" {
			fmt.Printf("  context: %s\n", slingMessage)
		}
		if slingArgs != "" {
			fmt.Printf("  args (in signal): %s\n", slingArgs)
		}
		fmt.Printf("Would inject start prompt to pane: %s\n", targetPane)
		return nil
	}

	// Ritual-on-bead mode: instantiate ritual and bond to original bead
	if formulaName != "" {
		fmt.Printf("  Instantiating ritual %s...\n", formulaName)

		// Route rl mutations (wisp/bond) to the correct relics context for the target bead.
		// Some rl mol commands don't support prefix routing, so we must run them from the
		// warband directory that owns the bead's database.
		formulaWorkDir := relics.ResolveHookDir(townRoot, beadID, hookWorkDir)

		// Step 1: Invoke the ritual (ensures proto exists)
		// Invoke runs from warband directory to access the correct ritual database
		cookCmd := exec.Command("rl", "--no-daemon", "invoke", formulaName)
		cookCmd.Dir = formulaWorkDir
		cookCmd.Stderr = os.Stderr
		if err := cookCmd.Run(); err != nil {
			return fmt.Errorf("cooking ritual %s: %w", formulaName, err)
		}

		// Step 2: Create wisp with feature and issue variables from bead
		// Run from warband directory so wisp is created in correct database
		featureVar := fmt.Sprintf("feature=%s", info.Title)
		issueVar := fmt.Sprintf("issue=%s", beadID)
		wispArgs := []string{"--no-daemon", "mol", "wisp", formulaName, "--var", featureVar, "--var", issueVar, "--json"}
		wispCmd := exec.Command("rl", wispArgs...)
		wispCmd.Dir = formulaWorkDir
		wispCmd.Env = append(os.Environ(), "HD_ROOT="+townRoot)
		wispCmd.Stderr = os.Stderr
		wispOut, err := wispCmd.Output()
		if err != nil {
			return fmt.Errorf("creating wisp for ritual %s: %w", formulaName, err)
		}

		// Parse wisp output to get the root ID
		wispRootID, err := parseWispIDFromJSON(wispOut)
		if err != nil {
			return fmt.Errorf("parsing wisp output: %w", err)
		}
		fmt.Printf("%s Ritual wisp created: %s\n", style.Bold.Render("✓"), wispRootID)

		// Step 3: Bond wisp to original bead (creates compound)
		// Use --no-daemon for mol bond (requires direct database access)
		bondArgs := []string{"--no-daemon", "mol", "bond", wispRootID, beadID, "--json"}
		bondCmd := exec.Command("rl", bondArgs...)
		bondCmd.Dir = formulaWorkDir
		bondCmd.Stderr = os.Stderr
		bondOut, err := bondCmd.Output()
		if err != nil {
			return fmt.Errorf("bonding ritual to bead: %w", err)
		}

		// Parse bond output - the wisp root becomes the compound root
		// After bonding, we hook the wisp root (which now contains the original bead)
		var bondResult struct {
			RootID string `json:"root_id"`
		}
		if err := json.Unmarshal(bondOut, &bondResult); err != nil {
			// Fallback: use wisp root as the compound root
			fmt.Printf("%s Could not parse bond output, using wisp root\n", style.Dim.Render("Warning:"))
		} else if bondResult.RootID != "" {
			wispRootID = bondResult.RootID
		}

		fmt.Printf("%s Ritual bonded to %s\n", style.Bold.Render("✓"), beadID)

		// Update beadID to hook the compound root instead of bare bead
		beadID = wispRootID
	}

	// Hook the bead using rl update.
	// See: https://github.com/deeklead/horde/issues/148
	hookCmd := exec.Command("rl", "--no-daemon", "update", beadID, "--status=bannered", "--assignee="+targetAgent)
	hookCmd.Dir = relics.ResolveHookDir(townRoot, beadID, hookWorkDir)
	hookCmd.Stderr = os.Stderr
	if err := hookCmd.Run(); err != nil {
		return fmt.Errorf("hooking bead: %w", err)
	}

	fmt.Printf("%s Work attached to hook (status=bannered)\n", style.Bold.Render("✓"))

	// Log charge event to activity feed
	actor := detectActor()
	_ = events.LogFeed(events.TypeSling, actor, events.SlingPayload(beadID, targetAgent))

	// Update agent bead's banner_bead field (ZFC: agents track their current work)
	updateAgentBannerBead(targetAgent, beadID, hookWorkDir, townRelicsDir)

	// Auto-summon totem-raider-work to raider agent relics
	// This ensures raiders have the standard work totem attached for guidance
	if strings.Contains(targetAgent, "/raiders/") {
		if err := attachRaiderWorkMolecule(targetAgent, hookWorkDir, townRoot); err != nil {
			// Warn but don't fail - raider will still work without totem
			fmt.Printf("%s Could not summon work totem: %v\n", style.Dim.Render("Warning:"), err)
		}
	}

	// Store dispatcher in bead description (enables completion notification to dispatcher)
	if err := storeDispatcherInBead(beadID, actor); err != nil {
		// Warn but don't fail - raider will still complete work
		fmt.Printf("%s Could not store dispatcher in bead: %v\n", style.Dim.Render("Warning:"), err)
	}

	// Store args in bead description (no-tmux mode: relics as data plane)
	if slingArgs != "" {
		if err := storeArgsInBead(beadID, slingArgs); err != nil {
			// Warn but don't fail - args will still be in the signal prompt
			fmt.Printf("%s Could not store args in bead: %v\n", style.Dim.Render("Warning:"), err)
		} else {
			fmt.Printf("%s Args stored in bead (durable)\n", style.Bold.Render("✓"))
		}
	}

	// Try to inject the "start now" prompt (graceful if no tmux)
	if targetPane == "" {
		fmt.Printf("%s No pane to signal (agent will discover work via hd rally)\n", style.Dim.Render("○"))
	} else {
		// Ensure agent is ready before nudging (prevents race condition where
		// message arrives before Claude has fully started - see issue #115)
		sessionName := getSessionFromPane(targetPane)
		if sessionName != "" {
			if err := ensureAgentReady(sessionName); err != nil {
				// Non-fatal: warn and continue, agent will discover work via hd rally
				fmt.Printf("%s Could not verify agent ready: %v\n", style.Dim.Render("○"), err)
			}
		}

		if err := injectStartPrompt(targetPane, beadID, slingSubject, slingArgs); err != nil {
			// Graceful fallback for no-tmux mode
			fmt.Printf("%s Could not signal (no tmux?): %v\n", style.Dim.Render("○"), err)
			fmt.Printf("  Agent will discover work via hd rally / rl show\n")
		} else {
			fmt.Printf("%s Start prompt sent\n", style.Bold.Render("▶"))
		}
	}

	return nil
}
