package feed

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/relics"
)

// EventSource represents a source of events
type EventSource interface {
	Events() <-chan Event
	Close() error
}

// BdActivitySource reads events from rl activity --follow
type BdActivitySource struct {
	cmd     *exec.Cmd
	events  chan Event
	cancel  context.CancelFunc
	workDir string
}

// NewBdActivitySource creates a new source that tails rl activity
func NewBdActivitySource(workDir string) (*BdActivitySource, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, "rl", "activity", "--follow")
	cmd.Dir = workDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	source := &BdActivitySource{
		cmd:     cmd,
		events:  make(chan Event, 100),
		cancel:  cancel,
		workDir: workDir,
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if event := parseBdActivityLine(line); event != nil {
				select {
				case source.events <- *event:
				default:
					// Drop event if channel full
				}
			}
		}
		close(source.events)
	}()

	return source, nil
}

// Events returns the event channel
func (s *BdActivitySource) Events() <-chan Event {
	return s.events
}

// Close stops the source
func (s *BdActivitySource) Close() error {
	s.cancel()
	return s.cmd.Wait()
}

// rl activity line pattern: [HH:MM:SS] SYMBOL BEAD_ID action · description
var bdActivityPattern = regexp.MustCompile(`^\[(\d{2}:\d{2}:\d{2})\]\s+([+→✓✗⊘📌])\s+(\S+)?\s*(\w+)?\s*·?\s*(.*)$`)

// parseBdActivityLine parses a line from rl activity output
func parseBdActivityLine(line string) *Event {
	matches := bdActivityPattern.FindStringSubmatch(line)
	if matches == nil {
		// Try simpler pattern
		return parseSimpleLine(line)
	}

	timeStr := matches[1]
	symbol := matches[2]
	beadID := matches[3]
	action := matches[4]
	message := matches[5]

	// Parse time (assume today)
	now := time.Now()
	t, err := time.Parse("15:04:05", timeStr)
	if err != nil {
		t = now
	} else {
		t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, now.Location())
	}

	// Map symbol to event type
	eventType := "update"
	switch symbol {
	case "+":
		eventType = "create"
	case "→":
		eventType = "update"
	case "✓":
		eventType = "complete"
	case "✗":
		eventType = "fail"
	case "⊘":
		eventType = "delete"
	case "📌":
		eventType = "pin"
	}

	// Try to extract actor and warband from bead ID
	actor, warband, role := parseBeadContext(beadID)

	return &Event{
		Time:    t,
		Type:    eventType,
		Actor:   actor,
		Target:  beadID,
		Message: strings.TrimSpace(action + " " + message),
		Warband:     warband,
		Role:    role,
		Raw:     line,
	}
}

// parseSimpleLine handles lines that don't match the full pattern
func parseSimpleLine(line string) *Event {
	if strings.TrimSpace(line) == "" {
		return nil
	}

	// Try to extract timestamp
	var t time.Time
	if len(line) > 10 && line[0] == '[' {
		if idx := strings.Index(line, "]"); idx > 0 {
			timeStr := line[1:idx]
			now := time.Now()
			if parsed, err := time.Parse("15:04:05", timeStr); err == nil {
				t = time.Date(now.Year(), now.Month(), now.Day(),
					parsed.Hour(), parsed.Minute(), parsed.Second(), 0, now.Location())
			}
		}
	}

	if t.IsZero() {
		t = time.Now()
	}

	return &Event{
		Time:    t,
		Type:    "update",
		Message: line,
		Raw:     line,
	}
}

// parseBeadContext extracts actor/warband/role from a bead ID
// Uses canonical naming: prefix-warband-role-name
// Examples: gt-horde-clan-joe, gt-horde-witness, gt-warchief
func parseBeadContext(beadID string) (actor, warband, role string) {
	if beadID == "" {
		return
	}

	// Use the canonical parser
	parsedRig, parsedRole, name, ok := relics.ParseAgentBeadID(beadID)
	if !ok {
		return
	}

	warband = parsedRig
	role = parsedRole

	// Build actor identifier
	switch parsedRole {
	case "warchief", "shaman":
		actor = parsedRole
	case "witness", "forge":
		actor = parsedRole
	case "clan":
		if name != "" {
			actor = parsedRig + "/clan/" + name
		} else {
			actor = parsedRole
		}
	case "raider":
		if name != "" {
			actor = parsedRig + "/" + name
		} else {
			actor = parsedRole
		}
	}

	return
}

