package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/events"
	"github.com/deeklead/horde/internal/style"
	"github.com/deeklead/horde/internal/workspace"
)

// Activity emit command flags
var (
	activityEventType string
	activityActor     string
	activityRig       string
	activityRaider   string
	activityTarget    string
	activityReason    string
	activityMessage   string
	activityStatus    string
	activityIssue     string
	activityTo        string
	activityCount     int
)

var activityCmd = &cobra.Command{
	Use:     "activity",
	GroupID: GroupDiag,
	Short:   "Emit and view activity events",
	Long: `Emit and view activity events for the Horde activity feed.

Events are written to ~/horde/.events.jsonl and can be viewed with 'hd feed'.

Subcommands:
  emit    Emit an activity event`,
}

var activityEmitCmd = &cobra.Command{
	Use:   "emit <event-type>",
	Short: "Emit an activity event",
	Long: `Emit an activity event to the Horde activity feed.

Supported event types for witness scout:
  patrol_started   - When witness begins scout cycle
  raider_checked  - When witness checks a raider
  raider_nudged   - When witness nudges a stuck raider
  escalation_sent  - When witness escalates to Warchief/Shaman
  patrol_complete  - When scout cycle finishes

Supported event types for forge:
  merge_started    - When forge starts a merge
  merge_complete   - When merge succeeds
  merge_failed     - When merge fails
  queue_processed  - When forge finishes processing queue

Common options:
  --actor    Who is emitting the event (e.g., greenplace/witness)
  --warband      Which warband the event is about
  --message  Human-readable message

Examples:
  hd activity emit patrol_started --warband greenplace --count 3
  hd activity emit raider_checked --warband greenplace --raider Toast --status working --issue gp-xyz
  hd activity emit raider_nudged --warband greenplace --raider Toast --reason "idle for 10 minutes"
  hd activity emit escalation_sent --warband greenplace --target Toast --to warchief --reason "unresponsive"
  hd activity emit patrol_complete --warband greenplace --count 3 --message "All raiders healthy"`,
	Args: cobra.ExactArgs(1),
	RunE: runActivityEmit,
}

func init() {
	// Emit command flags
	activityEmitCmd.Flags().StringVar(&activityActor, "actor", "", "Actor emitting the event (auto-detected if not set)")
	activityEmitCmd.Flags().StringVar(&activityRig, "warband", "", "Warband the event is about")
	activityEmitCmd.Flags().StringVar(&activityRaider, "raider", "", "Raider involved (for raider_checked, raider_nudged)")
	activityEmitCmd.Flags().StringVar(&activityTarget, "target", "", "Target of the action (for escalation)")
	activityEmitCmd.Flags().StringVar(&activityReason, "reason", "", "Reason for the action")
	activityEmitCmd.Flags().StringVar(&activityMessage, "message", "", "Human-readable message")
	activityEmitCmd.Flags().StringVar(&activityStatus, "status", "", "Status (for raider_checked: working, idle, stuck)")
	activityEmitCmd.Flags().StringVar(&activityIssue, "issue", "", "Issue ID (for raider_checked)")
	activityEmitCmd.Flags().StringVar(&activityTo, "to", "", "Escalation target (for escalation_sent: warchief, shaman)")
	activityEmitCmd.Flags().IntVar(&activityCount, "count", 0, "Raider count (for scout events)")

	activityCmd.AddCommand(activityEmitCmd)
	rootCmd.AddCommand(activityCmd)
}

func runActivityEmit(cmd *cobra.Command, args []string) error {
	eventType := args[0]

	// Validate we're in a Horde workspace
	_, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Auto-detect actor if not provided
	actor := activityActor
	if actor == "" {
		actor = detectActor()
	}

	// Build payload based on event type
	var payload map[string]interface{}

	switch eventType {
	case events.TypePatrolStarted, events.TypePatrolComplete:
		if activityRig == "" {
			return fmt.Errorf("--warband is required for %s events", eventType)
		}
		payload = events.PatrolPayload(activityRig, activityCount, activityMessage)

	case events.TypeRaiderChecked:
		if activityRig == "" || activityRaider == "" {
			return fmt.Errorf("--warband and --raider are required for raider_checked events")
		}
		if activityStatus == "" {
			activityStatus = "checked"
		}
		payload = events.RaiderCheckPayload(activityRig, activityRaider, activityStatus, activityIssue)

	case events.TypeRaiderNudged:
		if activityRig == "" || activityRaider == "" {
			return fmt.Errorf("--warband and --raider are required for raider_nudged events")
		}
		payload = events.NudgePayload(activityRig, activityRaider, activityReason)

	case events.TypeEscalationSent:
		if activityRig == "" || activityTarget == "" || activityTo == "" {
			return fmt.Errorf("--warband, --target, and --to are required for escalation_sent events")
		}
		payload = events.EscalationPayload(activityRig, activityTarget, activityTo, activityReason)

	case events.TypeMergeStarted, events.TypeMerged, events.TypeMergeFailed, events.TypeMergeSkipped:
		// Forge events - flexible payload
		payload = make(map[string]interface{})
		if activityRig != "" {
			payload["warband"] = activityRig
		}
		if activityMessage != "" {
			payload["message"] = activityMessage
		}
		if activityTarget != "" {
			payload["branch"] = activityTarget
		}
		if activityReason != "" {
			payload["reason"] = activityReason
		}

	default:
		// Generic event - use whatever flags are provided
		payload = make(map[string]interface{})
		if activityRig != "" {
			payload["warband"] = activityRig
		}
		if activityRaider != "" {
			payload["raider"] = activityRaider
		}
		if activityTarget != "" {
			payload["target"] = activityTarget
		}
		if activityReason != "" {
			payload["reason"] = activityReason
		}
		if activityMessage != "" {
			payload["message"] = activityMessage
		}
		if activityStatus != "" {
			payload["status"] = activityStatus
		}
		if activityIssue != "" {
			payload["issue"] = activityIssue
		}
		if activityTo != "" {
			payload["to"] = activityTo
		}
		if activityCount > 0 {
			payload["count"] = activityCount
		}
	}

	// Emit the event
	if err := events.LogFeed(eventType, actor, payload); err != nil {
		return fmt.Errorf("emitting event: %w", err)
	}

	// Print confirmation
	payloadJSON, _ := json.Marshal(payload)
	fmt.Printf("%s Emitted %s event\n", style.Success.Render("✓"), style.Bold.Render(eventType))
	fmt.Printf("  Actor:   %s\n", actor)
	fmt.Printf("  Payload: %s\n", string(payloadJSON))

	return nil
}

// Note: detectActor is defined in charge.go and reused here
