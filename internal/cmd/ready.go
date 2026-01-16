package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/config"
	"github.com/deeklead/horde/internal/constants"
	"github.com/deeklead/horde/internal/git"
	"github.com/deeklead/horde/internal/warband"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

var readyJSON bool
var readyRig string

var readyCmd = &cobra.Command{
	Use:     "ready",
	GroupID: GroupWork,
	Short:   "Show work ready across encampment",
	Long: `Display all ready work items across the encampment and all warbands.

Aggregates ready issues from:
- Encampment relics (hq-* items: raids, cross-warband coordination)
- Each warband's relics (project-level issues, MRs)

Ready items have no blockers and can be worked immediately.
Results are sorted by priority (highest first) then by source.

Examples:
  hd ready              # Show all ready work
  hd ready --json       # Output as JSON
  hd ready --warband=horde  # Show only one warband`,
	RunE: runReady,
}

func init() {
	readyCmd.Flags().BoolVar(&readyJSON, "json", false, "Output as JSON")
	readyCmd.Flags().StringVar(&readyRig, "warband", "", "Filter to a specific warband")
	rootCmd.AddCommand(readyCmd)
}

// ReadySource represents ready items from a single source (encampment or warband).
type ReadySource struct {
	Name   string         `json:"name"`   // "encampment" or warband name
	Issues []*relics.Issue `json:"issues"` // Ready issues from this source
	Error  string         `json:"error,omitempty"`
}

// ReadyResult is the aggregated result of hd ready.
type ReadyResult struct {
	Sources  []ReadySource `json:"sources"`
	Summary  ReadySummary  `json:"summary"`
	TownRoot string        `json:"town_root,omitempty"`
}

// ReadySummary provides counts for the ready report.
type ReadySummary struct {
	Total    int            `json:"total"`
	BySource map[string]int `json:"by_source"`
	P0Count  int            `json:"p0_count"`
	P1Count  int            `json:"p1_count"`
	P2Count  int            `json:"p2_count"`
	P3Count  int            `json:"p3_count"`
	P4Count  int            `json:"p4_count"`
}

func runReady(cmd *cobra.Command, args []string) error {
	// Find encampment root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Load warbands config
	rigsConfigPath := constants.WarchiefRigsPath(townRoot)
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Warbands: make(map[string]config.RigEntry)}
	}

	// Create warband manager and discover warbands
	g := git.NewGit(townRoot)
	mgr := warband.NewManager(townRoot, rigsConfig, g)
	warbands, err := mgr.DiscoverRigs()
	if err != nil {
		return fmt.Errorf("discovering warbands: %w", err)
	}

	// Filter warbands if --warband flag provided
	if readyRig != "" {
		var filtered []*warband.Warband
		for _, r := range warbands {
			if r.Name == readyRig {
				filtered = append(filtered, r)
				break
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("warband not found: %s", readyRig)
		}
		warbands = filtered
	}

	// Collect results from all sources in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	sources := make([]ReadySource, 0, len(warbands)+1)

	// Fetch encampment relics (only if not filtering to a specific warband)
	if readyRig == "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			townRelicsPath := relics.GetTownRelicsPath(townRoot)
			townRelics := relics.New(townRelicsPath)
			issues, err := townRelics.Ready()

			mu.Lock()
			defer mu.Unlock()
			src := ReadySource{Name: "encampment"}
			if err != nil {
				src.Error = err.Error()
			} else {
				src.Issues = issues
			}
			sources = append(sources, src)
		}()
	}

	// Fetch from each warband in parallel
	for _, r := range warbands {
		wg.Add(1)
		go func(r *warband.Warband) {
			defer wg.Done()
			// Use warchief/warband path where warband-level relics are stored
			rigRelicsPath := constants.RigWarchiefPath(r.Path)
			rigRelics := relics.New(rigRelicsPath)
			issues, err := rigRelics.Ready()

			mu.Lock()
			defer mu.Unlock()
			src := ReadySource{Name: r.Name}
			if err != nil {
				src.Error = err.Error()
			} else {
				src.Issues = issues
			}
			sources = append(sources, src)
		}(r)
	}

	wg.Wait()

	// Sort sources: encampment first, then warbands alphabetically
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Name == "encampment" {
			return true
		}
		if sources[j].Name == "encampment" {
			return false
		}
		return sources[i].Name < sources[j].Name
	})

	// Sort issues within each source by priority (lower number = higher priority)
	for i := range sources {
		sort.Slice(sources[i].Issues, func(a, b int) bool {
			return sources[i].Issues[a].Priority < sources[i].Issues[b].Priority
		})
	}

	// Build summary
	summary := ReadySummary{
		BySource: make(map[string]int),
	}
	for _, src := range sources {
		count := len(src.Issues)
		summary.Total += count
		summary.BySource[src.Name] = count
		for _, issue := range src.Issues {
			switch issue.Priority {
			case 0:
				summary.P0Count++
			case 1:
				summary.P1Count++
			case 2:
				summary.P2Count++
			case 3:
				summary.P3Count++
			case 4:
				summary.P4Count++
			}
		}
	}

	result := ReadyResult{
		Sources:  sources,
		Summary:  summary,
		TownRoot: townRoot,
	}

	// Output
	if readyJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	return printReadyHuman(result)
}

