package web

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/deeklead/horde/internal/activity"
)

// Test error for simulating fetch failures
var errFetchFailed = errors.New("fetch failed")

// MockRaidFetcher is a mock implementation for testing.
type MockRaidFetcher struct {
	Raids    []RaidRow
	MergeQueue []MergeQueueRow
	Raiders   []RaiderRow
	Error      error
}

func (m *MockRaidFetcher) FetchRaids() ([]RaidRow, error) {
	return m.Raids, m.Error
}

func (m *MockRaidFetcher) FetchMergeQueue() ([]MergeQueueRow, error) {
	return m.MergeQueue, nil
}

func (m *MockRaidFetcher) FetchRaiders() ([]RaiderRow, error) {
	return m.Raiders, nil
}

func TestRaidHandler_RendersTemplate(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{
			{
				ID:           "hq-cv-abc",
				Title:        "Test Raid",
				Status:       "open",
				Progress:     "2/5",
				Completed:    2,
				Total:        5,
				LastActivity: activity.Calculate(time.Now().Add(-1 * time.Minute)),
			},
		},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()

	// Check raid data is rendered
	if !strings.Contains(body, "hq-cv-abc") {
		t.Error("Response should contain raid ID")
	}
	if !strings.Contains(body, "Test Raid") {
		t.Error("Response should contain raid title")
	}
	if !strings.Contains(body, "2/5") {
		t.Error("Response should contain progress")
	}
}

func TestRaidHandler_LastActivityColors(t *testing.T) {
	tests := []struct {
		name      string
		age       time.Duration
		wantClass string
	}{
		{"green for active", 30 * time.Second, "activity-green"},
		{"yellow for stale", 3 * time.Minute, "activity-yellow"},
		{"red for stuck", 10 * time.Minute, "activity-red"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockRaidFetcher{
				Raids: []RaidRow{
					{
						ID:           "hq-cv-test",
						Title:        "Test",
						Status:       "open",
						LastActivity: activity.Calculate(time.Now().Add(-tt.age)),
					},
				},
			}

			handler, err := NewRaidHandler(mock)
			if err != nil {
				t.Fatalf("NewRaidHandler() error = %v", err)
			}

			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			body := w.Body.String()
			if !strings.Contains(body, tt.wantClass) {
				t.Errorf("Response should contain %q", tt.wantClass)
			}
		})
	}
}

func TestRaidHandler_EmptyRaids(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "No raids") {
		t.Error("Response should show empty state message")
	}
}

func TestRaidHandler_ContentType(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", contentType)
	}
}

func TestRaidHandler_MultipleRaids(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{
			{ID: "hq-cv-1", Title: "First Raid", Status: "open"},
			{ID: "hq-cv-2", Title: "Second Raid", Status: "closed"},
			{ID: "hq-cv-3", Title: "Third Raid", Status: "open"},
		},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	body := w.Body.String()

	// Check all raids are rendered
	for _, id := range []string{"hq-cv-1", "hq-cv-2", "hq-cv-3"} {
		if !strings.Contains(body, id) {
			t.Errorf("Response should contain raid %s", id)
		}
	}
}

// Integration tests for error handling

func TestRaidHandler_FetchRaidsError(t *testing.T) {
	mock := &MockRaidFetcher{
		Error: errFetchFailed,
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Failed to fetch raids") {
		t.Error("Response should contain error message")
	}
}

// Integration tests for merge queue rendering

