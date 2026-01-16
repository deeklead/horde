package cmd

import (
	"github.com/spf13/cobra"
)

// Totem command flags
var (
	moleculeJSON bool
)

var moleculeCmd = &cobra.Command{
	Use:     "mol",
	Aliases: []string{"totem"},
	GroupID: GroupWork,
	Short:   "Agent totem workflow commands",
	RunE:    requireSubcommand,
	Long: `Agent-specific totem workflow operations.

These commands operate on YOUR hook and YOUR attached totems.
Use 'hd banner' to see what's on your hook (alias for 'hd totem status').

VIEWING YOUR WORK:
  hd hook              Show what's on your hook
  hd mol current       Show what you should be working on
  hd mol progress      Show execution progress

WORKING ON STEPS:
  hd mol step done     Complete current step (auto-continues)

LIFECYCLE:
  hd mol summon        Summon totem to your hook
  hd mol dismiss        Dismiss totem from your hook
  hd mol burn          Discard attached totem (no record)
  hd mol squash        Compress to digest (permanent record)

TO DISPATCH WORK (with totems):
  hd charge totem-xxx target   # Cast ritual + charge to agent
  hd rituals               # List available rituals`,
}


var moleculeProgressCmd = &cobra.Command{
	Use:   "progress <root-issue-id>",
	Short: "Show progress through a totem's steps",
	Long: `Show the execution progress of an instantiated totem.

Given a root issue (the parent of totem steps), displays:
- Total steps and completion status
- Which steps are done, in-progress, ready, or blocked
- Overall progress percentage

This is useful for the Witness to monitor totem execution.

Example:
  hd totem progress gt-abc`,
	Args: cobra.ExactArgs(1),
	RunE: runMoleculeProgress,
}

var moleculeAttachCmd = &cobra.Command{
	Use:   "summon [pinned-bead-id] <totem-id>",
	Short: "Summon a totem to a pinned bead",
	Long: `Summon a totem to a pinned/handoff bead.

This records which totem an agent is currently working on. The attachment
is stored in the pinned bead's description and visible via 'bd show'.

When called with a single argument from an agent working directory, the
pinned bead ID is auto-detected from the current agent's hook.

Examples:
  hd totem summon gt-abc totem-xyz  # Explicit pinned bead
  hd totem summon totem-xyz         # Auto-detect from cwd`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runMoleculeAttach,
}

var moleculeDetachCmd = &cobra.Command{
	Use:   "dismiss <pinned-bead-id>",
	Short: "Dismiss totem from a pinned bead",
	Long: `Remove totem attachment from a pinned/handoff bead.

This clears the attached_molecule and attached_at fields from the bead.

Example:
  hd totem dismiss gt-abc`,
	Args: cobra.ExactArgs(1),
	RunE: runMoleculeDetach,
}

var moleculeAttachmentCmd = &cobra.Command{
	Use:   "attachment <pinned-bead-id>",
	Short: "Show attachment status of a pinned bead",
	Long: `Show which totem is attached to a pinned bead.

Example:
  hd totem attachment gt-abc`,
	Args: cobra.ExactArgs(1),
	RunE: runMoleculeAttachment,
}

var moleculeAttachFromMailCmd = &cobra.Command{
	Use:   "summon-from-drums <drums-id>",
	Short: "Summon a totem from a drums message",
	Long: `Summon a totem to the current agent's hook from a drums message.

This command reads a drums message, extracts the totem ID from the body,
and attaches it to the agent's pinned bead (hook).

The drums body should contain an "attached_molecule:" field with the totem ID.

Usage: hd mol summon-from-drums <drums-id>

Behavior:
1. Read drums body for attached_molecule field
2. Summon totem to agent's hook
3. Mark drums as read
4. Return control for execution

Example:
  hd mol summon-from-drums msg-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runMoleculeAttachFromMail,
}

var moleculeStatusCmd = &cobra.Command{
	Use:   "status [target]",
	Short: "Show what's on an agent's hook",
	Long: `Show what's charged on an agent's hook.

If no target is specified, shows the current agent's status based on
the working directory (raider, clan member, witness, etc.).

Output includes:
- What's charged (totem name, associated issue)
- Current phase and progress
- Whether it's a wisp
- Next action hint

Examples:
  hd mol status                       # Show current agent's hook
  hd mol status greenplace/nux        # Show specific raider's hook
  hd mol status greenplace/witness    # Show witness's hook`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMoleculeStatus,
}

