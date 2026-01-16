package protocol

import (
	"fmt"
	"io"
	"os"

	"github.com/deeklead/horde/internal/drums"
)

// DefaultForgeHandler provides the default implementation for Forge protocol handlers.
// It receives MERGE_READY messages from the Witness and acknowledges verified work.
// Note: The Forge now queries relics directly for merge requests (via ReadyWithType).
type DefaultForgeHandler struct {
	// Warband is the name of the warband this forge processes.
	Warband string

	// WorkDir is the working directory for operations.
	WorkDir string

	// Router is used to send drums messages.
	Router *drums.Router

	// Output is where to write status messages.
	Output io.Writer
}

// NewForgeHandler creates a new DefaultForgeHandler.
func NewForgeHandler(warband, workDir string) *DefaultForgeHandler {
	return &DefaultForgeHandler{
		Warband:     warband,
		WorkDir: workDir,
		Router:  drums.NewRouter(workDir),
		Output:  os.Stdout,
	}
}

// SetOutput sets the output writer for status messages.
func (h *DefaultForgeHandler) SetOutput(w io.Writer) {
	h.Output = w
}

// HandleMergeReady handles a MERGE_READY message from Witness.
// When a raider's work is verified and ready, the Forge acknowledges receipt.
//
// NOTE: The merge-request bead is created by `hd done`, so we no longer need
// to add to the mrqueue here. The Forge queries relics directly for ready MRs.
func (h *DefaultForgeHandler) HandleMergeReady(payload *MergeReadyPayload) error {
	_, _ = fmt.Fprintf(h.Output, "[Forge] MERGE_READY received for raider %s\n", payload.Raider)
	_, _ = fmt.Fprintf(h.Output, "  Branch: %s\n", payload.Branch)
	_, _ = fmt.Fprintf(h.Output, "  Issue: %s\n", payload.Issue)
	_, _ = fmt.Fprintf(h.Output, "  Verified: %s\n", payload.Verified)

	// Validate required fields
	if payload.Branch == "" {
		return fmt.Errorf("missing branch in MERGE_READY payload")
	}
	if payload.Raider == "" {
		return fmt.Errorf("missing raider in MERGE_READY payload")
	}

	// The merge-request bead is created by `hd done` with gt:merge-request label.
	// The Forge queries relics directly via ReadyWithType("merge-request").
	// No need to add to mrqueue - that was a duplicate tracking file.
	_, _ = fmt.Fprintf(h.Output, "[Forge] ✓ Work verified - Forge will pick up MR via relics query\n")

	return nil
}

// SendMerged sends a MERGED message to the Witness.
// Called by the Forge after successfully merging a branch.
func (h *DefaultForgeHandler) SendMerged(raider, branch, issue, targetBranch, mergeCommit string) error {
	msg := NewMergedMessage(h.Warband, raider, branch, issue, targetBranch, mergeCommit)
	return h.Router.Send(msg)
}

// SendMergeFailed sends a MERGE_FAILED message to the Witness.
// Called by the Forge when a merge fails.
func (h *DefaultForgeHandler) SendMergeFailed(raider, branch, issue, targetBranch, failureType, errorMsg string) error {
	msg := NewMergeFailedMessage(h.Warband, raider, branch, issue, targetBranch, failureType, errorMsg)
	return h.Router.Send(msg)
}

// SendReworkRequest sends a REWORK_REQUEST message to the Witness.
// Called by the Forge when a branch has conflicts.
func (h *DefaultForgeHandler) SendReworkRequest(raider, branch, issue, targetBranch string, conflictFiles []string) error {
	msg := NewReworkRequestMessage(h.Warband, raider, branch, issue, targetBranch, conflictFiles)
	return h.Router.Send(msg)
}

// NotifyMergeOutcome is a convenience method that sends the appropriate message
// based on the merge result.
type MergeOutcome struct {
	// Success indicates whether the merge was successful.
	Success bool

	// Conflict indicates the failure was due to conflicts (needs rebase).
	Conflict bool

	// FailureType categorizes the failure (e.g., "tests", "build").
	FailureType string

	// Error is the error message if the merge failed.
	Error string

	// MergeCommit is the SHA of the merge commit on success.
	MergeCommit string

	// ConflictFiles lists files with conflicts (if Conflict is true).
	ConflictFiles []string
}

// NotifyMergeOutcome sends the appropriate protocol message based on the outcome.
func (h *DefaultForgeHandler) NotifyMergeOutcome(raider, branch, issue, targetBranch string, outcome MergeOutcome) error {
	if outcome.Success {
		return h.SendMerged(raider, branch, issue, targetBranch, outcome.MergeCommit)
	}

	if outcome.Conflict {
		return h.SendReworkRequest(raider, branch, issue, targetBranch, outcome.ConflictFiles)
	}

	return h.SendMergeFailed(raider, branch, issue, targetBranch, outcome.FailureType, outcome.Error)
}

// Ensure DefaultForgeHandler implements ForgeHandler.
var _ ForgeHandler = (*DefaultForgeHandler)(nil)
