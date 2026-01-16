package feed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// raidIDPattern validates raid IDs to prevent SQL injection
var raidIDPattern = regexp.MustCompile(`^hq-[a-zA-Z0-9-]+$`)

// raidSubprocessTimeout is the timeout for rl and sqlite3 calls in the raid panel.
// Prevents TUI freezing if these commands hang.
const raidSubprocessTimeout = 5 * time.Second

// Raid represents a raid's status for the warmap
type Raid struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Completed int       `json:"completed"`
	Total     int       `json:"total"`
	CreatedAt time.Time `json:"created_at"`
	ClosedAt  time.Time `json:"closed_at,omitempty"`
}

// RaidState holds all raid data for the panel
type RaidState struct {
	InProgress []Raid
	Landed     []Raid
	LastUpdate time.Time
}

// FetchRaids retrieves raid status from encampment-level relics
func FetchRaids(townRoot string) (*RaidState, error) {
	townRelics := filepath.Join(townRoot, ".relics")

	state := &RaidState{
		InProgress: make([]Raid, 0),
		Landed:     make([]Raid, 0),
		LastUpdate: time.Now(),
	}

	// Fetch open raids
	openRaids, err := listRaids(townRelics, "open")
	if err != nil {
		// Not a fatal error - just return empty state
		return state, nil
	}

	for _, c := range openRaids {
		// Get detailed status for each raid
		raid := enrichRaid(townRelics, c)
		state.InProgress = append(state.InProgress, raid)
	}

	// Fetch recently closed raids (landed in last 24h)
	closedRaids, err := listRaids(townRelics, "closed")
	if err == nil {
		cutoff := time.Now().Add(-24 * time.Hour)
		for _, c := range closedRaids {
			raid := enrichRaid(townRelics, c)
			if !raid.ClosedAt.IsZero() && raid.ClosedAt.After(cutoff) {
				state.Landed = append(state.Landed, raid)
			}
		}
	}

	// Sort: in-progress by created (oldest first), landed by closed (newest first)
	sort.Slice(state.InProgress, func(i, j int) bool {
		return state.InProgress[i].CreatedAt.Before(state.InProgress[j].CreatedAt)
	})
	sort.Slice(state.Landed, func(i, j int) bool {
		return state.Landed[i].ClosedAt.After(state.Landed[j].ClosedAt)
	})

	return state, nil
}

// listRaids returns raids with the given status
func listRaids(relicsDir, status string) ([]raidListItem, error) {
	listArgs := []string{"list", "--type=raid", "--status=" + status, "--json"}

	ctx, cancel := context.WithTimeout(context.Background(), raidSubprocessTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "rl", listArgs...) //nolint:gosec // G204: args are constructed internally
	cmd.Dir = relicsDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var items []raidListItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		return nil, err
	}

	return items, nil
}

type raidListItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	ClosedAt  string `json:"closed_at,omitempty"`
}

// enrichRaid adds tracked issue counts to a raid
func enrichRaid(relicsDir string, item raidListItem) Raid {
	raid := Raid{
		ID:     item.ID,
		Title:  item.Title,
		Status: item.Status,
	}

	// Parse timestamps
	if t, err := time.Parse(time.RFC3339, item.CreatedAt); err == nil {
		raid.CreatedAt = t
	} else if t, err := time.Parse("2006-01-02 15:04", item.CreatedAt); err == nil {
		raid.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, item.ClosedAt); err == nil {
		raid.ClosedAt = t
	} else if t, err := time.Parse("2006-01-02 15:04", item.ClosedAt); err == nil {
		raid.ClosedAt = t
	}

	// Get tracked issues and their status
	tracked := getTrackedIssueStatus(relicsDir, item.ID)
	raid.Total = len(tracked)
	for _, t := range tracked {
		if t.Status == "closed" {
			raid.Completed++
		}
	}

	return raid
}

type trackedStatus struct {
	ID     string
	Status string
}

