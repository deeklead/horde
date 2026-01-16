# Horde Reference

Technical reference for Horde internals. Read the README first.

## Directory Structure

```
~/horde/                           Encampment root
├── .relics/                     Encampment-level relics (hq-* prefix)
├── warchief/                      Warchief agent home (encampment coordinator)
│   ├── encampment.json               Encampment configuration
│   ├── CLAUDE.md               Warchief context (on disk)
│   └── .claude/settings.json   Warchief Claude settings
├── shaman/                     Shaman agent home (background supervisor)
│   └── .claude/settings.json   Shaman settings (context via hd rally)
└── <warband>/                      Project container (NOT a git clone)
    ├── config.json             Warband identity
    ├── .relics/ → warchief/warband/.relics
    ├── .repo.git/              Bare repo (shared by worktrees)
    ├── warchief/warband/              Warchief's clone (canonical relics)
    │   └── CLAUDE.md           Per-warband warchief context (on disk)
    ├── witness/                Witness agent home (monitors only)
    │   └── .claude/settings.json  (context via hd rally)
    ├── forge/               Forge settings parent
    │   ├── .claude/settings.json
    │   └── warband/                Worktree on main
    │       └── CLAUDE.md       Forge context (on disk)
    ├── clan/                   Clan settings parent (shared)
    │   ├── .claude/settings.json  (context via hd rally)
    │   └── <name>/warband/         Human workspaces
    └── raiders/               Raider settings parent (shared)
        ├── .claude/settings.json  (context via hd rally)
        └── <name>/warband/         Worker worktrees
```

**Key points:**

- Warband root is a container, not a clone
- `.repo.git/` is bare - forge and raiders are worktrees
- Per-warband `warchief/warband/` holds canonical `.relics/`, others inherit via redirect
- Settings placed in parent dirs (not git clones) for upward traversal

## Relics Routing

Horde routes relics commands based on issue ID prefix. You don't need to think
about which database to use - just use the issue ID.

```bash
bd show gp-xyz    # Routes to greenplace warband's relics
bd show hq-abc    # Routes to encampment-level relics
bd show wyv-123   # Routes to wyvern warband's relics
```

