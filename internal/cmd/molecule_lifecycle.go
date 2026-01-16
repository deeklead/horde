package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// runMoleculeBurn burns (destroys) the current totem attachment.
func runMoleculeBurn(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Find encampment root
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	// Determine target agent
	var target string
	if len(args) > 0 {
		target = args[0]
	} else {
		// Auto-detect using env-aware role detection
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
		target = buildAgentIdentity(roleCtx)
		if target == "" {
			return fmt.Errorf("cannot determine agent identity (role: %s)", roleCtx.Role)
		}
	}

	// Find relics directory
	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Find agent's pinned bead (handoff bead)
	parts := strings.Split(target, "/")
	role := parts[len(parts)-1]

	handoff, err := b.FindHandoffBead(role)
	if err != nil {
		return fmt.Errorf("finding handoff bead: %w", err)
	}
	if handoff == nil {
		return fmt.Errorf("no handoff bead found for %s", target)
	}

	// Check for attached totem
	attachment := relics.ParseAttachmentFields(handoff)
	if attachment == nil || attachment.AttachedMolecule == "" {
		fmt.Printf("%s No totem attached to %s - nothing to burn\n",
			style.Dim.Render("ℹ"), target)
		return nil
	}

	moleculeID := attachment.AttachedMolecule

	// Recursively close all descendant step issues before detaching
	// This prevents orphaned step issues from accumulating (gt-psj76.1)
	childrenClosed := closeDescendants(b, moleculeID)

	// Dismiss the totem with audit logging (this "burns" it by removing the attachment)
	_, err = b.DetachMoleculeWithAudit(handoff.ID, relics.DetachOptions{
		Operation: "burn",
		Agent:     target,
		Reason:    "totem burned by agent",
	})
	if err != nil {
		return fmt.Errorf("detaching totem: %w", err)
	}

	if moleculeJSON {
		result := map[string]interface{}{
			"burned":          moleculeID,
			"from":            target,
			"handoff_id":      handoff.ID,
			"children_closed": childrenClosed,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Printf("%s Burned totem %s from %s\n",
		style.Bold.Render("🔥"), moleculeID, target)
	if childrenClosed > 0 {
		fmt.Printf("  Closed %d step issues\n", childrenClosed)
	}

	return nil
}

// runMoleculeSquash squashes the current totem into a digest.
func runMoleculeSquash(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Find encampment root
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	// Determine target agent
	var target string
	if len(args) > 0 {
		target = args[0]
	} else {
		// Auto-detect using env-aware role detection
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
		target = buildAgentIdentity(roleCtx)
		if target == "" {
			return fmt.Errorf("cannot determine agent identity (role: %s)", roleCtx.Role)
		}
	}

	// Find relics directory
	workDir, err := findLocalRelicsDir()
	if err != nil {
		return fmt.Errorf("not in a relics workspace: %w", err)
	}

	b := relics.New(workDir)

	// Find agent's pinned bead (handoff bead)
	parts := strings.Split(target, "/")
	role := parts[len(parts)-1]

	handoff, err := b.FindHandoffBead(role)
	if err != nil {
		return fmt.Errorf("finding handoff bead: %w", err)
	}
	if handoff == nil {
		return fmt.Errorf("no handoff bead found for %s", target)
	}

	// Check for attached totem
	attachment := relics.ParseAttachmentFields(handoff)
	if attachment == nil || attachment.AttachedMolecule == "" {
		fmt.Printf("%s No totem attached to %s - nothing to squash\n",
			style.Dim.Render("ℹ"), target)
		return nil
	}

	moleculeID := attachment.AttachedMolecule

	// Recursively close all descendant step issues before squashing
	// This prevents orphaned step issues from accumulating (gt-psj76.1)
	childrenClosed := closeDescendants(b, moleculeID)

	// Get progress info for the digest
	progress, _ := getMoleculeProgressInfo(b, moleculeID)

	// Create a digest issue
	digestTitle := fmt.Sprintf("Digest: %s", moleculeID)
	digestDesc := fmt.Sprintf(`Squashed totem execution.

totem: %s
agent: %s
squashed_at: %s
`, moleculeID, target, time.Now().UTC().Format(time.RFC3339))

	if progress != nil {
		digestDesc += fmt.Sprintf(`
## Execution Summary
- Steps: %d/%d completed
- Status: %s
`, progress.DoneSteps, progress.TotalSteps, func() string {
			if progress.Complete {
				return "complete"
			}
			return "partial"
		}())
	}

	// Create the digest bead
	digestIssue, err := b.Create(relics.CreateOptions{
		Title:       digestTitle,
		Description: digestDesc,
		Type:        "task",
		Priority:    4, // P4 - backlog priority for digests
		Actor:       target,
	})
	if err != nil {
		return fmt.Errorf("creating digest: %w", err)
	}

	// Add the digest label (non-fatal: digest works without label)
	_ = b.Update(digestIssue.ID, relics.UpdateOptions{
		AddLabels: []string{"digest"},
	})

	// Close the digest immediately
	closedStatus := "closed"
	err = b.Update(digestIssue.ID, relics.UpdateOptions{
		Status: &closedStatus,
	})
	if err != nil {
		style.PrintWarning("Created digest but couldn't close it: %v", err)
	}

	// Dismiss the totem from the handoff bead with audit logging
	_, err = b.DetachMoleculeWithAudit(handoff.ID, relics.DetachOptions{
		Operation: "squash",
		Agent:     target,
		Reason:    fmt.Sprintf("totem squashed to digest %s", digestIssue.ID),
	})
	if err != nil {
		return fmt.Errorf("detaching totem: %w", err)
	}

	if moleculeJSON {
		result := map[string]interface{}{
			"squashed":        moleculeID,
			"digest_id":       digestIssue.ID,
			"from":            target,
			"handoff_id":      handoff.ID,
			"children_closed": childrenClosed,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Printf("%s Squashed totem %s → digest %s\n",
		style.Bold.Render("📦"), moleculeID, digestIssue.ID)
	if childrenClosed > 0 {
		fmt.Printf("  Closed %d step issues\n", childrenClosed)
	}

	return nil
}

// closeDescendants recursively closes all descendant issues of a parent.
// Returns the count of issues closed. Logs warnings on errors but doesn't fail.
func closeDescendants(b *relics.Relics, parentID string) int {
	children, err := b.List(relics.ListOptions{
		Parent: parentID,
		Status: "all",
	})
	if err != nil {
		style.PrintWarning("could not list children of %s: %v", parentID, err)
		return 0
	}

	if len(children) == 0 {
		return 0
	}

	// First, recursively close grandchildren
	totalClosed := 0
	for _, child := range children {
		totalClosed += closeDescendants(b, child.ID)
	}

	// Then close direct children
	var idsToClose []string
	for _, child := range children {
		if child.Status != "closed" {
			idsToClose = append(idsToClose, child.ID)
		}
	}

	if len(idsToClose) > 0 {
		if closeErr := b.Close(idsToClose...); closeErr != nil {
			style.PrintWarning("could not close children of %s: %v", parentID, closeErr)
		} else {
			totalClosed += len(idsToClose)
		}
	}

	return totalClosed
}
