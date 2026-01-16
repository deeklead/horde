package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/git"
	"github.com/OWNER/horde/internal/raider"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/suggest"
	"github.com/OWNER/horde/internal/tmux"
	"github.com/OWNER/horde/internal/encampmentlog"
	"github.com/OWNER/horde/internal/workspace"
)

// Session command flags
var (
	sessionIssue     string
	sessionForce     bool
	sessionLines     int
	sessionMessage   string
	sessionFile      string
	sessionRigFilter string
	sessionListJSON  bool
)

var sessionCmd = &cobra.Command{
	Use:     "session",
	Aliases: []string{"sess"},
	GroupID: GroupAgents,
	Short:   "Manage raider sessions",
	RunE:    requireSubcommand,
	Long: `Manage tmux sessions for raiders.

Sessions are tmux sessions running Claude for each raider.
Use the subcommands to start, stop, summon, and monitor sessions.

TIP: To send messages to a running session, use 'hd signal' (not 'session inject').
The signal command uses reliable delivery that works correctly with Claude Code.`,
}

var sessionStartCmd = &cobra.Command{
	Use:   "start <warband>/<raider>",
	Short: "Start a raider session",
	Long: `Start a new tmux session for a raider.

Creates a tmux session, navigates to the raider's working directory,
and launches claude. Optionally inject an initial issue to work on.

Examples:
  hd session start wyvern/Toast
  hd session start wyvern/Toast --issue gt-123`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionStart,
}

var sessionStopCmd = &cobra.Command{
	Use:   "stop <warband>/<raider>",
	Short: "Stop a raider session",
	Long: `Stop a running raider session.

Attempts graceful shutdown first (Ctrl-C), then kills the tmux session.
Use --force to skip graceful shutdown.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionStop,
}

var sessionAtCmd = &cobra.Command{
	Use:     "at <warband>/<raider>",
	Aliases: []string{"summon"},
	Short:   "Summon to a running session",
	Long: `Summon to a running raider session.

Attaches the current terminal to the tmux session. Dismiss with Ctrl-B D.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionAttach,
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	Long: `List all running raider sessions.

Shows session status, warband, and raider name. Use --warband to filter by warband.`,
	RunE: runSessionList,
}

var sessionCaptureCmd = &cobra.Command{
	Use:   "capture <warband>/<raider> [count]",
	Short: "Capture recent session output",
	Long: `Capture recent output from a raider session.

Returns the last N lines of terminal output. Useful for checking progress.

Examples:
  hd session capture wyvern/Toast        # Last 100 lines (default)
  hd session capture wyvern/Toast 50     # Last 50 lines
  hd session capture wyvern/Toast -n 50  # Same as above`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSessionCapture,
}

var sessionInjectCmd = &cobra.Command{
	Use:   "inject <warband>/<raider>",
	Short: "Send message to session (prefer 'hd signal')",
	Long: `Send a message to a raider session.

NOTE: For sending messages to Claude sessions, use 'hd signal' instead.
It uses reliable delivery (literal mode + timing) that works correctly
with Claude Code's input handling.

This command is a low-level primitive for file-based injection or
cases where you need raw tmux send-keys behavior.

Examples:
  hd signal greenplace/furiosa "Check your drums"     # Preferred
  hd session inject wyvern/Toast -f prompt.txt   # For file injection`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionInject,
}

var sessionRestartCmd = &cobra.Command{
	Use:   "restart <warband>/<raider>",
	Short: "Restart a raider session",
	Long: `Restart a raider session (stop + start).

Gracefully stops the current session and starts a fresh one.
Use --force to skip graceful shutdown.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionRestart,
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status <warband>/<raider>",
	Short: "Show session status details",
	Long: `Show detailed status for a raider session.

Displays running state, uptime, session info, and activity.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionStatus,
}

var sessionCheckCmd = &cobra.Command{
	Use:   "check [warband]",
	Short: "Check session health for raiders",
	Long: `Check if raider tmux sessions are alive and healthy.

This command validates that:
1. Raiders with work-on-hook have running tmux sessions
2. Sessions are responsive

Use this for manual health checks or debugging session issues.

Examples:
  hd session check              # Check all warbands
  hd session check greenplace      # Check specific warband`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSessionCheck,
}

