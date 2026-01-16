# Raid Lifecycle Design

> Making raids actively converge on completion.

## Problem Statement

Raids are passive trackers. They group work but don't drive it. The completion
loop has a structural gap:

```
Create → Assign → Execute → Issues close → ??? → Raid closes
```

The `???` is "Shaman scout runs `hd raid check`" - a poll-based single point of
failure. When Shaman is down, raids don't close. Work completes but the loop
never lands.

## Current State

### What Works
- Raid creation and issue tracking
- `hd raid status` shows progress
- `hd raid stranded` finds unassigned work
- `hd raid check` auto-closes completed raids

### What Breaks
1. **Poll-based completion**: Only Shaman runs `hd raid check`
2. **No event-driven trigger**: Issue close doesn't propagate to raid
3. **No manual close**: Can't force-close abandoned raids
4. **Single observer**: No redundant completion detection
5. **Weak notification**: Raid owner not always clear

## Design: Active Raid Convergence

### Principle: Event-Driven, Redundantly Observed

Raid completion should be:
1. **Event-driven**: Triggered by issue close, not polling
2. **Redundantly observed**: Multiple agents can detect and close
3. **Manually overridable**: Humans can force-close

### Event-Driven Completion

When an issue closes, check if it's tracked by a raid:

```
Issue closes
    ↓
Is issue tracked by raid? ──(no)──► done
    │
   (yes)
    ↓
Run hd raid check <raid-id>
    ↓
All tracked issues closed? ──(no)──► done
    │
   (yes)
    ↓
Close raid, send notifications
```

**Implementation options:**
1. Daemon hook on `rl update --status=closed`
2. Forge step after successful merge
3. Witness step after verifying raider completion

Option 1 is most reliable - catches all closes regardless of source.

### Redundant Observers

Per PRIMING.md: "Redundant Monitoring Is Resilience."

Three places should check raid completion:

| Observer | When | Scope |
|----------|------|-------|
| **Daemon** | On any issue close | All raids |
| **Witness** | After verifying raider work | Warband's raid work |
| **Shaman** | Periodic scout | All raids (backup) |

Any observer noticing completion triggers close. Idempotent - closing
an already-closed raid is a no-op.

### Manual Close Command

**Desire path**: `hd raid close` is expected but missing.

```bash
# Close a completed raid
hd raid close hq-cv-abc

# Force-close an abandoned raid
hd raid close hq-cv-xyz --reason="work done differently"

# Close with explicit notification
hd raid close hq-cv-abc --notify warchief/
```

Use cases:
- Abandoned raids no longer relevant
- Work completed outside tracked path
- Force-closing stuck raids

### Raid Owner/Requester

Track who requested the raid for targeted notifications:

```bash
hd raid create "Feature X" gt-abc --owner warchief/ --notify overseer
```

| Field | Purpose |
|-------|---------|
| `owner` | Who requested (gets completion notification) |
| `notify` | Additional subscribers |

If `owner` not specified, defaults to creator (from `created_by`).

### Raid States

```
OPEN ──(all issues close)──► CLOSED
  │                             │
  │                             ▼
  │                    (add issues)
  │                             │
  └─────────────────────────────┘
         (auto-reopens)
```

Adding issues to closed raid reopens automatically.

**New state for abandonment:**

```
OPEN ──► CLOSED (completed)
  │
  └────► ABANDONED (force-closed without completion)
```

### Timeout/SLA (Future)

Optional `due_at` field for raid deadline:

```bash
hd raid create "Sprint work" gt-abc --due="2026-01-15"
```

Overdue raids surface in `hd raid stranded --overdue`.

## Commands

### New: `hd raid close`

```bash
hd raid close <raid-id> [--reason=<reason>] [--notify=<agent>]
```

- Closes raid regardless of tracked issue status
- Sets `close_reason` field
- Sends notification to owner and subscribers
- Idempotent - closing closed raid is no-op

### Enhanced: `hd raid check`

```bash
# Check all raids (current behavior)
hd raid check

# Check specific raid (new)
hd raid check <raid-id>

# Dry-run mode
hd raid check --dry-run
```

### New: `hd raid reopen`

```bash
hd raid reopen <raid-id>
```

Explicit reopen for clarity (currently implicit via add).

## Implementation Priority

1. **P0: `hd raid close`** - Desire path, escape hatch
2. **P0: Event-driven check** - Daemon hook on issue close
3. **P1: Redundant observers** - Witness/Forge integration
4. **P2: Owner field** - Targeted notifications
5. **P3: Timeout/SLA** - Deadline tracking

## Related

- [raid.md](../concepts/raid.md) - Raid concept and usage
- [watchdog-chain.md](watchdog-chain.md) - Shaman scout system
- [drums-protocol.md](drums-protocol.md) - Notification delivery
