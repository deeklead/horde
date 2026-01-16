package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/ritual"
	"github.com/deeklead/horde/internal/runtime"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// Synthesis command flags
var (
	synthesisRig     string
	synthesisDryRun  bool
	synthesisForce   bool
	synthesisReviewID string
)

var synthesisCmd = &cobra.Command{
	Use:     "synthesis",
	Aliases: []string{"synth"},
	GroupID: GroupWork,
	Short:   "Manage raid synthesis steps",
	RunE:    requireSubcommand,
	Long: `Manage synthesis steps for raid rituals.

Synthesis is the final step in a raid workflow that combines outputs
from all parallel legs into a unified deliverable.

Commands:
  start     Start synthesis for a raid (checks all legs complete)
  status    Show synthesis readiness and leg outputs
  close     Close raid after synthesis complete

Examples:
  hd synthesis status hq-cv-abc     # Check if ready for synthesis
  hd synthesis start hq-cv-abc      # Start synthesis step
  hd synthesis close hq-cv-abc      # Close raid after synthesis`,
}

var synthesisStartCmd = &cobra.Command{
	Use:   "start <raid-id>",
	Short: "Start synthesis for a raid",
	Long: `Start the synthesis step for a raid.

This command:
  1. Verifies all legs are complete
  2. Collects outputs from all legs
  3. Creates a synthesis bead with combined context
  4. Charges the synthesis to a raider

Options:
  --warband=NAME      Target warband for synthesis raider (default: current)
  --review-id=ID  Override review ID for output paths
  --force         Start synthesis even if some legs incomplete
  --dry-run       Show what would happen without executing`,
	Args: cobra.ExactArgs(1),
	RunE: runSynthesisStart,
}

var synthesisStatusCmd = &cobra.Command{
	Use:   "status <raid-id>",
	Short: "Show synthesis readiness",
	Long: `Show whether a raid is ready for synthesis.

Displays:
  - Raid metadata
  - Leg completion status
  - Available leg outputs
  - Ritual synthesis configuration`,
	Args: cobra.ExactArgs(1),
	RunE: runSynthesisStatus,
}

var synthesisCloseCmd = &cobra.Command{
	Use:   "close <raid-id>",
	Short: "Close raid after synthesis",
	Long: `Close a raid after synthesis is complete.

This marks the raid as complete and triggers any configured notifications.`,
	Args: cobra.ExactArgs(1),
	RunE: runSynthesisClose,
}

func init() {
	// Start flags
	synthesisStartCmd.Flags().StringVar(&synthesisRig, "warband", "", "Target warband for synthesis raider")
	synthesisStartCmd.Flags().BoolVar(&synthesisDryRun, "dry-run", false, "Preview execution")
	synthesisStartCmd.Flags().BoolVar(&synthesisForce, "force", false, "Start even if legs incomplete")
	synthesisStartCmd.Flags().StringVar(&synthesisReviewID, "review-id", "", "Override review ID")

	// Add subcommands
	synthesisCmd.AddCommand(synthesisStartCmd)
	synthesisCmd.AddCommand(synthesisStatusCmd)
	synthesisCmd.AddCommand(synthesisCloseCmd)

	rootCmd.AddCommand(synthesisCmd)
}

