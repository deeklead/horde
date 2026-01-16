// Package warband provides warband management functionality.
// This file implements the property layer lookup API for unified config access.
package warband

import (
	"path/filepath"
	"strconv"

	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/wisp"
)

// ConfigSource identifies which layer a config value came from.
type ConfigSource string

const (
	SourceWisp    ConfigSource = "wisp"    // Local wisp layer (.relics-wisp/config/)
	SourceBead    ConfigSource = "bead"    // Warband identity bead labels
	SourceTown    ConfigSource = "encampment"    // Encampment defaults (~/horde/settings/config.json)
	SourceSystem  ConfigSource = "system"  // Compiled-in system defaults
	SourceBlocked ConfigSource = "blocked" // Explicitly blocked at wisp layer
	SourceNone    ConfigSource = "none"    // No value found
)

// ConfigResult holds a config lookup result with its source.
type ConfigResult struct {
	Value  interface{}
	Source ConfigSource
}

// SystemDefaults contains compiled-in default values.
// These are the fallback when no other layer provides a value.
var SystemDefaults = map[string]interface{}{
	"status":              "operational",
	"auto_restart":        true,
	"max_raiders":        10,
	"priority_adjustment": 0,
	"dnd":                 false,
}

// StackingKeys defines which keys use stacking semantics (values add up).
// All other keys use override semantics (first non-nil wins).
var StackingKeys = map[string]bool{
	"priority_adjustment": true,
}

// GetConfig looks up a config value through all layers.
// Override semantics: first non-nil value wins.
// Layers are checked in order: wisp -> bead -> encampment -> system
func (r *Warband) GetConfig(key string) interface{} {
	result := r.GetConfigWithSource(key)
	return result.Value
}

// GetConfigWithSource looks up a config value and returns which layer it came from.
func (r *Warband) GetConfigWithSource(key string) ConfigResult {
	townRoot := filepath.Dir(r.Path)

	// Layer 1: Wisp (transient, local)
	wispCfg := wisp.NewConfig(townRoot, r.Name)
	if wispCfg.IsBlocked(key) {
		return ConfigResult{Value: nil, Source: SourceBlocked}
	}
	if val := wispCfg.Get(key); val != nil {
		return ConfigResult{Value: val, Source: SourceWisp}
	}

	// Layer 2: Warband identity bead labels
	if val := r.getBeadLabel(key); val != nil {
		return ConfigResult{Value: val, Source: SourceBead}
	}

	// Layer 3: Encampment defaults
	// Note: Encampment defaults for operational state would typically be in
	// ~/horde/settings/config.json. For now, we skip directly to system defaults.
	// Future: load from config.TownSettings

	// Layer 4: System defaults
	if val, ok := SystemDefaults[key]; ok {
		return ConfigResult{Value: val, Source: SourceSystem}
	}

	return ConfigResult{Value: nil, Source: SourceNone}
}

// GetBoolConfig looks up a boolean config value.
// Returns false if not set, not a bool, or blocked.
func (r *Warband) GetBoolConfig(key string) bool {
	result := r.GetConfig(key)
	if result == nil {
		return false
	}

	switch v := result.(type) {
	case bool:
		return v
	case string:
		// Handle string booleans from bead labels
		return v == "true" || v == "1" || v == "yes"
	default:
		return false
	}
}

// GetIntConfig looks up an integer config value with stacking semantics.
// For stacking keys, values from wisp and bead layers ADD to the base.
// For non-stacking keys, uses override semantics.
func (r *Warband) GetIntConfig(key string) int {
	townRoot := filepath.Dir(r.Path)

	// Check if this key uses stacking semantics
	if !StackingKeys[key] {
		// Override semantics: return first non-nil
		result := r.GetConfig(key)
		return toInt(result)
	}

	// Stacking semantics: sum up adjustments from all layers

	// Get base value (encampment or system default)
	base := 0
	if val, ok := SystemDefaults[key]; ok {
		base = toInt(val)
	}

	// Check wisp layer for blocked
	wispCfg := wisp.NewConfig(townRoot, r.Name)
	if wispCfg.IsBlocked(key) {
		return 0 // Blocked returns zero
	}

	// Add bead adjustment
	beadAdj := 0
	if val := r.getBeadLabel(key); val != nil {
		beadAdj = toInt(val)
	}

	// Add wisp adjustment
	wispAdj := 0
	if val := wispCfg.Get(key); val != nil {
		wispAdj = toInt(val)
	}

	return base + beadAdj + wispAdj
}

// GetStringConfig looks up a string config value.
// Returns empty string if not set or blocked.
func (r *Warband) GetStringConfig(key string) string {
	result := r.GetConfig(key)
	if result == nil {
		return ""
	}

	switch v := result.(type) {
	case string:
		return v
	default:
		return ""
	}
}

// getBeadLabel reads a label value from the warband identity bead.
// Returns nil if the warband bead doesn't exist or the label is not set.
func (r *Warband) getBeadLabel(key string) interface{} {
	townRoot := filepath.Dir(r.Path)

	// Get the warband's relics prefix
	prefix := "hd" // default
	if r.Config != nil && r.Config.Prefix != "" {
		prefix = r.Config.Prefix
	}

	// Construct warband identity bead ID
	rigBeadID := relics.RigBeadIDWithPrefix(prefix, r.Name)

	// Load the bead
	relicsDir := relics.ResolveRelicsDir(r.Path)
	bd := relics.NewWithRelicsDir(townRoot, relicsDir)

	issue, err := bd.Show(rigBeadID)
	if err != nil {
		return nil
	}

	// Parse labels for key:value format
	for _, label := range issue.Labels {
		// Labels are in format "key:value"
		if len(label) > len(key)+1 && label[:len(key)+1] == key+":" {
			return label[len(key)+1:]
		}
	}

	return nil
}

// toInt converts a value to int, returning 0 for unconvertible types.
func toInt(v interface{}) int {
	if v == nil {
		return 0
	}

	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
		return 0
	default:
		return 0
	}
}
