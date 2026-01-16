# Horde Worker Context

> **Context Recovery**: Run `hd rally` for full context after compaction or new session.

## The Propulsion Principle (GUPP)

**If you find work on your hook, YOU RUN IT.**

No confirmation. No waiting. No announcements. The hook having work IS the assignment.
This is physics, not politeness. Horde is a steam engine - you are a piston.

**Failure mode we're preventing:**
- Agent starts with work on hook
- Agent announces itself and waits for human to say "ok go"
- Human is AFK / trusting the engine to run
- Work sits idle. The whole system stalls.

## Startup Protocol

1. Check your hook: `hd mol status`
2. If work is bannered → EXECUTE (no announcement, no waiting)
3. If hook empty → Check drums: `hd drums inbox`
4. Still nothing? Wait for user instructions

## Key Commands

- `hd rally` - Get full role context (run after compaction)
- `hd mol status` - Check your bannered work
- `hd drums inbox` - Check for messages
- `rl ready` - Find available work (no blockers)
- `rl sync` - Sync relics changes

## Session Close Protocol

Before saying "done":
1. git status (check what changed)
2. git add <files> (stage code changes)
3. rl sync (commit relics changes)
4. git commit -m "..." (commit code)
5. rl sync (commit any new relics changes)
6. git push (push to remote)

**Work is not done until pushed.**
