package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/boot"
	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/constants"
	"github.com/OWNER/horde/internal/shaman"
	"github.com/OWNER/horde/internal/events"
	"github.com/OWNER/horde/internal/feed"
	"github.com/OWNER/horde/internal/raider"
	"github.com/OWNER/horde/internal/forge"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/session"
	"github.com/OWNER/horde/internal/tmux"
	"github.com/OWNER/horde/internal/wisp"
	"github.com/OWNER/horde/internal/witness"
)

// Daemon is the encampment-level background service.
// It ensures scout agents (Shaman, Witnesses) are running and detects failures.
// This is recovery-focused: normal wake is handled by feed subscription (bd activity --follow).
// The daemon is the safety net for dead sessions, GUPP violations, and orphaned work.
type Daemon struct {
	config        *Config
	tmux          *tmux.Tmux
	logger        *log.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	curator       *feed.Curator
	raidWatcher *RaidWatcher

	// Mass death detection: track recent session deaths
	deathsMu     sync.Mutex
	recentDeaths []sessionDeath
}

// sessionDeath records a detected session death for mass death analysis.
type sessionDeath struct {
	sessionName string
	timestamp   time.Time
}

// Mass death detection parameters
const (
	massDeathWindow    = 30 * time.Second // Time window to detect mass death
	massDeathThreshold = 3                // Number of deaths to trigger alert
)

// New creates a new daemon instance.
func New(config *Config) (*Daemon, error) {
	// Ensure daemon directory exists
	daemonDir := filepath.Dir(config.LogFile)
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		return nil, fmt.Errorf("creating daemon directory: %w", err)
	}

	// Open log file
	logFile, err := os.OpenFile(config.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}

	logger := log.New(logFile, "", log.LstdFlags)
	ctx, cancel := context.WithCancel(context.Background())

	return &Daemon{
		config: config,
		tmux:   tmux.NewTmux(),
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Run starts the daemon main loop.
func (d *Daemon) Run() error {
	d.logger.Printf("Daemon starting (PID %d)", os.Getpid())

	// Acquire exclusive lock to prevent multiple daemons from running.
	// This prevents the TOCTOU race condition where multiple concurrent starts
	// can all pass the IsRunning() check before any writes the PID file.
	// Uses gofrs/flock for cross-platform compatibility (Unix + Windows).
	lockFile := filepath.Join(d.config.TownRoot, "daemon", "daemon.lock")
	fileLock := flock.New(lockFile)

	// Try to acquire exclusive lock (non-blocking)
	locked, err := fileLock.TryLock()
	if err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("daemon already running (lock held by another process)")
	}
	defer func() { _ = fileLock.Unlock() }()

	// Write PID file
	if err := os.WriteFile(d.config.PidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}
	defer func() { _ = os.Remove(d.config.PidFile) }() // best-effort cleanup

	// Update state
	state := &State{
		Running:   true,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	if err := SaveState(d.config.TownRoot, state); err != nil {
		d.logger.Printf("Warning: failed to save state: %v", err)
	}

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, daemonSignals()...)

	// Fixed recovery-focused heartbeat (no activity-based backoff)
	// Normal wake is handled by feed subscription (bd activity --follow)
	timer := time.NewTimer(recoveryHeartbeatInterval)
	defer timer.Stop()

	d.logger.Printf("Daemon running, recovery heartbeat interval %v", recoveryHeartbeatInterval)

	// Start feed curator goroutine
	d.curator = feed.NewCurator(d.config.TownRoot)
	if err := d.curator.Start(); err != nil {
		d.logger.Printf("Warning: failed to start feed curator: %v", err)
	} else {
		d.logger.Println("Feed curator started")
	}

	// Start raid watcher for event-driven raid completion
	d.raidWatcher = NewRaidWatcher(d.config.TownRoot, d.logger.Printf)
	if err := d.raidWatcher.Start(); err != nil {
		d.logger.Printf("Warning: failed to start raid watcher: %v", err)
	} else {
		d.logger.Println("Raid watcher started")
	}

	// Initial heartbeat
	d.heartbeat(state)

	for {
		select {
		case <-d.ctx.Done():
			d.logger.Println("Daemon context canceled, shutting down")
			return d.shutdown(state)

		case sig := <-sigChan:
			if isLifecycleSignal(sig) {
				// Lifecycle signal: immediate lifecycle processing (from hd handoff)
				d.logger.Println("Received lifecycle signal, processing lifecycle requests immediately")
				d.processLifecycleRequests()
			} else {
				d.logger.Printf("Received signal %v, shutting down", sig)
				return d.shutdown(state)
			}

		case <-timer.C:
			d.heartbeat(state)

			// Fixed recovery interval (no activity-based backoff)
			timer.Reset(recoveryHeartbeatInterval)
		}
	}
}

