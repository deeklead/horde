# Totem Mall Design

> A marketplace for Horde rituals

## Vision

**Totem Mall** is a registry for sharing rituals across Horde installations. Think npm for totems, or Terraform Registry for workflows.

```
"Invoke a ritual, charge it to a raider, the witness watches, forge merges."

What if you could browse a mall of totems, install one, and immediately
have your raiders executing world-class workflows?
```

### The Network Effect

A well-designed ritual for "code review" or "security audit" or "deploy to K8s" can spread across thousands of Horde installations. Each adoption means:
- More agents executing proven workflows
- More structured, trackable work output
- Better capability routing (agents with track records on a ritual get similar work)

## Architecture

### Registry Types

```
┌─────────────────────────────────────────────────────────────────┐
│                     TOTEM MALL REGISTRIES                        │
└─────────────────────────────────────────────────────────────────┘

PUBLIC REGISTRY (molmall.horde.io)
├── Community rituals (MIT licensed)
├── Official Horde rituals (blessed)
├── Verified publisher rituals
└── Open contribution model

PRIVATE REGISTRY (self-hosted)
├── Organization-specific rituals
├── Proprietary workflows
├── Internal deployment patterns
└── Enterprise compliance rituals

FEDERATED REGISTRY (HOP future)
├── Cross-organization discovery
├── Skill-based search
└── Attribution chain tracking
└── hop:// URI resolution
```

### URI Scheme

```
hop://molmall.horde.io/rituals/totem-raider-work@4.0.0
       └──────────────────┘         └──────────────┘ └───┘
           registry host              ritual name   version

# Short forms
totem-raider-work                    # Default registry, latest version
totem-raider-work@4                  # Major version
totem-raider-work@4.0.0              # Exact version
@acme/totem-deploy                    # Scoped to publisher
hop://acme.corp/rituals/totem-deploy # Full HOP URI
```

### Registry API

```yaml
# OpenAPI-style specification

GET /rituals
  # List all rituals
  Query:
    - q: string          # Search query
    - capabilities: string[]   # Filter by capability tags
    - author: string     # Filter by author
    - limit: int
    - offset: int
  Response:
    rituals:
      - name: totem-raider-work
        version: 4.0.0
        description: "Full raider work lifecycle..."
        author: steve@horde.io
        downloads: 12543
        capabilities: [go, testing, code-review]

GET /rituals/{name}
  # Get ritual metadata
  Response:
    name: totem-raider-work
    versions: [4.0.0, 3.2.1, 3.2.0, ...]
    latest: 4.0.0
    author: steve@horde.io
    repository: https://github.com/OWNER/horde
    license: MIT
    capabilities:
      primary: [go, testing]
      secondary: [git, code-review]
    stats:
      downloads: 12543
      stars: 234
      used_by: 89  # towns using this ritual

GET /rituals/{name}/{version}
  # Get specific version
  Response:
    name: totem-raider-work
    version: 4.0.0
    checksum: sha256:abc123...
    signature: <optional PGP signature>
    content: <base64 or URL to .ritual.toml>
    changelog: "Added self-cleaning model..."
    published_at: 2026-01-10T00:00:00Z

POST /rituals
  # Publish ritual (authenticated)
  Body:
    name: totem-my-workflow
    version: 1.0.0
    content: <ritual TOML>
    changelog: "Initial release"
  Auth: Bearer token (linked to HOP identity)

GET /rituals/{name}/{version}/download
  # Download ritual content
  Response: raw .ritual.toml content
```

## Ritual Package Format

### Simple Case: Single File

Most rituals are single `.ritual.toml` files:

```bash
hd totem install totem-raider-code-review
# Downloads totem-raider-code-review.ritual.toml to ~/horde/.relics/rituals/
```

### Complex Case: Ritual Bundle

Some rituals need supporting files (scripts, templates, configs):

```
totem-deploy-k8s.ritual.bundle/
├── ritual.toml              # Main ritual
├── templates/
│   ├── deployment.yaml.tmpl
│   └── service.yaml.tmpl
├── scripts/
│   └── healthcheck.sh
└── README.md
```

