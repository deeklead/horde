// Package protocol provides inter-agent protocol message handling.
//
// This package defines protocol message types for Witness-Forge communication
// and provides handlers for processing these messages.
//
// Protocol Message Types:
//   - MERGE_READY: Witness → Forge (branch ready for merge)
//   - MERGED: Forge → Witness (merge succeeded, cleanup ok)
//   - MERGE_FAILED: Forge → Witness (merge failed, needs rework)
//   - REWORK_REQUEST: Forge → Witness (rebase needed)
package protocol

import (
	"strings"
	"time"
)

// MessageType identifies the protocol message type.
type MessageType string

const (
	// TypeMergeReady is sent from Witness to Forge when a raider's work
	// is verified and ready for merge queue processing.
	// Subject format: "MERGE_READY <raider-name>"
	TypeMergeReady MessageType = "MERGE_READY"

	// TypeMerged is sent from Forge to Witness when a branch has been
	// successfully merged to the target branch.
	// Subject format: "MERGED <raider-name>"
	TypeMerged MessageType = "MERGED"

	// TypeMergeFailed is sent from Forge to Witness when a merge attempt
	// failed (tests, build, or other non-conflict error).
	// Subject format: "MERGE_FAILED <raider-name>"
	TypeMergeFailed MessageType = "MERGE_FAILED"

	// TypeReworkRequest is sent from Forge to Witness when a raider's
	// branch needs rebasing due to conflicts with the target branch.
	// Subject format: "REWORK_REQUEST <raider-name>"
	TypeReworkRequest MessageType = "REWORK_REQUEST"
)

// ParseMessageType extracts the protocol message type from a drums subject.
// Returns empty string if subject doesn't match a known protocol type.
func ParseMessageType(subject string) MessageType {
	subject = strings.TrimSpace(subject)

	// Check each known prefix
	prefixes := []MessageType{
		TypeMergeReady,
		TypeMerged,
		TypeMergeFailed,
		TypeReworkRequest,
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(subject, string(prefix)) {
			return prefix
		}
	}

	return ""
}

// MergeReadyPayload contains the data for a MERGE_READY message.
// Sent by Witness after verifying raider work is complete.
type MergeReadyPayload struct {
	// Branch is the raider's work branch (e.g., "raider/Toast/gt-abc").
	Branch string `json:"branch"`

	// Issue is the relics issue ID the raider completed.
	Issue string `json:"issue"`

	// Raider is the worker name.
	Raider string `json:"raider"`

	// Warband is the warband name containing the raider.
	Warband string `json:"warband"`

	// Verified contains verification notes.
	Verified string `json:"verified,omitempty"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp"`
}

// MergedPayload contains the data for a MERGED message.
// Sent by Forge after successful merge to target branch.
type MergedPayload struct {
	// Branch is the source branch that was merged.
	Branch string `json:"branch"`

	// Issue is the relics issue ID.
	Issue string `json:"issue"`

	// Raider is the worker name.
	Raider string `json:"raider"`

	// Warband is the warband name.
	Warband string `json:"warband"`

	// MergedAt is when the merge completed.
	MergedAt time.Time `json:"merged_at"`

	// MergeCommit is the SHA of the merge commit.
	MergeCommit string `json:"merge_commit,omitempty"`

	// TargetBranch is the branch merged into (e.g., "main").
	TargetBranch string `json:"target_branch"`
}

// MergeFailedPayload contains the data for a MERGE_FAILED message.
// Sent by Forge when merge fails due to tests, build, or other errors.
type MergeFailedPayload struct {
	// Branch is the source branch that failed to merge.
	Branch string `json:"branch"`

	// Issue is the relics issue ID.
	Issue string `json:"issue"`

	// Raider is the worker name.
	Raider string `json:"raider"`

	// Warband is the warband name.
	Warband string `json:"warband"`

	// FailedAt is when the failure occurred.
	FailedAt time.Time `json:"failed_at"`

	// FailureType categorizes the failure (tests, build, push, etc.).
	FailureType string `json:"failure_type"`

	// Error is the error message.
	Error string `json:"error"`

	// TargetBranch is the branch we tried to merge into.
	TargetBranch string `json:"target_branch"`
}

// ReworkRequestPayload contains the data for a REWORK_REQUEST message.
// Sent by Forge when a raider's branch has conflicts requiring rebase.
type ReworkRequestPayload struct {
	// Branch is the source branch that needs rebasing.
	Branch string `json:"branch"`

	// Issue is the relics issue ID.
	Issue string `json:"issue"`

	// Raider is the worker name.
	Raider string `json:"raider"`

	// Warband is the warband name.
	Warband string `json:"warband"`

	// RequestedAt is when the rework was requested.
	RequestedAt time.Time `json:"requested_at"`

	// TargetBranch is the branch to rebase onto.
	TargetBranch string `json:"target_branch"`

	// ConflictFiles lists files with conflicts (if known).
	ConflictFiles []string `json:"conflict_files,omitempty"`

	// Instructions provides specific rebase instructions.
	Instructions string `json:"instructions,omitempty"`
}

// IsProtocolMessage returns true if the subject matches a known protocol type.
func IsProtocolMessage(subject string) bool {
	return ParseMessageType(subject) != ""
}

// ExtractRaider extracts the raider name from a protocol message subject.
// Subject format: "TYPE <raider-name>"
func ExtractRaider(subject string) string {
	subject = strings.TrimSpace(subject)
	parts := strings.SplitN(subject, " ", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
