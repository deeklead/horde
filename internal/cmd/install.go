package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/claude"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/deps"
	"github.com/deeklead/horde/internal/ritual"
	"github.com/deeklead/horde/internal/shell"
	"github.com/deeklead/horde/internal/state"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/templates"
	"github.com/deeklead/horde/internal/workspace"
	"github.com/deeklead/horde/internal/wrappers"
)

var (
	installForce      bool
	installName       string
	installOwner      string
	installPublicName string
	installNoRelics    bool
	installGit        bool
	installGitHub     string
	installPublic     bool
	installShell      bool
	installWrappers   bool
)

var installCmd = &cobra.Command{
	Use:     "install [path]",
	GroupID: GroupWorkspace,
	Short:   "Create a new Horde HQ (workspace)",
	Long: `Create a new Horde HQ at the specified path.

The HQ (headquarters) is the top-level directory where Horde is installed -
the root of your workspace where all warbands and agents live. It contains:
  - CLAUDE.md            Warchief role context (Warchief runs from HQ root)
  - warchief/               Warchief config, state, and warband registry
  - .relics/              Encampment-level relics DB (hq-* prefix for warchief drums)

If path is omitted, uses the current directory.

See docs/hq.md for advanced HQ configurations including relics
redirects, multi-system setups, and HQ templates.

Examples:
  hd install ~/horde                              # Create HQ at ~/horde
  hd install . --name my-workspace             # Initialize current dir
  hd install ~/horde --no-relics                   # Skip .relics/ initialization
  hd install ~/horde --git                        # Also init git with .gitignore
  hd install ~/horde --github=user/repo           # Create private GitHub repo (default)
  hd install ~/horde --github=user/repo --public  # Create public GitHub repo
  hd install ~/horde --shell                      # Install shell integration (sets HD_ENCAMPMENT_ROOT/HD_WARBAND)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInstall,
}

func init() {
	installCmd.Flags().BoolVarP(&installForce, "force", "f", false, "Overwrite existing HQ")
	installCmd.Flags().StringVarP(&installName, "name", "n", "", "Encampment name (defaults to directory name)")
	installCmd.Flags().StringVar(&installOwner, "owner", "", "Owner email for entity identity (defaults to git config user.email)")
	installCmd.Flags().StringVar(&installPublicName, "public-name", "", "Public display name (defaults to encampment name)")
	installCmd.Flags().BoolVar(&installNoRelics, "no-relics", false, "Skip encampment relics initialization")
	installCmd.Flags().BoolVar(&installGit, "git", false, "Initialize git with .gitignore")
	installCmd.Flags().StringVar(&installGitHub, "github", "", "Create GitHub repo (format: owner/repo, private by default)")
	installCmd.Flags().BoolVar(&installPublic, "public", false, "Make GitHub repo public (use with --github)")
	installCmd.Flags().BoolVar(&installShell, "shell", false, "Install shell integration (sets HD_ENCAMPMENT_ROOT/HD_WARBAND env vars)")
	installCmd.Flags().BoolVar(&installWrappers, "wrappers", false, "Install hd-codex/hd-opencode wrapper scripts to ~/bin/")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	// Determine target path
	targetPath := "."
	if len(args) > 0 {
		targetPath = args[0]
	}

	// Expand ~ and resolve to absolute path
	if targetPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		targetPath = filepath.Join(home, targetPath[1:])
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	// Determine encampment name
	townName := installName
	if townName == "" {
		townName = filepath.Base(absPath)
	}

	// Check if already a workspace
	if isWS, _ := workspace.IsWorkspace(absPath); isWS && !installForce {
		// If only --wrappers is requested in existing encampment, just install wrappers and exit
		if installWrappers {
			if err := wrappers.Install(); err != nil {
				return fmt.Errorf("installing wrapper scripts: %w", err)
			}
			fmt.Printf("✓ Installed hd-codex and hd-opencode to %s\n", wrappers.BinDir())
			return nil
		}
		return fmt.Errorf("directory is already a Horde HQ (use --force to reinitialize)")
	}

	// Check if inside an existing workspace
	if existingRoot, _ := workspace.Find(absPath); existingRoot != "" && existingRoot != absPath {
		style.PrintWarning("Creating HQ inside existing workspace at %s", existingRoot)
	}

	// Ensure relics (bd) is available before proceeding
	if !installNoRelics {
		if err := deps.EnsureRelics(true); err != nil {
			return fmt.Errorf("relics dependency check failed: %w", err)
		}
	}

	fmt.Printf("%s Creating Horde HQ at %s\n\n",
		style.Bold.Render("🏭"), style.Dim.Render(absPath))

	// Create directory structure
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Create warchief directory (holds config, state, and drums)
	warchiefDir := filepath.Join(absPath, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		return fmt.Errorf("creating warchief directory: %w", err)
	}
	fmt.Printf("   ✓ Created warchief/\n")

	// Determine owner (defaults to git user.email)
	owner := installOwner
	if owner == "" {
		out, err := exec.Command("git", "config", "user.email").Output()
		if err == nil {
			owner = strings.TrimSpace(string(out))
		}
	}

	// Determine public name (defaults to encampment name)
	publicName := installPublicName
	if publicName == "" {
		publicName = townName
	}

	// Create encampment.json in warchief/
	townConfig := &config.TownConfig{
		Type:       "encampment",
		Version:    config.CurrentTownVersion,
		Name:       townName,
		Owner:      owner,
		PublicName: publicName,
		CreatedAt:  time.Now(),
	}
	townPath := filepath.Join(warchiefDir, "encampment.json")
	if err := config.SaveTownConfig(townPath, townConfig); err != nil {
		return fmt.Errorf("writing encampment.json: %w", err)
	}
	fmt.Printf("   ✓ Created warchief/encampment.json\n")

	// Create warbands.json in warchief/
	rigsConfig := &config.RigsConfig{
		Version: config.CurrentRigsVersion,
		Warbands:    make(map[string]config.RigEntry),
	}
	rigsPath := filepath.Join(warchiefDir, "warbands.json")
	if err := config.SaveRigsConfig(rigsPath, rigsConfig); err != nil {
		return fmt.Errorf("writing warbands.json: %w", err)
	}
	fmt.Printf("   ✓ Created warchief/warbands.json\n")

	// Create Warchief CLAUDE.md at warchief/ (Warchief's canonical home)
	// IMPORTANT: CLAUDE.md must be in ~/horde/warchief/, NOT ~/horde/
	// CLAUDE.md at encampment root would be inherited by ALL agents via directory traversal,
	// causing clan/raider/etc to receive Warchief-specific instructions.
	if err := createWarchiefCLAUDEmd(warchiefDir, absPath); err != nil {
		fmt.Printf("   %s Could not create CLAUDE.md: %v\n", style.Dim.Render("⚠"), err)
	} else {
		fmt.Printf("   ✓ Created warchief/CLAUDE.md\n")
	}

	// Create warchief settings (warchief runs from ~/horde/warchief/)
	// IMPORTANT: Settings must be in ~/horde/warchief/.claude/, NOT ~/horde/.claude/
	// Settings at encampment root would be found by ALL agents via directory traversal,
	// causing clan/raider/etc to cd to encampment root before running commands.
	// warchiefDir already defined above
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		fmt.Printf("   %s Could not create warchief directory: %v\n", style.Dim.Render("⚠"), err)
	} else if err := claude.EnsureSettingsForRole(warchiefDir, "warchief"); err != nil {
		fmt.Printf("   %s Could not create warchief settings: %v\n", style.Dim.Render("⚠"), err)
	} else {
		fmt.Printf("   ✓ Created warchief/.claude/settings.json\n")
	}

	// Create shaman directory and settings (shaman runs from ~/horde/shaman/)
	shamanDir := filepath.Join(absPath, "shaman")
	if err := os.MkdirAll(shamanDir, 0755); err != nil {
		fmt.Printf("   %s Could not create shaman directory: %v\n", style.Dim.Render("⚠"), err)
	} else if err := claude.EnsureSettingsForRole(shamanDir, "shaman"); err != nil {
		fmt.Printf("   %s Could not create shaman settings: %v\n", style.Dim.Render("⚠"), err)
	} else {
		fmt.Printf("   ✓ Created shaman/.claude/settings.json\n")
	}

	// Initialize git BEFORE relics so that rl can compute repository fingerprint.
	// The fingerprint is required for the daemon to start properly.
	if installGit || installGitHub != "" {
		fmt.Println()
		if err := InitGitForHarness(absPath, installGitHub, !installPublic); err != nil {
			return fmt.Errorf("git initialization failed: %w", err)
		}
	}

	// Initialize encampment-level relics database (optional)
	// Encampment relics (hq- prefix) stores warchief drums, cross-warband coordination, and handoffs.
	// Warband relics are separate and have their own prefixes.
	if !installNoRelics {
		if err := initTownRelics(absPath); err != nil {
			fmt.Printf("   %s Could not initialize encampment relics: %v\n", style.Dim.Render("⚠"), err)
		} else {
			fmt.Printf("   ✓ Initialized .relics/ (encampment-level relics with hq- prefix)\n")

			// Provision embedded rituals to .relics/rituals/
			if count, err := ritual.ProvisionFormulas(absPath); err != nil {
				// Non-fatal: rituals are optional, just convenience
				fmt.Printf("   %s Could not provision rituals: %v\n", style.Dim.Render("⚠"), err)
			} else if count > 0 {
				fmt.Printf("   ✓ Provisioned %d rituals\n", count)
			}
		}

		// Create encampment-level agent relics (Warchief, Shaman) and role relics.
		// These use hq- prefix and are stored in encampment relics for cross-warband coordination.
		if err := initTownAgentRelics(absPath); err != nil {
			fmt.Printf("   %s Could not create encampment-level agent relics: %v\n", style.Dim.Render("⚠"), err)
		}
	}

	// Detect and save overseer identity
	overseer, err := config.DetectOverseer(absPath)
	if err != nil {
		fmt.Printf("   %s Could not detect overseer identity: %v\n", style.Dim.Render("⚠"), err)
	} else {
		overseerPath := config.OverseerConfigPath(absPath)
		if err := config.SaveOverseerConfig(overseerPath, overseer); err != nil {
			fmt.Printf("   %s Could not save overseer config: %v\n", style.Dim.Render("⚠"), err)
		} else {
			fmt.Printf("   ✓ Detected overseer: %s (via %s)\n", overseer.FormatOverseerIdentity(), overseer.Source)
		}
	}

	// Create default escalation config in settings/escalation.json
	escalationPath := config.EscalationConfigPath(absPath)
	if err := config.SaveEscalationConfig(escalationPath, config.NewEscalationConfig()); err != nil {
		fmt.Printf("   %s Could not create escalation config: %v\n", style.Dim.Render("⚠"), err)
	} else {
		fmt.Printf("   ✓ Created settings/escalation.json\n")
	}

	// Provision encampment-level slash commands (.claude/commands/)
	// All agents inherit these via Claude's directory traversal - no per-workspace copies needed.
	if err := templates.ProvisionCommands(absPath); err != nil {
		fmt.Printf("   %s Could not provision slash commands: %v\n", style.Dim.Render("⚠"), err)
	} else {
		fmt.Printf("   ✓ Created .claude/commands/ (slash commands for all agents)\n")
	}

	if installShell {
		fmt.Println()
		if err := shell.Install(); err != nil {
			fmt.Printf("   %s Could not install shell integration: %v\n", style.Dim.Render("⚠"), err)
		} else {
			fmt.Printf("   ✓ Installed shell integration (%s)\n", shell.RCFilePath(shell.DetectShell()))
		}
		if err := state.Enable(Version); err != nil {
			fmt.Printf("   %s Could not enable Horde: %v\n", style.Dim.Render("⚠"), err)
		} else {
			fmt.Printf("   ✓ Enabled Horde globally\n")
		}
	}

	if installWrappers {
		fmt.Println()
		if err := wrappers.Install(); err != nil {
			fmt.Printf("   %s Could not install wrapper scripts: %v\n", style.Dim.Render("⚠"), err)
		} else {
			fmt.Printf("   ✓ Installed hd-codex and hd-opencode to %s\n", wrappers.BinDir())
		}
	}

	fmt.Printf("\n%s HQ created successfully!\n", style.Bold.Render("✓"))
	fmt.Println()
	fmt.Println("Next steps:")
	step := 1
	if !installGit && installGitHub == "" {
		fmt.Printf("  %d. Initialize git: %s\n", step, style.Dim.Render("hd git-init"))
		step++
	}
	fmt.Printf("  %d. Add a warband: %s\n", step, style.Dim.Render("hd warband add <name> <git-url>"))
	step++
	fmt.Printf("  %d. (Optional) Configure agents: %s\n", step, style.Dim.Render("hd config agent list"))
	step++
	fmt.Printf("  %d. Enter the Warchief's office: %s\n", step, style.Dim.Render("hd warchief summon"))

	return nil
}

func createWarchiefCLAUDEmd(warchiefDir, _ string) error {
	// Create a minimal bootstrap pointer instead of full context.
	// Full context is injected ephemerally by `hd rally` at session start.
	// This keeps the on-disk file small (<30 lines) per priming architecture.
	bootstrap := `# Warchief Context

> **Recovery**: Run ` + "`hd rally`" + ` after compaction, clear, or new session

Full context is injected by ` + "`hd rally`" + ` at session start.

## Quick Reference

- Check drums: ` + "`hd drums inbox`" + `
- Check warbands: ` + "`hd warband list`" + `
- Start scout: ` + "`hd scout start`" + `
`
	claudePath := filepath.Join(warchiefDir, "CLAUDE.md")
	return os.WriteFile(claudePath, []byte(bootstrap), 0644)
}

func writeJSON(path string, data interface{}) error {
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

// initTownRelics initializes encampment-level relics database using rl init.
// Encampment relics use the "hq-" prefix for warchief drums and cross-warband coordination.
func initTownRelics(townPath string) error {
	// Run: rl init --prefix hq
	cmd := exec.Command("rl", "init", "--prefix", "hq")
	cmd.Dir = townPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if relics is already initialized
		if strings.Contains(string(output), "already initialized") {
			// Already initialized - still need to ensure fingerprint exists
		} else {
			return fmt.Errorf("bd init failed: %s", strings.TrimSpace(string(output)))
		}
	}

	// Configure custom types for Horde (agent, role, warband, raid, slot).
	// These were extracted from relics core in v0.46.0 and now require explicit config.
	configCmd := exec.Command("rl", "config", "set", "types.custom", constants.RelicsCustomTypes)
	configCmd.Dir = townPath
	if configOutput, configErr := configCmd.CombinedOutput(); configErr != nil {
		// Non-fatal: older relics versions don't need this, newer ones do
		fmt.Printf("   %s Could not set custom types: %s\n", style.Dim.Render("⚠"), strings.TrimSpace(string(configOutput)))
	}

	// Ensure database has repository fingerprint (GH #25).
	// This is idempotent - safe on both new and legacy (pre-0.17.5) databases.
	// Without fingerprint, the rl daemon fails to start silently.
	if err := ensureRepoFingerprint(townPath); err != nil {
		// Non-fatal: fingerprint is optional for functionality, just daemon optimization
		fmt.Printf("   %s Could not verify repo fingerprint: %v\n", style.Dim.Render("⚠"), err)
	}

	// Ensure issues.jsonl exists BEFORE creating routes.jsonl.
	// rl init creates relics.db but not issues.jsonl in SQLite mode.
	// If routes.jsonl is created first, bd's auto-export will write issues to routes.jsonl,
	// corrupting it. Creating an empty issues.jsonl prevents this.
	issuesJSONL := filepath.Join(townPath, ".relics", "issues.jsonl")
	if _, err := os.Stat(issuesJSONL); os.IsNotExist(err) {
		if err := os.WriteFile(issuesJSONL, []byte{}, 0644); err != nil {
			fmt.Printf("   %s Could not create issues.jsonl: %v\n", style.Dim.Render("⚠"), err)
		}
	}

	// Ensure routes.jsonl has an explicit encampment-level mapping for hq-* relics.
	// This keeps hq-* operations stable even when invoked from warband worktrees.
	if err := relics.AppendRoute(townPath, relics.Route{Prefix: "hq-", Path: "."}); err != nil {
		// Non-fatal: routing still works in many contexts, but explicit mapping is preferred.
		fmt.Printf("   %s Could not update routes.jsonl: %v\n", style.Dim.Render("⚠"), err)
	}

	// Register hq-cv- prefix for raid relics (auto-created by hd charge).
	// Raids use hq-cv-* IDs for visual distinction from other encampment relics.
	if err := relics.AppendRoute(townPath, relics.Route{Prefix: "hq-cv-", Path: "."}); err != nil {
		fmt.Printf("   %s Could not register raid prefix: %v\n", style.Dim.Render("⚠"), err)
	}

	return nil
}

// ensureRepoFingerprint runs rl migrate --update-repo-id to ensure the database
// has a repository fingerprint. Legacy databases (pre-0.17.5) lack this, which
// prevents the daemon from starting properly.
func ensureRepoFingerprint(relicsPath string) error {
	cmd := exec.Command("rl", "migrate", "--update-repo-id")
	cmd.Dir = relicsPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd migrate --update-repo-id: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// ensureCustomTypes registers Horde custom issue types with relics.
// Relics core only supports built-in types (bug, feature, task, etc.).
// Horde needs custom types: agent, role, warband, raid, slot.
// This is idempotent - safe to call multiple times.
func ensureCustomTypes(relicsPath string) error {
	cmd := exec.Command("rl", "config", "set", "types.custom", constants.RelicsCustomTypes)
	cmd.Dir = relicsPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd config set types.custom: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// initTownAgentRelics creates encampment-level agent and role relics using hq- prefix.
// This creates:
//   - hq-warchief, hq-shaman (agent relics for encampment-level agents)
//   - hq-warchief-role, hq-shaman-role, hq-witness-role, hq-forge-role,
//     hq-raider-role, hq-clan-role (role definition relics)
//
// These relics are stored in encampment relics (~/horde/.relics/) and are shared across all warbands.
// Warband-level agent relics (witness, forge) are created by hd warband add in warband relics.
//
// ERROR HANDLING ASYMMETRY:
// Agent relics (Warchief, Shaman) use hard fail - installation aborts if creation fails.
// Role relics use soft fail - logs warning and continues if creation fails.
//
// Rationale: Agent relics are identity relics that track agent state, hooks, and
// form the foundation of the CV/reputation ledger. Without them, agents cannot
// be properly tracked or coordinated. Role relics are documentation templates
// that define role characteristics but are not required for agent operation -
// agents can function without their role bead existing.
func initTownAgentRelics(townPath string) error {
	bd := relics.New(townPath)

	// rl init doesn't enable "custom" issue types by default, but Horde uses
	// agent/role relics during install and runtime. Ensure these types are enabled
	// before attempting to create any encampment-level system relics.
	if err := ensureRelicsCustomTypes(townPath, []string{"agent", "role", "warband", "raid", "slot"}); err != nil {
		return err
	}

	// Role relics (global templates) - use shared definitions from relics package
	for _, role := range relics.AllRoleBeadDefs() {
		// Check if already exists
		if _, err := bd.Show(role.ID); err == nil {
			continue // Already exists
		}

		// Create role bead using the relics API
		// CreateWithID with Type: "role" automatically adds gt:role label
		_, err := bd.CreateWithID(role.ID, relics.CreateOptions{
			Title:       role.Title,
			Type:        "role",
			Description: role.Desc,
			Priority:    -1, // No priority
		})
		if err != nil {
			// Log but continue - role relics are optional
			fmt.Printf("   %s Could not create role bead %s: %v\n",
				style.Dim.Render("⚠"), role.ID, err)
			continue
		}
		fmt.Printf("   ✓ Created role bead: %s\n", role.ID)
	}

	// Encampment-level agent relics
	agentDefs := []struct {
		id       string
		roleType string
		title    string
	}{
		{
			id:       relics.WarchiefBeadIDTown(),
			roleType: "warchief",
			title:    "Warchief - global coordinator, handles cross-warband communication and escalations.",
		},
		{
			id:       relics.ShamanBeadIDTown(),
			roleType: "shaman",
			title:    "Shaman (daemon beacon) - receives mechanical heartbeats, runs encampment plugins and monitoring.",
		},
	}

	existingAgents, err := bd.List(relics.ListOptions{
		Status:   "all",
		Type:     "agent",
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("listing existing agent relics: %w", err)
	}
	existingAgentIDs := make(map[string]struct{}, len(existingAgents))
	for _, issue := range existingAgents {
		existingAgentIDs[issue.ID] = struct{}{}
	}

	for _, agent := range agentDefs {
		if _, ok := existingAgentIDs[agent.id]; ok {
			continue
		}

		fields := &relics.AgentFields{
			RoleType:   agent.roleType,
			Warband:        "", // Encampment-level agents have no warband
			AgentState: "idle",
			BannerBead:   "",
			RoleBead:   relics.RoleBeadIDTown(agent.roleType),
		}

		if _, err := bd.CreateAgentBead(agent.id, agent.title, fields); err != nil {
			return fmt.Errorf("creating %s: %w", agent.id, err)
		}
		fmt.Printf("   ✓ Created agent bead: %s\n", agent.id)
	}

	return nil
}

func ensureRelicsCustomTypes(workDir string, types []string) error {
	if len(types) == 0 {
		return nil
	}

	cmd := exec.Command("rl", "config", "set", "types.custom", strings.Join(types, ","))
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd config set types.custom failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}
