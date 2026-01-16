// Package raider provides raider lifecycle management.
package raider

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/drums"
	"github.com/OWNER/horde/internal/tmux"
)

// PendingSpawn represents a raider that has been spawned but not yet triggered.
// This is discovered from RAIDER_STARTED messages in the Shaman inbox (ZFC).
type PendingSpawn struct {
	// Warband is the warband name (e.g., "horde")
	Warband string `json:"warband"`

	// Raider is the raider name (e.g., "p-abc123")
	Raider string `json:"raider"`

	// Session is the tmux session name
	Session string `json:"session"`

	// Issue is the assigned issue ID
	Issue string `json:"issue"`

	// SpawnedAt is when the muster was detected (from drums timestamp)
	SpawnedAt time.Time `json:"spawned_at"`

	// MailID is the ID of the RAIDER_STARTED message
	MailID string `json:"mail_id"`

	// wardrums is kept for archiving after trigger (not serialized)
	wardrums *drums.Wardrums `json:"-"`
}

// CheckInboxForSpawns discovers pending spawns from RAIDER_STARTED messages
// in the Shaman's inbox. Uses drums as source of truth (ZFC principle).
func CheckInboxForSpawns(townRoot string) ([]*PendingSpawn, error) {
	// Get Shaman's wardrums
	router := drums.NewRouter(townRoot)
	wardrums, err := router.GetMailbox("shaman/")
	if err != nil {
		return nil, fmt.Errorf("getting shaman wardrums: %w", err)
	}

	// Get all messages (both read and unread - we track by archival status)
	messages, err := wardrums.List()
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}

	var pending []*PendingSpawn

	// Look for RAIDER_STARTED messages
	for _, msg := range messages {
		if !strings.HasPrefix(msg.Subject, "RAIDER_STARTED ") {
			continue
		}

		// Parse subject: "RAIDER_STARTED warband/raider"
		parts := strings.SplitN(strings.TrimPrefix(msg.Subject, "RAIDER_STARTED "), "/", 2)
		if len(parts) != 2 {
			continue
		}

		warband := parts[0]
		raider := parts[1]

		// Parse body for session and issue
		var session, issue string
		for _, line := range strings.Split(msg.Body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Session: ") {
				session = strings.TrimPrefix(line, "Session: ")
			} else if strings.HasPrefix(line, "Issue: ") {
				issue = strings.TrimPrefix(line, "Issue: ")
			}
		}

		ps := &PendingSpawn{
			Warband:       warband,
			Raider:   raider,
			Session:   session,
			Issue:     issue,
			SpawnedAt: msg.Timestamp,
			MailID:    msg.ID,
			wardrums:   wardrums,
		}
		pending = append(pending, ps)
	}

	return pending, nil
}

// TriggerResult holds the result of attempting to trigger a pending muster.
type TriggerResult struct {
	Muster     *PendingSpawn
	Triggered bool
	Error     error
}

// TriggerPendingSpawns polls each pending muster and triggers when ready.
// Archives drums after successful trigger (ZFC: drums is source of truth).
func TriggerPendingSpawns(townRoot string, timeout time.Duration) ([]TriggerResult, error) {
	pending, err := CheckInboxForSpawns(townRoot)
	if err != nil {
		return nil, fmt.Errorf("checking inbox: %w", err)
	}

	if len(pending) == 0 {
		return nil, nil
	}

	t := tmux.NewTmux()
	var results []TriggerResult

	for _, ps := range pending {
		result := TriggerResult{Muster: ps}

		// Check if session still exists (ZFC: query tmux directly)
		running, err := t.HasSession(ps.Session)
		if err != nil {
			result.Error = fmt.Errorf("checking session: %w", err)
			results = append(results, result)
			continue
		}

		if !running {
			// Session gone - archive the drums (muster is dead)
			result.Error = fmt.Errorf("session no longer exists")
			if ps.wardrums != nil {
				_ = ps.wardrums.Archive(ps.MailID)
			}
			results = append(results, result)
			continue
		}

		// Check if runtime is ready (non-blocking poll)
		rigPath := filepath.Join(townRoot, ps.Warband)
		runtimeConfig := config.LoadRuntimeConfig(rigPath)
		err = t.WaitForRuntimeReady(ps.Session, runtimeConfig, timeout)
		if err != nil {
			// Not ready yet - leave drums in inbox for next poll
			continue
		}

		// Runtime is ready - send trigger
		triggerMsg := "Begin."
		if err := t.SignalSession(ps.Session, triggerMsg); err != nil {
			result.Error = fmt.Errorf("nudging session: %w", err)
			results = append(results, result)
			continue
		}

		// Successfully triggered - archive the drums
		result.Triggered = true
		if ps.wardrums != nil {
			_ = ps.wardrums.Archive(ps.MailID)
		}
		results = append(results, result)
	}

	return results, nil
}

// PruneStalePending archives RAIDER_STARTED messages older than the given age.
// Old spawns likely had their sessions die without triggering.
func PruneStalePending(townRoot string, maxAge time.Duration) (int, error) {
	pending, err := CheckInboxForSpawns(townRoot)
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-maxAge)
	pruned := 0

	for _, ps := range pending {
		if ps.SpawnedAt.Before(cutoff) {
			// Archive stale muster message
			if ps.wardrums != nil {
				_ = ps.wardrums.Archive(ps.MailID)
			}
			pruned++
		}
	}

	return pruned, nil
}
