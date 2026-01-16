package protocol

import (
	"fmt"
	"strings"
	"time"

	"github.com/OWNER/horde/internal/drums"
)

// NewMergeReadyMessage creates a MERGE_READY protocol message.
// Sent by Witness to Forge when a raider's work is verified and ready.
func NewMergeReadyMessage(warband, raider, branch, issue string) *drums.Message {
	payload := MergeReadyPayload{
		Branch:    branch,
		Issue:     issue,
		Raider:   raider,
		Warband:       warband,
		Verified:  "clean git state, issue closed",
		Timestamp: time.Now(),
	}

	body := formatMergeReadyBody(payload)

	msg := drums.NewMessage(
		fmt.Sprintf("%s/witness", warband),
		fmt.Sprintf("%s/forge", warband),
		fmt.Sprintf("MERGE_READY %s", raider),
		body,
	)
	msg.Priority = drums.PriorityHigh
	msg.Type = drums.TypeTask

	return msg
}

// formatMergeReadyBody formats the body of a MERGE_READY message.
func formatMergeReadyBody(p MergeReadyPayload) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Branch: %s\n", p.Branch))
	sb.WriteString(fmt.Sprintf("Issue: %s\n", p.Issue))
	sb.WriteString(fmt.Sprintf("Raider: %s\n", p.Raider))
	sb.WriteString(fmt.Sprintf("Warband: %s\n", p.Warband))
	if p.Verified != "" {
		sb.WriteString(fmt.Sprintf("Verified: %s\n", p.Verified))
	}
	return sb.String()
}

// NewMergedMessage creates a MERGED protocol message.
// Sent by Forge to Witness when a branch is successfully merged.
func NewMergedMessage(warband, raider, branch, issue, targetBranch, mergeCommit string) *drums.Message {
	payload := MergedPayload{
		Branch:       branch,
		Issue:        issue,
		Raider:      raider,
		Warband:          warband,
		MergedAt:     time.Now(),
		MergeCommit:  mergeCommit,
		TargetBranch: targetBranch,
	}

	body := formatMergedBody(payload)

	msg := drums.NewMessage(
		fmt.Sprintf("%s/forge", warband),
		fmt.Sprintf("%s/witness", warband),
		fmt.Sprintf("MERGED %s", raider),
		body,
	)
	msg.Priority = drums.PriorityHigh
	msg.Type = drums.TypeNotification

	return msg
}

// formatMergedBody formats the body of a MERGED message.
func formatMergedBody(p MergedPayload) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Branch: %s\n", p.Branch))
	sb.WriteString(fmt.Sprintf("Issue: %s\n", p.Issue))
	sb.WriteString(fmt.Sprintf("Raider: %s\n", p.Raider))
	sb.WriteString(fmt.Sprintf("Warband: %s\n", p.Warband))
	sb.WriteString(fmt.Sprintf("Target: %s\n", p.TargetBranch))
	sb.WriteString(fmt.Sprintf("Merged-At: %s\n", p.MergedAt.Format(time.RFC3339)))
	if p.MergeCommit != "" {
		sb.WriteString(fmt.Sprintf("Merge-Commit: %s\n", p.MergeCommit))
	}
	return sb.String()
}

// NewMergeFailedMessage creates a MERGE_FAILED protocol message.
// Sent by Forge to Witness when merge fails (tests, build, etc.).
func NewMergeFailedMessage(warband, raider, branch, issue, targetBranch, failureType, errorMsg string) *drums.Message {
	payload := MergeFailedPayload{
		Branch:       branch,
		Issue:        issue,
		Raider:      raider,
		Warband:          warband,
		FailedAt:     time.Now(),
		FailureType:  failureType,
		Error:        errorMsg,
		TargetBranch: targetBranch,
	}

	body := formatMergeFailedBody(payload)

	msg := drums.NewMessage(
		fmt.Sprintf("%s/forge", warband),
		fmt.Sprintf("%s/witness", warband),
		fmt.Sprintf("MERGE_FAILED %s", raider),
		body,
	)
	msg.Priority = drums.PriorityHigh
	msg.Type = drums.TypeTask

	return msg
}

