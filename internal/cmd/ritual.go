package cmd

import (
	"bufio"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Ritual command flags
var (
	formulaListJSON   bool
	formulaShowJSON   bool
	formulaRunPR      int
	formulaRunRig     string
	formulaRunDryRun  bool
	formulaCreateType string
)

var formulaCmd = &cobra.Command{
	Use:     "ritual",
	Aliases: []string{"rituals"},
	GroupID: GroupWork,
	Short:   "Manage workflow rituals",
	RunE:    requireSubcommand,
	Long: `Manage workflow rituals - reusable totem templates.

Rituals are TOML/JSON files that define workflows with steps, variables,
and composition rules. They can be "poured" to create totems or "wisped"
for ephemeral scout cycles.

Commands:
  list    List available rituals from all search paths
  show    Display ritual details (steps, variables, composition)
  run     Execute a ritual (cast and dispatch)
  create  Create a new ritual template

Search paths (in order):
  1. .relics/rituals/ (project)
  2. ~/.relics/rituals/ (user)
  3. $HD_ROOT/.relics/rituals/ (orchestrator)

Examples:
  hd ritual list                    # List all rituals
  hd ritual show shiny              # Show ritual details
  hd ritual run shiny --pr=123      # Run ritual on PR #123
  hd ritual create my-workflow      # Create new ritual template`,
}

var formulaListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available rituals",
	Long: `List available rituals from all search paths.

Searches for ritual files (.ritual.toml, .ritual.json) in:
  1. .relics/rituals/ (project)
  2. ~/.relics/rituals/ (user)
  3. $HD_ROOT/.relics/rituals/ (orchestrator)

Examples:
  hd ritual list            # List all rituals
  hd ritual list --json     # JSON output`,
	RunE: runFormulaList,
}

var formulaShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Display ritual details",
	Long: `Display detailed information about a ritual.

Shows:
  - Ritual metadata (name, type, description)
  - Variables with defaults and constraints
  - Steps with dependencies
  - Composition rules (extends, aspects)

Examples:
  hd ritual show shiny
  hd ritual show rule-of-five --json`,
	Args: cobra.ExactArgs(1),
	RunE: runFormulaShow,
}

var formulaRunCmd = &cobra.Command{
	Use:   "run [name]",
	Short: "Execute a ritual",
	Long: `Execute a ritual by pouring it and dispatching work.

This command:
  1. Looks up the ritual by name (or uses default from warband config)
  2. Pours it to create a totem (or uses existing proto)
  3. Dispatches the totem to available workers

For PR-based workflows, use --pr to specify the GitHub PR number.

If no ritual name is provided, uses the default ritual configured in
the warband's settings/config.json under workflow.default_formula.

Options:
  --pr=N      Run ritual on GitHub PR #N
  --warband=NAME  Target specific warband (default: current or horde)
  --dry-run   Show what would happen without executing

Examples:
  hd ritual run shiny                    # Run ritual in current warband
  hd ritual run                          # Run default ritual from warband config
  hd ritual run shiny --pr=123           # Run on PR #123
  hd ritual run security-audit --warband=relics  # Run in specific warband
  hd ritual run release --dry-run        # Preview execution`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFormulaRun,
}

var formulaCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new ritual template",
	Long: `Create a new ritual template file.

Creates a starter ritual file in .relics/rituals/ with the given name.
The template includes common sections that you can customize.

Ritual types:
  task      Single-step task ritual (default)
  workflow  Multi-step workflow with dependencies
  scout    Repeating scout cycle (for wisps)

Examples:
  hd ritual create my-task                  # Create task ritual
  hd ritual create my-workflow --type=workflow
  hd ritual create nightly-check --type=scout`,
	Args: cobra.ExactArgs(1),
	RunE: runFormulaCreate,
}

