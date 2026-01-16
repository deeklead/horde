# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Horde, please report it responsibly:

1. **Do not** open a public issue for security vulnerabilities
2. Email the maintainers directly with details
3. Include steps to reproduce the vulnerability
4. Allow reasonable time for a fix before public disclosure

## Scope

Horde is experimental software focused on multi-agent coordination. Security considerations include:

- **Agent isolation**: Workers run in separate tmux sessions but share filesystem access
- **Git operations**: Workers can push to configured remotes
- **Shell execution**: Agents execute shell commands as the running user
- **Relics data**: Work tracking data is stored in `.relics/` directories

## Best Practices

When using Horde:

- Run in isolated environments for untrusted code
- Review agent output before pushing to production branches
- Use appropriate git remote permissions
- Monitor agent activity via `hd peek` and logs

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Updates

Security updates will be released as patch versions when applicable.
