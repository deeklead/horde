package witness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/drums"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/tmux"
	"github.com/deeklead/horde/internal/util"
	"github.com/deeklead/horde/internal/workspace"
)

// HandlerResult tracks the result of handling a protocol message.
type HandlerResult struct {
	MessageID    string
	ProtocolType ProtocolType
	Handled      bool
	Action       string
	WispCreated  string // ID of created wisp (if any)
	MailSent     string // ID of sent drums (if any)
	Error        error
}

// HandleRaiderDone processes a RAIDER_DONE message from a raider.
// For ESCALATED/DEFERRED exits (no pending MR), auto-nukes if clean.
// For PHASE_COMPLETE exits, recycles the raider (session ends, worktree kept).
// For COMPLETED exits with MR and clean state, auto-nukes immediately (ephemeral model).
// For exits with pending MR but dirty state, creates cleanup wisp for manual intervention.
//
// Ephemeral Raider Model:
// Raiders are truly ephemeral - done at MR submission, recyclable immediately.
// Once the branch is pushed (cleanup_status=clean), the raider can be nuked.
// The MR lifecycle continues independently in the Forge.
// If conflicts arise, Forge creates a NEW conflict-resolution task for a NEW raider.
func HandleRaiderDone(workDir, rigName string, msg *drums.Message) *HandlerResult {
	result := &HandlerResult{
		MessageID:    msg.ID,
		ProtocolType: ProtoRaiderDone,
	}

	// Parse the message
	payload, err := ParseRaiderDone(msg.Subject, msg.Body)
	if err != nil {
		result.Error = fmt.Errorf("parsing RAIDER_DONE: %w", err)
		return result
	}

	// Handle PHASE_COMPLETE: recycle raider (session ends but worktree stays)
	// The raider is registered as a waiter on the gate and will be re-dispatched
	// when the gate closes via hd gate wake.
	if payload.Exit == "PHASE_COMPLETE" {
		result.Handled = true
		result.Action = fmt.Sprintf("phase-complete for %s (gate=%s) - session recycled, awaiting gate", payload.RaiderName, payload.Gate)
		// Note: The raider has already registered itself as a gate waiter via bd
		// The gate wake mechanism (hd gate wake) will send drums when gate closes
		// A new raider will be dispatched to continue the totem from the next step
		return result
	}

	// Check if this raider has a pending MR
	// ESCALATED/DEFERRED exits typically have no MR pending
	hasPendingMR := payload.MRID != "" || payload.Exit == "COMPLETED"

	// Local-only branches model: if there's a pending MR, DON'T nuke.
	// The raider's local branch is needed for conflict resolution if merge fails.
	// Once the MR merges (MERGED signal), HandleMerged will nuke the raider.
	if hasPendingMR {
		// Create cleanup wisp to track this raider is waiting for merge
		wispID, err := createCleanupWisp(workDir, payload.RaiderName, payload.IssueID, payload.Branch)
		if err != nil {
			result.Error = fmt.Errorf("creating cleanup wisp: %w", err)
			return result
		}

		// Update wisp state to indicate it's waiting for merge
		if err := UpdateCleanupWispState(workDir, wispID, "merge-requested"); err != nil {
			// Non-fatal - wisp was created, just couldn't update state
			result.Error = fmt.Errorf("updating wisp state: %w", err)
		}

		result.Handled = true
		result.WispCreated = wispID
		result.Action = fmt.Sprintf("deferred cleanup for %s (pending MR=%s, local branch preserved for conflict resolution)", payload.RaiderName, payload.MRID)
		return result
	}

	// No pending MR - try to auto-nuke immediately
	nukeResult := AutoNukeIfClean(workDir, rigName, payload.RaiderName)
	if nukeResult.Nuked {
		result.Handled = true
		result.Action = fmt.Sprintf("auto-nuked %s (exit=%s, no MR): %s", payload.RaiderName, payload.Exit, nukeResult.Reason)
		return result
	}
	if nukeResult.Error != nil {
		// Nuke failed - fall through to create wisp for manual cleanup
		result.Error = nukeResult.Error
	}

	// Couldn't auto-nuke (dirty state or verification failed) - create wisp for manual intervention
	wispID, err := createCleanupWisp(workDir, payload.RaiderName, payload.IssueID, payload.Branch)
	if err != nil {
		result.Error = fmt.Errorf("creating cleanup wisp: %w", err)
		return result
	}

	result.Handled = true
	result.WispCreated = wispID
	result.Action = fmt.Sprintf("created cleanup wisp %s for %s (needs manual cleanup: %s)", wispID, payload.RaiderName, nukeResult.Reason)

	return result
}

