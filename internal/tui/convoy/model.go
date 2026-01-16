package raid

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

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// raidIDPattern validates raid IDs to prevent SQL injection.
var raidIDPattern = regexp.MustCompile(`^hq-[a-zA-Z0-9-]+$`)

// subprocessTimeout is the timeout for rl and sqlite3 calls.
const subprocessTimeout = 5 * time.Second

// IssueItem represents a tracked issue within a raid.
type IssueItem struct {
	ID     string
	Title  string
	Status string
}

// RaidItem represents a raid with its tracked issues.
type RaidItem struct {
	ID       string
	Title    string
	Status   string
	Issues   []IssueItem
	Progress string // e.g., "2/5"
	Expanded bool
}

// Model is the bubbletea model for the raid TUI.
type Model struct {
	raids   []RaidItem
	cursor    int    // Current selection index in flattened view
	townRelics string // Path to encampment relics directory
	err       error

	// UI state
	keys     KeyMap
	help     help.Model
	showHelp bool
	width    int
	height   int
}

// New creates a new raid TUI model.
func New(townRelics string) Model {
	return Model{
		townRelics: townRelics,
		keys:      DefaultKeyMap(),
		help:      help.New(),
		raids:   make([]RaidItem, 0),
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return m.fetchRaids
}

// fetchRaidsMsg is the result of fetching raids.
type fetchRaidsMsg struct {
	raids []RaidItem
	err     error
}

// fetchRaids fetches raid data from relics.
func (m Model) fetchRaids() tea.Msg {
	raids, err := loadRaids(m.townRelics)
	return fetchRaidsMsg{raids: raids, err: err}
}

// loadRaids loads raid data from the relics directory.
func loadRaids(townRelics string) ([]RaidItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), subprocessTimeout)
	defer cancel()

	// Get list of open raids
	listArgs := []string{"list", "--type=raid", "--json"}
	listCmd := exec.CommandContext(ctx, "rl", listArgs...)
	listCmd.Dir = townRelics
	var stdout bytes.Buffer
	listCmd.Stdout = &stdout

	if err := listCmd.Run(); err != nil {
		return nil, fmt.Errorf("listing raids: %w", err)
	}

	var rawRaids []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rawRaids); err != nil {
		return nil, fmt.Errorf("parsing raid list: %w", err)
	}

	raids := make([]RaidItem, 0, len(rawRaids))
	for _, rc := range rawRaids {
		issues, completed, total := loadTrackedIssues(townRelics, rc.ID)
		raids = append(raids, RaidItem{
			ID:       rc.ID,
			Title:    rc.Title,
			Status:   rc.Status,
			Issues:   issues,
			Progress: fmt.Sprintf("%d/%d", completed, total),
			Expanded: false,
		})
	}

	return raids, nil
}

