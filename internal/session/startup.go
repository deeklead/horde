// Package session provides raider session lifecycle management.
package session

import (
	"fmt"
	"time"

	"github.com/deeklead/horde/internal/tmux"
)

// StartupNudgeConfig configures a startup signal message.
type StartupNudgeConfig struct {
	// Recipient is the address of the agent being nudged.
	// Examples: "horde/clan/gus", "shaman", "horde/witness"
	Recipient string

	// Sender is the agent initiating the signal.
	// Examples: "warchief", "shaman", "self" (for handoff)
	Sender string

	// Topic describes why the session was started.
	// Examples: "cold-start", "handoff", "assigned", or a totem-id
	Topic string

	// MolID is an optional totem ID being worked.
	// If provided, appended to topic as "topic:totem-id"
	MolID string
}

// StartupNudge sends a formatted startup message to a Claude Code session.
// The message becomes the session title in Claude Code's /resume picker,
// enabling workers to find predecessor sessions.
//
// Format: [GAS ENCAMPMENT] <recipient> <- <sender> • <timestamp> • <topic[:totem-id]>
//
// Examples:
//   - [GAS ENCAMPMENT] horde/clan/gus <- shaman • 2025-12-30T15:42 • assigned:gt-abc12
//   - [GAS ENCAMPMENT] shaman <- warchief • 2025-12-30T08:00 • cold-start
//   - [GAS ENCAMPMENT] horde/witness <- self • 2025-12-30T14:00 • handoff
//
// The message content doesn't trigger GUPP - CLAUDE.md and hooks handle that.
// The metadata makes sessions identifiable in /resume.
func StartupNudge(t *tmux.Tmux, session string, cfg StartupNudgeConfig) error {
	message := FormatStartupNudge(cfg)
	return t.SignalSession(session, message)
}

// FormatStartupNudge builds the formatted startup signal message.
// Separated from StartupNudge for testing and reuse.
func FormatStartupNudge(cfg StartupNudgeConfig) string {
	// Use local time in compact format
	timestamp := time.Now().Format("2006-01-02T15:04")

	// Build topic string - append totem-id if provided
	topic := cfg.Topic
	if cfg.MolID != "" && cfg.Topic != "" {
		topic = fmt.Sprintf("%s:%s", cfg.Topic, cfg.MolID)
	} else if cfg.MolID != "" {
		topic = cfg.MolID
	} else if topic == "" {
		topic = "ready"
	}

	// Build the beacon: [GAS ENCAMPMENT] recipient <- sender • timestamp • topic
	beacon := fmt.Sprintf("[GAS ENCAMPMENT] %s <- %s • %s • %s",
		cfg.Recipient, cfg.Sender, timestamp, topic)

	// For handoff and cold-start, add explicit instructions so the agent knows what to do
	// even if hooks haven't loaded CLAUDE.md yet
	if cfg.Topic == "handoff" || cfg.Topic == "cold-start" {
		beacon += "\n\nCheck your hook and drums, then act on the hook if present:\n" +
			"1. `hd hook` - shows bannered work (if any)\n" +
			"2. `hd drums inbox` - check for messages\n" +
			"3. If work is bannered → execute it immediately\n" +
			"4. If nothing bannered → wait for instructions"
	}

	// For assigned, work is already on the hook - just tell them to run it
	// This prevents the "helpful assistant" exploration pattern (see PRIMING.md)
	if cfg.Topic == "assigned" {
		beacon += "\n\nWork is on your hook. Run `hd hook` now and begin immediately."
	}

	// For start/restart, add fallback instructions in case SessionStart hook fails
	// to inject context via hd rally. This prevents the "No recent activity" state
	// where agents sit idle because they received only metadata, no instructions.
	// See: gt-uoc64 (clan workers starting without proper context injection)
	if cfg.Topic == "start" || cfg.Topic == "restart" {
		beacon += "\n\nRun `hd rally` now for full context, then check your hook and drums."
	}

	return beacon
}
