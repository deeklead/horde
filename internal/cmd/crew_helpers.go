package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/clan"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// inferRigFromCwd tries to determine the warband from the current directory.
func inferRigFromCwd(townRoot string) (string, error) {
	cwd, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}

	// Check if cwd is within a warband
	rel, err := filepath.Rel(townRoot, cwd)
	if err != nil {
		return "", fmt.Errorf("not in workspace")
	}

	// Normalize and split path - first component is the warband name
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")

	if len(parts) > 0 && parts[0] != "" && parts[0] != "." {
		return parts[0], nil
	}

	return "", fmt.Errorf("could not infer warband from current directory")
}

// getCrewManager returns a clan manager for the specified or inferred warband.
func getCrewManager(rigName string) (*clan.Manager, *warband.Warband, error) {
	// Handle optional warband inference from cwd
	if rigName == "" {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return nil, nil, fmt.Errorf("not in a Horde workspace: %w", err)
		}
		rigName, err = inferRigFromCwd(townRoot)
		if err != nil {
			return nil, nil, fmt.Errorf("could not determine warband (use --warband flag): %w", err)
		}
	}

	_, r, err := getRig(rigName)
	if err != nil {
		return nil, nil, err
	}

	crewGit := git.NewGit(r.Path)
	crewMgr := clan.NewManager(r, crewGit)

	return crewMgr, r, nil
}

// crewSessionName generates the tmux session name for a clan worker.
func crewSessionName(rigName, crewName string) string {
	return fmt.Sprintf("hd-%s-clan-%s", rigName, crewName)
}

// parseRigSlashName parses "warband/name" format into separate warband and name parts.
// Returns (warband, name, true) if the format matches, or ("", original, false) if not.
// Examples:
//   - "relics/emma" -> ("relics", "emma", true)
//   - "emma" -> ("", "emma", false)
//   - "relics/clan/emma" -> ("relics", "clan/emma", true) - only first slash splits
func parseRigSlashName(input string) (warband, name string, ok bool) {
	// Only split on first slash to handle edge cases
	idx := strings.Index(input, "/")
	if idx == -1 {
		return "", input, false
	}
	return input[:idx], input[idx+1:], true
}

// crewDetection holds the result of detecting clan workspace from cwd.
type crewDetection struct {
	rigName  string
	crewName string
}

// detectCrewFromCwd attempts to detect the clan workspace from the current directory.
// It looks for the pattern <encampment>/<warband>/clan/<name>/ in the current path.
func detectCrewFromCwd() (*crewDetection, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting cwd: %w", err)
	}

	// Find encampment root
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return nil, fmt.Errorf("not in Horde workspace: %w", err)
	}
	if townRoot == "" {
		return nil, fmt.Errorf("not in Horde workspace")
	}

	// Get relative path from encampment root
	relPath, err := filepath.Rel(townRoot, cwd)
	if err != nil {
		return nil, fmt.Errorf("getting relative path: %w", err)
	}

	// Normalize and split path
	relPath = filepath.ToSlash(relPath)
	parts := strings.Split(relPath, "/")

	// Look for pattern: <warband>/clan/<name>/...
	// Minimum: warband, clan, name = 3 parts
	if len(parts) < 3 {
		return nil, fmt.Errorf("not inside a clan workspace - specify the clan name or cd into a clan directory (e.g., horde/clan/max)")
	}

	rigName := parts[0]
	if parts[1] != "clan" {
		return nil, fmt.Errorf("not in a clan workspace (not in clan/ directory)")
	}
	crewName := parts[2]

	return &crewDetection{
		rigName:  rigName,
		crewName: crewName,
	}, nil
}

// isShellCommand checks if the command is a shell (meaning the runtime has exited).
func isShellCommand(cmd string) bool {
	shells := constants.SupportedShells
	for _, shell := range shells {
		if cmd == shell {
			return true
		}
	}
	return false
}

// execAgent execs the configured agent, replacing the current process.
// Used when we're already in the target session and just need to start the agent.
// If prompt is provided, it's passed as the initial prompt.
func execAgent(cfg *config.RuntimeConfig, prompt string) error {
	if cfg == nil {
		cfg = config.DefaultRuntimeConfig()
	}

	agentPath, err := exec.LookPath(cfg.Command)
	if err != nil {
		return fmt.Errorf("%s not found: %w", cfg.Command, err)
	}

	// exec replaces current process with agent
	// args[0] must be the command name (convention for exec)
	args := append([]string{cfg.Command}, cfg.Args...)
	if prompt != "" {
		args = append(args, prompt)
	}
	return syscall.Exec(agentPath, args, os.Environ())
}

