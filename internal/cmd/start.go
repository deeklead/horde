package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/clan"
	"github.com/deeklead/horde/internal/daemon"
	"github.com/deeklead/horde/internal/shaman"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/warchief"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/forge"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/witness"
	"github.com/deeklead/horde/internal/workspace"
)

var (
	startAll               bool
	startAgentOverride     string
	startCrewRig           string
	startCrewAccount       string
	startCrewAgentOverride string
	shutdownGraceful       bool
	shutdownWait           int
	shutdownAll            bool
	shutdownForce          bool
	shutdownYes            bool
	shutdownRaidersOnly   bool
	shutdownNuclear        bool
)

var startCmd = &cobra.Command{
	Use:     "start [path]",
	GroupID: GroupServices,
	Short:   "Start Horde or a clan workspace",
	Long: `Start Horde by launching the Shaman and Warchief.

The Shaman is the health-check orchestrator that monitors Warchief and Witnesses.
The Warchief is the global coordinator that dispatches work.

By default, other agents (Witnesses, Refineries) are started lazily as needed.
Use --all to start Witnesses and Refineries for all registered warbands immediately.

Clan shortcut:
  If a path like "warband/clan/name" is provided, starts that clan workspace.
  This is equivalent to 'hd start clan warband/name'.

To stop Horde, use 'hd shutdown'.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStart,
}

var shutdownCmd = &cobra.Command{
	Use:     "shutdown",
	GroupID: GroupServices,
	Short:   "Shutdown Horde with cleanup",
	Long: `Shutdown Horde by stopping agents and cleaning up raiders.

This is the "done for the day" command - it stops everything AND removes
raider worktrees/branches. For a reversible pause, use 'hd down' instead.

Comparison:
  hd down      - Pause (stop processes, keep worktrees) - reversible
  hd shutdown  - Done (stop + cleanup worktrees) - permanent cleanup

After killing sessions, raiders are cleaned up:
  - Worktrees are removed
  - Raider branches are deleted
  - Raiders with uncommitted work are SKIPPED (protected)

Shutdown levels (progressively more aggressive):
  (default)       - Stop infrastructure + raiders + cleanup
  --all           - Also stop clan sessions
  --raiders-only - Only stop raiders (leaves infrastructure running)

Use --force or --yes to skip confirmation prompt.
Use --graceful to allow agents time to save state before killing.
Use --nuclear to force cleanup even if raiders have uncommitted work (DANGER).`,
	RunE: runShutdown,
}

var startCrewCmd = &cobra.Command{
	Use:   "clan <name>",
	Short: "Start a clan workspace (creates if needed)",
	Long: `Start a clan workspace, creating it if it doesn't exist.

This is a convenience command that combines 'hd clan add' and 'hd clan at --detached'.
The clan session starts in the background with Claude running and ready.

The name can include the warband in slash format (e.g., greenplace/joe).
If not specified, the warband is inferred from the current directory.

Examples:
  hd start clan joe                    # Start joe in current warband
  hd start clan greenplace/joe            # Start joe in horde warband
  hd start clan joe --warband relics        # Start joe in relics warband`,
	Args: cobra.ExactArgs(1),
	RunE: runStartCrew,
}

