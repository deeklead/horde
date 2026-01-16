package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/encampmentlog"
	"github.com/deeklead/horde/internal/workspace"
)

// Log command flags
var (
	logTail   int
	logType   string
	logAgent  string
	logSince  string
	logFollow bool

	// log crash flags
	crashAgent    string
	crashSession  string
	crashExitCode int
)

var logCmd = &cobra.Command{
	Use:     "log",
	GroupID: GroupDiag,
	Short:   "View encampment activity log",
	Long: `View the centralized log of Horde agent lifecycle events.

Events logged include:
  muster   - new agent created
  wake    - agent resumed
  signal   - message injected into agent
  handoff - agent handed off to fresh session
  done    - agent finished work
  crash   - agent exited unexpectedly
  kill    - agent killed intentionally

Examples:
  hd log                     # Show last 20 events
  hd log -n 50               # Show last 50 events
  hd log --type muster        # Show only muster events
  hd log --agent greenplace/    # Show events for horde warband
  hd log --since 1h          # Show events from last hour
  hd log -f                  # Follow log (like tail -f)`,
	RunE: runLog,
}

var logCrashCmd = &cobra.Command{
	Use:   "crash",
	Short: "Record a crash event (called by tmux pane-died hook)",
	Long: `Record a crash event to the encampment log.

This command is called automatically by tmux when a pane exits unexpectedly.
It's not typically run manually.

The exit code determines if this was a crash or expected exit:
  - Exit code 0: Expected exit (logged as 'done' if no other done was recorded)
  - Exit code non-zero: Crash (logged as 'crash')

Examples:
  hd log crash --agent greenplace/Toast --session gt-greenplace-Toast --exit-code 1`,
	RunE: runLogCrash,
}

func init() {
	logCmd.Flags().IntVarP(&logTail, "tail", "n", 20, "Number of events to show")
	logCmd.Flags().StringVarP(&logType, "type", "t", "", "Filter by event type (muster,wake,signal,handoff,done,crash,kill)")
	logCmd.Flags().StringVarP(&logAgent, "agent", "a", "", "Filter by agent prefix (e.g., horde/, greenplace/clan/max)")
	logCmd.Flags().StringVar(&logSince, "since", "", "Show events since duration (e.g., 1h, 30m, 24h)")
	logCmd.Flags().BoolVarP(&logFollow, "follow", "f", false, "Follow log output (like tail -f)")

	// crash subcommand flags
	logCrashCmd.Flags().StringVar(&crashAgent, "agent", "", "Agent ID (e.g., greenplace/Toast)")
	logCrashCmd.Flags().StringVar(&crashSession, "session", "", "Tmux session name")
	logCrashCmd.Flags().IntVar(&crashExitCode, "exit-code", -1, "Exit code from pane")
	_ = logCrashCmd.MarkFlagRequired("agent")

	logCmd.AddCommand(logCrashCmd)
	rootCmd.AddCommand(logCmd)
}

func runLog(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	logPath := fmt.Sprintf("%s/logs/encampment.log", townRoot)

	// If following, use tail -f
	if logFollow {
		return followLog(logPath)
	}

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Printf("%s No log file yet (no events recorded)\n", style.Dim.Render("○"))
		return nil
	}

	// Read events
	events, err := encampmentlog.ReadEvents(townRoot)
	if err != nil {
		return fmt.Errorf("reading events: %w", err)
	}

	if len(events) == 0 {
		fmt.Printf("%s No events in log\n", style.Dim.Render("○"))
		return nil
	}

	// Build filter
	filter := encampmentlog.Filter{}

	if logType != "" {
		filter.Type = encampmentlog.EventType(logType)
	}

	if logAgent != "" {
		filter.Agent = logAgent
	}

	if logSince != "" {
		duration, err := time.ParseDuration(logSince)
		if err != nil {
			return fmt.Errorf("invalid --since duration: %w", err)
		}
		filter.Since = time.Now().Add(-duration)
	}

	// Apply filter
	events = encampmentlog.FilterEvents(events, filter)

	// Apply tail limit
	if logTail > 0 && len(events) > logTail {
		events = events[len(events)-logTail:]
	}

	if len(events) == 0 {
		fmt.Printf("%s No events match filter\n", style.Dim.Render("○"))
		return nil
	}

	// Print events
	for _, e := range events {
		printEvent(e)
	}

	return nil
}