func TestRaidHandler_MergeQueueRendering(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{},
		MergeQueue: []MergeQueueRow{
			{
				Number:     123,
				Repo:       "roxas",
				Title:      "Fix authentication bug",
				URL:        "https://github.com/test/repo/pull/123",
				CIStatus:   "pass",
				Mergeable:  "ready",
				ColorClass: "mq-green",
			},
			{
				Number:     456,
				Repo:       "horde",
				Title:      "Add warmap feature",
				URL:        "https://github.com/test/repo/pull/456",
				CIStatus:   "pending",
				Mergeable:  "pending",
				ColorClass: "mq-yellow",
			},
		},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()

	// Check merge queue section header
	if !strings.Contains(body, "Forge Merge Queue") {
		t.Error("Response should contain merge queue section header")
	}

	// Check PR numbers are rendered
	if !strings.Contains(body, "#123") {
		t.Error("Response should contain PR #123")
	}
	if !strings.Contains(body, "#456") {
		t.Error("Response should contain PR #456")
	}

	// Check repo names
	if !strings.Contains(body, "roxas") {
		t.Error("Response should contain repo 'roxas'")
	}

	// Check CI status badges
	if !strings.Contains(body, "ci-pass") {
		t.Error("Response should contain ci-pass class for passing PR")
	}
	if !strings.Contains(body, "ci-pending") {
		t.Error("Response should contain ci-pending class for pending PR")
	}
}

func TestRaidHandler_EmptyMergeQueue(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids:    []RaidRow{},
		MergeQueue: []MergeQueueRow{},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	body := w.Body.String()

	// Should show empty state for merge queue
	if !strings.Contains(body, "No PRs in queue") {
		t.Error("Response should show empty merge queue message")
	}
}

// Integration tests for raider workers rendering

func TestRaidHandler_RaiderWorkersRendering(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{},
		Raiders: []RaiderRow{
			{
				Name:         "dag",
				Warband:          "roxas",
				SessionID:    "hd-roxas-dag",
				LastActivity: activity.Calculate(time.Now().Add(-30 * time.Second)),
				StatusHint:   "Running tests...",
			},
			{
				Name:         "nux",
				Warband:          "roxas",
				SessionID:    "hd-roxas-nux",
				LastActivity: activity.Calculate(time.Now().Add(-5 * time.Minute)),
				StatusHint:   "Waiting for input",
			},
		},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()

	// Check raider section header
	if !strings.Contains(body, "Raider Workers") {
		t.Error("Response should contain raider workers section header")
	}

	// Check raider names
	if !strings.Contains(body, "dag") {
		t.Error("Response should contain raider 'dag'")
	}
	if !strings.Contains(body, "nux") {
		t.Error("Response should contain raider 'nux'")
	}

	// Check warband names
	if !strings.Contains(body, "roxas") {
		t.Error("Response should contain warband 'roxas'")
	}

	// Check status hints
	if !strings.Contains(body, "Running tests...") {
		t.Error("Response should contain status hint")
	}

	// Check activity colors (dag should be green, nux should be yellow/red)
	if !strings.Contains(body, "activity-green") {
		t.Error("Response should contain activity-green for recent activity")
	}
}

// Integration tests for work status rendering

func TestRaidHandler_WorkStatusRendering(t *testing.T) {
	tests := []struct {
		name           string
		workStatus     string
		wantClass      string
		wantStatusText string
	}{
		{"complete status", "complete", "work-complete", "complete"},
		{"active status", "active", "work-active", "active"},
		{"stale status", "stale", "work-stale", "stale"},
		{"stuck status", "stuck", "work-stuck", "stuck"},
		{"waiting status", "waiting", "work-waiting", "waiting"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockRaidFetcher{
				Raids: []RaidRow{
					{
						ID:           "hq-cv-test",
						Title:        "Test Raid",
						Status:       "open",
						WorkStatus:   tt.workStatus,
						Progress:     "1/2",
						Completed:    1,
						Total:        2,
						LastActivity: activity.Calculate(time.Now()),
					},
				},
			}

			handler, err := NewRaidHandler(mock)
			if err != nil {
				t.Fatalf("NewRaidHandler() error = %v", err)
			}

			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			body := w.Body.String()

			// Check work status class is applied
			if !strings.Contains(body, tt.wantClass) {
				t.Errorf("Response should contain class %q for work status %q", tt.wantClass, tt.workStatus)
			}

			// Check work status text is displayed
			if !strings.Contains(body, tt.wantStatusText) {
				t.Errorf("Response should contain status text %q", tt.wantStatusText)
			}
		})
	}
}

// Integration tests for progress bar rendering