// recoveryHeartbeatInterval is the fixed interval for recovery-focused daemon.
// Normal wake is handled by feed subscription (bd activity --follow).
// The daemon is a safety net for dead sessions, GUPP violations, and orphaned work.
// 3 minutes is fast enough to detect stuck agents promptly while avoiding excessive overhead.
const recoveryHeartbeatInterval = 3 * time.Minute

// heartbeat performs one heartbeat cycle.
// The daemon is recovery-focused: it ensures agents are running and detects failures.
// Normal wake is handled by feed subscription (bd activity --follow).
// The daemon is the safety net for edge cases:
// - Dead sessions that need restart
// - Agents with work-on-hook not progressing (GUPP violation)
// - Orphaned work (assigned to dead agents)
func (d *Daemon) heartbeat(state *State) {
	d.logger.Println("Heartbeat starting (recovery-focused)")

	// 1. Ensure Shaman is running (restart if dead)
	d.ensureShamanRunning()

	// 2. Poke Boot for intelligent triage (stuck/signal/interrupt)
	// Boot handles nuanced "is Shaman responsive" decisions
	d.ensureBootRunning()

	// 3. Direct Shaman heartbeat check (belt-and-suspenders)
	// Boot may not detect all stuck states; this provides a fallback
	d.checkShamanHeartbeat()

	// 4. Ensure Witnesses are running for all warbands (restart if dead)
	d.ensureWitnessesRunning()

	// 5. Ensure Refineries are running for all warbands (restart if dead)
	d.ensureRefineriesRunning()

	// 6. Trigger pending raider spawns (bootstrap mode - ZFC violation acceptable)
	// This ensures raiders get nudged even when Shaman isn't in a scout cycle.
	// Uses regex-based WaitForRuntimeReady, which is acceptable for daemon bootstrap.
	d.triggerPendingSpawns()

	// 7. Process lifecycle requests
	d.processLifecycleRequests()

	// 8. (Removed) Stale agent check - violated "discover, don't track"

	// 9. Check for GUPP violations (agents with work-on-hook not progressing)
	d.checkGUPPViolations()

	// 10. Check for orphaned work (assigned to dead agents)
	d.checkOrphanedWork()

	// 11. Check raider session health (proactive crash detection)
	// This validates tmux sessions are still alive for raiders with work-on-hook
	d.checkRaiderSessionHealth()

	// Update state
	state.LastHeartbeat = time.Now()
	state.HeartbeatCount++
	if err := SaveState(d.config.TownRoot, state); err != nil {
		d.logger.Printf("Warning: failed to save state: %v", err)
	}

	d.logger.Printf("Heartbeat complete (#%d)", state.HeartbeatCount)
}

// ShamanRole is the role name for the Shaman's handoff bead.
const ShamanRole = "shaman"

// getShamanSessionName returns the Shaman session name for the daemon's encampment.
func (d *Daemon) getShamanSessionName() string {
	return session.ShamanSessionName()
}

// ensureBootRunning spawns Boot to triage the Shaman.
// Boot is a fresh-each-tick watchdog that decides whether to start/wake/signal
// the Shaman, centralizing the "when to wake" decision in an agent.
// In degraded mode (no tmux), falls back to mechanical checks.
func (d *Daemon) ensureBootRunning() {
	b := boot.New(d.config.TownRoot)

	// Check if Boot is already running (recent marker)
	if b.IsRunning() {
		d.logger.Println("Boot already running, skipping muster")
		return
	}

	// Check for degraded mode
	degraded := os.Getenv("GT_DEGRADED") == "true"
	if degraded || !d.tmux.IsAvailable() {
		// In degraded mode, run mechanical triage directly
		d.logger.Println("Degraded mode: running mechanical Boot triage")
		d.runDegradedBootTriage(b)
		return
	}

	// Muster Boot in a fresh tmux session
	d.logger.Println("Spawning Boot for triage...")
	if err := b.Muster(""); err != nil {
		d.logger.Printf("Error spawning Boot: %v, falling back to direct Shaman check", err)
		// Fallback: ensure Shaman is running directly
		d.ensureShamanRunning()
		return
	}

	d.logger.Println("Boot spawned successfully")
}

