package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// branchInfo holds parsed branch information.
type branchInfo struct {
	Branch string // Full branch name
	Issue  string // Issue ID extracted from branch
	Worker string // Worker name (raider name)
}

// parseBranchName extracts issue ID and worker from a branch name.
// Supports formats:
//   - raider/<worker>/<issue>  → issue=<issue>, worker=<worker>
//   - <issue>                   → issue=<issue>, worker=""
func parseBranchName(branch string) branchInfo {
	info := branchInfo{Branch: branch}

	// Try raider/<worker>/<issue> format
	if strings.HasPrefix(branch, constants.BranchRaiderPrefix) {
		parts := strings.SplitN(branch, "/", 3)
		if len(parts) == 3 {
			info.Worker = parts[1]
			info.Issue = parts[2]
			return info
		}
	}

	// Try to find an issue ID pattern in the branch name
	// Common patterns: prefix-xxx, prefix-xxx.n (subtask)
	issuePattern := regexp.MustCompile(`([a-z]+-[a-z0-9]+(?:\.[0-9]+)?)`)
	if matches := issuePattern.FindStringSubmatch(branch); len(matches) > 1 {
		info.Issue = matches[1]
	}

	return info
}

func runMqSubmit(cmd *cobra.Command, args []string) error {
	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Find current warband
	rigName, _, err := findCurrentRig(townRoot)
	if err != nil {
		return err
	}

	// Initialize git for the current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}
	g := git.NewGit(cwd)

	// Get current branch
	branch := mqSubmitBranch
	if branch == "" {
		branch, err = g.CurrentBranch()
		if err != nil {
			return fmt.Errorf("getting current branch: %w", err)
		}
	}

	// Get configured default branch for this warband
	defaultBranch := "main" // fallback
	if rigCfg, err := warband.LoadRigConfig(filepath.Join(townRoot, rigName)); err == nil && rigCfg.DefaultBranch != "" {
		defaultBranch = rigCfg.DefaultBranch
	}

	if branch == defaultBranch || branch == "master" {
		return fmt.Errorf("cannot submit %s/master branch to merge queue", defaultBranch)
	}

	// Parse branch info
	info := parseBranchName(branch)

	// Override with explicit flags
	issueID := mqSubmitIssue
	if issueID == "" {
		issueID = info.Issue
	}
	worker := info.Worker

	if issueID == "" {
		return fmt.Errorf("cannot determine source issue from branch '%s'; use --issue to specify", branch)
	}

	// Initialize relics for looking up source issue
	bd := relics.New(cwd)

	// Determine target branch
	target := defaultBranch
	if mqSubmitEpic != "" {
		// Explicit --epic flag takes precedence
		target = "integration/" + mqSubmitEpic
	} else {
		// Auto-detect: check if source issue has a parent epic with an integration branch
		autoTarget, err := detectIntegrationBranch(bd, g, issueID)
		if err != nil {
			// Non-fatal: log and continue with default branch as target
			fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("(note: %v)", err)))
		} else if autoTarget != "" {
			target = autoTarget
		}
	}

	// Get source issue for priority inheritance
	var priority int
	if mqSubmitPriority >= 0 {
		priority = mqSubmitPriority
	} else {
		// Try to inherit from source issue
		sourceIssue, err := bd.Show(issueID)
		if err != nil {
			// Issue not found, use default priority
			priority = 2
		} else {
			priority = sourceIssue.Priority
		}
	}

	// Build MR bead title and description
	title := fmt.Sprintf("Merge: %s", issueID)
	description := fmt.Sprintf("branch: %s\ntarget: %s\nsource_issue: %s\nrig: %s",
		branch, target, issueID, rigName)
	if worker != "" {
		description += fmt.Sprintf("\nworker: %s", worker)
	}

	// Create MR bead (ephemeral wisp - will be cleaned up after merge)
	mrIssue, err := bd.Create(relics.CreateOptions{
		Title:       title,
		Type:        "merge-request",
		Priority:    priority,
		Description: description,
	})
	if err != nil {
		return fmt.Errorf("creating merge request bead: %w", err)
	}

	// Success output
	fmt.Printf("%s Submitted to merge queue\n", style.Bold.Render("✓"))
	fmt.Printf("  MR ID: %s\n", style.Bold.Render(mrIssue.ID))
	fmt.Printf("  Source: %s\n", branch)
	fmt.Printf("  Target: %s\n", target)
	fmt.Printf("  Issue: %s\n", issueID)
	if worker != "" {
		fmt.Printf("  Worker: %s\n", worker)
	}
	fmt.Printf("  Priority: P%d\n", priority)

	// Auto-cleanup for raiders: if this is a raider branch and cleanup not disabled,
	// send lifecycle request and wait for termination
	if worker != "" && !mqSubmitNoCleanup {
		fmt.Println()
		fmt.Printf("%s Auto-cleanup: raider work submitted\n", style.Bold.Render("✓"))
		if err := raiderCleanup(rigName, worker, townRoot); err != nil {
			// Non-fatal: warn but return success (MR was created)
			style.PrintWarning("Could not auto-cleanup: %v", err)
			fmt.Println(style.Dim.Render("  You may need to run 'hd handoff --shutdown' manually"))
			return nil
		}
		// raiderCleanup blocks forever waiting for termination, so we never reach here
	}

	return nil
}

