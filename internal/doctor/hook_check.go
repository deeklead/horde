package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/OWNER/horde/internal/relics"
)

// HookAttachmentValidCheck verifies that attached totems exist and are not closed.
// This detects when a hook's attached_molecule field points to a non-existent or
// closed issue, which can leave agents with stale work assignments.
type HookAttachmentValidCheck struct {
	FixableCheck
	invalidAttachments []invalidAttachment
}

type invalidAttachment struct {
	pinnedBeadID   string
	pinnedBeadDir  string // Directory where the pinned bead was found
	moleculeID     string
	reason         string // "not_found" or "closed"
}

// NewHookAttachmentValidCheck creates a new hook attachment validation check.
func NewHookAttachmentValidCheck() *HookAttachmentValidCheck {
	return &HookAttachmentValidCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "hook-attachment-valid",
				CheckDescription: "Verify attached totems exist and are not closed",
				CheckCategory:    CategoryHooks,
			},
		},
	}
}

// Run checks all pinned relics for invalid totem attachments.
func (c *HookAttachmentValidCheck) Run(ctx *CheckContext) *CheckResult {
	c.invalidAttachments = nil

	var details []string

	// Check encampment-level relics
	townRelicsDir := filepath.Join(ctx.TownRoot, ".relics")
	townInvalid := c.checkRelicsDir(townRelicsDir, "encampment")
	for _, inv := range townInvalid {
		details = append(details, c.formatInvalid(inv))
	}
	c.invalidAttachments = append(c.invalidAttachments, townInvalid...)

	// Check warband-level relics
	rigDirs := c.findRigRelicsDirs(ctx.TownRoot)
	for _, rigDir := range rigDirs {
		rigName := filepath.Base(filepath.Dir(rigDir))
		rigInvalid := c.checkRelicsDir(rigDir, rigName)
		for _, inv := range rigInvalid {
			details = append(details, c.formatInvalid(inv))
		}
		c.invalidAttachments = append(c.invalidAttachments, rigInvalid...)
	}

	if len(c.invalidAttachments) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All hook attachments are valid",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("Found %d invalid hook attachment(s)", len(c.invalidAttachments)),
		Details: details,
		FixHint: "Run 'hd doctor --fix' to dismiss invalid totems, or 'hd totem dismiss <pinned-bead-id>' manually",
	}
}

// checkRelicsDir checks all pinned relics in a directory for invalid attachments.
func (c *HookAttachmentValidCheck) checkRelicsDir(relicsDir, _ string) []invalidAttachment { // location unused but kept for future diagnostic output
	var invalid []invalidAttachment

	b := relics.New(filepath.Dir(relicsDir))

	// List all pinned relics
	pinnedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusPinned,
		Priority: -1,
	})
	if err != nil {
		// Can't list pinned relics - silently skip this directory
		return nil
	}

	for _, pinnedBead := range pinnedRelics {
		// Parse attachment fields from the pinned bead
		attachment := relics.ParseAttachmentFields(pinnedBead)
		if attachment == nil || attachment.AttachedMolecule == "" {
			continue // No attachment, skip
		}

		// Verify the attached totem exists and is not closed
		totem, err := b.Show(attachment.AttachedMolecule)
		if err != nil {
			// Totem not found
			invalid = append(invalid, invalidAttachment{
				pinnedBeadID:  pinnedBead.ID,
				pinnedBeadDir: relicsDir,
				moleculeID:    attachment.AttachedMolecule,
				reason:        "not_found",
			})
			continue
		}

		if totem.Status == "closed" {
			invalid = append(invalid, invalidAttachment{
				pinnedBeadID:  pinnedBead.ID,
				pinnedBeadDir: relicsDir,
				moleculeID:    attachment.AttachedMolecule,
				reason:        "closed",
			})
		}
	}

	return invalid
}

// findRigRelicsDirs finds all warband-level .relics directories.
func (c *HookAttachmentValidCheck) findRigRelicsDirs(townRoot string) []string {
	var dirs []string

	// Look for .relics directories in warband subdirectories
	// Pattern: <townRoot>/<warband>/.relics (but NOT <townRoot>/.relics which is encampment-level)
	cmd := exec.Command("find", townRoot, "-maxdepth", "2", "-type", "d", "-name", ".relics")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		// Skip encampment-level .relics
		if line == filepath.Join(townRoot, ".relics") {
			continue
		}
		// Skip warchief directory
		if strings.Contains(line, "/warchief/") {
			continue
		}
		dirs = append(dirs, line)
	}

	return dirs
}

// formatInvalid formats an invalid attachment for display.
func (c *HookAttachmentValidCheck) formatInvalid(inv invalidAttachment) string {
	reasonText := "not found"
	if inv.reason == "closed" {
		reasonText = "is closed"
	}
	return fmt.Sprintf("%s: attached totem %s %s", inv.pinnedBeadID, inv.moleculeID, reasonText)
}

