---
description: Hand off to fresh session, work continues from hook
allowed-tools: Bash(hd drums send:*),Bash(hd handoff:*)
argument-hint: [message]
---

Hand off to a fresh session.

User's handoff message (if any): $ARGUMENTS

Execute these steps in order:

1. If user provided a message, send handoff drums to yourself first.
   Construct your drums address from your identity (e.g., horde/clan/max for clan, warchief/ for warchief).
   Example: `hd drums send horde/clan/max -s "HANDOFF: Session cycling" -m "USER_MESSAGE_HERE"`

2. Run the handoff command (this will respawn your session with a fresh Claude):
   `hd handoff`

Note: The new session will auto-rally via the SessionStart hook and find your handoff drums.
End watch. A new session takes over, picking up any totem on the hook.
