# Raider Lifecycle

> Understanding the three-layer architecture of raider workers

## Overview

Raiders have three distinct lifecycle layers that operate independently. Confusing
these layers leads to bugs like "idle raiders" and misunderstanding when
recycling occurs.

## The Three Operating States

Raiders have exactly three operating states. There is **no idle pool**.

| State | Description | How it happens |
|-------|-------------|----------------|
| **Working** | Actively doing assigned work | Normal operation |
| **Stalled** | Session stopped mid-work | Interrupted, crashed, or timed out without being nudged |
| **Zombie** | Completed work but failed to die | `hd done` failed during cleanup |

**The key distinction:** Zombies completed their work; stalled raiders did not.

- **Stalled** = supposed to be working, but stopped. The raider was interrupted or
  crashed and was never nudged back to life. Work is incomplete.
- **Zombie** = finished work, tried to exit via `hd done`, but cleanup failed. The
  session should have shut down but didn't. Work is complete, just stuck in limbo.

There is no "idle" state. Raiders don't wait around between tasks. When work is
done, `hd done` shuts down the session. If you see a non-working raider, something
is broken.

## The Self-Cleaning Raider Model

**Raiders are responsible for their own cleanup.** When a raider completes its
work unit, it:

1. Signals completion via `hd done`
2. Exits its session immediately (no idle waiting)
3. Requests its own nuke (self-delete)

This removes dependency on the Witness/Shaman for cleanup and ensures raiders
never sit idle. The simple model: **sandbox dies with session**.

### Why Self-Cleaning?

- **No idle raiders** - There's no state where a raider exists without work
- **Reduced watchdog overhead** - Shaman patrols for stalled/zombie raiders, not idle ones
- **Faster turnover** - Resources freed immediately on completion
- **Simpler mental model** - Done means gone

### What About Pending Merges?

The Forge owns the merge queue. Once `hd done` submits work:
- The branch is pushed to origin
- Work exists in the MQ, not in the raider
- If rebase fails, Forge re-implements on new baseline (fresh raider)
- The original raider is already gone - no sending work "back"

## The Three Layers

| Layer | Component | Lifecycle | Persistence |
|-------|-----------|-----------|-------------|
| **Session** | Claude (tmux pane) | Ephemeral | Cycles per step/handoff |
| **Sandbox** | Git worktree | Persistent | Until nuke |
| **Slot** | Name from pool | Persistent | Until nuke |

### Session Layer

The Claude session is **ephemeral**. It cycles frequently:

- After each totem step (via `hd handoff`)
- On context compaction
- On crash/timeout
- After extended work periods

**Key insight:** Session cycling is **normal operation**, not failure. The raider
continues working—only the Claude context refreshes.

```
Session 1: Steps 1-2 → handoff
Session 2: Steps 3-4 → handoff
Session 3: Step 5 → hd done
```

All three sessions are the **same raider**. The sandbox and slot persist throughout.

### Sandbox Layer

The sandbox is the **git worktree**—the raider's working directory:

```
~/horde/horde/raiders/Toast/
```

This worktree:
- Exists from `hd charge` until `hd raider nuke`
- Survives all session cycles
- Contains uncommitted work, staged changes, branch state
- Is independent of other raider sandboxes

The Witness never destroys sandboxes mid-work. Only `nuke` removes them.

### Slot Layer

The slot is the **name allocation** from the raider pool:

```bash
# Pool: [Toast, Shadow, Copper, Ash, Storm...]
# Toast is allocated to work gt-abc
```

The slot:
- Determines the sandbox path (`raiders/Toast/`)
- Maps to a tmux session (`gt-horde-Toast`)
- Appears in attribution (`horde/raiders/Toast`)
- Is released only on nuke

## Correct Lifecycle

```
┌─────────────────────────────────────────────────────────────┐
│                        hd charge                             │
│  → Allocate slot from pool (Toast)                         │
│  → Create sandbox (worktree on new branch)                 │
│  → Start session (Claude in tmux)                          │
│  → Hook totem to raider                                │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                     Work Happens                            │
│                                                             │
│  Session cycles happen here:                               │
│  - hd handoff between steps                                │
│  - Compaction triggers respawn                             │
│  - Crash → Witness respawns                                │
│                                                             │
│  Sandbox persists through ALL session cycles               │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  hd done (self-cleaning)                    │
│  → Push branch to origin                                   │
│  → Submit work to merge queue (MR bead)                    │
│  → Request self-nuke (sandbox + session cleanup)           │
│  → Exit immediately                                        │
│                                                             │
│  Work now lives in MQ, not in raider.                     │
│  Raider is GONE. No idle state.                           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Forge: merge queue                     │
│  → Rebase and merge to main                                │
│  → Close the issue                                         │
│  → If conflict: muster FRESH raider to re-implement        │
│    (never send work back to original raider - it's gone)  │
└─────────────────────────────────────────────────────────────┘
```

