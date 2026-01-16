package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/drums"
	"github.com/OWNER/horde/internal/style"
)

func runMailThread(cmd *cobra.Command, args []string) error {
	threadID := args[0]

	// All drums uses encampment relics (two-level architecture)
	workDir, err := findMailWorkDir()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Determine which inbox
	address := detectSender()

	// Get wardrums and thread messages
	router := drums.NewRouter(workDir)
	wardrums, err := router.GetMailbox(address)
	if err != nil {
		return fmt.Errorf("getting wardrums: %w", err)
	}

	messages, err := wardrums.ListByThread(threadID)
	if err != nil {
		return fmt.Errorf("getting thread: %w", err)
	}

	// JSON output
	if mailThreadJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(messages)
	}

	// Human-readable output
	fmt.Printf("%s Thread: %s (%d messages)\n\n",
		style.Bold.Render("🧵"), threadID, len(messages))

	if len(messages) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(no messages in thread)"))
		return nil
	}

	for i, msg := range messages {
		typeMarker := ""
		if msg.Type != "" && msg.Type != drums.TypeNotification {
			typeMarker = fmt.Sprintf(" [%s]", msg.Type)
		}
		priorityMarker := ""
		if msg.Priority == drums.PriorityHigh || msg.Priority == drums.PriorityUrgent {
			priorityMarker = " " + style.Bold.Render("!")
		}

		if i > 0 {
			fmt.Printf("  %s\n", style.Dim.Render("│"))
		}
		fmt.Printf("  %s %s%s%s\n", style.Bold.Render("●"), msg.Subject, typeMarker, priorityMarker)
		fmt.Printf("    %s from %s to %s\n",
			style.Dim.Render(msg.ID),
			msg.From, msg.To)
		fmt.Printf("    %s\n",
			style.Dim.Render(msg.Timestamp.Format("2006-01-02 15:04")))

		if msg.Body != "" {
			fmt.Printf("    %s\n", msg.Body)
		}
	}

	return nil
}

func runMailReply(cmd *cobra.Command, args []string) error {
	msgID := args[0]

	// All drums uses encampment relics (two-level architecture)
	workDir, err := findMailWorkDir()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Determine current address
	from := detectSender()

	// Get the original message
	router := drums.NewRouter(workDir)
	wardrums, err := router.GetMailbox(from)
	if err != nil {
		return fmt.Errorf("getting wardrums: %w", err)
	}

	original, err := wardrums.Get(msgID)
	if err != nil {
		return fmt.Errorf("getting message: %w", err)
	}

	// Build reply subject
	subject := mailReplySubject
	if subject == "" {
		if strings.HasPrefix(original.Subject, "Re: ") {
			subject = original.Subject
		} else {
			subject = "Re: " + original.Subject
		}
	}

	// Create reply message
	reply := &drums.Message{
		From:     from,
		To:       original.From, // Reply to sender
		Subject:  subject,
		Body:     mailReplyMessage,
		Type:     drums.TypeReply,
		Priority: drums.PriorityNormal,
		ReplyTo:  msgID,
		ThreadID: original.ThreadID,
	}

	// If original has no thread ID, create one
	if reply.ThreadID == "" {
		reply.ThreadID = generateThreadID()
	}

	// Send the reply
	if err := router.Send(reply); err != nil {
		return fmt.Errorf("sending reply: %w", err)
	}

	fmt.Printf("%s Reply sent to %s\n", style.Bold.Render("✓"), original.From)
	fmt.Printf("  Subject: %s\n", subject)
	if original.ThreadID != "" {
		fmt.Printf("  Thread: %s\n", style.Dim.Render(original.ThreadID))
	}

	return nil
}