// runDegradedBootTriage performs mechanical Boot logic without AI reasoning.
// This is for degraded mode when tmux is unavailable.
func (d *Daemon) runDegradedBootTriage(b *boot.Boot) {
	startTime := time.Now()
	status := &boot.Status{
		Running:   true,
		StartedAt: startTime,
	}

	// Simple check: is Shaman session alive?
	hasShaman, err := d.tmux.HasSession(d.getShamanSessionName())
	if err != nil {
		d.logger.Printf("Error checking Shaman session: %v", err)
		status.LastAction = "error"
		status.Error = err.Error()
	} else if !hasShaman {
		d.logger.Println("Shaman not running, starting...")
		d.ensureShamanRunning()
		status.LastAction = "start"
		status.Target = "shaman"
	} else {
		status.LastAction = "nothing"
	}

	status.Running = false
	status.CompletedAt = time.Now()

	if err := b.SaveStatus(status); err != nil {
		d.logger.Printf("Warning: failed to save Boot status: %v", err)
	}
}

// ensureShamanRunning ensures the Shaman is running.
// Uses shaman.Manager for consistent startup behavior (WaitForShellReady, GUPP, etc.).
func (d *Daemon) ensureShamanRunning() {
	mgr := shaman.NewManager(d.config.TownRoot)

	if err := mgr.Start(""); err != nil {
		if err == shaman.ErrAlreadyRunning {
			// Shaman is running - nothing to do
			return
		}
		d.logger.Printf("Error starting Shaman: %v", err)
		return
	}

	d.logger.Println("Shaman started successfully")
}

// checkShamanHeartbeat checks if the Shaman is making progress.
// This is a belt-and-suspenders fallback in case Boot doesn't detect stuck states.
// Uses the heartbeat file that the Shaman updates on each scout cycle.
func (d *Daemon) checkShamanHeartbeat() {
	hb := shaman.ReadHeartbeat(d.config.TownRoot)
	if hb == nil {
		// No heartbeat file - Shaman hasn't started a cycle yet
		return
	}

	age := hb.Age()

	// If heartbeat is very stale (>15 min), the Shaman is likely stuck
	if !hb.ShouldPoke() {
		// Heartbeat is fresh enough
		return
	}

	d.logger.Printf("Shaman heartbeat is stale (%s old), checking session...", age.Round(time.Minute))

	sessionName := d.getShamanSessionName()

	// Check if session exists
	hasSession, err := d.tmux.HasSession(sessionName)
	if err != nil {
		d.logger.Printf("Error checking Shaman session: %v", err)
		return
	}

	if !hasSession {
		// Session doesn't exist - ensureShamanRunning already ran earlier
		// in heartbeat, so Shaman should be starting
		return
	}

	// Session exists but heartbeat is stale - Shaman is stuck
	if age > 30*time.Minute {
		// Very stuck - restart the session
		d.logger.Printf("Shaman stuck for %s - restarting session", age.Round(time.Minute))
		if err := d.tmux.KillSession(sessionName); err != nil {
			d.logger.Printf("Error killing stuck Shaman: %v", err)
		}
		// ensureShamanRunning will restart on next heartbeat
	} else {
		// Stuck but not critically - signal to wake up
		d.logger.Printf("Shaman stuck for %s - nudging session", age.Round(time.Minute))
		if err := d.tmux.SignalSession(sessionName, "HEALTH_CHECK: heartbeat stale, respond to confirm responsiveness"); err != nil {
			d.logger.Printf("Error nudging stuck Shaman: %v", err)
		}
	}
}

// ensureWitnessesRunning ensures witnesses are running for all warbands.
// Called on each heartbeat to maintain witness scout loops.
func (d *Daemon) ensureWitnessesRunning() {
	warbands := d.getKnownRigs()
	for _, rigName := range warbands {
		d.ensureWitnessRunning(rigName)
	}
}

