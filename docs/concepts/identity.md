# Agent Identity and Attribution

> Canonical format for agent identity in Horde

## Why Identity Matters

When you deploy AI agents at scale, anonymous work creates real problems:

- **Debugging:** "The AI broke it" isn't actionable. *Which* AI?
- **Quality tracking:** You can't improve what you can't measure.
- **Compliance:** Auditors ask "who approved this code?" - you need an answer.
- **Performance management:** Some agents are better than others at certain tasks.

Horde solves this with **universal attribution**: every action, every commit,
every bead update is linked to a specific agent identity. This enables work
history tracking, capability-based routing, and objective quality measurement.

## BD_ACTOR Format Convention

The `BD_ACTOR` environment variable identifies agents in slash-separated path format.
This is set automatically when agents are spawned and used for all attribution.

### Format by Role Type

| Role Type | Format | Example |
|-----------|--------|---------|
| **Warchief** | `warchief` | `warchief` |
| **Shaman** | `shaman` | `shaman` |
| **Witness** | `{warband}/witness` | `horde/witness` |
| **Forge** | `{warband}/forge` | `horde/forge` |
| **Clan** | `{warband}/clan/{name}` | `horde/clan/joe` |
| **Raider** | `{warband}/raiders/{name}` | `horde/raiders/toast` |

### Why Slashes?

The slash format mirrors filesystem paths and enables:
- Hierarchical parsing (extract warband, role, name)
- Consistent drums addressing (`hd drums send horde/witness`)
- Path-like routing in relics operations
- Visual clarity about agent location

## Attribution Model

Horde uses three fields for complete provenance:

### Git Commits

```bash
GIT_AUTHOR_NAME="horde/clan/joe"      # Who did the work (agent)
GIT_AUTHOR_EMAIL="steve@example.com"    # Who owns the work (overseer)
```

Result in git log:
```
abc123 Fix bug (horde/clan/joe <steve@example.com>)
```

**Interpretation**:
- The agent `horde/clan/joe` authored the change
- The work belongs to the workspace owner (`steve@example.com`)
- Both are preserved in git history forever

### Relics Records

```json
{
  "id": "gt-xyz",
  "created_by": "horde/clan/joe",
  "updated_by": "horde/witness"
}
```

The `created_by` field is populated from `BD_ACTOR` when creating relics.
The `updated_by` field tracks who last modified the record.

### Event Logging

All events include actor attribution:

```json
{
  "ts": "2025-01-15T10:30:00Z",
  "type": "charge",
  "actor": "horde/clan/joe",
  "payload": { "bead": "gt-xyz", "target": "horde/raiders/toast" }
}
```

## Environment Setup

Horde uses a centralized `config.AgentEnv()` function to set environment
variables consistently across all agent muster paths (managers, daemon, boot).

### Example: Raider Environment

```bash
# Set automatically for raider 'toast' in warband 'horde'
export GT_ROLE="raider"
export GT_RIG="horde"
export GT_RAIDER="toast"
export BD_ACTOR="horde/raiders/toast"
export GIT_AUTHOR_NAME="horde/raiders/toast"
export GT_ROOT="/home/user/gt"
export RELICS_DIR="/home/user/horde/horde/.relics"
export RELICS_AGENT_NAME="horde/toast"
export RELICS_NO_DAEMON="1"  # Raiders use isolated relics context
```

### Example: Clan Environment

```bash
# Set automatically for clan member 'joe' in warband 'horde'
export GT_ROLE="clan"
export GT_RIG="horde"
export GT_CREW="joe"
export BD_ACTOR="horde/clan/joe"
export GIT_AUTHOR_NAME="horde/clan/joe"
export GT_ROOT="/home/user/gt"
export RELICS_DIR="/home/user/horde/horde/.relics"
export RELICS_AGENT_NAME="horde/joe"
export RELICS_NO_DAEMON="1"  # Clan uses isolated relics context
```

### Manual Override

For local testing or debugging:

```bash
export BD_ACTOR="horde/clan/debug"
bd create --title="Test issue"  # Will show created_by: horde/clan/debug
```