func init() {
	startCmd.Flags().BoolVarP(&startAll, "all", "a", false,
		"Also start Witnesses and Refineries for all warbands")
	startCmd.Flags().StringVar(&startAgentOverride, "agent", "", "Agent alias to run Warchief/Shaman with (overrides encampment default)")

	startCrewCmd.Flags().StringVar(&startCrewRig, "warband", "", "Warband to use")
	startCrewCmd.Flags().StringVar(&startCrewAccount, "account", "", "Claude Code account handle to use")
	startCrewCmd.Flags().StringVar(&startCrewAgentOverride, "agent", "", "Agent alias to run clan worker with (overrides warband/encampment default)")
	startCmd.AddCommand(startCrewCmd)

	shutdownCmd.Flags().BoolVarP(&shutdownGraceful, "graceful", "g", false,
		"Send ESC to agents and wait for them to handoff before killing")
	shutdownCmd.Flags().IntVarP(&shutdownWait, "wait", "w", 30,
		"Seconds to wait for graceful shutdown (default 30)")
	shutdownCmd.Flags().BoolVarP(&shutdownAll, "all", "a", false,
		"Also stop clan sessions (by default, clan is preserved)")
	shutdownCmd.Flags().BoolVarP(&shutdownForce, "force", "f", false,
		"Skip confirmation prompt (alias for --yes)")
	shutdownCmd.Flags().BoolVarP(&shutdownYes, "yes", "y", false,
		"Skip confirmation prompt")
	shutdownCmd.Flags().BoolVar(&shutdownRaidersOnly, "raiders-only", false,
		"Only stop raiders (minimal shutdown)")
	shutdownCmd.Flags().BoolVar(&shutdownNuclear, "nuclear", false,
		"Force cleanup even if raiders have uncommitted work (DANGER: may lose work)")

	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(shutdownCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	// Check if arg looks like a clan path (warband/clan/name)
	if len(args) == 1 && strings.Contains(args[0], "/clan/") {
		// Parse warband/clan/name format
		parts := strings.SplitN(args[0], "/clan/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			// Route to clan start with warband/name format
			crewArg := parts[0] + "/" + parts[1]
			return runStartCrew(cmd, []string{crewArg})
		}
	}

	// Verify we're in a Horde workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	if err := config.EnsureDaemonPatrolConfig(townRoot); err != nil {
		fmt.Printf("  %s Could not ensure daemon config: %v\n", style.Dim.Render("○"), err)
	}

	t := tmux.NewTmux()

	fmt.Printf("Starting Horde from %s\n\n", style.Dim.Render(townRoot))
	fmt.Println("Starting all agents in parallel...")
	fmt.Println()

	// Discover warbands once upfront to avoid redundant calls from parallel goroutines
	warbands, rigsErr := discoverAllRigs(townRoot)
	if rigsErr != nil {
		fmt.Printf("  %s Could not discover warbands: %v\n", style.Dim.Render("○"), rigsErr)
		// Continue anyway - core agents don't need warbands
	}

	// Start all agent groups in parallel for maximum speed
	var wg sync.WaitGroup
	var mu sync.Mutex // Protects stdout
	var coreErr error

	// Start core agents (Warchief and Shaman) in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startCoreAgents(townRoot, startAgentOverride, &mu); err != nil {
			mu.Lock()
			coreErr = err
			mu.Unlock()
		}
	}()

	// Start warband agents (witnesses, refineries) if --all
	if startAll && warbands != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startRigAgents(warbands, &mu)
		}()
	}

	// Start configured clan
	if warbands != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startConfiguredCrew(t, warbands, townRoot, &mu)
		}()
	}

	wg.Wait()

	if coreErr != nil {
		return coreErr
	}

	fmt.Println()
	fmt.Printf("%s Horde is running\n", style.Bold.Render("✓"))
	fmt.Println()
	fmt.Printf("  Summon to Warchief:  %s\n", style.Dim.Render("hd warchief summon"))
	fmt.Printf("  Summon to Shaman: %s\n", style.Dim.Render("hd shaman summon"))
	fmt.Printf("  Check status:     %s\n", style.Dim.Render("hd status"))

	return nil
}

// startCoreAgents starts Warchief and Shaman sessions in parallel using the Manager pattern.
// The mutex is used to synchronize output with other parallel startup operations.
func startCoreAgents(townRoot string, agentOverride string, mu *sync.Mutex) error {
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	// Start Warchief in goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		warchiefMgr := warchief.NewManager(townRoot)
		if err := warchiefMgr.Start(agentOverride); err != nil {
			if errors.Is(err, warchief.ErrAlreadyRunning) {
				mu.Lock()
				fmt.Printf("  %s Warchief already running\n", style.Dim.Render("○"))
				mu.Unlock()
			} else {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("starting Warchief: %w", err)
				}
				errMu.Unlock()
				mu.Lock()
				fmt.Printf("  %s Warchief failed: %v\n", style.Dim.Render("○"), err)
				mu.Unlock()
			}
		} else {
			mu.Lock()
			fmt.Printf("  %s Warchief started\n", style.Bold.Render("✓"))
			mu.Unlock()
		}
	}()

	// Start Shaman in goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		shamanMgr := shaman.NewManager(townRoot)
		if err := shamanMgr.Start(agentOverride); err != nil {
			if errors.Is(err, shaman.ErrAlreadyRunning) {
				mu.Lock()
				fmt.Printf("  %s Shaman already running\n", style.Dim.Render("○"))
				mu.Unlock()
			} else {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("starting Shaman: %w", err)
				}
				errMu.Unlock()
				mu.Lock()
				fmt.Printf("  %s Shaman failed: %v\n", style.Dim.Render("○"), err)
				mu.Unlock()
			}
		} else {
			mu.Lock()
			fmt.Printf("  %s Shaman started\n", style.Bold.Render("✓"))
			mu.Unlock()
		}
	}()

	wg.Wait()
	return firstErr
}

