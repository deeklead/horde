package cmd

import (
	"fmt"
	"strings"

	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/raider"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/style"
)

// raiderTarget represents a raider to operate on.
type raiderTarget struct {
	rigName     string
	raiderName string
	mgr         *raider.Manager
	r           *warband.Warband
}

// resolveRaiderTargets builds a list of raiders from command args.
// If useAll is true, the first arg is treated as a warband name and all raiders in it are returned.
// Otherwise, args are parsed as warband/raider addresses.
func resolveRaiderTargets(args []string, useAll bool) ([]raiderTarget, error) {
	var targets []raiderTarget

	if useAll {
		// --all flag: first arg is just the warband name
		rigName := args[0]
		// Check if it looks like warband/raider format
		if _, _, err := parseAddress(rigName); err == nil {
			return nil, fmt.Errorf("with --all, provide just the warband name (e.g., 'hd raider <cmd> %s --all')", strings.Split(rigName, "/")[0])
		}

		mgr, r, err := getRaiderManager(rigName)
		if err != nil {
			return nil, err
		}

		raiders, err := mgr.List()
		if err != nil {
			return nil, fmt.Errorf("listing raiders: %w", err)
		}

		for _, p := range raiders {
			targets = append(targets, raiderTarget{
				rigName:     rigName,
				raiderName: p.Name,
				mgr:         mgr,
				r:           r,
			})
		}
	} else {
		// Multiple warband/raider arguments - require explicit warband/raider format
		for _, arg := range args {
			// Validate format: must contain "/" to avoid misinterpreting warband names as raider names
			if !strings.Contains(arg, "/") {
				return nil, fmt.Errorf("invalid address '%s': must be in 'warband/raider' format (e.g., 'horde/Toast')", arg)
			}

			rigName, raiderName, err := parseAddress(arg)
			if err != nil {
				return nil, fmt.Errorf("invalid address '%s': %w", arg, err)
			}

			mgr, r, err := getRaiderManager(rigName)
			if err != nil {
				return nil, err
			}

			targets = append(targets, raiderTarget{
				rigName:     rigName,
				raiderName: raiderName,
				mgr:         mgr,
				r:           r,
			})
		}
	}

	return targets, nil
}

// SafetyCheckResult holds the result of safety checks for a raider.
type SafetyCheckResult struct {
	Raider       string
	Blocked       bool
	Reasons       []string
	CleanupStatus raider.CleanupStatus
	BannerBead      string
	HookStale     bool // true if bannered bead is closed
	OpenMR        string
	GitState      *GitState
}

// checkRaiderSafety performs safety checks before destructive operations.
// Returns nil if the raider is safe to operate on, or a SafetyCheckResult with reasons if blocked.
func checkRaiderSafety(target raiderTarget) *SafetyCheckResult {
	result := &SafetyCheckResult{
		Raider: fmt.Sprintf("%s/%s", target.rigName, target.raiderName),
	}

	// Get raider info for branch name
	raiderInfo, infoErr := target.mgr.Get(target.raiderName)

	// Check 1: Unpushed commits via cleanup_status or git state
	bd := relics.New(target.r.Path)
	agentBeadID := relics.RaiderBeadID(target.rigName, target.raiderName)
	agentIssue, fields, err := bd.GetAgentBead(agentBeadID)

	if err != nil || fields == nil {
		// No agent bead - fall back to git check
		if infoErr == nil && raiderInfo != nil {
			gitState, gitErr := getGitState(raiderInfo.ClonePath)
			result.GitState = gitState
			if gitErr != nil {
				result.Reasons = append(result.Reasons, "cannot check git state")
			} else if !gitState.Clean {
				if gitState.UnpushedCommits > 0 {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has %d unpushed commit(s)", gitState.UnpushedCommits))
				} else if len(gitState.UncommittedFiles) > 0 {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has %d uncommitted file(s)", len(gitState.UncommittedFiles)))
				} else if gitState.StashCount > 0 {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has %d stash(es)", gitState.StashCount))
				}
			}
		}
	} else {
		// Check cleanup_status from agent bead
		result.CleanupStatus = raider.CleanupStatus(fields.CleanupStatus)
		switch result.CleanupStatus {
		case raider.CleanupClean:
			// OK
		case raider.CleanupUnpushed:
			result.Reasons = append(result.Reasons, "has unpushed commits")
		case raider.CleanupUncommitted:
			result.Reasons = append(result.Reasons, "has uncommitted changes")
		case raider.CleanupStash:
			result.Reasons = append(result.Reasons, "has stashed changes")
		case raider.CleanupUnknown, "":
			result.Reasons = append(result.Reasons, "cleanup status unknown")
		default:
			result.Reasons = append(result.Reasons, fmt.Sprintf("cleanup status: %s", result.CleanupStatus))
		}

		// Check 3: Work on hook
		bannerBead := agentIssue.BannerBead
		if bannerBead == "" {
			bannerBead = fields.BannerBead
		}
		if bannerBead != "" {
			result.BannerBead = bannerBead
			// Check if bannered bead is still active (not closed)
			hookedIssue, err := bd.Show(bannerBead)
			if err == nil && hookedIssue != nil {
				if hookedIssue.Status != "closed" {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has work on hook (%s)", bannerBead))
				} else {
					result.HookStale = true
				}
			} else {
				result.Reasons = append(result.Reasons, fmt.Sprintf("has work on hook (%s, unverified)", bannerBead))
			}
		}
	}

	// Check 2: Open MR relics for this branch
	if infoErr == nil && raiderInfo != nil && raiderInfo.Branch != "" {
		mr, mrErr := bd.FindMRForBranch(raiderInfo.Branch)
		if mrErr == nil && mr != nil {
			result.OpenMR = mr.ID
			result.Reasons = append(result.Reasons, fmt.Sprintf("has open MR (%s)", mr.ID))
		}
	}

	result.Blocked = len(result.Reasons) > 0
	return result
}

