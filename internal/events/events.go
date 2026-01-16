// Package events provides event logging for the hd activity feed.
//
// Events are written to ~/horde/.events.jsonl (raw audit log) and later
// curated by the feed daemon into ~/.feed.jsonl (user-facing).
package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/deeklead/horde/internal/workspace"
)

// Event represents an activity event in Horde.
type Event struct {
	Timestamp  string                 `json:"ts"`
	Source     string                 `json:"source"`
	Type       string                 `json:"type"`
	Actor      string                 `json:"actor"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	Visibility string                 `json:"visibility"`
}

// Visibility levels for events.
const (
	VisibilityAudit = "audit" // Only in raw events log
	VisibilityFeed  = "feed"  // Appears in curated feed
	VisibilityBoth  = "both"  // Both audit and feed
)

// Common event types for hd commands.
const (
	TypeSling   = "charge"
	TypeHook    = "banner"
	TypeUnhook  = "unhook"
	TypeHandoff = "handoff"
	TypeDone    = "done"
	TypeMail    = "drums"
	TypeSpawn   = "muster"
	TypeKill    = "kill"
	TypeNudge   = "signal"
	TypeBoot    = "boot"
	TypeHalt    = "halt"

	// Session events (for seance discovery)
	TypeSessionStart = "session_start"
	TypeSessionEnd   = "session_end"

	// Session death events (for crash investigation)
	TypeSessionDeath = "session_death" // Feed-visible session termination
	TypeMassDeath    = "mass_death"    // Multiple sessions died in short window

	// Witness scout events
	TypePatrolStarted   = "patrol_started"
	TypeRaiderChecked  = "raider_checked"
	TypeRaiderNudged   = "raider_nudged"
	TypeEscalationSent   = "escalation_sent"
	TypeEscalationAcked  = "escalation_acked"
	TypeEscalationClosed = "escalation_closed"
	TypePatrolComplete   = "patrol_complete"

	// Merge queue events (emitted by forge)
	TypeMergeStarted = "merge_started"
	TypeMerged       = "merged"
	TypeMergeFailed  = "merge_failed"
	TypeMergeSkipped = "merge_skipped"
)

// EventsFile is the name of the raw events log.
const EventsFile = ".events.jsonl"

// mutex protects concurrent writes to the events file.
var mutex sync.Mutex

// Log writes an event to the events log.
// The event is appended to ~/horde/.events.jsonl.
// Returns nil if logging fails (events are best-effort).
func Log(eventType, actor string, payload map[string]interface{}, visibility string) error {
	event := Event{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Source:     "hd",
		Type:       eventType,
		Actor:      actor,
		Payload:    payload,
		Visibility: visibility,
	}
	return write(event)
}

// LogFeed is a convenience wrapper for feed-visible events.
func LogFeed(eventType, actor string, payload map[string]interface{}) error {
	return Log(eventType, actor, payload, VisibilityFeed)
}

// LogAudit is a convenience wrapper for audit-only events.
func LogAudit(eventType, actor string, payload map[string]interface{}) error {
	return Log(eventType, actor, payload, VisibilityAudit)
}

// write appends an event to the events file.
func write(event Event) error {
	// Find encampment root
	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		// Silently ignore - we're not in a Horde workspace
		return nil
	}

	eventsPath := filepath.Join(townRoot, EventsFile)

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}
	data = append(data, '\n')

	// Append to file with proper locking
	mutex.Lock()
	defer mutex.Unlock()

	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gosec // G302: events file is non-sensitive operational data
	if err != nil {
		return fmt.Errorf("opening events file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing event: %w", err)
	}

	return nil
}

// Payload helpers for common event structures.

// SlingPayload creates a payload for charge events.
func SlingPayload(beadID, target string) map[string]interface{} {
	return map[string]interface{}{
		"bead":   beadID,
		"target": target,
	}
}

// HookPayload creates a payload for hook events.
func HookPayload(beadID string) map[string]interface{} {
	return map[string]interface{}{
		"bead": beadID,
	}
}

// HandoffPayload creates a payload for handoff events.
func HandoffPayload(subject string, toSession bool) map[string]interface{} {
	p := map[string]interface{}{
		"to_session": toSession,
	}
	if subject != "" {
		p["subject"] = subject
	}
	return p
}

// DonePayload creates a payload for done events.
func DonePayload(beadID, branch string) map[string]interface{} {
	return map[string]interface{}{
		"bead":   beadID,
		"branch": branch,
	}
}

// MailPayload creates a payload for drums events.
func MailPayload(to, subject string) map[string]interface{} {
	return map[string]interface{}{
		"to":      to,
		"subject": subject,
	}
}

// SpawnPayload creates a payload for muster events.
func SpawnPayload(warband, raider string) map[string]interface{} {
	return map[string]interface{}{
		"warband":     warband,
		"raider": raider,
	}
}

// BootPayload creates a payload for warband boot events.
func BootPayload(warband string, agents []string) map[string]interface{} {
	return map[string]interface{}{
		"warband":    warband,
		"agents": agents,
	}
}

// MergePayload creates a payload for merge queue events.
// mrID: merge request ID
// worker: raider name that submitted the work
// branch: source branch being merged
// reason: failure reason (for merge_failed/merge_skipped events)
func MergePayload(mrID, worker, branch, reason string) map[string]interface{} {
	p := map[string]interface{}{
		"mr":     mrID,
		"worker": worker,
		"branch": branch,
	}
	if reason != "" {
		p["reason"] = reason
	}
	return p
}

// PatrolPayload creates a payload for scout start/complete events.
func PatrolPayload(warband string, raiderCount int, message string) map[string]interface{} {
	p := map[string]interface{}{
		"warband":           warband,
		"raider_count": raiderCount,
	}
	if message != "" {
		p["message"] = message
	}
	return p
}

// RaiderCheckPayload creates a payload for raider check events.
func RaiderCheckPayload(warband, raider, status, issue string) map[string]interface{} {
	p := map[string]interface{}{
		"warband":     warband,
		"raider": raider,
		"status":  status,
	}
	if issue != "" {
		p["issue"] = issue
	}
	return p
}

// NudgePayload creates a payload for signal events.
func NudgePayload(warband, target, reason string) map[string]interface{} {
	return map[string]interface{}{
		"warband":    warband,
		"target": target,
		"reason": reason,
	}
}

// EscalationPayload creates a payload for escalation events.
func EscalationPayload(warband, target, to, reason string) map[string]interface{} {
	return map[string]interface{}{
		"warband":    warband,
		"target": target,
		"to":     to,
		"reason": reason,
	}
}

// UnhookPayload creates a payload for unhook events.
func UnhookPayload(beadID string) map[string]interface{} {
	return map[string]interface{}{
		"bead": beadID,
	}
}

// KillPayload creates a payload for kill events.
func KillPayload(warband, target, reason string) map[string]interface{} {
	return map[string]interface{}{
		"warband":    warband,
		"target": target,
		"reason": reason,
	}
}

// HaltPayload creates a payload for halt events.
func HaltPayload(services []string) map[string]interface{} {
	return map[string]interface{}{
		"services": services,
	}
}

// SessionDeathPayload creates a payload for session death events.
// session: tmux session name that died
// agent: Horde agent identity (e.g., "horde/raiders/Toast")
// reason: why the session was killed (e.g., "zombie cleanup", "user request", "doctor fix")
// caller: what initiated the kill (e.g., "daemon", "doctor", "hd down")
func SessionDeathPayload(session, agent, reason, caller string) map[string]interface{} {
	return map[string]interface{}{
		"session": session,
		"agent":   agent,
		"reason":  reason,
		"caller":  caller,
	}
}

// MassDeathPayload creates a payload for mass death events.
// count: number of sessions that died
// window: time window in which deaths occurred (e.g., "5s")
// sessions: list of session names that died
// possibleCause: suspected cause if known
func MassDeathPayload(count int, window string, sessions []string, possibleCause string) map[string]interface{} {
	p := map[string]interface{}{
		"count":    count,
		"window":   window,
		"sessions": sessions,
	}
	if possibleCause != "" {
		p["possible_cause"] = possibleCause
	}
	return p
}

// SessionPayload creates a payload for session start/end events.
// sessionID: Claude Code session UUID
// role: Horde role (e.g., "horde/clan/joe", "shaman")
// topic: What the session is working on
// cwd: Working directory
func SessionPayload(sessionID, role, topic, cwd string) map[string]interface{} {
	p := map[string]interface{}{
		"session_id": sessionID,
		"role":       role,
		"actor_pid":  fmt.Sprintf("%s-%d", role, os.Getpid()),
	}
	if topic != "" {
		p["topic"] = topic
	}
	if cwd != "" {
		p["cwd"] = cwd
	}
	return p
}
