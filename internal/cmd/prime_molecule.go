package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/constants"
	"github.com/OWNER/horde/internal/shaman"
	"github.com/OWNER/horde/internal/style"
)

// MoleculeCurrentOutput represents the JSON output of rl mol current.
type MoleculeCurrentOutput struct {
	MoleculeID    string `json:"molecule_id"`
	MoleculeTitle string `json:"molecule_title"`
	NextStep      *struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
	} `json:"next_step"`
	Completed int `json:"completed"`
	Total     int `json:"total"`
}

// showMoleculeExecutionPrompt calls rl mol current and shows the current step
// with execution instructions. This is the core of the Propulsion Principle.
func showMoleculeExecutionPrompt(workDir, moleculeID string) {
	// Call rl mol current with JSON output
	cmd := exec.Command("rl", "--no-daemon", "mol", "current", moleculeID, "--json")
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Fall back to simple message if rl mol current fails
		fmt.Println(style.Bold.Render("→ PROPULSION PRINCIPLE: Work is on your hook. RUN IT."))
		fmt.Println("  Begin working on this totem immediately.")
		fmt.Printf("  Check status with: rl mol current %s\n", moleculeID)
		return
	}
	// Handle rl --no-daemon exit 0 bug: empty stdout means not found
	if stdout.Len() == 0 {
		fmt.Println(style.Bold.Render("→ PROPULSION PRINCIPLE: Work is on your hook. RUN IT."))
		fmt.Println("  Begin working on this totem immediately.")
		return
	}

	// Parse JSON output - it's an array with one element
	var outputs []MoleculeCurrentOutput
	if err := json.Unmarshal(stdout.Bytes(), &outputs); err != nil || len(outputs) == 0 {
		// Fall back to simple message
		fmt.Println(style.Bold.Render("→ PROPULSION PRINCIPLE: Work is on your hook. RUN IT."))
		fmt.Println("  Begin working on this totem immediately.")
		return
	}
	output := outputs[0]

	// Show totem progress
	fmt.Printf("**Progress:** %d/%d steps complete\n\n",
		output.Completed, output.Total)

	// Show current step if available
	if output.NextStep != nil {
		step := output.NextStep
		fmt.Printf("%s\n\n", style.Bold.Render("## 🎬 CURRENT STEP: "+step.Title))
		fmt.Printf("**Step ID:** %s\n", step.ID)
		fmt.Printf("**Status:** %s (ready to execute)\n\n", step.Status)

		// Show step description if available
		if step.Description != "" {
			fmt.Println("### Instructions")
			fmt.Println()
			// Indent the description for readability
			lines := strings.Split(step.Description, "\n")
			for _, line := range lines {
				fmt.Printf("%s\n", line)
			}
			fmt.Println()
		}

		// The propulsion directive
		fmt.Println(style.Bold.Render("→ EXECUTE THIS STEP NOW."))
		fmt.Println()
		fmt.Println("When complete:")
		fmt.Printf("  1. Close the step: rl close %s\n", step.ID)
		fmt.Println("  2. Check for next step: rl ready")
		fmt.Println("  3. Continue until totem complete")
	} else {
		// No next step - totem may be complete
		fmt.Println(style.Bold.Render("✓ TOTEM COMPLETE"))
		fmt.Println()
		fmt.Println("All steps are done. You may:")
		fmt.Println("  - Report completion to supervisor")
		fmt.Println("  - Check for new work: rl ready")
	}
}

// outputMoleculeContext checks if the agent is working on a totem step and shows progress.
func outputMoleculeContext(ctx RoleContext) {
	// Applies to raiders, clan workers, shaman, witness, and forge
	if ctx.Role != RoleRaider && ctx.Role != RoleCrew && ctx.Role != RoleShaman && ctx.Role != RoleWitness && ctx.Role != RoleForge {
		return
	}

	// For Shaman, use special scout totem handling
	if ctx.Role == RoleShaman {
		outputShamanPatrolContext(ctx)
		return
	}

	// For Witness, use special scout totem handling (auto-bonds on startup)
	if ctx.Role == RoleWitness {
		outputWitnessPatrolContext(ctx)
		return
	}

	// For Forge, use special scout totem handling (auto-bonds on startup)
	if ctx.Role == RoleForge {
		outputForgePatrolContext(ctx)
		return
	}

	// Check for in-progress issues
	b := relics.New(ctx.WorkDir)
	issues, err := b.List(relics.ListOptions{
		Status:   "in_progress",
		Assignee: ctx.Raider,
		Priority: -1,
	})
	if err != nil || len(issues) == 0 {
		return
	}

	// Check if any in-progress issue is a totem step
	for _, issue := range issues {
		moleculeID := parseMoleculeMetadata(issue.Description)
		if moleculeID == "" {
			continue
		}

		// Get the parent (root) issue ID
		rootID := issue.Parent
		if rootID == "" {
			continue
		}

		// This is a totem step - show context
		fmt.Println()
		fmt.Printf("%s\n\n", style.Bold.Render("## 🧬 Totem Workflow"))
		fmt.Printf("You are working on a totem step.\n")
		fmt.Printf("  Current step: %s\n", issue.ID)
		fmt.Printf("  Totem: %s\n", moleculeID)
		fmt.Printf("  Root issue: %s\n\n", rootID)

		// Show totem progress by finding sibling steps
		showMoleculeProgress(b, rootID)

		fmt.Println()
		fmt.Println("**Totem Work Loop:**")
		fmt.Println("1. Complete current step, then `rl close " + issue.ID + "`")
		fmt.Println("2. Check for next steps: `rl ready --parent " + rootID + "`")
		fmt.Println("3. Work on next ready step(s)")
		fmt.Println("4. When all steps done, run `hd done`")
		break // Only show context for first totem step found
	}
}

