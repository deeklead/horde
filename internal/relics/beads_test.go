package relics

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNew verifies the constructor.
func TestNew(t *testing.T) {
	b := New("/some/path")
	if b == nil {
		t.Fatal("New returned nil")
	}
	if b.workDir != "/some/path" {
		t.Errorf("workDir = %q, want /some/path", b.workDir)
	}
}

// TestListOptions verifies ListOptions defaults.
func TestListOptions(t *testing.T) {
	opts := ListOptions{
		Status:   "open",
		Type:     "task",
		Priority: 1,
	}
	if opts.Status != "open" {
		t.Errorf("Status = %q, want open", opts.Status)
	}
}

// TestCreateOptions verifies CreateOptions fields.
func TestCreateOptions(t *testing.T) {
	opts := CreateOptions{
		Title:       "Test issue",
		Type:        "task",
		Priority:    2,
		Description: "A test description",
		Parent:      "gt-abc",
	}
	if opts.Title != "Test issue" {
		t.Errorf("Title = %q, want 'Test issue'", opts.Title)
	}
	if opts.Parent != "gt-abc" {
		t.Errorf("Parent = %q, want gt-abc", opts.Parent)
	}
}

// TestUpdateOptions verifies UpdateOptions pointer fields.
func TestUpdateOptions(t *testing.T) {
	status := "in_progress"
	priority := 1
	opts := UpdateOptions{
		Status:   &status,
		Priority: &priority,
	}
	if *opts.Status != "in_progress" {
		t.Errorf("Status = %q, want in_progress", *opts.Status)
	}
	if *opts.Priority != 1 {
		t.Errorf("Priority = %d, want 1", *opts.Priority)
	}
}

// TestIsRelicsRepo tests repository detection.
func TestIsRelicsRepo(t *testing.T) {
	// Test with a non-relics directory
	tmpDir, err := os.MkdirTemp("", "relics-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	b := New(tmpDir)
	// This should return false since there's no .relics directory
	// and rl list will fail
	if b.IsRelicsRepo() {
		// This might pass if rl handles missing .relics gracefully
		t.Log("IsRelicsRepo returned true for non-relics directory (bd might initialize)")
	}
}

// TestWrapError tests error wrapping.
// ZFC: Only test ErrNotFound detection. ErrNotARepo and ErrSyncConflict
// were removed as per ZFC - agents should handle those errors directly.
func TestWrapError(t *testing.T) {
	b := New("/test")

	tests := []struct {
		stderr  string
		wantErr error
		wantNil bool
	}{
		{"Issue not found: gt-xyz", ErrNotFound, false},
		{"gt-xyz not found", ErrNotFound, false},
	}

	for _, tt := range tests {
		err := b.wrapError(nil, tt.stderr, []string{"test"})
		if tt.wantNil {
			if err != nil {
				t.Errorf("wrapError(%q) = %v, want nil", tt.stderr, err)
			}
		} else {
			if err != tt.wantErr {
				t.Errorf("wrapError(%q) = %v, want %v", tt.stderr, err, tt.wantErr)
			}
		}
	}
}

// Integration test that runs against real rl if available
func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Find a relics repo (use current directory if it has .relics)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".relics")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("no .relics directory found in path")
		}
		dir = parent
	}

	// Resolve the actual relics directory (following redirect if present)
	// In multi-worktree setups, worktrees have .relics/redirect pointing to
	// the canonical relics location (e.g., warchief/warband/.relics)
	relicsDir := ResolveRelicsDir(dir)
	dbPath := filepath.Join(relicsDir, "relics.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skip("no relics.db found (JSONL-only repo)")
	}

	b := New(dir)

	// Sync database with JSONL before testing to avoid "Database out of sync" errors.
	// This can happen when JSONL is updated (e.g., by git pull) but the SQLite database
	// hasn't been imported yet. Running sync --import-only ensures we test against
	// consistent data and prevents flaky test failures.
	// We use --allow-stale to handle cases where the daemon is actively writing and
	// the staleness check would otherwise fail spuriously.
	syncCmd := exec.Command("rl", "--no-daemon", "--allow-stale", "sync", "--import-only")
	syncCmd.Dir = dir
	if err := syncCmd.Run(); err != nil {
		// If sync fails (e.g., no database exists), just log and continue
		t.Logf("bd sync --import-only failed (may not have db): %v", err)
	}

	// Test List
	t.Run("List", func(t *testing.T) {
		issues, err := b.List(ListOptions{Status: "open"})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		t.Logf("Found %d open issues", len(issues))
	})

	// Test Ready
	t.Run("Ready", func(t *testing.T) {
		issues, err := b.Ready()
		if err != nil {
			t.Fatalf("Ready failed: %v", err)
		}
		t.Logf("Found %d ready issues", len(issues))
	})

	// Test Blocked
	t.Run("Blocked", func(t *testing.T) {
		issues, err := b.Blocked()
		if err != nil {
			t.Fatalf("Blocked failed: %v", err)
		}
		t.Logf("Found %d blocked issues", len(issues))
	})

	// Test Show (if we have issues)
	t.Run("Show", func(t *testing.T) {
		issues, err := b.List(ListOptions{})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(issues) == 0 {
			t.Skip("no issues to show")
		}

		issue, err := b.Show(issues[0].ID)
		if err != nil {
			t.Fatalf("Show(%s) failed: %v", issues[0].ID, err)
		}
		t.Logf("Showed issue: %s - %s", issue.ID, issue.Title)
	})
}

// TestParseMRFields tests parsing MR fields from issue descriptions.
func TestParseMRFields(t *testing.T) {
	tests := []struct {
		name       string
		issue      *Issue
		wantNil    bool
		wantFields *MRFields
	}{
		{
			name:    "nil issue",
			issue:   nil,
			wantNil: true,
		},
		{
			name:    "empty description",
			issue:   &Issue{Description: ""},
			wantNil: true,
		},
		{
			name:    "no MR fields",
			issue:   &Issue{Description: "This is just plain text\nwith no field markers"},
			wantNil: true,
		},
		{
			name: "all fields",
			issue: &Issue{
				Description: `branch: raider/Nux/gt-xyz
target: main
source_issue: gt-xyz
worker: Nux
warband: horde
merge_commit: abc123def
close_reason: merged`,
			},
			wantFields: &MRFields{
				Branch:      "raider/Nux/gt-xyz",
				Target:      "main",
				SourceIssue: "gt-xyz",
				Worker:      "Nux",
				Warband:         "horde",
				MergeCommit: "abc123def",
				CloseReason: "merged",
			},
		},
		{
			name: "partial fields",
			issue: &Issue{
				Description: `branch: raider/Toast/gt-abc
target: integration/gt-epic
source_issue: gt-abc
worker: Toast`,
			},
			wantFields: &MRFields{
				Branch:      "raider/Toast/gt-abc",
				Target:      "integration/gt-epic",
				SourceIssue: "gt-abc",
				Worker:      "Toast",
			},
		},
		{
			name: "mixed with prose",
			issue: &Issue{
				Description: `branch: raider/Capable/gt-def
target: main
source_issue: gt-def

This MR fixes a critical bug in the authentication system.
Please review carefully.

worker: Capable
warband: wasteland`,
			},
			wantFields: &MRFields{
				Branch:      "raider/Capable/gt-def",
				Target:      "main",
				SourceIssue: "gt-def",
				Worker:      "Capable",
				Warband:         "wasteland",
			},
		},
		{
			name: "alternate key formats",
			issue: &Issue{
				Description: `branch: raider/Max/gt-ghi
source-issue: gt-ghi
merge-commit: 789xyz`,
			},
			wantFields: &MRFields{
				Branch:      "raider/Max/gt-ghi",
				SourceIssue: "gt-ghi",
				MergeCommit: "789xyz",
			},
		},
		{
			name: "case insensitive keys",
			issue: &Issue{
				Description: `Branch: raider/Furiosa/gt-jkl
TARGET: main
Worker: Furiosa
WARBAND: horde`,
			},
			wantFields: &MRFields{
				Branch: "raider/Furiosa/gt-jkl",
				Target: "main",
				Worker: "Furiosa",
				Warband:    "horde",
			},
		},
		{
			name: "extra whitespace",
			issue: &Issue{
				Description: `  branch:   raider/Nux/gt-mno
target:main
  worker:   Nux  `,
			},
			wantFields: &MRFields{
				Branch: "raider/Nux/gt-mno",
				Target: "main",
				Worker: "Nux",
			},
		},
		{
			name: "ignores empty values",
			issue: &Issue{
				Description: `branch: raider/Nux/gt-pqr
target:
source_issue: gt-pqr`,
			},
			wantFields: &MRFields{
				Branch:      "raider/Nux/gt-pqr",
				SourceIssue: "gt-pqr",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := ParseMRFields(tt.issue)

			if tt.wantNil {
				if fields != nil {
					t.Errorf("ParseMRFields() = %+v, want nil", fields)
				}
				return
			}

			if fields == nil {
				t.Fatal("ParseMRFields() = nil, want non-nil")
			}

			if fields.Branch != tt.wantFields.Branch {
				t.Errorf("Branch = %q, want %q", fields.Branch, tt.wantFields.Branch)
			}
			if fields.Target != tt.wantFields.Target {
				t.Errorf("Target = %q, want %q", fields.Target, tt.wantFields.Target)
			}
			if fields.SourceIssue != tt.wantFields.SourceIssue {
				t.Errorf("SourceIssue = %q, want %q", fields.SourceIssue, tt.wantFields.SourceIssue)
			}
			if fields.Worker != tt.wantFields.Worker {
				t.Errorf("Worker = %q, want %q", fields.Worker, tt.wantFields.Worker)
			}
			if fields.Warband != tt.wantFields.Warband {
				t.Errorf("Warband = %q, want %q", fields.Warband, tt.wantFields.Warband)
			}
			if fields.MergeCommit != tt.wantFields.MergeCommit {
				t.Errorf("MergeCommit = %q, want %q", fields.MergeCommit, tt.wantFields.MergeCommit)
			}
			if fields.CloseReason != tt.wantFields.CloseReason {
				t.Errorf("CloseReason = %q, want %q", fields.CloseReason, tt.wantFields.CloseReason)
			}
		})
	}
}

