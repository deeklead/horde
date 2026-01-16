package encampmentlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatLogLine(t *testing.T) {
	ts := time.Date(2025, 12, 26, 15, 30, 45, 0, time.UTC)

	tests := []struct {
		name     string
		event    Event
		contains []string
	}{
		{
			name: "muster event",
			event: Event{
				Timestamp: ts,
				Type:      EventSpawn,
				Agent:     "horde/clan/max",
				Context:   "hd-xyz",
			},
			contains: []string{"2025-12-26 15:30:45", "[muster]", "horde/clan/max", "spawned for gt-xyz"},
		},
		{
			name: "signal event",
			event: Event{
				Timestamp: ts,
				Type:      EventNudge,
				Agent:     "horde/clan/max",
				Context:   "start work",
			},
			contains: []string{"[signal]", "horde/clan/max", "nudged with"},
		},
		{
			name: "done event",
			event: Event{
				Timestamp: ts,
				Type:      EventDone,
				Agent:     "horde/clan/max",
				Context:   "hd-abc",
			},
			contains: []string{"[done]", "completed gt-abc"},
		},
		{
			name: "crash event",
			event: Event{
				Timestamp: ts,
				Type:      EventCrash,
				Agent:     "horde/raiders/Toast",
				Context:   "signal 9",
			},
			contains: []string{"[crash]", "exited unexpectedly", "signal 9"},
		},
		{
			name: "kill event",
			event: Event{
				Timestamp: ts,
				Type:      EventKill,
				Agent:     "horde/raiders/Toast",
				Context:   "hd stop",
			},
			contains: []string{"[kill]", "killed", "hd stop"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := formatLogLine(tt.event)
			for _, want := range tt.contains {
				if !strings.Contains(line, want) {
					t.Errorf("formatLogLine() = %q, want it to contain %q", line, want)
				}
			}
		})
	}
}

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantErr bool
		check   func(Event) bool
	}{
		{
			name: "valid muster line",
			line: "2025-12-26 15:30:45 [muster] horde/clan/max spawned for gt-xyz",
			check: func(e Event) bool {
				return e.Type == EventSpawn && e.Agent == "horde/clan/max"
			},
		},
		{
			name: "valid signal line",
			line: "2025-12-26 15:31:02 [signal] horde/clan/max nudged with \"start\"",
			check: func(e Event) bool {
				return e.Type == EventNudge && e.Agent == "horde/clan/max"
			},
		},
		{
			name:    "too short",
			line:    "short",
			wantErr: true,
		},
		{
			name:    "missing bracket",
			line:    "2025-12-26 15:30:45 muster horde/clan/max",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := parseLogLine(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseLogLine() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("parseLogLine() unexpected error: %v", err)
				return
			}
			if tt.check != nil && !tt.check(event) {
				t.Errorf("parseLogLine() check failed for event: %+v", event)
			}
		})
	}
}

func TestLoggerLogEvent(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "encampmentlog-test")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := NewLogger(tmpDir)

	// Log an event
	err = logger.Log(EventSpawn, "horde/clan/max", "hd-xyz")
	if err != nil {
		t.Fatalf("Log() error: %v", err)
	}

	// Verify log file was created
	logPath := filepath.Join(tmpDir, "logs", "encampment.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}

	if !strings.Contains(string(content), "[muster]") {
		t.Errorf("log file should contain [muster], got: %s", content)
	}
	if !strings.Contains(string(content), "horde/clan/max") {
		t.Errorf("log file should contain agent name, got: %s", content)
	}
}

func TestFilterEvents(t *testing.T) {
	now := time.Now()
	events := []Event{
		{Timestamp: now.Add(-2 * time.Hour), Type: EventSpawn, Agent: "horde/clan/max", Context: "hd-1"},
		{Timestamp: now.Add(-1 * time.Hour), Type: EventNudge, Agent: "horde/clan/max", Context: "hi"},
		{Timestamp: now.Add(-30 * time.Minute), Type: EventDone, Agent: "horde/raiders/Toast", Context: "hd-2"},
		{Timestamp: now.Add(-10 * time.Minute), Type: EventSpawn, Agent: "wyvern/clan/joe", Context: "hd-3"},
	}

	tests := []struct {
		name      string
		filter    Filter
		wantCount int
	}{
		{
			name:      "no filter",
			filter:    Filter{},
			wantCount: 4,
		},
		{
			name:      "filter by type",
			filter:    Filter{Type: EventSpawn},
			wantCount: 2,
		},
		{
			name:      "filter by agent prefix",
			filter:    Filter{Agent: "horde/"},
			wantCount: 3,
		},
		{
			name:      "filter by time",
			filter:    Filter{Since: now.Add(-45 * time.Minute)},
			wantCount: 2,
		},
		{
			name:      "combined filters",
			filter:    Filter{Type: EventSpawn, Agent: "horde/"},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterEvents(events, tt.filter)
			if len(result) != tt.wantCount {
				t.Errorf("FilterEvents() got %d events, want %d", len(result), tt.wantCount)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10c", 10, "exactly10c"},
		{"this is a longer string", 10, "this is..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