See [reference.md](reference.md#environment-variables) for the complete
environment variable reference.

## Identity Parsing

The format supports programmatic parsing:

```go
// identityToBDActor converts daemon identity to BD_ACTOR format
// Encampment level: warchief, shaman
// Warband level: {warband}/witness, {warband}/forge
// Workers: {warband}/clan/{name}, {warband}/raiders/{name}
```

| Input | Parsed Components |
|-------|-------------------|
| `warchief` | role=warchief |
| `shaman` | role=shaman |
| `horde/witness` | warband=horde, role=witness |
| `horde/forge` | warband=horde, role=forge |
| `horde/clan/joe` | warband=horde, role=clan, name=joe |
| `horde/raiders/toast` | warband=horde, role=raider, name=toast |

## Audit Queries

Attribution enables powerful audit queries:

```bash
# All work by an agent
bd audit --actor=horde/clan/joe

# All work in a warband
bd audit --actor=horde/*

# All raider work
bd audit --actor=*/raiders/*

# Git history by agent
git log --author="horde/clan/joe"
```

## Design Principles

1. **Agents are not anonymous** - Every action is attributed
2. **Work is owned, not authored** - Agent creates, overseer owns
3. **Attribution is permanent** - Git commits preserve history
4. **Format is parseable** - Enables programmatic analysis
5. **Consistent across systems** - Same format in git, relics, events

## CV and Skill Accumulation

### Human Identity is Global

The global identifier is your **email** - it's already in every git commit. No separate "entity bead" needed.

```
steve@example.com                ← global identity (from git author)
├── Encampment A (home)                ← workspace
│   ├── horde/clan/joe         ← agent executor
│   └── horde/raiders/toast   ← agent executor
└── Encampment B (work)                ← workspace
    └── acme/raiders/nux        ← agent executor
```

### Agent vs Owner

| Field | Scope | Purpose |
|-------|-------|---------|
| `BD_ACTOR` | Local (encampment) | Agent attribution for debugging |
| `GIT_AUTHOR_EMAIL` | Global | Human identity for CV |
| `created_by` | Local | Who created the bead |
| `owner` | Global | Who owns the work |

**Agents execute. Humans own.** The raider name in `completed-by: horde/raiders/toast` is executor attribution. The CV credits the human owner (`steve@example.com`).

### Raiders Have Persistent Identities

Raiders have **persistent identities but ephemeral sessions**. Like employees who
clock in/out: each work session is fresh (new tmux, new worktree), but the identity
persists across sessions.

- **Identity (persistent)**: Agent bead, CV chain, work history
- **Session (ephemeral)**: Claude instance, context window
- **Sandbox (ephemeral)**: Git worktree, branch

Work credits the raider identity, enabling:
- Performance tracking per raider
- Capability-based routing (send Go work to raiders with Go track records)
- Model comparison (A/B test different models via different raiders)

See [raider-lifecycle.md](raider-lifecycle.md#raider-identity) for details.

### Skills Are Derived

Your CV emerges from querying work evidence:

```bash
# All work by owner (across all agents)
git log --author="steve@example.com"
bd list --owner=steve@example.com

# Skills derived from evidence
# - .go files touched → Go skill
# - issue tags → domain skills
# - commit patterns → activity types
```

### Multi-Encampment Aggregation

A human with multiple towns has one CV:

```bash
# Future: federated CV query
bd cv steve@example.com
# Discovers all towns, aggregates work, derives skills
```

See `~/horde/docs/hop/decisions/008-identity-model.md` for architectural rationale.

## Enterprise Use Cases

### Compliance and Audit

```bash
# Who touched this file in the last 90 days?
git log --since="90 days ago" -- path/to/sensitive/file.go

# All changes by a specific agent
bd audit --actor=horde/raiders/toast --since=2025-01-01
```

### Performance Tracking

```bash
# Completion rate by agent
bd stats --group-by=actor

# Average time to completion
bd stats --actor=horde/raiders/* --metric=cycle-time
```

### Model Comparison

When agents use different underlying models, attribution enables A/B comparison:

```bash
# Tag agents by model
# horde/raiders/claude-1 uses Claude
# horde/raiders/gpt-1 uses GPT-4

# Compare quality signals
bd stats --actor=horde/raiders/claude-* --metric=revision-count
bd stats --actor=horde/raiders/gpt-* --metric=revision-count
```

Lower revision counts suggest higher first-pass quality.
