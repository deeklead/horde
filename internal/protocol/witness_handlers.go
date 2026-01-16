package protocol

import (
	"fmt"
	"io"
	"os"

	"github.com/deeklead/horde/internal/drums"
	"github.com/deeklead/horde/internal/witness"
)

// DefaultWitnessHandler provides the default implementation for Witness protocol handlers.
// It receives messages from the Forge about merge outcomes and takes appropriate action.
type DefaultWitnessHandler struct {
	// Warband is the name of the warband this witness manages.
	Warband string

	// WorkDir is the working directory for operations.
	WorkDir string

	// Router is used to send drums messages.
	Router *drums.Router

	// Output is where to write status messages.
	Output io.Writer
}

// NewWitnessHandler creates a new DefaultWitnessHandler.
func NewWitnessHandler(warband, workDir string) *DefaultWitnessHandler {
	return &DefaultWitnessHandler{
		Warband:     warband,
		WorkDir: workDir,
		Router:  drums.NewRouter(workDir),
		Output:  os.Stdout,
	}
}

// SetOutput sets the output writer for status messages.
func (h *DefaultWitnessHandler) SetOutput(w io.Writer) {
	h.Output = w
}

// HandleMerged handles a MERGED message from Forge.
// When a branch is successfully merged, the Witness:
// 1. Logs the success
// 2. Notifies the raider of successful merge
// 3. Initiates raider cleanup (nuke worktree)
func (h *DefaultWitnessHandler) HandleMerged(payload *MergedPayload) error {
	_, _ = fmt.Fprintf(h.Output, "[Witness] MERGED received for raider %s\n", payload.Raider)
	_, _ = fmt.Fprintf(h.Output, "  Branch: %s\n", payload.Branch)
	_, _ = fmt.Fprintf(h.Output, "  Issue: %s\n", payload.Issue)
	_, _ = fmt.Fprintf(h.Output, "  Merged to: %s\n", payload.TargetBranch)
	if payload.MergeCommit != "" {
		_, _ = fmt.Fprintf(h.Output, "  Commit: %s\n", payload.MergeCommit)
	}

	// Notify the raider about successful merge
	if err := h.notifyRaiderMerged(payload); err != nil {
		fmt.Fprintf(h.Output, "[Witness] Warning: failed to notify raider: %v\n", err)
		// Continue - notification is best-effort
	}

	// Initiate raider cleanup using AutoNukeIfClean
	// This verifies cleanup_status before nuking to prevent work loss.
	nukeResult := witness.AutoNukeIfClean(h.WorkDir, h.Warband, payload.Raider)
	if nukeResult.Nuked {
		fmt.Fprintf(h.Output, "[Witness] ✓ Auto-nuked raider %s: %s\n", payload.Raider, nukeResult.Reason)
	} else if nukeResult.Skipped {
		fmt.Fprintf(h.Output, "[Witness] ⚠ Cleanup skipped for %s: %s\n", payload.Raider, nukeResult.Reason)
	} else if nukeResult.Error != nil {
		fmt.Fprintf(h.Output, "[Witness] ✗ Cleanup failed for %s: %v\n", payload.Raider, nukeResult.Error)
	} else {
		fmt.Fprintf(h.Output, "[Witness] ✓ Raider %s work merged, cleanup can proceed\n", payload.Raider)
	}

	return nil
}

// HandleMergeFailed handles a MERGE_FAILED message from Forge.
// When a merge fails (tests, build, etc.), the Witness:
// 1. Logs the failure
// 2. Notifies the raider about the failure and required fixes
// 3. Updates the raider's state to indicate rework needed
func (h *DefaultWitnessHandler) HandleMergeFailed(payload *MergeFailedPayload) error {
	fmt.Fprintf(h.Output, "[Witness] MERGE_FAILED received for raider %s\n", payload.Raider)
	fmt.Fprintf(h.Output, "  Branch: %s\n", payload.Branch)
	fmt.Fprintf(h.Output, "  Issue: %s\n", payload.Issue)
	fmt.Fprintf(h.Output, "  Failure type: %s\n", payload.FailureType)
	fmt.Fprintf(h.Output, "  Error: %s\n", payload.Error)

	// Notify the raider about the failure
	if err := h.notifyRaiderFailed(payload); err != nil {
		fmt.Fprintf(h.Output, "[Witness] Warning: failed to notify raider: %v\n", err)
		// Continue - notification is best-effort
	}

	fmt.Fprintf(h.Output, "[Witness] ✗ Raider %s merge failed, rework needed\n", payload.Raider)

	return nil
}

