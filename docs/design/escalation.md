# Horde Escalation Protocol

> Reference for escalation paths in Horde

## Overview

Horde agents can escalate issues when automated resolution isn't possible.
This document covers:

- Severity levels and routing
- Escalation categories for structured communication
- Tiered escalation (Shaman -> Warchief -> Overseer)
- Decision patterns for async resolution
- Integration with gates and scout lifecycles

## Severity Levels

| Level | Priority | Description | Examples |
|-------|----------|-------------|----------|
| **CRITICAL** | P0 (urgent) | System-threatening, immediate attention | Data corruption, security breach, system down |
| **HIGH** | P1 (high) | Important blocker, needs human soon | Unresolvable merge conflict, critical bug, ambiguous spec |
| **MEDIUM** | P2 (normal) | Standard escalation, human at convenience | Design decision needed, unclear requirements |

## Escalation Categories

Categories provide structured routing based on the nature of the escalation:

| Category | Description | Default Route |
|----------|-------------|---------------|
| `decision` | Multiple valid paths, need choice | Shaman -> Warchief |
| `help` | Need guidance or expertise | Shaman -> Warchief |
| `blocked` | Waiting on unresolvable dependency | Warchief |
| `failed` | Unexpected error, can't proceed | Shaman |
| `emergency` | Security or data integrity issue | Overseer (direct) |
| `gate_timeout` | Gate didn't resolve in time | Shaman |
| `lifecycle` | Worker stuck or needs recycle | Witness |

## Escalation Command

### Basic Usage (unchanged)

```bash
# Basic escalation (default: MEDIUM severity)
gt escalate "Database migration failed"

# Critical escalation - immediate attention
gt escalate -s CRITICAL "Data corruption detected in user table"

# High priority escalation
gt escalate -s HIGH "Merge conflict cannot be resolved automatically"

# With additional details
gt escalate -s MEDIUM "Need clarification on API design" -m "Details..."
```

### Category-Based Escalation

```bash
# Decision needed - routes to Shaman first
gt escalate --type decision "Which auth approach?"

# Help request
gt escalate --type help "Need architecture guidance"

# Blocked on dependency
gt escalate --type blocked "Waiting on bd-xyz"

# Failure that can't be recovered
gt escalate --type failed "Tests failing unexpectedly"

# Emergency - direct to Overseer
gt escalate --type emergency "Security vulnerability found"
```

### Tiered Routing

```bash
# Explicit routing to specific tier
gt escalate --to shaman "Infra issue"
gt escalate --to warchief "Cross-warband coordination needed"
gt escalate --to overseer "Human judgment required"

# Forward from one tier to next
gt escalate --forward --to warchief "Shaman couldn't resolve"
```

### Structured Decisions

For decisions requiring explicit choices:

```bash
gt escalate --type decision \
  --question "Which authentication approach?" \
  --options "JWT tokens,Session cookies,OAuth2" \
  --context "Admin panel needs login" \
  --issue bd-xyz
```

This updates the issue with a structured decision format (see below).

## What Happens on Escalation

1. **Bead created/updated**: Escalation bead (tagged `escalation`) created or updated
2. **Drums sent**: Routed to appropriate tier (Shaman, Warchief, or Overseer)
3. **Activity logged**: Event logged to activity feed
4. **Issue updated**: For decision type, issue gets structured format

## Tiered Escalation Flow

```
Worker encounters issue
    |
    v
gt escalate --type <category> [--to <tier>]
    |
    v
[Shaman receives] (default for most categories)
    |
    +-- Can resolve? --> Updates issue, re-charges work
    |
    +-- Cannot resolve? --> hd escalate --forward --to warchief
                                |
                                v
                           [Warchief receives]
                                |
                                +-- Can resolve? --> Updates issue, re-charges
                                |
                                +-- Cannot resolve? --> hd escalate --forward --to overseer
                                                            |
                                                            v
                                                       [Overseer resolves]
```

Each tier can resolve OR forward. The escalation chain is tracked via comments.

## Decision Pattern

When `--type decision` is used, the issue is updated with structured format:

```markdown
## Decision Needed

**Question:** Which authentication approach?

| Option | Description |
|--------|-------------|
| A | JWT tokens |
| B | Session cookies |
| C | OAuth2 |

**Context:** Admin panel needs login

**Escalated by:** relics/raiders/obsidian
**Escalated at:** 2026-01-01T15:00:00Z

**To resolve:**
1. Comment with chosen option (e.g., "Decision: A")
2. Reassign to work queue or original worker
```

