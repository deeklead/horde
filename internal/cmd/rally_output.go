package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/checkpoint"
	"github.com/deeklead/horde/internal/shaman"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/templates"
	"github.com/deeklead/horde/internal/workspace"
)

// outputPrimeContext outputs the role-specific context using templates or fallback.
func outputPrimeContext(ctx RoleContext) error {
	// Try to use templates first
	tmpl, err := templates.New()
	if err != nil {
		// Fall back to hardcoded output if templates fail
		return outputPrimeContextFallback(ctx)
	}

	// Map role to template name
	var roleName string
	switch ctx.Role {
	case RoleWarchief:
		roleName = "warchief"
	case RoleShaman:
		roleName = "shaman"
	case RoleWitness:
		roleName = "witness"
	case RoleForge:
		roleName = "forge"
	case RoleRaider:
		roleName = "raider"
	case RoleCrew:
		roleName = "clan"
	default:
		// Unknown role - use fallback
		return outputPrimeContextFallback(ctx)
	}

	// Build template data
	// Get encampment name for session names
	townName, _ := workspace.GetTownName(ctx.TownRoot)

	// Get default branch from warband config (default to "main" if not set)
	defaultBranch := "main"
	if ctx.Warband != "" && ctx.TownRoot != "" {
		rigPath := filepath.Join(ctx.TownRoot, ctx.Warband)
		if rigCfg, err := warband.LoadRigConfig(rigPath); err == nil && rigCfg.DefaultBranch != "" {
			defaultBranch = rigCfg.DefaultBranch
		}
	}

	data := templates.RoleData{
		Role:          roleName,
		RigName:       ctx.Warband,
		TownRoot:      ctx.TownRoot,
		TownName:      townName,
		WorkDir:       ctx.WorkDir,
		DefaultBranch: defaultBranch,
		Raider:       ctx.Raider,
		WarchiefSession:  session.WarchiefSessionName(),
		ShamanSession: session.ShamanSessionName(),
	}

	// Render and output
	output, err := tmpl.RenderRole(roleName, data)
	if err != nil {
		return fmt.Errorf("rendering template: %w", err)
	}

	fmt.Print(output)
	return nil
}

func outputPrimeContextFallback(ctx RoleContext) error {
	switch ctx.Role {
	case RoleWarchief:
		outputWarchiefContext(ctx)
	case RoleWitness:
		outputWitnessContext(ctx)
	case RoleForge:
		outputForgeContext(ctx)
	case RoleRaider:
		outputRaiderContext(ctx)
	case RoleCrew:
		outputCrewContext(ctx)
	default:
		outputUnknownContext(ctx)
	}
	return nil
}

func outputWarchiefContext(ctx RoleContext) {
	fmt.Printf("%s\n\n", style.Bold.Render("# Warchief Context"))
	fmt.Println("You are the **Warchief** - the global coordinator of Horde.")
	fmt.Println()
	fmt.Println("## Responsibilities")
	fmt.Println("- Coordinate work across all warbands")
	fmt.Println("- Delegate to Refineries, not directly to raiders")
	fmt.Println("- Monitor overall system health")
	fmt.Println()
	fmt.Println("## Key Commands")
	fmt.Println("- `hd drums inbox` - Check your messages")
	fmt.Println("- `hd drums read <id>` - Read a specific message")
	fmt.Println("- `hd status` - Show overall encampment status")
	fmt.Println("- `hd warband list` - List all warbands")
	fmt.Println("- `rl ready` - Issues ready to work")
	fmt.Println()
	fmt.Println("## Hookable Drums")
	fmt.Println("Drums can be bannered for ad-hoc instructions: `hd hook summon <drums-id>`")
	fmt.Println("If drums is on your hook, read and execute its instructions (GUPP applies).")
	fmt.Println()
	fmt.Println("## Startup")
	fmt.Println("Check for handoff messages with 🤝 HANDOFF in subject - continue predecessor's work.")
	fmt.Println()
	fmt.Printf("Encampment root: %s\n", style.Dim.Render(ctx.TownRoot))
}

