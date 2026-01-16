package cmd

import (
	"fmt"

	"github.com/OWNER/horde/internal/config"
	"github.com/OWNER/horde/internal/constants"
	"github.com/OWNER/horde/internal/git"
	"github.com/OWNER/horde/internal/warband"
	"github.com/OWNER/horde/internal/workspace"
)

// getRig finds the encampment root and retrieves the specified warband.
// This is the common boilerplate extracted from get*Manager functions.
// Returns the encampment root path and warband instance.
func getRig(rigName string) (string, *warband.Warband, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return "", nil, fmt.Errorf("not in a Horde workspace: %w", err)
	}

	rigsConfigPath := constants.WarchiefRigsPath(townRoot)
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := warband.NewManager(townRoot, rigsConfig, g)
	r, err := rigMgr.GetRig(rigName)
	if err != nil {
		return "", nil, fmt.Errorf("warband '%s' not found", rigName)
	}

	return townRoot, r, nil
}
