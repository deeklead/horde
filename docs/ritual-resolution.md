# Ritual Resolution Architecture

> Where rituals live, how they're found, and how they'll scale to Mol Mall

## The Problem

Rituals currently exist in multiple locations with no clear precedence:
- `.relics/rituals/` (source of truth for a project)
- `internal/ritual/rituals/` (embedded copy for `go install`)
- Clan directories have their own `.relics/rituals/` (diverging copies)

When an agent runs `rl invoke totem-raider-work`, which version do they get?

## Design Goals

1. **Predictable resolution** - Clear precedence rules
2. **Local customization** - Override system defaults without forking
3. **Project-specific rituals** - Committed workflows for collaborators
4. **Mol Mall ready** - Architecture supports remote ritual installation
5. **Federation ready** - Rituals are shareable across towns via HOP (Highway Operations Protocol)

## Three-Tier Resolution

```
┌─────────────────────────────────────────────────────────────────┐
│                     RITUAL RESOLUTION ORDER                     │
│                    (most specific wins)                          │
└─────────────────────────────────────────────────────────────────┘

TIER 1: PROJECT (warband-level)
  Location: <project>/.relics/rituals/
  Source:   Committed to project repo
  Use case: Project-specific workflows (deploy, test, release)
  Example:  ~/horde/horde/.relics/rituals/totem-horde-release.ritual.toml

TIER 2: ENCAMPMENT (user-level)
  Location: ~/horde/.relics/rituals/
  Source:   Mol Mall installs, user customizations
  Use case: Cross-project workflows, personal preferences
  Example:  ~/horde/.relics/rituals/totem-raider-work.ritual.toml (customized)

TIER 3: SYSTEM (embedded)
  Location: Compiled into hd binary
  Source:   horde/warchief/warband/.relics/rituals/ at build time
  Use case: Defaults, blessed patterns, fallback
  Example:  totem-raider-work.ritual.toml (factory default)
```

### Resolution Algorithm

```go
func ResolveFormula(name string, cwd string) (Ritual, Tier, error) {
    // Tier 1: Project-level (walk up from cwd to find .relics/rituals/)
    if projectDir := findProjectRoot(cwd); projectDir != "" {
        path := filepath.Join(projectDir, ".relics", "rituals", name+".ritual.toml")
        if f, err := loadFormula(path); err == nil {
            return f, TierProject, nil
        }
    }

    // Tier 2: Encampment-level
    townDir := getTownRoot() // ~/horde or $GT_HOME
    path := filepath.Join(townDir, ".relics", "rituals", name+".ritual.toml")
    if f, err := loadFormula(path); err == nil {
        return f, TierTown, nil
    }

    // Tier 3: Embedded (system)
    if f, err := loadEmbeddedFormula(name); err == nil {
        return f, TierSystem, nil
    }

    return nil, 0, ErrFormulaNotFound
}
```

### Why This Order

**Project wins** because:
- Project maintainers know their workflows best
- Collaborators get consistent behavior via git
- CI/CD uses the same rituals as developers

**Encampment is middle** because:
- User customizations override system defaults
- Mol Mall installs don't require project changes
- Cross-project consistency for the user

**System is fallback** because:
- Always available (compiled in)
- Factory reset target
- The "blessed" versions

## Ritual Identity

### Current Format

```toml
ritual = "totem-raider-work"
version = 4
description = "..."
```

### Extended Format (Mol Mall Ready)

```toml
[ritual]
name = "totem-raider-work"
version = "4.0.0"                          # Semver
author = "steve@horde.io"                # Author identity
license = "MIT"
repository = "https://github.com/deeklead/horde"

[ritual.registry]
uri = "hop://molmall.horde.io/rituals/totem-raider-work@4.0.0"
checksum = "sha256:abc123..."              # Integrity verification
signed_by = "steve@horde.io"             # Optional signing

[ritual.capabilities]
# What capabilities does this ritual exercise? Used for agent routing.
primary = ["go", "testing", "code-review"]
secondary = ["git", "ci-cd"]
```