// startRigAgents starts witness and forge for all warbands in parallel.
// Called when --all flag is passed to hd start.
func startRigAgents(warbands []*warband.Warband, mu *sync.Mutex) {
	var wg sync.WaitGroup

	for _, r := range warbands {
		wg.Add(2) // Witness + Forge

		// Start Witness in goroutine
		go func(r *warband.Warband) {
			defer wg.Done()
			msg := startWitnessForRig(r)
			mu.Lock()
			fmt.Print(msg)
			mu.Unlock()
		}(r)

		// Start Forge in goroutine
		go func(r *warband.Warband) {
			defer wg.Done()
			msg := startForgeForRig(r)
			mu.Lock()
			fmt.Print(msg)
			mu.Unlock()
		}(r)
	}

	wg.Wait()
}

// startWitnessForRig starts the witness for a single warband and returns a status message.
func startWitnessForRig(r *warband.Warband) string {
	witMgr := witness.NewManager(r)
	if err := witMgr.Start(false, "", nil); err != nil {
		if errors.Is(err, witness.ErrAlreadyRunning) {
			return fmt.Sprintf("  %s %s witness already running\n", style.Dim.Render("○"), r.Name)
		}
		return fmt.Sprintf("  %s %s witness failed: %v\n", style.Dim.Render("○"), r.Name, err)
	}
	return fmt.Sprintf("  %s %s witness started\n", style.Bold.Render("✓"), r.Name)
}

// startForgeForRig starts the forge for a single warband and returns a status message.
func startForgeForRig(r *warband.Warband) string {
	forgeMgr := forge.NewManager(r)
	if err := forgeMgr.Start(false, ""); err != nil {
		if errors.Is(err, forge.ErrAlreadyRunning) {
			return fmt.Sprintf("  %s %s forge already running\n", style.Dim.Render("○"), r.Name)
		}
		return fmt.Sprintf("  %s %s forge failed: %v\n", style.Dim.Render("○"), r.Name, err)
	}
	return fmt.Sprintf("  %s %s forge started\n", style.Bold.Render("✓"), r.Name)
}

// startConfiguredCrew starts clan members configured in warband settings in parallel.
func startConfiguredCrew(t *tmux.Tmux, warbands []*warband.Warband, townRoot string, mu *sync.Mutex) {
	var wg sync.WaitGroup
	var startedAny int32 // Use atomic for thread-safe flag

	for _, r := range warbands {
		crewToStart := getCrewToStart(r)
		for _, crewName := range crewToStart {
			wg.Add(1)
			go func(r *warband.Warband, crewName string) {
				defer wg.Done()
				msg, started := startOrRestartCrewMember(t, r, crewName, townRoot)
				mu.Lock()
				fmt.Print(msg)
				mu.Unlock()
				if started {
					atomic.StoreInt32(&startedAny, 1)
				}
			}(r, crewName)
		}
	}

	wg.Wait()

	if atomic.LoadInt32(&startedAny) == 0 {
		mu.Lock()
		fmt.Printf("  %s No clan configured or all already running\n", style.Dim.Render("○"))
		mu.Unlock()
	}
}