// followLog uses tail -f to follow the log file.
func followLog(logPath string) error {
	// Check if log file exists, create empty if not
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		// Create logs directory and empty file
		if err := os.MkdirAll(fmt.Sprintf("%s", logPath[:len(logPath)-len("encampment.log")-1]), 0755); err != nil {
			return fmt.Errorf("creating logs directory: %w", err)
		}
		if _, err := os.Create(logPath); err != nil {
			return fmt.Errorf("creating log file: %w", err)
		}
	}

	fmt.Printf("%s Following %s (Ctrl+C to stop)\n\n", style.Dim.Render("○"), logPath)

	tailCmd := exec.Command("tail", "-f", logPath)
	tailCmd.Stdout = os.Stdout
	tailCmd.Stderr = os.Stderr

	return tailCmd.Run()
}

// printEvent prints a single event with styling.
func printEvent(e encampmentlog.Event) {
	ts := e.Timestamp.Format("2006-01-02 15:04:05")

	// Color-code event types
	var typeStr string
	switch e.Type {
	case encampmentlog.EventSpawn:
		typeStr = style.Success.Render("[muster]")
	case encampmentlog.EventWake:
		typeStr = style.Bold.Render("[wake]")
	case encampmentlog.EventNudge:
		typeStr = style.Dim.Render("[signal]")
	case encampmentlog.EventHandoff:
		typeStr = style.Bold.Render("[handoff]")
	case encampmentlog.EventDone:
		typeStr = style.Success.Render("[done]")
	case encampmentlog.EventCrash:
		typeStr = style.Error.Render("[crash]")
	case encampmentlog.EventKill:
		typeStr = style.Warning.Render("[kill]")
	case encampmentlog.EventCallback:
		typeStr = style.Bold.Render("[callback]")
	case encampmentlog.EventPatrolStarted:
		typeStr = style.Bold.Render("[patrol_started]")
	case encampmentlog.EventRaiderChecked:
		typeStr = style.Dim.Render("[raider_checked]")
	case encampmentlog.EventRaiderNudged:
		typeStr = style.Warning.Render("[raider_nudged]")
	case encampmentlog.EventEscalationSent:
		typeStr = style.Error.Render("[escalation_sent]")
	case encampmentlog.EventPatrolComplete:
		typeStr = style.Success.Render("[patrol_complete]")
	default:
		typeStr = fmt.Sprintf("[%s]", e.Type)
	}

	detail := formatEventDetail(e)
	fmt.Printf("%s %s %s %s\n", style.Dim.Render(ts), typeStr, e.Agent, detail)
}