// HandleLifecycleShutdown processes a LIFECYCLE:Shutdown message.
// Similar to RAIDER_DONE but triggered by daemon rather than raider.
// Auto-nukes if clean since shutdown means no pending work.
func HandleLifecycleShutdown(workDir, rigName string, msg *drums.Message) *HandlerResult {
	result := &HandlerResult{
		MessageID:    msg.ID,
		ProtocolType: ProtoLifecycleShutdown,
	}

	// Extract raider name from subject
	matches := PatternLifecycleShutdown.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		result.Error = fmt.Errorf("invalid LIFECYCLE:Shutdown subject: %s", msg.Subject)
		return result
	}
	raiderName := matches[1]

	// Shutdown means no pending work - try to auto-nuke immediately
	nukeResult := AutoNukeIfClean(workDir, rigName, raiderName)
	if nukeResult.Nuked {
		result.Handled = true
		result.Action = fmt.Sprintf("auto-nuked %s (shutdown): %s", raiderName, nukeResult.Reason)
		return result
	}
	if nukeResult.Error != nil {
		// Nuke failed - fall through to create wisp
		result.Error = nukeResult.Error
	}

	// Couldn't auto-nuke - create a cleanup wisp for manual intervention
	wispID, err := createCleanupWisp(workDir, raiderName, "", "")
	if err != nil {
		result.Error = fmt.Errorf("creating cleanup wisp: %w", err)
		return result
	}

	result.Handled = true
	result.WispCreated = wispID
	result.Action = fmt.Sprintf("created cleanup wisp %s for shutdown %s (needs manual cleanup)", wispID, raiderName)

	return result
}

// HandleHelp processes a HELP message from a raider requesting intervention.
// Assesses the request and either helps directly or escalates to Warchief.
func HandleHelp(workDir, rigName string, msg *drums.Message, router *drums.Router) *HandlerResult {
	result := &HandlerResult{
		MessageID:    msg.ID,
		ProtocolType: ProtoHelp,
	}

	// Parse the message
	payload, err := ParseHelp(msg.Subject, msg.Body)
	if err != nil {
		result.Error = fmt.Errorf("parsing HELP: %w", err)
		return result
	}

	// Assess the help request
	assessment := AssessHelpRequest(payload)

	if assessment.CanHelp {
		// Log that we can help - actual help is done by the Claude agent
		result.Handled = true
		result.Action = fmt.Sprintf("can help with '%s': %s", payload.Topic, assessment.HelpAction)
		return result
	}

	// Need to escalate to Warchief
	if assessment.NeedsEscalation {
		mailID, err := escalateToWarchief(router, rigName, payload, assessment.EscalationReason)
		if err != nil {
			result.Error = fmt.Errorf("escalating to warchief: %w", err)
			return result
		}

		result.Handled = true
		result.MailSent = mailID
		result.Action = fmt.Sprintf("escalated '%s' to warchief: %s", payload.Topic, assessment.EscalationReason)
	}

	return result
}

