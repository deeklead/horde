package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/daemon"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/raider"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/session"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/workspace"
)

const (
	shutdownLockFile    = "daemon/shutdown.lock"
	shutdownLockTimeout = 5 * time.Second
)

var downCmd = &cobra.Command{
	Use:     "down",
	GroupID: GroupServices,
	Short:   "Stop all Horde services",
	Long: `Stop Horde services (reversible pause).

Shutdown levels (progressively more aggressive):
  hd down                    Stop infrastructure (default)
  hd down --raiders         Also stop all raider sessions
  hd down --all              Also stop rl daemons/activity
  hd down --nuke             Also kill the tmux server (DESTRUCTIVE)

Infrastructure agents stopped:
  • Refineries - Per-warband work processors
  • Witnesses  - Per-warband raider managers
  • Warchief      - Global work coordinator
  • Boot       - Shaman's watchdog
  • Shaman     - Health orchestrator
  • Daemon     - Go background process

This is a "pause" operation - use 'hd start' to bring everything back up.
For permanent cleanup (removing worktrees), use 'hd shutdown' instead.

Use cases:
  • Taking a break (stop token consumption)
  • Clean shutdown before system maintenance
  • Resetting the encampment to a clean state`,
	RunE: runDown,
}

var (
	downQuiet    bool
	downForce    bool
	downAll      bool
	downNuke     bool
	downDryRun   bool
	downRaiders bool
)

func init() {
	downCmd.Flags().BoolVarP(&downQuiet, "quiet", "q", false, "Only show errors")
	downCmd.Flags().BoolVarP(&downForce, "force", "f", false, "Force kill without graceful shutdown")
	downCmd.Flags().BoolVarP(&downRaiders, "raiders", "p", false, "Also stop all raider sessions")
	downCmd.Flags().BoolVarP(&downAll, "all", "a", false, "Stop rl daemons/activity and verify shutdown")
	downCmd.Flags().BoolVar(&downNuke, "nuke", false, "Kill entire tmux server (DESTRUCTIVE - kills non-GT sessions!)")
	downCmd.Flags().BoolVar(&downDryRun, "dry-run", false, "Preview what would be stopped without taking action")
	rootCmd.AddCommand(downCmd)
}