func TestRaidHandler_ProgressBarRendering(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{
			{
				ID:           "hq-cv-progress",
				Title:        "Progress Test",
				Status:       "open",
				WorkStatus:   "active",
				Progress:     "3/4",
				Completed:    3,
				Total:        4,
				LastActivity: activity.Calculate(time.Now()),
			},
		},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	body := w.Body.String()

	// Check progress text
	if !strings.Contains(body, "3/4") {
		t.Error("Response should contain progress '3/4'")
	}

	// Check progress bar element
	if !strings.Contains(body, "progress-bar") {
		t.Error("Response should contain progress-bar class")
	}

	// Check progress fill with percentage (75%)
	if !strings.Contains(body, "progress-fill") {
		t.Error("Response should contain progress-fill class")
	}
	if !strings.Contains(body, "width: 75%") {
		t.Error("Response should contain 75% width for 3/4 progress")
	}
}

// Integration test for HTMX auto-refresh

func TestRaidHandler_HTMXAutoRefresh(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	body := w.Body.String()

	// Check htmx attributes for auto-refresh
	if !strings.Contains(body, "hx-get") {
		t.Error("Response should contain hx-get attribute for HTMX")
	}
	if !strings.Contains(body, "hx-trigger") {
		t.Error("Response should contain hx-trigger attribute for HTMX")
	}
	if !strings.Contains(body, "every 10s") {
		t.Error("Response should contain 'every 10s' trigger interval")
	}
}

// Integration test for full warmap with all sections

func TestRaidHandler_FullDashboard(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{
			{
				ID:           "hq-cv-full",
				Title:        "Full Test Raid",
				Status:       "open",
				WorkStatus:   "active",
				Progress:     "2/3",
				Completed:    2,
				Total:        3,
				LastActivity: activity.Calculate(time.Now().Add(-1 * time.Minute)),
			},
		},
		MergeQueue: []MergeQueueRow{
			{
				Number:     789,
				Repo:       "testrig",
				Title:      "Test PR",
				CIStatus:   "pass",
				Mergeable:  "ready",
				ColorClass: "mq-green",
			},
		},
		Raiders: []RaiderRow{
			{
				Name:         "worker1",
				Warband:          "testrig",
				SessionID:    "hd-testrig-worker1",
				LastActivity: activity.Calculate(time.Now()),
				StatusHint:   "Working...",
			},
		},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()

	// Verify all three sections are present
	if !strings.Contains(body, "Horde Raids") {
		t.Error("Response should contain main header")
	}
	if !strings.Contains(body, "hq-cv-full") {
		t.Error("Response should contain raid data")
	}
	if !strings.Contains(body, "Forge Merge Queue") {
		t.Error("Response should contain merge queue section")
	}
	if !strings.Contains(body, "#789") {
		t.Error("Response should contain PR data")
	}
	if !strings.Contains(body, "Raider Workers") {
		t.Error("Response should contain raider section")
	}
	if !strings.Contains(body, "worker1") {
		t.Error("Response should contain raider data")
	}
}

// =============================================================================
// End-to-End Tests with httptest.Server
// =============================================================================

// TestE2E_Server_FullDashboard tests the full warmap using a real HTTP server.
func TestE2E_Server_FullDashboard(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids: []RaidRow{
			{
				ID:           "hq-cv-e2e",
				Title:        "E2E Test Raid",
				Status:       "open",
				WorkStatus:   "active",
				Progress:     "2/4",
				Completed:    2,
				Total:        4,
				LastActivity: activity.Calculate(time.Now().Add(-45 * time.Second)),
			},
		},
		MergeQueue: []MergeQueueRow{
			{
				Number:     101,
				Repo:       "roxas",
				Title:      "E2E Test PR",
				URL:        "https://github.com/test/roxas/pull/101",
				CIStatus:   "pass",
				Mergeable:  "ready",
				ColorClass: "mq-green",
			},
		},
		Raiders: []RaiderRow{
			{
				Name:         "furiosa",
				Warband:          "roxas",
				SessionID:    "hd-roxas-furiosa",
				LastActivity: activity.Calculate(time.Now().Add(-30 * time.Second)),
				StatusHint:   "Running E2E tests",
			},
		},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	// Create a real HTTP server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Make HTTP request to the server
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	// Verify status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", contentType)
	}

	// Read and verify body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	body := string(bodyBytes)

	// Verify all three sections render
	checks := []struct {
		name    string
		content string
	}{
		{"Raid section header", "Horde Raids"},
		{"Raid ID", "hq-cv-e2e"},
		{"Raid title", "E2E Test Raid"},
		{"Raid progress", "2/4"},
		{"Merge queue section", "Forge Merge Queue"},
		{"PR number", "#101"},
		{"PR repo", "roxas"},
		{"Raider section", "Raider Workers"},
		{"Raider name", "furiosa"},
		{"Raider status", "Running E2E tests"},
		{"HTMX auto-refresh", `hx-trigger="every 10s"`},
	}

	for _, check := range checks {
		if !strings.Contains(body, check.content) {
			t.Errorf("%s: should contain %q", check.name, check.content)
		}
	}
}

