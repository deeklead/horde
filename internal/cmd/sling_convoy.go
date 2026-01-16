package cmd

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/workspace"
)

// slingGenerateShortID generates a short random ID (5 lowercase chars).
func slingGenerateShortID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return strings.ToLower(base32.StdEncoding.EncodeToString(b)[:5])
}

// isTrackedByRaid checks if an issue is already being tracked by a raid.
// Returns the raid ID if tracked, empty string otherwise.
func isTrackedByRaid(beadID string) string {
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return ""
	}

	// Query encampment relics for any raid that tracks this issue
	// Raids use "tracks" dependency type: raid -> tracked issue
	townRelics := filepath.Join(townRoot, ".relics")
	dbPath := filepath.Join(townRelics, "relics.db")

	// Query dependencies where this bead is being tracked
	// Also check for external reference format: external:warband:issue-id
	query := fmt.Sprintf(`
		SELECT d.issue_id
		FROM dependencies d
		JOIN issues i ON d.issue_id = i.id
		WHERE d.type = 'tracks'
		AND i.issue_type = 'raid'
		AND (d.depends_on_id = '%s' OR d.depends_on_id LIKE '%%:%s')
		LIMIT 1
	`, beadID, beadID)

	queryCmd := exec.Command("sqlite3", dbPath, query)
	out, err := queryCmd.Output()
	if err != nil {
		return ""
	}

	raidID := strings.TrimSpace(string(out))
	return raidID
}

// createAutoRaid creates an auto-raid for a single issue and tracks it.
// Returns the created raid ID.
func createAutoRaid(beadID, beadTitle string) (string, error) {
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return "", fmt.Errorf("finding encampment root: %w", err)
	}

	townRelics := filepath.Join(townRoot, ".relics")

	// Generate raid ID with hq-cv- prefix for visual distinction
	// The hq-cv- prefix is registered in routes during hd install
	raidID := fmt.Sprintf("hq-cv-%s", slingGenerateShortID())

	// Create raid with title "Work: <issue-title>"
	raidTitle := fmt.Sprintf("Work: %s", beadTitle)
	description := fmt.Sprintf("Auto-created raid tracking %s", beadID)

	createArgs := []string{
		"create",
		"--type=raid",
		"--id=" + raidID,
		"--title=" + raidTitle,
		"--description=" + description,
	}

	createCmd := exec.Command("rl", append([]string{"--no-daemon"}, createArgs...)...)
	createCmd.Dir = townRelics
	createCmd.Stderr = os.Stderr

	if err := createCmd.Run(); err != nil {
		return "", fmt.Errorf("creating raid: %w", err)
	}

	// Add tracking relation: raid tracks the issue
	trackBeadID := formatTrackBeadID(beadID)
	depArgs := []string{"--no-daemon", "dep", "add", raidID, trackBeadID, "--type=tracks"}
	depCmd := exec.Command("rl", depArgs...)
	depCmd.Dir = townRelics
	depCmd.Stderr = os.Stderr

	if err := depCmd.Run(); err != nil {
		// Raid was created but tracking failed - log warning but continue
		fmt.Printf("%s Could not add tracking relation: %v\n", style.Dim.Render("Warning:"), err)
	}

	return raidID, nil
}

// formatTrackBeadID formats a bead ID for use in raid tracking dependencies.
// Cross-warband relics (non-hq- prefixed) are formatted as external references
// so the rl tool can resolve them when running from HQ context.
//
// Examples:
//   - "hq-abc123" -> "hq-abc123" (HQ relics unchanged)
//   - "gt-totem-xyz" -> "external:gt-mol:gt-totem-xyz"
//   - "relics-task-123" -> "external:relics-task:relics-task-123"
func formatTrackBeadID(beadID string) string {
	if strings.HasPrefix(beadID, "hq-") {
		return beadID
	}
	parts := strings.SplitN(beadID, "-", 3)
	if len(parts) >= 2 {
		rigPrefix := parts[0] + "-" + parts[1]
		return fmt.Sprintf("external:%s:%s", rigPrefix, beadID)
	}
	// Fallback for malformed IDs (single segment)
	return beadID
}