// startOrRestartCrewMember starts or restarts a single clan member and returns a status message.
func startOrRestartCrewMember(t *tmux.Tmux, r *warband.Warband, crewName, townRoot string) (msg string, started bool) {
	sessionID := crewSessionName(r.Name, crewName)
	if running, _ := t.HasSession(sessionID); running {
		// Session exists - check if agent is still running
		agentCfg := config.ResolveRoleAgentConfig(constants.RoleCrew, townRoot, r.Path)
		if !t.IsAgentRunning(sessionID, config.ExpectedPaneCommands(agentCfg)...) {
			// Agent has exited, restart it
			// Build startup beacon for predecessor discovery via /resume
			address := fmt.Sprintf("%s/clan/%s", r.Name, crewName)
			beacon := session.FormatStartupNudge(session.StartupNudgeConfig{
				Recipient: address,
				Sender:    "human",
				Topic:     "restart",
			})
			agentCmd := config.BuildCrewStartupCommand(r.Name, crewName, r.Path, beacon)
			if err := t.SendKeys(sessionID, agentCmd); err != nil {
				return fmt.Sprintf("  %s %s/%s restart failed: %v\n", style.Dim.Render("○"), r.Name, crewName, err), false
			}
			return fmt.Sprintf("  %s %s/%s agent restarted\n", style.Bold.Render("✓"), r.Name, crewName), true
		}
		return fmt.Sprintf("  %s %s/%s already running\n", style.Dim.Render("○"), r.Name, crewName), false
	}

	if err := startCrewMember(r.Name, crewName, townRoot); err != nil {
		return fmt.Sprintf("  %s %s/%s failed: %v\n", style.Dim.Render("○"), r.Name, crewName, err), false
	}
	return fmt.Sprintf("  %s %s/%s started\n", style.Bold.Render("✓"), r.Name, crewName), true
}

// discoverAllRigs finds all warbands in the workspace.
func discoverAllRigs(townRoot string) ([]*warband.Warband, error) {
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		return nil, fmt.Errorf("loading warbands config: %w", err)
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)

	return rigMgr.DiscoverRigs()
}

func runShutdown(cmd *cobra.Command, args []string) error {
	t := tmux.NewTmux()

	// Find workspace root for raider cleanup
	townRoot, _ := workspace.FindFromCwd()

	// Collect sessions to show what will be stopped
	sessions, err := t.ListSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	// Get session names for categorization
	warchiefSession := getWarchiefSessionName()
	shamanSession := getShamanSessionName()
	toStop, preserved := categorizeSessions(sessions, warchiefSession, shamanSession)

	if len(toStop) == 0 {
		fmt.Printf("%s Horde was not running\n", style.Dim.Render("○"))
		return nil
	}

	// Show what will happen
	fmt.Println("Sessions to stop:")
	for _, sess := range toStop {
		fmt.Printf("  %s %s\n", style.Bold.Render("→"), sess)
	}
	if len(preserved) > 0 && !shutdownAll {
		fmt.Println()
		fmt.Println("Sessions preserved (clan):")
		for _, sess := range preserved {
			fmt.Printf("  %s %s\n", style.Dim.Render("○"), sess)
		}
	}
	fmt.Println()

	// Confirmation prompt
	if !shutdownYes && !shutdownForce {
		fmt.Printf("Proceed with shutdown? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Shutdown canceled.")
			return nil
		}
	}

	if shutdownGraceful {
		return runGracefulShutdown(t, toStop, townRoot)
	}
	return runImmediateShutdown(t, toStop, townRoot)
}

// categorizeSessions splits sessions into those to stop and those to preserve.
// warchiefSession and shamanSession are the dynamic session names for the current encampment.
func categorizeSessions(sessions []string, warchiefSession, shamanSession string) (toStop, preserved []string) {
	for _, sess := range sessions {
		// Horde sessions use gt- (warband-level) or hq- (encampment-level) prefix
		if !strings.HasPrefix(sess, "hd-") && !strings.HasPrefix(sess, "hq-") {
			continue // Not a Horde session
		}

		// Check if it's a clan session (pattern: gt-<warband>-clan-<name>)
		isCrew := strings.Contains(sess, "-clan-")

		// Check if it's a raider session (pattern: gt-<warband>-<name> where name is not clan/witness/forge)
		isRaider := false
		if !isCrew && sess != warchiefSession && sess != shamanSession {
			parts := strings.Split(sess, "-")
			if len(parts) >= 3 {
				role := parts[2]
				if role != "witness" && role != "forge" && role != "clan" {
					isRaider = true
				}
			}
		}

		// Decide based on flags
		if shutdownRaidersOnly {
			// Only stop raiders
			if isRaider {
				toStop = append(toStop, sess)
			} else {
				preserved = append(preserved, sess)
			}
		} else if shutdownAll {
			// Stop everything including clan
			toStop = append(toStop, sess)
		} else {
			// Default: preserve clan
			if isCrew {
				preserved = append(preserved, sess)
			} else {
				toStop = append(toStop, sess)
			}
		}
	}
	return
}