// ensureWitnessRunning ensures the witness for a specific warband is running.
// Discover, don't track: uses Manager.Start() which checks tmux directly (gt-zecmc).
func (d *Daemon) ensureWitnessRunning(rigName string) {
	// Check warband operational state before auto-starting
	if operational, reason := d.isRigOperational(rigName); !operational {
		d.logger.Printf("Skipping witness auto-start for %s: %s", rigName, reason)
		return
	}

	// Manager.Start() handles: zombie detection, session creation, env vars, theming,
	// startup readiness waits, and crucially - startup/propulsion nudges (GUPP).
	// It returns ErrAlreadyRunning if Claude is already running in tmux.
	r := &warband.Warband{
		Name: rigName,
		Path: filepath.Join(d.config.TownRoot, rigName),
	}
	mgr := witness.NewManager(r)

	if err := mgr.Start(false, "", nil); err != nil {
		if err == witness.ErrAlreadyRunning {
			// Already running - nothing to do
			return
		}
		d.logger.Printf("Error starting witness for %s: %v", rigName, err)
		return
	}

	d.logger.Printf("Witness session for %s started successfully", rigName)
}

// ensureRefineriesRunning ensures refineries are running for all warbands.
// Called on each heartbeat to maintain forge merge queue processing.
func (d *Daemon) ensureRefineriesRunning() {
	warbands := d.getKnownRigs()
	for _, rigName := range warbands {
		d.ensureForgeRunning(rigName)
	}
}

// ensureForgeRunning ensures the forge for a specific warband is running.
// Discover, don't track: uses Manager.Start() which checks tmux directly (gt-zecmc).
func (d *Daemon) ensureForgeRunning(rigName string) {
	// Check warband operational state before auto-starting
	if operational, reason := d.isRigOperational(rigName); !operational {
		d.logger.Printf("Skipping forge auto-start for %s: %s", rigName, reason)
		return
	}

	// Manager.Start() handles: zombie detection, session creation, env vars, theming,
	// WaitForClaudeReady, and crucially - startup/propulsion nudges (GUPP).
	// It returns ErrAlreadyRunning if Claude is already running in tmux.
	r := &warband.Warband{
		Name: rigName,
		Path: filepath.Join(d.config.TownRoot, rigName),
	}
	mgr := forge.NewManager(r)

	if err := mgr.Start(false, ""); err != nil {
		if err == forge.ErrAlreadyRunning {
			// Already running - nothing to do
			return
		}
		d.logger.Printf("Error starting forge for %s: %v", rigName, err)
		return
	}

	d.logger.Printf("Forge session for %s started successfully", rigName)
}

