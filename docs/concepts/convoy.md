# Raids

Raids are the primary unit for tracking batched work across warbands.

## Quick Start

```bash
# Create a raid tracking some issues
hd raid create "Feature X" gt-abc gt-def --notify chieftain

# Check progress
hd raid status hq-cv-abc

# List active raids (the warmap)
hd raid list

# See all raids including landed ones
hd raid list --all
```

## Concept

A **raid** is a persistent tracking unit that monitors related issues across
multiple warbands. When you kick off work - even a single issue - a raid tracks it
so you can see when it lands and what was included.

```
                 🚚 Raid (hq-cv-abc)
                         │
            ┌────────────┼────────────┐
            │            │            │
            ▼            ▼            ▼
       ┌─────────┐  ┌─────────┐  ┌─────────┐
       │ gt-xyz  │  │ gt-def  │  │ bd-abc  │
       │ horde │  │ horde │  │  relics  │
       └────┬────┘  └────┬────┘  └────┬────┘
            │            │            │
            ▼            ▼            ▼
       ┌─────────┐  ┌─────────┐  ┌─────────┐
       │  nux    │  │ furiosa │  │  amber  │
       │(raider)│  │(raider)│  │(raider)│
       └─────────┘  └─────────┘  └─────────┘
                         │
                    "the swarm"
                    (ephemeral)
```

## Raid vs Swarm

| Concept | Persistent? | ID | Description |
|---------|-------------|-----|-------------|
| **Raid** | Yes | hq-cv-* | Tracking unit. What you create, track, get notified about. |
| **Swarm** | No | None | Ephemeral. "The workers currently on this raid's issues." |

When you "kick off a swarm", you're really:
1. Creating a raid (the tracking unit)
2. Assigning raiders to the tracked issues
3. The "swarm" is just those raiders while they're working

When issues close, the raid lands and notifies you. The swarm dissolves.

## Raid Lifecycle

```
OPEN ──(all issues close)──► LANDED/CLOSED
  ↑                              │
  └──(add more issues)───────────┘
       (auto-reopens)
```

| State | Description |
|-------|-------------|
| `open` | Active tracking, work in progress |
| `closed` | All tracked issues closed, notification sent |

Adding issues to a closed raid reopens it automatically.

## Commands

### Create a Raid

```bash
# Track multiple issues across warbands
hd raid create "Deploy v2.0" gt-abc bd-xyz --notify horde/joe

# Track a single issue (still creates raid for warmap visibility)
hd raid create "Fix auth bug" gt-auth-fix

# With default notification (from config)
hd raid create "Feature X" gt-a gt-b gt-c
```

### Add Issues

> **Note**: `hd raid add` is not yet implemented. Use `rl dep add` directly:

```bash
# Add issue to existing raid
bd dep add hq-cv-abc gt-new-issue --type=tracks

# Adding to closed raid requires reopening first
bd update hq-cv-abc --status=open
bd dep add hq-cv-abc gt-followup-fix --type=tracks
```

### Check Status

```bash
# Show issues and active workers (the swarm)
hd raid status hq-abc

# All active raids (the warmap)
hd raid status
```

Example output:
```
🚚 hq-cv-abc: Deploy v2.0

  Status:    ●
  Progress:  2/4 completed
  Created:   2025-12-30T10:15:00-08:00

  Tracked Issues:
    ✓ gt-xyz: Update API endpoint [task]
    ✓ bd-abc: Fix validation [bug]
    ○ bd-ghi: Update docs [task]
    ○ gt-jkl: Deploy to prod [task]
```

### List Raids (Warmap)

```bash
# Active raids (default) - the primary attention view
hd raid list

# All raids including landed
hd raid list --all

# Only landed raids
hd raid list --status=closed

# JSON output
hd raid list --json
```

Example output:
```
Raids

  🚚 hq-cv-w3nm6: Feature X ●
  🚚 hq-cv-abc12: Bug fixes ●

Use 'hd raid status <id>' for detailed view.
```

## Notifications

When a raid lands (all tracked issues closed), subscribers are notified:

```bash
# Explicit subscriber
hd raid create "Feature X" gt-abc --notify horde/joe

# Multiple subscribers
hd raid create "Feature X" gt-abc --notify warchief/ --notify --human
```

Notification content:
```
🚚 Raid Landed: Deploy v2.0 (hq-cv-abc)

Issues (3):
  ✓ gt-xyz: Update API endpoint
  ✓ gt-def: Add validation
  ✓ bd-abc: Update docs

Duration: 2h 15m
```

## Auto-Raid on Charge

When you charge a single issue without an existing raid:

```bash
hd charge rl-xyz relics/amber
```

This auto-creates a raid so all work appears in the warmap:
1. Creates raid: "Work: bd-xyz"
2. Tracks the issue
3. Assigns the raider

Even "swarm of one" gets raid visibility.

## Cross-Warband Tracking

Raids live in encampment-level relics (`hq-cv-*` prefix) and can track issues from any warband:

```bash
# Track issues from multiple warbands
hd raid create "Full-stack feature" \
  gt-frontend-abc \
  gt-backend-def \
  bd-docs-xyz
```

The `tracks` relation is:
- **Non-blocking**: doesn't affect issue workflow
- **Additive**: can add issues anytime
- **Cross-warband**: raid in hq-*, issues in gt-*, bd-*, etc.

## Raid vs Warband Status

| View | Scope | Shows |
|------|-------|-------|
| `hd raid status [id]` | Cross-warband | Issues tracked by raid + workers |
| `hd warband status <warband>` | Single warband | All workers in warband + their raid membership |

Use raids for "what's the status of this batch of work?"
Use warband status for "what's everyone in this warband working on?"

## See Also

- [Propulsion Principle](propulsion-principle.md) - Worker execution model
- [Drums Protocol](../design/drums-protocol.md) - Notification delivery