Bundle format:
```bash
# Bundles are tarballs
totem-deploy-k8s-1.0.0.bundle.tar.gz
```

Installation:
```bash
hd totem install totem-deploy-k8s
# Extracts to ~/horde/.relics/rituals/totem-deploy-k8s/
# ritual.toml is at totem-deploy-k8s/ritual.toml
```

## Installation Flow

### Basic Install

```bash
$ hd totem install totem-raider-code-review

Resolving totem-raider-code-review...
  Registry: totemmall.horde.io
  Version:  1.2.0 (latest)
  Author:   steve@horde.io
  Skills:   code-review, security

Downloading... ████████████████████ 100%
Verifying checksum... ✓

Installed to: ~/horde/.relics/rituals/totem-raider-code-review.ritual.toml
```

### Version Pinning

```bash
$ hd totem install totem-raider-work@4.0.0

Installing totem-raider-work@4.0.0 (pinned)...
✓ Installed

$ hd totem list --installed
  totem-raider-work           4.0.0   [pinned]
  totem-raider-code-review    1.2.0   [latest]
```

### Upgrade Flow

```bash
$ hd totem upgrade totem-raider-code-review

Checking for updates...
  Current: 1.2.0
  Latest:  1.3.0

Changelog for 1.3.0:
  - Added security focus option
  - Improved test coverage step

Upgrade? [y/N] y

Downloading... ✓
Installed: totem-raider-code-review@1.3.0
```

### Lock File

```json
// ~/horde/.relics/rituals/.lock.json
{
  "version": 1,
  "rituals": {
    "totem-raider-work": {
      "version": "4.0.0",
      "pinned": true,
      "checksum": "sha256:abc123...",
      "installed_at": "2026-01-10T00:00:00Z",
      "source": "hop://molmall.horde.io/rituals/totem-raider-work@4.0.0"
    },
    "totem-raider-code-review": {
      "version": "1.3.0",
      "pinned": false,
      "checksum": "sha256:def456...",
      "installed_at": "2026-01-10T12:00:00Z",
      "source": "hop://molmall.horde.io/rituals/totem-raider-code-review@1.3.0"
    }
  }
}
```

## Publishing Flow

### First-Time Setup

```bash
$ hd totem publish --init

Setting up Totem Mall publishing...

1. Create account at https://totemmall.horde.io/signup
2. Generate API token at https://totemmall.horde.io/settings/tokens
3. Run: hd totem login

$ hd totem login
Token: ********
Logged in as: steve@horde.io
```

### Publishing

```bash
$ hd totem publish totem-raider-work

Publishing totem-raider-work...

Pre-flight checks:
  ✓ ritual.toml is valid
  ✓ Version 4.0.0 not yet published
  ✓ Required fields present (name, version, description)
  ✓ Skills declared

Publish to totemmall.horde.io? [y/N] y

Uploading... ✓
Published: hop://totemmall.horde.io/rituals/totem-raider-work@4.0.0

View at: https://totemmall.horde.io/rituals/totem-raider-work
```

### Verification Levels

```
┌─────────────────────────────────────────────────────────────────┐
│                    RITUAL TRUST LEVELS                          │
└─────────────────────────────────────────────────────────────────┘

UNVERIFIED (default)
  Anyone can publish
  Basic validation only
  Displayed with ⚠️ warning

VERIFIED PUBLISHER
  Publisher identity confirmed
  Displayed with ✓ checkmark
  Higher search ranking

OFFICIAL
  Maintained by Horde team
  Displayed with 🏛️ badge
  Included in embedded defaults

AUDITED
  Security review completed
  Displayed with 🔒 badge
  Required for enterprise registries
```

## Capability Tagging

### Ritual Capability Declaration

```toml
[ritual.capabilities]
# What capabilities does this ritual exercise? Used for agent routing.
primary = ["go", "testing", "code-review"]
secondary = ["git", "ci-cd"]

# Capability weights (optional, for fine-grained routing)
[ritual.capabilities.weights]
go = 0.3           # 30% of ritual work is Go
testing = 0.4      # 40% is testing
code-review = 0.3  # 30% is code review
```

