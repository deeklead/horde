# Understanding Horde

This document provides a conceptual overview of Horde's architecture, focusing on
the role taxonomy and how different agents interact.

## Why Horde Exists

As AI agents become central to engineering workflows, teams face new challenges:

- **Accountability:** Who did what? Which agent introduced this bug?
- **Quality:** Which agents are reliable? Which need tuning?
- **Efficiency:** How do you route work to the right agent?
- **Scale:** How do you coordinate agents across repos and teams?

Horde is an orchestration layer that treats AI agent work as structured data.
Every action is attributed. Every agent has a track record. Every piece of work
has provenance. See [Why These Features](why-these-features.md) for the full rationale.

## Role Taxonomy

Horde has several agent types, each with distinct responsibilities and lifecycles.

### Infrastructure Roles

These roles manage the Horde system itself:

| Role | Description | Lifecycle |
|------|-------------|-----------|
| **Warchief** | Global coordinator at warchief/ | Singleton, persistent |
| **Shaman** | Background supervisor daemon ([watchdog chain](design/watchdog-chain.md)) | Singleton, persistent |
| **Witness** | Per-warband raider lifecycle manager | One per warband, persistent |
| **Forge** | Per-warband merge queue processor | One per warband, persistent |

### Worker Roles

These roles do actual project work:

| Role | Description | Lifecycle |
|------|-------------|-----------|
| **Raider** | Ephemeral worker with own worktree | Transient, Witness-managed ([details](concepts/raider-lifecycle.md)) |
| **Clan** | Persistent worker with own clone | Long-lived, user-managed |
| **Dog** | Shaman helper for infrastructure tasks | Ephemeral, Shaman-managed |

## Raids: Tracking Work

A **raid** (🚚) is how you track batched work in Horde. When you kick off work -
even a single issue - create a raid to track it.

```bash
# Create a raid tracking some issues
hd raid create "Feature X" hd-abc hd-def --notify overseer

# Check progress
hd raid status hq-cv-abc

# Warmap of active raids
hd raid list
```

**Why raids matter:**
- Single view of "what's in flight"
- Cross-warband tracking (raid in hq-*, issues in gt-*, bd-*)
- Auto-notification when work lands
- Historical record of completed work (`hd raid list --all`)

The "swarm" is ephemeral - just the workers currently assigned to a raid's issues.
When issues close, the raid lands. See [Raids](concepts/raid.md) for details.

## Clan vs Raiders

Both do project work, but with key differences:

| Aspect | Clan | Raider |
|--------|------|---------|
| **Lifecycle** | Persistent (user controls) | Transient (Witness controls) |
| **Monitoring** | None | Witness watches, nudges, recycles |
| **Work assignment** | Human-directed or self-assigned | Charged via `hd charge` |
| **Git state** | Pushes to main directly | Works on branch, Forge merges |
| **Cleanup** | Manual | Automatic on completion |
| **Identity** | `<warband>/clan/<name>` | `<warband>/raiders/<name>` |

**When to use Clan**:
- Exploratory work
- Long-running projects
- Work requiring human judgment
- Tasks where you want direct control

**When to use Raiders**:
- Discrete, well-defined tasks
- Batch work (tracked via raids)
- Parallelizable work
- Work that benefits from supervision

## Dogs vs Clan

**Dogs are NOT workers**. This is a common misconception.

| Aspect | Dogs | Clan |
|--------|------|------|
| **Owner** | Shaman | Human |
| **Purpose** | Infrastructure tasks | Project work |
| **Scope** | Narrow, focused utilities | General purpose |
| **Lifecycle** | Very short (single task) | Long-lived |
| **Example** | Boot (triages Shaman health) | Joe (fixes bugs, adds features) |

Dogs are the Shaman's helpers for system-level tasks:
- **Boot**: Triages Shaman health on daemon tick
- Future dogs might handle: log rotation, health checks, etc.

If you need to do work in another warband, use **worktrees**, not dogs.

