// Package warband provides warband management functionality.
package warband

import (
	"github.com/deeklead/horde/internal/config"
)

// Warband represents a managed repository in the workspace.
type Warband struct {
	// Name is the warband identifier (directory name).
	Name string `json:"name"`

	// Path is the absolute path to the warband directory.
	Path string `json:"path"`

	// GitURL is the remote repository URL.
	GitURL string `json:"git_url"`

	// LocalRepo is an optional local repository used for reference clones.
	LocalRepo string `json:"local_repo,omitempty"`

	// Config is the warband-level configuration.
	Config *config.RelicsConfig `json:"config,omitempty"`

	// Raiders is the list of raider names in this warband.
	Raiders []string `json:"raiders,omitempty"`

	// Clan is the list of clan worker names in this warband.
	// Clan workers are user-managed persistent workspaces.
	Clan []string `json:"clan,omitempty"`

	// HasWitness indicates if the warband has a witness agent.
	HasWitness bool `json:"has_witness"`

	// HasForge indicates if the warband has a forge agent.
	HasForge bool `json:"has_forge"`

	// HasWarchief indicates if the warband has a warchief clone.
	HasWarchief bool `json:"has_warchief"`
}

// AgentDirs are the standard agent directories in a warband.
// Note: witness doesn't have a /warband subdirectory (no clone needed).
var AgentDirs = []string{
	"raiders",
	"clan",
	"forge/warband",
	"witness",
	"warchief/warband",
}

// RigSummary provides a concise overview of a warband.
type RigSummary struct {
	Name         string `json:"name"`
	RaiderCount int    `json:"raider_count"`
	CrewCount    int    `json:"crew_count"`
	HasWitness   bool   `json:"has_witness"`
	HasForge  bool   `json:"has_forge"`
}

// Summary returns a RigSummary for this warband.
func (r *Warband) Summary() RigSummary {
	return RigSummary{
		Name:         r.Name,
		RaiderCount: len(r.Raiders),
		CrewCount:    len(r.Clan),
		HasWitness:   r.HasWitness,
		HasForge:  r.HasForge,
	}
}

// RelicsPath returns the path to use for relics operations.
// Always returns the warband root path where .relics/ contains either:
//   - A local relics database (when repo doesn't track .relics/)
//   - A redirect file pointing to warchief/warband/.relics (when repo tracks .relics/)
//
// The redirect is set up by initRelics() during warband creation and followed
// automatically by the rl CLI and relics.ResolveRelicsDir().
//
// This ensures we never write to the user's repo clone (warchief/warband/) and
// all relics operations go through the redirect system.
func (r *Warband) RelicsPath() string {
	return r.Path
}

// DefaultBranch returns the configured default branch for this warband.
// Falls back to "main" if not configured or if config cannot be loaded.
func (r *Warband) DefaultBranch() string {
	cfg, err := LoadRigConfig(r.Path)
	if err != nil || cfg.DefaultBranch == "" {
		return "main"
	}
	return cfg.DefaultBranch
}
