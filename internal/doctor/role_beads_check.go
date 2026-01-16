package doctor

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/OWNER/horde/internal/relics"
)

// RoleRelicsCheck verifies that role definition relics exist.
// Role relics are templates that define role characteristics and lifecycle hooks.
// They are stored in encampment relics (~/.relics/) with hq- prefix:
//   - hq-warchief-role, hq-shaman-role, hq-dog-role
//   - hq-witness-role, hq-forge-role, hq-raider-role, hq-clan-role
//
// Role relics are created by hd install, but creation may fail silently.
// Without role relics, agents fall back to defaults which may differ from
// user expectations.
type RoleRelicsCheck struct {
	FixableCheck
	missing []string // Track missing role relics for fix
}

// NewRoleRelicsCheck creates a new role relics check.
func NewRoleRelicsCheck() *RoleRelicsCheck {
	return &RoleRelicsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "role-relics-exist",
				CheckDescription: "Verify role definition relics exist",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks if role relics exist.
func (c *RoleRelicsCheck) Run(ctx *CheckContext) *CheckResult {
	c.missing = nil // Reset

	townRelicsPath := relics.GetTownRelicsPath(ctx.TownRoot)
	bd := relics.New(townRelicsPath)

	var missing []string
	roleDefs := relics.AllRoleBeadDefs()

	for _, role := range roleDefs {
		if _, err := bd.Show(role.ID); err != nil {
			missing = append(missing, role.ID)
		}
	}

	c.missing = missing

	if len(missing) == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  fmt.Sprintf("All %d role relics exist", len(roleDefs)),
			Category: c.Category(),
		}
	}

	return &CheckResult{
		Name:     c.Name(),
		Status:   StatusWarning, // Warning, not error - agents work without role relics
		Message:  fmt.Sprintf("%d role bead(s) missing (agents will use defaults)", len(missing)),
		Details:  missing,
		FixHint:  "Run 'hd doctor --fix' to create missing role relics",
		Category: c.Category(),
	}
}

// Fix creates missing role relics.
func (c *RoleRelicsCheck) Fix(ctx *CheckContext) error {
	// Re-run check to populate missing if needed
	if c.missing == nil {
		result := c.Run(ctx)
		if result.Status == StatusOK {
			return nil // Nothing to fix
		}
	}

	if len(c.missing) == 0 {
		return nil
	}

	// Build lookup map for role definitions
	roleDefMap := make(map[string]relics.RoleBeadDef)
	for _, role := range relics.AllRoleBeadDefs() {
		roleDefMap[role.ID] = role
	}

	// Create missing role relics
	for _, id := range c.missing {
		role, ok := roleDefMap[id]
		if !ok {
			continue // Shouldn't happen
		}

		// Create role bead using rl create --type=role
		cmd := exec.Command("rl", "create",
			"--type=role",
			"--id="+role.ID,
			"--title="+role.Title,
			"--description="+role.Desc,
		)
		cmd.Dir = ctx.TownRoot
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("creating %s: %s", role.ID, strings.TrimSpace(string(output)))
		}
	}

	return nil
}