// HandleMerged processes a MERGED message from the Forge.
// Verifies cleanup_status before allowing nuke, escalates if work is at risk.
func HandleMerged(workDir, rigName string, msg *drums.Message) *HandlerResult {
	result := &HandlerResult{
		MessageID:    msg.ID,
		ProtocolType: ProtoMerged,
	}

	// Parse the message
	payload, err := ParseMerged(msg.Subject, msg.Body)
	if err != nil {
		result.Error = fmt.Errorf("parsing MERGED: %w", err)
		return result
	}

	// Find the cleanup wisp for this raider
	wispID, err := findCleanupWisp(workDir, payload.RaiderName)
	if err != nil {
		result.Error = fmt.Errorf("finding cleanup wisp: %w", err)
		return result
	}

	if wispID == "" {
		// No wisp found - raider may have been cleaned up already
		result.Handled = true
		result.Action = fmt.Sprintf("no cleanup wisp found for %s (may be already cleaned)", payload.RaiderName)
		return result
	}

	// Verify the raider's commit is actually on main before allowing nuke.
	// This prevents work loss when MERGED signal is for a stale MR or the merge failed.
	onMain, err := verifyCommitOnMain(workDir, rigName, payload.RaiderName)
	if err != nil {
		// Couldn't verify - log warning but continue with other checks
		// The raider may not exist anymore (already nuked) which is fine
		result.Action = fmt.Sprintf("warning: couldn't verify commit on main for %s: %v", payload.RaiderName, err)
	} else if !onMain {
		// Commit is NOT on main - don't nuke!
		result.Handled = true
		result.WispCreated = wispID
		result.Error = fmt.Errorf("raider %s commit is NOT on main - MERGED signal may be stale, DO NOT NUKE", payload.RaiderName)
		result.Action = fmt.Sprintf("BLOCKED: %s commit not verified on main, merge may have failed", payload.RaiderName)
		return result
	}

	// ZFC #10: Check cleanup_status before allowing nuke
	// This prevents work loss when MERGED signal arrives for stale MRs or
	// when raider has new unpushed work since the MR was created.
	cleanupStatus := getCleanupStatus(workDir, rigName, payload.RaiderName)

	switch cleanupStatus {
	case "clean":
		// Safe to nuke - raider has confirmed clean state
		// Execute the nuke immediately
		if err := NukeRaider(workDir, rigName, payload.RaiderName); err != nil {
			result.Handled = true
			result.WispCreated = wispID
			result.Error = fmt.Errorf("nuke failed for %s: %w", payload.RaiderName, err)
			result.Action = fmt.Sprintf("cleanup wisp %s for %s: nuke FAILED", wispID, payload.RaiderName)
		} else {
			result.Handled = true
			result.WispCreated = wispID
			result.Action = fmt.Sprintf("auto-nuked %s (cleanup_status=clean, wisp=%s)", payload.RaiderName, wispID)
		}

	case "has_uncommitted":
		// Has uncommitted changes - might be WIP, escalate to Warchief
		result.Handled = true
		result.WispCreated = wispID
		result.Error = fmt.Errorf("raider %s has uncommitted changes - escalate to Warchief before nuke", payload.RaiderName)
		result.Action = fmt.Sprintf("BLOCKED: %s has uncommitted work, needs escalation", payload.RaiderName)

	case "has_stash":
		// Has stashed work - definitely needs review
		result.Handled = true
		result.WispCreated = wispID
		result.Error = fmt.Errorf("raider %s has stashed work - escalate to Warchief before nuke", payload.RaiderName)
		result.Action = fmt.Sprintf("BLOCKED: %s has stashed work, needs escalation", payload.RaiderName)

	case "has_unpushed":
		// Critical: has unpushed commits that could be lost
		result.Handled = true
		result.WispCreated = wispID
		result.Error = fmt.Errorf("raider %s has unpushed commits - DO NOT NUKE, escalate to Warchief", payload.RaiderName)
		result.Action = fmt.Sprintf("BLOCKED: %s has unpushed commits, DO NOT NUKE", payload.RaiderName)

	default:
		// Unknown or no status - we already verified commit is on main above
		// Safe to nuke since verification passed
		if err := NukeRaider(workDir, rigName, payload.RaiderName); err != nil {
			result.Handled = true
			result.WispCreated = wispID
			result.Error = fmt.Errorf("nuke failed for %s: %w", payload.RaiderName, err)
			result.Action = fmt.Sprintf("cleanup wisp %s for %s: nuke FAILED", wispID, payload.RaiderName)
		} else {
			result.Handled = true
			result.WispCreated = wispID
			result.Action = fmt.Sprintf("auto-nuked %s (commit on main, cleanup_status=%s, wisp=%s)", payload.RaiderName, cleanupStatus, wispID)
		}
	}

	return result
}