func init() {
	// List flags
	formulaListCmd.Flags().BoolVar(&formulaListJSON, "json", false, "Output as JSON")

	// Show flags
	formulaShowCmd.Flags().BoolVar(&formulaShowJSON, "json", false, "Output as JSON")

	// Run flags
	formulaRunCmd.Flags().IntVar(&formulaRunPR, "pr", 0, "GitHub PR number to run ritual on")
	formulaRunCmd.Flags().StringVar(&formulaRunRig, "warband", "", "Target warband (default: current or horde)")
	formulaRunCmd.Flags().BoolVar(&formulaRunDryRun, "dry-run", false, "Preview execution without running")

	// Create flags
	formulaCreateCmd.Flags().StringVar(&formulaCreateType, "type", "task", "Ritual type: task, workflow, or scout")

	// Add subcommands
	formulaCmd.AddCommand(formulaListCmd)
	formulaCmd.AddCommand(formulaShowCmd)
	formulaCmd.AddCommand(formulaRunCmd)
	formulaCmd.AddCommand(formulaCreateCmd)

	rootCmd.AddCommand(formulaCmd)
}

// runFormulaList delegates to rl ritual list
func runFormulaList(cmd *cobra.Command, args []string) error {
	bdArgs := []string{"ritual", "list"}
	if formulaListJSON {
		bdArgs = append(bdArgs, "--json")
	}

	bdCmd := exec.Command("rl", bdArgs...)
	bdCmd.Stdout = os.Stdout
	bdCmd.Stderr = os.Stderr
	return bdCmd.Run()
}

// runFormulaShow delegates to rl ritual show
func runFormulaShow(cmd *cobra.Command, args []string) error {
	formulaName := args[0]
	bdArgs := []string{"ritual", "show", formulaName}
	if formulaShowJSON {
		bdArgs = append(bdArgs, "--json")
	}

	bdCmd := exec.Command("rl", bdArgs...)
	bdCmd.Stdout = os.Stdout
	bdCmd.Stderr = os.Stderr
	return bdCmd.Run()
}

// runFormulaRun executes a ritual by spawning a raid of raiders.
// For raid-type rituals, it creates a raid bead, creates leg relics,
// and charges each leg to a separate raider with leg-specific prompts.
func runFormulaRun(cmd *cobra.Command, args []string) error {
	// Determine target warband first (needed for default ritual lookup)
	targetRig := formulaRunRig
	var rigPath string
	if targetRig == "" {
		// Try to detect from current directory
		townRoot, err := workspace.FindFromCwd()
		if err == nil && townRoot != "" {
			rigName, r, rigErr := findCurrentRig(townRoot)
			if rigErr == nil && rigName != "" {
				targetRig = rigName
				if r != nil {
					rigPath = r.Path
				}
			}
			// If we still don't have a target warband but have townRoot, use horde
			if targetRig == "" {
				targetRig = "horde"
				rigPath = filepath.Join(townRoot, "horde")
			}
		} else {
			// No encampment root found, fall back to horde without rigPath
			targetRig = "horde"
		}
	} else {
		// If warband specified, construct path
		townRoot, err := workspace.FindFromCwd()
		if err == nil && townRoot != "" {
			rigPath = filepath.Join(townRoot, targetRig)
		}
	}

	// Get ritual name from args or default
	var formulaName string
	if len(args) > 0 {
		formulaName = args[0]
	} else {
		// Try to get default ritual from warband config
		if rigPath != "" {
			formulaName = config.GetDefaultFormula(rigPath)
		}
		if formulaName == "" {
			return fmt.Errorf("no ritual specified and no default ritual configured\n\nTo set a default ritual, add to your warband's settings/config.json:\n  \"workflow\": {\n    \"default_formula\": \"<ritual-name>\"\n  }")
		}
		fmt.Printf("%s Using default ritual: %s\n", style.Dim.Render("Note:"), formulaName)
	}

	// Find the ritual file
	formulaPath, err := findFormulaFile(formulaName)
	if err != nil {
		return fmt.Errorf("finding ritual: %w", err)
	}

	// Parse the ritual
	f, err := parseFormulaFile(formulaPath)
	if err != nil {
		return fmt.Errorf("parsing ritual: %w", err)
	}

	// Handle dry-run mode
	if formulaRunDryRun {
		return dryRunFormula(f, formulaName, targetRig)
	}

	// Currently only raid rituals are supported for execution
	if f.Type != "raid" {
		fmt.Printf("%s Ritual type '%s' not yet supported for execution.\n",
			style.Dim.Render("Note:"), f.Type)
		fmt.Printf("Currently only 'raid' rituals can be run.\n")
		fmt.Printf("\nTo run '%s' manually:\n", formulaName)
		fmt.Printf("  1. View ritual:   hd ritual show %s\n", formulaName)
		fmt.Printf("  2. Invoke to proto:  rl invoke %s\n", formulaName)
		fmt.Printf("  3. Cast totem:  rl cast %s\n", formulaName)
		fmt.Printf("  4. Charge to warband:   hd charge <totem-id> %s\n", targetRig)
		return nil
	}

	// Execute raid ritual
	return executeRaidFormula(f, formulaName, targetRig)
}

