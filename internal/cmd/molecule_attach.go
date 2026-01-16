package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

func runMoleculeAttach(cmd *cobra.Command, args []string) error {
	var pinnedBeadID, moleculeID string

	if len(args) == 2 {
		// Explicit: hd mol summon <pinned-bead-id> <totem-id>
		pinnedBeadID = args[0]
		moleculeID = args[1]
	} else {
		// Auto-detect: hd mol summon <totem-id>
		moleculeID = args[0]
		var err error
		pinnedBeadID, err = detectAgentBeadID()
		if err != nil {
			return fmt.Errorf("auto-detecting agent: %w", err)
		}
		if pinnedBeadID == "" {
			return fmt.Errorf("could not detect agent from current directory - provide explicit pinned bead ID")
		}
	}

	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Summon the totem
	issue, err := b.AttachMolecule(pinnedBeadID, moleculeID)
	if err != nil {
		return fmt.Errorf("attaching totem: %w", err)
	}

	attachment := relics.ParseAttachmentFields(issue)
	fmt.Printf("%s Attached %s to %s\n", style.Bold.Render("✓"), moleculeID, pinnedBeadID)
	if attachment != nil && attachment.AttachedAt != "" {
		fmt.Printf("  attached_at: %s\n", attachment.AttachedAt)
	}

	return nil
}

// detectAgentBeadID detects the current agent's bead ID from the working directory.
// Returns the agent bead ID (e.g., "hq-warchief", "hd-horde-raider-nux") or empty string if not detectable.
func detectAgentBeadID() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current directory: %w", err)
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return "", fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return "", fmt.Errorf("not in a Horde workspace")
	}

	roleInfo, err := GetRoleWithContext(cwd, townRoot)
	if err != nil {
		return "", fmt.Errorf("detecting role: %w", err)
	}

	roleCtx := RoleContext{
		Role:     roleInfo.Role,
		Warband:      roleInfo.Warband,
		Raider:  roleInfo.Raider,
		TownRoot: townRoot,
		WorkDir:  cwd,
	}

	identity := buildAgentIdentity(roleCtx)
	if identity == "" {
		return "", fmt.Errorf("cannot determine agent identity (role: %s)", roleCtx.Role)
	}

	beadID := buildAgentBeadID(identity, roleCtx.Role, townRoot)
	if beadID == "" {
		return "", fmt.Errorf("cannot build agent bead ID for identity: %s", identity)
	}

	return beadID, nil
}

func runMoleculeDetach(cmd *cobra.Command, args []string) error {
	pinnedBeadID := args[0]

	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Check current attachment first
	attachment, err := b.GetAttachment(pinnedBeadID)
	if err != nil {
		return fmt.Errorf("checking attachment: %w", err)
	}

	if attachment == nil {
		fmt.Printf("%s No totem attached to %s\n", style.Dim.Render("ℹ"), pinnedBeadID)
		return nil
	}

	previousMolecule := attachment.AttachedMolecule

	// Dismiss the totem with audit logging
	_, err = b.DetachMoleculeWithAudit(pinnedBeadID, relics.DetachOptions{
		Operation: "dismiss",
		Agent:     detectCurrentAgent(),
	})
	if err != nil {
		return fmt.Errorf("detaching totem: %w", err)
	}

	fmt.Printf("%s Detached %s from %s\n", style.Bold.Render("✓"), previousMolecule, pinnedBeadID)

	return nil
}

func runMoleculeAttachment(cmd *cobra.Command, args []string) error {
	pinnedBeadID := args[0]

	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Get the issue
	issue, err := b.Show(pinnedBeadID)
	if err != nil {
		return fmt.Errorf("getting issue: %w", err)
	}

	attachment := relics.ParseAttachmentFields(issue)

	if moleculeJSON {
		type attachmentOutput struct {
			IssueID          string `json:"issue_id"`
			IssueTitle       string `json:"issue_title"`
			Status           string `json:"status"`
			AttachedMolecule string `json:"attached_molecule,omitempty"`
			AttachedAt       string `json:"attached_at,omitempty"`
		}
		out := attachmentOutput{
			IssueID:    issue.ID,
			IssueTitle: issue.Title,
			Status:     issue.Status,
		}
		if attachment != nil {
			out.AttachedMolecule = attachment.AttachedMolecule
			out.AttachedAt = attachment.AttachedAt
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	// Human-readable output
	fmt.Printf("\n%s: %s\n", style.Bold.Render(issue.ID), issue.Title)
	fmt.Printf("Status: %s\n", issue.Status)

	if attachment == nil || attachment.AttachedMolecule == "" {
		fmt.Printf("\n%s\n", style.Dim.Render("No totem attached"))
	} else {
		fmt.Printf("\n%s\n", style.Bold.Render("Attached Totem:"))
		fmt.Printf("  ID: %s\n", attachment.AttachedMolecule)
		if attachment.AttachedAt != "" {
			fmt.Printf("  Attached at: %s\n", attachment.AttachedAt)
		}
	}

	return nil
}

// detectCurrentAgent returns the current agent identity based on HD_ROLE or working directory.
// Returns empty string if identity cannot be determined.
func detectCurrentAgent() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		return ""
	}

	roleInfo, err := GetRoleWithContext(cwd, townRoot)
	if err != nil {
		return ""
	}
	ctx := RoleContext{
		Role:     roleInfo.Role,
		Warband:      roleInfo.Warband,
		Raider:  roleInfo.Raider,
		TownRoot: townRoot,
		WorkDir:  cwd,
	}
	return buildAgentIdentity(ctx)
}
