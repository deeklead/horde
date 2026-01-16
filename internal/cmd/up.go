package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/clan"
	"github.com/deeklead/horde/internal/daemon"
	"github.com/deeklead/horde/internal/shaman"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/warchief"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/forge"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/witness"
	"github.com/deeklead/horde/internal/workspace"
)

// agentStartResult holds the result of starting an agent.
type agentStartResult struct {
	name   string // Display name like "Witness (horde)"
	ok     bool   // Whether start succeeded
	detail string // Status detail (session name or error)
}

// maxConcurrentAgentStarts limits parallel agent startups to avoid resource exhaustion.
const maxConcurrentAgentStarts = 10

var upCmd = &cobra.Command{
	Use:     "up",
	GroupID: GroupServices,
	Short:   "Bring up all Horde services",
	Long: `Start all Horde long-lived services.

This is the idempotent "boot" command for Horde. It ensures all
infrastructure agents are running:

  • Daemon     - Go background process that pokes agents
  • Shaman     - Health orchestrator (monitors Warchief/Witnesses)
  • Warchief      - Global work coordinator
  • Witnesses  - Per-warband raider managers
  • Refineries - Per-warband merge queue processors

Raiders are NOT started by this command - they are transient workers
spawned on demand by the Warchief or Witnesses.

Use --restore to also start:
  • Clan       - Per warband settings (settings/config.json clan.startup)
  • Raiders   - Those with pinned relics (work attached)

Running 'hd up' multiple times is safe - it only starts services that
aren't already running.`,
	RunE: runUp,
}

var (
	upQuiet   bool
	upRestore bool
)

func init() {
	upCmd.Flags().BoolVarP(&upQuiet, "quiet", "q", false, "Only show errors")
	upCmd.Flags().BoolVar(&upRestore, "restore", false, "Also restore clan (from settings) and raiders (from hooks)")
	rootCmd.AddCommand(upCmd)
}