func init() {
	// Start flags
	sessionStartCmd.Flags().StringVar(&sessionIssue, "issue", "", "Issue ID to work on")

	// Stop flags
	sessionStopCmd.Flags().BoolVarP(&sessionForce, "force", "f", false, "Force immediate shutdown")

	// List flags
	sessionListCmd.Flags().StringVar(&sessionRigFilter, "warband", "", "Filter by warband name")
	sessionListCmd.Flags().BoolVar(&sessionListJSON, "json", false, "Output as JSON")

	// Capture flags
	sessionCaptureCmd.Flags().IntVarP(&sessionLines, "lines", "n", 100, "Number of lines to capture")

	// Inject flags
	sessionInjectCmd.Flags().StringVarP(&sessionMessage, "message", "m", "", "Message to inject")
	sessionInjectCmd.Flags().StringVarP(&sessionFile, "file", "f", "", "File to read message from")

	// Restart flags
	sessionRestartCmd.Flags().BoolVarP(&sessionForce, "force", "f", false, "Force immediate shutdown")

	// Add subcommands
	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionStopCmd)
	sessionCmd.AddCommand(sessionAtCmd)
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionCaptureCmd)
	sessionCmd.AddCommand(sessionInjectCmd)
	sessionCmd.AddCommand(sessionRestartCmd)
	sessionCmd.AddCommand(sessionStatusCmd)
	sessionCmd.AddCommand(sessionCheckCmd)

	rootCmd.AddCommand(sessionCmd)
}

// parseAddress parses "warband/raider" format.
// If no "/" is present, attempts to infer warband from current directory.
func parseAddress(addr string) (rigName, raiderName string, err error) {
	parts := strings.SplitN(addr, "/", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], nil
	}

	// No slash - try to infer warband from cwd
	if !strings.Contains(addr, "/") && addr != "" {
		townRoot, err := workspace.FindFromCwd()
		if err == nil && townRoot != "" {
			inferredRig, err := inferRigFromCwd(townRoot)
			if err == nil && inferredRig != "" {
				return inferredRig, addr, nil
			}
		}
	}

	return "", "", fmt.Errorf("invalid address format: expected 'warband/raider', got '%s'", addr)
}

// getSessionManager creates a session manager for the given warband.
func getSessionManager(rigName string) (*raider.SessionManager, *warband.Warband, error) {
	_, r, err := getRig(rigName)
	if err != nil {
		return nil, nil, err
	}

	t := tmux.NewTmux()
	raiderMgr := raider.NewSessionManager(t, r)

	return raiderMgr, r, nil
}

func runSessionStart(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	raiderMgr, r, err := getSessionManager(rigName)
	if err != nil {
		return err
	}

	// Check raider exists
	found := false
	for _, p := range r.Raiders {
		if p == raiderName {
			found = true
			break
		}
	}
	if !found {
		suggestions := suggest.FindSimilar(raiderName, r.Raiders, 3)
		hint := fmt.Sprintf("Create with: hd raider add %s/%s", rigName, raiderName)
		return fmt.Errorf("%s", suggest.FormatSuggestion("Raider", raiderName, suggestions, hint))
	}

	opts := raider.SessionStartOptions{
		Issue: sessionIssue,
	}

	fmt.Printf("Starting session for %s/%s...\n", rigName, raiderName)
	if err := raiderMgr.Start(raiderName, opts); err != nil {
		return fmt.Errorf("starting session: %w", err)
	}

	fmt.Printf("%s Session started. Summon with: %s\n",
		style.Bold.Render("✓"),
		style.Dim.Render(fmt.Sprintf("hd session at %s/%s", rigName, raiderName)))

	// Log wake event
	if townRoot, err := workspace.FindFromCwd(); err == nil && townRoot != "" {
		agent := fmt.Sprintf("%s/%s", rigName, raiderName)
		logger := encampmentlog.NewLogger(townRoot)
		_ = logger.Log(encampmentlog.EventWake, agent, sessionIssue)
	}

	return nil
}

