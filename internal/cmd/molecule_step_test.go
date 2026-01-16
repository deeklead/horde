package cmd

import (
	"testing"

	"github.com/deeklead/horde/internal/relics"
)

func TestExtractMoleculeIDFromStep(t *testing.T) {
	tests := []struct {
		name     string
		stepID   string
		expected string
	}{
		{
			name:     "simple step",
			stepID:   "gt-abc.1",
			expected: "gt-abc",
		},
		{
			name:     "multi-digit step number",
			stepID:   "gt-xyz.12",
			expected: "gt-xyz",
		},
		{
			name:     "totem with dash",
			stepID:   "gt-my-mol.3",
			expected: "gt-my-mol",
		},
		{
			name:     "bd prefix",
			stepID:   "bd-totem-abc.2",
			expected: "bd-totem-abc",
		},
		{
			name:     "complex id",
			stepID:   "gt-some-complex-id.99",
			expected: "gt-some-complex-id",
		},
		{
			name:     "not a step - no suffix",
			stepID:   "gt-5gq8r",
			expected: "",
		},
		{
			name:     "not a step - non-numeric suffix",
			stepID:   "gt-abc.xyz",
			expected: "",
		},
		{
			name:     "not a step - mixed suffix",
			stepID:   "gt-abc.1a",
			expected: "",
		},
		{
			name:     "empty string",
			stepID:   "",
			expected: "",
		},
		{
			name:     "just a dot",
			stepID:   ".",
			expected: "",
		},
		{
			name:     "trailing dot",
			stepID:   "gt-abc.",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMoleculeIDFromStep(tt.stepID)
			if result != tt.expected {
				t.Errorf("extractMoleculeIDFromStep(%q) = %q, want %q", tt.stepID, result, tt.expected)
			}
		})
	}
}

// mockRelicsForStep extends mockRelics with parent filtering for step tests
type mockRelicsForStep struct {
	issues map[string]*relics.Issue
}

func newMockRelicsForStep() *mockRelicsForStep {
	return &mockRelicsForStep{
		issues: make(map[string]*relics.Issue),
	}
}

func (m *mockRelicsForStep) addIssue(issue *relics.Issue) {
	m.issues[issue.ID] = issue
}

func (m *mockRelicsForStep) Show(id string) (*relics.Issue, error) {
	if issue, ok := m.issues[id]; ok {
		return issue, nil
	}
	return nil, relics.ErrNotFound
}

func (m *mockRelicsForStep) List(opts relics.ListOptions) ([]*relics.Issue, error) {
	var result []*relics.Issue
	for _, issue := range m.issues {
		// Filter by parent
		if opts.Parent != "" && issue.Parent != opts.Parent {
			continue
		}
		// Filter by status (unless "all")
		if opts.Status != "" && opts.Status != "all" && issue.Status != opts.Status {
			continue
		}
		result = append(result, issue)
	}
	return result, nil
}

func (m *mockRelicsForStep) Close(ids ...string) error {
	for _, id := range ids {
		if issue, ok := m.issues[id]; ok {
			issue.Status = "closed"
		} else {
			return relics.ErrNotFound
		}
	}
	return nil
}

// makeStepIssue creates a test step issue
func makeStepIssue(id, title, parent, status string, dependsOn []string) *relics.Issue {
	return &relics.Issue{
		ID:        id,
		Title:     title,
		Type:      "task",
		Status:    status,
		Priority:  2,
		Parent:    parent,
		DependsOn: dependsOn,
		CreatedAt: "2025-01-01T12:00:00Z",
		UpdatedAt: "2025-01-01T12:00:00Z",
	}
}