func outputWitnessContext(ctx RoleContext) {
	fmt.Printf("%s\n\n", style.Bold.Render("# Witness Context"))
	fmt.Printf("You are the **Witness** for warband: %s\n\n", style.Bold.Render(ctx.Warband))
	fmt.Println("## Responsibilities")
	fmt.Println("- Monitor raider health via heartbeat")
	fmt.Println("- Muster replacement agents for stuck raiders")
	fmt.Println("- Report warband status to Warchief")
	fmt.Println()
	fmt.Println("## Key Commands")
	fmt.Println("- `hd witness status` - Show witness status")
	fmt.Println("- `hd raider list` - List raiders in this warband")
	fmt.Println()
	fmt.Println("## Hookable Drums")
	fmt.Println("Drums can be bannered for ad-hoc instructions: `hd hook summon <drums-id>`")
	fmt.Println("If drums is on your hook, read and execute its instructions (GUPP applies).")
	fmt.Println()
	fmt.Printf("Warband: %s\n", style.Dim.Render(ctx.Warband))
}

func outputForgeContext(ctx RoleContext) {
	fmt.Printf("%s\n\n", style.Bold.Render("# Forge Context"))
	fmt.Printf("You are the **Forge** for warband: %s\n\n", style.Bold.Render(ctx.Warband))
	fmt.Println("## Responsibilities")
	fmt.Println("- Process the merge queue for this warband")
	fmt.Println("- Merge raider work to integration branch")
	fmt.Println("- Resolve merge conflicts")
	fmt.Println("- Land completed swarms to main")
	fmt.Println()
	fmt.Println("## Key Commands")
	fmt.Println("- `hd merge queue` - Show pending merges")
	fmt.Println("- `hd merge next` - Process next merge")
	fmt.Println()
	fmt.Println("## Hookable Drums")
	fmt.Println("Drums can be bannered for ad-hoc instructions: `hd hook summon <drums-id>`")
	fmt.Println("If drums is on your hook, read and execute its instructions (GUPP applies).")
	fmt.Println()
	fmt.Printf("Warband: %s\n", style.Dim.Render(ctx.Warband))
}

func outputRaiderContext(ctx RoleContext) {
	fmt.Printf("%s\n\n", style.Bold.Render("# Raider Context"))
	fmt.Printf("You are raider **%s** in warband: %s\n\n",
		style.Bold.Render(ctx.Raider), style.Bold.Render(ctx.Warband))
	fmt.Println("## Startup Protocol")
	fmt.Println("1. Run `hd rally` - loads context and checks drums automatically")
	fmt.Println("2. Check inbox - if drums shown, read with `hd drums read <id>`")
	fmt.Println("3. Look for '📋 Work Assignment' messages for your task")
	fmt.Println("4. If no drums, check `rl list --status=in_progress` for existing work")
	fmt.Println()
	fmt.Println("## Key Commands")
	fmt.Println("- `hd drums inbox` - Check your inbox for work assignments")
	fmt.Println("- `rl show <issue>` - View your assigned issue")
	fmt.Println("- `rl close <issue>` - Mark issue complete")
	fmt.Println("- `hd done` - Signal work ready for merge")
	fmt.Println()
	fmt.Println("## Hookable Drums")
	fmt.Println("Drums can be bannered for ad-hoc instructions: `hd hook summon <drums-id>`")
	fmt.Println("If drums is on your hook, read and execute its instructions (GUPP applies).")
	fmt.Println()
	fmt.Printf("Raider: %s | Warband: %s\n",
		style.Dim.Render(ctx.Raider), style.Dim.Render(ctx.Warband))
}

func outputCrewContext(ctx RoleContext) {
	fmt.Printf("%s\n\n", style.Bold.Render("# Clan Worker Context"))
	fmt.Printf("You are clan worker **%s** in warband: %s\n\n",
		style.Bold.Render(ctx.Raider), style.Bold.Render(ctx.Warband))
	fmt.Println("## About Clan Workers")
	fmt.Println("- Persistent workspace (not auto-garbage-collected)")
	fmt.Println("- User-managed (not Witness-monitored)")
	fmt.Println("- Long-lived identity across sessions")
	fmt.Println()
	fmt.Println("## Key Commands")
	fmt.Println("- `hd drums inbox` - Check your inbox")
	fmt.Println("- `rl ready` - Available issues")
	fmt.Println("- `rl show <issue>` - View issue details")
	fmt.Println("- `rl close <issue>` - Mark issue complete")
	fmt.Println()
	fmt.Println("## Hookable Drums")
	fmt.Println("Drums can be bannered for ad-hoc instructions: `hd hook summon <drums-id>`")
	fmt.Println("If drums is on your hook, read and execute its instructions (GUPP applies).")
	fmt.Println()
	fmt.Printf("Clan: %s | Warband: %s\n",
		style.Dim.Render(ctx.Raider), style.Dim.Render(ctx.Warband))
}