func runDown(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	t := tmux.NewTmux()
	if !t.IsAvailable() {
		return fmt.Errorf("tmux not available (is tmux installed and on PATH?)")
	}

	// Phase 0: Acquire shutdown lock (skip for dry-run)
	if !downDryRun {
		lock, err := acquireShutdownLock(townRoot)
		if err != nil {
			return fmt.Errorf("cannot proceed: %w", err)
		}
		defer func() { _ = lock.Unlock() }()
	}
	allOK := true

	if downDryRun {
		fmt.Println("═══ DRY RUN: Preview of shutdown actions ═══")
		fmt.Println()
	}

	warbands := discoverRigs(townRoot)

	// Pre-fetch all sessions once for O(1) lookups (avoids N+1 subprocess calls)
	sessionSet, _ := t.GetSessionSet() // Ignore error - empty set is safe fallback

	// Phase 0.5: Stop raiders if --raiders
	if downRaiders {
		if downDryRun {
			fmt.Println("Would stop raiders...")
		} else {
			fmt.Println("Stopping raiders...")
		}
		raidersStopped := stopAllRaiders(t, townRoot, warbands, downForce, downDryRun)
		if downDryRun {
			if raidersStopped > 0 {
				printDownStatus("Raiders", true, fmt.Sprintf("%d would stop", raidersStopped))
			} else {
				printDownStatus("Raiders", true, "none running")
			}
		} else {
			if raidersStopped > 0 {
				printDownStatus("Raiders", true, fmt.Sprintf("%d stopped", raidersStopped))
			} else {
				printDownStatus("Raiders", true, "none running")
			}
		}
		fmt.Println()
	}

	// Phase 1: Stop rl resurrection layer (--all only)
	if downAll {
		daemonsKilled, activityKilled, err := relics.StopAllBdProcesses(downDryRun, downForce)
		if err != nil {
			printDownStatus("bd processes", false, err.Error())
			allOK = false
		} else {
			if downDryRun {
				if daemonsKilled > 0 || activityKilled > 0 {
					printDownStatus("bd daemon", true, fmt.Sprintf("%d would stop", daemonsKilled))
					printDownStatus("bd activity", true, fmt.Sprintf("%d would stop", activityKilled))
				} else {
					printDownStatus("bd processes", true, "none running")
				}
			} else {
				if daemonsKilled > 0 {
					printDownStatus("bd daemon", true, fmt.Sprintf("%d stopped", daemonsKilled))
				}
				if activityKilled > 0 {
					printDownStatus("bd activity", true, fmt.Sprintf("%d stopped", activityKilled))
				}
				if daemonsKilled == 0 && activityKilled == 0 {
					printDownStatus("bd processes", true, "none running")
				}
			}
		}
	}

	// Phase 2a: Stop refineries
	for _, rigName := range warbands {
		sessionName := fmt.Sprintf("hd-%s-forge", rigName)
		if downDryRun {
			if sessionSet.Has(sessionName) {
				printDownStatus(fmt.Sprintf("Forge (%s)", rigName), true, "would stop")
			}
			continue
		}
		wasRunning, err := stopSessionWithCache(t, sessionName, sessionSet)
		if err != nil {
			printDownStatus(fmt.Sprintf("Forge (%s)", rigName), false, err.Error())
			allOK = false
		} else if wasRunning {
			printDownStatus(fmt.Sprintf("Forge (%s)", rigName), true, "stopped")
		} else {
			printDownStatus(fmt.Sprintf("Forge (%s)", rigName), true, "not running")
		}
	}

	// Phase 2b: Stop witnesses
	for _, rigName := range warbands {
		sessionName := fmt.Sprintf("hd-%s-witness", rigName)
		if downDryRun {
			if sessionSet.Has(sessionName) {
				printDownStatus(fmt.Sprintf("Witness (%s)", rigName), true, "would stop")
			}
			continue
		}
		wasRunning, err := stopSessionWithCache(t, sessionName, sessionSet)
		if err != nil {
			printDownStatus(fmt.Sprintf("Witness (%s)", rigName), false, err.Error())
			allOK = false
		} else if wasRunning {
			printDownStatus(fmt.Sprintf("Witness (%s)", rigName), true, "stopped")
		} else {
			printDownStatus(fmt.Sprintf("Witness (%s)", rigName), true, "not running")
		}
	}

	// Phase 3: Stop encampment-level sessions (Warchief, Boot, Shaman)
	for _, ts := range session.TownSessions() {
		if downDryRun {
			if sessionSet.Has(ts.SessionID) {
				printDownStatus(ts.Name, true, "would stop")
			}
			continue
		}
		stopped, err := session.StopTownSessionWithCache(t, ts, downForce, sessionSet)
		if err != nil {
			printDownStatus(ts.Name, false, err.Error())
			allOK = false
		} else if stopped {
			printDownStatus(ts.Name, true, "stopped")
		} else {
			printDownStatus(ts.Name, true, "not running")
		}
	}

	// Phase 4: Stop Daemon
	running, pid, daemonErr := daemon.IsRunning(townRoot)
	if daemonErr != nil {
		printDownStatus("Daemon", false, fmt.Sprintf("status check failed: %v", daemonErr))
		allOK = false
	} else if downDryRun {
		if running {
			printDownStatus("Daemon", true, fmt.Sprintf("would stop (PID %d)", pid))
		}
	} else {
		if running {
			if err := daemon.StopDaemon(townRoot); err != nil {
				printDownStatus("Daemon", false, err.Error())
				allOK = false
			} else {
				printDownStatus("Daemon", true, fmt.Sprintf("stopped (was PID %d)", pid))
			}
		} else {
			printDownStatus("Daemon", true, "not running")
		}
	}

	// Phase 5: Verification (--all only)
	if downAll && !downDryRun {
		time.Sleep(500 * time.Millisecond)
		respawned := verifyShutdown(t, townRoot)
		if len(respawned) > 0 {
			fmt.Println()
			fmt.Printf("%s Warning: Some processes may have respawned:\n", style.Bold.Render("⚠"))
			for _, r := range respawned {
				fmt.Printf("  • %s\n", r)
			}
			fmt.Println()
			fmt.Printf("This may indicate systemd/launchd is managing bd.\n")
			fmt.Printf("Check with:\n")
			fmt.Printf("  %s\n", style.Dim.Render("systemctl status bd-daemon  # Linux"))
			fmt.Printf("  %s\n", style.Dim.Render("launchctl list | grep rl    # macOS"))
			allOK = false
		}
	}

	// Phase 6: Nuke tmux server (--nuke only, DESTRUCTIVE)
	if downNuke {
		if downDryRun {
			printDownStatus("Tmux server", true, "would kill (DESTRUCTIVE)")
		} else if os.Getenv("HD_NUKE_ACKNOWLEDGED") == "" {
			// Require explicit acknowledgement for destructive operation
			fmt.Println()
			fmt.Printf("%s The --nuke flag kills ALL tmux sessions, not just Horde.\n",
				style.Bold.Render("⚠ BLOCKED:"))
			fmt.Printf("This includes vim sessions, running builds, SSH connections, etc.\n")
			fmt.Println()
			fmt.Printf("To proceed, run with: %s\n", style.Bold.Render("HD_NUKE_ACKNOWLEDGED=1 hd down --nuke"))
			allOK = false
		} else {
			if err := t.KillServer(); err != nil {
				printDownStatus("Tmux server", false, err.Error())
				allOK = false
			} else {
				printDownStatus("Tmux server", true, "killed (all tmux sessions destroyed)")
			}
		}
	}

	// Summary
	fmt.Println()
	if downDryRun {
		fmt.Println("═══ DRY RUN COMPLETE (no changes made) ═══")
		return nil
	}

	if allOK {
		fmt.Printf("%s All services stopped\n", style.Bold.Render("✓"))
		stoppedServices := []string{"daemon", "shaman", "boot", "warchief"}
		for _, rigName := range warbands {
			stoppedServices = append(stoppedServices, fmt.Sprintf("%s/forge", rigName))
			stoppedServices = append(stoppedServices, fmt.Sprintf("%s/witness", rigName))
		}
		if downRaiders {
			stoppedServices = append(stoppedServices, "raiders")
		}
		if downAll {
			stoppedServices = append(stoppedServices, "bd-processes")
		}
		if downNuke {
			stoppedServices = append(stoppedServices, "tmux-server")
		}
		_ = events.LogFeed(events.TypeHalt, "hd", events.HaltPayload(stoppedServices))
	} else {
		fmt.Printf("%s Some services failed to stop\n", style.Bold.Render("✗"))
		return fmt.Errorf("not all services stopped")
	}

	return nil
}