// HandleMergeFailed processes a MERGE_FAILED message from the Forge.
// Notifies the raider that their merge was rejected and rework is needed.
func HandleMergeFailed(workDir, rigName string, msg *drums.Message, router *drums.Router) *HandlerResult {
	result := &HandlerResult{
		MessageID:    msg.ID,
		ProtocolType: ProtoMergeFailed,
	}

	// Parse the message
	payload, err := ParseMergeFailed(msg.Subject, msg.Body)
	if err != nil {
		result.Error = fmt.Errorf("parsing MERGE_FAILED: %w", err)
		return result
	}

	// Notify the raider about the failure
	raiderAddr := fmt.Sprintf("%s/raiders/%s", rigName, payload.RaiderName)
	notification := &drums.Message{
		From:     fmt.Sprintf("%s/witness", rigName),
		To:       raiderAddr,
		Subject:  fmt.Sprintf("Merge failed: %s", payload.FailureType),
		Priority: drums.PriorityHigh,
		Type:     drums.TypeTask,
		Body: fmt.Sprintf(`Your merge request was rejected.

Branch: %s
Issue: %s
Failure: %s
Error: %s

Please fix the issue and resubmit with 'hd done'.`,
			payload.Branch,
			payload.IssueID,
			payload.FailureType,
			payload.Error,
		),
	}

	if err := router.Send(notification); err != nil {
		result.Error = fmt.Errorf("sending failure notification: %w", err)
		return result
	}

	result.Handled = true
	result.MailSent = notification.ID
	result.Action = fmt.Sprintf("notified %s of merge failure: %s - %s", payload.RaiderName, payload.FailureType, payload.Error)

	return result
}

// HandleSwarmStart processes a SWARM_START message from the Warchief.
// Creates a swarm tracking wisp to monitor batch raider work.
func HandleSwarmStart(workDir string, msg *drums.Message) *HandlerResult {
	result := &HandlerResult{
		MessageID:    msg.ID,
		ProtocolType: ProtoSwarmStart,
	}

	// Parse the message
	payload, err := ParseSwarmStart(msg.Body)
	if err != nil {
		result.Error = fmt.Errorf("parsing SWARM_START: %w", err)
		return result
	}

	// Create a swarm tracking wisp
	wispID, err := createSwarmWisp(workDir, payload)
	if err != nil {
		result.Error = fmt.Errorf("creating swarm wisp: %w", err)
		return result
	}

	result.Handled = true
	result.WispCreated = wispID
	result.Action = fmt.Sprintf("created swarm tracking wisp %s for %s", wispID, payload.SwarmID)

	return result
}

// createCleanupWisp creates a wisp to track raider cleanup.
func createCleanupWisp(workDir, raiderName, issueID, branch string) (string, error) {
	title := fmt.Sprintf("cleanup:%s", raiderName)
	description := fmt.Sprintf("Verify and cleanup raider %s", raiderName)
	if issueID != "" {
		description += fmt.Sprintf("\nIssue: %s", issueID)
	}
	if branch != "" {
		description += fmt.Sprintf("\nBranch: %s", branch)
	}

	labels := strings.Join(CleanupWispLabels(raiderName, "pending"), ",")

	output, err := util.ExecWithOutput(workDir, "rl", "create",
		"--ephemeral",
		"--title", title,
		"--description", description,
		"--labels", labels,
	)
	if err != nil {
		return "", err
	}

	// Extract wisp ID from output (bd create outputs "Created: <id>")
	if strings.HasPrefix(output, "Created:") {
		return strings.TrimSpace(strings.TrimPrefix(output, "Created:")), nil
	}

	// Try to extract ID from output
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		// Look for bead ID pattern (e.g., "hd-abc123")
		if strings.Contains(line, "-") && len(line) < 20 {
			return line, nil
		}
	}

	return output, nil
}

// createSwarmWisp creates a wisp to track swarm (batch) work.
func createSwarmWisp(workDir string, payload *SwarmStartPayload) (string, error) {
	title := fmt.Sprintf("swarm:%s", payload.SwarmID)
	description := fmt.Sprintf("Tracking batch: %s\nTotal: %d raiders", payload.SwarmID, payload.Total)

	labels := strings.Join(SwarmWispLabels(payload.SwarmID, payload.Total, 0, payload.StartedAt), ",")

	output, err := util.ExecWithOutput(workDir, "rl", "create",
		"--ephemeral",
		"--title", title,
		"--description", description,
		"--labels", labels,
	)
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(output, "Created:") {
		return strings.TrimSpace(strings.TrimPrefix(output, "Created:")), nil
	}

	return output, nil
}

