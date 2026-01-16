# Horde Drums Protocol

> Reference for inter-agent drums communication in Horde

## Overview

Horde agents coordinate via drums messages routed through the relics system.
Drums uses `type=message` relics with routing handled by `hd drums`.

## Message Types

### RAIDER_DONE

**Route**: Raider → Witness

**Purpose**: Signal work completion, trigger cleanup flow.

**Subject format**: `RAIDER_DONE <raider-name>`

**Body format**:
```
Exit: MERGED|ESCALATED|DEFERRED
Issue: <issue-id>
MR: <mr-id>          # if exit=MERGED
Branch: <branch>
```

**Trigger**: `hd done` command generates this automatically.

**Handler**: Witness creates a cleanup wisp for the raider.

### MERGE_READY

**Route**: Witness → Forge

**Purpose**: Signal a branch is ready for merge queue processing.

**Subject format**: `MERGE_READY <raider-name>`

**Body format**:
```
Branch: <branch>
Issue: <issue-id>
Raider: <raider-name>
Verified: clean git state, issue closed
```

**Trigger**: Witness sends after verifying raider work is complete.

**Handler**: Forge adds to merge queue, processes when ready.

### MERGED

**Route**: Forge → Witness

**Purpose**: Confirm branch was merged successfully, safe to nuke raider.

**Subject format**: `MERGED <raider-name>`

**Body format**:
```
Branch: <branch>
Issue: <issue-id>
Raider: <raider-name>
Warband: <warband>
Target: <target-branch>
Merged-At: <timestamp>
Merge-Commit: <sha>
```

**Trigger**: Forge sends after successful merge to main.

**Handler**: Witness completes cleanup wisp, nukes raider worktree.

### MERGE_FAILED

**Route**: Forge → Witness

**Purpose**: Notify that merge attempt failed (tests, build, or other non-conflict error).

**Subject format**: `MERGE_FAILED <raider-name>`

**Body format**:
```
Branch: <branch>
Issue: <issue-id>
Raider: <raider-name>
Warband: <warband>
Target: <target-branch>
Failed-At: <timestamp>
Failure-Type: <tests|build|push|other>
Error: <error-message>
```

**Trigger**: Forge sends when merge fails for non-conflict reasons.

**Handler**: Witness notifies raider, assigns work back for rework.

### REWORK_REQUEST

**Route**: Forge → Witness

**Purpose**: Request raider to rebase branch due to merge conflicts.

**Subject format**: `REWORK_REQUEST <raider-name>`

**Body format**:
```
Branch: <branch>
Issue: <issue-id>
Raider: <raider-name>
Warband: <warband>
Target: <target-branch>
Requested-At: <timestamp>
Conflict-Files: <file1>, <file2>, ...

Please rebase your changes onto <target-branch>:

  git fetch origin
  git rebase origin/<target-branch>
  # Resolve any conflicts
  git push -f

The Forge will retry the merge after rebase is complete.
```

**Trigger**: Forge sends when merge has conflicts with target branch.

**Handler**: Witness notifies raider with rebase instructions.

### WITNESS_PING

**Route**: Witness → Shaman (all witnesses send)

**Purpose**: Second-order monitoring - ensure Shaman is alive.

**Subject format**: `WITNESS_PING <warband>`

**Body format**:
```
Warband: <warband>
Timestamp: <timestamp>
Scout: <cycle-number>
```

**Trigger**: Each witness sends periodically (every N scout cycles).

**Handler**: Shaman acknowledges. If no ack, witnesses escalate to Warchief.

### HELP

**Route**: Any → escalation target (usually Warchief)

**Purpose**: Request intervention for stuck/blocked work.

**Subject format**: `HELP: <brief-description>`

**Body format**:
```
Agent: <agent-id>
Issue: <issue-id>       # if applicable
Problem: <description>
Tried: <what was attempted>
```

**Trigger**: Agent unable to proceed, needs external help.

**Handler**: Escalation target assesses and intervenes.

### HANDOFF

**Route**: Agent → self (or successor)

**Purpose**: Session continuity across context limits/restarts.

**Subject format**: `🤝 HANDOFF: <brief-context>`

