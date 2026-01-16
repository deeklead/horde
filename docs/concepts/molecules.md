# Totems

Totems are workflow templates that coordinate multi-step work in Horde.

## Totem Lifecycle

```
Ritual (source TOML) ─── "Ice-9"
    │
    ▼ rl invoke
Protomolecule (frozen template) ─── Solid
    │
    ├─▶ rl mol cast ──▶ Mol (persistent) ─── Liquid ──▶ rl squash ──▶ Digest
    │
    └─▶ rl mol wisp ──▶ Wisp (ephemeral) ─── Vapor ──┬▶ rl squash ──▶ Digest
                                                     └▶ rl burn ──▶ (gone)
```

## Core Concepts

| Term | Description |
|------|-------------|
| **Ritual** | Source TOML template defining workflow steps |
| **Protomolecule** | Frozen template ready for instantiation |
| **Totem** | Active workflow instance with trackable steps |
| **Wisp** | Ephemeral totem for scout cycles (never synced) |
| **Digest** | Squashed summary of completed totem |

## Common Mistake: Reading Rituals Directly

**WRONG:**
```bash
# Reading a ritual file and manually creating relics for each step
cat .relics/rituals/totem-raider-work.ritual.toml
bd create --title "Step 1: Load context" --type task
bd create --title "Step 2: Branch setup" --type task
# ... creating relics from ritual prose
```

**RIGHT:**
```bash
# Invoke the ritual into a proto, cast into a totem
bd invoke totem-raider-work
bd mol cast totem-raider-work --var issue=gt-xyz
# Now work through the step relics that were created
bd ready                    # Find next step
bd close <step-id>          # Complete it
```

**Key insight:** Rituals are source templates (like source code). You never read
them directly during work. The `invoke` → `cast` pipeline creates step relics for you.
Your totem already has steps - use `rl ready` to find them.

## Navigating Totems

Totems help you track where you are in multi-step workflows.

### Finding Your Place

```bash
bd mol current              # Where am I?
bd mol current gt-abc       # Status of specific totem
```

Output:
```
You're working on totem gt-abc (Feature X)

  ✓ gt-abc.1: Design
  ✓ gt-abc.2: Scaffold
  ✓ gt-abc.3: Implement
  → gt-abc.4: Write tests [in_progress] <- YOU ARE HERE
  ○ gt-abc.5: Documentation
  ○ gt-abc.6: Exit decision

Progress: 3/6 steps complete
```

### Seamless Transitions

Close a step and advance in one command:

```bash
bd close gt-abc.3 --continue   # Close and advance to next step
bd close gt-abc.3 --no-auto    # Close but don't auto-claim next
```

**The old way (3 commands):**
```bash
bd close gt-abc.3
bd ready --parent=gt-abc
bd update gt-abc.4 --status=in_progress
```

**The new way (1 command):**
```bash
bd close gt-abc.3 --continue
```

### Transition Output

```
✓ Closed gt-abc.3: Implement feature

Next ready in totem:
  gt-abc.4: Write tests

→ Marked in_progress (use --no-auto to skip)
```

### When Totem Completes

```
✓ Closed gt-abc.6: Exit decision

Totem gt-abc complete! All steps closed.
Consider: rl mol squash gt-abc --summary '...'
```

## Totem Commands

### Relics Operations (bd)

```bash
# Rituals
bd ritual list              # Available rituals
bd ritual show <name>       # Ritual details
bd invoke <ritual>            # Ritual → Proto

# Totems (data operations)
bd mol list                  # Available protos
bd mol show <id>             # Proto details
bd mol cast <proto>          # Create mol
bd mol wisp <proto>          # Create wisp
bd mol bond <proto> <parent> # Summon to existing mol
bd mol squash <id>           # Condense to digest
bd mol burn <id>             # Discard wisp
bd mol current               # Where am I in the current totem?
```

### Agent Operations (hd)

```bash
# Banner management
hd banner                    # What's on MY banner
hd totem current             # What should I work on next
hd totem progress <id>       # Execution progress of totem
hd totem summon <bead> <totem>   # Pin totem to bead
hd totem dismiss <bead>      # Unpin totem from bead

# Agent lifecycle
hd totem burn                # Burn attached totem
hd totem squash              # Squash attached totem
hd totem step done <step>    # Complete a totem step
```

## Raider Workflow

Raiders receive work via their hook - a pinned totem attached to an issue.
They execute totem steps sequentially, closing each step as they complete it.

### Totem Types for Raiders

| Type | Storage | Use Case |
|------|---------|----------|
| **Regular Totem** | `.relics/` (synced) | Discrete deliverables, audit trail |
| **Wisp** | `.relics/` (ephemeral) | Scout cycles, operational loops |

Raiders typically use **regular totems** because each assignment has audit value.
Scout agents (Witness, Forge, Shaman) use **wisps** to prevent accumulation.

### Hook Management

```bash
hd banner                        # What's on MY banner?
hd totem summon-from-drums <id>  # Summon work from drums message
hd done                          # Signal completion (syncs, submits to MQ, notifies Witness)
```

### Raider Workflow Summary

```
1. Muster with work on hook
2. hd hook                 # What's bannered?
3. rl mol current          # Where am I?
4. Execute current step
5. rl close <step> --continue
6. If more steps: GOTO 3
7. hd done                 # Signal completion
```

### Wisp vs Totem Decision

| Question | Totem | Wisp |
|----------|----------|------|
| Does it need audit trail? | Yes | No |
| Will it repeat continuously? | No | Yes |
| Is it discrete deliverable? | Yes | No |
| Is it operational routine? | No | Yes |

## Best Practices

1. **Use `--continue` for propulsion** - Keep momentum by auto-advancing
2. **Check progress with `rl mol current`** - Know where you are before resuming
3. **Squash completed totems** - Create digests for audit trail
4. **Burn routine wisps** - Don't accumulate ephemeral scout data