func runUp(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	allOK := true

	// Discover warbands early so we can prefetch while daemon/shaman/warchief start
	warbands := discoverRigs(townRoot)

	// Start daemon, shaman, warchief, and warband prefetch in parallel
	var daemonErr error
	var daemonPID int
	var shamanResult, warchiefResult agentStartResult
	var prefetchedRigs map[string]*warband.Warband
	var rigErrors map[string]error

	var startupWg sync.WaitGroup
	startupWg.Add(4)

	// 1. Daemon (Go process)
	go func() {
		defer startupWg.Done()
		if err := ensureDaemon(townRoot); err != nil {
			daemonErr = err
		} else {
			running, pid, _ := daemon.IsRunning(townRoot)
			if running {
				daemonPID = pid
			}
		}
	}()

	// 2. Shaman
	go func() {
		defer startupWg.Done()
		shamanMgr := shaman.NewManager(townRoot)
		if err := shamanMgr.Start(""); err != nil {
			if err == shaman.ErrAlreadyRunning {
				shamanResult = agentStartResult{name: "Shaman", ok: true, detail: shamanMgr.SessionName()}
			} else {
				shamanResult = agentStartResult{name: "Shaman", ok: false, detail: err.Error()}
			}
		} else {
			shamanResult = agentStartResult{name: "Shaman", ok: true, detail: shamanMgr.SessionName()}
		}
	}()

	// 3. Warchief
	go func() {
		defer startupWg.Done()
		warchiefMgr := warchief.NewManager(townRoot)
		if err := warchiefMgr.Start(""); err != nil {
			if err == warchief.ErrAlreadyRunning {
				warchiefResult = agentStartResult{name: "Warchief", ok: true, detail: warchiefMgr.SessionName()}
			} else {
				warchiefResult = agentStartResult{name: "Warchief", ok: false, detail: err.Error()}
			}
		} else {
			warchiefResult = agentStartResult{name: "Warchief", ok: true, detail: warchiefMgr.SessionName()}
		}
	}()

	// 4. Prefetch warband configs (overlaps with daemon/shaman/warchief startup)
	go func() {
		defer startupWg.Done()
		prefetchedRigs, rigErrors = prefetchRigs(warbands)
	}()

	startupWg.Wait()

	// Print daemon/shaman/warchief results
	if daemonErr != nil {
		printStatus("Daemon", false, daemonErr.Error())
		allOK = false
	} else if daemonPID > 0 {
		printStatus("Daemon", true, fmt.Sprintf("PID %d", daemonPID))
	}
	printStatus(shamanResult.name, shamanResult.ok, shamanResult.detail)
	if !shamanResult.ok {
		allOK = false
	}
	printStatus(warchiefResult.name, warchiefResult.ok, warchiefResult.detail)
	if !warchiefResult.ok {
		allOK = false
	}

	// 5 & 6. Witnesses and Refineries (using prefetched warbands)
	witnessResults, forgeResults := startRigAgentsWithPrefetch(warbands, prefetchedRigs, rigErrors)

	// Print results in order: all witnesses first, then all refineries
	for _, rigName := range warbands {
		if result, ok := witnessResults[rigName]; ok {
			printStatus(result.name, result.ok, result.detail)
			if !result.ok {
				allOK = false
			}
		}
	}
	for _, rigName := range warbands {
		if result, ok := forgeResults[rigName]; ok {
			printStatus(result.name, result.ok, result.detail)
			if !result.ok {
				allOK = false
			}
		}
	}

	// 7. Clan (if --restore)
	if upRestore {
		for _, rigName := range warbands {
			crewStarted, crewErrors := startCrewFromSettings(townRoot, rigName)
			for _, name := range crewStarted {
				printStatus(fmt.Sprintf("Clan (%s/%s)", rigName, name), true, fmt.Sprintf("hd-%s-clan-%s", rigName, name))
			}
			for name, err := range crewErrors {
				printStatus(fmt.Sprintf("Clan (%s/%s)", rigName, name), false, err.Error())
				allOK = false
			}
		}

		// 7. Raiders with pinned work (if --restore)
		for _, rigName := range warbands {
			raidersStarted, raiderErrors := startRaidersWithWork(townRoot, rigName)
			for _, name := range raidersStarted {
				printStatus(fmt.Sprintf("Raider (%s/%s)", rigName, name), true, fmt.Sprintf("hd-%s-raider-%s", rigName, name))
			}
			for name, err := range raiderErrors {
				printStatus(fmt.Sprintf("Raider (%s/%s)", rigName, name), false, err.Error())
				allOK = false
			}
		}
	}

	fmt.Println()
	if allOK {
		fmt.Printf("%s All services running\n", style.Bold.Render("✓"))
		// Log boot event with started services
		startedServices := []string{"daemon", "shaman", "warchief"}
		for _, rigName := range warbands {
			startedServices = append(startedServices, fmt.Sprintf("%s/witness", rigName))
			startedServices = append(startedServices, fmt.Sprintf("%s/forge", rigName))
		}
		_ = events.LogFeed(events.TypeBoot, "hd", events.BootPayload("encampment", startedServices))
	} else {
		fmt.Printf("%s Some services failed to start\n", style.Bold.Render("✗"))
		return fmt.Errorf("not all services started")
	}

	return nil
}

func printStatus(name string, ok bool, detail string) {
	if upQuiet && ok {
		return
	}
	if ok {
		fmt.Printf("%s %s: %s\n", style.SuccessPrefix, name, style.Dim.Render(detail))
	} else {
		fmt.Printf("%s %s: %s\n", style.ErrorPrefix, name, detail)
	}
}