// dryRunFormula shows what would happen without executing
func dryRunFormula(f *formulaData, formulaName, targetRig string) error {
	fmt.Printf("%s Would execute ritual:\n", style.Dim.Render("[dry-run]"))
	fmt.Printf("  Ritual: %s\n", style.Bold.Render(formulaName))
	fmt.Printf("  Type:    %s\n", f.Type)
	fmt.Printf("  Warband:     %s\n", targetRig)
	if formulaRunPR > 0 {
		fmt.Printf("  PR:      #%d\n", formulaRunPR)
	}

	if f.Type == "raid" && len(f.Legs) > 0 {
		fmt.Printf("\n  Legs (%d parallel):\n", len(f.Legs))
		for _, leg := range f.Legs {
			fmt.Printf("    • %s: %s\n", leg.ID, leg.Title)
		}
		if f.Synthesis != nil {
			fmt.Printf("\n  Synthesis:\n")
			fmt.Printf("    • %s\n", f.Synthesis.Title)
		}
	}

	return nil
}

// executeRaidFormula spawns a raid of raiders to execute a raid ritual
func executeRaidFormula(f *formulaData, formulaName, targetRig string) error {
	fmt.Printf("%s Executing raid ritual: %s\n\n",
		style.Bold.Render("🚚"), formulaName)

	// Get encampment relics directory for raid creation
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding encampment root: %w", err)
	}
	townRelics := filepath.Join(townRoot, ".relics")

	// Step 1: Create raid bead
	raidID := fmt.Sprintf("hq-cv-%s", generateFormulaShortID())
	raidTitle := fmt.Sprintf("%s: %s", formulaName, f.Description)
	if len(raidTitle) > 80 {
		raidTitle = raidTitle[:77] + "..."
	}

	// Build description with ritual context
	description := fmt.Sprintf("Ritual raid: %s\n\nLegs: %d\nRig: %s",
		formulaName, len(f.Legs), targetRig)
	if formulaRunPR > 0 {
		description += fmt.Sprintf("\nPR: #%d", formulaRunPR)
	}

	createArgs := []string{
		"create",
		"--type=raid",
		"--id=" + raidID,
		"--title=" + raidTitle,
		"--description=" + description,
	}

	createCmd := exec.Command("rl", createArgs...)
	createCmd.Dir = townRelics
	createCmd.Stderr = os.Stderr
	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("creating raid bead: %w", err)
	}

	fmt.Printf("%s Created raid: %s\n", style.Bold.Render("✓"), raidID)

	// Step 2: Create leg relics and track them
	legRelics := make(map[string]string) // leg.ID -> bead ID
	for _, leg := range f.Legs {
		legBeadID := fmt.Sprintf("hq-leg-%s", generateFormulaShortID())

		// Build leg description with prompt if available
		legDesc := leg.Description
		if f.Prompts != nil {
			if basePrompt, ok := f.Prompts["base"]; ok {
				legDesc = fmt.Sprintf("%s\n\n---\nBase Prompt:\n%s", leg.Description, basePrompt)
			}
		}

		legArgs := []string{
			"create",
			"--type=task",
			"--id=" + legBeadID,
			"--title=" + leg.Title,
			"--description=" + legDesc,
		}

		legCmd := exec.Command("rl", legArgs...)
		legCmd.Dir = townRelics
		legCmd.Stderr = os.Stderr
		if err := legCmd.Run(); err != nil {
			fmt.Printf("%s Failed to create leg bead for %s: %v\n",
				style.Dim.Render("Warning:"), leg.ID, err)
			continue
		}

		// Track the leg with the raid
		trackArgs := []string{"dep", "add", raidID, legBeadID, "--type=tracks"}
		trackCmd := exec.Command("rl", trackArgs...)
		trackCmd.Dir = townRelics
		if err := trackCmd.Run(); err != nil {
			fmt.Printf("%s Failed to track leg %s: %v\n",
				style.Dim.Render("Warning:"), leg.ID, err)
		}

		legRelics[leg.ID] = legBeadID
		fmt.Printf("  %s Created leg: %s (%s)\n", style.Dim.Render("○"), leg.ID, legBeadID)
	}

	// Step 3: Create synthesis bead if defined
	var synthesisBeadID string
	if f.Synthesis != nil {
		synthesisBeadID = fmt.Sprintf("hq-syn-%s", generateFormulaShortID())

		synDesc := f.Synthesis.Description
		if synDesc == "" {
			synDesc = "Synthesize findings from all legs into unified output"
		}

		synArgs := []string{
			"create",
			"--type=task",
			"--id=" + synthesisBeadID,
			"--title=" + f.Synthesis.Title,
			"--description=" + synDesc,
		}

		synCmd := exec.Command("rl", synArgs...)
		synCmd.Dir = townRelics
		synCmd.Stderr = os.Stderr
		if err := synCmd.Run(); err != nil {
			fmt.Printf("%s Failed to create synthesis bead: %v\n",
				style.Dim.Render("Warning:"), err)
		} else {
			// Track synthesis with raid
			trackArgs := []string{"dep", "add", raidID, synthesisBeadID, "--type=tracks"}
			trackCmd := exec.Command("rl", trackArgs...)
			trackCmd.Dir = townRelics
			_ = trackCmd.Run()

			// Add dependencies: synthesis depends on all legs
			for _, legBeadID := range legRelics {
				depArgs := []string{"dep", "add", synthesisBeadID, legBeadID}
				depCmd := exec.Command("rl", depArgs...)
				depCmd.Dir = townRelics
				_ = depCmd.Run()
			}

			fmt.Printf("  %s Created synthesis: %s\n", style.Dim.Render("★"), synthesisBeadID)
		}
	}

	// Step 4: Charge each leg to a raider
	fmt.Printf("\n%s Dispatching legs to raiders...\n\n", style.Bold.Render("→"))

	slingCount := 0
	for _, leg := range f.Legs {
		legBeadID, ok := legRelics[leg.ID]
		if !ok {
			continue
		}

		// Build context message for the raider
		contextMsg := fmt.Sprintf("Raid leg: %s\nFocus: %s", leg.Title, leg.Focus)

		// Use hd charge with args for leg-specific context
		slingArgs := []string{
			"charge", legBeadID, targetRig,
			"-a", leg.Description,
			"-s", leg.Title,
		}

		slingCmd := exec.Command("hd", slingArgs...)
		slingCmd.Stdout = os.Stdout
		slingCmd.Stderr = os.Stderr

		if err := slingCmd.Run(); err != nil {
			fmt.Printf("%s Failed to charge leg %s: %v\n",
				style.Dim.Render("Warning:"), leg.ID, err)
			// Add comment to bead about failure
			commentArgs := []string{"comment", legBeadID, fmt.Sprintf("Failed to charge: %v", err)}
			commentCmd := exec.Command("rl", commentArgs...)
			commentCmd.Dir = townRelics
			_ = commentCmd.Run()
			continue
		}

		slingCount++
		_ = contextMsg // Used in future for richer context
	}

	// Summary
	fmt.Printf("\n%s Raid dispatched!\n", style.Bold.Render("✓"))
	fmt.Printf("  Raid:  %s\n", raidID)
	fmt.Printf("  Legs:    %d dispatched\n", slingCount)
	if synthesisBeadID != "" {
		fmt.Printf("  Synthesis: %s (blocked until legs complete)\n", synthesisBeadID)
	}
	fmt.Printf("\n  Track progress: hd raid status %s\n", raidID)

	return nil
}