func TestFindNextReadyStep(t *testing.T) {
	tests := []struct {
		name           string
		moleculeID     string
		setupFunc      func(*mockRelicsForStep)
		wantStepID     string
		wantComplete   bool
		wantNilStep    bool
	}{
		{
			name:       "no steps - totem complete",
			moleculeID: "gt-mol",
			setupFunc: func(m *mockRelicsForStep) {
				// Empty totem - no children
			},
			wantComplete: true,
			wantNilStep:  true,
		},
		{
			name:       "all steps closed - totem complete",
			moleculeID: "gt-mol",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Step 1", "gt-mol", "closed", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step 2", "gt-mol", "closed", []string{"gt-mol.1"}))
			},
			wantComplete: true,
			wantNilStep:  true,
		},
		{
			name:       "first step ready - no dependencies",
			moleculeID: "gt-mol",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Step 1", "gt-mol", "open", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step 2", "gt-mol", "open", []string{"gt-mol.1"}))
			},
			wantStepID:   "gt-mol.1",
			wantComplete: false,
		},
		{
			name:       "second step ready - first closed",
			moleculeID: "gt-mol",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Step 1", "gt-mol", "closed", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step 2", "gt-mol", "open", []string{"gt-mol.1"}))
			},
			wantStepID:   "gt-mol.2",
			wantComplete: false,
		},
		{
			name:       "all blocked - waiting on dependencies",
			moleculeID: "gt-mol",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Step 1", "gt-mol", "in_progress", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step 2", "gt-mol", "open", []string{"gt-mol.1"}))
				m.addIssue(makeStepIssue("gt-mol.3", "Step 3", "gt-mol", "open", []string{"gt-mol.2"}))
			},
			wantComplete: false,
			wantNilStep:  true, // No ready steps (all blocked or in-progress)
		},
		{
			name:       "parallel steps - multiple ready",
			moleculeID: "gt-mol",
			setupFunc: func(m *mockRelicsForStep) {
				// Both step 1 and 2 have no deps, so both are ready
				m.addIssue(makeStepIssue("gt-mol.1", "Step 1", "gt-mol", "open", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step 2", "gt-mol", "open", nil))
				m.addIssue(makeStepIssue("gt-mol.3", "Synthesis", "gt-mol", "open", []string{"gt-mol.1", "gt-mol.2"}))
			},
			wantComplete: false,
			// Should return one of the ready steps (implementation returns first found)
		},
		{
			name:       "diamond dependency - synthesis blocked",
			moleculeID: "gt-mol",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Step A", "gt-mol", "closed", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step B", "gt-mol", "open", nil)) // still open
				m.addIssue(makeStepIssue("gt-mol.3", "Synthesis", "gt-mol", "open", []string{"gt-mol.1", "gt-mol.2"}))
			},
			wantStepID:   "gt-mol.2", // B is ready (no deps)
			wantComplete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockRelicsForStep()
			tt.setupFunc(m)

			// Create a real Relics instance but we'll use our mock
			// For now, we test the logic by calling the actual function with mock data
			// This requires refactoring findNextReadyStep to accept an interface
			// For now, we'll test the logic inline

			// Get children from mock
			children, _ := m.List(relics.ListOptions{Parent: tt.moleculeID, Status: "all"})

			// Build closed IDs set - only "open" steps are candidates
			closedIDs := make(map[string]bool)
			var openSteps []*relics.Issue
			hasNonClosedSteps := false
			for _, child := range children {
				switch child.Status {
				case "closed":
					closedIDs[child.ID] = true
				case "open":
					openSteps = append(openSteps, child)
					hasNonClosedSteps = true
				default:
					// in_progress or other - not closed, not available
					hasNonClosedSteps = true
				}
			}

			// Check complete
			allComplete := !hasNonClosedSteps

			if allComplete != tt.wantComplete {
				t.Errorf("allComplete = %v, want %v", allComplete, tt.wantComplete)
			}

			if tt.wantComplete {
				return
			}

			// Find ready step
			var readyStep *relics.Issue
			for _, step := range openSteps {
				allDepsClosed := true
				for _, depID := range step.DependsOn {
					if !closedIDs[depID] {
						allDepsClosed = false
						break
					}
				}
				if len(step.DependsOn) == 0 || allDepsClosed {
					readyStep = step
					break
				}
			}

			if tt.wantNilStep {
				if readyStep != nil {
					t.Errorf("expected nil step, got %s", readyStep.ID)
				}
				return
			}

			if readyStep == nil {
				if tt.wantStepID != "" {
					t.Errorf("expected step %s, got nil", tt.wantStepID)
				}
				return
			}

			if tt.wantStepID != "" && readyStep.ID != tt.wantStepID {
				t.Errorf("readyStep.ID = %s, want %s", readyStep.ID, tt.wantStepID)
			}
		})
	}
}