// displaySafetyCheckBlocked prints blocked raiders and guidance.
func displaySafetyCheckBlocked(blocked []*SafetyCheckResult) {
	fmt.Printf("%s Cannot nuke the following raiders:\n\n", style.Error.Render("Error:"))
	var raiderList []string
	for _, b := range blocked {
		fmt.Printf("  %s:\n", style.Bold.Render(b.Raider))
		for _, r := range b.Reasons {
			fmt.Printf("    - %s\n", r)
		}
		raiderList = append(raiderList, b.Raider)
	}
	fmt.Println()
	fmt.Println("Safety checks failed. Resolve issues before nuking, or use --force.")
	fmt.Println("Options:")
	fmt.Printf("  1. Complete work: hd done (from raider session)\n")
	fmt.Printf("  2. Push changes: git push (from raider worktree)\n")
	fmt.Printf("  3. Escalate: hd drums send warchief/ -s \"RECOVERY_NEEDED\" -m \"...\"\n")
	fmt.Printf("  4. Force nuke (LOSES WORK): hd raider nuke --force %s\n", strings.Join(raiderList, " "))
	fmt.Println()
}

// displayDryRunSafetyCheck shows safety check status for dry-run mode.
func displayDryRunSafetyCheck(target raiderTarget) {
	fmt.Printf("\n  Safety checks:\n")
	raiderInfo, infoErr := target.mgr.Get(target.raiderName)
	bd := relics.New(target.r.Path)
	agentBeadID := relics.RaiderBeadID(target.rigName, target.raiderName)
	agentIssue, fields, err := bd.GetAgentBead(agentBeadID)

	// Check 1: Git state
	if err != nil || fields == nil {
		if infoErr == nil && raiderInfo != nil {
			gitState, gitErr := getGitState(raiderInfo.ClonePath)
			if gitErr != nil {
				fmt.Printf("    - Git state: %s\n", style.Warning.Render("cannot check"))
			} else if gitState.Clean {
				fmt.Printf("    - Git state: %s\n", style.Success.Render("clean"))
			} else {
				fmt.Printf("    - Git state: %s\n", style.Error.Render("dirty"))
			}
		} else {
			fmt.Printf("    - Git state: %s\n", style.Dim.Render("unknown (no raider info)"))
		}
		fmt.Printf("    - Hook: %s\n", style.Dim.Render("unknown (no agent bead)"))
	} else {
		cleanupStatus := raider.CleanupStatus(fields.CleanupStatus)
		if cleanupStatus.IsSafe() {
			fmt.Printf("    - Git state: %s\n", style.Success.Render("clean"))
		} else if cleanupStatus.RequiresRecovery() {
			fmt.Printf("    - Git state: %s (%s)\n", style.Error.Render("dirty"), cleanupStatus)
		} else {
			fmt.Printf("    - Git state: %s\n", style.Warning.Render("unknown"))
		}

		bannerBead := agentIssue.BannerBead
		if bannerBead == "" {
			bannerBead = fields.BannerBead
		}
		if bannerBead != "" {
			hookedIssue, err := bd.Show(bannerBead)
			if err == nil && hookedIssue != nil && hookedIssue.Status == "closed" {
				fmt.Printf("    - Hook: %s (%s, closed - stale)\n", style.Warning.Render("stale"), bannerBead)
			} else {
				fmt.Printf("    - Hook: %s (%s)\n", style.Error.Render("has work"), bannerBead)
			}
		} else {
			fmt.Printf("    - Hook: %s\n", style.Success.Render("empty"))
		}
	}

	// Check 2: Open MR
	if infoErr == nil && raiderInfo != nil && raiderInfo.Branch != "" {
		mr, mrErr := bd.FindMRForBranch(raiderInfo.Branch)
		if mrErr == nil && mr != nil {
			fmt.Printf("    - Open MR: %s (%s)\n", style.Error.Render("yes"), mr.ID)
		} else {
			fmt.Printf("    - Open MR: %s\n", style.Success.Render("none"))
		}
	} else {
		fmt.Printf("    - Open MR: %s\n", style.Dim.Render("unknown (no branch info)"))
	}
}