// formulaData holds parsed ritual information
type formulaData struct {
	Name        string
	Description string
	Type        string
	Legs        []formulaLeg
	Synthesis   *formulaSynthesis
	Prompts     map[string]string
}

type formulaLeg struct {
	ID          string
	Title       string
	Focus       string
	Description string
}

type formulaSynthesis struct {
	Title       string
	Description string
	DependsOn   []string
}

// findFormulaFile searches for a ritual file by name
func findFormulaFile(name string) (string, error) {
	// Search paths in order
	searchPaths := []string{}

	// 1. Project .relics/rituals/
	if cwd, err := os.Getwd(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(cwd, ".relics", "rituals"))
	}

	// 2. Encampment .relics/rituals/
	if townRoot, err := workspace.FindFromCwd(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(townRoot, ".relics", "rituals"))
	}

	// 3. User ~/.relics/rituals/
	if home, err := os.UserHomeDir(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(home, ".relics", "rituals"))
	}

	// Try each path with common extensions
	extensions := []string{".ritual.toml", ".ritual.json"}
	for _, basePath := range searchPaths {
		for _, ext := range extensions {
			path := filepath.Join(basePath, name+ext)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("ritual '%s' not found in search paths", name)
}

// parseFormulaFile parses a ritual file into formulaData
func parseFormulaFile(path string) (*formulaData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Use simple TOML parsing for the fields we need
	// (avoids importing the full ritual package which might cause cycles)
	f := &formulaData{
		Prompts: make(map[string]string),
	}

	content := string(data)

	// Parse ritual name
	if match := extractTOMLValue(content, "ritual"); match != "" {
		f.Name = match
	}

	// Parse description
	if match := extractTOMLMultiline(content, "description"); match != "" {
		f.Description = match
	}

	// Parse type
	if match := extractTOMLValue(content, "type"); match != "" {
		f.Type = match
	}

	// Parse legs (raid rituals)
	f.Legs = extractLegs(content)

	// Parse synthesis
	f.Synthesis = extractSynthesis(content)

	// Parse prompts
	f.Prompts = extractPrompts(content)

	return f, nil
}

// extractTOMLValue extracts a simple quoted value from TOML
func extractTOMLValue(content, key string) string {
	// Match: key = "value" or key = 'value'
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+" =") || strings.HasPrefix(line, key+"=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				// Remove quotes
				if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') {
					return val[1 : len(val)-1]
				}
				return val
			}
		}
	}
	return ""
}