// formatMergeFailedBody formats the body of a MERGE_FAILED message.
func formatMergeFailedBody(p MergeFailedPayload) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Branch: %s\n", p.Branch))
	sb.WriteString(fmt.Sprintf("Issue: %s\n", p.Issue))
	sb.WriteString(fmt.Sprintf("Raider: %s\n", p.Raider))
	sb.WriteString(fmt.Sprintf("Warband: %s\n", p.Warband))
	sb.WriteString(fmt.Sprintf("Target: %s\n", p.TargetBranch))
	sb.WriteString(fmt.Sprintf("Failed-At: %s\n", p.FailedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Failure-Type: %s\n", p.FailureType))
	sb.WriteString(fmt.Sprintf("Error: %s\n", p.Error))
	return sb.String()
}

// NewReworkRequestMessage creates a REWORK_REQUEST protocol message.
// Sent by Forge to Witness when a branch needs rebasing due to conflicts.
func NewReworkRequestMessage(warband, raider, branch, issue, targetBranch string, conflictFiles []string) *drums.Message {
	payload := ReworkRequestPayload{
		Branch:        branch,
		Issue:         issue,
		Raider:       raider,
		Warband:           warband,
		RequestedAt:   time.Now(),
		TargetBranch:  targetBranch,
		ConflictFiles: conflictFiles,
		Instructions:  formatRebaseInstructions(targetBranch),
	}

	body := formatReworkRequestBody(payload)

	msg := drums.NewMessage(
		fmt.Sprintf("%s/forge", warband),
		fmt.Sprintf("%s/witness", warband),
		fmt.Sprintf("REWORK_REQUEST %s", raider),
		body,
	)
	msg.Priority = drums.PriorityHigh
	msg.Type = drums.TypeTask

	return msg
}

// formatReworkRequestBody formats the body of a REWORK_REQUEST message.
func formatReworkRequestBody(p ReworkRequestPayload) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Branch: %s\n", p.Branch))
	sb.WriteString(fmt.Sprintf("Issue: %s\n", p.Issue))
	sb.WriteString(fmt.Sprintf("Raider: %s\n", p.Raider))
	sb.WriteString(fmt.Sprintf("Warband: %s\n", p.Warband))
	sb.WriteString(fmt.Sprintf("Target: %s\n", p.TargetBranch))
	sb.WriteString(fmt.Sprintf("Requested-At: %s\n", p.RequestedAt.Format(time.RFC3339)))

	if len(p.ConflictFiles) > 0 {
		sb.WriteString(fmt.Sprintf("Conflict-Files: %s\n", strings.Join(p.ConflictFiles, ", ")))
	}

	sb.WriteString("\n")
	sb.WriteString(p.Instructions)

	return sb.String()
}

// formatRebaseInstructions returns standard rebase instructions.
func formatRebaseInstructions(targetBranch string) string {
	return fmt.Sprintf(`Please rebase your changes onto %s:

  git fetch origin
  git rebase origin/%s
  # Resolve any conflicts
  git push -f

The Forge will retry the merge after rebase is complete.`, targetBranch, targetBranch)
}

// ParseMergeReadyPayload parses a MERGE_READY message body into a payload.
func ParseMergeReadyPayload(body string) *MergeReadyPayload {
	return &MergeReadyPayload{
		Branch:    parseField(body, "Branch"),
		Issue:     parseField(body, "Issue"),
		Raider:   parseField(body, "Raider"),
		Warband:       parseField(body, "Warband"),
		Verified:  parseField(body, "Verified"),
		Timestamp: time.Now(), // Use current time if not parseable
	}
}

// ParseMergedPayload parses a MERGED message body into a payload.
func ParseMergedPayload(body string) *MergedPayload {
	payload := &MergedPayload{
		Branch:       parseField(body, "Branch"),
		Issue:        parseField(body, "Issue"),
		Raider:      parseField(body, "Raider"),
		Warband:          parseField(body, "Warband"),
		TargetBranch: parseField(body, "Target"),
		MergeCommit:  parseField(body, "Merge-Commit"),
	}

	// Parse timestamp
	if ts := parseField(body, "Merged-At"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			payload.MergedAt = t
		}
	}

	return payload
}

// ParseMergeFailedPayload parses a MERGE_FAILED message body into a payload.
func ParseMergeFailedPayload(body string) *MergeFailedPayload {
	payload := &MergeFailedPayload{
		Branch:       parseField(body, "Branch"),
		Issue:        parseField(body, "Issue"),
		Raider:      parseField(body, "Raider"),
		Warband:          parseField(body, "Warband"),
		TargetBranch: parseField(body, "Target"),
		FailureType:  parseField(body, "Failure-Type"),
		Error:        parseField(body, "Error"),
	}

	// Parse timestamp
	if ts := parseField(body, "Failed-At"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			payload.FailedAt = t
		}
	}

	return payload
}

// ParseReworkRequestPayload parses a REWORK_REQUEST message body into a payload.
func ParseReworkRequestPayload(body string) *ReworkRequestPayload {
	payload := &ReworkRequestPayload{
		Branch:       parseField(body, "Branch"),
		Issue:        parseField(body, "Issue"),
		Raider:      parseField(body, "Raider"),
		Warband:          parseField(body, "Warband"),
		TargetBranch: parseField(body, "Target"),
	}

	// Parse timestamp
	if ts := parseField(body, "Requested-At"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			payload.RequestedAt = t
		}
	}

	// Parse conflict files
	if files := parseField(body, "Conflict-Files"); files != "" {
		payload.ConflictFiles = strings.Split(files, ", ")
	}

	return payload
}

// parseField extracts a field value from a key-value body format.
// Format: "Key: value"
func parseField(body, key string) string {
	lines := strings.Split(body, "\n")
	prefix := key + ": "

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}

	return ""
}
