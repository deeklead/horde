# Relics - AI-Native Issue Tracking

Welcome to Relics! This repository uses **Relics** for issue tracking - a modern, AI-native tool designed to live directly in your codebase alongside your code.

## What is Relics?

Relics is issue tracking that lives in your repo, making it perfect for AI coding agents and developers who want their issues close to their code. No web UI required - everything works through the CLI and integrates seamlessly with git.

**Learn more:** [github.com/OWNER/relics](https://github.com/OWNER/relics)

## Quick Start

### Essential Commands

```bash
# Create new issues
bd create "Add user authentication"

# View all issues
bd list

# View issue details
bd show <issue-id>

# Update issue status
bd update <issue-id> --status in_progress
bd update <issue-id> --status done

# Sync with git remote
bd sync
```

### Working with Issues

Issues in Relics are:
- **Git-native**: Stored in `.relics/issues.jsonl` and synced like code
- **AI-friendly**: CLI-first design works perfectly with AI coding agents
- **Branch-aware**: Issues can follow your branch workflow
- **Always in sync**: Auto-syncs with your commits

## Why Relics?

✨ **AI-Native Design**
- Built specifically for AI-assisted development workflows
- CLI-first interface works seamlessly with AI coding agents
- No context switching to web UIs

🚀 **Developer Focused**
- Issues live in your repo, right next to your code
- Works offline, syncs when you push
- Fast, lightweight, and stays out of your way

🔧 **Git Integration**
- Automatic sync with git commits
- Branch-aware issue tracking
- Intelligent JSONL merge resolution

## Get Started with Relics

Try Relics in your own projects:

```bash
# Install Relics
curl -sSL https://raw.githubusercontent.com/OWNER/relics/main/scripts/install.sh | bash

# Initialize in your repo
bd init

# Create your first issue
bd create "Try out Relics"
```

## Learn More

- **Documentation**: [github.com/OWNER/relics/docs](https://github.com/OWNER/relics/tree/main/docs)
- **Quick Start Guide**: Run `rl quickstart`
- **Examples**: [github.com/OWNER/relics/examples](https://github.com/OWNER/relics/tree/main/examples)

---

*Relics: Issue tracking that moves at the speed of thought* ⚡