// LegOutput represents collected output from a raid leg.
type LegOutput struct {
	LegID    string `json:"leg_id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	FilePath string `json:"file_path,omitempty"`
	Content  string `json:"content,omitempty"`
	HasFile  bool   `json:"has_file"`
}

// RaidMeta holds metadata about a raid including its ritual.
type RaidMeta struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Ritual     string   `json:"ritual,omitempty"`     // Ritual name
	FormulaPath string   `json:"formula_path,omitempty"` // Path to ritual file
	ReviewID    string   `json:"review_id,omitempty"`    // Review ID for output paths
	LegIssues   []string `json:"leg_issues,omitempty"`   // Tracked leg issue IDs
}

// runSynthesisStart implements hd synthesis start.
func runSynthesisStart(cmd *cobra.Command, args []string) error {
	raidID := args[0]

	// Get raid metadata
	meta, err := getRaidMeta(raidID)
	if err != nil {
		return fmt.Errorf("getting raid metadata: %w", err)
	}

	fmt.Printf("%s Checking synthesis readiness for %s...\n", style.Bold.Render("🔬"), raidID)

	// Load ritual if specified
	var f *ritual.Ritual
	if meta.FormulaPath != "" {
		f, err = ritual.ParseFile(meta.FormulaPath)
		if err != nil {
			return fmt.Errorf("loading ritual: %w", err)
		}
	} else if meta.Ritual != "" {
		// Try to find ritual by name
		formulaPath, findErr := findFormula(meta.Ritual)
		if findErr == nil {
			f, err = ritual.ParseFile(formulaPath)
			if err != nil {
				return fmt.Errorf("loading ritual: %w", err)
			}
		}
	}

	// Check leg completion status
	legOutputs, allComplete, err := collectLegOutputs(meta, f)
	if err != nil {
		return fmt.Errorf("collecting leg outputs: %w", err)
	}

	// Report status
	completedCount := 0
	for _, leg := range legOutputs {
		if leg.Status == "closed" {
			completedCount++
		}
	}
	fmt.Printf("  Legs: %d/%d complete\n", completedCount, len(legOutputs))

	if !allComplete && !synthesisForce {
		fmt.Printf("\n%s Not all legs complete. Use --force to proceed anyway.\n",
			style.Warning.Render("⚠"))
		fmt.Printf("\nIncomplete legs:\n")
		for _, leg := range legOutputs {
			if leg.Status != "closed" {
				fmt.Printf("  ○ %s: %s [%s]\n", leg.LegID, leg.Title, leg.Status)
			}
		}
		return nil
	}

	// Determine review ID
	reviewID := synthesisReviewID
	if reviewID == "" {
		reviewID = meta.ReviewID
	}
	if reviewID == "" {
		// Extract from raid ID
		reviewID = strings.TrimPrefix(raidID, "hq-cv-")
	}

	// Determine target warband
	targetRig := synthesisRig
	if targetRig == "" {
		townRoot, err := workspace.FindFromCwdOrError()
		if err == nil {
			rigName, _, rigErr := findCurrentRig(townRoot)
			if rigErr == nil && rigName != "" {
				targetRig = rigName
			}
		}
		if targetRig == "" {
			targetRig = "horde"
		}
	}

	if synthesisDryRun {
		fmt.Printf("\n%s Would start synthesis:\n", style.Dim.Render("[dry-run]"))
		fmt.Printf("  Raid:    %s\n", raidID)
		fmt.Printf("  Review ID: %s\n", reviewID)
		fmt.Printf("  Target:    %s\n", targetRig)
		fmt.Printf("  Legs:      %d outputs collected\n", len(legOutputs))
		if f != nil && f.Synthesis != nil {
			fmt.Printf("  Synthesis: %s\n", f.Synthesis.Title)
		}
		return nil
	}

	// Create synthesis bead
	synthesisID, err := createSynthesisBead(raidID, meta, f, legOutputs, reviewID)
	if err != nil {
		return fmt.Errorf("creating synthesis bead: %w", err)
	}
	fmt.Printf("%s Created synthesis bead: %s\n", style.Bold.Render("✓"), synthesisID)

	// Charge to target warband
	fmt.Printf("  Charging to %s...\n", targetRig)
	if err := slingSynthesis(synthesisID, targetRig); err != nil {
		return fmt.Errorf("charging synthesis: %w", err)
	}

	fmt.Printf("%s Synthesis started\n", style.Bold.Render("✓"))
	fmt.Printf("  Monitor: hd raid status %s\n", raidID)

	return nil
}

// runSynthesisStatus implements hd synthesis status.
func runSynthesisStatus(cmd *cobra.Command, args []string) error {
	raidID := args[0]

	meta, err := getRaidMeta(raidID)
	if err != nil {
		return fmt.Errorf("getting raid metadata: %w", err)
	}

	// Load ritual if available
	var f *ritual.Ritual
	if meta.FormulaPath != "" {
		f, _ = ritual.ParseFile(meta.FormulaPath)
	} else if meta.Ritual != "" {
		if path, err := findFormula(meta.Ritual); err == nil {
			f, _ = ritual.ParseFile(path)
		}
	}

	// Collect leg outputs
	legOutputs, allComplete, err := collectLegOutputs(meta, f)
	if err != nil {
		return fmt.Errorf("collecting leg outputs: %w", err)
	}

	// Display status
	fmt.Printf("🚚 %s %s\n\n", style.Bold.Render(raidID+":"), meta.Title)
	fmt.Printf("  Status: %s\n", formatRaidStatus(meta.Status))

	if meta.Ritual != "" {
		fmt.Printf("  Ritual: %s\n", meta.Ritual)
	}

	fmt.Printf("\n  %s\n", style.Bold.Render("Legs:"))
	for _, leg := range legOutputs {
		status := "○"
		if leg.Status == "closed" {
			status = "✓"
		}
		fileStatus := ""
		if leg.HasFile {
			fileStatus = style.Dim.Render(" (output: ✓)")
		}
		fmt.Printf("    %s %s: %s [%s]%s\n", status, leg.LegID, leg.Title, leg.Status, fileStatus)
	}

	// Synthesis readiness
	fmt.Printf("\n  %s\n", style.Bold.Render("Synthesis:"))
	if allComplete {
		fmt.Printf("    %s Ready - all legs complete\n", style.Success.Render("✓"))
		fmt.Printf("    Run: hd synthesis start %s\n", raidID)
	} else {
		completedCount := 0
		for _, leg := range legOutputs {
			if leg.Status == "closed" {
				completedCount++
			}
		}
		fmt.Printf("    %s Waiting - %d/%d legs complete\n",
			style.Warning.Render("○"), completedCount, len(legOutputs))
	}

	if f != nil && f.Synthesis != nil {
		fmt.Printf("\n  %s\n", style.Bold.Render("Synthesis Config:"))
		fmt.Printf("    Title: %s\n", f.Synthesis.Title)
		if f.Output != nil && f.Output.Synthesis != "" {
			fmt.Printf("    Output: %s\n", f.Output.Synthesis)
		}
	}

	return nil
}

// runSynthesisClose implements hd synthesis close.
func runSynthesisClose(cmd *cobra.Command, args []string) error {
	raidID := args[0]

	townRelics, err := getTownRelicsDir()
	if err != nil {
		return err
	}

	// Close the raid
	closeArgs := []string{"close", raidID, "--reason=synthesis complete"}
	if sessionID := runtime.SessionIDFromEnv(); sessionID != "" {
		closeArgs = append(closeArgs, "--session="+sessionID)
	}
	closeCmd := exec.Command("rl", closeArgs...)
	closeCmd.Dir = townRelics
	closeCmd.Stderr = os.Stderr

	if err := closeCmd.Run(); err != nil {
		return fmt.Errorf("closing raid: %w", err)
	}

	fmt.Printf("%s Raid closed: %s\n", style.Bold.Render("✓"), raidID)

	// TODO: Trigger notification if configured
	// Parse description for "Notify: <address>" and send drums

	return nil
}

// getRaidMeta retrieves raid metadata from relics.
func getRaidMeta(raidID string) (*RaidMeta, error) {
	townRelics, err := getTownRelicsDir()
	if err != nil {
		return nil, err
	}

	showCmd := exec.Command("rl", "show", raidID, "--json")
	showCmd.Dir = townRelics
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		return nil, fmt.Errorf("raid '%s' not found", raidID)
	}

	var raids []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		Description string `json:"description"`
		Type        string `json:"issue_type"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raids); err != nil {
		return nil, fmt.Errorf("parsing raid data: %w", err)
	}

	if len(raids) == 0 || raids[0].Type != "raid" {
		return nil, fmt.Errorf("'%s' is not a raid", raidID)
	}

	raid := raids[0]

	// Parse ritual and review ID from description
	meta := &RaidMeta{
		ID:     raid.ID,
		Title:  raid.Title,
		Status: raid.Status,
	}

	// Look for structured fields in description
	for _, line := range strings.Split(raid.Description, "\n") {
		line = strings.TrimSpace(line)
		if colonIdx := strings.Index(line, ":"); colonIdx != -1 {
			key := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
			value := strings.TrimSpace(line[colonIdx+1:])
			switch key {
			case "ritual":
				meta.Ritual = value
			case "formula_path", "ritual-path":
				meta.FormulaPath = value
			case "review_id", "review-id":
				meta.ReviewID = value
			}
		}
	}

	// Get tracked leg issues
	tracked := getTrackedIssues(townRelics, raidID)
	for _, t := range tracked {
		meta.LegIssues = append(meta.LegIssues, t.ID)
	}

	return meta, nil
}