// formatEventDetail returns a human-readable detail string for an event.
func formatEventDetail(e encampmentlog.Event) string {
	switch e.Type {
	case encampmentlog.EventSpawn:
		if e.Context != "" {
			return fmt.Sprintf("spawned for %s", e.Context)
		}
		return "spawned"
	case encampmentlog.EventWake:
		if e.Context != "" {
			return fmt.Sprintf("resumed (%s)", e.Context)
		}
		return "resumed"
	case encampmentlog.EventNudge:
		if e.Context != "" {
			return fmt.Sprintf("nudged with %q", truncateStr(e.Context, 40))
		}
		return "nudged"
	case encampmentlog.EventHandoff:
		if e.Context != "" {
			return fmt.Sprintf("handed off (%s)", e.Context)
		}
		return "handed off"
	case encampmentlog.EventDone:
		if e.Context != "" {
			return fmt.Sprintf("completed %s", e.Context)
		}
		return "completed work"
	case encampmentlog.EventCrash:
		if e.Context != "" {
			return fmt.Sprintf("exited unexpectedly (%s)", e.Context)
		}
		return "exited unexpectedly"
	case encampmentlog.EventKill:
		if e.Context != "" {
			return fmt.Sprintf("killed (%s)", e.Context)
		}
		return "killed"
	case encampmentlog.EventCallback:
		if e.Context != "" {
			return fmt.Sprintf("callback: %s", e.Context)
		}
		return "callback processed"
	case encampmentlog.EventPatrolStarted:
		if e.Context != "" {
			return fmt.Sprintf("started scout (%s)", e.Context)
		}
		return "started scout"
	case encampmentlog.EventRaiderChecked:
		if e.Context != "" {
			return fmt.Sprintf("checked %s", e.Context)
		}
		return "checked raider"
	case encampmentlog.EventRaiderNudged:
		if e.Context != "" {
			return fmt.Sprintf("nudged (%s)", e.Context)
		}
		return "nudged raider"
	case encampmentlog.EventEscalationSent:
		if e.Context != "" {
			return fmt.Sprintf("escalated (%s)", e.Context)
		}
		return "escalated"
	case encampmentlog.EventPatrolComplete:
		if e.Context != "" {
			return fmt.Sprintf("scout complete (%s)", e.Context)
		}
		return "scout complete"
	default:
		if e.Context != "" {
			return fmt.Sprintf("%s (%s)", e.Type, e.Context)
		}
		return string(e.Type)
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// runLogCrash handles the "hd log crash" command from tmux pane-died hooks.
func runLogCrash(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		// Try to find encampment root from conventional location
		// This is called from tmux hook which may not have proper cwd
		home := os.Getenv("HOME")
		defaultRoot := home + "/gt"
		if _, statErr := os.Stat(defaultRoot + "/warchief"); statErr == nil {
			townRoot = defaultRoot
		}
		if townRoot == "" {
			return fmt.Errorf("cannot find encampment root (tried cwd and ~/horde)")
		}
	}

	// Determine event type based on exit code
	var eventType encampmentlog.EventType
	var context string

	if crashExitCode == 0 {
		// Exit code 0 = normal exit
		// Could be handoff, done, or user quit - we log as "done" if no prior done event
		// The Witness can analyze further if needed
		eventType = encampmentlog.EventDone
		context = "exited normally"
	} else if crashExitCode == 130 {
		// Exit code 130 = Ctrl+C (SIGINT)
		// This is typically intentional user interrupt
		eventType = encampmentlog.EventKill
		context = fmt.Sprintf("interrupted (exit %d)", crashExitCode)
	} else {
		// Non-zero exit = crash
		eventType = encampmentlog.EventCrash
		context = fmt.Sprintf("exit code %d", crashExitCode)
		if crashSession != "" {
			context += fmt.Sprintf(" (session: %s)", crashSession)
		}
	}

	// Log the event
	logger := encampmentlog.NewLogger(townRoot)
	if err := logger.Log(eventType, crashAgent, context); err != nil {
		return fmt.Errorf("logging event: %w", err)
	}

	return nil
}

// LogEvent is a helper that logs an event from anywhere in the codebase.
// It finds the encampment root and logs the event.
func LogEvent(eventType encampmentlog.EventType, agent, context string) error {
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return err // Silently fail if not in a workspace
	}
	if townRoot == "" {
		return nil
	}

	logger := encampmentlog.NewLogger(townRoot)
	return logger.Log(eventType, agent, context)
}

// LogEventWithRoot logs an event when the encampment root is already known.
func LogEventWithRoot(townRoot string, eventType encampmentlog.EventType, agent, context string) error {
	logger := encampmentlog.NewLogger(townRoot)
	return logger.Log(eventType, agent, context)
}

// Convenience functions for common events

// LogSpawn logs a muster event.
func LogSpawn(townRoot, agent, issueID string) error {
	return LogEventWithRoot(townRoot, encampmentlog.EventSpawn, agent, issueID)
}

// LogWake logs a wake event.
func LogWake(townRoot, agent, context string) error {
	return LogEventWithRoot(townRoot, encampmentlog.EventWake, agent, context)
}

// LogNudge logs a signal event.
func LogNudge(townRoot, agent, message string) error {
	return LogEventWithRoot(townRoot, encampmentlog.EventNudge, agent, strings.TrimSpace(message))
}

// LogHandoff logs a handoff event.
func LogHandoff(townRoot, agent, context string) error {
	return LogEventWithRoot(townRoot, encampmentlog.EventHandoff, agent, context)
}

// LogDone logs a done event.
func LogDone(townRoot, agent, issueID string) error {
	return LogEventWithRoot(townRoot, encampmentlog.EventDone, agent, issueID)
}

// LogCrash logs a crash event.
func LogCrash(townRoot, agent, reason string) error {
	return LogEventWithRoot(townRoot, encampmentlog.EventCrash, agent, reason)
}

// LogKill logs a kill event.
func LogKill(townRoot, agent, reason string) error {
	return LogEventWithRoot(townRoot, encampmentlog.EventKill, agent, reason)
}