// detectIntegrationBranch checks if an issue is a child of an epic that has an integration branch.
// Returns the integration branch target (e.g., "integration/gt-epic") if found, or "" if not.
func detectIntegrationBranch(bd *relics.Relics, g *git.Git, issueID string) (string, error) {
	// Get the source issue
	issue, err := bd.Show(issueID)
	if err != nil {
		return "", fmt.Errorf("looking up issue %s: %w", issueID, err)
	}

	// Check if issue has a parent
	if issue.Parent == "" {
		return "", nil // No parent, no integration branch
	}

	// Get the parent issue
	parent, err := bd.Show(issue.Parent)
	if err != nil {
		return "", fmt.Errorf("looking up parent %s: %w", issue.Parent, err)
	}

	// Check if parent is an epic
	if parent.Type != "epic" {
		return "", nil // Parent is not an epic
	}

	// Check if integration branch exists
	integrationBranch := "integration/" + parent.ID

	// Check local first (faster)
	exists, err := g.BranchExists(integrationBranch)
	if err != nil {
		return "", fmt.Errorf("checking local branch: %w", err)
	}
	if exists {
		return integrationBranch, nil
	}

	// Check remote
	exists, err = g.RemoteBranchExists("origin", integrationBranch)
	if err != nil {
		// Remote check failure is non-fatal
		return "", nil
	}
	if exists {
		return integrationBranch, nil
	}

	return "", nil // No integration branch found
}

// raiderCleanup sends a lifecycle shutdown request to the witness and waits for termination.
// This is called after a raider successfully submits an MR.
func raiderCleanup(rigName, worker, townRoot string) error {
	// Send lifecycle request to witness
	manager := rigName + "/witness"
	subject := fmt.Sprintf("LIFECYCLE: raider-%s requesting shutdown", worker)
	body := fmt.Sprintf(`Lifecycle request from raider %s.

Action: shutdown
Reason: MR submitted to merge queue
Time: %s

Please verify state and execute lifecycle action.
`, worker, time.Now().Format(time.RFC3339))

	// Send via hd drums
	cmd := exec.Command("hd", "drums", "send", manager,
		"-s", subject,
		"-m", body,
	)
	cmd.Dir = townRoot

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sending lifecycle request: %w: %s", err, string(out))
	}
	fmt.Printf("%s Sent shutdown request to %s\n", style.Bold.Render("✓"), manager)

	// Wait for retirement with periodic status
	fmt.Println()
	fmt.Printf("%s Waiting for retirement...\n", style.Dim.Render("◌"))
	fmt.Println(style.Dim.Render("(Witness will terminate this session)"))

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	waitStart := time.Now()
	for {
		select {
		case <-ticker.C:
			elapsed := time.Since(waitStart).Round(time.Second)
			fmt.Printf("%s Still waiting (%v elapsed)...\n", style.Dim.Render("◌"), elapsed)
			if elapsed >= 2*time.Minute {
				fmt.Println(style.Dim.Render("  Hint: If witness isn't responding, you may need to:"))
				fmt.Println(style.Dim.Render("  - Check if witness is running"))
				fmt.Println(style.Dim.Render("  - Use Ctrl+C to abort and manually exit"))
			}
		}
	}
}