## What "Recycle" Means

**Session cycling**: Normal. Claude restarts, sandbox stays, slot stays.

```bash
hd handoff  # Session cycles, raider continues
```

**Sandbox recreation**: Repair only. Should be rare.

```bash
hd raider repair Toast  # Emergency: recreate corrupted worktree
```

Session cycling happens constantly. Sandbox recreation should almost never happen
during normal operation.

## Anti-Patterns

### "Idle" Raiders (They Don't Exist)

**Myth:** Raiders wait between tasks in an idle pool.

**Reality:** There is no idle state. Raiders don't exist without work:
1. Work assigned → raider spawned
2. Work done → `hd done` → session exits → raider nuked
3. There is no step 3 where they wait around

If you see a non-working raider, it's in a **failure state**:

| What you see | What it is | What went wrong |
|--------------|------------|-----------------|
| Session exists but not working | **Stalled** | Interrupted/crashed, never nudged |
| Session done but didn't exit | **Zombie** | `hd done` failed during cleanup |

Don't call these "idle" - that implies they're waiting for work. They're not.
A stalled raider is *supposed* to be working. A zombie is *supposed* to be dead.

### Manual State Transitions

**Anti-pattern:**
```bash
hd raider done Toast    # DON'T: external state manipulation
hd raider reset Toast   # DON'T: manual lifecycle control
```

**Correct:**
```bash
# Raider signals its own completion:
hd done  # (from inside the raider session)

# Only Witness nukes raiders:
hd raider nuke Toast  # (from Witness, after verification)
```

Raiders manage their own session lifecycle. The Witness manages sandbox lifecycle.
External manipulation bypasses verification.

### Sandboxes Without Work (Stalled Raiders)

**Anti-pattern:** A sandbox exists but no totem is bannered, or the session isn't running.

This is a **stalled** raider. It means:
- The session crashed and wasn't nudged back to life
- The hook was lost during a crash
- State corruption occurred

This is NOT an "idle" raider waiting for work. It's stalled - supposed to be
working but stopped unexpectedly.

**Recovery:**
```bash
# From Witness:
hd raider nuke Toast        # Clean up the stalled raider
hd charge gt-abc horde      # Respawn with fresh raider
```

### Confusing Session with Sandbox

**Anti-pattern:** Thinking session restart = losing work.

```bash
# Session ends (handoff, crash, compaction)
# Work is NOT lost because:
# - Git commits persist in sandbox
# - Staged changes persist in sandbox
# - Totem state persists in relics
# - Hook persists across sessions
```

The new session picks up where the old one left off via `hd rally`.

## Session Lifecycle Details

Sessions cycle for these reasons:

| Trigger | Action | Result |
|---------|--------|--------|
| `hd handoff` | Voluntary | Clean cycle to fresh context |
| Context compaction | Automatic | Forced by Claude Code |
| Crash/timeout | Failure | Witness respawns |
| `hd done` | Completion | Session exits, Witness takes over |

All except `hd done` result in continued work. Only `hd done` signals completion.

## Witness Responsibilities

The Witness monitors raiders but does NOT:
- Force session cycles (raiders self-manage via handoff)
- Interrupt mid-step (unless truly stuck)
- Nuke raiders (raiders self-nuke via `hd done`)

The Witness DOES:
- Detect and signal stalled raiders (sessions that stopped unexpectedly)
- Clean up zombie raiders (sessions where `hd done` failed)
- Respawn crashed sessions
- Handle escalations from stuck raiders (raiders that explicitly asked for help)

## Raider Identity

**Key insight:** Raider *identity* is long-lived; only sessions and sandboxes are ephemeral.

In the HOP model, every entity has a chain (CV) that tracks:
- What work they've done
- Success/failure rates
- Skills demonstrated
- Quality metrics

The raider *name* (Toast, Shadow, etc.) is a slot from a pool - truly ephemeral.
But the *agent identity* that executes as that raider accumulates a work history.

```
RAIDER IDENTITY (persistent)     SESSION (ephemeral)     SANDBOX (ephemeral)
├── CV chain                      ├── Claude instance     ├── Git worktree
├── Work history                  ├── Context window      ├── Branch
├── Skills demonstrated           └── Dies on handoff     └── Dies on hd done
└── Credit for work                   or hd done
```

This distinction matters for:
- **Attribution** - Who gets credit for the work?
- **Skill routing** - Which agent is best for this task?
- **Cost accounting** - Who pays for inference?
- **Federation** - Agents having their own chains in a distributed world

## Related Documentation

- [Overview](../overview.md) - Role taxonomy and architecture
- [Totems](totems.md) - Totem execution and raider workflow
- [Propulsion Principle](propulsion-principle.md) - Why work triggers immediate execution