// stopAllRaiders stops all raider sessions across all warbands.
// Returns the number of raiders stopped (or would be stopped in dry-run).
func stopAllRaiders(t *tmux.Tmux, townRoot string, rigNames []string, force bool, dryRun bool) int {
	stopped := 0

	// Load warbands config
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)

	for _, rigName := range rigNames {
		r, err := rigMgr.GetRig(rigName)
		if err != nil {
			continue
		}

		raiderMgr := raider.NewSessionManager(t, r)
		infos, err := raiderMgr.List()
		if err != nil {
			continue
		}

		for _, info := range infos {
			if dryRun {
				stopped++
				fmt.Printf("  %s [%s] %s would stop\n", style.Dim.Render("○"), rigName, info.Raider)
				continue
			}
			err := raiderMgr.Stop(info.Raider, force)
			if err == nil {
				stopped++
				fmt.Printf("  %s [%s] %s stopped\n", style.SuccessPrefix, rigName, info.Raider)
			} else {
				fmt.Printf("  %s [%s] %s: %s\n", style.ErrorPrefix, rigName, info.Raider, err.Error())
			}
		}
	}

	return stopped
}

func printDownStatus(name string, ok bool, detail string) {
	if downQuiet && ok {
		return
	}
	if ok {
		fmt.Printf("%s %s: %s\n", style.SuccessPrefix, name, style.Dim.Render(detail))
	} else {
		fmt.Printf("%s %s: %s\n", style.ErrorPrefix, name, detail)
	}
}