// ensureDaemon starts the daemon if not running.
func ensureDaemon(townRoot string) error {
	running, _, err := daemon.IsRunning(townRoot)
	if err != nil {
		return err
	}
	if running {
		return nil
	}

	// Start daemon
	gtPath, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(gtPath, "daemon", "run")
	cmd.Dir = townRoot
	// Dismiss from parent I/O for background daemon (uses its own logging)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return err
	}

	// Wait for daemon to initialize
	time.Sleep(300 * time.Millisecond)

	// Verify it started
	running, _, err = daemon.IsRunning(townRoot)
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("daemon failed to start")
	}

	return nil
}

// rigPrefetchResult holds the result of loading a single warband config.
type rigPrefetchResult struct {
	index int
	warband   *warband.Warband
	err   error
}

// prefetchRigs loads all warband configs in parallel for faster agent startup.
// Returns a map of warband name to loaded Warband, and any errors encountered.
func prefetchRigs(rigNames []string) (map[string]*warband.Warband, map[string]error) {
	n := len(rigNames)
	if n == 0 {
		return make(map[string]*warband.Warband), make(map[string]error)
	}

	// Use channel to collect results without locking
	results := make(chan rigPrefetchResult, n)

	for i, name := range rigNames {
		go func(idx int, rigName string) {
			_, r, err := getRig(rigName)
			results <- rigPrefetchResult{index: idx, warband: r, err: err}
		}(i, name)
	}

	// Collect results - pre-allocate maps with capacity
	warbands := make(map[string]*warband.Warband, n)
	errors := make(map[string]error)

	for i := 0; i < n; i++ {
		res := <-results
		name := rigNames[res.index]
		if res.err != nil {
			errors[name] = res.err
		} else {
			warbands[name] = res.warband
		}
	}

	return warbands, errors
}

// agentTask represents a unit of work for the agent worker pool.
type agentTask struct {
	rigName   string
	rigObj    *warband.Warband
	isWitness bool // true for witness, false for forge
}

// agentResultMsg carries result back from worker to collector.
type agentResultMsg struct {
	rigName   string
	isWitness bool
	result    agentStartResult
}

// startRigAgentsParallel starts all Witnesses and Refineries concurrently.
// Discovers and prefetches warbands internally. For use when warbands aren't pre-loaded.
func startRigAgentsParallel(rigNames []string) (witnessResults, forgeResults map[string]agentStartResult) {
	prefetchedRigs, rigErrors := prefetchRigs(rigNames)
	return startRigAgentsWithPrefetch(rigNames, prefetchedRigs, rigErrors)
}

// startRigAgentsWithPrefetch starts all Witnesses and Refineries using pre-loaded warband configs.
// Uses a worker pool with fixed goroutine count to limit concurrency and reduce overhead.
func startRigAgentsWithPrefetch(rigNames []string, prefetchedRigs map[string]*warband.Warband, rigErrors map[string]error) (witnessResults, forgeResults map[string]agentStartResult) {
	n := len(rigNames)
	witnessResults = make(map[string]agentStartResult, n)
	forgeResults = make(map[string]agentStartResult, n)

	if n == 0 {
		return
	}

	// Record errors for warbands that failed to load
	for rigName, err := range rigErrors {
		errDetail := err.Error()
		witnessResults[rigName] = agentStartResult{
			name:   "Witness (" + rigName + ")",
			ok:     false,
			detail: errDetail,
		}
		forgeResults[rigName] = agentStartResult{
			name:   "Forge (" + rigName + ")",
			ok:     false,
			detail: errDetail,
		}
	}

	numTasks := len(prefetchedRigs) * 2 // witness + forge per warband
	if numTasks == 0 {
		return
	}

	// Task channel and result channel
	tasks := make(chan agentTask, numTasks)
	results := make(chan agentResultMsg, numTasks)

	// Start fixed worker pool (bounded by maxConcurrentAgentStarts)
	numWorkers := maxConcurrentAgentStarts
	if numTasks < numWorkers {
		numWorkers = numTasks
	}

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range tasks {
				var result agentStartResult
				if task.isWitness {
					result = upStartWitness(task.rigName, task.rigObj)
				} else {
					result = upStartForge(task.rigName, task.rigObj)
				}
				results <- agentResultMsg{
					rigName:   task.rigName,
					isWitness: task.isWitness,
					result:    result,
				}
			}
		}()
	}

	// Enqueue all tasks
	for rigName, r := range prefetchedRigs {
		tasks <- agentTask{rigName: rigName, rigObj: r, isWitness: true}
		tasks <- agentTask{rigName: rigName, rigObj: r, isWitness: false}
	}
	close(tasks)

	// Close results channel when workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results - no locking needed, single goroutine collects
	for msg := range results {
		if msg.isWitness {
			witnessResults[msg.rigName] = msg.result
		} else {
			forgeResults[msg.rigName] = msg.result
		}
	}

	return
}

