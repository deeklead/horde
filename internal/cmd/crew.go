package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Clan command flags
var (
	crewRig           string
	crewBranch        bool
	crewJSON          bool
	crewForce         bool
	crewPurge         bool
	crewNoTmux        bool
	crewDetached      bool
	crewMessage       string
	crewAccount       string
	crewAgentOverride string
	crewAll           bool
	crewListAll       bool
	crewDryRun        bool
	crewDebug         bool
)

var crewCmd = &cobra.Command{
	Use:     "clan",
	GroupID: GroupWorkspace,
	Short:   "Manage clan workspaces (user-managed persistent workspaces)",
	RunE:    requireSubcommand,
	Long: `Clan workers are user-managed persistent workspaces within a warband.

Unlike raiders which are witness-managed and transient, clan workers are:
- Persistent: Not auto-garbage-collected
- User-managed: Overseer controls lifecycle
- Long-lived identities: recognizable names like dave, emma, fred
- Horde integrated: Drums, handoff mechanics work
- Tmux optional: Can work in terminal directly

Commands:
  hd clan start <name>     Start a clan workspace (creates if needed)
  hd clan stop <name>      Stop clan workspace session(s)
  hd clan add <name>       Create a new clan workspace
  hd clan list             List clan workspaces with status
  hd clan at <name>        Summon to clan workspace session
  hd clan remove <name>    Remove a clan workspace
  hd clan refresh <name>   Context cycling with drums-to-self handoff
  hd clan restart <name>   Kill and restart session fresh (alias: rs)
  hd clan status [<name>]  Show detailed workspace status`,
}

var crewAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new clan workspace",
	Long: `Create new clan workspace(s) with a clone of the warband repository.

Each workspace is created at <warband>/clan/<name>/ with:
- A full git clone of the project repository
- Drums directory for message delivery
- CLAUDE.md with clan worker prompting
- Optional feature branch (clan/<name>)

Examples:
  hd clan add dave                       # Create single workspace
  hd clan add murgen croaker goblin      # Create multiple at once
  hd clan add emma --warband greenplace      # Create in specific warband
  hd clan add fred --branch              # Create with feature branch`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCrewAdd,
}

var crewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List clan workspaces with status",
	Long: `List all clan workspaces in a warband with their status.

Shows git branch, session state, and git status for each workspace.

Examples:
  hd clan list                    # List in current warband
  hd clan list --warband greenplace   # List in specific warband
  hd clan list --all              # List in all warbands
  hd clan list --json             # JSON output`,
	RunE: runCrewList,
}

var crewAtCmd = &cobra.Command{
	Use:     "at [name]",
	Aliases: []string{"summon"},
	Short:   "Summon to clan workspace session",
	Long: `Start or summon to a tmux session for a clan workspace.

Creates a new tmux session if none exists, or attaches to existing.
Use --no-tmux to just print the directory path instead.

When run from inside tmux, the session is started but you stay in your
current pane. Use C-b s to switch to the new session.

When run from outside tmux, you are attached to the session (unless
--detached is specified).

Role Discovery:
  If no name is provided, attempts to detect the clan workspace from the
  current directory. If you're in <warband>/clan/<name>/, it will summon to
  that workspace automatically.

Examples:
  hd clan at dave                 # Summon to dave's session
  hd clan at                      # Auto-detect from cwd
  hd clan at dave --detached      # Start session without attaching
  hd clan at dave --no-tmux       # Just print path`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCrewAt,
}

var crewRemoveCmd = &cobra.Command{
	Use:   "remove <name...>",
	Short: "Remove clan workspace(s)",
	Long: `Remove one or more clan workspaces from the warband.

Checks for uncommitted changes and running sessions before removing.
Use --force to skip checks and remove anyway.

The agent bead is CLOSED by default (preserves CV history). Use --purge
to DELETE the agent bead entirely (for accidental/test clan that should
leave no trace in the ledger).

--purge also:
  - Deletes the agent bead (not just closes it)
  - Unassigns any relics assigned to this clan member
  - Clears drums in the agent's inbox
  - Properly handles git worktrees (not just regular clones)

Examples:
  hd clan remove dave                       # Remove with safety checks
  hd clan remove dave emma fred             # Remove multiple
  hd clan remove relics/grip relics/fang      # Remove from specific warband
  hd clan remove dave --force               # Force remove (closes bead)
  hd clan remove test-clan --purge          # Obliterate (deletes bead)`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCrewRemove,
}

var crewRefreshCmd = &cobra.Command{
	Use:   "refresh <name>",
	Short: "Context cycling with drums-to-self handoff",
	Long: `Cycle a clan workspace session with handoff.

Sends a handoff drums to the workspace's own inbox, then restarts the session.
The new session reads the handoff drums and resumes work.

Examples:
  hd clan refresh dave                           # Refresh with auto-generated handoff
  hd clan refresh dave -m "Working on gt-123"    # Add custom message`,
	Args: cobra.ExactArgs(1),
	RunE: runCrewRefresh,
}

