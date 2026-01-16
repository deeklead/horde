# Escalation System Design

> Detailed design for the Horde unified escalation system.
> Written 2026-01-11, clan/george session.
> Parent epic: gt-i9r20

## Problem Statement

Current escalation is ad-hoc "drums Warchief". Issues:
- Warchief gets backlogged easily (especially during swarms)
- No severity differentiation
- No alternative channels (email, SMS, Slack)
- No tracking of stale/unacknowledged escalations
- No visibility into escalation history

## Design Goals

1. **Unified API**: Single `hd escalate` command for all escalation needs
2. **Severity-based routing**: Different severities go to different channels
3. **Config-driven**: Encampment config controls routing, no code changes needed
4. **Audit trail**: All escalations tracked as relics
5. **Stale detection**: Unacknowledged escalations re-escalate automatically
6. **Extensible**: Easy to add new notification channels

---

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                    hd escalate command                       │
│  --severity --subject --body --source                       │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                  Escalation Manager                          │
│  1. Read config (settings/escalation.json)                  │
│  2. Create escalation bead                                   │
│  3. Execute route actions for severity                       │
└─────────────────────┬───────────────────────────────────────┘
                      │
          ┌───────────┼───────────┬───────────┐
          ▼           ▼           ▼           ▼
      ┌───────┐  ┌─────────┐  ┌───────┐  ┌───────┐
      │ Bead  │  │  Drums   │  │ Email │  │  SMS  │
      │Create │  │ Action  │  │Action │  │Action │
      └───────┘  └─────────┘  └───────┘  └───────┘
```

### Data Flow

1. Agent calls `hd escalate --severity=high --subject="..." --body="..."`
2. Command loads escalation config from `settings/escalation.json`
3. Creates escalation bead with severity, subject, body, source labels
4. Looks up route for severity level
5. Executes each action in the route (bead already created, then drums, email, etc.)
6. Returns escalation bead ID

### Stale Escalation Flow

1. Shaman scout (or plugin) runs `hd escalate stale`
2. Queries for escalation relics older than threshold without `acknowledged:true`
3. For each stale escalation:
   - Bump severity (low→medium, medium→high, high→critical)
   - Re-execute route for new severity
   - Add `reescalated:true` label and timestamp

---

## Configuration

### File Location

`~/horde/settings/escalation.json`

This follows the existing pattern where `~/horde/settings/` contains encampment-level behavioral config.

### Schema

```go
// EscalationConfig represents escalation routing configuration.
type EscalationConfig struct {
    Type    string `json:"type"`    // "escalation"
    Version int    `json:"version"` // schema version

    // Routes maps severity levels to action lists.
    // Actions are executed in order.
    Routes map[string][]string `json:"routes"`

    // Contacts contains contact information for actions.
    Contacts EscalationContacts `json:"contacts"`

    // StaleThreshold is how long before an unacknowledged escalation
    // is considered stale and gets re-escalated. Default: "4h"
    StaleThreshold string `json:"stale_threshold,omitempty"`

    // MaxReescalations limits how many times an escalation can be
    // re-escalated. Default: 2 (low→medium→high, then stops)
    MaxReescalations int `json:"max_reescalations,omitempty"`
}

// EscalationContacts contains contact information.
type EscalationContacts struct {
    HumanEmail string `json:"human_email,omitempty"`
    HumanSMS   string `json:"human_sms,omitempty"`
    SlackWebhook string `json:"slack_webhook,omitempty"`
}

const CurrentEscalationVersion = 1
```

### Default Configuration

```json
{
  "type": "escalation",
  "version": 1,
  "routes": {
    "low": ["bead"],
    "medium": ["bead", "drums:warchief"],
    "high": ["bead", "drums:warchief", "email:human"],
    "critical": ["bead", "drums:warchief", "email:human", "sms:human"]
  },
  "contacts": {
    "human_email": "",
    "human_sms": ""
  },
  "stale_threshold": "4h",
  "max_reescalations": 2
}
```

### Action Types

| Action | Format | Behavior |
|--------|--------|----------|
| `bead` | `bead` | Create escalation bead (always first, implicit) |
| `drums:<target>` | `drums:warchief` | Send hd drums to target |
| `email:human` | `email:human` | Send email to `contacts.human_email` |
| `sms:human` | `sms:human` | Send SMS to `contacts.human_sms` |
| `slack` | `slack` | Post to `contacts.slack_webhook` |
| `log` | `log` | Write to escalation log file |

### Severity Levels

| Level | Use Case | Default Route |
|-------|----------|---------------|
| `low` | Informational, non-urgent | bead only |
| `medium` | Needs attention soon | bead + drums warchief |
| `high` | Urgent, needs human | bead + drums + email |
| `critical` | Emergency, immediate | bead + drums + email + SMS |

---

## Escalation Relics

### Bead Format

```yaml
id: gt-esc-abc123
type: escalation
status: open
title: "Plugin FAILED: rebuild-gt"
labels:
  - severity:high
  - source:plugin:rebuild-gt
  - acknowledged:false
  - reescalated:false
  - reescalation_count:0
