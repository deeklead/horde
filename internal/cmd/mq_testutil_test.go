package cmd

import (
	"github.com/OWNER/horde/internal/relics"
)

// mockRelics is a test double for relics.Relics
type mockRelics struct {
	issues    map[string]*relics.Issue
	listFunc  func(opts relics.ListOptions) ([]*relics.Issue, error)
	showFunc  func(id string) (*relics.Issue, error)
	closeFunc func(id string) error
}

func newMockRelics() *mockRelics {
	return &mockRelics{
		issues: make(map[string]*relics.Issue),
	}
}

func (m *mockRelics) addIssue(issue *relics.Issue) {
	m.issues[issue.ID] = issue
}

func (m *mockRelics) Show(id string) (*relics.Issue, error) {
	if m.showFunc != nil {
		return m.showFunc(id)
	}
	if issue, ok := m.issues[id]; ok {
		return issue, nil
	}
	return nil, relics.ErrNotFound
}

func (m *mockRelics) List(opts relics.ListOptions) ([]*relics.Issue, error) {
	if m.listFunc != nil {
		return m.listFunc(opts)
	}
	var result []*relics.Issue
	for _, issue := range m.issues {
		// Apply basic filtering
		if opts.Type != "" && issue.Type != opts.Type {
			continue
		}
		if opts.Status != "" && issue.Status != opts.Status {
			continue
		}
		result = append(result, issue)
	}
	return result, nil
}

func (m *mockRelics) Close(id string) error {
	if m.closeFunc != nil {
		return m.closeFunc(id)
	}
	if issue, ok := m.issues[id]; ok {
		issue.Status = "closed"
		return nil
	}
	return relics.ErrNotFound
}

// makeTestIssue creates a test issue with common defaults
func makeTestIssue(id, title, issueType, status string) *relics.Issue {
	return &relics.Issue{
		ID:        id,
		Title:     title,
		Type:      issueType,
		Status:    status,
		Priority:  2,
		CreatedAt: "2025-01-01T12:00:00Z",
		UpdatedAt: "2025-01-01T12:00:00Z",
	}
}

// makeTestMR creates a test merge request issue
func makeTestMR(id, branch, target, worker string, status string) *relics.Issue {
	desc := relics.FormatMRFields(&relics.MRFields{
		Branch:      branch,
		Target:      target,
		Worker:      worker,
		SourceIssue: "gt-src-123",
		Warband:         "testrig",
	})
	return &relics.Issue{
		ID:          id,
		Title:       "Merge: " + branch,
		Type:        "merge-request",
		Status:      status,
		Priority:    2,
		Description: desc,
		CreatedAt:   "2025-01-01T12:00:00Z",
		UpdatedAt:   "2025-01-01T12:00:00Z",
	}
}
