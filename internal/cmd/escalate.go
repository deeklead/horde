package cmd

import (
	"github.com/spf13/cobra"
)

// Escalate command flags
var (
	escalateSeverity    string
	escalateReason      string
	escalateSource      string
	escalateRelatedBead string
	escalateJSON        bool
	escalateListJSON    bool
	escalateListAll     bool
	escalateStaleJSON   bool
	escalateDryRun      bool
	escalateCloseReason string
)

var escalateCmd = &cobra.Command{
	Use:     "escalate [description]",
	GroupID: GroupComm,
	Short:   "Escalation system for critical issues",
	RunE:    runEscalate,
	Long: `Create and manage escalations for critical issues.

The escalation system provides severity-based routing for issues that need
human or warchief attention. Escalations are tracked as relics with gt:escalation label.

SEVERITY LEVELS:
  critical  (P0) Immediate attention required
  high      (P1) Urgent, needs attention soon
  medium    (P2) Standard escalation (default)
  low       (P3) Informational, can wait

WORKFLOW:
  1. Agent encounters blocking issue
  2. Runs: hd escalate "Description" --severity high --reason "details"
  3. Escalation is routed based on settings/escalation.json
  4. Recipient acknowledges with: hd escalate ack <id>
  5. After resolution: hd escalate close <id> --reason "fixed"

CONFIGURATION:
  Routing is configured in ~/horde/settings/escalation.json:
  - routes: Map severity to action lists (bead, drums:warchief, email:human, sms:human)
  - contacts: Human email/SMS for external notifications
  - stale_threshold: When unacked escalations are re-escalated (default: 4h)
  - max_reescalations: How many times to bump severity (default: 2)

Examples:
  hd escalate "Build failing" --severity critical --reason "CI blocked"
  hd escalate "Need API credentials" --severity high --source "plugin:rebuild-gt"
  hd escalate "Code review requested" --reason "PR #123 ready"
  hd escalate list                          # Show open escalations
  hd escalate ack hq-abc123                 # Acknowledge
  hd escalate close hq-abc123 --reason "Fixed in commit abc"
  hd escalate stale                         # Re-escalate stale escalations`,
}

var escalateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List open escalations",
	Long: `List all open escalations.

Shows escalations that haven't been closed yet. Use --all to include
closed escalations.

Examples:
  hd escalate list              # Open escalations only
  hd escalate list --all        # Include closed
  hd escalate list --json       # JSON output`,
	RunE: runEscalateList,
}

var escalateAckCmd = &cobra.Command{
	Use:   "ack <escalation-id>",
	Short: "Acknowledge an escalation",
	Long: `Acknowledge an escalation to indicate you're working on it.

Adds an "acked" label and records who acknowledged and when.
This stops the stale escalation warnings.

Examples:
  hd escalate ack hq-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runEscalateAck,
}

var escalateCloseCmd = &cobra.Command{
	Use:   "close <escalation-id>",
	Short: "Close a resolved escalation",
	Long: `Close an escalation after the issue is resolved.

Records who closed it and the resolution reason.

Examples:
  hd escalate close hq-abc123 --reason "Fixed in commit abc"
  hd escalate close hq-abc123 --reason "Not reproducible"`,
	Args: cobra.ExactArgs(1),
	RunE: runEscalateClose,
}

var escalateStaleCmd = &cobra.Command{
	Use:   "stale",
	Short: "Re-escalate stale unacknowledged escalations",
	Long: `Find and re-escalate escalations that haven't been acknowledged within the threshold.

When run without --dry-run, this command:
1. Finds escalations older than the stale threshold (default: 4h)
2. Bumps their severity: low→medium→high→critical
3. Re-routes them according to the new severity level
4. Sends drums to the new routing targets

Respects max_reescalations from config (default: 2) to prevent infinite escalation.

The threshold is configured in settings/escalation.json.

Examples:
  hd escalate stale              # Re-escalate stale escalations
  hd escalate stale --dry-run    # Show what would be done
  hd escalate stale --json       # JSON output of results`,
	RunE: runEscalateStale,
}

var escalateShowCmd = &cobra.Command{
	Use:   "show <escalation-id>",
	Short: "Show details of an escalation",
	Long: `Display detailed information about an escalation.

Examples:
  hd escalate show hq-abc123
  hd escalate show hq-abc123 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runEscalateShow,
}

func init() {
	// Main escalate command flags
	escalateCmd.Flags().StringVarP(&escalateSeverity, "severity", "s", "medium", "Severity level: critical, high, medium, low")
	escalateCmd.Flags().StringVarP(&escalateReason, "reason", "r", "", "Detailed reason for escalation")
	escalateCmd.Flags().StringVar(&escalateSource, "source", "", "Source identifier (e.g., plugin:rebuild-gt, scout:shaman)")
	escalateCmd.Flags().StringVar(&escalateRelatedBead, "related", "", "Related bead ID (task, bug, etc.)")
	escalateCmd.Flags().BoolVar(&escalateJSON, "json", false, "Output as JSON")
	escalateCmd.Flags().BoolVarP(&escalateDryRun, "dry-run", "n", false, "Show what would be done without executing")

	// List subcommand flags
	escalateListCmd.Flags().BoolVar(&escalateListJSON, "json", false, "Output as JSON")
	escalateListCmd.Flags().BoolVar(&escalateListAll, "all", false, "Include closed escalations")

	// Close subcommand flags
	escalateCloseCmd.Flags().StringVar(&escalateCloseReason, "reason", "", "Resolution reason")
	_ = escalateCloseCmd.MarkFlagRequired("reason")

	// Stale subcommand flags
	escalateStaleCmd.Flags().BoolVar(&escalateStaleJSON, "json", false, "Output as JSON")
	escalateStaleCmd.Flags().BoolVarP(&escalateDryRun, "dry-run", "n", false, "Show what would be re-escalated without acting")

	// Show subcommand flags
	escalateShowCmd.Flags().BoolVar(&escalateJSON, "json", false, "Output as JSON")

	// Add subcommands
	escalateCmd.AddCommand(escalateListCmd)
	escalateCmd.AddCommand(escalateAckCmd)
	escalateCmd.AddCommand(escalateCloseCmd)
	escalateCmd.AddCommand(escalateStaleCmd)
	escalateCmd.AddCommand(escalateShowCmd)

	rootCmd.AddCommand(escalateCmd)
}