// GtEventsSource reads events from ~/horde/.events.jsonl (gt activity log)
type GtEventsSource struct {
	file   *os.File
	events chan Event
	cancel context.CancelFunc
}

// GtEvent is the structure of events in .events.jsonl
type GtEvent struct {
	Timestamp  string                 `json:"ts"`
	Source     string                 `json:"source"`
	Type       string                 `json:"type"`
	Actor      string                 `json:"actor"`
	Payload    map[string]interface{} `json:"payload"`
	Visibility string                 `json:"visibility"`
}

// NewGtEventsSource creates a source that tails ~/horde/.events.jsonl
func NewGtEventsSource(townRoot string) (*GtEventsSource, error) {
	eventsPath := filepath.Join(townRoot, ".events.jsonl")
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	source := &GtEventsSource{
		file:   file,
		events: make(chan Event, 100),
		cancel: cancel,
	}

	go source.tail(ctx)

	return source, nil
}

// tail follows the file and sends events
func (s *GtEventsSource) tail(ctx context.Context) {
	defer close(s.events)

	// Seek to end for live tailing
	_, _ = s.file.Seek(0, 2)

	scanner := bufio.NewScanner(s.file)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for scanner.Scan() {
				line := scanner.Text()
				if event := parseGtEventLine(line); event != nil {
					select {
					case s.events <- *event:
					default:
					}
				}
			}
		}
	}
}

// Events returns the event channel
func (s *GtEventsSource) Events() <-chan Event {
	return s.events
}

// Close stops the source
func (s *GtEventsSource) Close() error {
	s.cancel()
	return s.file.Close()
}

// parseGtEventLine parses a line from .events.jsonl
func parseGtEventLine(line string) *Event {
	if strings.TrimSpace(line) == "" {
		return nil
	}

	var ge GtEvent
	if err := json.Unmarshal([]byte(line), &ge); err != nil {
		return nil
	}

	// Only show feed-visible events
	if ge.Visibility != "feed" && ge.Visibility != "both" {
		return nil
	}

	t, err := time.Parse(time.RFC3339, ge.Timestamp)
	if err != nil {
		t = time.Now()
	}

	// Extract warband from payload or actor
	warband := ""
	if ge.Payload != nil {
		if r, ok := ge.Payload["warband"].(string); ok {
			warband = r
		}
	}
	if warband == "" && ge.Actor != "" {
		// Extract warband from actor like "horde/witness"
		parts := strings.Split(ge.Actor, "/")
		if len(parts) > 0 && parts[0] != "warchief" && parts[0] != "shaman" {
			warband = parts[0]
		}
	}

	// Extract role from actor
	role := ""
	if ge.Actor != "" {
		parts := strings.Split(ge.Actor, "/")
		if len(parts) >= 2 {
			role = parts[len(parts)-1]
			// Check for known roles
			switch parts[len(parts)-1] {
			case "witness", "forge":
				role = parts[len(parts)-1]
			default:
				// Could be raider name - check second-to-last part
				if len(parts) >= 2 {
					switch parts[len(parts)-2] {
					case "raiders":
						role = "raider"
					case "clan":
						role = "clan"
					}
				}
			}
		} else if len(parts) == 1 {
			role = parts[0]
		}
	}

	// Build message from event type and payload
	message := buildEventMessage(ge.Type, ge.Payload)

	return &Event{
		Time:    t,
		Type:    ge.Type,
		Actor:   ge.Actor,
		Target:  getPayloadString(ge.Payload, "bead"),
		Message: message,
		Warband:     warband,
		Role:    role,
		Raw:     line,
	}
}

