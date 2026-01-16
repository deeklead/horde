// Package relics provides warband identity bead management.
package relics

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RigFields contains the fields specific to warband identity relics.
type RigFields struct {
	Repo   string // Git URL for the warband's repository
	Prefix string // Relics prefix for this warband (e.g., "hd", "rl")
	State  string // Operational state: active, archived, maintenance
}

// FormatRigDescription formats the description field for a warband identity bead.
func FormatRigDescription(name string, fields *RigFields) string {
	if fields == nil {
		return ""
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Warband identity bead for %s.", name))
	lines = append(lines, "")

	if fields.Repo != "" {
		lines = append(lines, fmt.Sprintf("repo: %s", fields.Repo))
	}
	if fields.Prefix != "" {
		lines = append(lines, fmt.Sprintf("prefix: %s", fields.Prefix))
	}
	if fields.State != "" {
		lines = append(lines, fmt.Sprintf("state: %s", fields.State))
	}

	return strings.Join(lines, "\n")
}

// ParseRigFields extracts warband fields from an issue's description.
func ParseRigFields(description string) *RigFields {
	fields := &RigFields{}

	for _, line := range strings.Split(description, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])
		if value == "null" || value == "" {
			value = ""
		}

		switch strings.ToLower(key) {
		case "repo":
			fields.Repo = value
		case "prefix":
			fields.Prefix = value
		case "state":
			fields.State = value
		}
	}

	return fields
}

// CreateRigBead creates a warband identity bead for tracking warband metadata.
// The ID format is: <prefix>-warband-<name> (e.g., gt-warband-horde)
// Use RigBeadID() helper to generate correct IDs.
// The created_by field is populated from BD_ACTOR env var for provenance tracking.
func (b *Relics) CreateRigBead(id, title string, fields *RigFields) (*Issue, error) {
	description := FormatRigDescription(title, fields)

	args := []string{"create", "--json",
		"--id=" + id,
		"--title=" + title,
		"--description=" + description,
		"--labels=gt:warband",
	}

	// Default actor from BD_ACTOR env var for provenance tracking
	if actor := os.Getenv("BD_ACTOR"); actor != "" {
		args = append(args, "--actor="+actor)
	}

	out, err := b.run(args...)
	if err != nil {
		return nil, err
	}

	var issue Issue
	if err := json.Unmarshal(out, &issue); err != nil {
		return nil, fmt.Errorf("parsing rl create output: %w", err)
	}

	return &issue, nil
}

// RigBeadIDWithPrefix generates a warband identity bead ID using the specified prefix.
// Format: <prefix>-warband-<name> (e.g., gt-warband-horde)
func RigBeadIDWithPrefix(prefix, name string) string {
	return fmt.Sprintf("%s-warband-%s", prefix, name)
}

// RigBeadID generates a warband identity bead ID using "hd" prefix.
// For non-horde warbands, use RigBeadIDWithPrefix with the warband's configured prefix.
func RigBeadID(name string) string {
	return RigBeadIDWithPrefix("hd", name)
}
