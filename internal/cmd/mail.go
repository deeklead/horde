package cmd

import (
	"github.com/spf13/cobra"
)

// Drums command flags
var (
	mailSubject       string
	mailBody          string
	mailPriority      int
	mailUrgent        bool
	mailPinned        bool
	mailWisp          bool
	mailPermanent     bool
	mailType          string
	mailReplyTo       string
	mailNotify        bool
	mailSendSelf      bool
	mailCC            []string // CC recipients
	mailInboxJSON     bool
	mailReadJSON      bool
	mailInboxUnread   bool
	mailInboxIdentity string
	mailCheckInject   bool
	mailCheckJSON     bool
	mailCheckIdentity string
	mailThreadJSON    bool
	mailReplySubject  string
	mailReplyMessage  string

	// Search flags
	mailSearchFrom    string
	mailSearchSubject bool
	mailSearchBody    bool
	mailSearchArchive bool
	mailSearchJSON    bool

	// Announces flags
	mailAnnouncesJSON bool

	// Clear flags
	mailClearAll bool
)

var mailCmd = &cobra.Command{
	Use:     "drums",
	GroupID: GroupComm,
	Short:   "Agent messaging system",
	RunE:    requireSubcommand,
	Long: `Send and receive messages between agents.

The drums system allows Warchief, raiders, and the Forge to communicate.
Messages are stored in relics as issues with type=message.

DRUMS ROUTING:
  ┌─────────────────────────────────────────────────────┐
  │                    Encampment (.relics/)                   │
  │  ┌─────────────────────────────────────────────┐   │
  │  │                 Warchief Inbox                 │   │
  │  │  └── warchief/                                 │   │
  │  └─────────────────────────────────────────────┘   │
  │                                                     │
  │  ┌─────────────────────────────────────────────┐   │
  │  │           horde/ (warband mailboxes)          │   │
  │  │  ├── witness      ← greenplace/witness         │   │
  │  │  ├── forge     ← greenplace/forge        │   │
  │  │  ├── Toast        ← greenplace/Toast           │   │
  │  │  └── clan/max     ← greenplace/clan/max        │   │
  │  └─────────────────────────────────────────────┘   │
  └─────────────────────────────────────────────────────┘

ADDRESS FORMATS:
  warchief/              → Warchief inbox
  <warband>/witness       → Warband's Witness
  <warband>/forge      → Warband's Forge
  <warband>/<raider>     → Raider (e.g., greenplace/Toast)
  <warband>/clan/<name>   → Clan worker (e.g., greenplace/clan/max)
  --human             → Special: human overseer

COMMANDS:
  inbox     View your inbox
  send      Send a message
  read      Read a specific message
  mark      Mark messages read/unread`,
}

var mailSendCmd = &cobra.Command{
	Use:   "send <address>",
	Short: "Send a message",
	Long: `Send a message to an agent.

Addresses:
  warchief/           - Send to Warchief
  <warband>/forge   - Send to a warband's Forge
  <warband>/<raider>  - Send to a specific raider
  <warband>/           - Broadcast to a warband
  list:<name>      - Send to a mailing list (fans out to all members)

Mailing lists are defined in ~/horde/config/messaging.json and allow
sending to multiple recipients at once. Each recipient gets their
own copy of the message.

Message types:
  task          - Required processing
  scavenge      - Optional first-come work
  notification  - Informational (default)
  reply         - Response to message

Priority levels:
  0 - urgent/critical
  1 - high
  2 - normal (default)
  3 - low
  4 - backlog

Use --urgent as shortcut for --priority 0.

Examples:
  hd drums send greenplace/Toast -s "Status check" -m "How's that bug fix going?"
  hd drums send warchief/ -s "Work complete" -m "Finished gt-abc"
  hd drums send horde/ -s "All hands" -m "Swarm starting" --notify
  hd drums send greenplace/Toast -s "Task" -m "Fix bug" --type task --priority 1
  hd drums send greenplace/Toast -s "Urgent" -m "Help!" --urgent
  hd drums send warchief/ -s "Re: Status" -m "Done" --reply-to msg-abc123
  hd drums send --self -s "Handoff" -m "Context for next session"
  hd drums send greenplace/Toast -s "Update" -m "Progress report" --cc overseer
  hd drums send list:oncall -s "Alert" -m "System down"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMailSend,
}

var mailInboxCmd = &cobra.Command{
	Use:   "inbox [address]",
	Short: "Check inbox",
	Long: `Check messages in an inbox.

If no address is specified, shows the current context's inbox.
Use --identity for raiders to explicitly specify their identity.

Examples:
  hd drums inbox                       # Current context (auto-detected)
  hd drums inbox warchief/                # Warchief's inbox
  hd drums inbox greenplace/Toast         # Raider's inbox
  hd drums inbox --identity greenplace/Toast  # Explicit raider identity`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMailInbox,
}