### Capability-Based Search

```bash
$ hd totem search --capabilities="security,go"

Rituals matching capabilities: security, go

  totem-security-audit           v2.1.0   ⭐ 4.8   📥 8,234
    Capabilities: security, go, code-review
    "Comprehensive security audit workflow"

  totem-dependency-scan          v1.0.0   ⭐ 4.2   📥 3,102
    Capabilities: security, go, supply-chain
    "Scan Go dependencies for vulnerabilities"
```

### Agent Accountability

When a raider completes a totem, the execution is tracked:

```
Raider: relics/amber
Totem: totem-raider-code-review@1.3.0
Completed: 2026-01-10T15:30:00Z
Capabilities exercised:
  - code-review (primary)
  - security (secondary)
  - go (secondary)
```

This execution record enables:
1. **Routing** - Agents with successful track records get similar work
2. **Debugging** - Trace which agent did what, when
3. **Quality metrics** - Track success rates by agent and ritual

## Private Registries

### Enterprise Deployment

```yaml
# ~/.gtconfig.yaml
registries:
  - name: acme
    url: https://molmall.acme.corp
    auth: token
    priority: 1  # Check first

  - name: public
    url: https://molmall.horde.io
    auth: none
    priority: 2  # Fallback
```

### Self-Hosted Registry

```bash
# Docker deployment
docker run -d \
  -p 8080:8080 \
  -v /data/rituals:/rituals \
  -e AUTH_PROVIDER=oidc \
  horde/molmall-registry:latest

# Configuration
MOLMALL_STORAGE=s3://bucket/rituals
MOLMALL_AUTH=oidc
MOLMALL_OIDC_ISSUER=https://auth.acme.corp
```

## Federation

Federation enables ritual sharing across organizations using the Highway Operations Protocol (HOP).

### Cross-Registry Discovery

```bash
$ hd totem search "deploy kubernetes" --federated

Searching across federated registries...

  totemmall.horde.io:
    totem-deploy-k8s           v3.0.0   🏛️ Official

  totemmall.acme.corp:
    @acme/totem-deploy-k8s     v2.1.0   ✓ Verified

  totemmall.bigco.io:
    @bigco/k8s-workflow      v1.0.0   ⚠️ Unverified
```

### HOP URI Resolution

The `hop://` URI scheme provides cross-registry entity references:

```bash
# Full HOP URI
hd totem install hop://totemmall.acme.corp/rituals/@acme/totem-deploy@2.1.0

# Resolution via HOP (Highway Operations Protocol)
1. Parse hop:// URI
2. Resolve registry endpoint (DNS/HOP discovery)
3. Authenticate (if required)
4. Download ritual
5. Verify checksum/signature
6. Install to encampment-level
```

## Implementation Phases

### Phase 1: Local Commands (Now)

- `hd totem list` with tier display
- `hd totem show --resolve`
- Ritual resolution order (project → encampment → system)

### Phase 2: Manual Sharing

- Ritual export/import
- `hd totem export totem-raider-work > totem-raider-work.ritual.toml`
- `hd totem import < totem-raider-work.ritual.toml`
- Lock file format

### Phase 3: Public Registry

- totemmall.horde.io launch
- `hd totem install` from registry
- `hd totem publish` flow
- Basic search and browse

### Phase 4: Enterprise Features

- Private registry support
- Authentication integration
- Verification levels
- Audit logging

### Phase 5: Federation (HOP)

- Capability tags in schema
- Federation protocol (Highway Operations Protocol)
- Cross-registry search
- Agent execution tracking for accountability

## Related Documents

- [Ritual Resolution](ritual-resolution.md) - Local resolution order
- [totems.md](totems.md) - Ritual lifecycle (invoke, cast, squash)
- [understanding-horde.md](../../../docs/understanding-horde.md) - Horde architecture
