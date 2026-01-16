/*
hd is the Horde CLI for managing multi-agent AI coding workspaces.

Horde coordinates multiple AI coding agents (Claude Code, Codex, Gemini CLI)
working in parallel on your codebase. It provides:

  - Warchief: High-level coordinator for task distribution
  - Witness: Verification and monitoring agent
  - Forge: Merge queue management
  - Raiders: Ephemeral worker agents for autonomous tasks
  - Clans: Persistent workspaces for developers
  - Drums: Inter-agent messaging system

Usage:

	hd <command> [arguments]

Common commands:

	hd init           Initialize a new encampment
	hd warband add    Add a new project warband
	hd warchief summon  Start the Warchief coordinator
	hd status         Show encampment status
	hd version        Print version information

See 'hd help <command>' for more information on a specific command.
*/
package main

import (
	"os"

	"github.com/deeklead/horde/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