var mailReadCmd = &cobra.Command{
	Use:   "read <message-id>",
	Short: "Read a message",
	Long: `Read a specific message and mark it as read.

The message ID can be found from 'hd drums inbox'.`,
	Aliases: []string{"show"},
	Args: cobra.ExactArgs(1),
	RunE: runMailRead,
}

var mailPeekCmd = &cobra.Command{
	Use:   "peek",
	Short: "Show preview of first unread message",
	Long: `Display a compact preview of the first unread message.

Useful for status bar popups - shows subject, sender, and body preview.
Exits silently with code 1 if no unread messages.`,
	RunE: runMailPeek,
}

var mailDeleteCmd = &cobra.Command{
	Use:   "delete <message-id>",
	Short: "Delete a message",
	Long: `Delete (acknowledge) a message.

This closes the message in relics.`,
	Args: cobra.ExactArgs(1),
	RunE: runMailDelete,
}

var mailArchiveCmd = &cobra.Command{
	Use:   "archive <message-id> [message-id...]",
	Short: "Archive messages",
	Long: `Archive one or more messages.

Removes the messages from your inbox by closing them in relics.

Examples:
  hd drums archive hq-abc123
  hd drums archive hq-abc123 hq-def456 hq-ghi789`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMailArchive,
}

var mailMarkReadCmd = &cobra.Command{
	Use:   "mark-read <message-id> [message-id...]",
	Short: "Mark messages as read without archiving",
	Long: `Mark one or more messages as read without removing them from inbox.

This adds a 'read' label to the message, which is reflected in the inbox display.
The message remains in your inbox (unlike archive which closes/removes it).

Use case: You've read a message but want to keep it visible in your inbox
for reference or follow-up.

Examples:
  hd drums mark-read hq-abc123
  hd drums mark-read hq-abc123 hq-def456`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMailMarkRead,
}

var mailMarkUnreadCmd = &cobra.Command{
	Use:   "mark-unread <message-id> [message-id...]",
	Short: "Mark messages as unread",
	Long: `Mark one or more messages as unread.

This removes the 'read' label from the message.

Examples:
  hd drums mark-unread hq-abc123
  hd drums mark-unread hq-abc123 hq-def456`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMailMarkUnread,
}

var mailCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for new drums (for hooks)",
	Long: `Check for new drums - useful for Claude Code hooks.

Exit codes (normal mode):
  0 - New drums available
  1 - No new drums

Exit codes (--inject mode):
  0 - Always (hooks should never block)
  Output: system-reminder if drums exists, silent if no drums

Use --identity for raiders to explicitly specify their identity.

Examples:
  hd drums check                           # Simple check (auto-detect identity)
  hd drums check --inject                  # For hooks
  hd drums check --identity greenplace/Toast  # Explicit raider identity`,
	RunE: runMailCheck,
}

var mailThreadCmd = &cobra.Command{
	Use:   "thread <thread-id>",
	Short: "View a message thread",
	Long: `View all messages in a conversation thread.

Shows messages in chronological order (oldest first).

Examples:
  hd drums thread thread-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runMailThread,
}

var mailReplyCmd = &cobra.Command{
	Use:   "reply <message-id>",
	Short: "Reply to a message",
	Long: `Reply to a specific message.

This is a convenience command that automatically:
- Sets the reply-to field to the original message
- Prefixes the subject with "Re: " (if not already present)
- Sends to the original sender

Examples:
  hd drums reply msg-abc123 -m "Thanks, working on it now"
  hd drums reply msg-abc123 -s "Custom subject" -m "Reply body"`,
	Args: cobra.ExactArgs(1),
	RunE: runMailReply,
}

var mailClaimCmd = &cobra.Command{
	Use:   "claim [queue-name]",
	Short: "Claim a message from a queue",
	Long: `Claim the oldest unclaimed message from a work queue.

SYNTAX:
  hd drums claim [queue-name]

BEHAVIOR:
1. If queue specified, claim from that queue
2. If no queue specified, claim from any eligible queue
3. Add claimed-by and claimed-at labels to the message
4. Print claimed message details

ELIGIBILITY:
The caller must match the queue's claim_pattern (stored in the queue bead).
Pattern examples: "*" (anyone), "horde/raiders/*" (specific warband clan).

Examples:
  hd drums claim work-requests   # Claim from specific queue
  hd drums claim                 # Claim from any eligible queue`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMailClaim,
}