// parseMoleculeMetadata extracts totem info from a step's description.
// Looks for lines like:
//
//	instantiated_from: totem-xyz
func parseMoleculeMetadata(description string) string {
	lines := strings.Split(description, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "instantiated_from:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "instantiated_from:"))
		}
	}
	return ""
}

// showMoleculeProgress displays the progress through a totem's steps.
func showMoleculeProgress(b *relics.Relics, rootID string) {
	if rootID == "" {
		return
	}

	// Find all children of the root issue
	children, err := b.List(relics.ListOptions{
		Parent:   rootID,
		Status:   "all",
		Priority: -1,
	})
	if err != nil || len(children) == 0 {
		return
	}

	total := len(children)
	done := 0
	inProgress := 0
	var readySteps []string

	for _, child := range children {
		switch child.Status {
		case "closed":
			done++
		case "in_progress":
			inProgress++
		case "open":
			// Check if ready (no open dependencies)
			if len(child.DependsOn) == 0 {
				readySteps = append(readySteps, child.ID)
			}
		}
	}

	fmt.Printf("Progress: %d/%d steps complete", done, total)
	if inProgress > 0 {
		fmt.Printf(" (%d in progress)", inProgress)
	}
	fmt.Println()

	if len(readySteps) > 0 {
		fmt.Printf("Ready steps: %s\n", strings.Join(readySteps, ", "))
	}
}

// outputShamanPatrolContext shows scout totem status for the Shaman.
// Shaman uses wisps (Wisp:true issues in main .relics/) for scout cycles.
// Shaman is a encampment-level role, so it uses encampment root relics (not warband relics).
func outputShamanPatrolContext(ctx RoleContext) {
	// Check if Shaman is paused - if so, output PAUSED message and skip scout context
	paused, state, err := shaman.IsPaused(ctx.TownRoot)
	if err == nil && paused {
		outputShamanPausedMessage(state)
		return
	}

	cfg := PatrolConfig{
		RoleName:        "shaman",
		PatrolMolName:   "totem-shaman-scout",
		RelicsDir:        ctx.TownRoot, // Encampment-level role uses encampment root relics
		Assignee:        "shaman",
		HeaderEmoji:     "🔄",
		HeaderTitle:     "Scout Status (Wisp-based)",
		CheckInProgress: false,
		WorkLoopSteps: []string{
			"Check next step: `rl ready`",
			"Execute the step (heartbeat, drums, health checks, etc.)",
			"Close step: `rl close <step-id>`",
			"Check next: `rl ready`",
			"At cycle end (loop-or-exit step):\n   - If context LOW:\n     * Squash: `rl mol squash <totem-id> --summary \"<summary>\"`\n     * Create new scout: `rl mol wisp totem-shaman-scout`\n     * Continue executing from inbox-check step\n   - If context HIGH:\n     * Send handoff: `hd handoff -s \"Shaman scout\" -m \"<observations>\"`\n     * Exit cleanly (daemon respawns fresh session)",
		},
	}
	outputPatrolContext(cfg)
}

// outputWitnessPatrolContext shows scout totem status for the Witness.
// Witness AUTO-BONDS its scout totem on startup if one isn't already running.
func outputWitnessPatrolContext(ctx RoleContext) {
	cfg := PatrolConfig{
		RoleName:        "witness",
		PatrolMolName:   "totem-witness-scout",
		RelicsDir:        ctx.WorkDir,
		Assignee:        ctx.Warband + "/witness",
		HeaderEmoji:     constants.EmojiWitness,
		HeaderTitle:     "Witness Scout Status",
		CheckInProgress: true,
		WorkLoopSteps: []string{
			"Check inbox: `hd drums inbox`",
			"Check next step: `rl ready`",
			"Execute the step (survey raiders, inspect, signal, etc.)",
			"Close step: `rl close <step-id>`",
			"Check next: `rl ready`",
			"At cycle end (loop-or-exit step):\n   - If context LOW:\n     * Squash: `rl mol squash <totem-id> --summary \"<summary>\"`\n     * Create new scout: `rl mol wisp totem-witness-scout`\n     * Continue executing from inbox-check step\n   - If context HIGH:\n     * Send handoff: `hd handoff -s \"Witness scout\" -m \"<observations>\"`\n     * Exit cleanly (daemon respawns fresh session)",
		},
	}
	outputPatrolContext(cfg)
}

// outputForgePatrolContext shows scout totem status for the Forge.
// Forge AUTO-BONDS its scout totem on startup if one isn't already running.
func outputForgePatrolContext(ctx RoleContext) {
	cfg := PatrolConfig{
		RoleName:        "forge",
		PatrolMolName:   "totem-forge-scout",
		RelicsDir:        ctx.WorkDir,
		Assignee:        ctx.Warband + "/forge",
		HeaderEmoji:     "🔧",
		HeaderTitle:     "Forge Scout Status",
		CheckInProgress: true,
		WorkLoopSteps: []string{
			"Check inbox: `hd drums inbox`",
			"Check next step: `rl ready`",
			"Execute the step (queue scan, process branch, tests, merge)",
			"Close step: `rl close <step-id>`",
			"Check next: `rl ready`",
			"At cycle end (loop-or-exit step):\n   - If context LOW:\n     * Squash: `rl mol squash <totem-id> --summary \"<summary>\"`\n     * Create new scout: `rl mol wisp totem-forge-scout`\n     * Continue executing from inbox-check step\n   - If context HIGH:\n     * Send handoff: `hd handoff -s \"Forge scout\" -m \"<observations>\"`\n     * Exit cleanly (daemon respawns fresh session)",
		},
	}
	outputPatrolContext(cfg)
}
