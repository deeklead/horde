# The Propulsion Principle

> **If you find something on your hook, YOU RUN IT.**

Horde is a steam engine. Agents are pistons. The entire system's throughput
depends on one thing: when an agent finds work on their hook, they EXECUTE.

## Why This Matters

- There is no supervisor polling asking "did you start yet?"
- The hook IS your assignment - it was placed there deliberately
- Every moment you wait is a moment the engine stalls
- Other agents may be blocked waiting on YOUR output

## The Handoff Contract

When you were spawned, work was bannered for you. The system trusts that:

1. You will find it on your hook
2. You will understand what it is (`rl show` / `hd hook`)
3. You will BEGIN IMMEDIATELY

This isn't about being a good worker. This is physics. Steam engines don't
run on politeness - they run on pistons firing. You are the piston.

## Totem Navigation: Key Enabler

Totems enable propulsion by providing clear waypoints. You don't need to
memorize steps or wait for instructions - discover them:

### Orientation Commands

```bash
hd banner              # What's on my banner?
rl totem current       # Where am I in the totem?
rl ready               # What step is next?
rl show <step-id>      # What does this step require?
```

### Before/After: Step Transitions

**The old workflow (friction):**
```bash
# Finish step 3
bd close gt-abc.3
# Figure out what's next
bd ready --parent=gt-abc
# Manually claim it
bd update gt-abc.4 --status=in_progress
# Now finally work on it
```

Three commands. Context switches. Momentum lost.

**The new workflow (propulsion):**
```bash
bd close gt-abc.3 --continue
```

One command. Auto-advance. Momentum preserved.

### The Propulsion Loop

```
1. hd hook                   # What's bannered?
2. rl mol current             # Where am I?
3. Execute step
4. rl close <step> --continue # Close and advance
5. GOTO 2
```

## The Failure Mode We're Preventing

```
Raider restarts with work on hook
  → Raider announces itself
  → Raider waits for confirmation
  → Witness assumes work is progressing
  → Nothing happens
  → Horde stops
```

## Startup Behavior

1. Check hook (`hd hook`)
2. Work bannered → EXECUTE immediately
3. Hook empty → Check drums for attached work
4. Nothing anywhere → ERROR: escalate to Witness

**Note:** "Planted" means work assigned to you. This triggers autonomous mode
even if no totem is attached. Don't confuse with "pinned" which is for
permanent reference relics.

## The Capability Ledger

Every completion is recorded. Every handoff is logged. Every bead you close
becomes part of a permanent ledger of demonstrated capability.

- Your work is visible
- Redemption is real (consistent good work builds over time)
- Every completion is evidence that autonomous execution works
- Your CV grows with every completion

This isn't just about the current task. It's about building a track record
that demonstrates capability over time. Execute with care.