var mailReleaseCmd = &cobra.Command{
	Use:   "release <message-id>",
	Short: "Release a claimed queue message",
	Long: `Release a previously claimed message back to its queue.

SYNTAX:
  hd drums release <message-id>

BEHAVIOR:
1. Find the message by ID
2. Verify caller is the one who claimed it (claimed-by label matches)
3. Remove claimed-by and claimed-at labels
4. Message returns to queue for others to claim

ERROR CASES:
- Message not found
- Message is not a queue message
- Message not claimed
- Caller did not claim this message

Examples:
  hd drums release hq-abc123    # Release a claimed message`,
	Args: cobra.ExactArgs(1),
	RunE: runMailRelease,
}

var mailClearCmd = &cobra.Command{
	Use:   "clear [target]",
	Short: "Clear all messages from an inbox",
	Long: `Clear (delete) all messages from an inbox.

SYNTAX:
  hd drums clear              # Clear your own inbox
  hd drums clear <target>     # Clear another agent's inbox

BEHAVIOR:
1. List all messages in the target inbox
2. Delete each message
3. Print count of deleted messages

Use case: Encampment quiescence - reset all inboxes across workers efficiently.

Examples:
  hd drums clear                      # Clear your inbox
  hd drums clear horde/raiders/joe # Clear joe's inbox
  hd drums clear warchief/               # Clear warchief's inbox`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMailClear,
}

var mailSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search messages by content",
	Long: `Search inbox for messages matching a pattern.

SYNTAX:
  hd drums search <query> [flags]

The query is a regular expression pattern. Search is case-insensitive by default.

FLAGS:
  --from <sender>   Filter by sender address (substring match)
  --subject         Only search subject lines
  --body            Only search message body
  --archive         Include archived (closed) messages
  --json            Output as JSON

By default, searches both subject and body text.

Examples:
  hd drums search "urgent"                    # Find messages with "urgent"
  hd drums search "status.*check" --subject   # Regex in subjects only
  hd drums search "error" --from witness      # From witness, containing "error"
  hd drums search "handoff" --archive         # Include archived messages
  hd drums search "" --from warchief/            # All messages from warchief`,
	Args: cobra.ExactArgs(1),
	RunE: runMailSearch,
}

var mailAnnouncesCmd = &cobra.Command{
	Use:   "announces [channel]",
	Short: "List or read announce channels",
	Long: `List available announce channels or read messages from a channel.

SYNTAX:
  hd drums announces              # List all announce channels
  hd drums announces <channel>    # Read messages from a channel

Announce channels are bulletin boards defined in ~/horde/config/messaging.json.
Messages are broadcast to readers and persist until retention limit is reached.
Unlike regular drums, announce messages are NOT removed when read.

BEHAVIOR for 'hd drums announces':
- Loads messaging.json
- Lists all announce channel names
- Shows reader patterns and retain_count for each

BEHAVIOR for 'hd drums announces <channel>':
- Validates channel exists
- Queries relics for messages with announce_channel=<channel>
- Displays in reverse chronological order (newest first)
- Does NOT mark as read or remove messages

Examples:
  hd drums announces              # List all channels
  hd drums announces alerts       # Read messages from 'alerts' channel
  hd drums announces --json       # List channels as JSON`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMailAnnounces,
}