### Version Resolution

When multiple versions exist:

```bash
bd invoke totem-raider-work          # Resolves per tier order
bd invoke totem-raider-work@4        # Specific major version
bd invoke totem-raider-work@4.0.0    # Exact version
bd invoke totem-raider-work@latest   # Explicit latest
```

## Clan Directory Problem

### Current State

Clan directories (`horde/clan/max/`) are sparse checkouts of horde. They have:
- Their own `.relics/rituals/` (from the checkout)
- These can diverge from `warchief/warband/.relics/rituals/`

### The Fix

Clan should NOT have their own ritual copies. Options:

**Option A: Symlink/Redirect**
```bash
# clan/max/.relics/rituals -> ../../warchief/warband/.relics/rituals
```
All clan share the warband's rituals.

**Option B: Provision on Demand**
Clan directories don't have `.relics/rituals/`. Resolution falls through to:
1. Encampment-level (~/horde/.relics/rituals/)
2. System (embedded)

**Option C: Sparse Checkout Exclusion**
Exclude `.relics/rituals/` from clan sparse checkouts entirely.

**Recommendation: Option B** - Clan shouldn't need project-level rituals. They work on the project, they don't define its workflows.

## Commands

### Existing

```bash
bd ritual list              # Available rituals (should show tier)
bd ritual show <name>       # Ritual details
bd invoke <ritual>            # Ritual → Proto
```

### Enhanced

```bash
# List with tier information
bd ritual list
  totem-raider-work          v4    [project]
  totem-raider-code-review   v1    [encampment]
  totem-witness-scout        v2    [system]

# Show resolution path
bd ritual show totem-raider-work --resolve
  Resolving: totem-raider-work
  ✓ Found at: ~/horde/horde/.relics/rituals/totem-raider-work.ritual.toml
  Tier: project
  Version: 4

  Resolution path checked:
  1. [project] ~/horde/horde/.relics/rituals/ ← FOUND
  2. [encampment]    ~/horde/.relics/rituals/
  3. [system]  <embedded>

# Override tier for testing
bd invoke totem-raider-work --tier=system    # Force embedded version
bd invoke totem-raider-work --tier=encampment      # Force encampment version
```

### Future (Totem Mall)

```bash
# Install from Totem Mall
hd totem install totem-code-review-strict
hd totem install totem-code-review-strict@2.0.0
hd totem install hop://acme.corp/rituals/totem-deploy

# Manage installed rituals
hd totem list --installed              # What's in encampment-level
hd totem upgrade totem-raider-work      # Update to latest
hd totem pin totem-raider-work@4.0.0    # Lock version
hd totem uninstall totem-code-review-strict
```

## Migration Path

### Phase 1: Resolution Order (Now)

1. Implement three-tier resolution in `rl invoke`
2. Add `--resolve` flag to show resolution path
3. Update `rl ritual list` to show tiers
4. Fix clan directories (Option B)

### Phase 2: Encampment-Level Rituals

1. Establish `~/horde/.relics/rituals/` as encampment ritual location
2. Add `hd totem` commands for managing encampment rituals
3. Support manual installation (copy file, track in `.installed.json`)

### Phase 3: Mol Mall Integration

1. Define registry API (see totem-mall-design.md)
2. Implement `hd ritual install` from remote
3. Add version pinning and upgrade flows
4. Add integrity verification (checksums, optional signing)

### Phase 4: Federation (HOP)

1. Add capability tags to ritual schema
2. Track ritual execution for agent accountability
3. Enable federation (cross-encampment ritual sharing via Highway Operations Protocol)
4. Author attribution and validation records

## Related Documents

- [Totem Mall Design](totem-mall-design.md) - Registry architecture
- [totems.md](totems.md) - Ritual → Proto → Mol lifecycle
- [understanding-horde.md](../../../docs/understanding-horde.md) - Horde architecture
