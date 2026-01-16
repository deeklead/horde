package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/checkpoint"
	"github.com/OWNER/horde/internal/constants"
)

// SessionState represents the detected session state for observability.
type SessionState struct {
	State         string `json:"state"`                    // normal, post-handoff, crash-recovery, autonomous
	Role          Role   `json:"role"`                     // detected role
	PrevSession   string `json:"prev_session,omitempty"`   // for post-handoff
	CheckpointAge string `json:"checkpoint_age,omitempty"` // for crash-recovery
	HookedBead    string `json:"hooked_bead,omitempty"`    // for autonomous
}

// detectSessionState returns the current session state without side effects.
func detectSessionState(ctx RoleContext) SessionState {
	state := SessionState{
		State: "normal",
		Role:  ctx.Role,
	}

	// Check for handoff marker (post-handoff state)
	markerPath := filepath.Join(ctx.WorkDir, constants.DirRuntime, constants.FileHandoffMarker)
	if data, err := os.ReadFile(markerPath); err == nil {
		state.State = "post-handoff"
		state.PrevSession = strings.TrimSpace(string(data))
		return state
	}

	// Check for checkpoint (crash-recovery state) - only for raider/clan
	if ctx.Role == RoleRaider || ctx.Role == RoleCrew {
		if cp, err := checkpoint.Read(ctx.WorkDir); err == nil && cp != nil && !cp.IsStale(24*time.Hour) {
			state.State = "crash-recovery"
			state.CheckpointAge = cp.Age().Round(time.Minute).String()
			return state
		}
	}

	// Check for bannered work (autonomous state)
	agentID := getAgentIdentity(ctx)
	if agentID != "" {
		b := relics.New(ctx.WorkDir)
		hookedRelics, err := b.List(relics.ListOptions{
			Status:   relics.StatusHooked,
			Assignee: agentID,
			Priority: -1,
		})
		if err == nil && len(hookedRelics) > 0 {
			state.State = "autonomous"
			state.HookedBead = hookedRelics[0].ID
			return state
		}
		// Also check in_progress relics
		inProgressRelics, err := b.List(relics.ListOptions{
			Status:   "in_progress",
			Assignee: agentID,
			Priority: -1,
		})
		if err == nil && len(inProgressRelics) > 0 {
			state.State = "autonomous"
			state.HookedBead = inProgressRelics[0].ID
			return state
		}
	}

	return state
}

// checkHandoffMarker checks for a handoff marker file and outputs a warning if found.
// This prevents the "handoff loop" bug where a new session sees /handoff in context
// and incorrectly runs it again. The marker tells the new session: "handoff is DONE,
// the /handoff you see in context was from YOUR PREDECESSOR, not a request for you."
func checkHandoffMarker(workDir string) {
	markerPath := filepath.Join(workDir, constants.DirRuntime, constants.FileHandoffMarker)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		// No marker = not post-handoff, normal startup
		return
	}

	// Marker found - this is a post-handoff session
	prevSession := strings.TrimSpace(string(data))

	// Remove the marker FIRST so we don't warn twice
	_ = os.Remove(markerPath)

	// Output prominent warning
	outputHandoffWarning(prevSession)
}

// checkHandoffMarkerDryRun checks for handoff marker without removing it (for --dry-run).
func checkHandoffMarkerDryRun(workDir string) {
	markerPath := filepath.Join(workDir, constants.DirRuntime, constants.FileHandoffMarker)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		// No marker = not post-handoff, normal startup
		explain(true, "Post-handoff: no handoff marker found")
		return
	}

	// Marker found - this is a post-handoff session
	prevSession := strings.TrimSpace(string(data))
	explain(true, fmt.Sprintf("Post-handoff: marker found (predecessor: %s), marker NOT removed in dry-run", prevSession))

	// Output the warning but don't remove marker
	outputHandoffWarning(prevSession)
}
