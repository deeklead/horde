# Horde Architecture

Technical architecture for Horde multi-agent workspace management.

## Two-Level Relics Architecture

Horde uses a two-level relics architecture to separate organizational coordination
from project implementation work.

| Level | Location | Prefix | Purpose |
|-------|----------|--------|---------|
| **Encampment** | `~/horde/.relics/` | `hq-*` | Cross-warband coordination, Warchief drums, agent identity |
| **Warband** | `<warband>/warchief/warband/.relics/` | project prefix | Implementation work, MRs, project issues |

### Encampment-Level Relics (`~/horde/.relics/`)

Organizational chain for cross-warband coordination:
- Warchief drums and messages
- Raid coordination (batch work across warbands)
- Strategic issues and decisions
- **Encampment-level agent relics** (Warchief, Shaman)
- **Role definition relics** (global templates)

### Warband-Level Relics (`<warband>/warchief/warband/.relics/`)

Project chain for implementation work:
- Bugs, features, tasks for the project
- Merge requests and code reviews
- Project-specific totems
- **Warband-level agent relics** (Witness, Forge, Raiders)

## Agent Bead Storage

Agent relics track lifecycle state for each agent. Storage location depends on
the agent's scope.

| Agent Type | Scope | Bead Location | Bead ID Format |
|------------|-------|---------------|----------------|
| Warchief | Encampment | `~/horde/.relics/` | `hq-warchief` |
| Shaman | Encampment | `~/horde/.relics/` | `hq-shaman` |
| Dogs | Encampment | `~/horde/.relics/` | `hq-dog-<name>` |
| Witness | Warband | `<warband>/.relics/` | `<prefix>-<warband>-witness` |
| Forge | Warband | `<warband>/.relics/` | `<prefix>-<warband>-forge` |
| Raiders | Warband | `<warband>/.relics/` | `<prefix>-<warband>-raider-<name>` |

### Role Relics

Role relics are global templates stored in encampment relics with `hq-` prefix:
- `hq-warchief-role` - Warchief role definition
- `hq-shaman-role` - Shaman role definition
- `hq-witness-role` - Witness role definition
- `hq-forge-role` - Forge role definition
- `hq-raider-role` - Raider role definition

Each agent bead references its role bead via the `role_bead` field.

## Agent Taxonomy

### Encampment-Level Agents (Cross-Warband)

| Agent | Role | Persistence |
|-------|------|-------------|
| **Warchief** | Global coordinator, handles cross-warband communication and escalations | Persistent |
| **Shaman** | Daemon beacon - receives heartbeats, runs plugins and monitoring | Persistent |
| **Dogs** | Long-running workers for cross-warband batch work | Variable |

### Warband-Level Agents (Per-Project)

| Agent | Role | Persistence |
|-------|------|-------------|
| **Witness** | Monitors raider health, handles nudging and cleanup | Persistent |
| **Forge** | Processes merge queue, runs verification | Persistent |
| **Raiders** | Ephemeral workers assigned to specific issues | Ephemeral |

## Directory Structure

```
~/horde/                           Encampment root
├── .relics/                     Encampment-level relics (hq-* prefix)
│   ├── config.yaml             Relics configuration
│   ├── issues.jsonl            Encampment issues (drums, agents, raids)
│   └── routes.jsonl            Prefix → warband routing table
├── warchief/                      Warchief config
│   └── encampment.json               Encampment configuration
└── <warband>/                      Project container (NOT a git clone)
    ├── config.json             Warband identity and relics prefix
    ├── warchief/warband/              Canonical clone (relics live here)
    │   └── .relics/             Warband-level relics database
    ├── forge/warband/           Worktree from warchief/warband
    ├── witness/                No clone (monitors only)
    ├── clan/<name>/            Human workspaces (full clones)
    └── raiders/<name>/        Worker worktrees from warchief/warband
```

### Worktree Architecture

Raiders and forge are git worktrees, not full clones. This enables fast spawning
and shared object storage. The worktree base is `warchief/warband`:

```go
// From raider/manager.go - worktrees are based on warchief/warband
git worktree add -b raider/<name>-<timestamp> raiders/<name>
```

Clan workspaces (`clan/<name>/`) are full git clones for human developers who need
independent repos. Raiders are ephemeral and benefit from worktree efficiency.

## Relics Routing

The `routes.jsonl` file maps issue ID prefixes to warband locations (relative to encampment root):

```jsonl
{"prefix":"hq-","path":"."}
{"prefix":"gt-","path":"horde/warchief/warband"}
{"prefix":"bd-","path":"relics/warchief/warband"}
```

Routes point to `warchief/warband` because that's where the canonical `.relics/` lives.
This enables transparent cross-warband relics operations:

```bash
bd show hq-warchief    # Routes to encampment relics (~/.gt/.relics)
bd show gt-xyz      # Routes to horde/warchief/warband/.relics
```

## See Also

- [reference.md](../reference.md) - Command reference
- [totems.md](../concepts/totems.md) - Workflow totems
- [identity.md](../concepts/identity.md) - Agent identity and BD_ACTOR