// execRuntime execs the runtime CLI, replacing the current process.
// Used when we're already in the target session and just need to start the runtime.
// If prompt is provided, it's passed according to the runtime's prompt mode.
func execRuntime(prompt, rigPath, configDir string) error {
	runtimeConfig := config.LoadRuntimeConfig(rigPath)
	args := runtimeConfig.BuildArgsWithPrompt(prompt)
	if len(args) == 0 {
		return fmt.Errorf("runtime command not configured")
	}

	binPath, err := exec.LookPath(args[0])
	if err != nil {
		return fmt.Errorf("runtime command not found: %w", err)
	}

	env := os.Environ()
	if runtimeConfig.Session != nil && runtimeConfig.Session.ConfigDirEnv != "" && configDir != "" {
		env = append(env, fmt.Sprintf("%s=%s", runtimeConfig.Session.ConfigDirEnv, configDir))
	}

	return syscall.Exec(binPath, args, env)
}

// isInTmuxSession checks if we're currently inside the target tmux session.
func isInTmuxSession(targetSession string) bool {
	// TMUX env var format: /tmp/tmux-501/default,12345,0
	// We need to get the current session name via tmux display-message
	tmuxEnv := os.Getenv("TMUX")
	if tmuxEnv == "" {
		return false // Not in tmux at all
	}

	// Get current session name
	cmd := exec.Command("tmux", "display-message", "-p", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	currentSession := strings.TrimSpace(string(out))
	return currentSession == targetSession
}

// attachToTmuxSession attaches to a tmux session.
// Should only be called from outside tmux.
func attachToTmuxSession(sessionID string) error {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}

	cmd := exec.Command(tmuxPath, "summon-session", "-t", sessionID)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ensureDefaultBranch checks if a git directory is on the default branch.
// If not, warns the user and offers to switch.
// Returns true if on default branch (or switched to it), false if user declined.
// The rigPath parameter is used to look up the configured default branch.
func ensureDefaultBranch(dir, roleName, rigPath string) bool { //nolint:unparam // bool return kept for future callers to check
	g := git.NewGit(dir)

	branch, err := g.CurrentBranch()
	if err != nil {
		// Not a git repo or other error, skip check
		return true
	}

	// Get configured default branch for this warband
	defaultBranch := "main" // fallback
	if rigCfg, err := warband.LoadRigConfig(rigPath); err == nil && rigCfg.DefaultBranch != "" {
		defaultBranch = rigCfg.DefaultBranch
	}

	if branch == defaultBranch || branch == "master" {
		return true
	}

	// Warn about wrong branch
	fmt.Printf("\n%s %s is on branch '%s', not %s\n",
		style.Warning.Render("⚠"),
		roleName,
		branch,
		defaultBranch)
	fmt.Printf("  Persistent roles should work on %s to avoid orphaned work.\n", defaultBranch)
	fmt.Println()

	// Auto-switch to default branch
	fmt.Printf("  Switching to %s...\n", defaultBranch)
	if err := g.Checkout(defaultBranch); err != nil {
		fmt.Printf("  %s Could not switch to %s: %v\n", style.Error.Render("✗"), defaultBranch, err)
		fmt.Printf("  Please manually run: git checkout %s && git pull\n", defaultBranch)
		return false
	}

	// Pull latest
	if err := g.Pull("origin", defaultBranch); err != nil {
		fmt.Printf("  %s Pull failed (continuing anyway): %v\n", style.Warning.Render("⚠"), err)
	} else {
		fmt.Printf("  %s Switched to %s and pulled latest\n", style.Success.Render("✓"), defaultBranch)
	}

	return true
}

// parseCrewSessionName extracts warband and clan name from a tmux session name.
// Format: gt-<warband>-clan-<name>
// Returns empty strings and false if the format doesn't match.
func parseCrewSessionName(sessionName string) (rigName, crewName string, ok bool) {
	// Must start with "hd-" and contain "-clan-"
	if !strings.HasPrefix(sessionName, "hd-") {
		return "", "", false
	}

	// Remove "hd-" prefix
	rest := sessionName[3:]

	// Find "-clan-" separator
	idx := strings.Index(rest, "-clan-")
	if idx == -1 {
		return "", "", false
	}

	rigName = rest[:idx]
	crewName = rest[idx+6:] // len("-clan-") = 6

	if rigName == "" || crewName == "" {
		return "", "", false
	}

	return rigName, crewName, true
}

// findRigCrewSessions returns all clan sessions for a given warband, sorted alphabetically.
// Uses tmux list-sessions to find sessions matching gt-<warband>-clan-* pattern.
func findRigCrewSessions(rigName string) ([]string, error) { //nolint:unparam // error return kept for future use
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		// No tmux server or no sessions
		return nil, nil
	}

	prefix := fmt.Sprintf("hd-%s-clan-", rigName)
	var sessions []string

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, prefix) {
			sessions = append(sessions, line)
		}
	}

	// Sessions are already sorted by tmux, but sort explicitly for consistency
	// (alphabetical by session name means alphabetical by clan name)
	return sessions, nil
}
