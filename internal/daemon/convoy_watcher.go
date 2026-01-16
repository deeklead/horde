package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RaidWatcher monitors rl activity for issue closes and triggers raid completion checks.
// When an issue closes, it checks if the issue is tracked by any raid and runs the
// completion check if all tracked issues are now closed.
type RaidWatcher struct {
	townRoot string
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	logger   func(format string, args ...interface{})
}

// bdActivityEvent represents an event from rl activity --json.
type bdActivityEvent struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	IssueID   string `json:"issue_id"`
	Symbol    string `json:"symbol"`
	Message   string `json:"message"`
	OldStatus string `json:"old_status,omitempty"`
	NewStatus string `json:"new_status,omitempty"`
}

// NewRaidWatcher creates a new raid watcher.
func NewRaidWatcher(townRoot string, logger func(format string, args ...interface{})) *RaidWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &RaidWatcher{
		townRoot: townRoot,
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
	}
}

// Start begins the raid watcher goroutine.
func (w *RaidWatcher) Start() error {
	w.wg.Add(1)
	go w.run()
	return nil
}

// Stop gracefully stops the raid watcher.
func (w *RaidWatcher) Stop() {
	w.cancel()
	w.wg.Wait()
}

// run is the main watcher loop.
func (w *RaidWatcher) run() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			// Start rl activity --follow --encampment --json
			if err := w.watchActivity(); err != nil {
				w.logger("raid watcher: rl activity error: %v, restarting in 5s", err)
				// Wait before retry, but respect context cancellation
				select {
				case <-w.ctx.Done():
					return
				case <-time.After(5 * time.Second):
					// Continue to retry
				}
			}
		}
	}
}

// watchActivity starts rl activity and processes events until error or context cancellation.
func (w *RaidWatcher) watchActivity() error {
	cmd := exec.CommandContext(w.ctx, "rl", "activity", "--follow", "--encampment", "--json")
	cmd.Dir = w.townRoot

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting rl activity: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		select {
		case <-w.ctx.Done():
			_ = cmd.Process.Kill()
			return nil
		default:
		}

		line := scanner.Text()
		w.processLine(line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading rl activity: %w", err)
	}

	return cmd.Wait()
}

// processLine processes a single line from rl activity (NDJSON format).
func (w *RaidWatcher) processLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	var event bdActivityEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return // Skip malformed lines
	}

	// Only interested in status changes to closed
	if event.Type != "status" || event.NewStatus != "closed" {
		return
	}

	w.logger("raid watcher: detected close of %s", event.IssueID)

	// Check if this issue is tracked by any raid
	raidIDs := w.getTrackingRaids(event.IssueID)
	if len(raidIDs) == 0 {
		return
	}

	w.logger("raid watcher: %s is tracked by %d raid(s): %v", event.IssueID, len(raidIDs), raidIDs)

	// Check each tracking raid for completion
	for _, raidID := range raidIDs {
		w.checkRaidCompletion(raidID)
	}
}

// getTrackingRaids returns raid IDs that track the given issue.
func (w *RaidWatcher) getTrackingRaids(issueID string) []string {
	townRelics := filepath.Join(w.townRoot, ".relics")
	dbPath := filepath.Join(townRelics, "relics.db")

	// Query for raids that track this issue
	// Handle both direct ID and external reference format
	safeIssueID := strings.ReplaceAll(issueID, "'", "''")

	// Query for dependencies where this issue is the target
	// Raids use "tracks" type: raid -> tracked issue (depends_on_id)
	query := fmt.Sprintf(`
		SELECT DISTINCT issue_id FROM dependencies
		WHERE type = 'tracks'
		AND (depends_on_id = '%s' OR depends_on_id LIKE '%%:%s')
	`, safeIssueID, safeIssueID)

	queryCmd := exec.Command("sqlite3", "-json", dbPath, query)
	var stdout bytes.Buffer
	queryCmd.Stdout = &stdout

	if err := queryCmd.Run(); err != nil {
		return nil
	}

	var results []struct {
		IssueID string `json:"issue_id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		return nil
	}

	raidIDs := make([]string, 0, len(results))
	for _, r := range results {
		raidIDs = append(raidIDs, r.IssueID)
	}
	return raidIDs
}

// checkRaidCompletion checks if all issues tracked by a raid are closed.
// If so, runs hd raid check to close the raid.
func (w *RaidWatcher) checkRaidCompletion(raidID string) {
	townRelics := filepath.Join(w.townRoot, ".relics")
	dbPath := filepath.Join(townRelics, "relics.db")

	// First check if the raid is still open
	raidQuery := fmt.Sprintf(`SELECT status FROM issues WHERE id = '%s'`,
		strings.ReplaceAll(raidID, "'", "''"))

	queryCmd := exec.Command("sqlite3", "-json", dbPath, raidQuery)
	var stdout bytes.Buffer
	queryCmd.Stdout = &stdout

	if err := queryCmd.Run(); err != nil {
		return
	}

	var raidStatus []struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raidStatus); err != nil || len(raidStatus) == 0 {
		return
	}

	if raidStatus[0].Status == "closed" {
		return // Already closed
	}

	// Run hd raid check to handle the completion
	// This reuses the existing logic which handles notifications, etc.
	w.logger("raid watcher: running completion check for %s", raidID)

	checkCmd := exec.Command("hd", "raid", "check")
	checkCmd.Dir = w.townRoot
	var checkStdout, checkStderr bytes.Buffer
	checkCmd.Stdout = &checkStdout
	checkCmd.Stderr = &checkStderr

	if err := checkCmd.Run(); err != nil {
		w.logger("raid watcher: hd raid check failed: %v: %s", err, checkStderr.String())
		return
	}

	if output := checkStdout.String(); output != "" && !strings.Contains(output, "No raids ready") {
		w.logger("raid watcher: %s", strings.TrimSpace(output))
	}
}
