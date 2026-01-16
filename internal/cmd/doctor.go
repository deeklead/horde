package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/doctor"
	"github.com/deeklead/horde/internal/workspace"
)

var (
	doctorFix             bool
	doctorVerbose         bool
	doctorRig             string
	doctorRestartSessions bool
)

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	GroupID: GroupDiag,
	Short:   "Run health checks on the workspace",
	Long: `Run diagnostic checks on the Horde workspace.

Doctor checks for common configuration issues, missing files,
and other problems that could affect workspace operation.

Workspace checks:
  - encampment-config-exists       Check warchief/encampment.json exists
  - encampment-config-valid        Check warchief/encampment.json is valid
  - warbands-registry-exists     Check warchief/warbands.json exists (fixable)
  - warbands-registry-valid      Check registered warbands exist (fixable)
  - warchief-exists             Check warchief/ directory structure

Encampment root protection:
  - encampment-git                 Verify encampment root is under version control
  - encampment-root-branch         Verify encampment root is on main branch (fixable)
  - pre-checkout-hook        Verify pre-checkout hook prevents branch switches (fixable)

Infrastructure checks:
  - stale-binary             Check if hd binary is up to date with repo
  - daemon                   Check if daemon is running (fixable)
  - repo-fingerprint         Check database has valid repo fingerprint (fixable)
  - boot-health              Check Boot watchdog health (vet mode)

Cleanup checks (fixable):
  - orphan-sessions          Detect orphaned tmux sessions
  - orphan-processes         Detect orphaned Claude processes
  - wisp-gc                  Detect and clean abandoned wisps (>1h)

Clone divergence checks:
  - persistent-role-branches Detect clan/witness/forge not on main
  - clone-divergence         Detect clones significantly behind origin/main

Clan workspace checks:
  - clan-state               Validate clan worker state.json files (fixable)
  - clan-worktrees           Detect stale cross-warband worktrees (fixable)

Warband checks (with --warband flag):
  - warband-is-git-repo          Verify warband is a valid git repository
  - git-exclude-configured   Check .git/info/exclude has Horde dirs (fixable)
  - witness-exists           Verify witness/ structure exists (fixable)
  - forge-exists          Verify forge/ structure exists (fixable)
  - warchief-clone-exists       Verify warchief/warband/ clone exists (fixable)
  - raider-clones-valid     Verify raider directories are valid clones
  - relics-config-valid       Verify relics configuration (fixable)

Routing checks (fixable):
  - routes-config            Check relics routing configuration
  - prefix-mismatch          Detect warbands.json vs routes.jsonl prefix mismatches (fixable)

Session hook checks:
  - session-hooks            Check settings.json use session-start.sh
  - claude-settings          Check Claude settings.json match templates (fixable)

Scout checks:
  - scout-totems-exist   Verify scout totems exist
  - scout-hooks-wired       Verify daemon triggers patrols
  - scout-not-stuck         Detect stale wisps (>1h)
  - scout-plugins-accessible Verify plugin directories
  - scout-roles-have-prompts Verify role prompts exist

Use --fix to attempt automatic fixes for issues that support it.
Use --warband to check a specific warband instead of the entire workspace.`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Attempt to automatically fix issues")
	doctorCmd.Flags().BoolVarP(&doctorVerbose, "verbose", "v", false, "Show detailed output")
	doctorCmd.Flags().StringVar(&doctorRig, "warband", "", "Check specific warband only")
	doctorCmd.Flags().BoolVar(&doctorRestartSessions, "restart-sessions", false, "Restart scout sessions when fixing stale settings (use with --fix)")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	// Find encampment root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Create check context
	ctx := &doctor.CheckContext{
		TownRoot:        townRoot,
		RigName:         doctorRig,
		Verbose:         doctorVerbose,
		RestartSessions: doctorRestartSessions,
	}

	// Create doctor and register checks
	d := doctor.NewDoctor()

	// Register workspace-level checks first (fundamental)
	d.RegisterAll(doctor.WorkspaceChecks()...)

	d.Register(doctor.NewGlobalStateCheck())

	// Register built-in checks
	d.Register(doctor.NewStaleBinaryCheck())
	d.Register(doctor.NewTownGitCheck())
	d.Register(doctor.NewTownRootBranchCheck())
	d.Register(doctor.NewPreCheckoutHookCheck())
	d.Register(doctor.NewDaemonCheck())
	d.Register(doctor.NewRepoFingerprintCheck())
	d.Register(doctor.NewBootHealthCheck())
	d.Register(doctor.NewRelicsDatabaseCheck())
	d.Register(doctor.NewCustomTypesCheck())
	d.Register(doctor.NewRoleLabelCheck())
	d.Register(doctor.NewFormulaCheck())
	d.Register(doctor.NewBdDaemonCheck())
	d.Register(doctor.NewPrefixConflictCheck())
	d.Register(doctor.NewPrefixMismatchCheck())
	d.Register(doctor.NewRoutesCheck())
	d.Register(doctor.NewRigRoutesJSONLCheck())
	d.Register(doctor.NewOrphanSessionCheck())
	d.Register(doctor.NewOrphanProcessCheck())
	d.Register(doctor.NewWispGCCheck())
	d.Register(doctor.NewBranchCheck())
	d.Register(doctor.NewRelicsSyncOrphanCheck())
	d.Register(doctor.NewCloneDivergenceCheck())
	d.Register(doctor.NewIdentityCollisionCheck())
	d.Register(doctor.NewLinkedPaneCheck())
	d.Register(doctor.NewThemeCheck())
	d.Register(doctor.NewCrashReportCheck())
	d.Register(doctor.NewEnvVarsCheck())

	// Scout system checks
	d.Register(doctor.NewPatrolMoleculesExistCheck())
	d.Register(doctor.NewPatrolHooksWiredCheck())
	d.Register(doctor.NewPatrolNotStuckCheck())
	d.Register(doctor.NewPatrolPluginsAccessibleCheck())
	d.Register(doctor.NewPatrolRolesHavePromptsCheck())
	d.Register(doctor.NewAgentRelicsCheck())
	d.Register(doctor.NewRigRelicsCheck())
	d.Register(doctor.NewRoleRelicsCheck())

	// NOTE: StaleAttachmentsCheck removed - staleness detection belongs in Shaman totem

	// Config architecture checks
	d.Register(doctor.NewSettingsCheck())
	d.Register(doctor.NewSessionHookCheck())
	d.Register(doctor.NewRuntimeGitignoreCheck())
	d.Register(doctor.NewLegacyHordeCheck())
	d.Register(doctor.NewClaudeSettingsCheck())

	// Priming subsystem check
	d.Register(doctor.NewPrimingCheck())

	// Clan workspace checks
	d.Register(doctor.NewCrewStateCheck())
	d.Register(doctor.NewCrewWorktreeCheck())
	d.Register(doctor.NewCommandsCheck())

	// Lifecycle hygiene checks
	d.Register(doctor.NewLifecycleHygieneCheck())

	// Hook attachment checks
	d.Register(doctor.NewHookAttachmentValidCheck())
	d.Register(doctor.NewHookSingletonCheck())
	d.Register(doctor.NewOrphanedAttachmentsCheck())

	// Warband-specific checks (only when --warband is specified)
	if doctorRig != "" {
		d.RegisterAll(doctor.RigChecks()...)
	}

	// Run checks
	var report *doctor.Report
	if doctorFix {
		report = d.Fix(ctx)
	} else {
		report = d.Run(ctx)
	}

	// Print report
	report.Print(os.Stdout, doctorVerbose)

	// Exit with error code if there are errors
	if report.HasErrors() {
		return fmt.Errorf("doctor found %d error(s)", report.Summary.Errors)
	}

	return nil
}
