package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// Environment variables for role detection
const (
	EnvGTRole     = "HD_ROLE"
	EnvGTRoleHome = "HD_ROLE_HOME"
)

// RoleInfo contains information about a role and its detection source.
// This is the canonical struct for role detection - used by both GetRole()
// and detectRole() functions.
type RoleInfo struct {
	Role          Role   `json:"role"`
	Source        string `json:"source"` // "env", "cwd", or "explicit"
	Home          string `json:"home"`
	Warband           string `json:"warband,omitempty"`
	Raider       string `json:"raider,omitempty"`
	EnvRole       string `json:"env_role,omitempty"`    // Value of HD_ROLE if set
	CwdRole       Role   `json:"cwd_role,omitempty"`    // Role detected from cwd
	Mismatch      bool   `json:"mismatch,omitempty"`    // True if env != cwd detection
	EnvIncomplete bool   `json:"env_incomplete,omitempty"` // True if env was set but missing warband/raider, filled from cwd
	TownRoot      string `json:"town_root,omitempty"`
	WorkDir       string `json:"work_dir,omitempty"`    // Current working directory
}

var roleCmd = &cobra.Command{
	Use:     "role",
	GroupID: GroupAgents,
	Short:   "Show or manage agent role",
	Long: `Display the current agent role and its detection source.

Role is determined by:
1. HD_ROLE environment variable (authoritative if set)
2. Current working directory (fallback)

If both are available and disagree, a warning is shown.`,
	RunE: runRoleShow,
}

var roleShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current role",
	RunE:  runRoleShow,
}

var roleHomeCmd = &cobra.Command{
	Use:   "home [ROLE]",
	Short: "Show home directory for a role",
	Long: `Show the canonical home directory for a role.

If no role is specified, shows the home for the current role.

Examples:
  hd role home           # Home for current role
  hd role home warchief     # Home for warchief
  hd role home witness   # Home for witness (requires --warband)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRoleHome,
}

var roleDetectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Force cwd-based role detection (debugging)",
	Long: `Detect role from current working directory, ignoring HD_ROLE env var.

This is useful for debugging role detection issues.`,
	RunE: runRoleDetect,
}

var roleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all known roles",
	RunE:  runRoleList,
}

var roleEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "Print export statements for current role",
	Long: `Print shell export statements for the current role.

Role is determined from HD_ROLE environment variable or current working directory.
This is a read-only command that displays the current role's env vars.

Examples:
  eval $(gt role env)    # Export current role's env vars
  hd role env            # View what would be exported`,
	RunE: runRoleEnv,
}

// Flags for role home command
var (
	roleRig     string
	roleRaider string
)

func init() {
	rootCmd.AddCommand(roleCmd)
	roleCmd.AddCommand(roleShowCmd)
	roleCmd.AddCommand(roleHomeCmd)
	roleCmd.AddCommand(roleDetectCmd)
	roleCmd.AddCommand(roleListCmd)
	roleCmd.AddCommand(roleEnvCmd)

	// Add --warband and --raider flags to home command for overrides
	roleHomeCmd.Flags().StringVar(&roleRig, "warband", "", "Warband name (required for warband-specific roles)")
	roleHomeCmd.Flags().StringVar(&roleRaider, "raider", "", "Raider/clan member name")
}

// GetRole returns the current role, checking HD_ROLE first then falling back to cwd.
// This is the canonical function for role detection.
func GetRole() (RoleInfo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return RoleInfo{}, fmt.Errorf("getting current directory: %w", err)
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return RoleInfo{}, fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return RoleInfo{}, fmt.Errorf("not in a Horde workspace")
	}

	return GetRoleWithContext(cwd, townRoot)
}