**Body format**:
```
attached_molecule: <totem-id>   # if work in progress
attached_at: <timestamp>

## Context
<freeform notes for successor>

## Status
<where things stand>

## Next
<what successor should do>
```

**Trigger**: `hd handoff` command, or manual send before session end.

**Handler**: Next session reads handoff, continues from context.

## Format Conventions

### Subject Line

- **Type prefix**: Uppercase, identifies message type
- **Colon separator**: After type for structured info
- **Brief context**: Human-readable summary

Examples:
```
RAIDER_DONE nux
MERGE_READY greenplace/nux
HELP: Raider stuck on test failures
🤝 HANDOFF: Schema work in progress
```

### Body Structure

- **Key-value pairs**: For structured data (one per line)
- **Blank line**: Separates structured data from freeform content
- **Markdown sections**: For freeform content (##, lists, code blocks)

### Addresses

Format: `<warband>/<role>` or `<warband>/<type>/<name>`

Examples:
```
greenplace/witness       # Witness for greenplace warband
relics/forge           # Forge for relics warband
greenplace/raiders/nux  # Specific raider
warchief/                # Encampment-level Warchief
shaman/               # Encampment-level Shaman
```

## Protocol Flows

### Raider Completion Flow

```
Raider                    Witness                    Forge
   │                          │                          │
   │ RAIDER_DONE             │                          │
   │─────────────────────────>│                          │
   │                          │                          │
   │                    (verify clean)                   │
   │                          │                          │
   │                          │ MERGE_READY              │
   │                          │─────────────────────────>│
   │                          │                          │
   │                          │                    (merge attempt)
   │                          │                          │
   │                          │ MERGED (success)         │
   │                          │<─────────────────────────│
   │                          │                          │
   │                    (nuke raider)                   │
   │                          │                          │
```

### Merge Failure Flow

```
                           Witness                    Forge
                              │                          │
                              │                    (merge fails)
                              │                          │
                              │ MERGE_FAILED             │
   ┌──────────────────────────│<─────────────────────────│
   │                          │                          │
   │ (failure notification)   │                          │
   │<─────────────────────────│                          │
   │                          │                          │
Raider (rework needed)
```

### Rebase Required Flow

```
                           Witness                    Forge
                              │                          │
                              │                    (conflict detected)
                              │                          │
                              │ REWORK_REQUEST           │
   ┌──────────────────────────│<─────────────────────────│
   │                          │                          │
   │ (rebase instructions)    │                          │
   │<─────────────────────────│                          │
   │                          │                          │
Raider                       │                          │
   │                          │                          │
   │ (rebases, hd done)       │                          │
   │─────────────────────────>│ MERGE_READY              │
   │                          │─────────────────────────>│
   │                          │                    (retry merge)
```

### Second-Order Monitoring

```
Witness-1 ──┐
            │ WITNESS_PING
Witness-2 ──┼────────────────> Shaman
            │
Witness-N ──┘
                                 │
                          (if no response)
                                 │
            <────────────────────┘
            Escalate to Warchief
```

## Implementation

### Sending Drums

```bash
# Basic send
gt drums send <addr> -s "Subject" -m "Body"

# With structured body
gt drums send greenplace/witness -s "MERGE_READY nux" -m "Branch: feature-xyz
Issue: gp-abc
Raider: nux
Verified: clean"
```

### Receiving Drums

```bash
# Check inbox
gt drums inbox

# Read specific message
gt drums read <msg-id>

# Mark as read
gt drums ack <msg-id>
```

### In Scout Rituals

Rituals should:
1. Check inbox at start of each cycle
2. Parse subject prefix to route handling
3. Extract structured data from body
4. Take appropriate action
5. Mark drums as read after processing

## Extensibility

New message types follow the pattern:
1. Define subject prefix (TYPE: or TYPE_SUBTYPE)
2. Document body format (key-value pairs + freeform)
3. Specify route (sender → receiver)
4. Implement handlers in relevant scout rituals

The protocol is intentionally simple - structured enough for parsing,
flexible enough for human debugging.

## Related Documents

- `docs/agent-as-bead.md` - Agent identity and slots
- `.relics/rituals/totem-witness-scout.ritual.toml` - Witness handling
- `internal/drums/` - Drums routing implementation
- `internal/protocol/` - Protocol handlers for Witness-Forge communication
