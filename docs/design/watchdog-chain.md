# Daemon/Boot/Shaman Watchdog Chain

> Autonomous health monitoring and recovery in Horde.

## Overview

Horde uses a three-tier watchdog chain for autonomous health monitoring:

```
Daemon (Go process)          ← Dumb transport, 3-min heartbeat
    │
    └─► Boot (AI agent)       ← Intelligent triage, fresh each tick
            │
            └─► Shaman (AI agent)  ← Continuous scout, long-running
                    │
                    └─► Witnesses & Refineries  ← Per-warband agents
```

**Key insight**: The daemon is mechanical (can't reason), but health decisions need
intelligence (is the agent stuck or just thinking?). Boot bridges this gap.

## Design Rationale: Why Two Agents?

### The Problem

The daemon needs to ensure the Shaman is healthy, but:

1. **Daemon can't reason** - It's Go code following the ZFC principle (don't reason
   about other agents). It can check "is session alive?" but not "is agent stuck?"

2. **Waking costs context** - Each time you muster an AI agent, you consume context
   tokens. In idle towns, waking Shaman every 3 minutes wastes resources.

3. **Observation requires intelligence** - Distinguishing "agent composing large
   artifact" from "agent hung on tool prompt" requires reasoning.

### The Solution: Boot as Triage

Boot is a narrow, ephemeral AI agent that:
- Runs fresh each daemon tick (no accumulated context debt)
- Makes a single decision: should Shaman wake?
- Exits immediately after deciding

This gives us intelligent triage without the cost of keeping a full AI running.

### Why Not Merge Boot into Shaman?

We could have Shaman handle its own "should I be awake?" logic, but:

1. **Shaman can't observe itself** - A hung Shaman can't detect it's hung
2. **Context accumulation** - Shaman runs continuously; Boot restarts fresh
3. **Cost in idle towns** - Boot only costs tokens when it runs; Shaman costs
   tokens constantly if kept alive

### Why Not Replace with Go Code?

The daemon could directly monitor agents without AI, but:

1. **Can't observe panes** - Go code can't interpret tmux output semantically
2. **Can't distinguish stuck vs working** - No reasoning about agent state
3. **Escalation is complex** - When to notify? When to force-restart? AI handles
   nuanced decisions better than hardcoded thresholds

## Session Ownership

| Agent | Session Name | Location | Lifecycle |
|-------|--------------|----------|-----------|
| Daemon | (Go process) | `~/horde/daemon/` | Persistent, auto-restart |
| Boot | `gt-boot` | `~/horde/shaman/dogs/boot/` | Ephemeral, fresh each tick |
| Shaman | `hq-shaman` | `~/horde/shaman/` | Long-running, handoff loop |

**Critical**: Boot runs in `gt-boot`, NOT `hq-shaman`. This prevents Boot
from conflicting with a running Shaman session.

## Heartbeat Mechanics

### Daemon Heartbeat (3 minutes)

The daemon runs a heartbeat tick every 3 minutes:

```go
func (d *Daemon) heartbeatTick() {
    d.ensureBootRunning()           // 1. Muster Boot for triage
    d.checkShamanHeartbeat()        // 2. Belt-and-suspenders fallback
    d.ensureWitnessesRunning()      // 3. Witness health (checks tmux directly)
    d.ensureRefineriesRunning()     // 4. Forge health (checks tmux directly)
    d.triggerPendingSpawns()        // 5. Bootstrap raiders
    d.processLifecycleRequests()    // 6. Cycle/restart requests
    // Agent state derived from tmux, not recorded in relics (gt-zecmc)
}
```

### Shaman Heartbeat (continuous)

The Shaman updates `~/horde/shaman/heartbeat.json` at the start of each scout cycle:

```json
{
  "timestamp": "2026-01-02T18:30:00Z",
  "cycle": 42,
  "last_action": "health-scan",
  "healthy_agents": 3,
  "unhealthy_agents": 0
}
```

### Heartbeat Freshness

| Age | State | Boot Action |
|-----|-------|-------------|
| < 5 min | Fresh | Nothing (Shaman active) |
| 5-15 min | Stale | Signal if pending drums |
| > 15 min | Very stale | Wake (Shaman may be stuck) |

## Boot Decision Matrix

When Boot runs, it observes:
- Is Shaman session alive?
- How old is Shaman's heartbeat?
- Is there pending drums for Shaman?
- What's in Shaman's tmux pane?

Then decides:

| Condition | Action | Command |
|-----------|--------|---------|
| Session dead | START | Exit; daemon calls `ensureShamanRunning()` |
| Heartbeat > 15 min | WAKE | `hd signal shaman "Boot wake: check your inbox"` |
| Heartbeat 5-15 min + drums | SIGNAL | `hd signal shaman "Boot check-in: pending work"` |
| Heartbeat fresh | NOTHING | Exit silently |

## Handoff Flow

### Shaman Handoff

The Shaman runs continuous scout cycles. After N cycles or high context:

```
End of scout cycle:
    │
    ├─ Squash wisp to digest (ephemeral → permanent)
    ├─ Write summary to totem state
    └─ hd handoff -s "Routine cycle" -m "Details"
        │
        └─ Creates drums for next session
```

Next daemon tick:
```
Daemon → ensureShamanRunning()
    │
    └─ Spawns fresh Shaman in gt-shaman
        │
        └─ SessionStart hook: hd drums check --inject
            │
            └─ Previous handoff drums injected
                │
                └─ Shaman reads and continues
```

### Boot Handoff (Rare)

Boot is ephemeral - it exits after each tick. No persistent handoff needed.

However, Boot uses a marker file to prevent double-spawning:
- Marker: `~/horde/shaman/dogs/boot/.boot-running` (TTL: 5 minutes)
- Status: `~/horde/shaman/dogs/boot/.boot-status.json` (last action/result)

If the marker exists and is recent, daemon skips Boot muster for that tick.

## Degraded Mode

When tmux is unavailable, Horde enters degraded mode:

| Capability | Normal | Degraded |
|------------|--------|----------|
| Boot runs | As AI in tmux | As Go code (mechanical) |
| Observe panes | Yes | No |
| Signal agents | Yes | No |
| Start agents | tmux sessions | Direct muster |

Degraded Boot triage is purely mechanical:
- Session dead → start
- Heartbeat stale → restart
- No reasoning, just thresholds

## Fallback Chain

Multiple layers ensure recovery:

1. **Boot triage** - Intelligent observation, first line
2. **Daemon checkShamanHeartbeat()** - Belt-and-suspenders if Boot fails
3. **Tmux-based discovery** - Daemon checks tmux sessions directly (no bead state)
4. **Human escalation** - Drums to overseer for unrecoverable states

## State Files

| File | Purpose | Updated By |
|------|---------|-----------|
| `shaman/heartbeat.json` | Shaman freshness | Shaman (each cycle) |
| `shaman/dogs/boot/.boot-running` | Boot in-progress marker | Boot muster |
| `shaman/dogs/boot/.boot-status.json` | Boot last action | Boot triage |
| `shaman/health-check-state.json` | Agent health tracking | `hd shaman health-check` |
| `daemon/daemon.log` | Daemon activity | Daemon |
| `daemon/daemon.pid` | Daemon process ID | Daemon startup |

## Debugging

```bash
# Check Shaman heartbeat
cat ~/horde/shaman/heartbeat.json | jq .

# Check Boot status
cat ~/horde/shaman/dogs/boot/.boot-status.json | jq .

# View daemon log
tail -f ~/horde/daemon/daemon.log

# Manual Boot run
gt boot triage

# Manual Shaman health check
gt shaman health-check
```

## Common Issues

### Boot Spawns in Wrong Session

**Symptom**: Boot runs in `hq-shaman` instead of `gt-boot`
**Cause**: Session name confusion in muster code
**Fix**: Ensure `hd boot triage` specifies `--session=gt-boot`

### Zombie Sessions Block Restart

**Symptom**: tmux session exists but Claude is dead
**Cause**: Daemon checks session existence, not process health
**Fix**: Kill zombie sessions before recreating: `hd session kill hq-shaman`

### Status Shows Wrong State

**Symptom**: `hd status` shows wrong state for agents
**Cause**: Previously bead state and tmux state could diverge
**Fix**: As of gt-zecmc, status derives state from tmux directly (no bead state for
observable conditions like running/stopped). Non-observable states (stuck, awaiting-gate)
are still stored in relics.

## Design Decision: Keep Separation

The issue [gt-1847v] considered three options:

### Option A: Keep Boot/Shaman Separation (CHOSEN)

- Boot is ephemeral, spawns fresh each heartbeat
- Boot runs in `gt-boot`, exits after triage
- Shaman runs in `hq-shaman`, continuous scout
- Clear session boundaries, clear lifecycle

**Verdict**: This is the correct design. The implementation needs fixing, not the architecture.

### Option B: Merge Boot into Shaman (Rejected)

- Single `hq-shaman` session handles everything
- Shaman checks "should I be awake?" internally

**Why rejected**:
- Shaman can't observe itself (hung Shaman can't detect hang)
- Context accumulates even when idle (cost in quiet towns)
- No external watchdog means no recovery from Shaman failure