**How it works**: Routes are defined in `~/horde/.relics/routes.jsonl`. Each warband's
prefix maps to its relics location (the warchief's clone in that warband).

| Prefix | Routes To | Purpose |
|--------|-----------|---------|
| `hq-*` | `~/horde/.relics/` | Warchief drums, cross-warband coordination |
| `gp-*` | `~/horde/greenplace/warchief/warband/.relics/` | Greenplace project issues |
| `wyv-*` | `~/horde/wyvern/warchief/warband/.relics/` | Wyvern project issues |

Debug routing: `BD_DEBUG_ROUTING=1 rl show <id>`

## Configuration

### Warband Config (`config.json`)

```json
{
  "type": "warband",
  "name": "myproject",
  "git_url": "https://github.com/...",
  "relics": { "prefix": "mp" }
}
```

### Settings (`settings/config.json`)

```json
{
  "theme": "desert",
  "max_workers": 5,
  "merge_queue": { "enabled": true }
}
```

### Runtime (`.runtime/` - gitignored)

Process state, PIDs, ephemeral data.

## Ritual Format

```toml
ritual = "name"
type = "workflow"           # workflow | expansion | aspect
version = 1
description = "..."

[vars.feature]
description = "..."
required = true

[[steps]]
id = "step-id"
title = "{{feature}}"
description = "..."
needs = ["other-step"]      # Dependencies
```

**Composition:**

```toml
extends = ["base-ritual"]

[compose]
aspects = ["cross-cutting"]

[[compose.expand]]
target = "step-id"
with = "macro-ritual"
```

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

**Note**: Wisps are stored in `.relics/` with an ephemeral flag - they're not
persisted to JSONL. They exist only in memory during execution.

## Totem Commands

**Principle**: `bd` = relics data operations, `hd` = agent operations.

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
bd mol squash <id>           # Condense to digest (explicit ID)
bd mol burn <id>             # Discard wisp (explicit ID)
```

### Agent Operations (hd)

```bash
# Banner management (operates on current agent's banner)
hd banner                    # What's on MY banner
hd totem current               # What should I work on next
hd totem progress <id>         # Execution progress of totem
hd totem summon <bead> <totem>   # Pin totem to bead
hd totem dismiss <bead>         # Unpin totem from bead
hd totem summon-from-drums <id> # Summon from drums message

# Agent lifecycle (operates on agent's attached totem)
hd totem burn                  # Burn attached totem (no ID needed)
hd totem squash                # Squash attached totem (no ID needed)
hd totem step done <step>      # Complete a totem step
```

**Key distinction**: `rl mol burn/squash <id>` take explicit totem IDs.
`hd totem burn/squash` operate on the current agent's attached totem
(auto-detected from working directory).

## Agent Lifecycle

### Raider Shutdown

```
1. Complete work steps
2. rl mol squash (create digest)
3. Submit to merge queue
4. hd handoff (request shutdown)
5. Wait for Witness to kill session
6. Witness removes worktree + branch
```

### Session Cycling

```
1. Agent notices context filling
2. hd handoff (sends drums to self)
3. Manager kills session
4. Manager starts new session
5. New session reads handoff drums
```

## Environment Variables

Horde sets environment variables for each agent session via `config.AgentEnv()`.
These are set in tmux session environment when agents are spawned.

### Core Variables (All Agents)

| Variable | Purpose | Example |
|----------|---------|---------|
| `GT_ROLE` | Agent role type | `warchief`, `witness`, `raider`, `clan` |
| `GT_ROOT` | Encampment root directory | `/home/user/gt` |
| `BD_ACTOR` | Agent identity for attribution | `horde/raiders/toast` |
| `GIT_AUTHOR_NAME` | Commit attribution (same as BD_ACTOR) | `horde/raiders/toast` |
| `RELICS_DIR` | Relics database location | `/home/user/horde/horde/.relics` |

### Warband-Level Variables

| Variable | Purpose | Roles |
|----------|---------|-------|
| `GT_RIG` | Warband name | witness, forge, raider, clan |
| `GT_RAIDER` | Raider worker name | raider only |
| `GT_CREW` | Clan worker name | clan only |
| `RELICS_AGENT_NAME` | Agent name for relics operations | raider, clan |
| `RELICS_NO_DAEMON` | Disable relics daemon (isolated context) | raider, clan |

### Other Variables

| Variable | Purpose |
|----------|---------|
| `GIT_AUTHOR_EMAIL` | Workspace owner email (from git config) |
| `GT_TOWN_ROOT` | Override encampment root detection (manual use) |
| `CLAUDE_RUNTIME_CONFIG_DIR` | Custom Claude settings directory |

### Environment by Role

| Role | Key Variables |
|------|---------------|
| **Warchief** | `GT_ROLE=warchief`, `BD_ACTOR=warchief` |
| **Shaman** | `GT_ROLE=shaman`, `BD_ACTOR=shaman` |
| **Boot** | `GT_ROLE=boot`, `BD_ACTOR=shaman-boot` |
| **Witness** | `GT_ROLE=witness`, `GT_RIG=<warband>`, `BD_ACTOR=<warband>/witness` |
| **Forge** | `GT_ROLE=forge`, `GT_RIG=<warband>`, `BD_ACTOR=<warband>/forge` |
| **Raider** | `GT_ROLE=raider`, `GT_RIG=<warband>`, `GT_RAIDER=<name>`, `BD_ACTOR=<warband>/raiders/<name>` |
| **Clan** | `GT_ROLE=clan`, `GT_RIG=<warband>`, `GT_CREW=<name>`, `BD_ACTOR=<warband>/clan/<name>` |

### Doctor Check

The `hd doctor` command verifies that running tmux sessions have correct
environment variables. Mismatches are reported as warnings:

```
⚠ env-vars: Found 3 env var mismatch(es) across 1 session(s)
    hq-warchief: missing GT_ROOT (expected "/home/user/gt")
```

Fix by restarting sessions: `hd shutdown && hd up`

## Agent Working Directories and Settings

Each agent runs in a specific working directory and has its own Claude settings.
Understanding this hierarchy is essential for proper configuration.

### Working Directories by Role

| Role | Working Directory | Notes |
|------|-------------------|-------|
| **Warchief** | `~/horde/warchief/` | Encampment-level coordinator, isolated from warbands |
| **Shaman** | `~/horde/shaman/` | Background supervisor daemon |
| **Witness** | `~/horde/<warband>/witness/` | No git clone, monitors raiders only |
| **Forge** | `~/horde/<warband>/forge/warband/` | Worktree on main branch |
| **Clan** | `~/horde/<warband>/clan/<name>/warband/` | Persistent human workspace clone |
| **Raider** | `~/horde/<warband>/raiders/<name>/warband/` | Ephemeral worker worktree |

Note: The per-warband `<warband>/warchief/warband/` directory is NOT a working directory—it's
a git clone that holds the canonical `.relics/` database for that warband.

### Settings File Locations

Claude Code searches for `.claude/settings.json` starting from the working
directory and traversing upward. Settings are placed in **parent directories**
(not inside git clones) so they're found via directory traversal without
polluting source repositories:

```
~/horde/
├── warchief/.claude/settings.json          # Warchief settings
├── shaman/.claude/settings.json         # Shaman settings
└── <warband>/
    ├── witness/.claude/settings.json    # Witness settings (no warband/ subdir)
    ├── forge/.claude/settings.json   # Found by forge/warband/ via traversal
    ├── clan/.claude/settings.json       # Shared by all clan/<name>/warband/
    └── raiders/.claude/settings.json   # Shared by all raiders/<name>/warband/
```

**Why parent directories?** Agents working in git clones (like `forge/warband/`)
would pollute the source repo if settings were placed there. By putting settings
one level up, Claude finds them via upward traversal, and all workers of the
same type share the same settings.

### CLAUDE.md Locations

Role context is delivered via CLAUDE.md files or ephemeral injection:

| Role | CLAUDE.md Location | Method |
|------|-------------------|--------|
| **Warchief** | `~/horde/warchief/CLAUDE.md` | On disk |
| **Shaman** | (none) | Injected via `hd rally` at SessionStart |
| **Witness** | (none) | Injected via `hd rally` at SessionStart |
| **Forge** | `<warband>/forge/warband/CLAUDE.md` | On disk (inside worktree) |
| **Clan** | (none) | Injected via `hd rally` at SessionStart |
| **Raider** | (none) | Injected via `hd rally` at SessionStart |

Additionally, each warband has `<warband>/warchief/warband/CLAUDE.md` for the per-warband warchief clone
(used for relics operations, not a running agent).

**Why ephemeral injection?** Writing CLAUDE.md into git clones would:
1. Pollute source repos when agents commit/push
2. Leak Horde internals into project history
3. Conflict with project-specific CLAUDE.md files

The `hd rally` command runs at SessionStart hook and injects context without
persisting it to disk.

### Sparse Checkout (Source Repo Isolation)

When agents work on source repositories that have their own Claude Code configuration,
Horde uses git sparse checkout to exclude all context files:

```bash
# Automatically configured for worktrees - excludes:
# - .claude/       : settings, rules, agents, commands
# - CLAUDE.md      : primary context file
# - CLAUDE.local.md: personal context file
# - .mcp.json      : MCP server configuration
git sparse-checkout set --no-cone '/*' '!/.claude/' '!/CLAUDE.md' '!/CLAUDE.local.md' '!/.mcp.json'
```

This ensures agents use Horde's context, not the source repo's instructions.

**Doctor check**: `hd doctor` verifies sparse checkout is configured correctly.
Run `hd doctor --fix` to update legacy configurations missing the newer patterns.

### Settings Inheritance

Claude Code's settings search order (first match wins):

1. `.claude/settings.json` in current working directory
2. `.claude/settings.json` in parent directories (traversing up)
3. `~/.claude/settings.json` (user global settings)

Horde places settings at each agent's working directory root, so agents
find their role-specific settings before reaching any parent or global config.

### Settings Templates

Horde uses two settings templates based on role type:

| Type | Roles | Key Difference |
|------|-------|----------------|
| **Interactive** | Warchief, Clan | Drums injected on `UserPromptSubmit` hook |
| **Autonomous** | Raider, Witness, Forge, Shaman | Drums injected on `SessionStart` hook |

Autonomous agents may start without user input, so they need drums checked
at session start. Interactive agents wait for user prompts.

### Troubleshooting

| Problem | Solution |
|---------|----------|
| Agent using wrong settings | Check `hd doctor`, verify sparse checkout |
| Settings not found | Ensure `.claude/settings.json` exists at role home |
| Source repo settings leaking | Run `hd doctor --fix` to configure sparse checkout |
| Warchief settings affecting raiders | Warchief should run in `warchief/`, not encampment root |

## CLI Reference

### Encampment Management

```bash
hd install [path]            # Create encampment
hd install --git             # With git init
hd doctor                    # Health check
hd doctor --fix              # Auto-repair
```

### Configuration

```bash
# Agent management
hd config agent list [--json]     # List all agents (built-in + custom)
hd config agent get <name>        # Show agent configuration
hd config agent set <name> <cmd>  # Create or update custom agent
hd config agent remove <name>     # Remove custom agent (built-ins protected)

# Default agent
hd config default-agent [name]    # Get or set encampment default agent
```

**Built-in agents**: `claude`, `gemini`, `codex`, `cursor`, `auggie`, `amp`

**Custom agents**: Define per-encampment via CLI or JSON:
```bash
hd config agent set claude-glm "claude-glm --model glm-4"
hd config agent set claude "claude-opus"  # Override built-in
hd config default-agent claude-glm       # Set default
```

**Advanced agent config** (`settings/agents.json`):
```json
{
  "version": 1,
  "agents": {
    "opencode": {
      "command": "opencode",
      "args": [],
      "resume_flag": "--session",
      "resume_style": "flag",
      "non_interactive": {
        "subcommand": "run",
        "output_flag": "--format json"
      }
    }
  }
}
```

**Warband-level agents** (`<warband>/settings/config.json`):
```json
{
  "type": "warband-settings",
  "version": 1,
  "agent": "opencode",
  "agents": {
    "opencode": {
      "command": "opencode",
      "args": ["--session"]
    }
  }
}
```

**Agent resolution order**: warband-level → encampment-level → built-in presets.

For OpenCode autonomous mode, set env var in your shell profile:
```bash
export OPENCODE_PERMISSION='{"*":"allow"}'
```

### Warband Management

```bash
hd warband add <name> <url>
hd warband list
hd warband remove <name>
```

### Raid Management (Primary Warmap)

```bash
hd raid list                          # Warmap of active raids
hd raid status [raid-id]            # Show progress (🚚 hq-cv-*)
hd raid create "name" [issues...]     # Create raid tracking issues
hd raid create "name" hd-a bd-b --notify warchief/  # With notification
hd raid list --all                    # Include landed raids
hd raid list --status=closed          # Only landed raids
```

Note: "Swarm" is ephemeral (workers on a raid's issues). See [Raids](concepts/raid.md).

### Work Assignment

```bash
# Standard workflow: raid first, then charge
hd raid create "Feature X" hd-abc hd-def
hd charge hd-abc <warband>                    # Assign to raider
hd charge hd-abc <warband> --agent codex      # Override runtime for this charge/muster
hd charge <proto> --on hd-def <warband>       # With workflow template

# Quick charge (auto-creates raid)
hd charge <bead> <warband>                    # Auto-raid for warmap visibility
```

Agent overrides:

- `hd start --agent <alias>` overrides the Warchief/Shaman runtime for this launch.
- `hd warchief start|summon|restart --agent <alias>` and `hd shaman start|summon|restart --agent <alias>` do the same.
- `hd start clan <name> --agent <alias>` and `hd clan at <name> --agent <alias>` override the clan worker runtime.

### Communication

```bash
hd drums inbox
hd drums read <id>
hd drums send <addr> -s "Subject" -m "Body"
hd drums send --human -s "..."    # To overseer
```

### Escalation

```bash
hd escalate "topic"              # Default: MEDIUM severity
hd escalate -s CRITICAL "msg"    # Urgent, immediate attention
hd escalate -s HIGH "msg"        # Important blocker
hd escalate -s MEDIUM "msg" -m "Details..."
```

See [escalation.md](design/escalation.md) for full protocol.

### Sessions

```bash
hd handoff                   # Request cycle (context-aware)
hd handoff --shutdown        # Terminate (raiders)
hd session stop <warband>/<agent>
hd peek <agent>              # Check health
hd signal <agent> "message"   # Send message to agent
hd seance                    # List discoverable predecessor sessions
hd seance --talk <id>        # Talk to predecessor (full context)
hd seance --talk <id> -p "Where is X?"  # One-shot question
```

**Session Discovery**: Each session has a startup signal that becomes searchable
in Claude's `/resume` picker:

```
[GAS ENCAMPMENT] recipient <- sender • timestamp • topic[:totem-id]
```

Example: `[GAS ENCAMPMENT] horde/clan/gus <- human • 2025-12-30T15:42 • restart`

**IMPORTANT**: Always use `hd signal` to send messages to Claude sessions.
Never use raw `tmux send-keys` - it doesn't handle Claude's input correctly.
`hd signal` uses literal mode + debounce + separate Enter for reliable delivery.

### Emergency

```bash
hd stop --all                # Kill all sessions
hd stop --warband <name>         # Kill warband sessions
```

## Relics Commands (bd)

```bash
bd ready                     # Work with no blockers
bd list --status=open
bd list --status=in_progress
bd show <id>
bd create --title="..." --type=task
bd update <id> --status=in_progress
bd close <id>
bd dep add <child> <parent>  # child depends on parent
bd sync                      # Push/pull changes
```

## Scout Agents

Shaman, Witness, and Forge run continuous scout loops using wisps:

| Agent | Scout Totem | Responsibility |
|-------|-----------------|----------------|
| **Shaman** | `totem-shaman-scout` | Agent lifecycle, plugin execution, health checks |
| **Witness** | `totem-witness-scout` | Monitor raiders, signal stuck workers |
| **Forge** | `totem-forge-scout` | Process merge queue, review MRs |

```
1. rl mol wisp totem-<role>-scout
2. Execute steps (check workers, process queue, run plugins)
3. rl mol squash (or burn if routine)
4. Loop
```

## Plugin Totems

Plugins are totems with specific labels:

```json
{
  "id": "totem-security-scan",
  "labels": ["template", "plugin", "witness", "tier:haiku"]
}
```

Scout totems bond plugins dynamically:

```bash
bd mol bond totem-security-scan $PATROL_ID --var scope="$SCOPE"
```

## Common Issues

| Problem | Solution |
|---------|----------|
| Agent in wrong directory | Check cwd, `hd doctor` |
| Relics prefix mismatch | Check `rl show` vs warband config |
| Worktree conflicts | Ensure `RELICS_NO_DAEMON=1` for raiders |
| Stuck worker | `hd signal`, then `hd peek` |
| Dirty git state | Commit or discard, then `hd handoff` |

## Architecture Notes

**Bare repo pattern**: `.repo.git/` is bare (no working dir). Forge and raiders are worktrees sharing refs. Raider branches visible to forge immediately.

**Relics as control plane**: No separate orchestrator. Totem steps ARE relics issues. State transitions are git commits.

**Nondeterministic idempotence**: Any worker can continue any totem. Steps are atomic checkpoints in relics.

**Raid tracking**: Raids track batched work across warbands. A "swarm" is ephemeral - just the workers currently on a raid's issues. See [Raids](concepts/raid.md) for details.