// collectLegOutputs gathers outputs from all raid legs.
func collectLegOutputs(meta *RaidMeta, f *ritual.Ritual) ([]LegOutput, bool, error) { //nolint:unparam // error return kept for future use
	var outputs []LegOutput
	allComplete := true

	// If we have tracked issues, use those as legs
	if len(meta.LegIssues) > 0 {
		for _, issueID := range meta.LegIssues {
			details := getIssueDetails(issueID)
			output := LegOutput{
				LegID: issueID,
				Title: "(unknown)",
			}
			if details != nil {
				output.Title = details.Title
				output.Status = details.Status
			}
			if output.Status != "closed" {
				allComplete = false
			}
			outputs = append(outputs, output)
		}
	}

	// If we have a ritual, also try to find output files
	if f != nil && f.Output != nil && meta.ReviewID != "" {
		for _, leg := range f.Legs {
			// Expand output path template
			outputPath := expandOutputPath(f.Output.Directory, f.Output.LegPattern,
				meta.ReviewID, leg.ID)

			// Check if file exists and read content
			if content, err := os.ReadFile(outputPath); err == nil {
				// Find or create leg output entry
				found := false
				for i := range outputs {
					if outputs[i].LegID == leg.ID {
						outputs[i].FilePath = outputPath
						outputs[i].Content = string(content)
						outputs[i].HasFile = true
						found = true
						break
					}
				}
				if !found {
					outputs = append(outputs, LegOutput{
						LegID:    leg.ID,
						Title:    leg.Title,
						Status:   "closed", // If file exists, assume complete
						FilePath: outputPath,
						Content:  string(content),
						HasFile:  true,
					})
				}
			}
		}
	}

	return outputs, allComplete, nil
}