// TestE2E_Server_ActivityColors tests activity color rendering via HTTP server.
func TestE2E_Server_ActivityColors(t *testing.T) {
	tests := []struct {
		name      string
		age       time.Duration
		wantClass string
	}{
		{"green for recent", 20 * time.Second, "activity-green"},
		{"yellow for stale", 3 * time.Minute, "activity-yellow"},
		{"red for stuck", 8 * time.Minute, "activity-red"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockRaidFetcher{
				Raiders: []RaiderRow{
					{
						Name:         "test-worker",
						Warband:          "test-warband",
						SessionID:    "hd-test-warband-test-worker",
						LastActivity: activity.Calculate(time.Now().Add(-tt.age)),
						StatusHint:   "Testing",
					},
				},
			}

			handler, err := NewRaidHandler(mock)
			if err != nil {
				t.Fatalf("NewRaidHandler() error = %v", err)
			}

			server := httptest.NewServer(handler)
			defer server.Close()

			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatalf("HTTP GET failed: %v", err)
			}
			defer resp.Body.Close()

			bodyBytes, _ := io.ReadAll(resp.Body)
			body := string(bodyBytes)

			if !strings.Contains(body, tt.wantClass) {
				t.Errorf("Should contain activity class %q for age %v", tt.wantClass, tt.age)
			}
		})
	}
}

// TestE2E_Server_MergeQueueEmpty tests that empty merge queue shows message.
func TestE2E_Server_MergeQueueEmpty(t *testing.T) {
	mock := &MockRaidFetcher{
		Raids:    []RaidRow{},
		MergeQueue: []MergeQueueRow{},
		Raiders:   []RaiderRow{},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	// Section header should always be visible
	if !strings.Contains(body, "Forge Merge Queue") {
		t.Error("Merge queue section should always be visible")
	}

	// Empty state message
	if !strings.Contains(body, "No PRs in queue") {
		t.Error("Should show 'No PRs in queue' when empty")
	}
}

// TestE2E_Server_MergeQueueStatuses tests all PR status combinations.
func TestE2E_Server_MergeQueueStatuses(t *testing.T) {
	tests := []struct {
		name       string
		ciStatus   string
		mergeable  string
		colorClass string
		wantCI     string
		wantMerge  string
	}{
		{"green when ready", "pass", "ready", "mq-green", "ci-pass", "merge-ready"},
		{"red when CI fails", "fail", "ready", "mq-red", "ci-fail", "merge-ready"},
		{"red when conflict", "pass", "conflict", "mq-red", "ci-pass", "merge-conflict"},
		{"yellow when pending", "pending", "pending", "mq-yellow", "ci-pending", "merge-pending"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockRaidFetcher{
				MergeQueue: []MergeQueueRow{
					{
						Number:     42,
						Repo:       "test",
						Title:      "Test PR",
						URL:        "https://github.com/test/test/pull/42",
						CIStatus:   tt.ciStatus,
						Mergeable:  tt.mergeable,
						ColorClass: tt.colorClass,
					},
				},
			}

			handler, err := NewRaidHandler(mock)
			if err != nil {
				t.Fatalf("NewRaidHandler() error = %v", err)
			}

			server := httptest.NewServer(handler)
			defer server.Close()

			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatalf("HTTP GET failed: %v", err)
			}
			defer resp.Body.Close()

			bodyBytes, _ := io.ReadAll(resp.Body)
			body := string(bodyBytes)

			if !strings.Contains(body, tt.colorClass) {
				t.Errorf("Should contain row class %q", tt.colorClass)
			}
			if !strings.Contains(body, tt.wantCI) {
				t.Errorf("Should contain CI class %q", tt.wantCI)
			}
			if !strings.Contains(body, tt.wantMerge) {
				t.Errorf("Should contain merge class %q", tt.wantMerge)
			}
		})
	}
}