// buildEventMessage creates a human-readable message from event type and payload
func buildEventMessage(eventType string, payload map[string]interface{}) string {
	switch eventType {
	case "patrol_started":
		count := getPayloadInt(payload, "raider_count")
		if msg := getPayloadString(payload, "message"); msg != "" {
			return msg
		}
		if count > 0 {
			return fmt.Sprintf("scout started (%d raiders)", count)
		}
		return "scout started"

	case "patrol_complete":
		count := getPayloadInt(payload, "raider_count")
		if msg := getPayloadString(payload, "message"); msg != "" {
			return msg
		}
		if count > 0 {
			return fmt.Sprintf("scout complete (%d raiders)", count)
		}
		return "scout complete"

	case "raider_checked":
		raider := getPayloadString(payload, "raider")
		status := getPayloadString(payload, "status")
		if raider != "" {
			if status != "" {
				return fmt.Sprintf("checked %s (%s)", raider, status)
			}
			return fmt.Sprintf("checked %s", raider)
		}
		return "raider checked"

	case "raider_nudged":
		raider := getPayloadString(payload, "raider")
		reason := getPayloadString(payload, "reason")
		if raider != "" {
			if reason != "" {
				return fmt.Sprintf("nudged %s: %s", raider, reason)
			}
			return fmt.Sprintf("nudged %s", raider)
		}
		return "raider nudged"

	case "escalation_sent":
		target := getPayloadString(payload, "target")
		to := getPayloadString(payload, "to")
		reason := getPayloadString(payload, "reason")
		if target != "" && to != "" {
			if reason != "" {
				return fmt.Sprintf("escalated %s to %s: %s", target, to, reason)
			}
			return fmt.Sprintf("escalated %s to %s", target, to)
		}
		return "escalation sent"

	case "charge":
		bead := getPayloadString(payload, "bead")
		target := getPayloadString(payload, "target")
		if bead != "" && target != "" {
			return fmt.Sprintf("charged %s to %s", bead, target)
		}
		return "work charged"

	case "banner":
		bead := getPayloadString(payload, "bead")
		if bead != "" {
			return fmt.Sprintf("bannered %s", bead)
		}
		return "bead bannered"

	case "handoff":
		subject := getPayloadString(payload, "subject")
		if subject != "" {
			return fmt.Sprintf("handoff: %s", subject)
		}
		return "session handoff"

	case "done":
		bead := getPayloadString(payload, "bead")
		if bead != "" {
			return fmt.Sprintf("done: %s", bead)
		}
		return "work done"

	case "drums":
		subject := getPayloadString(payload, "subject")
		to := getPayloadString(payload, "to")
		if subject != "" {
			if to != "" {
				return fmt.Sprintf("→ %s: %s", to, subject)
			}
			return subject
		}
		return "drums sent"

	case "merged":
		worker := getPayloadString(payload, "worker")
		if worker != "" {
			return fmt.Sprintf("merged work from %s", worker)
		}
		return "merged"

	case "merge_failed":
		reason := getPayloadString(payload, "reason")
		if reason != "" {
			return fmt.Sprintf("merge failed: %s", reason)
		}
		return "merge failed"

	default:
		if msg := getPayloadString(payload, "message"); msg != "" {
			return msg
		}
		return eventType
	}
}

// getPayloadString extracts a string from payload
func getPayloadString(payload map[string]interface{}, key string) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

// getPayloadInt extracts an int from payload
func getPayloadInt(payload map[string]interface{}, key string) int {
	if payload == nil {
		return 0
	}
	if v, ok := payload[key].(float64); ok {
		return int(v)
	}
	return 0
}

// CombinedSource merges events from multiple sources
type CombinedSource struct {
	sources []EventSource
	events  chan Event
	cancel  context.CancelFunc
}

// NewCombinedSource creates a source that merges multiple event sources
func NewCombinedSource(sources ...EventSource) *CombinedSource {
	ctx, cancel := context.WithCancel(context.Background())

	combined := &CombinedSource{
		sources: sources,
		events:  make(chan Event, 100),
		cancel:  cancel,
	}

	// Fan-in from all sources
	for _, src := range sources {
		go func(s EventSource) {
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-s.Events():
					if !ok {
						return
					}
					select {
					case combined.events <- event:
					default:
						// Drop if full
					}
				}
			}
		}(src)
	}

	return combined
}

// Events returns the combined event channel
func (c *CombinedSource) Events() <-chan Event {
	return c.events
}

// Close stops all sources
func (c *CombinedSource) Close() error {
	c.cancel()
	var lastErr error
	for _, src := range c.sources {
		if err := src.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// FindRelicsDir finds the relics directory for the given working directory
func FindRelicsDir(workDir string) (string, error) {
	// Walk up looking for .relics
	dir := workDir
	for {
		relicsPath := filepath.Join(dir, ".relics")
		if info, err := os.Stat(relicsPath); err == nil && info.IsDir() {
			return relicsPath, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", os.ErrNotExist
}