description: |
  Build failed: make returned exit code 2

  ## Context
  - Source: plugin:rebuild-gt
  - Original severity: medium
  - Escalated at: 2026-01-11T19:00:00Z
created_at: 2026-01-11T15:00:00Z
```

### Label Schema

| Label | Values | Purpose |
|-------|--------|---------|
| `severity:<level>` | low, medium, high, critical | Current severity |
| `source:<type>:<name>` | plugin:rebuild-gt, scout:shaman | What triggered it |
| `acknowledged:<bool>` | true, false | Has human acknowledged |
| `reescalated:<bool>` | true, false | Has been re-escalated |
| `reescalation_count:<n>` | 0, 1, 2, ... | Times re-escalated |
| `original_severity:<level>` | low, medium, high | Initial severity |

---

## Commands

### hd escalate

Create a new escalation.

```bash
gt escalate \
  --severity=<low|medium|high|critical> \
  --subject="Short description" \
  --body="Detailed explanation" \
  [--source="plugin:rebuild-gt"]
```

**Flags:**
- `--severity` (required): Escalation severity level
- `--subject` (required): Short description (becomes bead title)
- `--body` (required): Detailed explanation (becomes bead description)
- `--source`: Source identifier for tracking (e.g., "plugin:rebuild-gt")
- `--dry-run`: Show what would happen without executing
- `--json`: Output escalation bead ID as JSON

**Exit codes:**
- 0: Success
- 1: Config error or invalid flags
- 2: Action failed (e.g., email send failed)

**Example:**
```bash
gt escalate \
  --severity=high \
  --subject="Plugin FAILED: rebuild-gt" \
  --body="Build failed: make returned exit code 2. Working directory: ~/horde/horde/clan/george" \
  --source="plugin:rebuild-gt"

# Output:
# ✓ Created escalation gt-esc-abc123 (severity: high)
# → Created bead
# → Mailed warchief/
# → Emailed steve@example.com
```

### hd escalate ack

Acknowledge an escalation.

```bash
gt escalate ack <bead-id> [--note="Investigating"]
```

**Behavior:**
- Sets `acknowledged:true` label
- Optionally adds note to bead
- Prevents re-escalation

**Example:**
```bash
gt escalate ack gt-esc-abc123 --note="Looking into it"
# ✓ Acknowledged gt-esc-abc123
```

### hd escalate list

List escalations.

```bash
gt escalate list [--severity=...] [--stale] [--unacked] [--all]
```

**Flags:**
- `--severity`: Filter by severity level
- `--stale`: Show only stale (past threshold, unacked)
- `--unacked`: Show only unacknowledged
- `--all`: Include acknowledged/closed
- `--json`: Output as JSON

**Example:**
```bash
gt escalate list --unacked
# 📢 Unacknowledged Escalations (2)
#
#   ● gt-esc-abc123 [HIGH] Plugin FAILED: rebuild-gt
#     Source: plugin:rebuild-gt · Age: 2h · Stale in: 2h
#   ● gt-esc-def456 [MEDIUM] Witness unresponsive
#     Source: scout:shaman · Age: 30m · Stale in: 3h30m
```

### hd escalate stale

Check for and re-escalate stale escalations.

```bash
gt escalate stale [--dry-run]
```

**Behavior:**
- Queries unacked escalations older than `stale_threshold`
- For each, bumps severity and re-executes route
- Respects `max_reescalations` limit

**Example:**
```bash
gt escalate stale
# 🔄 Re-escalating stale escalations...
#
#   gt-esc-abc123: medium → high (age: 5h, reescalation: 1/2)
#   → Emailed steve@example.com
#
# ✓ Re-escalated 1 escalation
```

### hd escalate close

Close an escalation (resolved).

```bash
gt escalate close <bead-id> [--reason="Fixed in commit abc123"]
```

**Behavior:**
- Sets status to closed
- Adds resolution note
- Records who closed it

---

## Implementation Details

### File: internal/cmd/escalate.go

```go
package cmd

// escalateCmd is the parent command for escalation management.
var escalateCmd = &cobra.Command{
    Use:   "escalate",
    Short: "Manage escalations",
    Long:  `Create, acknowledge, and manage escalations with severity-based routing.`,
}

// escalateCreateCmd creates a new escalation.
var escalateCreateCmd = &cobra.Command{
    Use:   "escalate --severity=<level> --subject=<text> --body=<text>",
    Short: "Create a new escalation",
    // ... implementation
}

// escalateAckCmd acknowledges an escalation.
var escalateAckCmd = &cobra.Command{
    Use:   "ack <bead-id>",
    Short: "Acknowledge an escalation",
    // ... implementation
}

// escalateListCmd lists escalations.
var escalateListCmd = &cobra.Command{
    Use:   "list",
    Short: "List escalations",
    // ... implementation
}

// escalateStaleCmd checks for stale escalations.
var escalateStaleCmd = &cobra.Command{
    Use:   "stale",
    Short: "Re-escalate stale escalations",
    // ... implementation
}