// extractTOMLMultiline extracts a multiline string (""" ... """)
func extractTOMLMultiline(content, key string) string {
	// Look for key = """
	keyPattern := key + ` = """`
	idx := strings.Index(content, keyPattern)
	if idx == -1 {
		// Try single-line
		return extractTOMLValue(content, key)
	}

	start := idx + len(keyPattern)
	end := strings.Index(content[start:], `"""`)
	if end == -1 {
		return ""
	}

	return strings.TrimSpace(content[start : start+end])
}

// extractLegs parses [[legs]] sections from TOML
func extractLegs(content string) []formulaLeg {
	var legs []formulaLeg

	// Split by [[legs]]
	sections := strings.Split(content, "[[legs]]")
	for i, section := range sections {
		if i == 0 {
			continue // Skip content before first [[legs]]
		}

		// Find where this section ends (next [[ or EOF)
		endIdx := strings.Index(section, "[[")
		if endIdx == -1 {
			endIdx = len(section)
		}
		section = section[:endIdx]

		leg := formulaLeg{
			ID:          extractTOMLValue(section, "id"),
			Title:       extractTOMLValue(section, "title"),
			Focus:       extractTOMLValue(section, "focus"),
			Description: extractTOMLMultiline(section, "description"),
		}

		if leg.ID != "" {
			legs = append(legs, leg)
		}
	}

	return legs
}

