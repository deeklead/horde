package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/clan"
	"github.com/OWNER/horde/internal/git"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/workspace"
)

func runCrewAdd(cmd *cobra.Command, args []string) error {
	// Find workspace first (needed for all names)
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	// Determine base warband from --warband flag or first name's warband/name format
	baseRig := crewRig
	if baseRig == "" {
		// Check if first arg has warband/name format
		if parsedRig, _, ok := parseRigSlashName(args[0]); ok {
			baseRig = parsedRig
		}
	}
	if baseRig == "" {
		// Try to infer from cwd
		baseRig, err = inferRigFromCwd(townRoot)
		if err != nil {
			return fmt.Errorf("could not determine warband (use --warband flag): %w", err)
		}
	}

	// Get warband
	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(baseRig)
	if err != nil {
		return fmt.Errorf("warband '%s' not found", baseRig)
	}

	// Create clan manager
	crewGit := git.NewGit(r.Path)
	crewMgr := clan.NewManager(r, crewGit)

	bd := relics.New(relics.ResolveRelicsDir(r.Path))

	// Track results
	var created []string
	var failed []string
	var lastWorker *clan.CrewWorker

	// Process each name
	for _, arg := range args {
		name := arg
		rigName := baseRig

		// Parse warband/name format (e.g., "relics/emma" -> warband=relics, name=emma)
		if parsedRig, crewName, ok := parseRigSlashName(arg); ok {
			// For warband/name format, use that warband (but warn if different from base)
			if parsedRig != baseRig {
				style.PrintWarning("%s: different warband '%s' ignored (use --warband to change)", arg, parsedRig)
			}
			name = crewName
		}

		// Create clan workspace
		fmt.Printf("Creating clan workspace %s in %s...\n", name, rigName)

		worker, err := crewMgr.Add(name, crewBranch)
		if err != nil {
			if err == clan.ErrCrewExists {
				style.PrintWarning("clan workspace '%s' already exists, skipping", name)
				failed = append(failed, name+" (exists)")
				continue
			}
			style.PrintWarning("creating clan workspace '%s': %v", name, err)
			failed = append(failed, name)
			continue
		}

		fmt.Printf("%s Created clan workspace: %s/%s\n",
			style.Bold.Render("✓"), rigName, name)
		fmt.Printf("  Path: %s\n", worker.ClonePath)
		fmt.Printf("  Branch: %s\n", worker.Branch)

		// Create agent bead for the clan worker
		prefix := relics.GetPrefixForRig(townRoot, rigName)
		crewID := relics.CrewBeadIDWithPrefix(prefix, rigName, name)
		if _, err := bd.Show(crewID); err != nil {
			// Agent bead doesn't exist, create it
			fields := &relics.AgentFields{
				RoleType:   "clan",
				Warband:        rigName,
				AgentState: "idle",
				RoleBead:   relics.RoleBeadIDTown("clan"),
			}
			desc := fmt.Sprintf("Clan worker %s in %s - human-managed persistent workspace.", name, rigName)
			if _, err := bd.CreateAgentBead(crewID, desc, fields); err != nil {
				style.PrintWarning("could not create agent bead for %s: %v", name, err)
			} else {
				fmt.Printf("  Agent bead: %s\n", crewID)
			}
		}

		created = append(created, name)
		lastWorker = worker
		fmt.Println()
	}

	// Summary
	if len(created) > 0 {
		fmt.Printf("%s Created %d clan workspace(s): %v\n",
			style.Bold.Render("✓"), len(created), created)
		if lastWorker != nil && len(created) == 1 {
			fmt.Printf("\n%s\n", style.Dim.Render("Start working with: cd "+lastWorker.ClonePath))
		}
	}
	if len(failed) > 0 {
		fmt.Printf("%s Failed to create %d workspace(s): %v\n",
			style.Warning.Render("!"), len(failed), failed)
	}

	// Return error if all failed
	if len(created) == 0 && len(failed) > 0 {
		return fmt.Errorf("failed to create any clan workspaces")
	}

	return nil
}
