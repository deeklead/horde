package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/drums"
	"github.com/deeklead/horde/internal/style"
)

// runMailSearch searches for messages matching a pattern.
func runMailSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	// Determine which inbox to search
	address := detectSender()

	// Get workspace for drums operations
	workDir, err := findMailWorkDir()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Get wardrums
	router := drums.NewRouter(workDir)
	wardrums, err := router.GetMailbox(address)
	if err != nil {
		return fmt.Errorf("getting wardrums: %w", err)
	}

	// Build search options
	opts := drums.SearchOptions{
		Query:       query,
		FromFilter:  mailSearchFrom,
		SubjectOnly: mailSearchSubject,
		BodyOnly:    mailSearchBody,
	}

	// Execute search
	messages, err := wardrums.Search(opts)
	if err != nil {
		return fmt.Errorf("searching messages: %w", err)
	}

	// JSON output
	if mailSearchJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(messages)
	}

	// Human-readable output
	fmt.Printf("%s Search results for %s: %d message(s)\n\n",
		style.Bold.Render("🔍"), address, len(messages))

	if len(messages) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(no matches)"))
		return nil
	}

	for _, msg := range messages {
		readMarker := "●"
		if msg.Read {
			readMarker = "○"
		}
		typeMarker := ""
		if msg.Type != "" && msg.Type != drums.TypeNotification {
			typeMarker = fmt.Sprintf(" [%s]", msg.Type)
		}
		priorityMarker := ""
		if msg.Priority == drums.PriorityHigh || msg.Priority == drums.PriorityUrgent {
			priorityMarker = " " + style.Bold.Render("!")
		}
		wispMarker := ""
		if msg.Wisp {
			wispMarker = " " + style.Dim.Render("(wisp)")
		}

		fmt.Printf("  %s %s%s%s%s\n", readMarker, msg.Subject, typeMarker, priorityMarker, wispMarker)
		fmt.Printf("    %s from %s\n",
			style.Dim.Render(msg.ID),
			msg.From)
		fmt.Printf("    %s\n",
			style.Dim.Render(msg.Timestamp.Format("2006-01-02 15:04")))
	}

	return nil
}