// findCleanupWisp finds an existing cleanup wisp for a raider.
func findCleanupWisp(workDir, raiderName string) (string, error) {
	output, err := util.ExecWithOutput(workDir, "rl", "list",
		"--ephemeral",
		"--labels", fmt.Sprintf("raider:%s,state:merge-requested", raiderName),
		"--status", "open",
		"--json",
	)
	if err != nil {
		// Empty result is fine
		if strings.Contains(err.Error(), "no issues found") {
			return "", nil
		}
		return "", err
	}

	// Parse JSON to get the wisp ID
	if output == "" || output == "[]" || output == "null" {
		return "", nil
	}

	// Simple extraction - look for "id" field
	// Full JSON parsing would add dependency on encoding/json
	if idx := strings.Index(output, `"id":`); idx >= 0 {
		rest := output[idx+5:]
		rest = strings.TrimLeft(rest, ` "`)
		if endIdx := strings.IndexAny(rest, `",}`); endIdx > 0 {
			return rest[:endIdx], nil
		}
	}

	return "", nil
}

// agentBeadResponse is used to parse the rl show --json response for agent relics.
type agentBeadResponse struct {
	Description string `json:"description"`
}

// getCleanupStatus retrieves the cleanup_status from a raider's agent bead.
// Returns the status string: "clean", "has_uncommitted", "has_stash", "has_unpushed"
// Returns empty string if agent bead doesn't exist or has no cleanup_status.
//
// ZFC #10: This enables the Witness to verify it's safe to nuke before proceeding.
// The raider self-reports its git state when running `hd done`, and we trust that report.
func getCleanupStatus(workDir, rigName, raiderName string) string {
	// Construct agent bead ID using the warband's configured prefix
	// This supports non-hd prefixes like "bd-" for the relics warband
	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		// Fall back to default prefix
		townRoot = workDir
	}
	prefix := relics.GetPrefixForRig(townRoot, rigName)
	agentBeadID := relics.RaiderBeadIDWithPrefix(prefix, rigName, raiderName)

	output, err := util.ExecWithOutput(workDir, "rl", "show", agentBeadID, "--json")
	if err != nil {
		// Agent bead doesn't exist or rl failed - return empty (unknown status)
		return ""
	}

	if output == "" {
		return ""
	}

	// Parse the JSON response
	var resp agentBeadResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		return ""
	}

	// Parse cleanup_status from description
	// Description format has "cleanup_status: <value>" line
	for _, line := range strings.Split(resp.Description, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "cleanup_status:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "cleanup_status:"))
			value = strings.TrimSpace(strings.TrimPrefix(value, "Cleanup_status:"))
			if value != "" && value != "null" {
				return value
			}
		}
	}

	return ""
}

// escalateToWarchief sends an escalation drums to the Warchief.
func escalateToWarchief(router *drums.Router, rigName string, payload *HelpPayload, reason string) (string, error) {
	msg := &drums.Message{
		From:     fmt.Sprintf("%s/witness", rigName),
		To:       "warchief/",
		Subject:  fmt.Sprintf("Escalation: %s needs help", payload.Agent),
		Priority: drums.PriorityHigh,
		Body: fmt.Sprintf(`Agent: %s
Issue: %s
Topic: %s
Problem: %s
Tried: %s
Escalation reason: %s
Requested at: %s`,
			payload.Agent,
			payload.IssueID,
			payload.Topic,
			payload.Problem,
			payload.Tried,
			reason,
			payload.RequestedAt.Format(time.RFC3339),
		),
	}

	if err := router.Send(msg); err != nil {
		return "", err
	}

	return msg.ID, nil
}

// RecoveryPayload contains data for RECOVERY_NEEDED escalation.
type RecoveryPayload struct {
	RaiderName   string
	Warband           string
	CleanupStatus string
	Branch        string
	IssueID       string
	DetectedAt    time.Time
}

