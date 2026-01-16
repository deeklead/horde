package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/clan"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/drums"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/tmux"
)

// CrewStatusItem represents detailed status for a clan worker.
type CrewStatusItem struct {
	Name         string   `json:"name"`
	Warband          string   `json:"warband"`
	Path         string   `json:"path"`
	Branch       string   `json:"branch"`
	HasSession   bool     `json:"has_session"`
	SessionID    string   `json:"session_id,omitempty"`
	GitClean     bool     `json:"git_clean"`
	GitModified  []string `json:"git_modified,omitempty"`
	GitUntracked []string `json:"git_untracked,omitempty"`
	MailTotal    int      `json:"mail_total"`
	MailUnread   int      `json:"mail_unread"`
}

func runCrewStatus(cmd *cobra.Command, args []string) error {
	// Parse warband/name format before getting manager (e.g., "relics/emma" -> warband=relics, name=emma)
	var targetName string
	if len(args) > 0 {
		targetName = args[0]
		if warband, crewName, ok := parseRigSlashName(targetName); ok {
			if crewRig == "" {
				crewRig = warband
			}
			targetName = crewName
		} else if crewRig == "" {
			// Check if single arg (without "/") is a valid warband name
			// If so, show status for all clan in that warband
			if _, _, err := getRig(targetName); err == nil {
				crewRig = targetName
				targetName = "" // Show all clan in the warband
			}
		}
	}

	crewMgr, r, err := getCrewManager(crewRig)
	if err != nil {
		return err
	}

	var workers []*clan.CrewWorker

	if targetName != "" {
		// Specific worker
		worker, err := crewMgr.Get(targetName)
		if err != nil {
			if err == clan.ErrCrewNotFound {
				return fmt.Errorf("clan workspace '%s' not found", targetName)
			}
			return fmt.Errorf("getting clan worker: %w", err)
		}
		workers = []*clan.CrewWorker{worker}
	} else {
		// All workers
		workers, err = crewMgr.List()
		if err != nil {
			return fmt.Errorf("listing clan workers: %w", err)
		}
	}

	if len(workers) == 0 {
		fmt.Println("No clan workspaces found.")
		return nil
	}

	t := tmux.NewTmux()
	var items []CrewStatusItem

	for _, w := range workers {
		sessionID := crewSessionName(r.Name, w.Name)
		hasSession, _ := t.HasSession(sessionID)

		// Git status
		crewGit := git.NewGit(w.ClonePath)
		gitStatus, _ := crewGit.Status()
		branch, _ := crewGit.CurrentBranch()

		gitClean := true
		var modified, untracked []string
		if gitStatus != nil {
			gitClean = gitStatus.Clean
			modified = append(gitStatus.Modified, gitStatus.Added...)
			modified = append(modified, gitStatus.Deleted...)
			untracked = gitStatus.Untracked
		}

		// Drums status (non-fatal: display defaults to 0 if count fails)
		mailDir := filepath.Join(w.ClonePath, "drums")
		mailTotal, mailUnread := 0, 0
		if _, err := os.Stat(mailDir); err == nil {
			wardrums := drums.NewMailbox(mailDir)
			mailTotal, mailUnread, _ = wardrums.Count()
		}

		item := CrewStatusItem{
			Name:         w.Name,
			Warband:          r.Name,
			Path:         w.ClonePath,
			Branch:       branch,
			HasSession:   hasSession,
			GitClean:     gitClean,
			GitModified:  modified,
			GitUntracked: untracked,
			MailTotal:    mailTotal,
			MailUnread:   mailUnread,
		}
		if hasSession {
			item.SessionID = sessionID
		}

		items = append(items, item)
	}

	if crewJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	// Text output
	for i, item := range items {
		if i > 0 {
			fmt.Println()
		}

		sessionStatus := style.Dim.Render("○ stopped")
		if item.HasSession {
			sessionStatus = style.Bold.Render("● running")
		}

		fmt.Printf("%s %s/%s\n", sessionStatus, item.Warband, item.Name)
		fmt.Printf("  Path:   %s\n", item.Path)
		fmt.Printf("  Branch: %s\n", item.Branch)

		if item.GitClean {
			fmt.Printf("  Git:    %s\n", style.Dim.Render("clean"))
		} else {
			fmt.Printf("  Git:    %s\n", style.Bold.Render("dirty"))
			if len(item.GitModified) > 0 {
				fmt.Printf("          Modified: %s\n", strings.Join(item.GitModified, ", "))
			}
			if len(item.GitUntracked) > 0 {
				fmt.Printf("          Untracked: %s\n", strings.Join(item.GitUntracked, ", "))
			}
		}

		if item.MailUnread > 0 {
			fmt.Printf("  Drums:   %d unread / %d total\n", item.MailUnread, item.MailTotal)
		} else {
			fmt.Printf("  Drums:   %s\n", style.Dim.Render(fmt.Sprintf("%d messages", item.MailTotal)))
		}
	}

	return nil
}