func outputUnknownContext(ctx RoleContext) {
	fmt.Printf("%s\n\n", style.Bold.Render("# Horde Context"))
	fmt.Println("Could not determine specific role from current directory.")
	fmt.Println()
	if ctx.Warband != "" {
		fmt.Printf("You appear to be in warband: %s\n\n", style.Bold.Render(ctx.Warband))
	}
	fmt.Println("Navigate to a specific agent directory:")
	fmt.Println("- `<warband>/raiders/<name>/` - Raider role")
	fmt.Println("- `<warband>/witness/warband/` - Witness role")
	fmt.Println("- `<warband>/forge/warband/` - Forge role")
	fmt.Println("- Encampment root or `warchief/` - Warchief role")
	fmt.Println()
	fmt.Printf("Encampment root: %s\n", style.Dim.Render(ctx.TownRoot))
}

// outputHandoffContent reads and displays the pinned handoff bead for the role.
func outputHandoffContent(ctx RoleContext) {
	if ctx.Role == RoleUnknown {
		return
	}

	// Get role key for handoff bead lookup
	roleKey := string(ctx.Role)

	bd := relics.New(ctx.TownRoot)
	issue, err := bd.FindHandoffBead(roleKey)
	if err != nil {
		// Silently skip if relics lookup fails (might not be a relics repo)
		return
	}
	if issue == nil || issue.Description == "" {
		// No handoff content
		return
	}

	// Display handoff content
	fmt.Println()
	fmt.Printf("%s\n\n", style.Bold.Render("## 🤝 Handoff from Previous Session"))
	fmt.Println(issue.Description)
	fmt.Println()
	fmt.Println(style.Dim.Render("(Clear with: hd warband reset --handoff)"))
}

// outputStartupDirective outputs role-specific instructions for the agent.
// This tells agents like Warchief to announce themselves on startup.
func outputStartupDirective(ctx RoleContext) {
	switch ctx.Role {
	case RoleWarchief:
		fmt.Println()
		fmt.Println("---")
		fmt.Println()
		fmt.Println("**STARTUP PROTOCOL**: You are the Warchief. Please:")
		fmt.Println("1. Announce: \"Warchief, checking in.\"")
		fmt.Println("2. Check drums: `hd drums inbox` - look for 🤝 HANDOFF messages")
		fmt.Println("3. Check for attached work: `hd hook`")
		fmt.Println("   - If mol attached → **RUN IT** (no human input needed)")
		fmt.Println("   - If no mol → await user instruction")
	case RoleWitness:
		fmt.Println()
		fmt.Println("---")
		fmt.Println()
		fmt.Println("**STARTUP PROTOCOL**: You are the Witness. Please:")
		fmt.Println("1. Announce: \"Witness, checking in.\"")
		fmt.Println("2. Check drums: `hd drums inbox` - look for 🤝 HANDOFF messages")
		fmt.Println("3. Check for attached scout: `hd hook`")
		fmt.Println("   - If mol attached → **RUN IT** (resume from current step)")
		fmt.Println("   - If no mol → create scout: `rl mol wisp totem-witness-scout`")
	case RoleRaider:
		fmt.Println()
		fmt.Println("---")
		fmt.Println()
		fmt.Println("**STARTUP PROTOCOL**: You are a raider. Please:")
		fmt.Printf("1. Announce: \"%s Raider %s, checking in.\"\n", ctx.Warband, ctx.Raider)
		fmt.Println("2. Check drums: `hd drums inbox`")
		fmt.Println("3. If there's a 🤝 HANDOFF message, read it for context")
		fmt.Println("4. Check for attached work: `hd hook`")
		fmt.Println("   - If mol attached → **RUN IT** (you were spawned with this work)")
		fmt.Println("   - If no mol → ERROR: raiders must have work attached; escalate to Witness")
	case RoleForge:
		fmt.Println()
		fmt.Println("---")
		fmt.Println()
		fmt.Println("**STARTUP PROTOCOL**: You are the Forge. Please:")
		fmt.Println("1. Announce: \"Forge, checking in.\"")
		fmt.Println("2. Check drums: `hd drums inbox` - look for 🤝 HANDOFF messages")
		fmt.Println("3. Check for attached scout: `hd hook`")
		fmt.Println("   - If mol attached → **RUN IT** (resume from current step)")
		fmt.Println("   - If no mol → create scout: `rl mol wisp totem-forge-scout`")
	case RoleCrew:
		fmt.Println()
		fmt.Println("---")
		fmt.Println()
		fmt.Println("**STARTUP PROTOCOL**: You are a clan worker. Please:")
		fmt.Printf("1. Announce: \"%s Clan %s, checking in.\"\n", ctx.Warband, ctx.Raider)
		fmt.Println("2. Check drums: `hd drums inbox`")
		fmt.Println("3. If there's a 🤝 HANDOFF message, read it and continue the work")
		fmt.Println("4. Check for attached work: `hd hook`")
		fmt.Println("   - If attachment found → **RUN IT** (no human input needed)")
		fmt.Println("   - If no attachment → await user instruction")
	case RoleShaman:
		// Skip startup protocol if paused - the pause message was already shown
		paused, _, _ := shaman.IsPaused(ctx.TownRoot)
		if paused {
			return
		}
		fmt.Println()
		fmt.Println("---")
		fmt.Println()
		fmt.Println("**STARTUP PROTOCOL**: You are the Shaman. Please:")
		fmt.Println("1. Announce: \"Shaman, checking in.\"")
		fmt.Println("2. Signal awake: `hd shaman heartbeat \"starting scout\"`")
		fmt.Println("3. Check drums: `hd drums inbox` - look for 🤝 HANDOFF messages")
		fmt.Println("4. Check for attached scout: `hd hook`")
		fmt.Println("   - If mol attached → **RUN IT** (resume from current step)")
		fmt.Println("   - If no mol → create scout: `rl mol wisp totem-shaman-scout`")
	}
}