// expandOutputPath expands template variables in output paths.
// Supports: {{review_id}}, {{leg.id}}
func expandOutputPath(directory, pattern, reviewID, legID string) string {
	// Expand directory
	dir := strings.ReplaceAll(directory, "{{review_id}}", reviewID)

	// Expand pattern
	file := strings.ReplaceAll(pattern, "{{leg.id}}", legID)

	return filepath.Join(dir, file)
}

// createSynthesisBead creates a bead for the synthesis step.
func createSynthesisBead(raidID string, meta *RaidMeta, f *ritual.Ritual,
	legOutputs []LegOutput, reviewID string) (string, error) {

	// Build synthesis title
	title := "Synthesis: " + meta.Title
	if f != nil && f.Synthesis != nil && f.Synthesis.Title != "" {
		title = f.Synthesis.Title + ": " + meta.Title
	}

	// Build synthesis description with leg outputs
	var desc strings.Builder
	desc.WriteString(fmt.Sprintf("raid: %s\n", raidID))
	desc.WriteString(fmt.Sprintf("review_id: %s\n", reviewID))
	desc.WriteString("\n")

	// Add synthesis instructions from ritual
	if f != nil && f.Synthesis != nil && f.Synthesis.Description != "" {
		desc.WriteString("## Instructions\n\n")
		desc.WriteString(f.Synthesis.Description)
		desc.WriteString("\n\n")
	}

	// Add collected leg outputs
	desc.WriteString("## Leg Outputs\n\n")
	for _, leg := range legOutputs {
		desc.WriteString(fmt.Sprintf("### %s: %s\n\n", leg.LegID, leg.Title))
		if leg.Content != "" {
			desc.WriteString(leg.Content)
			desc.WriteString("\n\n")
		} else if leg.FilePath != "" {
			desc.WriteString(fmt.Sprintf("Output file: %s\n\n", leg.FilePath))
		} else {
			desc.WriteString("(no output available)\n\n")
		}
	}

	// Add output path if configured
	if f != nil && f.Output != nil && f.Output.Synthesis != "" {
		outputPath := strings.ReplaceAll(f.Output.Directory, "{{review_id}}", reviewID)
		outputPath = filepath.Join(outputPath, f.Output.Synthesis)
		desc.WriteString(fmt.Sprintf("\n## Output\n\nWrite synthesis to: %s\n", outputPath))
	}

	// Create the bead
	createArgs := []string{
		"create",
		"--type=task",
		"--title=" + title,
		"--description=" + desc.String(),
		"--json",
	}

	townRelics, err := getTownRelicsDir()
	if err != nil {
		return "", err
	}

	createCmd := exec.Command("rl", createArgs...)
	createCmd.Dir = townRelics
	var stdout bytes.Buffer
	createCmd.Stdout = &stdout
	createCmd.Stderr = os.Stderr

	if err := createCmd.Run(); err != nil {
		return "", fmt.Errorf("creating synthesis bead: %w", err)
	}

	// Parse created bead ID
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		// Try to extract ID from non-JSON output
		out := strings.TrimSpace(stdout.String())
		if strings.HasPrefix(out, "hq-") || strings.HasPrefix(out, "hd-") {
			return out, nil
		}
		return "", fmt.Errorf("parsing created bead: %w", err)
	}

	// Add tracking relation: raid tracks synthesis
	depArgs := []string{"dep", "add", raidID, result.ID, "--type=tracks"}
	depCmd := exec.Command("rl", depArgs...)
	depCmd.Dir = townRelics
	_ = depCmd.Run() // Non-fatal if this fails

	return result.ID, nil
}