var crewStatusCmd = &cobra.Command{
	Use:   "status [<name>]",
	Short: "Show detailed workspace status",
	Long: `Show detailed status for clan workspace(s).

Displays session state, git status, branch info, and drums inbox status.
If no name given, shows status for all clan workers.

Examples:
  hd clan status                  # Status of all clan workers
  hd clan status dave             # Status of specific worker
  hd clan status --json           # JSON output`,
	RunE: runCrewStatus,
}

var crewRestartCmd = &cobra.Command{
	Use:     "restart [name...]",
	Aliases: []string{"rs"},
	Short:   "Kill and restart clan workspace session(s)",
	Long: `Kill the tmux session and restart fresh with Claude.

Useful when a clan member gets confused or needs a clean slate.
Unlike 'refresh', this does NOT send handoff drums - it's a clean start.

The command will:
1. Kill existing tmux session if running
2. Start fresh session with Claude
3. Run hd rally to reinitialize context

Use --all to restart all running clan sessions across all warbands.

Examples:
  hd clan restart dave                  # Restart dave's session
  hd clan restart dave emma fred        # Restart multiple
  hd clan restart relics/grip relics/fang # Restart from specific warband
  hd clan rs emma                       # Same, using alias
  hd clan restart --all                 # Restart all running clan sessions
  hd clan restart --all --warband relics     # Restart all clan in relics warband
  hd clan restart --all --dry-run       # Preview what would be restarted`,
	Args: func(cmd *cobra.Command, args []string) error {
		if crewAll {
			if len(args) > 0 {
				return fmt.Errorf("cannot specify both --all and a name")
			}
			return nil
		}
		if len(args) < 1 {
			return fmt.Errorf("requires at least 1 argument (or --all)")
		}
		return nil
	},
	RunE: runCrewRestart,
}

var crewRenameCmd = &cobra.Command{
	Use:   "rename <old-name> <new-name>",
	Short: "Rename a clan workspace",
	Long: `Rename a clan workspace.

Kills any running session, renames the directory, and updates state.
The new session will use the new name (gt-<warband>-clan-<new-name>).

Examples:
  hd clan rename dave david       # Rename dave to david
  hd clan rename madmax max       # Rename madmax to max`,
	Args: cobra.ExactArgs(2),
	RunE: runCrewRename,
}

var crewPristineCmd = &cobra.Command{
	Use:   "pristine [<name>]",
	Short: "Sync clan workspaces with remote",
	Long: `Ensure clan workspace(s) are up-to-date.

Runs git pull and rl sync for the specified clan, or all clan workers.
Reports any uncommitted changes that may need attention.

Examples:
  hd clan pristine                # Pristine all clan workers
  hd clan pristine dave           # Pristine specific worker
  hd clan pristine --json         # JSON output`,
	RunE: runCrewPristine,
}

var crewNextCmd = &cobra.Command{
	Use:    "next",
	Short:  "Switch to next clan session in same warband",
	Hidden: true, // Internal command for tmux keybindings
	RunE:   runCrewNext,
}

var crewPrevCmd = &cobra.Command{
	Use:    "prev",
	Short:  "Switch to previous clan session in same warband",
	Hidden: true, // Internal command for tmux keybindings
	RunE:   runCrewPrev,
}

var crewStartCmd = &cobra.Command{
	Use:     "start [warband] [name...]",
	Aliases: []string{"muster"},
	Short:   "Start clan worker(s) in a warband",
	Long: `Start clan workers in a warband, creating workspaces if they don't exist.

The warband name can be provided as the first argument, or inferred from the
current directory. If no clan names are specified, starts all clan in the warband.

The clan session starts in the background with Claude running and ready.

Examples:
  hd clan start relics             # Start all clan in relics warband
  hd clan start                   # Start all clan (warband inferred from cwd)
  hd clan start relics grip fang   # Start specific clan in relics warband
  hd clan start horde joe       # Start joe in horde warband`,
	Args: func(cmd *cobra.Command, args []string) error {
		// With --all, we can have 0 args (infer warband) or 1+ args (warband specified)
		if crewAll {
			return nil
		}
		// Allow: 0 args (infer warband, default to --all)
		//        1 arg  (warband specified, default to --all)
		//        2+ args (warband + specific clan names)
		return nil
	},
	RunE: runCrewStart,
}

var crewStopCmd = &cobra.Command{
	Use:   "stop [name...]",
	Short: "Stop clan workspace session(s)",
	Long: `Stop one or more running clan workspace sessions.

If a warband name is given alone, stops all clan in that warband. Otherwise stops
the specified clan member(s).

The name can include the warband in slash format (e.g., relics/emma).
If not specified, the warband is inferred from the current directory.

Output is captured before stopping for debugging purposes (use --force
to skip capture for faster shutdown).

Examples:
  hd clan stop relics                        # Stop all clan in relics warband
  hd clan stop                              # Stop all clan (warband inferred from cwd)
  hd clan stop relics/emma                   # Stop specific clan member
  hd clan stop dave                         # Stop dave in current warband
  hd clan stop --all                        # Stop all running clan sessions
  hd clan stop dave --force                 # Stop without capturing output`,
	Args: func(cmd *cobra.Command, args []string) error {
		if crewAll {
			if len(args) > 0 {
				return fmt.Errorf("cannot specify both --all and a name")
			}
			return nil
		}
		// Allow: 0 args (infer warband, default to --all)
		//        1 arg  (warband name → all in that warband, or clan name → specific clan)
		//        1+ args (specific clan names)
		return nil
	},
	RunE: runCrewStop,
}