// EscalateRecoveryNeeded sends a RECOVERY_NEEDED escalation to the Warchief.
// This is used when a dormant raider has unpushed work that needs recovery
// before cleanup. The Warchief should coordinate recovery (e.g., push the branch,
// save the work) before authorizing cleanup.
func EscalateRecoveryNeeded(router *drums.Router, rigName string, payload *RecoveryPayload) (string, error) {
	msg := &drums.Message{
		From:     fmt.Sprintf("%s/witness", rigName),
		To:       "warchief/",
		Subject:  fmt.Sprintf("RECOVERY_NEEDED %s/%s", rigName, payload.RaiderName),
		Priority: drums.PriorityUrgent,
		Body: fmt.Sprintf(`Raider: %s/%s
Cleanup Status: %s
Branch: %s
Issue: %s
Detected: %s

This raider has unpushed/uncommitted work that will be lost if nuked.
Please coordinate recovery before authorizing cleanup:
1. Check if branch can be pushed to origin
2. Review uncommitted changes for value
3. Either recover the work or authorize force-nuke

DO NOT nuke without --force after recovery.`,
			rigName,
			payload.RaiderName,
			payload.CleanupStatus,
			payload.Branch,
			payload.IssueID,
			payload.DetectedAt.Format(time.RFC3339),
		),
	}

	if err := router.Send(msg); err != nil {
		return "", err
	}

	return msg.ID, nil
}

// UpdateCleanupWispState updates a cleanup wisp's state label.
func UpdateCleanupWispState(workDir, wispID, newState string) error {
	// Get current labels to preserve other labels
	output, err := util.ExecWithOutput(workDir, "rl", "show", wispID, "--json")
	if err != nil {
		return fmt.Errorf("getting wisp: %w", err)
	}

	// Extract raider name from existing labels for the update
	var raiderName string
	if idx := strings.Index(output, `raider:`); idx >= 0 {
		rest := output[idx+8:]
		if endIdx := strings.IndexAny(rest, `",]}`); endIdx > 0 {
			raiderName = rest[:endIdx]
		}
	}

	if raiderName == "" {
		raiderName = "unknown"
	}

	// Update with new state
	newLabels := strings.Join(CleanupWispLabels(raiderName, newState), ",")

	return util.ExecRun(workDir, "rl", "update", wispID, "--labels", newLabels)
}

// NukeRaider executes the actual nuke operation for a raider.
// This kills the tmux session, removes the worktree, and cleans up relics.
// Should only be called after all safety checks pass.
func NukeRaider(workDir, rigName, raiderName string) error {
	// CRITICAL: Kill the tmux session FIRST and unconditionally.
	// The session name follows the pattern gt-<warband>-<raider>.
	// We do this explicitly here because hd raider nuke may fail to kill the
	// session due to warband loading issues or race conditions with IsRunning checks.
	// See: gt-g9ft5 - sessions were piling up because nuke wasn't killing them.
	sessionName := fmt.Sprintf("hd-%s-%s", rigName, raiderName)
	t := tmux.NewTmux()

	// Check if session exists and kill it
	if running, _ := t.HasSession(sessionName); running {
		// Try graceful shutdown first (Ctrl-C), then force kill
		_ = t.SendKeysRaw(sessionName, "C-c")
		// Brief delay for graceful handling
		time.Sleep(100 * time.Millisecond)
		// Force kill the session
		if err := t.KillSession(sessionName); err != nil {
			// Log but continue - session might already be dead
			// The important thing is we tried
		}
	}

	// Now run hd raider nuke to clean up worktree, branch, and relics
	address := fmt.Sprintf("%s/%s", rigName, raiderName)

	if err := util.ExecRun(workDir, "hd", "raider", "nuke", address); err != nil {
		return fmt.Errorf("nuke failed: %w", err)
	}

	return nil
}

// NukeRaiderResult contains the result of an auto-nuke attempt.
type NukeRaiderResult struct {
	Nuked   bool
	Skipped bool
	Reason  string
	Error   error
}