func printReadyHuman(result ReadyResult) error {
	if result.Summary.Total == 0 {
		fmt.Println("No ready work across encampment.")
		return nil
	}

	fmt.Printf("%s Ready work across encampment:\n\n", style.Bold.Render("📋"))

	for _, src := range result.Sources {
		if src.Error != "" {
			fmt.Printf("%s %s\n", style.Dim.Render(src.Name+"/"), style.Warning.Render("(error: "+src.Error+")"))
			continue
		}

		count := len(src.Issues)
		if count == 0 {
			fmt.Printf("%s %s\n", style.Dim.Render(src.Name+"/"), style.Dim.Render("(none)"))
			continue
		}

		fmt.Printf("%s (%d items)\n", style.Bold.Render(src.Name+"/"), count)
		for _, issue := range src.Issues {
			priorityStr := fmt.Sprintf("P%d", issue.Priority)
			var priorityStyled string
			switch issue.Priority {
			case 0:
				priorityStyled = style.Error.Render(priorityStr) // P0 is critical
			case 1:
				priorityStyled = style.Error.Render(priorityStr)
			case 2:
				priorityStyled = style.Warning.Render(priorityStr)
			default:
				priorityStyled = style.Dim.Render(priorityStr)
			}

			// Truncate title if too long
			title := issue.Title
			if len(title) > 60 {
				title = title[:57] + "..."
			}

			fmt.Printf("  [%s] %s %s\n", priorityStyled, style.Dim.Render(issue.ID), title)
		}
		fmt.Println()
	}

	// Summary line
	parts := []string{}
	if result.Summary.P0Count > 0 {
		parts = append(parts, fmt.Sprintf("%d P0", result.Summary.P0Count))
	}
	if result.Summary.P1Count > 0 {
		parts = append(parts, fmt.Sprintf("%d P1", result.Summary.P1Count))
	}
	if result.Summary.P2Count > 0 {
		parts = append(parts, fmt.Sprintf("%d P2", result.Summary.P2Count))
	}
	if result.Summary.P3Count > 0 {
		parts = append(parts, fmt.Sprintf("%d P3", result.Summary.P3Count))
	}
	if result.Summary.P4Count > 0 {
		parts = append(parts, fmt.Sprintf("%d P4", result.Summary.P4Count))
	}

	if len(parts) > 0 {
		fmt.Printf("Total: %d items ready (%s)\n", result.Summary.Total, strings.Join(parts, ", "))
	} else {
		fmt.Printf("Total: %d items ready\n", result.Summary.Total)
	}

	return nil
}