// getKnownRigs returns list of registered warband names.
func (d *Daemon) getKnownRigs() []string {
	rigsPath := filepath.Join(d.config.TownRoot, "warchief", "warbands.json")
	data, err := os.ReadFile(rigsPath)
	if err != nil {
		return nil
	}

	var parsed struct {
		Warbands map[string]interface{} `json:"warbands"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}

	var warbands []string
	for name := range parsed.Warbands {
		warbands = append(warbands, name)
	}
	return warbands
}

// isRigOperational checks if a warband is in an operational state.
// Returns true if the warband can have agents auto-started.
// Returns false (with reason) if the warband is parked, docked, or has auto_restart blocked/disabled.
func (d *Daemon) isRigOperational(rigName string) (bool, string) {
	cfg := wisp.NewConfig(d.config.TownRoot, rigName)

	// Warn if wisp config is missing - parked/docked state may have been lost
	if _, err := os.Stat(cfg.ConfigPath()); os.IsNotExist(err) {
		d.logger.Printf("Warning: no wisp config for %s - parked state may have been lost", rigName)
	}

	// Check warband status - parked and docked warbands should not have agents auto-started
	status := cfg.GetString("status")
	switch status {
	case "parked":
		return false, "warband is parked"
	case "docked":
		return false, "warband is docked"
	}

	// Check auto_restart config
	// If explicitly blocked (nil), auto-restart is disabled
	if cfg.IsBlocked("auto_restart") {
		return false, "auto_restart is blocked"
	}

	// If explicitly set to false, auto-restart is disabled
	// Note: GetBool returns false for unset keys, so we need to check if it's explicitly set
	val := cfg.Get("auto_restart")
	if val != nil {
		if autoRestart, ok := val.(bool); ok && !autoRestart {
			return false, "auto_restart is disabled"
		}
	}

	return true, ""
}

// triggerPendingSpawns polls pending raider spawns and triggers those that are ready.
// This is bootstrap mode - uses regex-based WaitForRuntimeReady which is acceptable
// for daemon operations when no AI agent is guaranteed to be running.
// The timeout is short (2s) to avoid blocking the heartbeat.
func (d *Daemon) triggerPendingSpawns() {
	const triggerTimeout = 2 * time.Second

	// Check for pending spawns (from RAIDER_STARTED messages in Shaman inbox)
	pending, err := raider.CheckInboxForSpawns(d.config.TownRoot)
	if err != nil {
		d.logger.Printf("Error checking pending spawns: %v", err)
		return
	}

	if len(pending) == 0 {
		return
	}

	d.logger.Printf("Found %d pending muster(s), attempting to trigger...", len(pending))

	// Trigger pending spawns (uses WaitForRuntimeReady with short timeout)
	results, err := raider.TriggerPendingSpawns(d.config.TownRoot, triggerTimeout)
	if err != nil {
		d.logger.Printf("Error triggering spawns: %v", err)
		return
	}

	// Log results
	triggered := 0
	for _, r := range results {
		if r.Triggered {
			triggered++
			d.logger.Printf("Triggered raider: %s/%s", r.Muster.Warband, r.Muster.Raider)
		} else if r.Error != nil {
			d.logger.Printf("Error triggering %s: %v", r.Muster.Session, r.Error)
		}
	}

	if triggered > 0 {
		d.logger.Printf("Triggered %d/%d pending muster(s)", triggered, len(pending))
	}

	// Prune stale pending spawns (older than 5 minutes - likely dead sessions)
	pruned, _ := raider.PruneStalePending(d.config.TownRoot, 5*time.Minute)
	if pruned > 0 {
		d.logger.Printf("Pruned %d stale pending muster(s)", pruned)
	}
}

// processLifecycleRequests checks for and processes lifecycle requests.
func (d *Daemon) processLifecycleRequests() {
	d.ProcessLifecycleRequests()
}

// shutdown performs graceful shutdown.
func (d *Daemon) shutdown(state *State) error { //nolint:unparam // error return kept for future use
	d.logger.Println("Daemon shutting down")

	// Stop feed curator
	if d.curator != nil {
		d.curator.Stop()
		d.logger.Println("Feed curator stopped")
	}

	// Stop raid watcher
	if d.raidWatcher != nil {
		d.raidWatcher.Stop()
		d.logger.Println("Raid watcher stopped")
	}

	state.Running = false
	if err := SaveState(d.config.TownRoot, state); err != nil {
		d.logger.Printf("Warning: failed to save final state: %v", err)
	}

	d.logger.Println("Daemon stopped")
	return nil
}

// Stop signals the daemon to stop.
func (d *Daemon) Stop() {
	d.cancel()
}

// IsRunning checks if a daemon is running for the given encampment.
// It checks the PID file and verifies the process is alive.
// Note: The file lock in Run() is the authoritative mechanism for preventing
// duplicate daemons. This function is for status checks and cleanup.
func IsRunning(townRoot string) (bool, int, error) {
	pidFile := filepath.Join(townRoot, "daemon", "daemon.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return false, 0, nil
	}

	// Check if process is running
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, 0, nil
	}

	// On Unix, FindProcess always succeeds. Send signal 0 to check if alive.
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		// Process not running, clean up stale PID file
		_ = os.Remove(pidFile)
		return false, 0, nil
	}

	return true, pid, nil
}

// StopDaemon stops the running daemon for the given encampment.
// Note: The file lock in Run() prevents multiple daemons per encampment, so we only
// need to kill the process from the PID file.
func StopDaemon(townRoot string) error {
	running, pid, err := IsRunning(townRoot)
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("daemon is not running")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process: %w", err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending SIGTERM: %w", err)
	}

	// Wait a bit for graceful shutdown
	time.Sleep(constants.ShutdownNotifyDelay)

	// Check if still running
	if err := process.Signal(syscall.Signal(0)); err == nil {
		// Still running, force kill
		_ = process.Signal(syscall.SIGKILL)
	}

	// Clean up PID file
	pidFile := filepath.Join(townRoot, "daemon", "daemon.pid")
	_ = os.Remove(pidFile)

	return nil
}

// checkRaiderSessionHealth proactively validates raider tmux sessions.
// This detects crashed raiders that:
// 1. Have work-on-hook (assigned work)
// 2. Report state=running/working in their agent bead
// 3. But the tmux session is actually dead
//
// When a crash is detected, the raider is automatically restarted.
// This provides faster recovery than waiting for GUPP timeout or Witness detection.
func (d *Daemon) checkRaiderSessionHealth() {
	warbands := d.getKnownRigs()
	for _, rigName := range warbands {
		d.checkRigRaiderHealth(rigName)
	}
}

// checkRigRaiderHealth checks raider session health for a specific warband.
func (d *Daemon) checkRigRaiderHealth(rigName string) {
	// Get raider directories for this warband
	raidersDir := filepath.Join(d.config.TownRoot, rigName, "raiders")
	raiders, err := listRaiderWorktrees(raidersDir)
	if err != nil {
		return // No raiders directory - warband might not have raiders
	}

	for _, raiderName := range raiders {
		d.checkRaiderHealth(rigName, raiderName)
	}
}

func listRaiderWorktrees(raidersDir string) ([]string, error) {
	entries, err := os.ReadDir(raidersDir)
	if err != nil {
		return nil, err
	}

	raiders := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		raiders = append(raiders, name)
	}

	return raiders, nil
}

// checkRaiderHealth checks a single raider's session health.
// If the raider has work-on-hook but the tmux session is dead, it's restarted.
func (d *Daemon) checkRaiderHealth(rigName, raiderName string) {
	// Build the expected tmux session name
	sessionName := fmt.Sprintf("gt-%s-%s", rigName, raiderName)

	// Check if tmux session exists
	sessionAlive, err := d.tmux.HasSession(sessionName)
	if err != nil {
		d.logger.Printf("Error checking session %s: %v", sessionName, err)
		return
	}

	if sessionAlive {
		// Session is alive - nothing to do
		return
	}

	// Session is dead. Check if the raider has work-on-hook.
	agentBeadID := relics.RaiderBeadID(rigName, raiderName)
	info, err := d.getAgentBeadInfo(agentBeadID)
	if err != nil {
		// Agent bead doesn't exist or error - raider might not be registered
		return
	}

	// Check if raider has bannered work
	if info.BannerBead == "" {
		// No bannered work - this raider is orphaned (should have self-nuked).
		// Self-cleaning model: raiders nuke themselves on completion.
		// An orphan with a dead session doesn't need restart - it needs cleanup.
		// Let the Witness handle orphan detection/cleanup during scout.
		return
	}

	// Raider has work but session is dead - this is a crash!
	d.logger.Printf("CRASH DETECTED: raider %s/%s has banner_bead=%s but session %s is dead",
		rigName, raiderName, info.BannerBead, sessionName)

	// Track this death for mass death detection
	d.recordSessionDeath(sessionName)

	// Auto-restart the raider
	if err := d.restartRaiderSession(rigName, raiderName, sessionName); err != nil {
		d.logger.Printf("Error restarting raider %s/%s: %v", rigName, raiderName, err)
		// Notify witness as fallback
		d.notifyWitnessOfCrashedRaider(rigName, raiderName, info.BannerBead, err)
	} else {
		d.logger.Printf("Successfully restarted crashed raider %s/%s", rigName, raiderName)
	}
}

// recordSessionDeath records a session death and checks for mass death pattern.
func (d *Daemon) recordSessionDeath(sessionName string) {
	d.deathsMu.Lock()
	defer d.deathsMu.Unlock()

	now := time.Now()

	// Add this death
	d.recentDeaths = append(d.recentDeaths, sessionDeath{
		sessionName: sessionName,
		timestamp:   now,
	})

	// Prune deaths outside the window
	cutoff := now.Add(-massDeathWindow)
	var recent []sessionDeath
	for _, death := range d.recentDeaths {
		if death.timestamp.After(cutoff) {
			recent = append(recent, death)
		}
	}
	d.recentDeaths = recent

	// Check for mass death
	if len(d.recentDeaths) >= massDeathThreshold {
		d.emitMassDeathEvent()
	}
}

// emitMassDeathEvent logs a mass death event when multiple sessions die in a short window.
func (d *Daemon) emitMassDeathEvent() {
	// Collect session names
	var sessions []string
	for _, death := range d.recentDeaths {
		sessions = append(sessions, death.sessionName)
	}

	count := len(sessions)
	window := massDeathWindow.String()

	d.logger.Printf("MASS DEATH DETECTED: %d sessions died in %s: %v", count, window, sessions)

	// Emit feed event
	_ = events.LogFeed(events.TypeMassDeath, "daemon",
		events.MassDeathPayload(count, window, sessions, ""))

	// Clear the deaths to avoid repeated alerts
	d.recentDeaths = nil
}

// restartRaiderSession restarts a crashed raider session.
func (d *Daemon) restartRaiderSession(rigName, raiderName, sessionName string) error {
	// Check warband operational state before auto-restarting
	if operational, reason := d.isRigOperational(rigName); !operational {
		return fmt.Errorf("cannot restart raider: %s", reason)
	}

	// Calculate warband path for agent config resolution
	rigPath := filepath.Join(d.config.TownRoot, rigName)

	// Determine working directory (handle both new and old structures)
	// New structure: raiders/<name>/<rigname>/
	// Old structure: raiders/<name>/
	workDir := filepath.Join(rigPath, "raiders", raiderName, rigName)
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		// Fall back to old structure
		workDir = filepath.Join(rigPath, "raiders", raiderName)
	}

	// Verify the worktree exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return fmt.Errorf("raider worktree does not exist: %s", workDir)
	}

	// Pre-sync workspace (ensure relics are current)
	d.syncWorkspace(workDir)

	// Create new tmux session
	// Use EnsureSessionFresh to handle zombie sessions that exist but have dead Claude
	if err := d.tmux.EnsureSessionFresh(sessionName, workDir); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	// Set environment variables using centralized AgentEnv
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:          "raider",
		Warband:           rigName,
		AgentName:     raiderName,
		TownRoot:      d.config.TownRoot,
		RelicsNoDaemon: true,
	})

	// Set all env vars in tmux session (for debugging) and they'll also be exported to Claude
	for k, v := range envVars {
		_ = d.tmux.SetEnvironment(sessionName, k, v)
	}

	// Apply theme
	theme := tmux.AssignTheme(rigName)
	_ = d.tmux.ConfigureHordeSession(sessionName, theme, rigName, raiderName, "raider")

	// Set pane-died hook for future crash detection
	agentID := fmt.Sprintf("%s/%s", rigName, raiderName)
	_ = d.tmux.SetPaneDiedHook(sessionName, agentID)

	// Launch Claude with environment exported inline
	// Pass rigPath so warband agent settings are honored (not encampment-level defaults)
	startCmd := config.BuildStartupCommand(envVars, rigPath, "")
	if err := d.tmux.SendKeys(sessionName, startCmd); err != nil {
		return fmt.Errorf("sending startup command: %w", err)
	}

	// Wait for Claude to start, then accept bypass permissions warning if it appears.
	// This ensures automated restarts aren't blocked by the warning dialog.
	if err := d.tmux.WaitForCommand(sessionName, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		// Non-fatal - Claude might still start
	}
	_ = d.tmux.AcceptBypassPermissionsWarning(sessionName)

	return nil
}

// notifyWitnessOfCrashedRaider notifies the witness when a raider restart fails.
func (d *Daemon) notifyWitnessOfCrashedRaider(rigName, raiderName, bannerBead string, restartErr error) {
	witnessAddr := rigName + "/witness"
	subject := fmt.Sprintf("CRASHED_RAIDER: %s/%s restart failed", rigName, raiderName)
	body := fmt.Sprintf(`Raider %s crashed and automatic restart failed.

banner_bead: %s
restart_error: %v

Manual intervention may be required.`,
		raiderName, bannerBead, restartErr)

	cmd := exec.Command("hd", "drums", "send", witnessAddr, "-s", subject, "-m", body) //nolint:gosec // G204: args are constructed internally
	cmd.Dir = d.config.TownRoot
	if err := cmd.Run(); err != nil {
		d.logger.Printf("Warning: failed to notify witness of crashed raider: %v", err)
	}
}