// escalateCloseCmd closes an escalation.
var escalateCloseCmd = &cobra.Command{
    Use:   "close <bead-id>",
    Short: "Close an escalation",
    // ... implementation
}
```

### File: internal/escalation/manager.go

```go
package escalation

// Manager handles escalation creation and routing.
type Manager struct {
    config *config.EscalationConfig
    relics  *relics.Client
    mailer *drums.Client
}

// Escalate creates a new escalation and executes the route.
func (m *Manager) Escalate(ctx context.Context, opts EscalateOptions) (*Escalation, error) {
    // 1. Validate options
    // 2. Create escalation bead
    // 3. Look up route for severity
    // 4. Execute each action
    // 5. Return escalation with results
}

// Acknowledge marks an escalation as acknowledged.
func (m *Manager) Acknowledge(ctx context.Context, beadID string, note string) error {
    // 1. Load escalation bead
    // 2. Set acknowledged:true label
    // 3. Add note if provided
}

// ReescalateStale finds and re-escalates stale escalations.
func (m *Manager) ReescalateStale(ctx context.Context) ([]Reescalation, error) {
    // 1. Query unacked escalations older than threshold
    // 2. For each, bump severity
    // 3. Execute new route
    // 4. Update labels
}
```

### File: internal/escalation/actions.go

```go
package escalation

// Action is an escalation route action.
type Action interface {
    Execute(ctx context.Context, esc *Escalation) error
    String() string
}

// BeadAction creates the escalation bead.
type BeadAction struct{}

// MailAction sends hd drums.
type MailAction struct {
    Target string // e.g., "warchief"
}

// EmailAction sends email.
type EmailAction struct {
    Recipient string // from config.contacts
}

// SMSAction sends SMS.
type SMSAction struct {
    Recipient string // from config.contacts
}

// ParseAction parses an action string into an Action.
func ParseAction(s string) (Action, error) {
    // "bead" -> BeadAction{}
    // "drums:warchief" -> MailAction{Target: "warchief"}
    // "email:human" -> EmailAction{Recipient: "human"}
    // etc.
}
```

### Email/SMS Implementation

For v1, use simple exec of external commands:

```go
// EmailAction sends email using the 'drums' command or similar.
func (a *EmailAction) Execute(ctx context.Context, esc *Escalation) error {
    // Option 1: Use system drums command
    // Option 2: Use sendgrid/ses API (future)
    // Option 3: Use configured webhook

    // For now, just log a placeholder
    // Real implementation can be added based on user's infrastructure
}
```

The email/SMS actions can start as stubs that log warnings, with real implementations added based on the user's infrastructure (SendGrid, Twilio, etc.).

---

## Integration Points

### Plugin System

Plugins use escalation for failure notification:

```markdown
# In plugin.md execution section:

On failure:
```bash
gt escalate \
  --severity=medium \
  --subject="Plugin FAILED: rebuild-gt" \
  --body="$ERROR" \
  --source="plugin:rebuild-gt"
```
```

### Shaman Scout

Shaman uses escalation for health issues:

```bash
# In health-scan step:
if [ $unresponsive_cycles -ge 5 ]; then
  hd escalate \
    --severity=high \
    --subject="Witness unresponsive: horde" \
    --body="Witness has been unresponsive for $unresponsive_cycles cycles" \
    --source="scout:shaman:health-scan"
fi
```

### Stale Escalation Check

Can be either:
1. A Shaman scout step
2. A plugin (dogfood!)
3. Part of `hd escalate` itself (run periodically)

Recommendation: Start as scout step, migrate to plugin later.

---

## Testing Plan

### Unit Tests

- Config loading and validation
- Action parsing
- Severity level ordering
- Re-escalation logic

### Integration Tests

- Create escalation → bead exists
- Acknowledge → label updated
- Stale detection → re-escalation triggers
- Route execution → all actions called

### Manual Testing

1. `hd escalate --severity=low --subject="Test" --body="Testing"`
2. `hd escalate list --unacked`
3. `hd escalate ack <id>`
4. Wait for stale threshold, run `hd escalate stale`

---

## Dependencies

### Internal Dependencies (task order)

```
gt-i9r20.2 (Config Schema)
    │
    ▼
gt-i9r20.1 (gt escalate command)
    │
    ├──▶ gt-i9r20.4 (gt escalate ack)
    │
    └──▶ gt-i9r20.3 (Stale scout)
```

### External Dependencies

- `rl create` for creating escalation relics
- `rl list` for querying escalations
- `rl label` for updating labels
- `hd drums send` for drums action

---

## Open Questions (Resolved)

1. **Where to store config?** → `settings/escalation.json` (follows existing pattern)
2. **How to implement email/SMS?** → Start with stubs, add real impl based on infrastructure
3. **Stale check: scout step or plugin?** → Start as scout step, can migrate to plugin
4. **Escalation bead type?** → `type: escalation` (new bead type)

---

## Future Enhancements

1. **Slack integration**: Post to Slack channels
2. **PagerDuty integration**: Create incidents
3. **Escalation warmap**: Web UI for escalation management
4. **Scheduled escalations**: "Remind me in 2h if not resolved"
5. **Escalation templates**: Pre-defined escalation types