// Fix detaches all invalid totem attachments.
func (c *HookAttachmentValidCheck) Fix(ctx *CheckContext) error {
	var errors []string

	for _, inv := range c.invalidAttachments {
		b := relics.New(filepath.Dir(inv.pinnedBeadDir))

		_, err := b.DetachMolecule(inv.pinnedBeadID)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to dismiss from %s: %v", inv.pinnedBeadID, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return nil
}

// HookSingletonCheck ensures each agent has at most one handoff bead.
// Detects when multiple pinned relics exist with the same "{role} Handoff" title,
// which can cause confusion about which handoff is authoritative.
type HookSingletonCheck struct {
	FixableCheck
	duplicates []duplicateHandoff
}

type duplicateHandoff struct {
	title     string
	relicsDir  string
	beadIDs   []string // All IDs with this title (first one is kept, rest are duplicates)
}

// NewHookSingletonCheck creates a new hook singleton check.
func NewHookSingletonCheck() *HookSingletonCheck {
	return &HookSingletonCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "hook-singleton",
				CheckDescription: "Ensure each agent has at most one handoff bead",
				CheckCategory:    CategoryHooks,
			},
		},
	}
}

// Run checks all pinned relics for duplicate handoff titles.
func (c *HookSingletonCheck) Run(ctx *CheckContext) *CheckResult {
	c.duplicates = nil

	var details []string

	// Check encampment-level relics
	townRelicsDir := filepath.Join(ctx.TownRoot, ".relics")
	townDups := c.checkRelicsDir(townRelicsDir)
	for _, dup := range townDups {
		details = append(details, c.formatDuplicate(dup))
	}
	c.duplicates = append(c.duplicates, townDups...)

	// Check warband-level relics using the shared helper
	attachCheck := &HookAttachmentValidCheck{}
	rigDirs := attachCheck.findRigRelicsDirs(ctx.TownRoot)
	for _, rigDir := range rigDirs {
		rigDups := c.checkRelicsDir(rigDir)
		for _, dup := range rigDups {
			details = append(details, c.formatDuplicate(dup))
		}
		c.duplicates = append(c.duplicates, rigDups...)
	}

	if len(c.duplicates) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All handoff relics are unique",
		}
	}

	totalDups := 0
	for _, dup := range c.duplicates {
		totalDups += len(dup.beadIDs) - 1 // Count extras beyond the first
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("Found %d duplicate handoff bead(s)", totalDups),
		Details: details,
		FixHint: "Run 'hd doctor --fix' to close duplicates, or 'bd close <id>' manually",
	}
}

// checkRelicsDir checks for duplicate handoff relics in a directory.
func (c *HookSingletonCheck) checkRelicsDir(relicsDir string) []duplicateHandoff {
	var duplicates []duplicateHandoff

	b := relics.New(filepath.Dir(relicsDir))

	// List all pinned relics
	pinnedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusPinned,
		Priority: -1,
	})
	if err != nil {
		return nil
	}

	// Group pinned relics by title (only those matching "{role} Handoff" pattern)
	titleToIDs := make(map[string][]string)
	for _, bead := range pinnedRelics {
		// Check if title matches handoff pattern (ends with " Handoff")
		if strings.HasSuffix(bead.Title, " Handoff") {
			titleToIDs[bead.Title] = append(titleToIDs[bead.Title], bead.ID)
		}
	}

	// Find duplicates (titles with more than one bead)
	for title, ids := range titleToIDs {
		if len(ids) > 1 {
			duplicates = append(duplicates, duplicateHandoff{
				title:    title,
				relicsDir: relicsDir,
				beadIDs:  ids,
			})
		}
	}

	return duplicates
}

// formatDuplicate formats a duplicate handoff for display.
func (c *HookSingletonCheck) formatDuplicate(dup duplicateHandoff) string {
	return fmt.Sprintf("%q has %d relics: %s", dup.title, len(dup.beadIDs), strings.Join(dup.beadIDs, ", "))
}