// TestE2E_Server_HTMLStructure validates HTML document structure.
func TestE2E_Server_HTMLStructure(t *testing.T) {
	mock := &MockRaidFetcher{Raids: []RaidRow{}}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	// Validate HTML structure
	elements := []string{
		"<!DOCTYPE html>",
		"<html",
		"<head>",
		"<title>Horde Warmap</title>",
		"htmx.org",
		"<body>",
		"</body>",
		"</html>",
	}

	for _, elem := range elements {
		if !strings.Contains(body, elem) {
			t.Errorf("Should contain HTML element %q", elem)
		}
	}

	// Validate CSS variables for theming
	cssVars := []string{"--bg-dark", "--green", "--yellow", "--red"}
	for _, v := range cssVars {
		if !strings.Contains(body, v) {
			t.Errorf("Should contain CSS variable %q", v)
		}
	}
}

// TestE2E_Server_ForgeInRaiders tests that forge appears in raider workers.
func TestE2E_Server_ForgeInRaiders(t *testing.T) {
	mock := &MockRaidFetcher{
		Raiders: []RaiderRow{
			{
				Name:         "forge",
				Warband:          "roxas",
				SessionID:    "hd-roxas-forge",
				LastActivity: activity.Calculate(time.Now().Add(-10 * time.Second)),
				StatusHint:   "Idle - Waiting for PRs",
			},
			{
				Name:         "dag",
				Warband:          "roxas",
				SessionID:    "hd-roxas-dag",
				LastActivity: activity.Calculate(time.Now().Add(-30 * time.Second)),
				StatusHint:   "Working on feature",
			},
		},
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	// Forge should appear in raider workers
	if !strings.Contains(body, "forge") {
		t.Error("Forge should appear in raider workers section")
	}
	if !strings.Contains(body, "Idle - Waiting for PRs") {
		t.Error("Forge idle status should be shown")
	}

	// Regular raiders should also appear
	if !strings.Contains(body, "dag") {
		t.Error("Regular raider 'dag' should appear")
	}
}

// Test that merge queue and raider errors are non-fatal

type MockRaidFetcherWithErrors struct {
	Raids          []RaidRow
	MergeQueueError  error
	RaidersError    error
}

func (m *MockRaidFetcherWithErrors) FetchRaids() ([]RaidRow, error) {
	return m.Raids, nil
}

func (m *MockRaidFetcherWithErrors) FetchMergeQueue() ([]MergeQueueRow, error) {
	return nil, m.MergeQueueError
}

func (m *MockRaidFetcherWithErrors) FetchRaiders() ([]RaiderRow, error) {
	return nil, m.RaidersError
}

func TestRaidHandler_NonFatalErrors(t *testing.T) {
	mock := &MockRaidFetcherWithErrors{
		Raids: []RaidRow{
			{ID: "hq-cv-test", Title: "Test", Status: "open", WorkStatus: "active"},
		},
		MergeQueueError: errFetchFailed,
		RaidersError:   errFetchFailed,
	}

	handler, err := NewRaidHandler(mock)
	if err != nil {
		t.Fatalf("NewRaidHandler() error = %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should still return OK even if merge queue and raiders fail
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d (non-fatal errors should not fail request)", w.Code, http.StatusOK)
	}

	body := w.Body.String()

	// Raids should still render
	if !strings.Contains(body, "hq-cv-test") {
		t.Error("Response should contain raid data even when other fetches fail")
	}
}