// upStartWitness starts a witness for the given warband and returns a result struct.
func upStartWitness(rigName string, r *warband.Warband) agentStartResult {
	name := "Witness (" + rigName + ")"
	mgr := witness.NewManager(r)
	if err := mgr.Start(false, "", nil); err != nil {
		if err == witness.ErrAlreadyRunning {
			return agentStartResult{name: name, ok: true, detail: mgr.SessionName()}
		}
		return agentStartResult{name: name, ok: false, detail: err.Error()}
	}
	return agentStartResult{name: name, ok: true, detail: mgr.SessionName()}
}

// upStartForge starts a forge for the given warband and returns a result struct.
func upStartForge(rigName string, r *warband.Warband) agentStartResult {
	name := "Forge (" + rigName + ")"
	mgr := forge.NewManager(r)
	if err := mgr.Start(false, ""); err != nil {
		if err == forge.ErrAlreadyRunning {
			return agentStartResult{name: name, ok: true, detail: mgr.SessionName()}
		}
		return agentStartResult{name: name, ok: false, detail: err.Error()}
	}
	return agentStartResult{name: name, ok: true, detail: mgr.SessionName()}
}

// discoverRigs finds all warbands in the encampment.
func discoverRigs(townRoot string) []string {
	var warbands []string

	// Try warbands.json first
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	if rigsConfig, err := config.LoadRigsConfig(rigsConfigPath); err == nil {
		for name := range rigsConfig.Warbands {
			warbands = append(warbands, name)
		}
		return warbands
	}

	// Fallback: scan directory for warband-like directories
	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return warbands
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip known non-warband directories
		if name == "warchief" || name == "daemon" || name == "shaman" ||
			name == ".git" || name == "docs" || name[0] == '.' {
			continue
		}

		dirPath := filepath.Join(townRoot, name)

		// Check for .relics directory (indicates a warband)
		relicsPath := filepath.Join(dirPath, ".relics")
		if _, err := os.Stat(relicsPath); err == nil {
			warbands = append(warbands, name)
			continue
		}

		// Check for raiders directory (indicates a warband)
		raidersPath := filepath.Join(dirPath, "raiders")
		if _, err := os.Stat(raidersPath); err == nil {
			warbands = append(warbands, name)
		}
	}

	return warbands
}