### Option C: Replace with Go Watchdog (Rejected)

- Daemon directly monitors witness/forge
- No Boot, no Shaman AI for health checks
- AI agents only for complex decisions

**Why rejected**:
- Go code can't interpret tmux pane output semantically
- Can't distinguish "stuck" from "thinking deeply"
- Loses the intelligent triage that makes the system resilient
- Escalation decisions are nuanced (when to notify? force-restart?)

### Implementation Fixes Needed

The separation is correct; these bugs need fixing:

1. **Session confusion** (gt-sgzsb): Boot spawns in wrong session
2. **Zombie blocking** (gt-j1i0r): Daemon can't kill zombie sessions
3. ~~**Status mismatch** (gt-doih4): Bead vs tmux state divergence~~ → FIXED in gt-zecmc
4. **Ensure semantics** (gt-ekc5u): Start should kill zombies first

## Summary

The watchdog chain provides autonomous recovery:

- **Daemon**: Mechanical heartbeat, spawns Boot
- **Boot**: Intelligent triage, decides Shaman fate
- **Shaman**: Continuous scout, monitors workers

Boot exists because the daemon can't reason and Shaman can't observe itself.
The separation costs complexity but enables:

1. **Intelligent triage** without constant AI cost
2. **Fresh context** for each triage decision
3. **Graceful degradation** when tmux unavailable
4. **Multiple fallback** layers for reliability