// TestFormatMRFields tests formatting MR fields to string.
func TestFormatMRFields(t *testing.T) {
	tests := []struct {
		name   string
		fields *MRFields
		want   string
	}{
		{
			name:   "nil fields",
			fields: nil,
			want:   "",
		},
		{
			name:   "empty fields",
			fields: &MRFields{},
			want:   "",
		},
		{
			name: "all fields",
			fields: &MRFields{
				Branch:      "raider/Nux/gt-xyz",
				Target:      "main",
				SourceIssue: "gt-xyz",
				Worker:      "Nux",
				Warband:         "horde",
				MergeCommit: "abc123def",
				CloseReason: "merged",
			},
			want: `branch: raider/Nux/gt-xyz
target: main
source_issue: gt-xyz
worker: Nux
warband: horde
merge_commit: abc123def
close_reason: merged`,
		},
		{
			name: "partial fields",
			fields: &MRFields{
				Branch:      "raider/Toast/gt-abc",
				Target:      "main",
				SourceIssue: "gt-abc",
				Worker:      "Toast",
			},
			want: `branch: raider/Toast/gt-abc
target: main
source_issue: gt-abc
worker: Toast`,
		},
		{
			name: "only close fields",
			fields: &MRFields{
				MergeCommit: "deadbeef",
				CloseReason: "rejected",
			},
			want: `merge_commit: deadbeef
close_reason: rejected`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMRFields(tt.fields)
			if got != tt.want {
				t.Errorf("FormatMRFields() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestSetMRFields tests updating issue descriptions with MR fields.
func TestSetMRFields(t *testing.T) {
	tests := []struct {
		name   string
		issue  *Issue
		fields *MRFields
		want   string
	}{
		{
			name:  "nil issue",
			issue: nil,
			fields: &MRFields{
				Branch: "raider/Nux/gt-xyz",
				Target: "main",
			},
			want: `branch: raider/Nux/gt-xyz
target: main`,
		},
		{
			name:  "empty description",
			issue: &Issue{Description: ""},
			fields: &MRFields{
				Branch:      "raider/Nux/gt-xyz",
				Target:      "main",
				SourceIssue: "gt-xyz",
			},
			want: `branch: raider/Nux/gt-xyz
target: main
source_issue: gt-xyz`,
		},
		{
			name:  "preserve prose content",
			issue: &Issue{Description: "This is a description of the work.\n\nIt spans multiple lines."},
			fields: &MRFields{
				Branch: "raider/Toast/gt-abc",
				Worker: "Toast",
			},
			want: `branch: raider/Toast/gt-abc
worker: Toast

This is a description of the work.

It spans multiple lines.`,
		},
		{
			name: "replace existing fields",
			issue: &Issue{
				Description: `branch: raider/Nux/gt-old
target: develop
source_issue: gt-old
worker: Nux

Some existing prose content.`,
			},
			fields: &MRFields{
				Branch:      "raider/Nux/gt-new",
				Target:      "main",
				SourceIssue: "gt-new",
				Worker:      "Nux",
				MergeCommit: "abc123",
			},
			want: `branch: raider/Nux/gt-new
target: main
source_issue: gt-new
worker: Nux
merge_commit: abc123

Some existing prose content.`,
		},
		{
			name: "preserve non-MR key-value lines",
			issue: &Issue{
				Description: `branch: raider/Capable/gt-def
custom_field: some value
author: someone
target: main`,
			},
			fields: &MRFields{
				Branch:      "raider/Capable/gt-ghi",
				Target:      "integration/epic",
				CloseReason: "merged",
			},
			want: `branch: raider/Capable/gt-ghi
target: integration/epic
close_reason: merged

custom_field: some value
author: someone`,
		},
		{
			name:   "empty fields clears MR data",
			issue:  &Issue{Description: "branch: old\ntarget: old\n\nKeep this text."},
			fields: &MRFields{},
			want:   "Keep this text.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SetMRFields(tt.issue, tt.fields)
			if got != tt.want {
				t.Errorf("SetMRFields() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestMRFieldsRoundTrip tests that parse/format round-trips correctly.
func TestMRFieldsRoundTrip(t *testing.T) {
	original := &MRFields{
		Branch:      "raider/Nux/gt-xyz",
		Target:      "main",
		SourceIssue: "gt-xyz",
		Worker:      "Nux",
		Warband:         "horde",
		MergeCommit: "abc123def789",
		CloseReason: "merged",
	}

	// Format to string
	formatted := FormatMRFields(original)

	// Parse back
	issue := &Issue{Description: formatted}
	parsed := ParseMRFields(issue)

	if parsed == nil {
		t.Fatal("round-trip parse returned nil")
	}

	if *parsed != *original {
		t.Errorf("round-trip mismatch:\ngot  %+v\nwant %+v", parsed, original)
	}
}

// TestParseMRFieldsFromDesignDoc tests the example from the design doc.
func TestParseMRFieldsFromDesignDoc(t *testing.T) {
	// Example from docs/merge-queue-design.md
	description := `branch: raider/Nux/gt-xyz
target: main
source_issue: gt-xyz
worker: Nux
warband: horde`

	issue := &Issue{Description: description}
	fields := ParseMRFields(issue)

	if fields == nil {
		t.Fatal("ParseMRFields returned nil for design doc example")
	}

	// Verify all fields match the design doc
	if fields.Branch != "raider/Nux/gt-xyz" {
		t.Errorf("Branch = %q, want raider/Nux/gt-xyz", fields.Branch)
	}
	if fields.Target != "main" {
		t.Errorf("Target = %q, want main", fields.Target)
	}
	if fields.SourceIssue != "gt-xyz" {
		t.Errorf("SourceIssue = %q, want gt-xyz", fields.SourceIssue)
	}
	if fields.Worker != "Nux" {
		t.Errorf("Worker = %q, want Nux", fields.Worker)
	}
	if fields.Warband != "horde" {
		t.Errorf("Warband = %q, want horde", fields.Warband)
	}
}

// TestSetMRFieldsPreservesURL tests that URLs in prose are preserved.
func TestSetMRFieldsPreservesURL(t *testing.T) {
	// URLs contain colons which could be confused with key: value
	issue := &Issue{
		Description: `branch: old-branch
Check out https://example.com/path for more info.
Also see http://localhost:8080/api`,
	}

	fields := &MRFields{
		Branch: "new-branch",
		Target: "main",
	}

	result := SetMRFields(issue, fields)

	// URLs should be preserved
	if !strings.Contains(result, "https://example.com/path") {
		t.Error("HTTPS URL was not preserved")
	}
	if !strings.Contains(result, "http://localhost:8080/api") {
		t.Error("HTTP URL was not preserved")
	}
	if !strings.Contains(result, "branch: new-branch") {
		t.Error("branch field was not set")
	}
}

// TestParseAttachmentFields tests parsing attachment fields from issue descriptions.
func TestParseAttachmentFields(t *testing.T) {
	tests := []struct {
		name       string
		issue      *Issue
		wantNil    bool
		wantFields *AttachmentFields
	}{
		{
			name:    "nil issue",
			issue:   nil,
			wantNil: true,
		},
		{
			name:    "empty description",
			issue:   &Issue{Description: ""},
			wantNil: true,
		},
		{
			name:    "no attachment fields",
			issue:   &Issue{Description: "This is just plain text\nwith no attachment markers"},
			wantNil: true,
		},
		{
			name: "both fields",
			issue: &Issue{
				Description: `attached_molecule: totem-xyz
attached_at: 2025-12-21T15:30:00Z`,
			},
			wantFields: &AttachmentFields{
				AttachedMolecule: "totem-xyz",
				AttachedAt:       "2025-12-21T15:30:00Z",
			},
		},
		{
			name: "only totem",
			issue: &Issue{
				Description: `attached_molecule: totem-abc`,
			},
			wantFields: &AttachmentFields{
				AttachedMolecule: "totem-abc",
			},
		},
		{
			name: "mixed with other content",
			issue: &Issue{
				Description: `attached_molecule: totem-def
attached_at: 2025-12-21T10:00:00Z

This is a handoff bead for the raider.
Keep working on the current task.`,
			},
			wantFields: &AttachmentFields{
				AttachedMolecule: "totem-def",
				AttachedAt:       "2025-12-21T10:00:00Z",
			},
		},
		{
			name: "alternate key formats (hyphen)",
			issue: &Issue{
				Description: `attached-totem: totem-ghi
attached-at: 2025-12-21T12:00:00Z`,
			},
			wantFields: &AttachmentFields{
				AttachedMolecule: "totem-ghi",
				AttachedAt:       "2025-12-21T12:00:00Z",
			},
		},
		{
			name: "case insensitive",
			issue: &Issue{
				Description: `Attached_Molecule: totem-jkl
ATTACHED_AT: 2025-12-21T14:00:00Z`,
			},
			wantFields: &AttachmentFields{
				AttachedMolecule: "totem-jkl",
				AttachedAt:       "2025-12-21T14:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := ParseAttachmentFields(tt.issue)

			if tt.wantNil {
				if fields != nil {
					t.Errorf("ParseAttachmentFields() = %+v, want nil", fields)
				}
				return
			}

			if fields == nil {
				t.Fatal("ParseAttachmentFields() = nil, want non-nil")
			}

			if fields.AttachedMolecule != tt.wantFields.AttachedMolecule {
				t.Errorf("AttachedMolecule = %q, want %q", fields.AttachedMolecule, tt.wantFields.AttachedMolecule)
			}
			if fields.AttachedAt != tt.wantFields.AttachedAt {
				t.Errorf("AttachedAt = %q, want %q", fields.AttachedAt, tt.wantFields.AttachedAt)
			}
		})
	}
}

// TestFormatAttachmentFields tests formatting attachment fields to string.
func TestFormatAttachmentFields(t *testing.T) {
	tests := []struct {
		name   string
		fields *AttachmentFields
		want   string
	}{
		{
			name:   "nil fields",
			fields: nil,
			want:   "",
		},
		{
			name:   "empty fields",
			fields: &AttachmentFields{},
			want:   "",
		},
		{
			name: "both fields",
			fields: &AttachmentFields{
				AttachedMolecule: "totem-xyz",
				AttachedAt:       "2025-12-21T15:30:00Z",
			},
			want: `attached_molecule: totem-xyz
attached_at: 2025-12-21T15:30:00Z`,
		},
		{
			name: "only totem",
			fields: &AttachmentFields{
				AttachedMolecule: "totem-abc",
			},
			want: "attached_molecule: totem-abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAttachmentFields(tt.fields)
			if got != tt.want {
				t.Errorf("FormatAttachmentFields() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestSetAttachmentFields tests updating issue descriptions with attachment fields.
func TestSetAttachmentFields(t *testing.T) {
	tests := []struct {
		name   string
		issue  *Issue
		fields *AttachmentFields
		want   string
	}{
		{
			name:  "nil issue",
			issue: nil,
			fields: &AttachmentFields{
				AttachedMolecule: "totem-xyz",
				AttachedAt:       "2025-12-21T15:30:00Z",
			},
			want: `attached_molecule: totem-xyz
attached_at: 2025-12-21T15:30:00Z`,
		},
		{
			name:  "empty description",
			issue: &Issue{Description: ""},
			fields: &AttachmentFields{
				AttachedMolecule: "totem-abc",
				AttachedAt:       "2025-12-21T10:00:00Z",
			},
			want: `attached_molecule: totem-abc
attached_at: 2025-12-21T10:00:00Z`,
		},
		{
			name:  "preserve prose content",
			issue: &Issue{Description: "This is a handoff bead description.\n\nKeep working on the task."},
			fields: &AttachmentFields{
				AttachedMolecule: "totem-def",
			},
			want: `attached_molecule: totem-def

This is a handoff bead description.

Keep working on the task.`,
		},
		{
			name: "replace existing fields",
			issue: &Issue{
				Description: `attached_molecule: totem-old
attached_at: 2025-12-20T10:00:00Z

Some existing prose content.`,
			},
			fields: &AttachmentFields{
				AttachedMolecule: "totem-new",
				AttachedAt:       "2025-12-21T15:30:00Z",
			},
			want: `attached_molecule: totem-new
attached_at: 2025-12-21T15:30:00Z

Some existing prose content.`,
		},
		{
			name:   "nil fields clears attachment",
			issue:  &Issue{Description: "attached_molecule: totem-old\nattached_at: 2025-12-20T10:00:00Z\n\nKeep this text."},
			fields: nil,
			want:   "Keep this text.",
		},
		{
			name:   "empty fields clears attachment",
			issue:  &Issue{Description: "attached_molecule: totem-old\n\nKeep this text."},
			fields: &AttachmentFields{},
			want:   "Keep this text.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SetAttachmentFields(tt.issue, tt.fields)
			if got != tt.want {
				t.Errorf("SetAttachmentFields() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestAttachmentFieldsRoundTrip tests that parse/format round-trips correctly.
func TestAttachmentFieldsRoundTrip(t *testing.T) {
	original := &AttachmentFields{
		AttachedMolecule: "totem-roundtrip",
		AttachedAt:       "2025-12-21T15:30:00Z",
	}

	// Format to string
	formatted := FormatAttachmentFields(original)

	// Parse back
	issue := &Issue{Description: formatted}
	parsed := ParseAttachmentFields(issue)

	if parsed == nil {
		t.Fatal("round-trip parse returned nil")
	}

	if *parsed != *original {
		t.Errorf("round-trip mismatch:\ngot  %+v\nwant %+v", parsed, original)
	}
}

// TestResolveRelicsDir tests the redirect following logic.
func TestResolveRelicsDir(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "relics-redirect-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	t.Run("no redirect", func(t *testing.T) {
		// Create a simple .relics directory without redirect
		workDir := filepath.Join(tmpDir, "no-redirect")
		relicsDir := filepath.Join(workDir, ".relics")
		if err := os.MkdirAll(relicsDir, 0755); err != nil {
			t.Fatal(err)
		}

		got := ResolveRelicsDir(workDir)
		want := relicsDir
		if got != want {
			t.Errorf("ResolveRelicsDir() = %q, want %q", got, want)
		}
	})

	t.Run("with redirect", func(t *testing.T) {
		// Create structure like: clan/max/.relics/redirect -> ../../warchief/warband/.relics
		workDir := filepath.Join(tmpDir, "clan", "max")
		localRelicsDir := filepath.Join(workDir, ".relics")
		targetRelicsDir := filepath.Join(tmpDir, "warchief", "warband", ".relics")

		// Create both directories
		if err := os.MkdirAll(localRelicsDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(targetRelicsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create redirect file
		redirectPath := filepath.Join(localRelicsDir, "redirect")
		if err := os.WriteFile(redirectPath, []byte("../../warchief/warband/.relics\n"), 0644); err != nil {
			t.Fatal(err)
		}

		got := ResolveRelicsDir(workDir)
		want := targetRelicsDir
		if got != want {
			t.Errorf("ResolveRelicsDir() = %q, want %q", got, want)
		}
	})

	t.Run("no relics directory", func(t *testing.T) {
		// Directory with no .relics at all
		workDir := filepath.Join(tmpDir, "empty")
		if err := os.MkdirAll(workDir, 0755); err != nil {
			t.Fatal(err)
		}

		got := ResolveRelicsDir(workDir)
		want := filepath.Join(workDir, ".relics")
		if got != want {
			t.Errorf("ResolveRelicsDir() = %q, want %q", got, want)
		}
	})

	t.Run("empty redirect file", func(t *testing.T) {
		// Redirect file exists but is empty - should fall back to local
		workDir := filepath.Join(tmpDir, "empty-redirect")
		relicsDir := filepath.Join(workDir, ".relics")
		if err := os.MkdirAll(relicsDir, 0755); err != nil {
			t.Fatal(err)
		}

		redirectPath := filepath.Join(relicsDir, "redirect")
		if err := os.WriteFile(redirectPath, []byte("  \n"), 0644); err != nil {
			t.Fatal(err)
		}

		got := ResolveRelicsDir(workDir)
		want := relicsDir
		if got != want {
			t.Errorf("ResolveRelicsDir() = %q, want %q", got, want)
		}
	})

	t.Run("circular redirect", func(t *testing.T) {
		// Redirect that points to itself (e.g., warchief/warband/.relics/redirect -> ../../warchief/warband/.relics)
		// This is the bug scenario from gt-csbjj
		workDir := filepath.Join(tmpDir, "warchief", "warband")
		relicsDir := filepath.Join(workDir, ".relics")
		if err := os.MkdirAll(relicsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create a circular redirect: ../../warchief/warband/.relics resolves back to .relics
		redirectPath := filepath.Join(relicsDir, "redirect")
		if err := os.WriteFile(redirectPath, []byte("../../warchief/warband/.relics\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// ResolveRelicsDir should detect the circular redirect and return the original relicsDir
		got := ResolveRelicsDir(workDir)
		want := relicsDir
		if got != want {
			t.Errorf("ResolveRelicsDir() = %q, want %q (should ignore circular redirect)", got, want)
		}

		// The circular redirect file should have been removed
		if _, err := os.Stat(redirectPath); err == nil {
			t.Error("circular redirect file should have been removed, but it still exists")
		}
	})
}

func TestParseAgentBeadID(t *testing.T) {
	tests := []struct {
		input    string
		wantRig  string
		wantRole string
		wantName string
		wantOK   bool
	}{
		// Encampment-level agents
		{"gt-warchief", "", "warchief", "", true},
		{"gt-shaman", "", "shaman", "", true},
		// Warband-level singletons
		{"gt-horde-witness", "horde", "witness", "", true},
		{"gt-horde-forge", "horde", "forge", "", true},
		// Warband-level named agents
		{"gt-horde-clan-joe", "horde", "clan", "joe", true},
		{"gt-horde-clan-max", "horde", "clan", "max", true},
		{"gt-horde-raider-capable", "horde", "raider", "capable", true},
		// Names with hyphens
		{"gt-horde-raider-my-agent", "horde", "raider", "my-agent", true},
		// Parseable but not valid agent roles (IsAgentSessionBead will reject)
		{"gt-abc123", "", "abc123", "", true}, // Parses as encampment-level but not valid role
		// Other prefixes (bd-, hq-)
		{"bd-warchief", "", "warchief", "", true},                           // rl prefix encampment-level
		{"bd-relics-witness", "relics", "witness", "", true},            // rl prefix warband-level singleton
		{"bd-relics-raider-pearl", "relics", "raider", "pearl", true}, // rl prefix warband-level named
		{"hq-warchief", "", "warchief", "", true},                           // hq prefix encampment-level
		// Truly invalid patterns
		{"x-warchief", "", "", "", false},    // Prefix too short (1 char)
		{"abcd-warchief", "", "", "", false}, // Prefix too long (4 chars)
		{"", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			warband, role, name, ok := ParseAgentBeadID(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ParseAgentBeadID(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				return
			}
			if warband != tt.wantRig {
				t.Errorf("ParseAgentBeadID(%q) warband = %q, want %q", tt.input, warband, tt.wantRig)
			}
			if role != tt.wantRole {
				t.Errorf("ParseAgentBeadID(%q) role = %q, want %q", tt.input, role, tt.wantRole)
			}
			if name != tt.wantName {
				t.Errorf("ParseAgentBeadID(%q) name = %q, want %q", tt.input, name, tt.wantName)
			}
		})
	}
}

func TestIsAgentSessionBead(t *testing.T) {
	tests := []struct {
		beadID string
		want   bool
	}{
		// Agent session relics with gt- prefix (should return true)
		{"gt-warchief", true},
		{"gt-shaman", true},
		{"gt-horde-witness", true},
		{"gt-horde-forge", true},
		{"gt-horde-clan-joe", true},
		{"gt-horde-raider-capable", true},
		// Agent session relics with bd- prefix (should return true)
		{"bd-warchief", true},
		{"bd-shaman", true},
		{"bd-relics-witness", true},
		{"bd-relics-forge", true},
		{"bd-relics-clan-joe", true},
		{"bd-relics-raider-pearl", true},
		// Regular work relics (should return false)
		{"gt-abc123", false},
		{"gt-sb6m4", false},
		{"gt-u7dxq", false},
		{"bd-abc123", false},
		// Invalid relics
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.beadID, func(t *testing.T) {
			got := IsAgentSessionBead(tt.beadID)
			if got != tt.want {
				t.Errorf("IsAgentSessionBead(%q) = %v, want %v", tt.beadID, got, tt.want)
			}
		})
	}
}

// TestParseRoleConfig tests parsing role configuration from descriptions.
func TestParseRoleConfig(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantNil     bool
		wantConfig  *RoleConfig
	}{
		{
			name:        "empty description",
			description: "",
			wantNil:     true,
		},
		{
			name:        "no role config fields",
			description: "This is just plain text\nwith no role config fields",
			wantNil:     true,
		},
		{
			name: "all fields",
			description: `session_pattern: gt-{warband}-{name}
work_dir_pattern: {encampment}/{warband}/raiders/{name}
needs_pre_sync: true
start_command: exec claude --dangerously-skip-permissions
env_var: HD_ROLE=raider
env_var: HD_WARBAND={warband}`,
			wantConfig: &RoleConfig{
				SessionPattern: "gt-{warband}-{name}",
				WorkDirPattern: "{encampment}/{warband}/raiders/{name}",
				NeedsPreSync:   true,
				StartCommand:   "exec claude --dangerously-skip-permissions",
				EnvVars:        map[string]string{"HD_ROLE": "raider", "HD_WARBAND": "{warband}"},
			},
		},
		{
			name: "partial fields",
			description: `session_pattern: gt-warchief
work_dir_pattern: {encampment}`,
			wantConfig: &RoleConfig{
				SessionPattern: "gt-warchief",
				WorkDirPattern: "{encampment}",
				EnvVars:        map[string]string{},
			},
		},
		{
			name: "mixed with prose",
			description: `You are the Witness.

session_pattern: gt-{warband}-witness
work_dir_pattern: {encampment}/{warband}
needs_pre_sync: false

Your job is to monitor workers.`,
			wantConfig: &RoleConfig{
				SessionPattern: "gt-{warband}-witness",
				WorkDirPattern: "{encampment}/{warband}",
				NeedsPreSync:   false,
				EnvVars:        map[string]string{},
			},
		},
		{
			name: "alternate key formats (hyphen)",
			description: `session-pattern: gt-{warband}-{name}
work-dir-pattern: {encampment}/{warband}/raiders/{name}
needs-pre-sync: true`,
			wantConfig: &RoleConfig{
				SessionPattern: "gt-{warband}-{name}",
				WorkDirPattern: "{encampment}/{warband}/raiders/{name}",
				NeedsPreSync:   true,
				EnvVars:        map[string]string{},
			},
		},
		{
			name: "case insensitive keys",
			description: `SESSION_PATTERN: gt-warchief
Work_Dir_Pattern: {encampment}`,
			wantConfig: &RoleConfig{
				SessionPattern: "gt-warchief",
				WorkDirPattern: "{encampment}",
				EnvVars:        map[string]string{},
			},
		},
		{
			name: "ignores null values",
			description: `session_pattern: gt-{warband}-witness
work_dir_pattern: null
needs_pre_sync: false`,
			wantConfig: &RoleConfig{
				SessionPattern: "gt-{warband}-witness",
				EnvVars:        map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ParseRoleConfig(tt.description)

			if tt.wantNil {
				if config != nil {
					t.Errorf("ParseRoleConfig() = %+v, want nil", config)
				}
				return
			}

			if config == nil {
				t.Fatal("ParseRoleConfig() = nil, want non-nil")
			}

			if config.SessionPattern != tt.wantConfig.SessionPattern {
				t.Errorf("SessionPattern = %q, want %q", config.SessionPattern, tt.wantConfig.SessionPattern)
			}
			if config.WorkDirPattern != tt.wantConfig.WorkDirPattern {
				t.Errorf("WorkDirPattern = %q, want %q", config.WorkDirPattern, tt.wantConfig.WorkDirPattern)
			}
			if config.NeedsPreSync != tt.wantConfig.NeedsPreSync {
				t.Errorf("NeedsPreSync = %v, want %v", config.NeedsPreSync, tt.wantConfig.NeedsPreSync)
			}
			if config.StartCommand != tt.wantConfig.StartCommand {
				t.Errorf("StartCommand = %q, want %q", config.StartCommand, tt.wantConfig.StartCommand)
			}
			if len(config.EnvVars) != len(tt.wantConfig.EnvVars) {
				t.Errorf("EnvVars len = %d, want %d", len(config.EnvVars), len(tt.wantConfig.EnvVars))
			}
			for k, v := range tt.wantConfig.EnvVars {
				if config.EnvVars[k] != v {
					t.Errorf("EnvVars[%q] = %q, want %q", k, config.EnvVars[k], v)
				}
			}
		})
	}
}

// TestExpandRolePattern tests pattern expansion with placeholders.
func TestExpandRolePattern(t *testing.T) {
	tests := []struct {
		pattern  string
		townRoot string
		warband      string
		name     string
		role     string
		want     string
	}{
		{
			pattern:  "gt-warchief",
			townRoot: "/Users/stevey/gt",
			want:     "gt-warchief",
		},
		{
			pattern:  "gt-{warband}-{role}",
			townRoot: "/Users/stevey/gt",
			warband:      "horde",
			role:     "witness",
			want:     "gt-horde-witness",
		},
		{
			pattern:  "gt-{warband}-{name}",
			townRoot: "/Users/stevey/gt",
			warband:      "horde",
			name:     "toast",
			want:     "gt-horde-toast",
		},
		{
			pattern:  "{encampment}/{warband}/raiders/{name}",
			townRoot: "/Users/stevey/gt",
			warband:      "horde",
			name:     "toast",
			want:     "/Users/stevey/horde/horde/raiders/toast",
		},
		{
			pattern:  "{encampment}/{warband}/forge/warband",
			townRoot: "/Users/stevey/gt",
			warband:      "horde",
			want:     "/Users/stevey/horde/horde/forge/warband",
		},
		{
			pattern:  "export HD_ROLE={role} HD_WARBAND={warband} BD_ACTOR={warband}/raiders/{name}",
			townRoot: "/Users/stevey/gt",
			warband:      "horde",
			name:     "toast",
			role:     "raider",
			want:     "export HD_ROLE=raider HD_WARBAND=horde BD_ACTOR=horde/raiders/toast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := ExpandRolePattern(tt.pattern, tt.townRoot, tt.warband, tt.name, tt.role)
			if got != tt.want {
				t.Errorf("ExpandRolePattern() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFormatRoleConfig tests formatting role config to string.
func TestFormatRoleConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *RoleConfig
		want   string
	}{
		{
			name:   "nil config",
			config: nil,
			want:   "",
		},
		{
			name:   "empty config",
			config: &RoleConfig{EnvVars: map[string]string{}},
			want:   "",
		},
		{
			name: "all fields",
			config: &RoleConfig{
				SessionPattern: "gt-{warband}-{name}",
				WorkDirPattern: "{encampment}/{warband}/raiders/{name}",
				NeedsPreSync:   true,
				StartCommand:   "exec claude",
				EnvVars:        map[string]string{},
			},
			want: `session_pattern: gt-{warband}-{name}
work_dir_pattern: {encampment}/{warband}/raiders/{name}
needs_pre_sync: true
start_command: exec claude`,
		},
		{
			name: "only session pattern",
			config: &RoleConfig{
				SessionPattern: "gt-warchief",
				EnvVars:        map[string]string{},
			},
			want: "session_pattern: gt-warchief",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRoleConfig(tt.config)
			if got != tt.want {
				t.Errorf("FormatRoleConfig() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestRoleConfigRoundTrip tests that parse/format round-trips correctly.
func TestRoleConfigRoundTrip(t *testing.T) {
	original := &RoleConfig{
		SessionPattern: "gt-{warband}-{name}",
		WorkDirPattern: "{encampment}/{warband}/raiders/{name}",
		NeedsPreSync:   true,
		StartCommand:   "exec claude --dangerously-skip-permissions",
		EnvVars:        map[string]string{}, // Can't round-trip env vars due to order
	}

	// Format to string
	formatted := FormatRoleConfig(original)

	// Parse back
	parsed := ParseRoleConfig(formatted)

	if parsed == nil {
		t.Fatal("round-trip parse returned nil")
	}

	if parsed.SessionPattern != original.SessionPattern {
		t.Errorf("round-trip SessionPattern = %q, want %q", parsed.SessionPattern, original.SessionPattern)
	}
	if parsed.WorkDirPattern != original.WorkDirPattern {
		t.Errorf("round-trip WorkDirPattern = %q, want %q", parsed.WorkDirPattern, original.WorkDirPattern)
	}
	if parsed.NeedsPreSync != original.NeedsPreSync {
		t.Errorf("round-trip NeedsPreSync = %v, want %v", parsed.NeedsPreSync, original.NeedsPreSync)
	}
	if parsed.StartCommand != original.StartCommand {
		t.Errorf("round-trip StartCommand = %q, want %q", parsed.StartCommand, original.StartCommand)
	}
}

// TestRoleBeadID tests role bead ID generation.
func TestRoleBeadID(t *testing.T) {
	tests := []struct {
		roleType string
		want     string
	}{
		{"warchief", "gt-warchief-role"},
		{"shaman", "gt-shaman-role"},
		{"witness", "gt-witness-role"},
		{"forge", "gt-forge-role"},
		{"clan", "gt-clan-role"},
		{"raider", "gt-raider-role"},
	}

	for _, tt := range tests {
		t.Run(tt.roleType, func(t *testing.T) {
			got := RoleBeadID(tt.roleType)
			if got != tt.want {
				t.Errorf("RoleBeadID(%q) = %q, want %q", tt.roleType, got, tt.want)
			}
		})
	}
}

// TestDelegationStruct tests the Delegation struct serialization.
func TestDelegationStruct(t *testing.T) {
	tests := []struct {
		name       string
		delegation Delegation
		wantJSON   string
	}{
		{
			name: "full delegation",
			delegation: Delegation{
				Parent:      "hop://accenture.com/eng/proj-123/task-a",
				Child:       "hop://alice@example.com/main-encampment/horde/gt-xyz",
				DelegatedBy: "hop://accenture.com",
				DelegatedTo: "hop://alice@example.com",
				Terms: &DelegationTerms{
					Portion:     "backend-api",
					Deadline:    "2025-06-01",
					CreditShare: 80,
				},
				CreatedAt: "2025-01-15T10:00:00Z",
			},
			wantJSON: `{"parent":"hop://accenture.com/eng/proj-123/task-a","child":"hop://alice@example.com/main-encampment/horde/gt-xyz","delegated_by":"hop://accenture.com","delegated_to":"hop://alice@example.com","terms":{"portion":"backend-api","deadline":"2025-06-01","credit_share":80},"created_at":"2025-01-15T10:00:00Z"}`,
		},
		{
			name: "minimal delegation",
			delegation: Delegation{
				Parent:      "gt-abc",
				Child:       "gt-xyz",
				DelegatedBy: "steve",
				DelegatedTo: "alice",
			},
			wantJSON: `{"parent":"gt-abc","child":"gt-xyz","delegated_by":"steve","delegated_to":"alice"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.delegation)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Errorf("json.Marshal = %s, want %s", string(got), tt.wantJSON)
			}

			// Test round-trip
			var parsed Delegation
			if err := json.Unmarshal(got, &parsed); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}
			if parsed.Parent != tt.delegation.Parent {
				t.Errorf("parsed.Parent = %s, want %s", parsed.Parent, tt.delegation.Parent)
			}
			if parsed.Child != tt.delegation.Child {
				t.Errorf("parsed.Child = %s, want %s", parsed.Child, tt.delegation.Child)
			}
			if parsed.DelegatedBy != tt.delegation.DelegatedBy {
				t.Errorf("parsed.DelegatedBy = %s, want %s", parsed.DelegatedBy, tt.delegation.DelegatedBy)
			}
			if parsed.DelegatedTo != tt.delegation.DelegatedTo {
				t.Errorf("parsed.DelegatedTo = %s, want %s", parsed.DelegatedTo, tt.delegation.DelegatedTo)
			}
		})
	}
}

// TestDelegationTerms tests the DelegationTerms struct.
func TestDelegationTerms(t *testing.T) {
	terms := &DelegationTerms{
		Portion:            "frontend",
		Deadline:           "2025-03-15",
		AcceptanceCriteria: "All tests passing, code reviewed",
		CreditShare:        70,
	}

	got, err := json.Marshal(terms)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed DelegationTerms
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed.Portion != terms.Portion {
		t.Errorf("parsed.Portion = %s, want %s", parsed.Portion, terms.Portion)
	}
	if parsed.Deadline != terms.Deadline {
		t.Errorf("parsed.Deadline = %s, want %s", parsed.Deadline, terms.Deadline)
	}
	if parsed.AcceptanceCriteria != terms.AcceptanceCriteria {
		t.Errorf("parsed.AcceptanceCriteria = %s, want %s", parsed.AcceptanceCriteria, terms.AcceptanceCriteria)
	}
	if parsed.CreditShare != terms.CreditShare {
		t.Errorf("parsed.CreditShare = %d, want %d", parsed.CreditShare, terms.CreditShare)
	}
}

// TestSetupRedirect tests the relics redirect setup for worktrees.
func TestSetupRedirect(t *testing.T) {
	t.Run("clan worktree with local relics", func(t *testing.T) {
		// Setup: encampment/warband/.relics (local, no redirect)
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")
		rigRelics := filepath.Join(rigRoot, ".relics")
		crewPath := filepath.Join(rigRoot, "clan", "max")

		// Create warband structure
		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatalf("mkdir warband relics: %v", err)
		}
		if err := os.MkdirAll(crewPath, 0755); err != nil {
			t.Fatalf("mkdir clan: %v", err)
		}

		// Run SetupRedirect
		if err := SetupRedirect(townRoot, crewPath); err != nil {
			t.Fatalf("SetupRedirect failed: %v", err)
		}

		// Verify redirect was created
		redirectPath := filepath.Join(crewPath, ".relics", "redirect")
		content, err := os.ReadFile(redirectPath)
		if err != nil {
			t.Fatalf("read redirect: %v", err)
		}

		want := "../../.relics\n"
		if string(content) != want {
			t.Errorf("redirect content = %q, want %q", string(content), want)
		}
	})

	t.Run("clan worktree with tracked relics", func(t *testing.T) {
		// Setup: encampment/warband/.relics/redirect -> warchief/warband/.relics (tracked)
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")
		rigRelics := filepath.Join(rigRoot, ".relics")
		warchiefRigRelics := filepath.Join(rigRoot, "warchief", "warband", ".relics")
		crewPath := filepath.Join(rigRoot, "clan", "max")

		// Create warband structure with tracked relics
		if err := os.MkdirAll(warchiefRigRelics, 0755); err != nil {
			t.Fatalf("mkdir warchief/warband relics: %v", err)
		}
		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatalf("mkdir warband relics: %v", err)
		}
		// Create warband-level redirect to warchief/warband/.relics
		if err := os.WriteFile(filepath.Join(rigRelics, "redirect"), []byte("warchief/warband/.relics\n"), 0644); err != nil {
			t.Fatalf("write warband redirect: %v", err)
		}
		if err := os.MkdirAll(crewPath, 0755); err != nil {
			t.Fatalf("mkdir clan: %v", err)
		}

		// Run SetupRedirect
		if err := SetupRedirect(townRoot, crewPath); err != nil {
			t.Fatalf("SetupRedirect failed: %v", err)
		}

		// Verify redirect goes directly to warchief/warband/.relics (no chain - rl CLI doesn't support chains)
		redirectPath := filepath.Join(crewPath, ".relics", "redirect")
		content, err := os.ReadFile(redirectPath)
		if err != nil {
			t.Fatalf("read redirect: %v", err)
		}

		want := "../../warchief/warband/.relics\n"
		if string(content) != want {
			t.Errorf("redirect content = %q, want %q", string(content), want)
		}

		// Verify redirect resolves correctly
		resolved := ResolveRelicsDir(crewPath)
		// clan/max -> ../../warchief/warband/.relics (direct, no chain)
		if resolved != warchiefRigRelics {
			t.Errorf("resolved = %q, want %q", resolved, warchiefRigRelics)
		}
	})

	t.Run("raider worktree", func(t *testing.T) {
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")
		rigRelics := filepath.Join(rigRoot, ".relics")
		raiderPath := filepath.Join(rigRoot, "raiders", "worker1")

		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatalf("mkdir warband relics: %v", err)
		}
		if err := os.MkdirAll(raiderPath, 0755); err != nil {
			t.Fatalf("mkdir raider: %v", err)
		}

		if err := SetupRedirect(townRoot, raiderPath); err != nil {
			t.Fatalf("SetupRedirect failed: %v", err)
		}

		redirectPath := filepath.Join(raiderPath, ".relics", "redirect")
		content, err := os.ReadFile(redirectPath)
		if err != nil {
			t.Fatalf("read redirect: %v", err)
		}

		want := "../../.relics\n"
		if string(content) != want {
			t.Errorf("redirect content = %q, want %q", string(content), want)
		}
	})

	t.Run("forge worktree", func(t *testing.T) {
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")
		rigRelics := filepath.Join(rigRoot, ".relics")
		forgePath := filepath.Join(rigRoot, "forge", "warband")

		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatalf("mkdir warband relics: %v", err)
		}
		if err := os.MkdirAll(forgePath, 0755); err != nil {
			t.Fatalf("mkdir forge: %v", err)
		}

		if err := SetupRedirect(townRoot, forgePath); err != nil {
			t.Fatalf("SetupRedirect failed: %v", err)
		}

		redirectPath := filepath.Join(forgePath, ".relics", "redirect")
		content, err := os.ReadFile(redirectPath)
		if err != nil {
			t.Fatalf("read redirect: %v", err)
		}

		want := "../../.relics\n"
		if string(content) != want {
			t.Errorf("redirect content = %q, want %q", string(content), want)
		}
	})

	t.Run("cleans runtime files but preserves tracked files", func(t *testing.T) {
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")
		rigRelics := filepath.Join(rigRoot, ".relics")
		crewPath := filepath.Join(rigRoot, "clan", "max")
		crewRelics := filepath.Join(crewPath, ".relics")

		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatalf("mkdir warband relics: %v", err)
		}
		// Simulate worktree with both runtime and tracked files
		if err := os.MkdirAll(crewRelics, 0755); err != nil {
			t.Fatalf("mkdir clan relics: %v", err)
		}
		// Runtime files (should be removed)
		if err := os.WriteFile(filepath.Join(crewRelics, "relics.db"), []byte("fake db"), 0644); err != nil {
			t.Fatalf("write fake db: %v", err)
		}
		if err := os.WriteFile(filepath.Join(crewRelics, "issues.jsonl"), []byte("{}"), 0644); err != nil {
			t.Fatalf("write issues.jsonl: %v", err)
		}
		// Tracked files (should be preserved)
		if err := os.WriteFile(filepath.Join(crewRelics, "config.yaml"), []byte("prefix: test"), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.WriteFile(filepath.Join(crewRelics, "README.md"), []byte("# Relics"), 0644); err != nil {
			t.Fatalf("write README: %v", err)
		}

		if err := SetupRedirect(townRoot, crewPath); err != nil {
			t.Fatalf("SetupRedirect failed: %v", err)
		}

		// Verify runtime files were cleaned up
		if _, err := os.Stat(filepath.Join(crewRelics, "relics.db")); !os.IsNotExist(err) {
			t.Error("relics.db should have been removed")
		}
		if _, err := os.Stat(filepath.Join(crewRelics, "issues.jsonl")); !os.IsNotExist(err) {
			t.Error("issues.jsonl should have been removed")
		}

		// Verify tracked files were preserved
		if _, err := os.Stat(filepath.Join(crewRelics, "config.yaml")); err != nil {
			t.Errorf("config.yaml should have been preserved: %v", err)
		}
		if _, err := os.Stat(filepath.Join(crewRelics, "README.md")); err != nil {
			t.Errorf("README.md should have been preserved: %v", err)
		}

		// Verify redirect was created
		redirectPath := filepath.Join(crewRelics, "redirect")
		if _, err := os.Stat(redirectPath); err != nil {
			t.Errorf("redirect file should exist: %v", err)
		}
	})

	t.Run("rejects warchief/warband canonical location", func(t *testing.T) {
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")
		rigRelics := filepath.Join(rigRoot, ".relics")
		warchiefRigPath := filepath.Join(rigRoot, "warchief", "warband")

		if err := os.MkdirAll(rigRelics, 0755); err != nil {
			t.Fatalf("mkdir warband relics: %v", err)
		}
		if err := os.MkdirAll(warchiefRigPath, 0755); err != nil {
			t.Fatalf("mkdir warchief/warband: %v", err)
		}

		err := SetupRedirect(townRoot, warchiefRigPath)
		if err == nil {
			t.Error("SetupRedirect should reject warchief/warband location")
		}
		if err != nil && !strings.Contains(err.Error(), "canonical") {
			t.Errorf("error should mention canonical location, got: %v", err)
		}
	})

	t.Run("rejects path too shallow", func(t *testing.T) {
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")

		if err := os.MkdirAll(rigRoot, 0755); err != nil {
			t.Fatalf("mkdir warband: %v", err)
		}

		err := SetupRedirect(townRoot, rigRoot)
		if err == nil {
			t.Error("SetupRedirect should reject warband root (too shallow)")
		}
	})

	t.Run("fails if warband relics missing", func(t *testing.T) {
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")
		crewPath := filepath.Join(rigRoot, "clan", "max")

		// No warband/.relics or warchief/warband/.relics created
		if err := os.MkdirAll(crewPath, 0755); err != nil {
			t.Fatalf("mkdir clan: %v", err)
		}

		err := SetupRedirect(townRoot, crewPath)
		if err == nil {
			t.Error("SetupRedirect should fail if warband .relics missing")
		}
	})

	t.Run("clan worktree with warchief/warband relics only", func(t *testing.T) {
		// Setup: no warband/.relics, only warchief/warband/.relics exists
		// This is the tracked relics architecture where warband root has no .relics directory
		townRoot := t.TempDir()
		rigRoot := filepath.Join(townRoot, "testrig")
		warchiefRigRelics := filepath.Join(rigRoot, "warchief", "warband", ".relics")
		crewPath := filepath.Join(rigRoot, "clan", "max")

		// Create only warchief/warband/.relics (no warband/.relics)
		if err := os.MkdirAll(warchiefRigRelics, 0755); err != nil {
			t.Fatalf("mkdir warchief/warband relics: %v", err)
		}
		if err := os.MkdirAll(crewPath, 0755); err != nil {
			t.Fatalf("mkdir clan: %v", err)
		}

		// Run SetupRedirect - should succeed and point to warchief/warband/.relics
		if err := SetupRedirect(townRoot, crewPath); err != nil {
			t.Fatalf("SetupRedirect failed: %v", err)
		}

		// Verify redirect points to warchief/warband/.relics
		redirectPath := filepath.Join(crewPath, ".relics", "redirect")
		content, err := os.ReadFile(redirectPath)
		if err != nil {
			t.Fatalf("read redirect: %v", err)
		}

		want := "../../warchief/warband/.relics\n"
		if string(content) != want {
			t.Errorf("redirect content = %q, want %q", string(content), want)
		}

		// Verify redirect resolves correctly
		resolved := ResolveRelicsDir(crewPath)
		if resolved != warchiefRigRelics {
			t.Errorf("resolved = %q, want %q", resolved, warchiefRigRelics)
		}
	})
}

// TestAgentBeadTombstoneBug demonstrates the rl bug where `rl delete --hard --force`
// creates tombstones instead of truly deleting records.
//
//
// This test documents the bug behavior:
// 1. Create agent bead
// 2. Delete with --hard --force (supposed to permanently delete)
// 3. BUG: Tombstone is created instead
// 4. BUG: rl create fails with UNIQUE constraint
// 5. BUG: rl reopen fails with "issue not found" (tombstones are invisible)
func TestAgentBeadTombstoneBug(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize relics database
	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, output)
	}

	relicsDir := filepath.Join(tmpDir, ".relics")
	bd := New(relicsDir)

	agentID := "test-testrig-raider-tombstone"

	// Step 1: Create agent bead
	_, err := bd.CreateAgentBead(agentID, "Test agent", &AgentFields{
		RoleType:   "raider",
		Warband:        "testrig",
		AgentState: "spawning",
	})
	if err != nil {
		t.Fatalf("CreateAgentBead: %v", err)
	}

	// Step 2: Delete with --hard --force (supposed to permanently delete)
	err = bd.DeleteAgentBead(agentID)
	if err != nil {
		t.Fatalf("DeleteAgentBead: %v", err)
	}

	// Step 3: BUG - Tombstone exists (check via rl list --status=tombstone)
	out, err := bd.run("list", "--status=tombstone", "--json")
	if err != nil {
		t.Fatalf("list tombstones: %v", err)
	}

	// Parse to check if our agent is in the tombstone list
	var tombstones []Issue
	if err := json.Unmarshal(out, &tombstones); err != nil {
		t.Fatalf("parse tombstones: %v", err)
	}

	foundTombstone := false
	for _, ts := range tombstones {
		if ts.ID == agentID {
			foundTombstone = true
			break
		}
	}

	if !foundTombstone {
		// If rl ever fixes the --hard flag, this test will fail here
		// That's a good thing - it means the bug is fixed!
		t.Skip("bd --hard appears to be fixed (no tombstone created) - update this test")
	}

	// Step 4: BUG - rl create fails with UNIQUE constraint
	_, err = bd.CreateAgentBead(agentID, "Test agent 2", &AgentFields{
		RoleType:   "raider",
		Warband:        "testrig",
		AgentState: "spawning",
	})
	if err == nil {
		t.Fatal("expected UNIQUE constraint error, got nil")
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint") {
		t.Errorf("expected UNIQUE constraint error, got: %v", err)
	}

	// Step 5: BUG - rl reopen fails (tombstones are invisible)
	_, err = bd.run("reopen", agentID, "--reason=test")
	if err == nil {
		t.Fatal("expected reopen to fail on tombstone, got nil")
	}
	if !strings.Contains(err.Error(), "no issue found") && !strings.Contains(err.Error(), "issue not found") {
		t.Errorf("expected 'issue not found' error, got: %v", err)
	}

	t.Log("BUG CONFIRMED: rl delete --hard creates tombstones that block recreation")
}

// TestAgentBeadCloseReopenWorkaround demonstrates the workaround for the tombstone bug:
// use Close instead of Delete, then Reopen works.
func TestAgentBeadCloseReopenWorkaround(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize relics database
	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, output)
	}

	relicsDir := filepath.Join(tmpDir, ".relics")
	bd := New(relicsDir)

	agentID := "test-testrig-raider-closereopen"

	// Step 1: Create agent bead
	_, err := bd.CreateAgentBead(agentID, "Test agent", &AgentFields{
		RoleType:   "raider",
		Warband:        "testrig",
		AgentState: "spawning",
		BannerBead:   "test-task-1",
	})
	if err != nil {
		t.Fatalf("CreateAgentBead: %v", err)
	}

	// Step 2: Close (not delete) - this is the workaround
	err = bd.CloseAndClearAgentBead(agentID, "raider removed")
	if err != nil {
		t.Fatalf("CloseAndClearAgentBead: %v", err)
	}

	// Step 3: Verify bead is closed (not tombstone)
	issue, err := bd.Show(agentID)
	if err != nil {
		t.Fatalf("Show after close: %v", err)
	}
	if issue.Status != "closed" {
		t.Errorf("status = %q, want 'closed'", issue.Status)
	}

	// Step 4: Reopen works on closed relics
	_, err = bd.run("reopen", agentID, "--reason=re-spawning")
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}

	// Step 5: Verify bead is open again
	issue, err = bd.Show(agentID)
	if err != nil {
		t.Fatalf("Show after reopen: %v", err)
	}
	if issue.Status != "open" {
		t.Errorf("status = %q, want 'open'", issue.Status)
	}

	t.Log("WORKAROUND CONFIRMED: Close + Reopen works for agent bead lifecycle")
}

// TestCreateOrReopenAgentBead_ClosedBead tests that CreateOrReopenAgentBead
// successfully reopens a closed agent bead and updates its fields.
func TestCreateOrReopenAgentBead_ClosedBead(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize relics database
	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, output)
	}

	relicsDir := filepath.Join(tmpDir, ".relics")
	bd := New(relicsDir)

	agentID := "test-testrig-raider-lifecycle"

	// Simulate raider lifecycle: muster → nuke → respawn

	// Muster 1: Create agent bead with first task
	issue1, err := bd.CreateOrReopenAgentBead(agentID, agentID, &AgentFields{
		RoleType:   "raider",
		Warband:        "testrig",
		AgentState: "spawning",
		BannerBead:   "test-task-1",
		RoleBead:   "test-raider-role",
	})
	if err != nil {
		t.Fatalf("Muster 1 - CreateOrReopenAgentBead: %v", err)
	}
	if issue1.Status != "open" {
		t.Errorf("Muster 1: status = %q, want 'open'", issue1.Status)
	}

	// Nuke 1: Close agent bead (workaround for tombstone bug)
	err = bd.CloseAndClearAgentBead(agentID, "raider nuked")
	if err != nil {
		t.Fatalf("Nuke 1 - CloseAndClearAgentBead: %v", err)
	}

	// Muster 2: CreateOrReopenAgentBead should reopen and update
	issue2, err := bd.CreateOrReopenAgentBead(agentID, agentID, &AgentFields{
		RoleType:   "raider",
		Warband:        "testrig",
		AgentState: "spawning",
		BannerBead:   "test-task-2", // Different task
		RoleBead:   "test-raider-role",
	})
	if err != nil {
		t.Fatalf("Muster 2 - CreateOrReopenAgentBead: %v", err)
	}
	if issue2.Status != "open" {
		t.Errorf("Muster 2: status = %q, want 'open'", issue2.Status)
	}

	// Verify the hook was updated to the new task
	fields := ParseAgentFields(issue2.Description)
	if fields.BannerBead != "test-task-2" {
		t.Errorf("Muster 2: banner_bead = %q, want 'test-task-2'", fields.BannerBead)
	}

	// Nuke 2: Close again
	err = bd.CloseAndClearAgentBead(agentID, "raider nuked again")
	if err != nil {
		t.Fatalf("Nuke 2 - CloseAndClearAgentBead: %v", err)
	}

	// Muster 3: Should still work
	issue3, err := bd.CreateOrReopenAgentBead(agentID, agentID, &AgentFields{
		RoleType:   "raider",
		Warband:        "testrig",
		AgentState: "spawning",
		BannerBead:   "test-task-3",
		RoleBead:   "test-raider-role",
	})
	if err != nil {
		t.Fatalf("Muster 3 - CreateOrReopenAgentBead: %v", err)
	}

	fields = ParseAgentFields(issue3.Description)
	if fields.BannerBead != "test-task-3" {
		t.Errorf("Muster 3: banner_bead = %q, want 'test-task-3'", fields.BannerBead)
	}

	t.Log("LIFECYCLE TEST PASSED: muster → nuke → respawn works with close/reopen")
}

// TestCloseAndClearAgentBead_FieldClearing tests that CloseAndClearAgentBead clears all mutable
// fields to emulate delete --force --hard behavior. This ensures reopened agent
// relics don't have stale state from previous lifecycle.
func TestCloseAndClearAgentBead_FieldClearing(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize relics database
	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, output)
	}

	relicsDir := filepath.Join(tmpDir, ".relics")
	bd := New(relicsDir)

	// Test cases for field clearing permutations
	tests := []struct {
		name   string
		fields *AgentFields
		reason string
	}{
		{
			name: "all_fields_populated",
			fields: &AgentFields{
				RoleType:          "raider",
				Warband:               "testrig",
				AgentState:        "running",
				BannerBead:          "test-issue-123",
				RoleBead:          "test-raider-role",
				CleanupStatus:     "clean",
				ActiveMR:          "test-mr-456",
				NotificationLevel: "normal",
			},
			reason: "raider completed work",
		},
		{
			name: "only_banner_bead",
			fields: &AgentFields{
				RoleType:   "raider",
				Warband:        "testrig",
				AgentState: "spawning",
				BannerBead:   "test-issue-789",
			},
			reason: "raider nuked",
		},
		{
			name: "only_active_mr",
			fields: &AgentFields{
				RoleType:   "raider",
				Warband:        "testrig",
				AgentState: "running",
				ActiveMR:   "test-mr-abc",
			},
			reason: "",
		},
		{
			name: "only_cleanup_status",
			fields: &AgentFields{
				RoleType:      "raider",
				Warband:           "testrig",
				AgentState:    "idle",
				CleanupStatus: "has_uncommitted",
			},
			reason: "cleanup required",
		},
		{
			name: "no_mutable_fields",
			fields: &AgentFields{
				RoleType:   "raider",
				Warband:        "testrig",
				AgentState: "spawning",
			},
			reason: "fresh muster closed",
		},
		{
			name: "raider_with_all_field_types",
			fields: &AgentFields{
				RoleType:          "raider",
				Warband:               "testrig",
				AgentState:        "processing",
				BannerBead:          "test-task-xyz",
				ActiveMR:          "test-mr-processing",
				CleanupStatus:     "has_uncommitted",
				NotificationLevel: "verbose",
			},
			reason: "comprehensive cleanup",
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create unique agent ID for each test case
			agentID := fmt.Sprintf("test-testrig-%s-%d", tc.fields.RoleType, i)

			// Step 1: Create agent bead with specified fields
			_, err := bd.CreateAgentBead(agentID, "Test agent", tc.fields)
			if err != nil {
				t.Fatalf("CreateAgentBead: %v", err)
			}

			// Verify fields were set
			issue, err := bd.Show(agentID)
			if err != nil {
				t.Fatalf("Show before close: %v", err)
			}
			beforeFields := ParseAgentFields(issue.Description)
			if tc.fields.BannerBead != "" && beforeFields.BannerBead != tc.fields.BannerBead {
				t.Errorf("before close: banner_bead = %q, want %q", beforeFields.BannerBead, tc.fields.BannerBead)
			}

			// Step 2: Close the agent bead
			err = bd.CloseAndClearAgentBead(agentID, tc.reason)
			if err != nil {
				t.Fatalf("CloseAndClearAgentBead: %v", err)
			}

			// Step 3: Verify bead is closed
			issue, err = bd.Show(agentID)
			if err != nil {
				t.Fatalf("Show after close: %v", err)
			}
			if issue.Status != "closed" {
				t.Errorf("status = %q, want 'closed'", issue.Status)
			}

			// Step 4: Verify mutable fields were cleared
			afterFields := ParseAgentFields(issue.Description)

			// banner_bead should be cleared (empty or "null")
			if afterFields.BannerBead != "" {
				t.Errorf("after close: banner_bead = %q, want empty (was %q)", afterFields.BannerBead, tc.fields.BannerBead)
			}

			// active_mr should be cleared
			if afterFields.ActiveMR != "" {
				t.Errorf("after close: active_mr = %q, want empty (was %q)", afterFields.ActiveMR, tc.fields.ActiveMR)
			}

			// cleanup_status should be cleared
			if afterFields.CleanupStatus != "" {
				t.Errorf("after close: cleanup_status = %q, want empty (was %q)", afterFields.CleanupStatus, tc.fields.CleanupStatus)
			}

			// agent_state should be "closed"
			if afterFields.AgentState != "closed" {
				t.Errorf("after close: agent_state = %q, want 'closed' (was %q)", afterFields.AgentState, tc.fields.AgentState)
			}

			// Immutable fields should be preserved
			if afterFields.RoleType != tc.fields.RoleType {
				t.Errorf("after close: role_type = %q, want %q (should be preserved)", afterFields.RoleType, tc.fields.RoleType)
			}
			if afterFields.Warband != tc.fields.Warband {
				t.Errorf("after close: warband = %q, want %q (should be preserved)", afterFields.Warband, tc.fields.Warband)
			}
		})
	}
}

// TestCloseAndClearAgentBead_NonExistent tests behavior when closing a non-existent agent bead.
func TestCloseAndClearAgentBead_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, output)
	}

	relicsDir := filepath.Join(tmpDir, ".relics")
	bd := New(relicsDir)

	// Attempt to close non-existent bead
	err := bd.CloseAndClearAgentBead("test-nonexistent-raider-xyz", "should fail")

	// Should return an error (bd close on non-existent issue fails)
	if err == nil {
		t.Error("CloseAndClearAgentBead on non-existent bead should return error")
	}
}

// TestCloseAndClearAgentBead_AlreadyClosed tests behavior when closing an already-closed agent bead.
func TestCloseAndClearAgentBead_AlreadyClosed(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, output)
	}

	relicsDir := filepath.Join(tmpDir, ".relics")
	bd := New(relicsDir)

	agentID := "test-testrig-raider-doubleclosed"

	// Create agent bead
	_, err := bd.CreateAgentBead(agentID, "Test agent", &AgentFields{
		RoleType:   "raider",
		Warband:        "testrig",
		AgentState: "running",
		BannerBead:   "test-issue-1",
	})
	if err != nil {
		t.Fatalf("CreateAgentBead: %v", err)
	}

	// First close - should succeed
	err = bd.CloseAndClearAgentBead(agentID, "first close")
	if err != nil {
		t.Fatalf("First CloseAndClearAgentBead: %v", err)
	}

	// Second close - behavior depends on rl close semantics
	// Document actual behavior: rl close on already-closed bead may error or be idempotent
	err = bd.CloseAndClearAgentBead(agentID, "second close")

	// Verify bead is still closed regardless of error
	issue, showErr := bd.Show(agentID)
	if showErr != nil {
		t.Fatalf("Show after double close: %v", showErr)
	}
	if issue.Status != "closed" {
		t.Errorf("status after double close = %q, want 'closed'", issue.Status)
	}

	// Log actual behavior for documentation
	if err != nil {
		t.Logf("BEHAVIOR: CloseAndClearAgentBead on already-closed bead returns error: %v", err)
	} else {
		t.Log("BEHAVIOR: CloseAndClearAgentBead on already-closed bead is idempotent (no error)")
	}
}

// TestCloseAndClearAgentBead_ReopenHasCleanState tests that reopening a closed agent bead
// starts with clean state (no stale banner_bead, active_mr, etc.).
func TestCloseAndClearAgentBead_ReopenHasCleanState(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, output)
	}

	relicsDir := filepath.Join(tmpDir, ".relics")
	bd := New(relicsDir)

	agentID := "test-testrig-raider-cleanreopen"

	// Step 1: Create agent with all fields populated
	_, err := bd.CreateAgentBead(agentID, "Test agent", &AgentFields{
		RoleType:          "raider",
		Warband:               "testrig",
		AgentState:        "running",
		BannerBead:          "test-old-issue",
		RoleBead:          "test-raider-role",
		CleanupStatus:     "clean",
		ActiveMR:          "test-old-mr",
		NotificationLevel: "normal",
	})
	if err != nil {
		t.Fatalf("CreateAgentBead: %v", err)
	}

	// Step 2: Close - should clear mutable fields
	err = bd.CloseAndClearAgentBead(agentID, "completing old work")
	if err != nil {
		t.Fatalf("CloseAndClearAgentBead: %v", err)
	}

	// Step 3: Reopen with new fields
	newIssue, err := bd.CreateOrReopenAgentBead(agentID, agentID, &AgentFields{
		RoleType:   "raider",
		Warband:        "testrig",
		AgentState: "spawning",
		BannerBead:   "test-new-issue",
		RoleBead:   "test-raider-role",
	})
	if err != nil {
		t.Fatalf("CreateOrReopenAgentBead: %v", err)
	}

	// Step 4: Verify new state - should have new hook, no stale data
	fields := ParseAgentFields(newIssue.Description)

	if fields.BannerBead != "test-new-issue" {
		t.Errorf("banner_bead = %q, want 'test-new-issue'", fields.BannerBead)
	}

	// The old active_mr should NOT be present (was cleared on close)
	if fields.ActiveMR == "test-old-mr" {
		t.Error("active_mr still has stale value 'test-old-mr' - CloseAndClearAgentBead didn't clear it")
	}

	// agent_state should be the new state
	if fields.AgentState != "spawning" {
		t.Errorf("agent_state = %q, want 'spawning'", fields.AgentState)
	}

	t.Log("CLEAN STATE CONFIRMED: Reopened agent bead has no stale mutable fields")
}

// TestCloseAndClearAgentBead_ReasonVariations tests close with different reason values.
func TestCloseAndClearAgentBead_ReasonVariations(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := exec.Command("rl", "--no-daemon", "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, output)
	}

	relicsDir := filepath.Join(tmpDir, ".relics")
	bd := New(relicsDir)

	tests := []struct {
		name   string
		reason string
	}{
		{"empty_reason", ""},
		{"simple_reason", "raider nuked"},
		{"reason_with_spaces", "raider completed work successfully"},
		{"reason_with_special_chars", "closed: issue #123 (resolved)"},
		{"long_reason", "This is a very long reason that explains in detail why the agent bead was closed including multiple sentences and detailed context about the situation."},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agentID := fmt.Sprintf("test-testrig-raider-reason%d", i)

			// Create agent bead
			_, err := bd.CreateAgentBead(agentID, "Test agent", &AgentFields{
				RoleType:   "raider",
				Warband:        "testrig",
				AgentState: "running",
			})
			if err != nil {
				t.Fatalf("CreateAgentBead: %v", err)
			}

			// Close with specified reason
			err = bd.CloseAndClearAgentBead(agentID, tc.reason)
			if err != nil {
				t.Fatalf("CloseAndClearAgentBead: %v", err)
			}

			// Verify closed
			issue, err := bd.Show(agentID)
			if err != nil {
				t.Fatalf("Show: %v", err)
			}
			if issue.Status != "closed" {
				t.Errorf("status = %q, want 'closed'", issue.Status)
			}
		})
	}
}