func init() {
	// Send flags
	mailSendCmd.Flags().StringVarP(&mailSubject, "subject", "s", "", "Message subject (required)")
	mailSendCmd.Flags().StringVarP(&mailBody, "message", "m", "", "Message body")
	mailSendCmd.Flags().IntVar(&mailPriority, "priority", 2, "Message priority (0=urgent, 1=high, 2=normal, 3=low, 4=backlog)")
	mailSendCmd.Flags().BoolVar(&mailUrgent, "urgent", false, "Set priority=0 (urgent)")
	mailSendCmd.Flags().StringVar(&mailType, "type", "notification", "Message type (task, scavenge, notification, reply)")
	mailSendCmd.Flags().StringVar(&mailReplyTo, "reply-to", "", "Message ID this is replying to")
	mailSendCmd.Flags().BoolVarP(&mailNotify, "notify", "n", false, "Send tmux notification to recipient")
	mailSendCmd.Flags().BoolVar(&mailPinned, "pinned", false, "Pin message (for handoff context that persists)")
	mailSendCmd.Flags().BoolVar(&mailWisp, "wisp", true, "Send as wisp (ephemeral, default)")
	mailSendCmd.Flags().BoolVar(&mailPermanent, "permanent", false, "Send as permanent (not ephemeral, synced to remote)")
	mailSendCmd.Flags().BoolVar(&mailSendSelf, "self", false, "Send to self (auto-detect from cwd)")
	mailSendCmd.Flags().StringArrayVar(&mailCC, "cc", nil, "CC recipients (can be used multiple times)")
	_ = mailSendCmd.MarkFlagRequired("subject") // cobra flags: error only at runtime if missing

	// Inbox flags
	mailInboxCmd.Flags().BoolVar(&mailInboxJSON, "json", false, "Output as JSON")
	mailInboxCmd.Flags().BoolVarP(&mailInboxUnread, "unread", "u", false, "Show only unread messages")
	mailInboxCmd.Flags().StringVar(&mailInboxIdentity, "identity", "", "Explicit identity for inbox (e.g., greenplace/Toast)")
	mailInboxCmd.Flags().StringVar(&mailInboxIdentity, "address", "", "Alias for --identity")

	// Read flags
	mailReadCmd.Flags().BoolVar(&mailReadJSON, "json", false, "Output as JSON")

	// Check flags
	mailCheckCmd.Flags().BoolVar(&mailCheckInject, "inject", false, "Output format for Claude Code hooks")
	mailCheckCmd.Flags().BoolVar(&mailCheckJSON, "json", false, "Output as JSON")
	mailCheckCmd.Flags().StringVar(&mailCheckIdentity, "identity", "", "Explicit identity for inbox (e.g., greenplace/Toast)")
	mailCheckCmd.Flags().StringVar(&mailCheckIdentity, "address", "", "Alias for --identity")

	// Thread flags
	mailThreadCmd.Flags().BoolVar(&mailThreadJSON, "json", false, "Output as JSON")

	// Reply flags
	mailReplyCmd.Flags().StringVarP(&mailReplySubject, "subject", "s", "", "Override reply subject (default: Re: <original>)")
	mailReplyCmd.Flags().StringVarP(&mailReplyMessage, "message", "m", "", "Reply message body (required)")
	_ = mailReplyCmd.MarkFlagRequired("message")

	// Search flags
	mailSearchCmd.Flags().StringVar(&mailSearchFrom, "from", "", "Filter by sender address")
	mailSearchCmd.Flags().BoolVar(&mailSearchSubject, "subject", false, "Only search subject lines")
	mailSearchCmd.Flags().BoolVar(&mailSearchBody, "body", false, "Only search message body")
	mailSearchCmd.Flags().BoolVar(&mailSearchArchive, "archive", false, "Include archived messages")
	mailSearchCmd.Flags().BoolVar(&mailSearchJSON, "json", false, "Output as JSON")

	// Announces flags
	mailAnnouncesCmd.Flags().BoolVar(&mailAnnouncesJSON, "json", false, "Output as JSON")

	// Clear flags
	mailClearCmd.Flags().BoolVar(&mailClearAll, "all", false, "Clear all messages (default behavior)")

	// Add subcommands
	mailCmd.AddCommand(mailSendCmd)
	mailCmd.AddCommand(mailInboxCmd)
	mailCmd.AddCommand(mailReadCmd)
	mailCmd.AddCommand(mailPeekCmd)
	mailCmd.AddCommand(mailDeleteCmd)
	mailCmd.AddCommand(mailArchiveCmd)
	mailCmd.AddCommand(mailMarkReadCmd)
	mailCmd.AddCommand(mailMarkUnreadCmd)
	mailCmd.AddCommand(mailCheckCmd)
	mailCmd.AddCommand(mailThreadCmd)
	mailCmd.AddCommand(mailReplyCmd)
	mailCmd.AddCommand(mailClaimCmd)
	mailCmd.AddCommand(mailReleaseCmd)
	mailCmd.AddCommand(mailClearCmd)
	mailCmd.AddCommand(mailSearchCmd)
	mailCmd.AddCommand(mailAnnouncesCmd)

	rootCmd.AddCommand(mailCmd)
}
