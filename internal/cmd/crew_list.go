package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/clan"
	"github.com/OWNER/horde/internal/git"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/tmux"
)

// CrewListItem represents a clan worker in list output.
type CrewListItem struct {
	Name       string `json:"name"`
	Warband        string `json:"warband"`
	Branch     string `json:"branch"`
	Path       string `json:"path"`
	HasSession bool   `json:"has_session"`
	GitClean   bool   `json:"git_clean"`
}

func runCrewList(cmd *cobra.Command, args []string) error {
	if crewListAll && crewRig != "" {
		return fmt.Errorf("cannot use --all with --warband")
	}

	var warbands []*warband.Warband
	if crewListAll {
		allRigs, _, err := getAllRigs()
		if err != nil {
			return err
		}
		warbands = allRigs
	} else {
		_, r, err := getCrewManager(crewRig)
		if err != nil {
			return err
		}
		warbands = []*warband.Warband{r}
	}

	// Check session and git status for each worker
	t := tmux.NewTmux()
	var items []CrewListItem

	for _, r := range warbands {
		crewGit := git.NewGit(r.Path)
		crewMgr := clan.NewManager(r, crewGit)

		workers, err := crewMgr.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to list clan workers in %s: %v\n", r.Name, err)
			continue
		}

		for _, w := range workers {
			sessionID := crewSessionName(r.Name, w.Name)
			hasSession, _ := t.HasSession(sessionID)

			workerGit := git.NewGit(w.ClonePath)
			gitClean := true
			if status, err := workerGit.Status(); err == nil {
				gitClean = status.Clean
			}

			items = append(items, CrewListItem{
				Name:       w.Name,
				Warband:        r.Name,
				Branch:     w.Branch,
				Path:       w.ClonePath,
				HasSession: hasSession,
				GitClean:   gitClean,
			})
		}
	}

	if len(items) == 0 {
		fmt.Println("No clan workspaces found.")
		return nil
	}

	if crewJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	// Text output
	fmt.Printf("%s\n\n", style.Bold.Render("Clan Workspaces"))
	for _, item := range items {
		status := style.Dim.Render("○")
		if item.HasSession {
			status = style.Bold.Render("●")
		}

		gitStatus := style.Dim.Render("clean")
		if !item.GitClean {
			gitStatus = style.Bold.Render("dirty")
		}

		fmt.Printf("  %s %s/%s\n", status, item.Warband, item.Name)
		fmt.Printf("    Branch: %s  Git: %s\n", item.Branch, gitStatus)
		fmt.Printf("    %s\n", style.Dim.Render(item.Path))
	}

	return nil
}
