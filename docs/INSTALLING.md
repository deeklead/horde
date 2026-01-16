# Installing Horde

Complete setup guide for Horde multi-agent orchestrator.

## Prerequisites

### Required

| Tool | Version | Check | Install |
|------|---------|-------|---------|
| **Go** | 1.24+ | `go version` | See [golang.org](https://go.dev/doc/install) |
| **Git** | 2.20+ | `git --version` | See below |
| **Relics** | latest | `rl version` | `go install github.com/deeklead/relics/cmd/rl@latest` |

### Optional (for Full Stack Mode)

| Tool | Version | Check | Install |
|------|---------|-------|---------|
| **tmux** | 3.0+ | `tmux -V` | See below |
| **Claude Code** (default) | latest | `claude --version` | See [claude.ai/claude-code](https://claude.ai/claude-code) |
| **Codex CLI** (optional) | latest | `codex --version` | See [developers.openai.com/codex/cli](https://developers.openai.com/codex/cli) |
| **OpenCode CLI** (optional) | latest | `opencode --version` | See [opencode.ai](https://opencode.ai) |

## Installing Prerequisites

### macOS

```bash
# Install Homebrew if needed
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Required
brew install go git

# Optional (for full stack mode)
brew install tmux
```

### Linux (Debian/Ubuntu)

```bash
# Required
sudo apt update
sudo apt install -y git

# Install Go (apt version may be outdated, use official installer)
wget https://go.dev/dl/go1.24.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.24.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc
source ~/.bashrc

# Optional (for full stack mode)
sudo apt install -y tmux
```

### Linux (Fedora/RHEL)

```bash
# Required
sudo dnf install -y git golang

# Optional
sudo dnf install -y tmux
```

### Verify Prerequisites

```bash
# Check all prerequisites
go version        # Should show go1.24 or higher
git --version     # Should show 2.20 or higher
tmux -V           # (Optional) Should show 3.0 or higher
```

## Installing Horde

### Step 1: Install the Binaries

```bash
# Install Horde CLI
go install github.com/deeklead/horde/cmd/hd@latest

# Install Relics (issue tracker)
go install github.com/deeklead/relics/cmd/rl@latest

# Verify installation
hd version
bd version
```

If `hd` is not found, ensure `$GOPATH/bin` (usually `~/go/bin`) is in your PATH:

```bash
# Add to ~/.bashrc, ~/.zshrc, or equivalent
export PATH="$PATH:$HOME/go/bin"
```

### Step 2: Create Your Workspace

```bash
# Create a Horde workspace (HQ)
hd install ~/horde

# This creates:
#   ~/horde/
#   ├── CLAUDE.md          # Warchief role context
#   ├── warchief/             # Warchief config and state
#   ├── warbands/              # Project containers (initially empty)
#   └── .relics/            # Encampment-level issue tracking
```

### Step 3: Add a Project (Warband)

```bash
# Add your first project
hd warband add myproject https://github.com/you/repo.git

# This clones the repo and sets up:
#   ~/horde/myproject/
#   ├── .relics/            # Project issue tracking
#   ├── warchief/warband/         # Warchief's clone (canonical)
#   ├── forge/warband/      # Merge queue processor
#   ├── witness/           # Worker monitor
#   └── raiders/          # Worker clones (created on demand)
```

### Step 4: Verify Installation

```bash
cd ~/horde
hd doctor              # Run health checks
hd status              # Show workspace status
```

### Step 5: Configure Agents (Optional)

Horde supports built-in runtimes (`claude`, `gemini`, `codex`) plus custom agent aliases.

```bash
# List available agents
hd config agent list

# Create an alias (aliases can encode model/thinking flags)
hd config agent set codex-low "codex --thinking low"
hd config agent set claude-haiku "claude --model haiku --dangerously-skip-permissions"

# Set the encampment default agent (used when a warband doesn't specify one)
hd config default-agent codex-low
```

You can also override the agent per command without changing defaults:

```bash
hd start --agent codex-low
hd charge issue-123 myproject --agent claude-haiku
```

## Minimal Mode vs Full Stack Mode

Horde supports two operational modes:

### Minimal Mode (No Daemon)

Run individual runtime instances manually. Horde only tracks state.

```bash
# Create and assign work
hd raid create "Fix bugs" issue-123
hd charge issue-123 myproject

# Run runtime manually
cd ~/horde/myproject/raiders/<worker>
claude --resume          # Claude Code
# or: codex              # Codex CLI

# Check progress
hd raid list
```

**When to use**: Testing, simple workflows, or when you prefer manual control.

### Full Stack Mode (With Daemon)

Agents run in tmux sessions. Daemon manages lifecycle automatically.

```bash
# Start the daemon
hd shaman start

# Create and assign work (workers muster automatically)
hd raid create "Feature X" issue-123 issue-456
hd charge issue-123 myproject
hd charge issue-456 myproject

# Monitor on warmap
hd raid list

# Summon to any agent session
hd warchief summon
hd witness summon myproject
```

**When to use**: Production workflows with multiple concurrent agents.

### Choosing Roles

Horde is modular. Enable only what you need:

| Configuration | Roles | Use Case |
|--------------|-------|----------|
| **Raiders only** | Workers | Manual spawning, no monitoring |
| **+ Witness** | + Monitor | Automatic lifecycle, stuck detection |
| **+ Forge** | + Merge queue | MR review, code integration |
| **+ Warchief** | + Coordinator | Cross-project coordination |

## Troubleshooting

### `hd: command not found`

Your Go bin directory is not in PATH:

```bash
# Add to your shell config (~/.bashrc, ~/.zshrc)
export PATH="$PATH:$HOME/go/bin"
source ~/.bashrc  # or restart terminal
```

### `bd: command not found`

Relics CLI not installed:

```bash
go install github.com/deeklead/relics/cmd/rl@latest
```

### `hd doctor` shows errors

Run with `--fix` to auto-repair common issues:

```bash
hd doctor --fix
```

For persistent issues, check specific errors:

```bash
hd doctor --verbose
```

### Daemon not starting

Check if tmux is installed and working:

```bash
tmux -V                    # Should show version
tmux new-session -d -s test && tmux kill-session -t test  # Quick test
```

### Git authentication issues

Ensure SSH keys or credentials are configured:

```bash
# Test SSH access
ssh -T git@github.com

# Or configure credential helper
git config --global credential.helper cache
```

### Relics sync issues

If relics aren't syncing across clones:

```bash
cd ~/horde/myproject/warchief/warband
bd sync --status           # Check sync status
bd doctor                  # Run relics health check
```

## Updating

To update Horde and Relics:

```bash
go install github.com/deeklead/horde/cmd/hd@latest
go install github.com/deeklead/relics/cmd/rl@latest
hd doctor --fix            # Fix any post-update issues
```

## Uninstalling

```bash
# Remove binaries
rm $(which hd) $(which bd)

# Remove workspace (CAUTION: deletes all work)
rm -rf ~/horde
```

## Next Steps

After installation:

1. **Read the README** - Core concepts and workflows
2. **Try a simple workflow** - `hd raid create "Test" test-issue`
3. **Explore docs** - `docs/reference.md` for command reference
4. **Run hd doctor regularly** - `hd doctor` catches problems early
