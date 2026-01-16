package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/clan"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/tmux"
)

func runCrewRename(cmd *cobra.Command, args []string) error {
	oldName := args[0]
	newName := args[1]
	// Parse warband/name format for oldName (e.g., "relics/emma" -> warband=relics, name=emma)
	if warband, crewName, ok := parseRigSlashName(oldName); ok {
		if crewRig == "" {
			crewRig = warband
		}
		oldName = crewName
	}
	// Note: newName is just the new name, no warband prefix expected

	crewMgr, r, err := getCrewManager(crewRig)
	if err != nil {
		return err
	}

	// Kill any running session for the old name
	t := tmux.NewTmux()
	oldSessionID := crewSessionName(r.Name, oldName)
	if hasSession, _ := t.HasSession(oldSessionID); hasSession {
		if err := t.KillSession(oldSessionID); err != nil {
			return fmt.Errorf("killing old session: %w", err)
		}
		fmt.Printf("Killed session %s\n", oldSessionID)
	}

	// Perform the rename
	if err := crewMgr.Rename(oldName, newName); err != nil {
		if err == clan.ErrCrewNotFound {
			return fmt.Errorf("clan workspace '%s' not found", oldName)
		}
		if err == clan.ErrCrewExists {
			return fmt.Errorf("clan workspace '%s' already exists", newName)
		}
		return fmt.Errorf("renaming clan workspace: %w", err)
	}

	fmt.Printf("%s Renamed clan workspace: %s/%s → %s/%s\n",
		style.Bold.Render("✓"), r.Name, oldName, r.Name, newName)
	fmt.Printf("New session will be: %s\n", style.Dim.Render(crewSessionName(r.Name, newName)))

	return nil
}

func runCrewPristine(cmd *cobra.Command, args []string) error {
	crewMgr, r, err := getCrewManager(crewRig)
	if err != nil {
		return err
	}

	var workers []*clan.CrewWorker

	if len(args) > 0 {
		// Specific worker
		name := args[0]
		// Parse warband/name format (e.g., "relics/emma" -> warband=relics, name=emma)
		if _, crewName, ok := parseRigSlashName(name); ok {
			name = crewName
		}
		worker, err := crewMgr.Get(name)
		if err != nil {
			if err == clan.ErrCrewNotFound {
				return fmt.Errorf("clan workspace '%s' not found", name)
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

	var results []*clan.PristineResult

	for _, w := range workers {
		result, err := crewMgr.Pristine(w.Name)
		if err != nil {
			return fmt.Errorf("pristine %s: %w", w.Name, err)
		}
		results = append(results, result)
	}

	if crewJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	// Text output
	for _, result := range results {
		fmt.Printf("%s %s/%s\n", style.Bold.Render("→"), r.Name, result.Name)

		if result.HadChanges {
			fmt.Printf("  %s\n", style.Bold.Render("⚠ Has uncommitted changes"))
		}

		if result.Pulled {
			fmt.Printf("  %s git pull\n", style.Dim.Render("✓"))
		} else if result.PullError != "" {
			fmt.Printf("  %s git pull: %s\n", style.Bold.Render("✗"), result.PullError)
		}

		if result.Synced {
			fmt.Printf("  %s rl sync\n", style.Dim.Render("✓"))
		} else if result.SyncError != "" {
			fmt.Printf("  %s rl sync: %s\n", style.Bold.Render("✗"), result.SyncError)
		}
	}

	return nil
}
