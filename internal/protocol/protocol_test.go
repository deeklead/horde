package protocol

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/deeklead/horde/internal/drums"
)

func TestParseMessageType(t *testing.T) {
	tests := []struct {
		subject  string
		expected MessageType
	}{
		{"MERGE_READY nux", TypeMergeReady},
		{"MERGED Toast", TypeMerged},
		{"MERGE_FAILED ace", TypeMergeFailed},
		{"REWORK_REQUEST valkyrie", TypeReworkRequest},
		{"MERGE_READY", TypeMergeReady}, // no raider name
		{"Unknown subject", ""},
		{"", ""},
		{"  MERGE_READY nux  ", TypeMergeReady}, // with whitespace
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			result := ParseMessageType(tt.subject)
			if result != tt.expected {
				t.Errorf("ParseMessageType(%q) = %q, want %q", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestExtractRaider(t *testing.T) {
	tests := []struct {
		subject  string
		expected string
	}{
		{"MERGE_READY nux", "nux"},
		{"MERGED Toast", "Toast"},
		{"MERGE_FAILED ace", "ace"},
		{"REWORK_REQUEST valkyrie", "valkyrie"},
		{"MERGE_READY", ""},
		{"", ""},
		{"  MERGE_READY nux  ", "nux"},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			result := ExtractRaider(tt.subject)
			if result != tt.expected {
				t.Errorf("ExtractRaider(%q) = %q, want %q", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestIsProtocolMessage(t *testing.T) {
	tests := []struct {
		subject  string
		expected bool
	}{
		{"MERGE_READY nux", true},
		{"MERGED Toast", true},
		{"MERGE_FAILED ace", true},
		{"REWORK_REQUEST valkyrie", true},
		{"Unknown subject", false},
		{"", false},
		{"Hello world", false},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			result := IsProtocolMessage(tt.subject)
			if result != tt.expected {
				t.Errorf("IsProtocolMessage(%q) = %v, want %v", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestNewMergeReadyMessage(t *testing.T) {
	msg := NewMergeReadyMessage("horde", "nux", "raider/nux/gt-abc", "hd-abc")

	if msg.Subject != "MERGE_READY nux" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "MERGE_READY nux")
	}
	if msg.From != "horde/witness" {
		t.Errorf("From = %q, want %q", msg.From, "horde/witness")
	}
	if msg.To != "horde/forge" {
		t.Errorf("To = %q, want %q", msg.To, "horde/forge")
	}
	if msg.Priority != drums.PriorityHigh {
		t.Errorf("Priority = %q, want %q", msg.Priority, drums.PriorityHigh)
	}
	if !strings.Contains(msg.Body, "Branch: raider/nux/gt-abc") {
		t.Errorf("Body missing branch: %s", msg.Body)
	}
	if !strings.Contains(msg.Body, "Issue: gt-abc") {
		t.Errorf("Body missing issue: %s", msg.Body)
	}
}

func TestNewMergedMessage(t *testing.T) {
	msg := NewMergedMessage("horde", "nux", "raider/nux/gt-abc", "hd-abc", "main", "abc123")

	if msg.Subject != "MERGED nux" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "MERGED nux")
	}
	if msg.From != "horde/forge" {
		t.Errorf("From = %q, want %q", msg.From, "horde/forge")
	}
	if msg.To != "horde/witness" {
		t.Errorf("To = %q, want %q", msg.To, "horde/witness")
	}
	if !strings.Contains(msg.Body, "Merge-Commit: abc123") {
		t.Errorf("Body missing merge commit: %s", msg.Body)
	}
}

func TestNewMergeFailedMessage(t *testing.T) {
	msg := NewMergeFailedMessage("horde", "nux", "raider/nux/gt-abc", "hd-abc", "main", "tests", "Test failed")

	if msg.Subject != "MERGE_FAILED nux" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "MERGE_FAILED nux")
	}
	if !strings.Contains(msg.Body, "Failure-Type: tests") {
		t.Errorf("Body missing failure type: %s", msg.Body)
	}
	if !strings.Contains(msg.Body, "Error: Test failed") {
		t.Errorf("Body missing error: %s", msg.Body)
	}
}

func TestNewReworkRequestMessage(t *testing.T) {
	conflicts := []string{"file1.go", "file2.go"}
	msg := NewReworkRequestMessage("horde", "nux", "raider/nux/gt-abc", "hd-abc", "main", conflicts)

	if msg.Subject != "REWORK_REQUEST nux" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "REWORK_REQUEST nux")
	}
	if !strings.Contains(msg.Body, "Conflict-Files: file1.go, file2.go") {
		t.Errorf("Body missing conflict files: %s", msg.Body)
	}
	if !strings.Contains(msg.Body, "git rebase origin/main") {
		t.Errorf("Body missing rebase instructions: %s", msg.Body)
	}
}

func TestParseMergeReadyPayload(t *testing.T) {
	body := `Branch: raider/nux/gt-abc
Issue: gt-abc
Raider: nux
Warband: horde
Verified: clean git state`

	payload := ParseMergeReadyPayload(body)

	if payload.Branch != "raider/nux/gt-abc" {
		t.Errorf("Branch = %q, want %q", payload.Branch, "raider/nux/gt-abc")
	}
	if payload.Issue != "hd-abc" {
		t.Errorf("Issue = %q, want %q", payload.Issue, "hd-abc")
	}
	if payload.Raider != "nux" {
		t.Errorf("Raider = %q, want %q", payload.Raider, "nux")
	}
	if payload.Warband != "horde" {
		t.Errorf("Warband = %q, want %q", payload.Warband, "horde")
	}
}

func TestParseMergedPayload(t *testing.T) {
	ts := time.Now().Format(time.RFC3339)
	body := `Branch: raider/nux/gt-abc
Issue: gt-abc
Raider: nux
Warband: horde
Target: main
Merged-At: ` + ts + `
Merge-Commit: abc123`

	payload := ParseMergedPayload(body)

	if payload.Branch != "raider/nux/gt-abc" {
		t.Errorf("Branch = %q, want %q", payload.Branch, "raider/nux/gt-abc")
	}
	if payload.MergeCommit != "abc123" {
		t.Errorf("MergeCommit = %q, want %q", payload.MergeCommit, "abc123")
	}
	if payload.TargetBranch != "main" {
		t.Errorf("TargetBranch = %q, want %q", payload.TargetBranch, "main")
	}
}

func TestHandlerRegistry(t *testing.T) {
	registry := NewHandlerRegistry()

	handled := false
	registry.Register(TypeMergeReady, func(msg *drums.Message) error {
		handled = true
		return nil
	})

	msg := &drums.Message{Subject: "MERGE_READY nux"}

	if !registry.CanHandle(msg) {
		t.Error("Registry should be able to handle MERGE_READY message")
	}

	if err := registry.Handle(msg); err != nil {
		t.Errorf("Handle returned error: %v", err)
	}

	if !handled {
		t.Error("Handler was not called")
	}

	// Test unregistered message type
	unknownMsg := &drums.Message{Subject: "UNKNOWN message"}
	if registry.CanHandle(unknownMsg) {
		t.Error("Registry should not handle unknown message type")
	}
}

func TestWrapWitnessHandlers(t *testing.T) {
	handler := &mockWitnessHandler{}
	registry := WrapWitnessHandlers(handler)

	// Test MERGED
	mergedMsg := &drums.Message{
		Subject: "MERGED nux",
		Body:    "Branch: raider/nux\nIssue: gt-abc\nRaider: nux\nRig: horde\nTarget: main",
	}
	if err := registry.Handle(mergedMsg); err != nil {
		t.Errorf("HandleMerged error: %v", err)
	}
	if !handler.mergedCalled {
		t.Error("HandleMerged was not called")
	}

	// Test MERGE_FAILED
	failedMsg := &drums.Message{
		Subject: "MERGE_FAILED nux",
		Body:    "Branch: raider/nux\nIssue: gt-abc\nRaider: nux\nRig: horde\nTarget: main\nFailure-Type: tests\nError: failed",
	}
	if err := registry.Handle(failedMsg); err != nil {
		t.Errorf("HandleMergeFailed error: %v", err)
	}
	if !handler.failedCalled {
		t.Error("HandleMergeFailed was not called")
	}

	// Test REWORK_REQUEST
	reworkMsg := &drums.Message{
		Subject: "REWORK_REQUEST nux",
		Body:    "Branch: raider/nux\nIssue: gt-abc\nRaider: nux\nRig: horde\nTarget: main",
	}
	if err := registry.Handle(reworkMsg); err != nil {
		t.Errorf("HandleReworkRequest error: %v", err)
	}
	if !handler.reworkCalled {
		t.Error("HandleReworkRequest was not called")
	}
}

func TestWrapForgeHandlers(t *testing.T) {
	handler := &mockForgeHandler{}
	registry := WrapForgeHandlers(handler)

	msg := &drums.Message{
		Subject: "MERGE_READY nux",
		Body:    "Branch: raider/nux\nIssue: gt-abc\nRaider: nux\nRig: horde",
	}

	if err := registry.Handle(msg); err != nil {
		t.Errorf("HandleMergeReady error: %v", err)
	}
	if !handler.readyCalled {
		t.Error("HandleMergeReady was not called")
	}
}

func TestDefaultWitnessHandler(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewWitnessHandler("horde", tmpDir)

	// Capture output
	var buf bytes.Buffer
	handler.SetOutput(&buf)

	// Test HandleMerged
	mergedPayload := &MergedPayload{
		Branch:       "raider/nux/gt-abc",
		Issue:        "hd-abc",
		Raider:      "nux",
		Warband:          "horde",
		TargetBranch: "main",
		MergeCommit:  "abc123",
	}
	if err := handler.HandleMerged(mergedPayload); err != nil {
		t.Errorf("HandleMerged error: %v", err)
	}
	if !strings.Contains(buf.String(), "MERGED received") {
		t.Errorf("Output missing expected text: %s", buf.String())
	}

	// Test HandleMergeFailed
	buf.Reset()
	failedPayload := &MergeFailedPayload{
		Branch:       "raider/nux/gt-abc",
		Issue:        "hd-abc",
		Raider:      "nux",
		Warband:          "horde",
		TargetBranch: "main",
		FailureType:  "tests",
		Error:        "Test failed",
	}
	if err := handler.HandleMergeFailed(failedPayload); err != nil {
		t.Errorf("HandleMergeFailed error: %v", err)
	}
	if !strings.Contains(buf.String(), "MERGE_FAILED received") {
		t.Errorf("Output missing expected text: %s", buf.String())
	}

	// Test HandleReworkRequest
	buf.Reset()
	reworkPayload := &ReworkRequestPayload{
		Branch:        "raider/nux/gt-abc",
		Issue:         "hd-abc",
		Raider:       "nux",
		Warband:           "horde",
		TargetBranch:  "main",
		ConflictFiles: []string{"file1.go"},
	}
	if err := handler.HandleReworkRequest(reworkPayload); err != nil {
		t.Errorf("HandleReworkRequest error: %v", err)
	}
	if !strings.Contains(buf.String(), "REWORK_REQUEST received") {
		t.Errorf("Output missing expected text: %s", buf.String())
	}
}

// Mock handlers for testing

type mockWitnessHandler struct {
	mergedCalled bool
	failedCalled bool
	reworkCalled bool
}

func (m *mockWitnessHandler) HandleMerged(payload *MergedPayload) error {
	m.mergedCalled = true
	return nil
}

func (m *mockWitnessHandler) HandleMergeFailed(payload *MergeFailedPayload) error {
	m.failedCalled = true
	return nil
}

func (m *mockWitnessHandler) HandleReworkRequest(payload *ReworkRequestPayload) error {
	m.reworkCalled = true
	return nil
}

type mockForgeHandler struct {
	readyCalled bool
}

func (m *mockForgeHandler) HandleMergeReady(payload *MergeReadyPayload) error {
	m.readyCalled = true
	return nil
}