func runGracefulShutdown(t *tmux.Tmux, gtSessions []string, townRoot string) error {
	fmt.Printf("Graceful shutdown of Horde (waiting up to %ds)...\n\n", shutdownWait)

	// Phase 1: Send ESC to all agents to interrupt them
	fmt.Printf("Phase 1: Sending ESC to %d agent(s)...\n", len(gtSessions))
	for _, sess := range gtSessions {
		fmt.Printf("  %s Interrupting %s\n", style.Bold.Render("→"), sess)
		_ = t.SendKeysRaw(sess, "Escape") // best-effort interrupt
	}

	// Phase 2: Send shutdown message asking agents to handoff
	fmt.Printf("\nPhase 2: Requesting handoff from agents...\n")
	shutdownMsg := "[SHUTDOWN] Horde is shutting down. Please save your state and update your handoff bead, then type /exit or wait to be terminated."
	for _, sess := range gtSessions {
		// Small delay then send the message
		time.Sleep(constants.ShutdownNotifyDelay)
		_ = t.SendKeys(sess, shutdownMsg) // best-effort notification
	}

	// Phase 3: Wait for agents to complete handoff
	fmt.Printf("\nPhase 3: Waiting %ds for agents to complete handoff...\n", shutdownWait)
	fmt.Printf("  %s\n", style.Dim.Render("(Press Ctrl-C to force immediate shutdown)"))

	// Wait with countdown
	for remaining := shutdownWait; remaining > 0; remaining -= 5 {
		if remaining < shutdownWait {
			fmt.Printf("  %s %ds remaining...\n", style.Dim.Render("⏳"), remaining)
		}
		sleepTime := 5
		if remaining < 5 {
			sleepTime = remaining
		}
		time.Sleep(time.Duration(sleepTime) * time.Second)
	}

	// Phase 4: Kill sessions in correct order
	fmt.Printf("\nPhase 4: Terminating sessions...\n")
	warchiefSession := getWarchiefSessionName()
	shamanSession := getShamanSessionName()
	stopped := killSessionsInOrder(t, gtSessions, warchiefSession, shamanSession)

	// Phase 5: Cleanup raider worktrees and branches
	fmt.Printf("\nPhase 5: Cleaning up raiders...\n")
	if townRoot != "" {
		cleanupRaiders(townRoot)
	}

	// Phase 6: Stop the daemon
	fmt.Printf("\nPhase 6: Stopping daemon...\n")
	if townRoot != "" {
		stopDaemonIfRunning(townRoot)
	}

	fmt.Println()
	fmt.Printf("%s Graceful shutdown complete (%d sessions stopped)\n", style.Bold.Render("✓"), stopped)
	return nil
}

func runImmediateShutdown(t *tmux.Tmux, gtSessions []string, townRoot string) error {
	fmt.Println("Shutting down Horde...")

	warchiefSession := getWarchiefSessionName()
	shamanSession := getShamanSessionName()
	stopped := killSessionsInOrder(t, gtSessions, warchiefSession, shamanSession)

	// Cleanup raider worktrees and branches
	if townRoot != "" {
		fmt.Println()
		fmt.Println("Cleaning up raiders...")
		cleanupRaiders(townRoot)
	}

	// Stop the daemon
	if townRoot != "" {
		fmt.Println()
		fmt.Println("Stopping daemon...")
		stopDaemonIfRunning(townRoot)
	}

	fmt.Println()
	fmt.Printf("%s Horde shutdown complete (%d sessions stopped)\n", style.Bold.Render("✓"), stopped)

	return nil
}

// killSessionsInOrder stops sessions in the correct order:
// 1. Shaman first (so it doesn't restart others)
// 2. Everything except Warchief
// 3. Warchief last
// warchiefSession and shamanSession are the dynamic session names for the current encampment.
func killSessionsInOrder(t *tmux.Tmux, sessions []string, warchiefSession, shamanSession string) int {
	stopped := 0

	// Helper to check if session is in our list
	inList := func(sess string) bool {
		for _, s := range sessions {
			if s == sess {
				return true
			}
		}
		return false
	}

	// 1. Stop Shaman first
	if inList(shamanSession) {
		if err := t.KillSessionWithProcesses(shamanSession); err == nil {
			fmt.Printf("  %s %s stopped\n", style.Bold.Render("✓"), shamanSession)
			stopped++
		}
	}

	// 2. Stop others (except Warchief)
	for _, sess := range sessions {
		if sess == shamanSession || sess == warchiefSession {
			continue
		}
		if err := t.KillSessionWithProcesses(sess); err == nil {
			fmt.Printf("  %s %s stopped\n", style.Bold.Render("✓"), sess)
			stopped++
		}
	}

	// 3. Stop Warchief last
	if inList(warchiefSession) {
		if err := t.KillSessionWithProcesses(warchiefSession); err == nil {
			fmt.Printf("  %s %s stopped\n", style.Bold.Render("✓"), warchiefSession)
			stopped++
		}
	}

	return stopped
}