func runSessionStop(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	raiderMgr, _, err := getSessionManager(rigName)
	if err != nil {
		return err
	}

	if sessionForce {
		fmt.Printf("Force stopping session for %s/%s...\n", rigName, raiderName)
	} else {
		fmt.Printf("Stopping session for %s/%s...\n", rigName, raiderName)
	}
	if err := raiderMgr.Stop(raiderName, sessionForce); err != nil {
		return fmt.Errorf("stopping session: %w", err)
	}

	fmt.Printf("%s Session stopped.\n", style.Bold.Render("✓"))

	// Log kill event
	if townRoot, err := workspace.FindFromCwd(); err == nil && townRoot != "" {
		agent := fmt.Sprintf("%s/%s", rigName, raiderName)
		reason := "hd session stop"
		if sessionForce {
			reason = "hd session stop --force"
		}
		logger := encampmentlog.NewLogger(townRoot)
		_ = logger.Log(encampmentlog.EventKill, agent, reason)
	}

	return nil
}

func runSessionAttach(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	raiderMgr, _, err := getSessionManager(rigName)
	if err != nil {
		return err
	}

	// Summon (this replaces the process)
	return raiderMgr.Summon(raiderName)
}

// SessionListItem represents a session in list output.
type SessionListItem struct {
	Warband       string `json:"warband"`
	Raider   string `json:"raider"`
	SessionID string `json:"session_id"`
	Running   bool   `json:"running"`
}

func runSessionList(cmd *cobra.Command, args []string) error {
	// Find encampment root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	// Get all warbands
	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	warbands, err := rigMgr.DiscoverRigs()
	if err != nil {
		return fmt.Errorf("discovering warbands: %w", err)
	}

	// Filter if requested
	if sessionRigFilter != "" {
		var filtered []*warband.Warband
		for _, r := range warbands {
			if r.Name == sessionRigFilter {
				filtered = append(filtered, r)
			}
		}
		warbands = filtered
	}

	// Collect sessions from all warbands
	t := tmux.NewTmux()
	var allSessions []SessionListItem

	for _, r := range warbands {
		raiderMgr := raider.NewSessionManager(t, r)
		infos, err := raiderMgr.List()
		if err != nil {
			continue
		}

		for _, info := range infos {
			allSessions = append(allSessions, SessionListItem{
				Warband:       r.Name,
				Raider:   info.Raider,
				SessionID: info.SessionID,
				Running:   info.Running,
			})
		}
	}

	// Output
	if sessionListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(allSessions)
	}

	if len(allSessions) == 0 {
		fmt.Println("No active sessions.")
		return nil
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Active Sessions"))
	for _, s := range allSessions {
		status := style.Bold.Render("●")
		if !s.Running {
			status = style.Dim.Render("○")
		}
		fmt.Printf("  %s %s/%s\n", status, s.Warband, s.Raider)
		fmt.Printf("    %s\n", style.Dim.Render(s.SessionID))
	}

	return nil
}

func runSessionCapture(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	raiderMgr, _, err := getSessionManager(rigName)
	if err != nil {
		return err
	}

	// Use positional count if provided, otherwise use flag value
	lines := sessionLines
	if len(args) > 1 {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid line count '%s': must be a number", args[1])
		}
		if n <= 0 {
			return fmt.Errorf("line count must be positive, got %d", n)
		}
		lines = n
	}

	output, err := raiderMgr.Capture(raiderName, lines)
	if err != nil {
		return fmt.Errorf("capturing output: %w", err)
	}

	fmt.Print(output)
	return nil
}

func runSessionInject(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	// Get message
	message := sessionMessage
	if sessionFile != "" {
		data, err := os.ReadFile(sessionFile)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}
		message = string(data)
	}

	if message == "" {
		return fmt.Errorf("no message provided (use -m or -f)")
	}

	raiderMgr, _, err := getSessionManager(rigName)
	if err != nil {
		return err
	}

	if err := raiderMgr.Inject(raiderName, message); err != nil {
		return fmt.Errorf("injecting message: %w", err)
	}

	fmt.Printf("%s Message sent to %s/%s\n",
		style.Bold.Render("✓"), rigName, raiderName)
	return nil
}