// GetRoleWithContext returns role info given explicit cwd and encampment root.
func GetRoleWithContext(cwd, townRoot string) (RoleInfo, error) {
	info := RoleInfo{
		TownRoot: townRoot,
		WorkDir:  cwd,
	}

	// Check environment variable first
	envRole := os.Getenv(EnvGTRole)
	info.EnvRole = envRole

	// Always detect from cwd for comparison/fallback
	cwdCtx := detectRole(cwd, townRoot)
	info.CwdRole = cwdCtx.Role

	// Determine authoritative role
	if envRole != "" {
		// Parse env role - it might be simple ("warchief") or compound ("horde/witness")
		parsedRole, warband, raider := parseRoleString(envRole)
		info.Role = parsedRole
		info.Warband = warband
		info.Raider = raider
		info.Source = "env"

		// For simple role strings like "clan" or "raider", also check
		// HD_WARBAND and HD_CLAN/HD_RAIDER env vars for the full identity
		if info.Warband == "" {
			if envRig := os.Getenv("HD_WARBAND"); envRig != "" {
				info.Warband = envRig
			}
		}
		if info.Raider == "" {
			if envCrew := os.Getenv("HD_CLAN"); envCrew != "" {
				info.Raider = envCrew
			} else if envRaider := os.Getenv("HD_RAIDER"); envRaider != "" {
				info.Raider = envRaider
			}
		}

		// If env is incomplete (missing warband/raider for roles that need them),
		// fill gaps from cwd detection and mark as incomplete
		needsRig := parsedRole == RoleWitness || parsedRole == RoleForge || parsedRole == RoleRaider || parsedRole == RoleCrew
		needsRaider := parsedRole == RoleRaider || parsedRole == RoleCrew

		if needsRig && info.Warband == "" && cwdCtx.Warband != "" {
			info.Warband = cwdCtx.Warband
			info.EnvIncomplete = true
		}
		if needsRaider && info.Raider == "" && cwdCtx.Raider != "" {
			info.Raider = cwdCtx.Raider
			info.EnvIncomplete = true
		}

		// Check for mismatch with cwd detection
		if cwdCtx.Role != RoleUnknown && cwdCtx.Role != parsedRole {
			info.Mismatch = true
		}
	} else {
		// Fall back to cwd detection - copy all fields from cwdCtx
		info.Role = cwdCtx.Role
		info.Warband = cwdCtx.Warband
		info.Raider = cwdCtx.Raider
		info.Source = "cwd"
	}

	// Determine home directory
	info.Home = getRoleHome(info.Role, info.Warband, info.Raider, townRoot)

	return info, nil
}

// parseRoleString parses a role string like "warchief", "horde/witness", or "horde/raiders/alpha".
func parseRoleString(s string) (Role, string, string) {
	s = strings.TrimSpace(s)

	// Simple roles
	switch s {
	case "warchief":
		return RoleWarchief, "", ""
	case "shaman":
		return RoleShaman, "", ""
	}

	// Compound roles: warband/role or warband/raiders/name or warband/clan/name
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		// Unknown format, try to match as simple role
		return Role(s), "", ""
	}

	warband := parts[0]

	switch parts[1] {
	case "witness":
		return RoleWitness, warband, ""
	case "forge":
		return RoleForge, warband, ""
	case "raiders":
		if len(parts) >= 3 {
			return RoleRaider, warband, parts[2]
		}
		return RoleRaider, warband, ""
	case "clan":
		if len(parts) >= 3 {
			return RoleCrew, warband, parts[2]
		}
		return RoleCrew, warband, ""
	default:
		// Might be warband/raiderName format
		return RoleRaider, warband, parts[1]
	}
}

// ActorString returns the actor identity string for relics attribution.
// Format matches relics created_by convention:
//   - Simple roles: "warchief", "shaman"
//   - Warband-specific: "horde/witness", "horde/forge"
//   - Workers: "horde/clan/max", "horde/raiders/Toast"
func (info RoleInfo) ActorString() string {
	switch info.Role {
	case RoleWarchief:
		return "warchief"
	case RoleShaman:
		return "shaman"
	case RoleWitness:
		if info.Warband != "" {
			return fmt.Sprintf("%s/witness", info.Warband)
		}
		return "witness"
	case RoleForge:
		if info.Warband != "" {
			return fmt.Sprintf("%s/forge", info.Warband)
		}
		return "forge"
	case RoleRaider:
		if info.Warband != "" && info.Raider != "" {
			return fmt.Sprintf("%s/raiders/%s", info.Warband, info.Raider)
		}
		return "raider"
	case RoleCrew:
		if info.Warband != "" && info.Raider != "" {
			return fmt.Sprintf("%s/clan/%s", info.Warband, info.Raider)
		}
		return "clan"
	default:
		return string(info.Role)
	}
}

// getRoleHome returns the canonical home directory for a role.
func getRoleHome(role Role, warband, raider, townRoot string) string {
	switch role {
	case RoleWarchief:
		return filepath.Join(townRoot, "warchief")
	case RoleShaman:
		return filepath.Join(townRoot, "shaman")
	case RoleWitness:
		if warband == "" {
			return ""
		}
		return filepath.Join(townRoot, warband, "witness")
	case RoleForge:
		if warband == "" {
			return ""
		}
		return filepath.Join(townRoot, warband, "forge", "warband")
	case RoleRaider:
		if warband == "" || raider == "" {
			return ""
		}
		return filepath.Join(townRoot, warband, "raiders", raider, "warband")
	case RoleCrew:
		if warband == "" || raider == "" {
			return ""
		}
		return filepath.Join(townRoot, warband, "clan", raider, "warband")
	default:
		return ""
	}
}