// outputAttachmentStatus checks for attached work totem and outputs status.
// This is key for the autonomous overnight work pattern.
// The Propulsion Principle: "If you find something on your hook, YOU RUN IT."
func outputAttachmentStatus(ctx RoleContext) {
	// Skip only unknown roles - all valid roles can have pinned work
	if ctx.Role == RoleUnknown {
		return
	}

	// Check for pinned relics with attachments
	b := relics.New(ctx.WorkDir)

	// Build assignee string based on role (same as getAgentIdentity)
	assignee := getAgentIdentity(ctx)
	if assignee == "" {
		return
	}

	// Find pinned relics for this agent
	pinnedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusPinned,
		Assignee: assignee,
		Priority: -1,
	})
	if err != nil || len(pinnedRelics) == 0 {
		// No pinned relics - interactive mode
		return
	}

	// Check first pinned bead for attachment
	attachment := relics.ParseAttachmentFields(pinnedRelics[0])
	if attachment == nil || attachment.AttachedMolecule == "" {
		// No attachment - interactive mode
		return
	}

	// Has attached work - output prominently with current step
	fmt.Println()
	fmt.Printf("%s\n\n", style.Bold.Render("## 🎯 ATTACHED WORK DETECTED"))
	fmt.Printf("Pinned bead: %s\n", pinnedRelics[0].ID)
	fmt.Printf("Attached totem: %s\n", attachment.AttachedMolecule)
	if attachment.AttachedAt != "" {
		fmt.Printf("Attached at: %s\n", attachment.AttachedAt)
	}
	if attachment.AttachedArgs != "" {
		fmt.Println()
		fmt.Printf("%s\n", style.Bold.Render("📋 ARGS (use these to guide execution):"))
		fmt.Printf("  %s\n", attachment.AttachedArgs)
	}
	fmt.Println()

	// Show current step from totem
	showMoleculeExecutionPrompt(ctx.WorkDir, attachment.AttachedMolecule)
}

// outputHandoffWarning outputs the post-handoff warning message.
func outputHandoffWarning(prevSession string) {
	fmt.Println()
	fmt.Println(style.Bold.Render("╔══════════════════════════════════════════════════════════════════╗"))
	fmt.Println(style.Bold.Render("║  ✅ HANDOFF COMPLETE - You are the NEW session                   ║"))
	fmt.Println(style.Bold.Render("╚══════════════════════════════════════════════════════════════════╝"))
	fmt.Println()
	if prevSession != "" {
		fmt.Printf("Your predecessor (%s) handed off to you.\n", prevSession)
	}
	fmt.Println()
	fmt.Println(style.Bold.Render("⚠️  DO NOT run /handoff - that was your predecessor's action."))
	fmt.Println("   The /handoff you see in context is NOT a request for you.")
	fmt.Println()
	fmt.Println("Instead: Check your hook (`hd mol status`) and drums (`hd drums inbox`).")
	fmt.Println()
}

