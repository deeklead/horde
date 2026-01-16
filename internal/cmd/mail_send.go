package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/relics"
	"github.com/OWNER/horde/internal/events"
	"github.com/OWNER/horde/internal/drums"
	"github.com/OWNER/horde/internal/style"
	"github.com/OWNER/horde/internal/workspace"
)

func runMailSend(cmd *cobra.Command, args []string) error {
	var to string

	if mailSendSelf {
		// Auto-detect identity from cwd
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}
		townRoot, err := workspace.FindFromCwd()
		if err != nil || townRoot == "" {
			return fmt.Errorf("not in a Horde workspace")
		}
		roleInfo, err := GetRoleWithContext(cwd, townRoot)
		if err != nil {
			return fmt.Errorf("detecting role: %w", err)
		}
		ctx := RoleContext{
			Role:     roleInfo.Role,
			Warband:      roleInfo.Warband,
			Raider:  roleInfo.Raider,
			TownRoot: townRoot,
			WorkDir:  cwd,
		}
		to = buildAgentIdentity(ctx)
		if to == "" {
			return fmt.Errorf("cannot determine identity (role: %s)", ctx.Role)
		}
	} else if len(args) > 0 {
		to = args[0]
	} else {
		return fmt.Errorf("address required (or use --self)")
	}

	// All drums uses encampment relics (two-level architecture)
	workDir, err := findMailWorkDir()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Determine sender
	from := detectSender()

	// Create message
	msg := &drums.Message{
		From:    from,
		To:      to,
		Subject: mailSubject,
		Body:    mailBody,
	}

	// Set priority (--urgent overrides --priority)
	if mailUrgent {
		msg.Priority = drums.PriorityUrgent
	} else {
		msg.Priority = drums.PriorityFromInt(mailPriority)
	}
	if mailNotify && msg.Priority == drums.PriorityNormal {
		msg.Priority = drums.PriorityHigh
	}

	// Set message type
	msg.Type = drums.ParseMessageType(mailType)

	// Set pinned flag
	msg.Pinned = mailPinned

	// Set wisp flag (ephemeral message) - default true, --permanent overrides
	msg.Wisp = mailWisp && !mailPermanent

	// Set CC recipients
	msg.CC = mailCC

	// Handle reply-to: auto-set type to reply and look up thread
	if mailReplyTo != "" {
		msg.ReplyTo = mailReplyTo
		if msg.Type == drums.TypeNotification {
			msg.Type = drums.TypeReply
		}

		// Look up original message to get thread ID
		router := drums.NewRouter(workDir)
		wardrums, err := router.GetMailbox(from)
		if err == nil {
			if original, err := wardrums.Get(mailReplyTo); err == nil {
				msg.ThreadID = original.ThreadID
			}
		}
	}

	// Generate thread ID for new threads
	if msg.ThreadID == "" {
		msg.ThreadID = generateThreadID()
	}

	// Use address resolver for new address types
	townRoot, _ := workspace.FindFromCwd()
	b := relics.New(townRoot)
	resolver := drums.NewResolver(b, townRoot)

	recipients, err := resolver.Resolve(to)
	if err != nil {
		// Fall back to legacy routing if resolver fails
		router := drums.NewRouter(workDir)
		if err := router.Send(msg); err != nil {
			return fmt.Errorf("sending message: %w", err)
		}
		_ = events.LogFeed(events.TypeMail, from, events.MailPayload(to, mailSubject))
		fmt.Printf("%s Message sent to %s\n", style.Bold.Render("✓"), to)
		fmt.Printf("  Subject: %s\n", mailSubject)
		return nil
	}

	// Route based on recipient type
	router := drums.NewRouter(workDir)
	var recipientAddrs []string

	for _, rec := range recipients {
		switch rec.Type {
		case drums.RecipientQueue:
			// Queue messages: single message, workers claim
			msg.To = rec.Address
			if err := router.Send(msg); err != nil {
				return fmt.Errorf("sending to queue: %w", err)
			}
			recipientAddrs = append(recipientAddrs, rec.Address)

		case drums.RecipientChannel:
			// Channel messages: single message, broadcast
			msg.To = rec.Address
			if err := router.Send(msg); err != nil {
				return fmt.Errorf("sending to channel: %w", err)
			}
			recipientAddrs = append(recipientAddrs, rec.Address)

		default:
			// Direct/agent messages: fan out to each recipient
			msgCopy := *msg
			msgCopy.To = rec.Address
			if err := router.Send(&msgCopy); err != nil {
				return fmt.Errorf("sending to %s: %w", rec.Address, err)
			}
			recipientAddrs = append(recipientAddrs, rec.Address)
		}
	}

	// Log drums event to activity feed
	_ = events.LogFeed(events.TypeMail, from, events.MailPayload(to, mailSubject))

	fmt.Printf("%s Message sent to %s\n", style.Bold.Render("✓"), to)
	fmt.Printf("  Subject: %s\n", mailSubject)

	// Show resolved recipients if fan-out occurred
	if len(recipientAddrs) > 1 || (len(recipientAddrs) == 1 && recipientAddrs[0] != to) {
		fmt.Printf("  Recipients: %s\n", strings.Join(recipientAddrs, ", "))
	}

	if len(msg.CC) > 0 {
		fmt.Printf("  CC: %s\n", strings.Join(msg.CC, ", "))
	}
	if msg.Type != drums.TypeNotification {
		fmt.Printf("  Type: %s\n", msg.Type)
	}

	return nil
}

// generateThreadID creates a random thread ID for new message threads.
func generateThreadID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b) // crypto/rand.Read only fails on broken system
	return "thread-" + hex.EncodeToString(b)
}