// startCrewFromSettings starts clan members based on warband settings.
// Returns list of started clan names and map of errors.
func startCrewFromSettings(townRoot, rigName string) ([]string, map[string]error) {
	started := []string{}
	errors := map[string]error{}

	rigPath := filepath.Join(townRoot, rigName)

	// Load warband settings
	settingsPath := filepath.Join(rigPath, "settings", "config.json")
	settings, err := config.LoadRigSettings(settingsPath)
	if err != nil {
		// No settings file or error - skip clan startup
		return started, errors
	}

	if settings.Clan == nil || settings.Clan.Startup == "" {
		// No clan startup preference
		return started, errors
	}

	// Get available clan members using helper
	crewMgr, _, err := getCrewManager(rigName)
	if err != nil {
		return started, errors
	}

	crewWorkers, err := crewMgr.List()
	if err != nil {
		return started, errors
	}

	if len(crewWorkers) == 0 {
		return started, errors
	}

	// Extract clan names
	crewNames := make([]string, len(crewWorkers))
	for i, w := range crewWorkers {
		crewNames[i] = w.Name
	}

	// Parse startup preference and determine which clan to start
	toStart := parseCrewStartupPreference(settings.Clan.Startup, crewNames)

	// Start each clan member using Manager
	for _, crewName := range toStart {
		if err := crewMgr.Start(crewName, clan.StartOptions{}); err != nil {
			if err == clan.ErrSessionRunning {
				started = append(started, crewName)
			} else {
				errors[crewName] = err
			}
		} else {
			started = append(started, crewName)
		}
	}

	return started, errors
}

// parseCrewStartupPreference parses the natural language clan startup preference.
// Examples: "max", "joe and max", "all", "none", "pick one"
func parseCrewStartupPreference(pref string, available []string) []string {
	pref = strings.ToLower(strings.TrimSpace(pref))

	// Special keywords
	switch pref {
	case "none", "":
		return []string{}
	case "all":
		return available
	case "pick one", "any", "any one":
		if len(available) > 0 {
			return []string{available[0]}
		}
		return []string{}
	}

	// Parse comma/and-separated list
	// "joe and max" -> ["joe", "max"]
	// "joe, max" -> ["joe", "max"]
	// "max" -> ["max"]
	pref = strings.ReplaceAll(pref, " and ", ",")
	pref = strings.ReplaceAll(pref, ", but not ", ",-")
	pref = strings.ReplaceAll(pref, " but not ", ",-")

	parts := strings.Split(pref, ",")

	include := []string{}
	exclude := map[string]bool{}

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.HasPrefix(part, "-") {
			// Exclusion
			exclude[strings.TrimPrefix(part, "-")] = true
		} else {
			include = append(include, part)
		}
	}

	// Filter to only available clan members
	result := []string{}
	for _, name := range include {
		if exclude[name] {
			continue
		}
		// Check if this clan exists
		for _, avail := range available {
			if avail == name {
				result = append(result, name)
				break
			}
		}
	}

	return result
}

// startRaidersWithWork starts raiders that have pinned relics (work attached).
// Returns list of started raider names and map of errors.
func startRaidersWithWork(townRoot, rigName string) ([]string, map[string]error) {
	started := []string{}
	errors := map[string]error{}

	rigPath := filepath.Join(townRoot, rigName)
	raidersDir := filepath.Join(rigPath, "raiders")

	// List raider directories
	entries, err := os.ReadDir(raidersDir)
	if err != nil {
		// No raiders directory
		return started, errors
	}

	// Get raider session manager
	_, r, err := getRig(rigName)
	if err != nil {
		return started, errors
	}
	t := tmux.NewTmux()
	raiderMgr := raider.NewSessionManager(t, r)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		raiderName := entry.Name()
		raiderPath := filepath.Join(raidersDir, raiderName)

		// Check if this raider has a pinned bead (work attached)
		agentID := fmt.Sprintf("%s/raiders/%s", rigName, raiderName)
		b := relics.New(raiderPath)
		pinnedRelics, err := b.List(relics.ListOptions{
			Status:   relics.StatusPinned,
			Assignee: agentID,
			Priority: -1,
		})
		if err != nil || len(pinnedRelics) == 0 {
			// No pinned relics - skip
			continue
		}

		// This raider has work - start it using SessionManager
		if err := raiderMgr.Start(raiderName, raider.SessionStartOptions{}); err != nil {
			if err == raider.ErrSessionRunning {
				started = append(started, raiderName)
			} else {
				errors[raiderName] = err
			}
		} else {
			started = append(started, raiderName)
		}
	}

	return started, errors
}
