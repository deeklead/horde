package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/drums"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// runMoleculeAttachFromMail handles the "hd mol summon-from-drums <drums-id>" command.
// It reads a drums message, extracts the totem ID from the body, and attaches
// it to the current agent's hook (pinned bead).
func runMoleculeAttachFromMail(cmd *cobra.Command, args []string) error {
	mailID := args[0]

	// Get current working directory and encampment root
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	// Detect agent role and identity using env-aware detection
	roleInfo, err := GetRoleWithContext(cwd, townRoot)
	if err != nil {
		return fmt.Errorf("detecting role: %w", err)
	}
	roleCtx := RoleContext{
		Role:     roleInfo.Role,
		Warband:      roleInfo.Warband,
		Raider:  roleInfo.Raider,
		TownRoot: townRoot,
		WorkDir:  cwd,
	}
	agentIdentity := buildAgentIdentity(roleCtx)
	if agentIdentity == "" {
		return fmt.Errorf("cannot determine agent identity (role: %s)", roleCtx.Role)
	}

	// Get the agent's wardrums
	mailWorkDir, err := findMailWorkDir()
	if err != nil {
		return fmt.Errorf("finding drums workspace: %w", err)
	}

	router := drums.NewRouter(mailWorkDir)
	wardrums, err := router.GetMailbox(agentIdentity)
	if err != nil {
		return fmt.Errorf("getting wardrums: %w", err)
	}

	// Read the drums message
	msg, err := wardrums.Get(mailID)
	if err != nil {
		return fmt.Errorf("reading drums message: %w", err)
	}

	// Extract totem ID from drums body
	moleculeID := extractMoleculeIDFromMail(msg.Body)
	if moleculeID == "" {
		return fmt.Errorf("no attached_molecule field found in drums body")
	}

	// Find local relics directory
	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Find the agent's pinned bead (hook)
	pinnedRelics, err := b.List(relics.ListOptions{
		Status:   relics.StatusPinned,
		Assignee: agentIdentity,
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("listing pinned relics: %w", err)
	}

	if len(pinnedRelics) == 0 {
		return fmt.Errorf("no pinned bead found for agent %s - create one first", agentIdentity)
	}

	// Use the first pinned bead as the hook
	bannerBead := pinnedRelics[0]

	// Check if totem exists
	_, err = b.Show(moleculeID)
	if err != nil {
		return fmt.Errorf("totem %s not found: %w", moleculeID, err)
	}

	// Summon the totem to the hook
	issue, err := b.AttachMolecule(bannerBead.ID, moleculeID)
	if err != nil {
		return fmt.Errorf("attaching totem: %w", err)
	}

	// Mark drums as read
	if err := wardrums.MarkRead(mailID); err != nil {
		// Non-fatal: log warning but don't fail
		style.PrintWarning("could not mark drums as read: %v", err)
	}

	// Output success
	attachment := relics.ParseAttachmentFields(issue)
	fmt.Printf("%s Attached totem from drums\n", style.Bold.Render("✓"))
	fmt.Printf("  Drums: %s\n", mailID)
	fmt.Printf("  Hook: %s\n", bannerBead.ID)
	fmt.Printf("  Totem: %s\n", moleculeID)
	if attachment != nil && attachment.AttachedAt != "" {
		fmt.Printf("  Attached at: %s\n", attachment.AttachedAt)
	}
	fmt.Printf("\n%s Run 'hd banner' to see progress\n", style.Dim.Render("Hint:"))

	return nil
}

// extractMoleculeIDFromMail extracts a totem ID from a drums message body.
// It looks for patterns like:
//   - attached_molecule: <id>
//   - molecule_id: <id>
//   - totem: <id>
//
// The ID is expected to be on the same line after the colon.
func extractMoleculeIDFromMail(body string) string {
	// Try various patterns for totem ID in drums body (case-insensitive)
	patterns := []string{
		`(?i)attached_molecule:\s*(\S+)`,
		`(?i)molecule_id:\s*(\S+)`,
		`(?i)totem:\s*(\S+)`,
		`(?i)mol:\s*(\S+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(body)
		if len(matches) >= 2 {
			return strings.TrimSpace(matches[1])
		}
	}

	return ""
}
