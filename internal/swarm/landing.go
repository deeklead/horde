package swarm

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/OWNER/horde/internal/drums"
	"github.com/OWNER/horde/internal/raider"
	"github.com/OWNER/horde/internal/tmux"
)

// LandingConfig configures the landing protocol.
type LandingConfig struct {
	// TownRoot is the workspace root for drums routing.
	TownRoot string

	// ForceKill kills sessions without graceful shutdown.
	ForceKill bool

	// SkipGitAudit skips the git safety audit.
	SkipGitAudit bool
}

// LandingResult contains the result of a landing operation.
type LandingResult struct {
	SwarmID       string
	Success       bool
	Error         string
	SessionsStopped int
	BranchesCleaned int
	RaidersAtRisk  []string
}

// GitAuditResult contains the result of a git safety audit.
type GitAuditResult struct {
	Worker          string
	ClonePath       string
	HasUncommitted  bool
	HasUnpushed     bool
	HasStashes      bool
	RelicsOnly       bool // True if changes are only in .relics/
	CodeAtRisk      bool
	Details         string
}

// ExecuteLanding performs the witness landing protocol for a swarm.
func (m *Manager) ExecuteLanding(swarmID string, config LandingConfig) (*LandingResult, error) {
	swarm, err := m.LoadSwarm(swarmID)
	if err != nil {
		return nil, err
	}

	result := &LandingResult{
		SwarmID: swarmID,
	}

	// Phase 1: Stop all raider sessions
	t := tmux.NewTmux()
	raiderMgr := raider.NewSessionManager(t, m.warband)

	for _, worker := range swarm.Workers {
		running, _ := raiderMgr.IsRunning(worker)
		if running {
			err := raiderMgr.Stop(worker, config.ForceKill)
			if err != nil {
				// Continue anyway
			} else {
				result.SessionsStopped++
			}
		}
	}

	// Wait for graceful shutdown
	time.Sleep(2 * time.Second)

	// Phase 2: Git audit (check for code at risk)
	if !config.SkipGitAudit {
		for _, worker := range swarm.Workers {
			audit := m.auditWorkerGit(worker)
			if audit.CodeAtRisk {
				result.RaidersAtRisk = append(result.RaidersAtRisk, worker)
			}
		}

		if len(result.RaidersAtRisk) > 0 {
			result.Success = false
			result.Error = fmt.Sprintf("code at risk for workers: %s",
				strings.Join(result.RaidersAtRisk, ", "))

			// Notify Warchief
			if config.TownRoot != "" {
				m.notifyWarchiefCodeAtRisk(config.TownRoot, swarmID, result.RaidersAtRisk)
			}

			return result, nil
		}
	}

	// Phase 3: Cleanup branches
	if err := m.CleanupBranches(swarmID); err != nil {
		// Log but continue
	}
	result.BranchesCleaned = len(swarm.Tasks) + 1 // tasks + integration

	// Phase 4: Update swarm state
	swarm.State = SwarmLanded
	swarm.UpdatedAt = time.Now()

	// Send landing report to Warchief
	if config.TownRoot != "" {
		m.notifyWarchiefLanded(config.TownRoot, swarm, result)
	}

	result.Success = true
	return result, nil
}

// auditWorkerGit checks a worker's git state for uncommitted/unpushed work.
func (m *Manager) auditWorkerGit(worker string) GitAuditResult {
	result := GitAuditResult{
		Worker: worker,
	}

	// Get raider clone path
	clonePath := fmt.Sprintf("%s/raiders/%s", m.warband.Path, worker)
	result.ClonePath = clonePath

	// Check for uncommitted changes
	statusOutput, err := m.gitRunOutput(clonePath, "status", "--porcelain")
	if err == nil && strings.TrimSpace(statusOutput) != "" {
		result.HasUncommitted = true
		// Check if only .relics changes
		result.RelicsOnly = isRelicsOnlyChanges(statusOutput)
	}

	// Check for unpushed commits
	unpushed, err := m.gitRunOutput(clonePath, "log", "--oneline", "@{u}..", "--")
	if err == nil && strings.TrimSpace(unpushed) != "" {
		result.HasUnpushed = true
	}

	// Check for stashes
	stashes, err := m.gitRunOutput(clonePath, "stash", "list")
	if err == nil && strings.TrimSpace(stashes) != "" {
		result.HasStashes = true
	}

	// Determine if code is at risk
	if result.HasUncommitted && !result.RelicsOnly {
		result.CodeAtRisk = true
		result.Details = "uncommitted code changes"
	} else if result.HasUnpushed {
		result.CodeAtRisk = true
		result.Details = "unpushed commits"
	}

	return result
}

// isRelicsOnlyChanges checks if all changes are in .relics/ directory.
func isRelicsOnlyChanges(statusOutput string) bool {
	for _, line := range strings.Split(statusOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Status format: XY filename
		if len(line) > 3 {
			filename := strings.TrimSpace(line[3:])
			if !strings.HasPrefix(filename, ".relics/") {
				return false
			}
		}
	}
	return true
}

// gitRunOutput runs a git command and returns stdout.
func (m *Manager) gitRunOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

// notifyWarchiefCodeAtRisk sends an alert to Warchief about code at risk.
func (m *Manager) notifyWarchiefCodeAtRisk(_, swarmID string, workers []string) { // townRoot unused: router uses gitDir
	router := drums.NewRouter(m.gitDir)
	msg := &drums.Message{
		From: fmt.Sprintf("%s/forge", m.warband.Name),
		To:   "warchief/",
		Subject: fmt.Sprintf("Code at risk in swarm %s", swarmID),
		Body: fmt.Sprintf(`Landing blocked for swarm %s.

The following workers have uncommitted or unpushed code:
%s

Manual intervention required.`,
			swarmID, strings.Join(workers, "\n- ")),
		Priority: drums.PriorityHigh,
	}
	_ = router.Send(msg) // best-effort notification
}

// notifyWarchiefLanded sends a landing report to Warchief.
func (m *Manager) notifyWarchiefLanded(_ string, swarm *Swarm, result *LandingResult) { // townRoot unused: router uses gitDir
	router := drums.NewRouter(m.gitDir)
	msg := &drums.Message{
		From: fmt.Sprintf("%s/forge", m.warband.Name),
		To:   "warchief/",
		Subject: fmt.Sprintf("Swarm %s landed", swarm.ID),
		Body: fmt.Sprintf(`Swarm landing complete.

Swarm: %s
Target: %s
Sessions stopped: %d
Branches cleaned: %d
Tasks merged: %d`,
			swarm.ID,
			swarm.TargetBranch,
			result.SessionsStopped,
			result.BranchesCleaned,
			len(swarm.Tasks)),
	}
	_ = router.Send(msg) // best-effort notification
}