// extractSynthesis parses [synthesis] section from TOML
func extractSynthesis(content string) *formulaSynthesis {
	idx := strings.Index(content, "[synthesis]")
	if idx == -1 {
		return nil
	}

	section := content[idx:]
	// Find where section ends
	if endIdx := strings.Index(section[1:], "\n["); endIdx != -1 {
		section = section[:endIdx+1]
	}

	syn := &formulaSynthesis{
		Title:       extractTOMLValue(section, "title"),
		Description: extractTOMLMultiline(section, "description"),
	}

	// Parse depends_on array
	if depsLine := extractTOMLValue(section, "depends_on"); depsLine != "" {
		// Simple array parsing: ["a", "b", "c"]
		depsLine = strings.Trim(depsLine, "[]")
		for _, dep := range strings.Split(depsLine, ",") {
			dep = strings.Trim(strings.TrimSpace(dep), `"'`)
			if dep != "" {
				syn.DependsOn = append(syn.DependsOn, dep)
			}
		}
	}

	if syn.Title == "" && syn.Description == "" {
		return nil
	}

	return syn
}

// extractPrompts parses [prompts] section from TOML
func extractPrompts(content string) map[string]string {
	prompts := make(map[string]string)

	idx := strings.Index(content, "[prompts]")
	if idx == -1 {
		return prompts
	}

	section := content[idx:]
	// Find where section ends
	if endIdx := strings.Index(section[1:], "\n["); endIdx != -1 {
		section = section[:endIdx+1]
	}

	// Extract base prompt
	if base := extractTOMLMultiline(section, "base"); base != "" {
		prompts["base"] = base
	}

	return prompts
}

// generateFormulaShortID generates a short random ID (5 lowercase chars)
func generateFormulaShortID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return strings.ToLower(base32.StdEncoding.EncodeToString(b)[:5])
}

// runFormulaCreate creates a new ritual template
func runFormulaCreate(cmd *cobra.Command, args []string) error {
	formulaName := args[0]

	// Find or create rituals directory
	formulasDir := ".relics/rituals"

	// Check if we're in a relics-enabled directory
	if _, err := os.Stat(".relics"); os.IsNotExist(err) {
		// Try user rituals directory
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot find home directory: %w", err)
		}
		formulasDir = filepath.Join(home, ".relics", "rituals")
	}

	// Ensure directory exists
	if err := os.MkdirAll(formulasDir, 0755); err != nil {
		return fmt.Errorf("creating rituals directory: %w", err)
	}

	// Generate filename
	filename := filepath.Join(formulasDir, formulaName+".ritual.toml")

	// Check if file already exists
	if _, err := os.Stat(filename); err == nil {
		return fmt.Errorf("ritual already exists: %s", filename)
	}

	// Generate template based on type
	var template string
	switch formulaCreateType {
	case "task":
		template = generateTaskTemplate(formulaName)
	case "workflow":
		template = generateWorkflowTemplate(formulaName)
	case "scout":
		template = generatePatrolTemplate(formulaName)
	default:
		return fmt.Errorf("unknown ritual type: %s (use: task, workflow, or scout)", formulaCreateType)
	}

	// Write the file
	if err := os.WriteFile(filename, []byte(template), 0644); err != nil {
		return fmt.Errorf("writing ritual file: %w", err)
	}

	fmt.Printf("%s Created ritual: %s\n", style.Bold.Render("✓"), filename)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Edit the ritual: %s\n", filename)
	fmt.Printf("  2. View it:          hd ritual show %s\n", formulaName)
	fmt.Printf("  3. Run it:           hd ritual run %s\n", formulaName)

	return nil
}