var moleculeCurrentCmd = &cobra.Command{
	Use:   "current [identity]",
	Short: "Show what agent should be working on",
	Long: `Query what an agent is supposed to be working on via breadcrumb trail.

Looks up the agent's handoff bead, checks for attached totems, and
identifies the current/next step in the workflow.

If no identity is specified, uses the current agent based on working directory.

Output includes:
- Identity and handoff bead info
- Attached totem (if any)
- Progress through steps
- Current step that should be worked on next

Examples:
  hd totem current                 # Current agent's work
  hd totem current greenplace/furiosa
  hd totem current shaman
  hd mol current greenplace/witness`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMoleculeCurrent,
}


var moleculeBurnCmd = &cobra.Command{
	Use:   "burn [target]",
	Short: "Burn current totem without creating a digest",
	Long: `Burn (destroy) the current totem attachment.

This discards the totem without creating a permanent record. Use this
when abandoning work or when a totem doesn't need an audit trail.

If no target is specified, burns the current agent's attached totem.

For wisps, burning is the default completion action. For regular totems,
consider using 'squash' instead to preserve an audit trail.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMoleculeBurn,
}

var moleculeSquashCmd = &cobra.Command{
	Use:   "squash [target]",
	Short: "Compress totem into a digest",
	Long: `Squash the current totem into a permanent digest.

This condenses a completed totem's execution into a compact record.
The digest preserves:
- What totem was executed
- When it ran
- Summary of results

Use this for scout cycles and other operational work that should have
a permanent (but compact) record.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMoleculeSquash,
}

var moleculeStepCmd = &cobra.Command{
	Use:   "step",
	Short: "Totem step operations",
	RunE:  requireSubcommand,
	Long: `Commands for working with totem steps.

A totem is a DAG of steps. Each step is a relics issue with the totem root
as its parent. Steps can have dependencies on other steps.

When a raider is working on a totem, it processes one step at a time:
1. Work on the current step
2. When done: hd mol step done <step-id>
3. System auto-continues to next ready step

IMPORTANT: Always use 'hd totem step done' to complete steps. Do not manually
close steps with 'bd close' - that skips the auto-continuation logic.`,
}


func init() {
	// Progress flags
	moleculeProgressCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")

	// Attachment flags
	moleculeAttachmentCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")

	// Status flags
	moleculeStatusCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")

	// Current flags
	moleculeCurrentCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")

	// Burn flags
	moleculeBurnCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")

	// Squash flags
	moleculeSquashCmd.Flags().BoolVar(&moleculeJSON, "json", false, "Output as JSON")

	// Add step subcommand with its children
	moleculeStepCmd.AddCommand(moleculeStepDoneCmd)
	moleculeCmd.AddCommand(moleculeStepCmd)

	// Add subcommands (agent-specific operations only)
	moleculeCmd.AddCommand(moleculeStatusCmd)
	moleculeCmd.AddCommand(moleculeCurrentCmd)
	moleculeCmd.AddCommand(moleculeBurnCmd)
	moleculeCmd.AddCommand(moleculeSquashCmd)
	moleculeCmd.AddCommand(moleculeProgressCmd)
	moleculeCmd.AddCommand(moleculeAttachCmd)
	moleculeCmd.AddCommand(moleculeDetachCmd)
	moleculeCmd.AddCommand(moleculeAttachmentCmd)
	moleculeCmd.AddCommand(moleculeAttachFromMailCmd)

	rootCmd.AddCommand(moleculeCmd)
}