func runRoleShow(cmd *cobra.Command, args []string) error {
	info, err := GetRole()
	if err != nil {
		return err
	}

	// Header
	fmt.Printf("%s\n", style.Bold.Render(string(info.Role)))
	fmt.Printf("Source: %s\n", info.Source)

	if info.Home != "" {
		fmt.Printf("Home: %s\n", info.Home)
	}

	if info.Warband != "" {
		fmt.Printf("Warband: %s\n", info.Warband)
	}

	if info.Raider != "" {
		fmt.Printf("Worker: %s\n", info.Raider)
	}

	// Show mismatch warning
	if info.Mismatch {
		fmt.Println()
		fmt.Printf("%s\n", style.Bold.Render("⚠️  ROLE MISMATCH"))
		fmt.Printf("  HD_ROLE=%s (authoritative)\n", info.EnvRole)
		fmt.Printf("  cwd suggests: %s\n", info.CwdRole)
		fmt.Println()
		fmt.Println("The HD_ROLE env var takes precedence, but you may be in the wrong directory.")
		fmt.Printf("Expected home: %s\n", info.Home)
	}

	return nil
}

func runRoleHome(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	// Validate flag combinations: --raider requires --warband to prevent strange merges
	if roleRaider != "" && roleRig == "" {
		return fmt.Errorf("--raider requires --warband to be specified")
	}

	// Start with current role detection (from env vars or cwd)
	info, err := GetRole()
	if err != nil {
		return err
	}
	role := info.Role
	warband := info.Warband
	raider := info.Raider

	// Apply overrides from arguments/flags
	if len(args) > 0 {
		role, _, _ = parseRoleString(args[0])
	}
	if roleRig != "" {
		warband = roleRig
	}
	if roleRaider != "" {
		raider = roleRaider
	}

	home := getRoleHome(role, warband, raider, townRoot)
	if home == "" {
		return fmt.Errorf("cannot determine home for role %s (warband=%q, raider=%q)", role, warband, raider)
	}

	// Warn if computed home doesn't match cwd
	if home != cwd && !strings.HasPrefix(cwd, home) {
		fmt.Fprintf(os.Stderr, "⚠️  Warning: cwd (%s) is not within role home (%s)\n", cwd, home)
	}

	fmt.Println(home)
	return nil
}

func runRoleDetect(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	ctx := detectRole(cwd, townRoot)

	fmt.Printf("%s (from cwd)\n", style.Bold.Render(string(ctx.Role)))
	fmt.Printf("Directory: %s\n", cwd)

	if ctx.Warband != "" {
		fmt.Printf("Warband: %s\n", ctx.Warband)
	}
	if ctx.Raider != "" {
		fmt.Printf("Worker: %s\n", ctx.Raider)
	}

	// Check if env var disagrees
	envRole := os.Getenv(EnvGTRole)
	if envRole != "" {
		parsedRole, _, _ := parseRoleString(envRole)
		if parsedRole != ctx.Role {
			fmt.Println()
			fmt.Printf("%s\n", style.Bold.Render("⚠️  Mismatch with $HD_ROLE"))
			fmt.Printf("  $HD_ROLE=%s\n", envRole)
			fmt.Println("  The env var takes precedence in normal operation.")
		}
	}

	return nil
}

func runRoleList(cmd *cobra.Command, args []string) error {
	roles := []struct {
		name Role
		desc string
	}{
		{RoleWarchief, "Global coordinator at warchief/"},
		{RoleShaman, "Background supervisor daemon"},
		{RoleWitness, "Per-warband raider lifecycle manager"},
		{RoleForge, "Per-warband merge queue processor"},
		{RoleRaider, "Ephemeral worker with own worktree"},
		{RoleCrew, "Persistent worker with own worktree"},
	}

	fmt.Println("Available roles:")
	fmt.Println()
	for _, r := range roles {
		fmt.Printf("  %-10s  %s\n", style.Bold.Render(string(r.name)), r.desc)
	}
	return nil
}

func runRoleEnv(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Horde workspace")
	}

	// Get current role (read-only - from env vars or cwd)
	info, err := GetRole()
	if err != nil {
		return err
	}

	home := getRoleHome(info.Role, info.Warband, info.Raider, townRoot)
	if home == "" {
		return fmt.Errorf("cannot determine home for role %s (warband=%q, raider=%q)", info.Role, info.Warband, info.Raider)
	}

	// Warn if env was incomplete and we filled from cwd
	if info.EnvIncomplete {
		fmt.Fprintf(os.Stderr, "⚠️  Warning: env vars incomplete, filled from cwd\n")
	}

	// Warn if computed home doesn't match cwd
	if home != cwd && !strings.HasPrefix(cwd, home) {
		fmt.Fprintf(os.Stderr, "⚠️  Warning: cwd (%s) is not within role home (%s)\n", cwd, home)
	}

	// Get canonical env vars from shared source of truth
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:      string(info.Role),
		Warband:       info.Warband,
		AgentName: info.Raider,
		TownRoot:  townRoot,
	})
	envVars[EnvGTRoleHome] = home

	// Output in sorted order for consistent output
	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("export %s=%s\n", k, envVars[k])
	}

	return nil
}