// cleanupRaiders removes raider worktrees and branches for all warbands.
// It refuses to clean up raiders with uncommitted work unless --nuclear is set.
func cleanupRaiders(townRoot string) {
	// Load warbands config
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		fmt.Printf("  %s Could not load warbands config: %v\n", style.Dim.Render("○"), err)
		return
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)

	// Discover all warbands
	warbands, err := rigMgr.DiscoverRigs()
	if err != nil {
		fmt.Printf("  %s Could not discover warbands: %v\n", style.Dim.Render("○"), err)
		return
	}

	totalCleaned := 0
	totalSkipped := 0
	var uncommittedRaiders []string

	for _, r := range warbands {
		raiderGit := git.NewGit(r.Path)
		raiderMgr := raider.NewManager(r, raiderGit, nil) // nil tmux: just listing, not allocating

		raiders, err := raiderMgr.List()
		if err != nil {
			continue
		}

		for _, p := range raiders {
			// Check for uncommitted work
			pGit := git.NewGit(p.ClonePath)
			status, err := pGit.CheckUncommittedWork()
			if err != nil {
				// Can't check, be safe and skip unless nuclear
				if !shutdownNuclear {
					fmt.Printf("  %s %s/%s: could not check status, skipping\n",
						style.Dim.Render("○"), r.Name, p.Name)
					totalSkipped++
					continue
				}
			} else if !status.Clean() {
				// Has uncommitted work
				if !shutdownNuclear {
					uncommittedRaiders = append(uncommittedRaiders,
						fmt.Sprintf("%s/%s (%s)", r.Name, p.Name, status.String()))
					totalSkipped++
					continue
				}
				// Nuclear mode: warn but proceed
				fmt.Printf("  %s %s/%s: NUCLEAR - removing despite %s\n",
					style.Bold.Render("⚠"), r.Name, p.Name, status.String())
			}

			// Clean: remove worktree and branch
			if err := raiderMgr.RemoveWithOptions(p.Name, true, shutdownNuclear); err != nil {
				fmt.Printf("  %s %s/%s: cleanup failed: %v\n",
					style.Dim.Render("○"), r.Name, p.Name, err)
				totalSkipped++
				continue
			}

			// Delete the raider branch from warchief's clone
			branchName := fmt.Sprintf("raider/%s", p.Name)
			warchiefPath := filepath.Join(r.Path, "warchief", "warband")
			warchiefGit := git.NewGit(warchiefPath)
			_ = warchiefGit.DeleteBranch(branchName, true) // Ignore errors

			fmt.Printf("  %s %s/%s: cleaned up\n", style.Bold.Render("✓"), r.Name, p.Name)
			totalCleaned++
		}
	}

	// Summary
	if len(uncommittedRaiders) > 0 {
		fmt.Println()
		fmt.Printf("  %s Raiders with uncommitted work (use --nuclear to force):\n",
			style.Bold.Render("⚠"))
		for _, pc := range uncommittedRaiders {
			fmt.Printf("    • %s\n", pc)
		}
	}

	if totalCleaned > 0 || totalSkipped > 0 {
		fmt.Printf("  Cleaned: %d, Skipped: %d\n", totalCleaned, totalSkipped)
	} else {
		fmt.Printf("  %s No raiders to clean up\n", style.Dim.Render("○"))
	}
}

// stopDaemonIfRunning stops the daemon if it is running.
// This prevents the daemon from restarting agents after shutdown.
func stopDaemonIfRunning(townRoot string) {
	running, _, _ := daemon.IsRunning(townRoot)
	if running {
		if err := daemon.StopDaemon(townRoot); err != nil {
			fmt.Printf("  %s Daemon: %s\n", style.Dim.Render("○"), err.Error())
		} else {
			fmt.Printf("  %s Daemon stopped\n", style.Bold.Render("✓"))
		}
	} else {
		fmt.Printf("  %s Daemon not running\n", style.Dim.Render("○"))
	}
}

