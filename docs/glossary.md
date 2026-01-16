# Horde Glossary

Horde is an agentic development environment for managing multiple Claude Code instances simultaneously using the `hd` and `bd` (Relics) binaries, coordinated with tmux in git-managed directories.

## Core Principles

### HOWL (Molecular Expression of Work)
Breaking large goals into detailed instructions for agents. Supported by Relics, Epics, Rituals, and Totems. HOWL ensures work is decomposed into trackable, atomic units that agents can execute autonomously.

### GUPP (Horde Universal Propulsion Principle)
"If there is work on your Hook, YOU MUST RUN IT." This principle ensures agents autonomously proceed with available work without waiting for external input. GUPP is the heartbeat of autonomous operation.

### NDI (Nondeterministic Idempotence)
The overarching goal ensuring useful outcomes through orchestration of potentially unreliable processes. Persistent Relics and oversight agents (Witness, Shaman) guarantee eventual workflow completion even when individual operations may fail or produce varying results.

## Environments

### Encampment
The management headquarters (e.g., `~/horde/`). The Encampment coordinates all workers across multiple Warbands and houses encampment-level agents like Warchief and Shaman.

### Warband
A project-specific Git repository under Horde management. Each Warband has its own Raiders, Forge, Witness, and Clan members. Warbands are where actual development work happens.

## Encampment-Level Roles

### Warchief
Chief-of-staff agent responsible for initiating Raids, coordinating work distribution, and notifying users of important events. The Warchief operates from the encampment level and has visibility across all Warbands.

### Shaman
Daemon beacon running continuous Scout cycles. The Shaman ensures worker activity, monitors system health, and triggers recovery when agents become unresponsive. Think of the Shaman as the system's watchdog.

### Dogs
The Shaman's clan of maintenance agents handling background tasks like cleanup, health checks, and system maintenance.

### Boot (the Dog)
A special Dog that checks the Shaman every 5 minutes, ensuring the watchdog itself is still watching. This creates a chain of accountability.

## Warband-Level Roles

### Raider
Ephemeral worker agents that produce Merge Requests. Raiders are spawned for specific tasks, complete their work, and are then cleaned up. They work in isolated git worktrees to avoid conflicts.

### Forge
Manages the Merge Queue for a Warband. The Forge intelligently merges changes from Raiders, handling conflicts and ensuring code quality before changes reach the main branch.

### Witness
Scout agent that oversees Raiders and the Forge within a Warband. The Witness monitors progress, detects stuck agents, and can trigger recovery actions.

### Clan
Long-lived, named agents for persistent collaboration. Unlike ephemeral Raiders, Clan members maintain context across sessions and are ideal for ongoing work relationships.

## Work Units

### Bead
Git-backed atomic work unit stored in JSONL format. Relics are the fundamental unit of work tracking in Horde. They can represent issues, tasks, epics, or any trackable work item.

### Ritual
TOML-based workflow source template. Rituals define reusable patterns for common operations like scout cycles, code review, or deployment.

### Protomolecule
A template class for instantiating Totems. Protomolecules define the structure and steps of a workflow without being tied to specific work items.

### Totem
Durable chained Bead workflows. Totems represent multi-step processes where each step is tracked as a Bead. They survive agent restarts and ensure complex workflows complete.

### Wisp
Ephemeral Relics destroyed after runs. Wisps are lightweight work items used for transient operations that don't need permanent tracking.

### Hook
A special pinned Bead for each agent. The Hook is an agent's primary work queue - when work appears on your Hook, GUPP dictates you must run it.

## Workflow Commands

### Raid
Primary work-order wrapping related Relics. Raids group related tasks together and can be assigned to multiple workers. Created with `hd raid create`.

### Charging
Assigning work to agents via `hd charge`. When you charge work to a Raider or Clan member, you're putting it on their Hook for execution.

### Nudging
Real-time messaging between agents with `hd signal`. Nudges allow immediate communication without going through the drums system.

### Handoff
Agent session refresh via `/handoff`. When context gets full or an agent needs a fresh start, handoff transfers work state to a new session.

### Seance
Communicating with previous sessions via `hd seance`. Allows agents to query their predecessors for context and decisions from earlier work.

### Scout
Ephemeral loop maintaining system heartbeat. Scout agents (Shaman, Witness) continuously cycle through health checks and trigger actions as needed.

---

*This glossary was contributed by [Clay Shirky](https://github.com/cshirky) in [Issue #80](https://github.com/deeklead/horde/issues/80).*
