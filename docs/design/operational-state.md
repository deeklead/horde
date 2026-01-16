# Operational State in Horde

> Managing runtime state through events and labels.

## Overview

Horde tracks operational state changes as structured data. This document covers:
- **Events**: State transitions as relics (immutable audit trail)
- **Labels-as-state**: Fast queries via role bead labels (current state cache)

For Boot triage and degraded mode details, see [Watchdog Chain](watchdog-chain.md).

## Events: State Transitions as Data

Operational state changes are recorded as event relics. Each event captures:
- **What** changed (`event_type`)
- **Who** caused it (`actor`)
- **What** was affected (`target`)
- **Context** (`payload`)
- **When** (`created_at`)

### Event Types

| Event Type | Description | Payload |
|------------|-------------|---------|
| `scout.muted` | Scout cycle disabled | `{reason, until?}` |
| `scout.unmuted` | Scout cycle re-enabled | `{reason?}` |
| `agent.started` | Agent session began | `{session_id?}` |
| `agent.stopped` | Agent session ended | `{reason, outcome?}` |
| `mode.degraded` | System entered degraded mode | `{reason}` |
| `mode.normal` | System returned to normal | `{}` |

### Creating Events

```bash
# Mute shaman scout
bd create --type=event --event-type=scout.muted \
  --actor=human:overseer --target=agent:shaman \
  --payload='{"reason":"fixing raid deadlock","until":"gt-abc1"}'

# System entered degraded mode
bd create --type=event --event-type=mode.degraded \
  --actor=system:daemon --target=warband:greenplace \
  --payload='{"reason":"tmux unavailable"}'
```

### Querying Events

```bash
# Recent events for an agent
bd list --type=event --target=agent:shaman --limit=10

# All scout state changes
bd list --type=event --event-type=scout.muted
bd list --type=event --event-type=scout.unmuted

# Events in the activity feed
bd activity --follow --type=event
```

## Labels-as-State Pattern

Events capture the full history. Labels cache the current state for fast queries.

### Convention

Labels use `<dimension>:<value>` format:
- `scout:muted` / `scout:active`
- `mode:degraded` / `mode:normal`
- `status:idle` / `status:working` (for persistent agents only - see note)

**Note on raiders:** The `status:idle` label does NOT apply to raiders. Raiders
have no idle state - they're either working, stalled (stopped unexpectedly), or
zombie (`hd done` failed). This label is for persistent agents like Shaman, Witness,
and Clan members who can legitimately be idle between tasks.

### State Change Flow

1. Create event bead (full context, immutable)
2. Update role bead labels (current state cache)

```bash
# Mute scout
bd create --type=event --event-type=scout.muted ...
bd update role-shaman --add-label=scout:muted --remove-label=scout:active

# Unmute scout
bd create --type=event --event-type=scout.unmuted ...
bd update role-shaman --add-label=scout:active --remove-label=scout:muted
```

### Querying Current State

```bash
# Is shaman scout muted?
bd show role-shaman | grep scout:

# All agents with muted scout
bd list --type=role --label=scout:muted

# All agents in degraded mode
bd list --type=role --label=mode:degraded
```

## Configuration vs State

| Type | Storage | Example |
|------|---------|---------|
| **Static config** | TOML files | Daemon tick interval |
| **Operational state** | Relics (events + labels) | Scout muted |
| **Runtime flags** | Marker files | `.shaman-disabled` |

Static config rarely changes and doesn't need history.
Operational state changes at runtime and benefits from audit trail.
Marker files are fast checks that can trigger deeper relics queries.

## Commands Summary

```bash
# Create operational event
bd create --type=event --event-type=<type> \
  --actor=<entity> --target=<entity> --payload='<json>'

# Update state label
bd update <role-bead> --add-label=<dim>:<val> --remove-label=<dim>:<old>

# Query current state
bd list --type=role --label=<dim>:<val>

# Query state history
bd list --type=event --target=<entity>

# Boot management
gt dog status boot
gt dog call boot
gt dog rally boot
```

---

*Events are the source of truth. Labels are the cache.*