// outputState outputs only the session state (for --state flag).
// If jsonOutput is true, outputs JSON format instead of key:value.
func outputState(ctx RoleContext, jsonOutput bool) {
	state := detectSessionState(ctx)

	if jsonOutput {
		data, err := json.Marshal(state)
		if err != nil {
			// Fall back to plain text on error
			fmt.Printf("state: %s\n", state.State)
			fmt.Printf("role: %s\n", state.Role)
			return
		}
		fmt.Println(string(data))
		return
	}

	fmt.Printf("state: %s\n", state.State)
	fmt.Printf("role: %s\n", state.Role)

	switch state.State {
	case "post-handoff":
		if state.PrevSession != "" {
			fmt.Printf("prev_session: %s\n", state.PrevSession)
		}
	case "crash-recovery":
		if state.CheckpointAge != "" {
			fmt.Printf("checkpoint_age: %s\n", state.CheckpointAge)
		}
	case "autonomous":
		if state.HookedBead != "" {
			fmt.Printf("hooked_bead: %s\n", state.HookedBead)
		}
	}
}

// outputCheckpointContext reads and displays any previous session checkpoint.
// This enables crash recovery by showing what the previous session was working on.
func outputCheckpointContext(ctx RoleContext) {
	// Only applies to raiders and clan workers
	if ctx.Role != RoleRaider && ctx.Role != RoleCrew {
		return
	}

	// Read checkpoint
	cp, err := checkpoint.Read(ctx.WorkDir)
	if err != nil {
		// Silently ignore read errors
		return
	}
	if cp == nil {
		// No checkpoint exists
		return
	}

	// Check if checkpoint is stale (older than 24 hours)
	if cp.IsStale(24 * time.Hour) {
		// Remove stale checkpoint
		_ = checkpoint.Remove(ctx.WorkDir)
		return
	}

	// Display checkpoint context
	fmt.Println()
	fmt.Printf("%s\n\n", style.Bold.Render("## 📌 Previous Session Checkpoint"))
	fmt.Printf("A previous session left a checkpoint %s ago.\n\n", cp.Age().Round(time.Minute))

	if cp.StepTitle != "" {
		fmt.Printf("  **Working on:** %s\n", cp.StepTitle)
	}
	if cp.MoleculeID != "" {
		fmt.Printf("  **Totem:** %s\n", cp.MoleculeID)
	}
	if cp.CurrentStep != "" {
		fmt.Printf("  **Step:** %s\n", cp.CurrentStep)
	}
	if cp.HookedBead != "" {
		fmt.Printf("  **Planted bead:** %s\n", cp.HookedBead)
	}
	if cp.Branch != "" {
		fmt.Printf("  **Branch:** %s\n", cp.Branch)
	}
	if len(cp.ModifiedFiles) > 0 {
		fmt.Printf("  **Modified files:** %d\n", len(cp.ModifiedFiles))
		// Show first few files
		maxShow := 5
		if len(cp.ModifiedFiles) < maxShow {
			maxShow = len(cp.ModifiedFiles)
		}
		for i := 0; i < maxShow; i++ {
			fmt.Printf("    - %s\n", cp.ModifiedFiles[i])
		}
		if len(cp.ModifiedFiles) > maxShow {
			fmt.Printf("    ... and %d more\n", len(cp.ModifiedFiles)-maxShow)
		}
	}
	if cp.Notes != "" {
		fmt.Printf("  **Notes:** %s\n", cp.Notes)
	}
	fmt.Println()

	fmt.Println("Use this context to resume work. The checkpoint will be updated as you progress.")
	fmt.Println()
}

// outputShamanPausedMessage outputs a prominent PAUSED message for the Shaman.
// When paused, the Shaman must not perform any scout actions.
func outputShamanPausedMessage(state *shaman.PauseState) {
	fmt.Println()
	fmt.Printf("%s\n\n", style.Bold.Render("## ⏸️  SHAMAN PAUSED"))
	fmt.Println("You are paused and must NOT perform any scout actions.")
	fmt.Println()
	if state.Reason != "" {
		fmt.Printf("Reason: %s\n", state.Reason)
	}
	fmt.Printf("Paused at: %s\n", state.PausedAt.Format(time.RFC3339))
	if state.PausedBy != "" {
		fmt.Printf("Paused by: %s\n", state.PausedBy)
	}
	fmt.Println()
	fmt.Println("Wait for human to run `hd shaman resume` before working.")
	fmt.Println()
	fmt.Println("**DO NOT:**")
	fmt.Println("- Create scout totems")
	fmt.Println("- Run heartbeats")
	fmt.Println("- Check agent health")
	fmt.Println("- Take any autonomous actions")
	fmt.Println()
	fmt.Println("You may respond to direct human questions.")
}

// explain outputs an explanatory message if --explain mode is enabled.
func explain(condition bool, reason string) {
	if primeExplain && condition {
		fmt.Printf("\n[EXPLAIN] %s\n", reason)
	}
}