// HandleReworkRequest handles a REWORK_REQUEST message from Forge.
// When a branch has conflicts requiring rebase, the Witness:
// 1. Logs the conflict
// 2. Notifies the raider with rebase instructions
// 3. Updates the raider's state to indicate rebase needed
func (h *DefaultWitnessHandler) HandleReworkRequest(payload *ReworkRequestPayload) error {
	fmt.Fprintf(h.Output, "[Witness] REWORK_REQUEST received for raider %s\n", payload.Raider)
	fmt.Fprintf(h.Output, "  Branch: %s\n", payload.Branch)
	fmt.Fprintf(h.Output, "  Issue: %s\n", payload.Issue)
	fmt.Fprintf(h.Output, "  Target: %s\n", payload.TargetBranch)
	if len(payload.ConflictFiles) > 0 {
		fmt.Fprintf(h.Output, "  Conflicts in: %v\n", payload.ConflictFiles)
	}

	// Notify the raider about the rebase requirement
	if err := h.notifyRaiderRebase(payload); err != nil {
		fmt.Fprintf(h.Output, "[Witness] Warning: failed to notify raider: %v\n", err)
		// Continue - notification is best-effort
	}

	fmt.Fprintf(h.Output, "[Witness] ⚠ Raider %s needs to rebase onto %s\n", payload.Raider, payload.TargetBranch)

	return nil
}

// notifyRaiderMerged sends a merge success notification to a raider.
func (h *DefaultWitnessHandler) notifyRaiderMerged(payload *MergedPayload) error {
	msg := drums.NewMessage(
		fmt.Sprintf("%s/witness", h.Warband),
		fmt.Sprintf("%s/%s", h.Warband, payload.Raider),
		"Work merged successfully",
		fmt.Sprintf(`Your work has been merged to %s.

Branch: %s
Issue: %s
Commit: %s

Thank you for your contribution! Your worktree will be cleaned up shortly.`,
			payload.TargetBranch,
			payload.Branch,
			payload.Issue,
			payload.MergeCommit,
		),
	)
	msg.Priority = drums.PriorityNormal

	return h.Router.Send(msg)
}

// notifyRaiderFailed sends a merge failure notification to a raider.
func (h *DefaultWitnessHandler) notifyRaiderFailed(payload *MergeFailedPayload) error {
	msg := drums.NewMessage(
		fmt.Sprintf("%s/witness", h.Warband),
		fmt.Sprintf("%s/%s", h.Warband, payload.Raider),
		fmt.Sprintf("Merge failed: %s", payload.FailureType),
		fmt.Sprintf(`Your merge request failed.

Branch: %s
Issue: %s
Failure: %s
Error: %s

Please fix the issue and resubmit your work with 'hd done'.`,
			payload.Branch,
			payload.Issue,
			payload.FailureType,
			payload.Error,
		),
	)
	msg.Priority = drums.PriorityHigh
	msg.Type = drums.TypeTask

	return h.Router.Send(msg)
}

// notifyRaiderRebase sends a rebase request notification to a raider.
func (h *DefaultWitnessHandler) notifyRaiderRebase(payload *ReworkRequestPayload) error {
	conflictInfo := ""
	if len(payload.ConflictFiles) > 0 {
		conflictInfo = fmt.Sprintf("\nConflicting files:\n")
		for _, f := range payload.ConflictFiles {
			conflictInfo += fmt.Sprintf("  - %s\n", f)
		}
	}

	msg := drums.NewMessage(
		fmt.Sprintf("%s/witness", h.Warband),
		fmt.Sprintf("%s/%s", h.Warband, payload.Raider),
		"Rebase required - merge conflict",
		fmt.Sprintf(`Your branch has conflicts with %s.

Branch: %s
Issue: %s
%s
Please rebase your changes:

  git fetch origin
  git rebase origin/%s
  # Resolve any conflicts
  git push -f

Then run 'hd done' to resubmit for merge.`,
			payload.TargetBranch,
			payload.Branch,
			payload.Issue,
			conflictInfo,
			payload.TargetBranch,
		),
	)
	msg.Priority = drums.PriorityHigh
	msg.Type = drums.TypeTask

	return h.Router.Send(msg)
}

// Ensure DefaultWitnessHandler implements WitnessHandler.
var _ WitnessHandler = (*DefaultWitnessHandler)(nil)
