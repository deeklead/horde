package daemon

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// testDaemon creates a minimal Daemon for testing.
func testDaemon() *Daemon {
	return &Daemon{
		config: &Config{TownRoot: "/tmp/test"},
		logger: log.New(io.Discard, "", 0), // silent logger for tests
	}
}

// testDaemonWithTown creates a Daemon with a proper encampment setup for testing.
// Returns the daemon and a cleanup function.
func testDaemonWithTown(t *testing.T, townName string) (*Daemon, func()) {
	t.Helper()
	townRoot := t.TempDir()

	// Create warchief directory and encampment.json
	warchiefDir := filepath.Join(townRoot, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("failed to create warchief dir: %v", err)
	}
	townJSON := filepath.Join(warchiefDir, "encampment.json")
	content := `{"name": "` + townName + `"}`
	if err := os.WriteFile(townJSON, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write encampment.json: %v", err)
	}

	d := &Daemon{
		config: &Config{TownRoot: townRoot},
		logger: log.New(io.Discard, "", 0),
	}

	return d, func() {
		// Cleanup handled by t.TempDir()
	}
}

func TestParseLifecycleRequest_Cycle(t *testing.T) {
	d := testDaemon()

	tests := []struct {
		subject  string
		body     string
		expected LifecycleAction
	}{
		// JSON body format
		{"LIFECYCLE: requesting action", `{"action": "cycle"}`, ActionCycle},
		// Simple text body format
		{"LIFECYCLE: requesting action", "cycle", ActionCycle},
		{"lifecycle: action request", "action: cycle", ActionCycle},
	}

	for _, tc := range tests {
		msg := &RelicsMessage{
			Subject: tc.subject,
			Body:    tc.body,
			From:    "test-sender",
		}
		result := d.parseLifecycleRequest(msg)
		if result == nil {
			t.Errorf("parseLifecycleRequest(subject=%q, body=%q) returned nil, expected action %s", tc.subject, tc.body, tc.expected)
			continue
		}
		if result.Action != tc.expected {
			t.Errorf("parseLifecycleRequest(subject=%q, body=%q) action = %s, expected %s", tc.subject, tc.body, result.Action, tc.expected)
		}
	}
}

func TestParseLifecycleRequest_RestartAndShutdown(t *testing.T) {
	// Verify that restart and shutdown are correctly parsed using structured body.
	d := testDaemon()

	tests := []struct {
		subject  string
		body     string
		expected LifecycleAction
	}{
		{"LIFECYCLE: action", `{"action": "restart"}`, ActionRestart},
		{"LIFECYCLE: action", `{"action": "shutdown"}`, ActionShutdown},
		{"lifecycle: action", "stop", ActionShutdown},
		{"LIFECYCLE: action", "restart", ActionRestart},
	}

	for _, tc := range tests {
		msg := &RelicsMessage{
			Subject: tc.subject,
			Body:    tc.body,
			From:    "test-sender",
		}
		result := d.parseLifecycleRequest(msg)
		if result == nil {
			t.Errorf("parseLifecycleRequest(subject=%q, body=%q) returned nil", tc.subject, tc.body)
			continue
		}
		if result.Action != tc.expected {
			t.Errorf("parseLifecycleRequest(subject=%q, body=%q) action = %s, expected %s", tc.subject, tc.body, result.Action, tc.expected)
		}
	}
}

func TestParseLifecycleRequest_NotLifecycle(t *testing.T) {
	d := testDaemon()

	tests := []string{
		"Regular message",
		"HEARTBEAT: check warbands",
		"lifecycle without colon",
		"Something else: requesting cycle",
		"",
	}

	for _, title := range tests {
		msg := &RelicsMessage{
			Subject: title,
			From:    "test-sender",
		}
		result := d.parseLifecycleRequest(msg)
		if result != nil {
			t.Errorf("parseLifecycleRequest(%q) = %+v, expected nil", title, result)
		}
	}
}

func TestParseLifecycleRequest_UsesFromField(t *testing.T) {
	d := testDaemon()

	// Now that we use structured body, the From field comes directly from the message
	tests := []struct {
		subject      string
		body         string
		sender       string
		expectedFrom string
	}{
		{"LIFECYCLE: action", `{"action": "cycle"}`, "warchief", "warchief"},
		{"LIFECYCLE: action", "restart", "horde-witness", "horde-witness"},
		{"lifecycle: action", "shutdown", "my-warband-forge", "my-warband-forge"},
	}

	for _, tc := range tests {
		msg := &RelicsMessage{
			Subject: tc.subject,
			Body:    tc.body,
			From:    tc.sender,
		}
		result := d.parseLifecycleRequest(msg)
		if result == nil {
			t.Errorf("parseLifecycleRequest(body=%q) returned nil", tc.body)
			continue
		}
		if result.From != tc.expectedFrom {
			t.Errorf("parseLifecycleRequest() from = %q, expected %q", result.From, tc.expectedFrom)
		}
	}
}

func TestParseLifecycleRequest_AlwaysUsesFromField(t *testing.T) {
	d := testDaemon()

	// With structured body parsing, From always comes from message From field
	msg := &RelicsMessage{
		Subject: "LIFECYCLE: action",
		Body:    "cycle",
		From:    "the-sender",
	}
	result := d.parseLifecycleRequest(msg)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.From != "the-sender" {
		t.Errorf("parseLifecycleRequest() from = %q, expected 'the-sender'", result.From)
	}
}

func TestIdentityToSession_Warchief(t *testing.T) {
	d, cleanup := testDaemonWithTown(t, "ai")
	defer cleanup()

	// Warchief session name is now fixed (one per machine, uses hq- prefix)
	result := d.identityToSession("warchief")
	if result != "hq-warchief" {
		t.Errorf("identityToSession('warchief') = %q, expected 'hq-warchief'", result)
	}
}

func TestIdentityToSession_Witness(t *testing.T) {
	d := testDaemon()

	tests := []struct {
		identity string
		expected string
	}{
		{"horde-witness", "gt-horde-witness"},
		{"myrig-witness", "gt-myrig-witness"},
		{"my-warband-name-witness", "gt-my-warband-name-witness"},
	}

	for _, tc := range tests {
		result := d.identityToSession(tc.identity)
		if result != tc.expected {
			t.Errorf("identityToSession(%q) = %q, expected %q", tc.identity, result, tc.expected)
		}
	}
}

func TestIdentityToSession_Unknown(t *testing.T) {
	d := testDaemon()

	tests := []string{
		"unknown",
		"raider",
		"forge",
		"horde", // warband name without -witness
		"",
	}

	for _, identity := range tests {
		result := d.identityToSession(identity)
		if result != "" {
			t.Errorf("identityToSession(%q) = %q, expected empty string", identity, result)
		}
	}
}

func TestRelicsMessage_Serialization(t *testing.T) {
	msg := RelicsMessage{
		ID:       "msg-123",
		Subject:  "Test Message",
		Body:     "A test message body",
		From:     "test-sender",
		To:       "test-recipient",
		Priority: "high",
		Type:     "message",
	}

	// Verify all fields are accessible
	if msg.ID != "msg-123" {
		t.Errorf("ID mismatch")
	}
	if msg.Subject != "Test Message" {
		t.Errorf("Subject mismatch")
	}
	if msg.From != "test-sender" {
		t.Errorf("From mismatch")
	}
}