func generateTaskTemplate(name string) string {
	// Sanitize name for use in template
	title := strings.ReplaceAll(name, "-", " ")
	title = cases.Title(language.English).String(title)

	return fmt.Sprintf(`# Ritual: %s
# Type: task
# Created by: hd ritual create

description = """%s task.

Add a detailed description here."""
ritual = "%s"
version = 1

# Single step task
[[steps]]
id = "do-task"
title = "Execute task"
description = """
Perform the main task work.

**Steps:**
1. Understand the requirements
2. Implement the changes
3. Verify the work
"""

# Variables that can be passed when running the ritual
# [vars]
# [vars.issue]
# description = "Issue ID to work on"
# required = true
#
# [vars.target]
# description = "Target branch"
# default = "main"
`, name, title, name)
}

func generateWorkflowTemplate(name string) string {
	title := strings.ReplaceAll(name, "-", " ")
	title = cases.Title(language.English).String(title)

	return fmt.Sprintf(`# Ritual: %s
# Type: workflow
# Created by: hd ritual create

description = """%s workflow.

A multi-step workflow with dependencies between steps."""
ritual = "%s"
version = 1

# Step 1: Setup
[[steps]]
id = "setup"
title = "Setup environment"
description = """
Prepare the environment for the workflow.

**Steps:**
1. Check prerequisites
2. Set up working environment
"""

# Step 2: Implementation (depends on setup)
[[steps]]
id = "implement"
title = "Implement changes"
needs = ["setup"]
description = """
Make the necessary code changes.

**Steps:**
1. Understand requirements
2. Write code
3. Test locally
"""

# Step 3: Test (depends on implementation)
[[steps]]
id = "test"
title = "Run tests"
needs = ["implement"]
description = """
Verify the changes work correctly.

**Steps:**
1. Run unit tests
2. Run integration tests
3. Check for regressions
"""

# Step 4: Complete (depends on tests)
[[steps]]
id = "complete"
title = "Complete workflow"
needs = ["test"]
description = """
Finalize and clean up.

**Steps:**
1. Commit final changes
2. Clean up temporary files
"""

# Variables
[vars]
[vars.issue]
description = "Issue ID to work on"
required = true
`, name, title, name)
}

func generatePatrolTemplate(name string) string {
	title := strings.ReplaceAll(name, "-", " ")
	title = cases.Title(language.English).String(title)

	return fmt.Sprintf(`# Ritual: %s
# Type: scout
# Created by: hd ritual create
#
# Scout rituals are for repeating cycles (wisps).
# They run continuously and are NOT synced to git.

description = """%s scout.

A scout ritual for periodic checks. Scout rituals create wisps
(ephemeral totems) that are NOT synced to git."""
ritual = "%s"
version = 1

# The scout step(s)
[[steps]]
id = "check"
title = "Run scout check"
description = """
Perform the scout inspection.

**Check for:**
1. Health indicators
2. Warning signs
3. Items needing attention

**On findings:**
- Log the issue
- Escalate if critical
"""

# Optional: remediation step
# [[steps]]
# id = "remediate"
# title = "Fix issues"
# needs = ["check"]
# description = """
# Fix any issues found during the check.
# """

# Variables (optional)
# [vars]
# [vars.verbose]
# description = "Enable verbose output"
# default = "false"
`, name, title, name)
}

// promptYesNo asks the user a yes/no question
func promptYesNo(question string) bool {
	fmt.Printf("%s [y/N]: ", question)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}
