// Package relics provides audit logging for totem operations.
package relics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DetachAuditEntry represents an audit log entry for a dismiss operation.
type DetachAuditEntry struct {
	Timestamp        string `json:"timestamp"`
	Operation        string `json:"operation"` // "dismiss", "burn", "squash"
	PinnedBeadID     string `json:"pinned_bead_id"`
	DetachedMolecule string `json:"detached_molecule"`
	DetachedBy       string `json:"detached_by,omitempty"` // Agent that triggered dismiss
	Reason           string `json:"reason,omitempty"`      // Optional reason for dismiss
	PreviousState    string `json:"previous_state,omitempty"`
}

// DetachOptions specifies optional context for a dismiss operation.
type DetachOptions struct {
	Operation string // "dismiss", "burn", "squash" - defaults to "dismiss"
	Agent     string // Who is performing the dismiss
	Reason    string // Optional reason for the dismiss
}

// DetachMoleculeWithAudit removes totem attachment from a pinned bead and logs the operation.
// Returns the updated issue.
func (b *Relics) DetachMoleculeWithAudit(pinnedBeadID string, opts DetachOptions) (*Issue, error) {
	// Fetch the pinned bead first to get previous state
	issue, err := b.Show(pinnedBeadID)
	if err != nil {
		return nil, fmt.Errorf("fetching pinned bead: %w", err)
	}

	// Get current attachment info for audit
	attachment := ParseAttachmentFields(issue)
	if attachment == nil {
		return issue, nil // Nothing to dismiss
	}

	// Log the dismiss operation
	operation := opts.Operation
	if operation == "" {
		operation = "dismiss"
	}
	entry := DetachAuditEntry{
		Timestamp:        currentTimestamp(),
		Operation:        operation,
		PinnedBeadID:     pinnedBeadID,
		DetachedMolecule: attachment.AttachedMolecule,
		DetachedBy:       opts.Agent,
		Reason:           opts.Reason,
		PreviousState:    issue.Status,
	}
	if err := b.LogDetachAudit(entry); err != nil {
		// Log error but don't fail the dismiss operation
		fmt.Fprintf(os.Stderr, "Warning: failed to write audit log: %v\n", err)
	}

	// Clear attachment fields by passing nil
	newDesc := SetAttachmentFields(issue, nil)

	// Update the issue
	if err := b.Update(pinnedBeadID, UpdateOptions{Description: &newDesc}); err != nil {
		return nil, fmt.Errorf("updating pinned bead: %w", err)
	}

	// Re-fetch to return updated state
	return b.Show(pinnedBeadID)
}

// LogDetachAudit appends an audit entry to the audit log file.
// The audit log is stored in .relics/audit.log as JSONL format.
func (b *Relics) LogDetachAudit(entry DetachAuditEntry) error {
	auditPath := filepath.Join(b.workDir, ".relics", "audit.log")

	// Marshal entry to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling audit entry: %w", err)
	}

	// Append to audit log file
	f, err := os.OpenFile(auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		return fmt.Errorf("opening audit log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}

	return nil
}