// slingSynthesis charges the synthesis bead to a warband.
func slingSynthesis(beadID, targetRig string) error {
	slingArgs := []string{"charge", beadID, targetRig}
	slingCmd := exec.Command("hd", slingArgs...)
	slingCmd.Stdout = os.Stdout
	slingCmd.Stderr = os.Stderr

	return slingCmd.Run()
}

// findFormula searches for a ritual file by name.
func findFormula(name string) (string, error) {
	// Search paths
	searchPaths := []string{
		".relics/rituals",
	}

	// Add home directory rituals
	if home, err := os.UserHomeDir(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(home, ".relics", "rituals"))
	}

	// Add GT_ROOT rituals if set
	if gtRoot := os.Getenv("GT_ROOT"); gtRoot != "" {
		searchPaths = append(searchPaths, filepath.Join(gtRoot, ".relics", "rituals"))
	}

	// Try each search path
	for _, searchPath := range searchPaths {
		// Try with .ritual.toml extension
		path := filepath.Join(searchPath, name+".ritual.toml")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}

		// Try with .ritual.json extension
		path = filepath.Join(searchPath, name+".ritual.json")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("ritual '%s' not found", name)
}

// CheckSynthesisReady checks if a raid is ready for synthesis.
// Returns true if all tracked legs are complete.
func CheckSynthesisReady(raidID string) (bool, error) {
	meta, err := getRaidMeta(raidID)
	if err != nil {
		return false, err
	}

	_, allComplete, err := collectLegOutputs(meta, nil)
	return allComplete, err
}

// TriggerSynthesisIfReady checks raid status and starts synthesis if ready.
// This can be called by the witness when a leg completes.
func TriggerSynthesisIfReady(raidID, targetRig string) error {
	ready, err := CheckSynthesisReady(raidID)
	if err != nil {
		return err
	}

	if !ready {
		return nil // Not ready yet
	}

	// Synthesis is ready - start it
	fmt.Printf("%s All legs complete, starting synthesis...\n", style.Bold.Render("🔬"))

	meta, err := getRaidMeta(raidID)
	if err != nil {
		return err
	}

	// Load ritual if available
	var f *ritual.Ritual
	if meta.FormulaPath != "" {
		f, _ = ritual.ParseFile(meta.FormulaPath)
	} else if meta.Ritual != "" {
		if path, err := findFormula(meta.Ritual); err == nil {
			f, _ = ritual.ParseFile(path)
		}
	}

	legOutputs, _, _ := collectLegOutputs(meta, f)
	reviewID := meta.ReviewID
	if reviewID == "" {
		reviewID = strings.TrimPrefix(raidID, "hq-cv-")
	}

	synthesisID, err := createSynthesisBead(raidID, meta, f, legOutputs, reviewID)
	if err != nil {
		return fmt.Errorf("creating synthesis bead: %w", err)
	}

	if err := slingSynthesis(synthesisID, targetRig); err != nil {
		return fmt.Errorf("charging synthesis: %w", err)
	}

	return nil
}