// AutoNukeIfClean checks if a raider is safe to nuke and nukes it if so.
// This is used for orphaned raiders (no bannered work, no pending MR).
// With the self-cleaning model, raiders should self-nuke on completion.
// An orphan is likely from a crash before hd done completed.
// Returns whether the nuke was performed and any error.
func AutoNukeIfClean(workDir, rigName, raiderName string) *NukeRaiderResult {
	result := &NukeRaiderResult{}

	// Check cleanup_status from agent bead
	cleanupStatus := getCleanupStatus(workDir, rigName, raiderName)

	switch cleanupStatus {
	case "clean":
		// Safe to nuke
		if err := NukeRaider(workDir, rigName, raiderName); err != nil {
			result.Error = err
			result.Reason = fmt.Sprintf("nuke failed: %v", err)
		} else {
			result.Nuked = true
			result.Reason = "auto-nuked (cleanup_status=clean, no MR)"
		}

	case "has_uncommitted", "has_stash", "has_unpushed":
		// Not safe - has work that could be lost
		result.Skipped = true
		result.Reason = fmt.Sprintf("skipped: has %s", strings.TrimPrefix(cleanupStatus, "has_"))

	default:
		// Unknown status - check git state directly as fallback
		onMain, err := verifyCommitOnMain(workDir, rigName, raiderName)
		if err != nil {
			// Can't verify - skip (raider may not exist)
			result.Skipped = true
			result.Reason = fmt.Sprintf("skipped: couldn't verify git state: %v", err)
		} else if onMain {
			// Commit is on main, likely safe
			if err := NukeRaider(workDir, rigName, raiderName); err != nil {
				result.Error = err
				result.Reason = fmt.Sprintf("nuke failed: %v", err)
			} else {
				result.Nuked = true
				result.Reason = "auto-nuked (commit on main, no cleanup_status)"
			}
		} else {
			// Not on main - skip, might have unpushed work
			result.Skipped = true
			result.Reason = "skipped: commit not on main, may have unpushed work"
		}
	}

	return result
}

// verifyCommitOnMain checks if the raider's current commit is on the default branch.
// This prevents nuking a raider whose work wasn't actually merged.
//
// In multi-remote setups, the code may live on a remote other than "origin"
// (e.g., "horde" for horde.git). This function checks ALL remotes to find
// the one containing the default branch with the merged commit.
//
// Returns:
//   - true, nil: commit is verified on default branch
//   - false, nil: commit is NOT on default branch (don't nuke!)
//   - false, error: couldn't verify (treat as unsafe)
func verifyCommitOnMain(workDir, rigName, raiderName string) (bool, error) {
	// Find encampment root from workDir
	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		return false, fmt.Errorf("finding encampment root: %v", err)
	}

	// Get configured default branch for this warband
	defaultBranch := "main" // fallback
	if rigCfg, err := warband.LoadRigConfig(filepath.Join(townRoot, rigName)); err == nil && rigCfg.DefaultBranch != "" {
		defaultBranch = rigCfg.DefaultBranch
	}

	// Construct raider path, handling both new and old structures
	// New structure: raiders/<name>/<rigname>/
	// Old structure: raiders/<name>/
	raiderPath := filepath.Join(townRoot, rigName, "raiders", raiderName, rigName)
	if _, err := os.Stat(raiderPath); os.IsNotExist(err) {
		// Fall back to old structure
		raiderPath = filepath.Join(townRoot, rigName, "raiders", raiderName)
	}

	// Get git for the raider worktree
	g := git.NewGit(raiderPath)

	// Get the current HEAD commit SHA
	commitSHA, err := g.Rev("HEAD")
	if err != nil {
		return false, fmt.Errorf("getting raider HEAD: %w", err)
	}

	// Get all configured remotes and check each one for the commit
	// This handles multi-remote setups where code may be on a remote other than "origin"
	remotes, err := g.Remotes()
	if err != nil {
		// If we can't list remotes, fall back to checking just the local branch
		isOnDefaultBranch, err := g.IsAncestor(commitSHA, defaultBranch)
		if err != nil {
			return false, fmt.Errorf("checking if commit is on %s: %w", defaultBranch, err)
		}
		return isOnDefaultBranch, nil
	}

	// Try each remote/<defaultBranch> until we find one where commit is an ancestor
	for _, remote := range remotes {
		remoteBranch := remote + "/" + defaultBranch
		isOnRemote, err := g.IsAncestor(commitSHA, remoteBranch)
		if err == nil && isOnRemote {
			return true, nil
		}
	}

	// Also try the local default branch (in case we're not tracking a remote)
	isOnDefaultBranch, err := g.IsAncestor(commitSHA, defaultBranch)
	if err == nil && isOnDefaultBranch {
		return true, nil
	}

	// Commit is not on any remote's default branch
	return false, nil
}