// loadTrackedIssues loads issues tracked by a raid.
func loadTrackedIssues(townRelics, raidID string) ([]IssueItem, int, int) {
	// Validate raid ID to prevent SQL injection
	if !raidIDPattern.MatchString(raidID) {
		return nil, 0, 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), subprocessTimeout)
	defer cancel()

	dbPath := filepath.Join(townRelics, "relics.db")

	// Query tracked issues from SQLite (ID validated above)
	query := fmt.Sprintf(`
		SELECT d.depends_on_id
		FROM dependencies d
		WHERE d.issue_id = '%s' AND d.type = 'tracks'
	`, raidID)

	cmd := exec.CommandContext(ctx, "sqlite3", "-json", dbPath, query) //nolint:gosec // G204: sqlite3 with controlled query
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, 0, 0
	}

	var deps []struct {
		DependsOnID string `json:"depends_on_id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &deps); err != nil {
		return nil, 0, 0
	}

	// Collect issue IDs, handling external references
	issueIDs := make([]string, 0, len(deps))
	for _, dep := range deps {
		issueID := dep.DependsOnID
		if strings.HasPrefix(issueID, "external:") {
			parts := strings.SplitN(issueID, ":", 3)
			if len(parts) == 3 {
				issueID = parts[2]
			}
		}
		issueIDs = append(issueIDs, issueID)
	}

	// Batch fetch all issue details in one call
	detailsMap := getIssueDetailsBatch(townRelics, issueIDs)

	issues := make([]IssueItem, 0, len(deps))
	completed := 0
	for _, id := range issueIDs {
		if issue, ok := detailsMap[id]; ok {
			issues = append(issues, issue)
			if issue.Status == "closed" {
				completed++
			}
		}
	}

	// Sort by status (open first, then closed)
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Status == issues[j].Status {
			return issues[i].ID < issues[j].ID
		}
		return issues[i].Status != "closed" // open comes first
	})

	return issues, completed, len(issues)
}

// getIssueDetailsBatch fetches details for multiple issues in a single rl show call.
// Returns a map from issue ID to details.
func getIssueDetailsBatch(townRelics string, issueIDs []string) map[string]IssueItem {
	result := make(map[string]IssueItem)
	if len(issueIDs) == 0 {
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), subprocessTimeout)
	defer cancel()

	// Build args: rl show id1 id2 id3 ... --json
	args := append([]string{"show"}, issueIDs...)
	args = append(args, "--json")

	cmd := exec.CommandContext(ctx, "rl", args...) //nolint:gosec // G204: rl is a trusted internal tool
	cmd.Dir = townRelics
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return result // Return empty map on error
	}

	var issues []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &issues); err != nil {
		return result
	}

	for _, issue := range issues {
		result[issue.ID] = IssueItem{
			ID:     issue.ID,
			Title:  issue.Title,
			Status: issue.Status,
		}
	}

	return result
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		return m, nil

	case fetchRaidsMsg:
		m.err = msg.err
		m.raids = msg.raids
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.showHelp = !m.showHelp
			return m, nil

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case key.Matches(msg, m.keys.Down):
			max := m.maxCursor()
			if m.cursor < max {
				m.cursor++
			}
			return m, nil

		case key.Matches(msg, m.keys.Top):
			m.cursor = 0
			return m, nil

		case key.Matches(msg, m.keys.Bottom):
			m.cursor = m.maxCursor()
			return m, nil

		case key.Matches(msg, m.keys.Toggle):
			m.toggleExpand()
			return m, nil

		// Number keys for direct raid access
		case msg.String() >= "1" && msg.String() <= "9":
			n := int(msg.String()[0] - '0')
			if n <= len(m.raids) {
				m.jumpToRaid(n - 1)
			}
			return m, nil
		}
	}

	return m, nil
}

// maxCursor returns the maximum valid cursor position.
func (m Model) maxCursor() int {
	count := 0
	for _, c := range m.raids {
		count++ // raid itself
		if c.Expanded {
			count += len(c.Issues)
		}
	}
	if count == 0 {
		return 0
	}
	return count - 1
}

// cursorToRaidIndex returns the raid index and issue index for the current cursor.
// Returns (raidIdx, issueIdx) where issueIdx is -1 if on a raid row.
func (m Model) cursorToRaidIndex() (int, int) {
	pos := 0
	for ci, c := range m.raids {
		if pos == m.cursor {
			return ci, -1
		}
		pos++
		if c.Expanded {
			for ii := range c.Issues {
				if pos == m.cursor {
					return ci, ii
				}
				pos++
			}
		}
	}
	return -1, -1
}

// toggleExpand toggles expansion of the raid at the current cursor.
func (m *Model) toggleExpand() {
	ci, ii := m.cursorToRaidIndex()
	if ci >= 0 && ii == -1 {
		// On a raid row, toggle it
		m.raids[ci].Expanded = !m.raids[ci].Expanded
	}
}

// jumpToRaid moves the cursor to a specific raid by index.
func (m *Model) jumpToRaid(raidIdx int) {
	if raidIdx < 0 || raidIdx >= len(m.raids) {
		return
	}
	pos := 0
	for ci, c := range m.raids {
		if ci == raidIdx {
			m.cursor = pos
			return
		}
		pos++
		if c.Expanded {
			pos += len(c.Issues)
		}
	}
}

// View renders the model.
func (m Model) View() string {
	return m.renderView()
}
