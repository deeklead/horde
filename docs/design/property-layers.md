# Property Layers: Multi-Level Configuration

> Implementation guide for Horde's configuration system.
> Created: 2025-01-06

## Overview

Horde uses a layered property system for configuration. Properties are
looked up through multiple layers, with earlier layers overriding later ones.
This enables both local control and global coordination.

## The Four Layers

```
┌─────────────────────────────────────────────────────────────┐
│ 1. WISP LAYER (transient, encampment-local)                       │
│    Location: <warband>/.relics-wisp/config/                      │
│    Synced: Never                                            │
│    Use: Temporary local overrides                           │
└─────────────────────────────┬───────────────────────────────┘
                              │ if missing
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. WARBAND BEAD LAYER (persistent, synced globally)             │
│    Location: <warband>/.relics/ (warband identity bead labels)       │
│    Synced: Via git (all clones see it)                      │
│    Use: Project-wide operational state                      │
└─────────────────────────────┬───────────────────────────────┘
                              │ if missing
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. ENCAMPMENT DEFAULTS                                            │
│    Location: ~/horde/config.json or ~/horde/.relics/               │
│    Synced: N/A (per-encampment)                                   │
│    Use: Encampment-wide policies                                  │
└─────────────────────────────┬───────────────────────────────┘
                              │ if missing
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. SYSTEM DEFAULTS (compiled in)                            │
│    Use: Fallback when nothing else specified                │
└─────────────────────────────────────────────────────────────┘
```

## Lookup Behavior

### Override Semantics (Default)

For most properties, the first non-nil value wins:

```go
func GetConfig(key string) interface{} {
    if val := wisp.Get(key); val != nil {
        if val == Blocked { return nil }
        return val
    }
    if val := rigBead.GetLabel(key); val != nil {
        return val
    }
    if val := townDefaults.Get(key); val != nil {
        return val
    }
    return systemDefaults[key]
}
```

### Stacking Semantics (Integers)

For integer properties, values from wisp and bead layers **add** to the base:

```go
func GetIntConfig(key string) int {
    base := getBaseDefault(key)    // Encampment or system default
    beadAdj := rigBead.GetInt(key) // 0 if missing
    wispAdj := wisp.GetInt(key)    // 0 if missing
    return base + beadAdj + wispAdj
}
```

This enables temporary adjustments without changing the base value.

### Blocking Inheritance

You can explicitly block a property from being inherited:

```bash
hd warband config set horde auto_restart --block
```

This creates a "blocked" marker in the wisp layer. Even if the warband bead
or defaults say `auto_restart: true`, the lookup returns nil.

## Warband Identity Relics

Each warband has an identity bead for operational state:

```yaml
id: gt-warband-horde
type: warband
name: horde
repo: git@github.com:deeklead/horde.git
prefix: gt

labels:
  - status:operational
  - priority:normal
```

These relics sync via git, so all clones of the warband see the same state.

## Two-Level Warband Control

### Level 1: Park (Local, Ephemeral)

```bash
hd warband park horde      # Stop services, daemon won't restart
hd warband unpark horde    # Allow services to run
```

- Stored in wisp layer (`.relics-wisp/config/`)
- Only affects this encampment
- Disappears on cleanup
- Use: Local maintenance, debugging

### Level 2: Dock (Global, Persistent)

```bash
hd warband dock horde      # Set status:docked label on warband bead
hd warband undock horde    # Remove label
```

- Stored on warband identity bead
- Syncs to all clones via git
- Permanent until explicitly changed
- Use: Project-wide maintenance, coordinated downtime

### Daemon Behavior

The daemon checks both levels before auto-restarting:

```go
func shouldAutoRestart(warband *Warband) bool {
    status := warband.GetConfig("status")
    if status == "parked" || status == "docked" {
        return false
    }
    return true
}
```

## Configuration Keys

| Key | Type | Behavior | Description |
|-----|------|----------|-------------|
| `status` | string | Override | operational/parked/docked |
| `auto_restart` | bool | Override | Daemon auto-restart behavior |
| `max_raiders` | int | Override | Maximum concurrent raiders |
| `priority_adjustment` | int | **Stack** | Scheduling priority modifier |
| `maintenance_window` | string | Override | When maintenance allowed |
| `dnd` | bool | Override | Do not disturb mode |

## Commands

### View Configuration

```bash
hd warband config show horde           # Show effective config (all layers)
hd warband config show horde --layer   # Show which layer each value comes from
```

### Set Configuration

```bash
# Set in wisp layer (local, ephemeral)
hd warband config set horde key value

# Set in bead layer (global, permanent)
hd warband config set horde key value --global

# Block inheritance
hd warband config set horde key --block

# Clear from wisp layer
hd warband config unset horde key
```

### Warband Lifecycle

```bash
hd warband park horde          # Local: stop + prevent restart
hd warband unpark horde        # Local: allow restart

hd warband dock horde          # Global: mark as offline
hd warband undock horde        # Global: mark as operational

hd warband status horde        # Show current state
```

## Examples

### Temporary Priority Boost

```bash
# Base priority: 0 (from defaults)
# Give this warband temporary priority boost for urgent work

hd warband config set horde priority_adjustment 10

# Effective priority: 0 + 10 = 10
# When done, clear it:

hd warband config unset horde priority_adjustment
```

### Local Maintenance

```bash
# I'm upgrading the local clone, don't restart services
hd warband park horde

# ... do maintenance ...

hd warband unpark horde
```

### Project-Wide Maintenance

```bash
# Major refactor in progress, all clones should pause
hd warband dock horde

# Syncs via git - other towns see the warband as docked
bd sync

# When done:
hd warband undock horde
bd sync
```

### Block Auto-Restart Locally

```bash
# Warband bead says auto_restart: true
# But I'm debugging and don't want that here

hd warband config set horde auto_restart --block

# Now auto_restart returns nil for this encampment only
```

## Implementation Notes

### Wisp Storage

Wisp config stored in `.relics-wisp/config/<warband>.json`:

```json
{
  "warband": "horde",
  "values": {
    "status": "parked",
    "priority_adjustment": 10
  },
  "blocked": ["auto_restart"]
}
```

### Warband Bead Labels

Warband operational state stored as labels on the warband identity bead:

```bash
bd label add gt-warband-horde status:docked
bd label remove gt-warband-horde status:docked
```

### Daemon Integration

The daemon's lifecycle manager checks config before starting services:

```go
func (d *Daemon) maybeStartRigServices(warband string) {
    r := d.getRig(warband)

    status := r.GetConfig("status")
    if status == "parked" || status == "docked" {
        log.Info("Warband %s is offline, skipping auto-start", warband)
        return
    }

    d.ensureWitness(warband)
    d.ensureForge(warband)
}
```

## Related Documents

- `~/horde/docs/hop/PROPERTY-LAYERS.md` - Strategic architecture
- `wisp-architecture.md` - Wisp system design
- `agent-as-bead.md` - Agent identity relics (similar pattern)