// runStartCrew starts a clan workspace, creating it if it doesn't exist.
// This combines the functionality of 'hd clan add' and 'hd clan at --detached'.
func runStartCrew(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Parse warband/name format (e.g., "greenplace/joe" -> warband=horde, name=joe)
	rigName := startCrewRig
	if parsedRig, crewName, ok := parseRigSlashName(name); ok {
		if rigName == "" {
			rigName = parsedRig
		}
		name = crewName
	}

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// If warband still not specified, try to infer from cwd
	if rigName == "" {
		rigName, err = inferRigFromCwd(townRoot)
		if err != nil {
			return fmt.Errorf("could not determine warband (use --warband flag or warband/name format): %w", err)
		}
	}

	// Load warbands config
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	// Get warband
	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(rigName)
	if err != nil {
		return fmt.Errorf("warband '%s' not found", rigName)
	}

	// Create clan manager
	crewGit := git.NewGit(r.Path)
	crewMgr := clan.NewManager(r, crewGit)

	// Resolve account for Claude config
	accountsPath := constants.WarchiefAccountsPath(townRoot)
	claudeConfigDir, accountHandle, err := config.ResolveAccountConfigDir(accountsPath, startCrewAccount)
	if err != nil {
		return fmt.Errorf("resolving account: %w", err)
	}
	if accountHandle != "" {
		fmt.Printf("Using account: %s\n", accountHandle)
	}

	// Use manager's Start() method - handles workspace creation, settings, and session
	err = crewMgr.Start(name, clan.StartOptions{
		Account:         startCrewAccount,
		ClaudeConfigDir: claudeConfigDir,
		AgentOverride:   startCrewAgentOverride,
	})
	if err != nil {
		if errors.Is(err, clan.ErrSessionRunning) {
			fmt.Printf("%s Session already running: %s\n", style.Dim.Render("○"), crewMgr.SessionName(name))
		} else {
			return err
		}
	} else {
		fmt.Printf("%s Started clan workspace: %s/%s\n",
			style.Bold.Render("✓"), rigName, name)
	}

	fmt.Printf("Summon with: %s\n", style.Dim.Render(fmt.Sprintf("hd clan at %s", name)))
	return nil
}

// getCrewToStart reads warband settings and parses the clan.startup field.
// Returns a list of clan names to start.
func getCrewToStart(r *warband.Warband) []string {
	// Load warband settings
	settingsPath := filepath.Join(r.Path, "settings", "config.json")
	settings, err := config.LoadRigSettings(settingsPath)
	if err != nil {
		return nil
	}

	if settings.Clan == nil || settings.Clan.Startup == "" || settings.Clan.Startup == "none" {
		return nil
	}

	startup := settings.Clan.Startup

	// Handle "all" - list all existing clan
	if startup == "all" {
		crewGit := git.NewGit(r.Path)
		crewMgr := clan.NewManager(r, crewGit)
		workers, err := crewMgr.List()
		if err != nil {
			return nil
		}
		var names []string
		for _, w := range workers {
			names = append(names, w.Name)
		}
		return names
	}

	// Parse names: "max", "max and joe", "max, joe", "max, joe, emma"
	// Replace "and" with comma for uniform parsing
	startup = strings.ReplaceAll(startup, " and ", ", ")
	parts := strings.Split(startup, ",")

	var names []string
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			names = append(names, name)
		}
	}

	return names
}

// startCrewMember starts a single clan member, creating if needed.
// This is a simplified version of runStartCrew that doesn't print output.
func startCrewMember(rigName, crewName, townRoot string) error {
	// Load warbands config
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	// Get warband
	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(rigName)
	if err != nil {
		return fmt.Errorf("warband '%s' not found", rigName)
	}

	// Create clan manager and use Start() method
	crewGit := git.NewGit(r.Path)
	crewMgr := clan.NewManager(r, crewGit)

	// Start handles workspace creation, settings, and session all in one
	err = crewMgr.Start(crewName, clan.StartOptions{})
	if err != nil && !errors.Is(err, clan.ErrSessionRunning) {
		return err
	}

	return nil
}