The issue becomes the async communication channel. Resolution updates the issue
and can trigger re-charging to the original worker.

## Integration Points

### Gate Timeouts

When timer gates expire (see bd-7zka.2), Witness escalates:

```go
if gate.Expired() {
    exec.Command("hd", "escalate",
        "--type", "gate_timeout",
        "--severity", "HIGH",
        "--issue", gate.BlockedIssueID,
        fmt.Sprintf("Gate %s timed out after %s", gate.ID, gate.Timeout)).Run()
}
```

### Witness Scout

Witness formalizes stuck-raider detection as escalation:

```go
exec.Command("hd", "escalate",
    "--type", "lifecycle",
    "--to", "warchief",
    "--issue", raider.CurrentIssue,
    fmt.Sprintf("Raider %s stuck: no progress for %d minutes", raider.ID, minutes)).Run()
```

### Forge

On merge failures that can't be auto-resolved:

```go
exec.Command("hd", "escalate",
    "--type", "failed",
    "--issue", mr.IssueID,
    "Merge failed: "+reason).Run()
```

## Raider Exit with Escalation

When a raider needs a decision to continue:

```bash
# 1. Update issue with decision structure
bd update $ISSUE --notes "$(cat <<EOF
## Decision Needed

**Question:** Which approach for caching?

| Option | Description |
|--------|-------------|
| A | Redis (external dependency) |
| B | In-memory (simpler, no persistence) |
| C | SQLite (local persistence) |

**Context:** API response times are slow, need caching layer.
EOF
)"

# 2. Escalate
hd escalate --type decision --issue $ISSUE "Caching approach needs decision"

# 3. Exit cleanly
hd done --status ESCALATED
```

## Warchief Startup Check

On `hd rally`, Warchief checks for pending escalations:

```
## PENDING ESCALATIONS

There are 3 escalation(s) awaiting attention:

  CRITICAL: 1
  HIGH: 1
  MEDIUM: 1

  [CRITICAL] Data corruption detected (gt-abc)
  [HIGH] Merge conflict in auth module (gt-def)
  [MEDIUM] API design clarification needed (gt-ghi)

**Action required:** Review escalations with `rl list --tag=escalation`
Close resolved ones with `rl close <id> --reason "resolution"`
```

## When to Escalate

### Agents SHOULD escalate when:

- **System errors**: Database corruption, disk full, network failures
- **Security issues**: Unauthorized access attempts, credential exposure
- **Unresolvable conflicts**: Merge conflicts that can't be auto-resolved
- **Ambiguous requirements**: Spec is unclear, multiple valid interpretations
- **Design decisions**: Architectural choices that need human judgment
- **Stuck loops**: Agent is stuck and can't make progress
- **Gate timeouts**: Async conditions didn't resolve in expected time

### Agents should NOT escalate for:

- **Normal workflow**: Regular work that can proceed without human input
- **Recoverable errors**: Transient failures that will auto-retry
- **Information queries**: Questions that can be answered from context

## Viewing Escalations

```bash
# List all open escalations
bd list --status=open --tag=escalation

# Filter by category
bd list --tag=escalation --tag=decision

# View specific escalation
bd show <escalation-id>

# Close resolved escalation
bd close <id> --reason "Resolved by fixing X"
```

## Implementation Phases

### Phase 1: Extend hd escalate
- Add `--type` flag for categories
- Add `--to` flag for routing (shaman, warchief, overseer)
- Add `--forward` flag for tier forwarding
- Backward compatible with existing usage

### Phase 2: Decision Pattern
- Add `--question`, `--options`, `--context` flags
- Auto-update issue with decision structure
- Parse decision from issue comments on resolution

### Phase 3: Gate Integration
- Add `gate_timeout` escalation type
- Witness checks timer gates, escalates on timeout
- Forge checks GH gates, escalates on timeout/failure

### Phase 4: Scout Integration
- Formalize Witness stuck-raider as escalation
- Formalize Forge merge-failure as escalation
- Unified escalation handling in Warchief

## References

- bd-7zka.2: Gate evaluation (uses escalation for timeouts)
- bd-0sgd: Design issue for this extended escalation system