// stopSession gracefully stops a tmux session.
// Returns (wasRunning, error) - wasRunning is true if session existed and was stopped.
func stopSession(t *tmux.Tmux, sessionName string) (bool, error) {
	running, err := t.HasSession(sessionName)
	if err != nil {
		return false, err
	}
	if !running {
		return false, nil // Already stopped
	}

	// Try graceful shutdown first (Ctrl-C, best-effort interrupt)
	if !downForce {
		_ = t.SendKeysRaw(sessionName, "C-c")
		time.Sleep(100 * time.Millisecond)
	}

	// Kill the session (with explicit process termination to prevent orphans)
	return true, t.KillSessionWithProcesses(sessionName)
}

// stopSessionWithCache is like stopSession but uses a pre-fetched SessionSet
// for O(1) existence check instead of spawning a subprocess.
func stopSessionWithCache(t *tmux.Tmux, sessionName string, cache *tmux.SessionSet) (bool, error) {
	if !cache.Has(sessionName) {
		return false, nil // Already stopped
	}

	// Try graceful shutdown first (Ctrl-C, best-effort interrupt)
	if !downForce {
		_ = t.SendKeysRaw(sessionName, "C-c")
		time.Sleep(100 * time.Millisecond)
	}

	// Kill the session (with explicit process termination to prevent orphans)
	return true, t.KillSessionWithProcesses(sessionName)
}

// acquireShutdownLock prevents concurrent shutdowns.
// Returns the lock (caller must defer Unlock()) or error if lock held.
func acquireShutdownLock(townRoot string) (*flock.Flock, error) {
	lockPath := filepath.Join(townRoot, shutdownLockFile)

	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("creating lock directory: %w", err)
	}

	lock := flock.New(lockPath)

	ctx, cancel := context.WithTimeout(context.Background(), shutdownLockTimeout)
	defer cancel()

	locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("lock acquisition failed: %w", err)
	}

	if !locked {
		return nil, fmt.Errorf("another shutdown is in progress (lock held: %s)", lockPath)
	}

	return lock, nil
}

// verifyShutdown checks for respawned processes after shutdown.
// Returns list of things that are still running or respawned.
func verifyShutdown(t *tmux.Tmux, townRoot string) []string {
	var respawned []string

	if count := relics.CountBdDaemons(); count > 0 {
		respawned = append(respawned, fmt.Sprintf("bd daemon (%d running)", count))
	}

	if count := relics.CountBdActivityProcesses(); count > 0 {
		respawned = append(respawned, fmt.Sprintf("bd activity (%d running)", count))
	}

	sessions, err := t.ListSessions()
	if err == nil {
		for _, sess := range sessions {
			if strings.HasPrefix(sess, "hd-") || strings.HasPrefix(sess, "hq-") {
				respawned = append(respawned, fmt.Sprintf("tmux session %s", sess))
			}
		}
	}

	pidFile := filepath.Join(townRoot, "daemon", "daemon.pid")
	if pidData, err := os.ReadFile(pidFile); err == nil {
		var pid int
		if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err == nil {
			if isProcessRunning(pid) {
				respawned = append(respawned, fmt.Sprintf("hd daemon (PID %d)", pid))
			}
		}
	}

	return respawned
}
