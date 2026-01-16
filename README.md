# Horde

**Multi-agent orchestration system for AI coding assistants**

## Overview

Horde coordinates multiple AI coding agents (Claude Code, Codex, Gemini CLI) working in parallel on your codebase. Instead of losing context when agents restart, Horde persists work state in git-backed banners, enabling reliable multi-agent workflows.

### Why Horde?

| Challenge | Horde Solution |
|-----------|----------------|
| Agents lose context on restart | Work persists in git-backed banners |
| Manual agent coordination | Built-in wardrums, identities, and handoffs |
| 4-10 agents become chaotic | Scale comfortably to 20-30 agents |
| Work state lost in agent memory | Work state stored in Relics ledger |

## Core Concepts

### The Warchief 👑
Your primary AI coordinator. The Warchief is a Claude Code instance with full context about your workspace, projects, and agents.

### Encampment 🏕️
Your workspace directory (default: `~/horde/`). Contains all projects, agents, and configuration.

### Warbands ⚔️
Project containers. Each warband wraps a git repository and manages its associated agents.

### Clans 👤
Your personal workspace within a warband. Where you do hands-on work.

### Raiders 🏹
Ephemeral worker agents that muster, complete a task, and disappear.

### Banners 🚩
Git worktree-based persistent storage for agent work. Survives crashes and restarts.

### Raids 📋
Work tracking units. Bundle multiple issues/tasks that get assigned to agents.

## Installation

### Prerequisites

- **Go 1.23+** - [go.dev/dl](https://go.dev/dl/)
- **Git 2.25+** - for worktree support
- **tmux 3.0+** - recommended for full experience
- **Claude Code CLI** - [claude.ai/code](https://claude.ai/code)

### Setup

```bash
# Install Horde
go install github.com/OWNER/horde/cmd/hd@latest

# Add to PATH
export PATH="$PATH:$HOME/go/bin"

# Create workspace
hd install ~/horde --git
cd ~/horde

# Add your first project
hd warband add myproject https://github.com/you/repo.git

# Create your clan workspace
hd clan add yourname --warband myproject
cd myproject/clan/yourname

# Start the Warchief
hd warchief summon
```

## Quick Start

```bash
# Start the Warchief (main interface)
hd warchief summon

# Create a raid with work items
hd raid create "Feature X" issue-123 issue-456 --notify

# Assign work to an agent
hd charge issue-123 myproject

# Track progress
hd raid list

# Monitor agents
hd raiders
```

## Command Reference

### Workspace Management
```bash
hd install <path>               # Initialize workspace
hd warband add <name> <repo>    # Add project
hd warband list                 # List projects
hd clan add <name> --warband    # Create personal workspace
```

### Agent Operations
```bash
hd raiders                      # List active agents
hd charge <issue> <warband>     # Assign work to agent
hd warchief summon              # Start Warchief session
hd rally                        # Context recovery
```

### Raid (Work Tracking)
```bash
hd raid create <name> [issues]  # Create raid
hd raid list                    # List all raids
hd raid show [id]               # Show raid details
```

### Communication
```bash
hd drums inbox                  # Check messages
hd drums send <to> <msg>        # Send message
hd signal <agent>               # Signal an agent
```

### Configuration
```bash
hd config agent set <name> <cmd>
hd config default-agent <name>
hd config show
```

### Warmap (Dashboard)
```bash
hd warmap --port 8080
```

## Architecture

```
~/horde/                        # Encampment root
├── warchief/                   # Warchief config & state
├── myproject/                  # Warband
│   ├── warband/                # Git clone
│   ├── clan/                   # Clan workspaces
│   │   └── yourname/           # Your workspace
│   ├── raiders/                # Raider worktrees
│   └── .relics/                # Issue tracking
└── .relics/                    # Encampment-level relics
```

## HOWL Pattern

HOWL (Horde Orchestrated Workflow Loop) is the recommended orchestration pattern:

1. **Tell the Warchief** - Describe what you want
2. **Warchief analyzes** - Breaks down into tasks
3. **Raid creation** - Warchief creates raid with issues
4. **Agent mustering** - Warchief musters appropriate agents
5. **Work distribution** - Issues charged to agents via banners
6. **Progress monitoring** - Track through raid status
7. **Completion** - Warchief summarizes results

## License

MIT License