func runSessionRestart(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	raiderMgr, _, err := getSessionManager(rigName)
	if err != nil {
		return err
	}

	// Check if running
	running, err := raiderMgr.IsRunning(raiderName)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}

	if running {
		// Stop first
		if sessionForce {
			fmt.Printf("Force stopping session for %s/%s...\n", rigName, raiderName)
		} else {
			fmt.Printf("Stopping session for %s/%s...\n", rigName, raiderName)
		}
		if err := raiderMgr.Stop(raiderName, sessionForce); err != nil {
			return fmt.Errorf("stopping session: %w", err)
		}
	}

	// Start fresh session
	fmt.Printf("Starting session for %s/%s...\n", rigName, raiderName)
	opts := raider.SessionStartOptions{}
	if err := raiderMgr.Start(raiderName, opts); err != nil {
		return fmt.Errorf("starting session: %w", err)
	}

	fmt.Printf("%s Session restarted. Summon with: %s\n",
		style.Bold.Render("✓"),
		style.Dim.Render(fmt.Sprintf("hd session at %s/%s", rigName, raiderName)))
	return nil
}

func runSessionStatus(cmd *cobra.Command, args []string) error {
	rigName, raiderName, err := parseAddress(args[0])
	if err != nil {
		return err
	}

	raiderMgr, _, err := getSessionManager(rigName)
	if err != nil {
		return err
	}

	// Get session info
	info, err := raiderMgr.Status(raiderName)
	if err != nil {
		return fmt.Errorf("getting status: %w", err)
	}

	// Format output
	fmt.Printf("%s Session: %s/%s\n\n", style.Bold.Render("📺"), rigName, raiderName)

	if info.Running {
		fmt.Printf("  State: %s\n", style.Bold.Render("● running"))
	} else {
		fmt.Printf("  State: %s\n", style.Dim.Render("○ stopped"))
		return nil
	}

	fmt.Printf("  Session ID: %s\n", info.SessionID)

	if info.Attached {
		fmt.Printf("  Attached: yes\n")
	} else {
		fmt.Printf("  Attached: no\n")
	}

	if !info.Created.IsZero() {
		uptime := time.Since(info.Created)
		fmt.Printf("  Created: %s\n", info.Created.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Uptime: %s\n", formatDuration(uptime))
	}

	fmt.Printf("\nAttach with: %s\n", style.Dim.Render(fmt.Sprintf("hd session at %s/%s", rigName, raiderName)))
	return nil
}

// formatDuration formats a duration for human display.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours >= 24 {
		days := hours / 24
		hours = hours % 24
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}

func runSessionCheck(cmd *cobra.Command, args []string) error {
	// Find encampment root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	// Get warbands to check
	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	warbands, err := rigMgr.DiscoverRigs()
	if err != nil {
		return fmt.Errorf("discovering warbands: %w", err)
	}

	// Filter if specific warband requested
	if len(args) > 0 {
		rigFilter := args[0]
		var filtered []*warband.Warband
		for _, r := range warbands {
			if r.Name == rigFilter {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("warband not found: %s", rigFilter)
		}
		warbands = filtered
	}

	fmt.Printf("%s Session Health Check\n\n", style.Bold.Render("🔍"))

	t := tmux.NewTmux()
	totalChecked := 0
	totalHealthy := 0
	totalCrashed := 0

	for _, r := range warbands {
		raidersDir := filepath.Join(r.Path, "raiders")
		entries, err := os.ReadDir(raidersDir)
		if err != nil {
			continue // Warband might not have raiders
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			raiderName := entry.Name()
			sessionName := fmt.Sprintf("gt-%s-%s", r.Name, raiderName)
			totalChecked++

			// Check if session exists
			running, err := t.HasSession(sessionName)
			if err != nil {
				fmt.Printf("  %s %s/%s: %s\n", style.Bold.Render("⚠"), r.Name, raiderName, style.Dim.Render("error checking session"))
				continue
			}

			if running {
				fmt.Printf("  %s %s/%s: %s\n", style.Bold.Render("✓"), r.Name, raiderName, style.Dim.Render("session alive"))
				totalHealthy++
			} else {
				// Check if raider has work on hook (would need restart)
				fmt.Printf("  %s %s/%s: %s\n", style.Bold.Render("✗"), r.Name, raiderName, style.Dim.Render("session not running"))
				totalCrashed++
			}
		}
	}

	// Summary
	fmt.Printf("\n%s Summary: %d checked, %d healthy, %d not running\n",
		style.Bold.Render("📊"), totalChecked, totalHealthy, totalCrashed)

	if totalCrashed > 0 {
		fmt.Printf("\n%s To restart crashed raiders: hd session restart <warband>/<raider>\n",
			style.Dim.Render("Tip:"))
	}

	return nil
}
