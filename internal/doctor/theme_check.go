package doctor

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/OWNER/horde/internal/tmux"
)

// ThemeCheck verifies tmux sessions have correct themes applied.
type ThemeCheck struct {
	FixableCheck
}

// NewThemeCheck creates a new theme check.
func NewThemeCheck() *ThemeCheck {
	return &ThemeCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "themes",
				CheckDescription: "Check tmux session theme configuration",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks if tmux sessions have themes applied correctly.
func (c *ThemeCheck) Run(ctx *CheckContext) *CheckResult {
	t := tmux.NewTmux()

	// List all sessions
	sessions, err := t.ListSessions()
	if err != nil {
		// No tmux server or error - not a problem, just skip
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No tmux sessions running",
		}
	}

	// Check for Horde sessions
	var gtSessions []string
	for _, s := range sessions {
		if strings.HasPrefix(s, "hd-") {
			gtSessions = append(gtSessions, s)
		}
	}

	if len(gtSessions) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No Horde sessions running",
		}
	}

	// Check if sessions have proper status-left format (no brackets = new format)
	var needsUpdate []string
	for _, session := range gtSessions {
		statusLeft, err := getSessionStatusLeft(session)
		if err != nil {
			continue
		}
		// Old format had brackets like [Warchief] or [horde/clan]
		if strings.Contains(statusLeft, "[") && strings.Contains(statusLeft, "]") {
			needsUpdate = append(needsUpdate, session)
		}
	}

	if len(needsUpdate) > 0 {
		details := make([]string, len(needsUpdate))
		for i, s := range needsUpdate {
			details[i] = fmt.Sprintf("Needs update: %s", s)
		}
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d session(s) have outdated theme format", len(needsUpdate)),
			Details: details,
			FixHint: "Run 'hd theme apply --all' or 'hd doctor --fix'",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("%d session(s) have correct themes", len(gtSessions)),
	}
}

// Fix applies themes to all sessions.
func (c *ThemeCheck) Fix(ctx *CheckContext) error {
	cmd := exec.Command("hd", "theme", "apply", "--all")
	cmd.Dir = ctx.TownRoot
	return cmd.Run()
}

// getSessionStatusLeft retrieves the status-left setting for a tmux session.
func getSessionStatusLeft(session string) (string, error) {
	cmd := exec.Command("tmux", "show-options", "-t", session, "status-left")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Parse: status-left "value"
	line := strings.TrimSpace(string(output))
	if idx := strings.Index(line, "\""); idx != -1 {
		end := strings.LastIndex(line, "\"")
		if end > idx {
			return line[idx+1 : end], nil
		}
	}
	return line, nil
}