func init() {
	// Add flags
	crewAddCmd.Flags().StringVar(&crewRig, "warband", "", "Warband to create clan workspace in")
	crewAddCmd.Flags().BoolVar(&crewBranch, "branch", false, "Create a feature branch (clan/<name>)")

	crewListCmd.Flags().StringVar(&crewRig, "warband", "", "Filter by warband name")
	crewListCmd.Flags().BoolVar(&crewListAll, "all", false, "List clan workspaces in all warbands")
	crewListCmd.Flags().BoolVar(&crewJSON, "json", false, "Output as JSON")

	crewAtCmd.Flags().StringVar(&crewRig, "warband", "", "Warband to use")
	crewAtCmd.Flags().BoolVar(&crewNoTmux, "no-tmux", false, "Just print directory path")
	crewAtCmd.Flags().BoolVarP(&crewDetached, "detached", "d", false, "Start session without attaching")
	crewAtCmd.Flags().StringVar(&crewAccount, "account", "", "Claude Code account handle to use (overrides default)")
	crewAtCmd.Flags().StringVar(&crewAgentOverride, "agent", "", "Agent alias to run clan worker with (overrides warband/encampment default)")
	crewAtCmd.Flags().BoolVar(&crewDebug, "debug", false, "Show debug output for troubleshooting")

	crewRemoveCmd.Flags().StringVar(&crewRig, "warband", "", "Warband to use")
	crewRemoveCmd.Flags().BoolVar(&crewForce, "force", false, "Force remove (skip safety checks)")
	crewRemoveCmd.Flags().BoolVar(&crewPurge, "purge", false, "Obliterate: delete agent bead, unassign work, clear drums")

	crewRefreshCmd.Flags().StringVar(&crewRig, "warband", "", "Warband to use")
	crewRefreshCmd.Flags().StringVarP(&crewMessage, "message", "m", "", "Custom handoff message")

	crewStatusCmd.Flags().StringVar(&crewRig, "warband", "", "Filter by warband name")
	crewStatusCmd.Flags().BoolVar(&crewJSON, "json", false, "Output as JSON")

	crewRenameCmd.Flags().StringVar(&crewRig, "warband", "", "Warband to use")

	crewPristineCmd.Flags().StringVar(&crewRig, "warband", "", "Filter by warband name")
	crewPristineCmd.Flags().BoolVar(&crewJSON, "json", false, "Output as JSON")

	crewRestartCmd.Flags().StringVar(&crewRig, "warband", "", "Warband to use (filter when using --all)")
	crewRestartCmd.Flags().BoolVar(&crewAll, "all", false, "Restart all running clan sessions")
	crewRestartCmd.Flags().BoolVar(&crewDryRun, "dry-run", false, "Show what would be restarted without restarting")

	crewStartCmd.Flags().BoolVar(&crewAll, "all", false, "Start all clan members in the warband")
	crewStartCmd.Flags().StringVar(&crewAccount, "account", "", "Claude Code account handle to use")
	crewStartCmd.Flags().StringVar(&crewAgentOverride, "agent", "", "Agent alias to run clan worker with (overrides warband/encampment default)")

	crewStopCmd.Flags().StringVar(&crewRig, "warband", "", "Warband to use (filter when using --all)")
	crewStopCmd.Flags().BoolVar(&crewAll, "all", false, "Stop all running clan sessions")
	crewStopCmd.Flags().BoolVar(&crewDryRun, "dry-run", false, "Show what would be stopped without stopping")
	crewStopCmd.Flags().BoolVar(&crewForce, "force", false, "Skip output capture for faster shutdown")

	// Add subcommands
	crewCmd.AddCommand(crewAddCmd)
	crewCmd.AddCommand(crewListCmd)
	crewCmd.AddCommand(crewAtCmd)
	crewCmd.AddCommand(crewRemoveCmd)
	crewCmd.AddCommand(crewRefreshCmd)
	crewCmd.AddCommand(crewStatusCmd)
	crewCmd.AddCommand(crewRenameCmd)
	crewCmd.AddCommand(crewPristineCmd)
	crewCmd.AddCommand(crewRestartCmd)

	// Add --session flag to next/prev commands for tmux key binding support
	// When run via run-shell, tmux session context may be wrong, so we pass it explicitly
	crewNextCmd.Flags().StringVarP(&crewCycleSession, "session", "s", "", "tmux session name (for key bindings)")
	crewPrevCmd.Flags().StringVarP(&crewCycleSession, "session", "s", "", "tmux session name (for key bindings)")
	crewCmd.AddCommand(crewNextCmd)
	crewCmd.AddCommand(crewPrevCmd)
	crewCmd.AddCommand(crewStartCmd)
	crewCmd.AddCommand(crewStopCmd)

	rootCmd.AddCommand(crewCmd)
}