// Fix closes duplicate handoff relics, keeping the first one.
func (c *HookSingletonCheck) Fix(ctx *CheckContext) error {
	var errors []string

	for _, dup := range c.duplicates {
		b := relics.New(filepath.Dir(dup.relicsDir))

		// Close all but the first bead (keep the oldest/first one)
		toClose := dup.beadIDs[1:]
		if len(toClose) > 0 {
			err := b.CloseWithReason("duplicate handoff bead", toClose...)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed to close duplicates for %q: %v", dup.title, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return nil
}

// OrphanedAttachmentsCheck detects handoff relics for agents that no longer exist.
// This happens when a raider worktree is deleted but its handoff bead remains,
// leaving totems attached to non-existent agents.
type OrphanedAttachmentsCheck struct {
	BaseCheck
	orphans []orphanedHandoff
}

type orphanedHandoff struct {
	beadID    string
	beadTitle string
	relicsDir  string
	agent     string // Parsed agent identity
}

// NewOrphanedAttachmentsCheck creates a new orphaned attachments check.
func NewOrphanedAttachmentsCheck() *OrphanedAttachmentsCheck {
	return &OrphanedAttachmentsCheck{
		BaseCheck: BaseCheck{
			CheckName:        "orphaned-attachments",
			CheckDescription: "Detect handoff relics for non-existent agents",
			CheckCategory:    CategoryHooks,
		},
	}
}

// Run checks all handoff relics for orphaned agents.
func (c *OrphanedAttachmentsCheck) Run(ctx *CheckContext) *CheckResult {
	c.orphans = nil

	var details []string

	// Check encampment-level relics
	townRelicsDir := filepath.Join(ctx.TownRoot, ".relics")
	townOrphans := c.checkRelicsDir(townRelicsDir, ctx.TownRoot)
	for _, orph := range townOrphans {
		details = append(details, c.formatOrphan(orph))
	}
	c.orphans = append(c.orphans, townOrphans...)

	// Check warband-level relics using the shared helper
	attachCheck := &HookAttachmentValidCheck{}
	rigDirs := attachCheck.findRigRelicsDirs(ctx.TownRoot)
	for _, rigDir := range rigDirs {
		rigOrphans := c.checkRelicsDir(rigDir, ctx.TownRoot)
		for _, orph := range rigOrphans {
			details = append(details, c.formatOrphan(orph))
		}
		c.orphans = append(c.orphans, rigOrphans...)
	}

	if len(c.orphans) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No orphaned handoff relics found",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("Found %d orphaned handoff bead(s)", len(c.orphans)),
		Details: details,
		FixHint: "Reassign with 'hd charge <id> <agent>', or close with 'bd close <id>'",
	}
}

// checkRelicsDir checks for orphaned handoff relics in a directory.
func (c *OrphanedAttachmentsCheck) checkRelicsDir(relicsDir, townRoot string) []orphanedHandoff {
	var orphans []orphanedHandoff

	b := relics.New(filepath.Dir(relicsDir))

	// List all pinned relics
	pinnedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusPinned,
		Priority: -1,
	})
	if err != nil {
		return nil
	}

	for _, bead := range pinnedRelics {
		// Check if title matches handoff pattern (ends with " Handoff")
		if !strings.HasSuffix(bead.Title, " Handoff") {
			continue
		}

		// Extract agent identity from title
		agent := strings.TrimSuffix(bead.Title, " Handoff")
		if agent == "" {
			continue
		}

		// Check if agent worktree exists
		if !c.agentExists(agent, townRoot) {
			orphans = append(orphans, orphanedHandoff{
				beadID:    bead.ID,
				beadTitle: bead.Title,
				relicsDir:  relicsDir,
				agent:     agent,
			})
		}
	}

	return orphans
}

// agentExists checks if an agent's worktree exists.
// Agent identities follow patterns like:
//   - "horde/nux" → raider at <townRoot>/horde/raiders/nux
//   - "horde/clan/joe" → clan at <townRoot>/horde/clan/joe
//   - "warchief" → warchief at <townRoot>/warchief
//   - "horde-witness" → witness at <townRoot>/horde/witness
//   - "horde-forge" → forge at <townRoot>/horde/forge
func (c *OrphanedAttachmentsCheck) agentExists(agent, townRoot string) bool {
	// Handle special roles with hyphen separator
	if strings.HasSuffix(agent, "-witness") {
		warband := strings.TrimSuffix(agent, "-witness")
		path := filepath.Join(townRoot, warband, "witness")
		return dirExists(path)
	}
	if strings.HasSuffix(agent, "-forge") {
		warband := strings.TrimSuffix(agent, "-forge")
		path := filepath.Join(townRoot, warband, "forge")
		return dirExists(path)
	}

	// Handle warchief
	if agent == "warchief" {
		return dirExists(filepath.Join(townRoot, "warchief"))
	}

	// Handle clan (warband/clan/name pattern)
	if strings.Contains(agent, "/clan/") {
		parts := strings.SplitN(agent, "/clan/", 2)
		if len(parts) == 2 {
			path := filepath.Join(townRoot, parts[0], "clan", parts[1])
			return dirExists(path)
		}
	}

	// Handle raiders (warband/name pattern) - most common case
	if strings.Contains(agent, "/") {
		parts := strings.SplitN(agent, "/", 2)
		if len(parts) == 2 {
			path := filepath.Join(townRoot, parts[0], "raiders", parts[1])
			return dirExists(path)
		}
	}

	// Unknown pattern - assume exists to avoid false positives
	return true
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// formatOrphan formats an orphaned handoff for display.
func (c *OrphanedAttachmentsCheck) formatOrphan(orph orphanedHandoff) string {
	return fmt.Sprintf("%s: agent %q no longer exists", orph.beadID, orph.agent)
}