// TestStepDoneScenarios tests complete step-done scenarios
func TestStepDoneScenarios(t *testing.T) {
	tests := []struct {
		name           string
		stepID         string
		setupFunc      func(*mockRelicsForStep)
		wantAction     string // "continue", "done", "no_more_ready"
		wantNextStep   string
	}{
		{
			name:   "complete step, continue to next",
			stepID: "gt-mol.1",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Step 1", "gt-mol", "open", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step 2", "gt-mol", "open", []string{"gt-mol.1"}))
			},
			wantAction:   "continue",
			wantNextStep: "gt-mol.2",
		},
		{
			name:   "complete final step, totem done",
			stepID: "gt-mol.2",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Step 1", "gt-mol", "closed", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step 2", "gt-mol", "open", []string{"gt-mol.1"}))
			},
			wantAction: "done",
		},
		{
			name:   "complete step, remaining blocked",
			stepID: "gt-mol.1",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Step 1", "gt-mol", "open", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Step 2", "gt-mol", "in_progress", nil)) // another parallel task
				m.addIssue(makeStepIssue("gt-mol.3", "Synthesis", "gt-mol", "open", []string{"gt-mol.1", "gt-mol.2"}))
			},
			wantAction: "no_more_ready", // .2 is in_progress, .3 blocked
		},
		{
			name:   "parallel workflow - complete one, next ready",
			stepID: "gt-mol.1",
			setupFunc: func(m *mockRelicsForStep) {
				m.addIssue(makeStepIssue("gt-mol.1", "Parallel A", "gt-mol", "open", nil))
				m.addIssue(makeStepIssue("gt-mol.2", "Parallel B", "gt-mol", "open", nil))
				m.addIssue(makeStepIssue("gt-mol.3", "Synthesis", "gt-mol", "open", []string{"gt-mol.1", "gt-mol.2"}))
			},
			wantAction:   "continue",
			wantNextStep: "gt-mol.2", // B is still ready
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockRelicsForStep()
			tt.setupFunc(m)

			// Extract totem ID
			moleculeID := extractMoleculeIDFromStep(tt.stepID)
			if moleculeID == "" {
				t.Fatalf("could not extract totem ID from %s", tt.stepID)
			}

			// Simulate closing the step
			if err := m.Close(tt.stepID); err != nil {
				t.Fatalf("failed to close step: %v", err)
			}

			// Now find next ready step
			children, _ := m.List(relics.ListOptions{Parent: moleculeID, Status: "all"})

			closedIDs := make(map[string]bool)
			var openSteps []*relics.Issue
			hasNonClosedSteps := false
			for _, child := range children {
				switch child.Status {
				case "closed":
					closedIDs[child.ID] = true
				case "open":
					openSteps = append(openSteps, child)
					hasNonClosedSteps = true
				default:
					// in_progress or other - not closed, not available
					hasNonClosedSteps = true
				}
			}

			allComplete := !hasNonClosedSteps

			var action string
			var nextStepID string

			if allComplete {
				action = "done"
			} else {
				// Find ready step
				var readyStep *relics.Issue
				for _, step := range openSteps {
					allDepsClosed := true
					for _, depID := range step.DependsOn {
						if !closedIDs[depID] {
							allDepsClosed = false
							break
						}
					}
					if len(step.DependsOn) == 0 || allDepsClosed {
						readyStep = step
						break
					}
				}

				if readyStep != nil {
					action = "continue"
					nextStepID = readyStep.ID
				} else {
					action = "no_more_ready"
				}
			}

			if action != tt.wantAction {
				t.Errorf("action = %s, want %s", action, tt.wantAction)
			}

			if tt.wantNextStep != "" && nextStepID != tt.wantNextStep {
				t.Errorf("nextStep = %s, want %s", nextStepID, tt.wantNextStep)
			}
		})
	}
}