// getTrackedIssueStatus queries tracked issues and their status
func getTrackedIssueStatus(relicsDir, raidID string) []trackedStatus {
	// Validate raidID to prevent SQL injection
	if !raidIDPattern.MatchString(raidID) {
		return nil
	}

	dbPath := filepath.Join(relicsDir, "relics.db")

	ctx, cancel := context.WithTimeout(context.Background(), raidSubprocessTimeout)
	defer cancel()

	// Query tracked dependencies from SQLite
	// raidID is validated above to match ^hq-[a-zA-Z0-9-]+$
	cmd := exec.CommandContext(ctx, "sqlite3", "-json", dbPath, //nolint:gosec // G204: raidID is validated against strict pattern
		fmt.Sprintf(`SELECT depends_on_id FROM dependencies WHERE issue_id = '%s' AND type = 'tracks'`, raidID))

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}

	var deps []struct {
		DependsOnID string `json:"depends_on_id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &deps); err != nil {
		return nil
	}

	var tracked []trackedStatus
	for _, dep := range deps {
		issueID := dep.DependsOnID

		// Handle external reference format: external:warband:issue-id
		if strings.HasPrefix(issueID, "external:") {
			parts := strings.SplitN(issueID, ":", 3)
			if len(parts) == 3 {
				issueID = parts[2]
			}
		}

		// Get issue status
		status := getIssueStatus(issueID)
		tracked = append(tracked, trackedStatus{ID: issueID, Status: status})
	}

	return tracked
}

// getIssueStatus fetches just the status of an issue
func getIssueStatus(issueID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), raidSubprocessTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "rl", "show", issueID, "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "unknown"
	}

	var issues []struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &issues); err != nil || len(issues) == 0 {
		return "unknown"
	}

	return issues[0].Status
}

// Raid panel styles
var (
	RaidPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorDim).
				Padding(0, 1)

	RaidTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	RaidSectionStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Bold(true)

	RaidIDStyle = lipgloss.NewStyle().
			Foreground(colorHighlight)

	RaidNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15"))

	RaidProgressStyle = lipgloss.NewStyle().
				Foreground(colorSuccess)

	RaidLandedStyle = lipgloss.NewStyle().
				Foreground(colorSuccess).
				Bold(true)

	RaidAgeStyle = lipgloss.NewStyle().
			Foreground(colorDim)
)

// renderRaidPanel renders the raid status panel
func (m *Model) renderRaidPanel() string {
	style := RaidPanelStyle
	if m.focusedPanel == PanelRaid {
		style = FocusedBorderStyle
	}
	// Add title before content
	title := RaidTitleStyle.Render("🚚 Raids")
	content := title + "\n" + m.raidViewport.View()
	return style.Width(m.width - 2).Render(content)
}

// renderRaids renders the raid panel content
func (m *Model) renderRaids() string {
	if m.raidState == nil {
		return AgentIdleStyle.Render("Loading raids...")
	}

	var lines []string

	// In Progress section
	lines = append(lines, RaidSectionStyle.Render("IN PROGRESS"))
	if len(m.raidState.InProgress) == 0 {
		lines = append(lines, "  "+AgentIdleStyle.Render("No active raids"))
	} else {
		for _, c := range m.raidState.InProgress {
			lines = append(lines, renderRaidLine(c, false))
		}
	}

	lines = append(lines, "")

	// Recently Landed section
	lines = append(lines, RaidSectionStyle.Render("RECENTLY LANDED (24h)"))
	if len(m.raidState.Landed) == 0 {
		lines = append(lines, "  "+AgentIdleStyle.Render("No recent landings"))
	} else {
		for _, c := range m.raidState.Landed {
			lines = append(lines, renderRaidLine(c, true))
		}
	}

	return strings.Join(lines, "\n")
}

// renderRaidLine renders a single raid status line
func renderRaidLine(c Raid, landed bool) string {
	// Format: "  hq-xyz  Title       2/4 ●●○○" or "  hq-xyz  Title       ✓ 2h ago"
	id := RaidIDStyle.Render(c.ID)

	// Truncate title if too long
	title := c.Title
	if len(title) > 20 {
		title = title[:17] + "..."
	}
	title = RaidNameStyle.Render(title)

	if landed {
		// Show checkmark and time since landing
		age := formatAge(time.Since(c.ClosedAt))
		status := RaidLandedStyle.Render("✓") + " " + RaidAgeStyle.Render(age+" ago")
		return fmt.Sprintf("  %s  %-20s  %s", id, title, status)
	}

	// Show progress bar
	progress := renderProgressBar(c.Completed, c.Total)
	count := RaidProgressStyle.Render(fmt.Sprintf("%d/%d", c.Completed, c.Total))
	return fmt.Sprintf("  %s  %-20s  %s %s", id, title, count, progress)
}

// renderProgressBar creates a simple progress bar: ●●○○
func renderProgressBar(completed, total int) string {
	if total == 0 {
		return ""
	}

	// Cap at 5 dots for display
	displayTotal := total
	if displayTotal > 5 {
		displayTotal = 5
	}

	filled := (completed * displayTotal) / total
	if filled > displayTotal {
		filled = displayTotal
	}

	bar := strings.Repeat("●", filled) + strings.Repeat("○", displayTotal-filled)
	return RaidProgressStyle.Render(bar)
}