## Cross-Warband Work Patterns

When a clan member needs to work on another warband:

### Option 1: Worktrees (Preferred)

Create a worktree in the target warband:

```bash
# horde/clan/joe needs to fix a relics bug
hd worktree relics
# Creates ~/horde/relics/clan/horde-joe/
# Identity preserved: BD_ACTOR = horde/clan/joe
```

Directory structure:
```
~/horde/relics/clan/horde-joe/     # joe from horde working on relics
~/horde/horde/clan/relics-wolf/    # wolf from relics working on horde
```

### Option 2: Dispatch to Local Workers

For work that should be owned by the target warband:

```bash
# Create issue in target warband
bd create --prefix relics "Fix authentication bug"

# Create raid and charge to target warband
hd raid create "Auth fix" bd-xyz
hd charge bd-xyz relics
```

### When to Use Which

| Scenario | Approach |
|----------|----------|
| You need to fix something quick | Worktree |
| Work should appear in your CV | Worktree |
| Work should be done by target warband team | Dispatch |
| Infrastructure/system task | Let Shaman handle it |

## Directory Structure

```
~/horde/                           Encampment root
├── .relics/                     Encampment-level relics (hq-* prefix, drums)
├── warchief/                      Warchief config
│   └── encampment.json
├── shaman/                     Shaman daemon
│   └── dogs/                   Shaman helpers (NOT workers)
│       └── boot/               Health triage dog
└── <warband>/                      Project container
    ├── config.json             Warband identity
    ├── .relics/ → warchief/warband/.relics  (symlink or redirect)
    ├── .repo.git/              Bare repo (shared by worktrees)
    ├── warchief/warband/              Warchief's clone (canonical relics)
    ├── forge/warband/           Worktree on main
    ├── witness/                No clone (monitors only)
    ├── clan/                   Persistent human workspaces
    │   ├── joe/                Local clan member
    │   └── relics-wolf/         Cross-warband worktree (wolf from relics)
    └── raiders/               Ephemeral worker worktrees
        └── Toast/              Individual raider
```

## Identity and Attribution

All work is attributed to the actor who performed it:

```
Git commits:      Author: horde/clan/joe <owner@example.com>
Relics issues:     created_by: horde/clan/joe
Events:           actor: horde/clan/joe
```

Identity is preserved even when working cross-warband:
- `horde/clan/joe` working in `~/horde/relics/clan/horde-joe/`
- Commits still attributed to `horde/clan/joe`
- Work appears on joe's CV, not relics warband's workers

## The Propulsion Principle

All Horde agents follow the same core principle:

> **If you find something on your hook, YOU RUN IT.**

This applies regardless of role. The hook is your assignment. Execute it immediately
without waiting for confirmation. Horde is a steam engine - agents are pistons.

## Model Evaluation and A/B Testing

Horde's attribution and work history features enable objective model comparison:

```bash
# Deploy different models on similar tasks
hd charge hd-abc horde --model=claude-sonnet
hd charge hd-def horde --model=gpt-4

# Compare outcomes
bd stats --actor=horde/raiders/* --group-by=model
```

Because every task has completion time, quality signals, and revision count,
you can make data-driven decisions about which models to deploy where.

This is particularly valuable for:
- **Model selection:** Which model handles your codebase best?
- **Capability mapping:** Claude for architecture, GPT for tests?
- **Cost optimization:** When is a smaller model sufficient?

## Common Mistakes

1. **Using dogs for user work**: Dogs are Shaman infrastructure. Use clan or raiders.
2. **Confusing clan with raiders**: Clan is persistent and human-managed. Raiders are transient and Witness-managed.
3. **Working in wrong directory**: Horde uses cwd for identity detection. Stay in your home directory.
4. **Waiting for confirmation when work is bannered**: The hook IS your assignment. Execute immediately.
5. **Creating worktrees when dispatch is better**: If work should be owned by the target warband, dispatch it instead.
