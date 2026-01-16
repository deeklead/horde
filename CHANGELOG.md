# Changelog

All notable changes to Horde will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-01-16

### Added

- Initial release of Horde multi-agent workspace manager
- Warchief agent for high-level coordination and task distribution
- Witness agent for monitoring and verification
- Forge agent for merge queue management
- Raider agents for autonomous task execution
- Clan workers for persistent development tasks
- Shaman agent for system maintenance
- Drums messaging system for inter-agent communication
- Relics integration for issue tracking
- Tmux-based session management
- Web dashboard for monitoring
- npm package for easy installation

## [0.2.6] - 2026-01-12

### Added

#### Escalation System
- **Unified escalation system** - Complete escalation implementation with severity levels, routing, and tracking (gt-i9r20)
- **Escalation config schema alignment** - Configuration now matches design doc specifications

#### Agent Identity & Management
- **`hd raider identity` subcommand group** - Agent bead management commands for raider lifecycle
- **AGENTS.md fallback copy** - Raiders automatically copy AGENTS.md from warchief/warband for context bootstrapping
- **`--debug` flag for `hd clan at`** - Debug mode for clan attachment troubleshooting
- **Boot role detection in priming** - Proper context injection for boot role agents (#370)

#### Statusline Improvements
- **Per-agent-type health tracking** - Statusline now shows health status per agent type (#344)
- **Visual warband grouping** - Warbands sorted by activity with visual grouping in tmux statusline (#337)

#### Drums & Communication
- **`hd drums show` alias** - Alternative command for reading drums (#340)

#### Developer Experience
- **`hd stale` command** - Check for stale binaries and version mismatches

### Changed

- **Refactored statusline** - Merged session loops and removed dead code for cleaner implementation
- **Refactored charge.go** - Split 1560-line file into 7 focused modules for maintainability
- **Magic numbers extracted** - Suggest package now uses named constants (#353)

### Fixed

#### Configuration & Environment
- **Empty GT_ROOT/RELICS_DIR not exported** - AgentEnv no longer exports empty environment variables (#385)
- **Inherited RELICS_DIR prefix mismatch** - Prevent inherited RELICS_DIR from causing prefix mismatches (#321)

#### Relics & Routing
- **routes.jsonl corruption prevention** - Added protection against routes.jsonl corruption with doctor check for warband-level issues (#377)
- **Tracked relics init after clone** - Initialize relics database for tracked relics after git clone (#376)
- **Warband root from RelicsPath()** - Correctly return warband root to respect redirect system

#### Charge & Ritual
- **Feature and issue vars in ritual-on-bead mode** - Pass both variables correctly (#382)
- **Clan member shorthand resolution** - Resolve clan members correctly with shorthand paths
- **Removed obsolete --naked flag** - Cleanup of deprecated charge option

#### Doctor & Diagnostics
- **Role relics check with shared definitions** - Doctor now validates role relics using shared role definitions (#378)
- **Filter rl "Note:" messages** - Custom types check no longer confused by rl informational output (#381)

#### Installation & Setup
- **gt:role label on role relics** - Role relics now properly labeled during creation (#383)
- **Fetch origin after refspec config** - Bare clones now fetch after configuring refspec (#384)
- **Allow --wrappers in existing encampment** - No longer recreates HQ unnecessarily (#366)

#### Session & Lifecycle
- **Fallback instructions in start/restart beacons** - Session beacons now include fallback instructions
- **Handoff recognizes raider session pattern** - Correctly handles gt-<warband>-<name> session names (#373)
- **hd done resilient to missing agent relics** - No longer fails when agent relics don't exist
- **MR relics as ephemeral wisps** - Create MR relics as ephemeral wisps for proper cleanup
- **Auto-detect cleanup status** - Prevents premature raider nuke (#361)
- **Delete remote raider branches after merge** - Forge cleans up remote branches (#369)

#### Costs & Events
- **Query all relics locations for session events** - Cost tracking finds events across locations (#374)

#### Linting & Quality
- **errcheck and unparam violations resolved** - Fixed linting errors
- **SignalSession for all agent notifications** - Drums now uses consistent notification method

### Documentation

- **Raider three-state model** - Clarified working/stalled/zombie states
- **Name pool vs raider pool** - Clarified misconception about pools
- **Plugin and escalation system designs** - Added design documentation
- **Documentation reorganization** - Concepts, design, and examples structure
- **gt rally clarification** - Clarified that hd rally is context recovery, not session start (GH #308)
- **Ritual package documentation** - Comprehensive package docs
- **Various godoc additions** - GenerateMRIDWithTime, isAutonomousRole, formatInt, nil sentinel pattern
- **Relics issue ID format** - Clarified format in README (gt-uzx2c)
- **Stale raider identity description** - Fixed outdated documentation

### Tests

- **AGENTS.md worktree tests** - Test coverage for AGENTS.md in worktrees
- **Comprehensive test coverage** - Added tests for 5 packages (#351)
- **Charge test for rl empty output** - Fixed test for empty output handling

### Deprecated

- **`hd raider add`** - Added migration warning for deprecated command

### Contributors

Thanks to all contributors for this release:
- @JeremyKalmus - Various contributions (#364)
- @boshu2 - Ritual package documentation (#343), PR documentation (#352)
- @sauerdaniel - Raider drums notification fix (#347)
- @abhijit360 - Assign model to role (#368)
- @julianknutsen - Relics path fix (#334)

## [0.2.5] - 2026-01-11

### Added
- **`hd drums mark-read`** - Mark messages as read without opening them (desire path)
- **`hd down --raiders`** - Shut down raiders without affecting other components
- **Self-cleaning raider model** - Raiders self-nuke on completion, witness tracks leases
- **`hd rally --state` validation** - Flag exclusivity checks for cleaner CLI

### Changed
- **Removed `hd stop`** - Use `hd down --raiders` instead (cleaner semantics)
- **Policy-neutral templates** - clan.md.tmpl checks remote origin for PR policy
- **Refactored rally.go** - Split 1833-line file into logical modules

### Fixed
- **Raider re-muster** - CreateOrReopenAgentBead handles raider lifecycle correctly (#333)
- **Vim mode compatibility** - tmux sends Escape before Enter for vim users
- **Worktree default branch** - Uses warband's configured default branch (#325)
- **Agent bead type** - Sets --type=agent when creating agent relics
- **Bootstrap priming** - Reduced AGENTS.md to bootstrap pointer, fixed CLAUDE.md templates

### Documentation
- Updated witness help text for self-cleaning model
- Updated daemon comments for self-cleaning model
- Policy-aware PR guidance in clan template

## [0.2.4] - 2026-01-10

Priming subsystem overhaul and Zero Framework Cognition (ZFC) improvements.

### Added

#### Priming Subsystem
- **RALLY.md provisioning** - Auto-provision RALLY.md at warband level so all workers inherit Horde context (GUPP, hooks, propulsion) (#hq-5z76w)
- **Post-handoff detection** - `hd rally` detects handoff marker and outputs "HANDOFF COMPLETE" warning to prevent handoff loop bug (#hq-ukjrr)
- **Priming health checks** - `hd doctor` validates priming subsystem: SessionStart hook, hd rally command, RALLY.md presence, CLAUDE.md size (#hq-5scnt)
- **`hd rally --dry-run`** - Preview priming without side effects
- **`hd rally --state`** - Output session state (normal, post-handoff, crash-recovery, autonomous)
- **`hd rally --explain`** - Add [EXPLAIN] tags for debugging priming decisions

#### Ritual & Configuration
- **Warband-level default rituals** - Configure default ritual at warband level (#297)
- **Witness --agent/--env overrides** - Override agent and environment variables for witness (#293, #294)

#### Developer Experience
- **UX system import** - Comprehensive UX system from relics (#311)
- **Explicit handoff instructions** - Clearer signal message for handoff recipients

### Fixed

#### Zero Framework Cognition (ZFC)
- **Query tmux directly** - Remove marker TTL, query tmux for agent state
- **Remove PID-based detection** - Agent liveness from tmux, not PIDs
- **Agent-controlled thresholds** - Stuck detection moved to agent config
- **Remove pending.json tracking** - Eliminated anti-pattern
- **Derive state from files** - ZFC state from filesystem, not memory cache
- **Remove Go-side computation** - No stderr parsing violations

#### Hooks & Relics
- **Cross-level hook visibility** - Planted relics visible to warchief/shaman (#aeb4c0d)
- **Warn on closed bannered bead** - Alert when bannered bead already closed (#2f50a59)
- **Correct agent bead ID format** - Fix rl create flags for agent relics (#c4fcdd8)

#### Ritual
- **rigPath fallback** - Set rigPath when falling back to horde default (#afb944f)

#### Doctor
- **Full AgentEnv for env-vars check** - Use complete environment for validation (#ce231a3)

### Changed

- **Refactored relics/drums modules** - Split large files into focused modules for maintainability

## [0.2.3] - 2026-01-09

Worker safety release - prevents accidental termination of active agents.

> **Note**: The Shaman safety improvements are believed to be correct but have not
> yet been extensively tested in production. We recommend running with
> `hd shaman pause` initially and monitoring behavior before enabling full scout.
> Please report any issues. A 0.3.0 release will follow once these changes are
> battle-tested.

### Critical Safety Improvements

- **Kill authority removed from Shaman** - Shaman scout now only detects zombies via `--dry-run`, never kills directly. Death warrants are filed for Boot to handle interrogation/execution. This prevents destruction of worker context, mid-task progress, and unsaved state (#gt-vhaej)
- **Bulletproof pause mechanism** - Multi-layer pause for Shaman with file-based state, `hd shaman pause/resume` commands, and guards in `hd rally` and heartbeat (#265)
- **Doctor warns instead of killing** - `hd doctor` now warns about stale encampment-root settings rather than killing sessions (#243)
- **Orphan process check informational** - Doctor's orphan process detection is now informational only, not actionable (#272)

### Added

- **`hd account switch` command** - Switch between Claude Code accounts with `hd account switch <handle>`. Manages `~/.claude` symlinks and updates default account
- **`hd clan list --all`** - Show all clan members across all warbands (#276)
- **Warband-level custom agent support** - Configure different agents per-warband (#12)
- **Warband identity relics check** - Doctor validates warband identity relics exist
- **GT_ROOT env var** - Set for all agent sessions for consistent environment
- **New agent presets** - Added Cursor, Auggie (Augment Code), and Sourcegraph AMP as built-in agent presets (#247)
- **Context Management docs** - Added to Witness template for better context handling (gt-jjama)

### Fixed

- **`hd rally --hook` recognized** - Doctor now recognizes `hd rally --hook` as valid session hook config (#14)
- **Integration test reliability** - Improved test stability (#13)
- **IsClaudeRunning detection** - Now detects 'claude' and version patterns correctly (#273)
- **Shaman heartbeat restored** - `ensureShamanRunning` restored to heartbeat using Manager pattern (#271)
- **Shaman session names** - Correct session name references in rituals (#270)
- **Hidden directory scanning** - Ignore `.claude` and other dot directories when enumerating raiders (#258, #279)
- **SetupRedirect tracked relics** - Works correctly with tracked relics architecture where canonical location is `warchief/warband/.relics`
- **Tmux shell ready** - Wait for shell ready before sending keys (#264)
- **Horde prefix derivation** - Correctly derive `gt-` prefix for horde compound words (gt-m46bb)
- **Custom relics types** - Register custom relics types during install (#250)

### Changed

- **Forge Manager pattern** - Replaced `ensureForgeSession` with `forge.Manager.Start()` for consistency

### Removed

- **Unused ritual JSON** - Removed unused JSON ritual file (cleanup)

### Contributors

Thanks to all contributors for this release:
- @julianknutsen - Doctor fixes (#14, #271, #272, #273), ritual fixes (#270), GT_ROOT env (#268)
- @joshuavial - Hidden directory scanning (#258, #279), clan list --all (#276)

## [0.2.2] - 2026-01-07

Warband operational state management, unified agent startup, and extensive stability fixes.

### Added

#### Warband Operational State Management
- **`hd warband park/unpark` commands** - Level 1 warband control: pause daemon auto-start while preserving sessions
- **`hd warband dock/undock` commands** - Level 2 warband control: stop all sessions and prevent auto-start (gt-9gm9n)
- **`hd warband config` commands** - Per-warband configuration management (gt-hhmkq)
- **Warband identity relics** - Schema and creation for warband identity tracking (gt-zmznh)
- **Property layer lookup** - Hierarchical configuration resolution (gt-emh1c)
- **Operational state in status** - `hd warband status` shows park/dock state

#### Agent Configuration & Startup
- **`--agent` overrides** - Override agent for start/summon/charge commands
- **Unified agent startup** - Manager pattern for consistent agent initialization
- **Claude settings installation** - Auto-install during warband and HQ creation
- **Runtime-aware tmux checks** - Detect actual agent state from tmux sessions

#### Status & Monitoring
- **`hd status --watch`** - Watch mode with auto-refresh (#231)
- **Compact status output** - One-line-per-worker format as new default
- **LED status indicators** - Visual indicators for warbands in Warchief tmux status line
- **Parked/docked indicators** - Pause emoji (⏸) for inactive warbands in statusline

#### Relics & Workflow
- **Minimum relics version check** - Validates relics CLI compatibility (gt-im3fl)
- **ZFC raid auto-close** - `rl close` triggers raid completion (gt-3qw5s)
- **Stale bannered bead cleanup** - Shaman clears orphaned hooks (gt-2yls3)
- **Doctor prefix mismatch detection** - Detect misconfigured warband prefixes (gt-17wdl)
- **Unified relics redirect** - Single redirect system for tracked and local relics (#222)
- **Route from warband to encampment relics** - Cross-level bead routing

#### Infrastructure
- **Windows-compatible file locking** - Daemon lock works on Windows
- **`--purge` flag for clans** - Full clan obliteration option
- **Debug logging for suppressed errors** - Better visibility into startup issues (gt-6d7eh)
- **hq- prefix in tmux cycle bindings** - Navigate to Warchief/Shaman sessions
- **Wisp config storage layer** - Transient/local settings for ephemeral workflows
- **Sparse checkout** - Exclude Claude context files from source repos

### Changed

- **Daemon respects warband operational state** - Parked/docked warbands not auto-started
- **Agent startup unified** - Manager pattern replaces ad-hoc initialization
- **Warchief files moved** - Reorganized into `warchief/` subdirectory
- **Forge merges local branches** - No longer fetches from origin (gt-cio03)
- **Raiders start from origin/default-branch** - Consistent recycled state
- **Observable states removed** - Discover agent state from tmux, don't track (gt-zecmc)
- **totem-encampment-shutdown v3** - Complete cleanup ritual (gt-ux23f)
- **Witness delays raider cleanup** - Wait until MR merges (gt-12hwb)
- **Signal on divergence** - Daemon nudges agents instead of silent accept
- **README rewritten** - Comprehensive guides and architecture docs (#226)
- **`hd warbands` → `hd warband list`** - Command renamed in templates/docs (#217)

### Fixed

#### Doctor & Lifecycle
- **`--restart-sessions` flag required** - Doctor won't cycle sessions without explicit flag (gt-j44ri)
- **Only cycle scout roles** - Doctor --fix doesn't restart clan/raiders (hq-qthgye)
- **Session-ended events auto-closed** - Prevent accumulation (gt-8tc1v)
- **GUPP propulsion signal** - Added to daemon restartSession

#### Charge & Relics
- **Charge uses rl native routing** - No RELICS_DIR override needed
- **Charge parses wisp JSON correctly** - Handle `new_epic_id` field
- **Charge resolves warband path** - Cross-warband bead hooking works
- **Charge waits for Claude ready** - Don't signal until session responsive (#146)
- **Correct relics database for charge** - Warband-level relics used (gt-n5gga)
- **Close bannered relics before clearing** - Proper cleanup order (gt-vwjz6)
- **Removed dead charge flags** - `--totem` and `--quality` cleaned up

#### Agent Sessions
- **Witness kills tmux on Stop()** - Clean session termination
- **Shaman uses session package** - Correct hq- session names (gt-r38pj)
- **Honor warband agent for witness/forge** - Respect per-warband settings
- **Canonical hq role bead IDs** - Consistent naming
- **hq- prefix in status display** - Global agents shown correctly (gt-vcvyd)
- **Restart Claude when dead** - Recover sessions where tmux exists but Claude died
- **Encampment session cycling** - Works from any directory

#### Raider & Clan
- **Nuke not blocked by stale hooks** - Closed relics don't prevent cleanup (gt-jc7bq)
- **Clan stop dry-run support** - Preview cleanup before executing (gt-kjcx4)
- **Clan defaults to --all** - `hd clan start <warband>` starts all clan (gt-s8mpt)
- **Raider cleanup handlers** - `hd witness process` invokes handlers (gt-h3gzj)

#### Daemon & Configuration
- **Create warchief/daemon.json** - `hd start` and `hd doctor --fix` initialize daemon state (#225)
- **Initialize git before relics** - Enable repo fingerprint (#180)
- **Handoff preserves env vars** - Claude Code environment not lost (#216)
- **Agent settings passed correctly** - Witness and daemon respawn use rigPath
- **Log warband discovery errors** - Don't silently swallow (gt-rsnj9)

#### Forge & Merge Queue
- **Use warband's default_branch** - Not hardcoded 'main'
- **MERGE_FAILED sent to Witness** - Proper failure notification
- **Removed BranchPushedToRemote checks** - Local-only workflow support (gt-dymy5)

#### Misc Fixes
- **RelicsSetupRedirect preserves tracked files** - Don't clobber existing files (gt-fj0ol)
- **PATH export in hooks** - Ensure commands find binaries
- **Replace panic with fallback** - ID generation gracefully degrades (#213)
- **Removed duplicate WorktreeAddFromRef** - Code cleanup
- **Encampment root relics for Shaman** - Use correct relics location (gt-sstg)

### Refactored

- **AgentStateManager pattern** - Shared state management extracted (gt-gaw8e)
- **CleanupStatus type** - Replace raw strings (gt-77gq7)
- **ExecWithOutput utility** - Common command execution (gt-vurfr)
- **runBdCommand helper** - DRY drums package (gt-8i6bg)
- **Config expansion helper** - Generic DRY config (gt-i85sg)

### Documentation

- **Property layers guide** - Implementation documentation
- **Worktree architecture** - Clarified relics routing
- **Agent config** - Onboarding docs mention --agent overrides
- **Raider Operations section** - Added to Warchief docs (#140)

### Contributors

Thanks to all contributors for this release:
- @julianknutsen - Claude settings inheritance (#239)
- @joshuavial - Charge wisp JSON parse (#238)
- @michaellady - Unified relics redirect (#222), daemon.json fix (#225)
- @greghughespdx - PATH in hooks fix (#139)

## [0.2.1] - 2026-01-05

Bug fixes, security hardening, and new `hd config` command.

### Added

- **`hd config` command** - Manage agent settings (model, provider) per-warband or globally
- **`hq-` prefix for scout sessions** - Warchief and Shaman sessions use encampment-prefixed names
- **Doctor hooks-path check** - Verify Git hooks path is configured correctly
- **Block internal PRs** - Pre-push hook and GitHub Action prevent accidental internal PRs (#117)
- **Dispatcher notifications** - Notify dispatcher when raider work completes
- **Unit tests** - Added tests for `formatTrackBeadID` helper, done redirect, banner slot E2E

### Fixed

#### Security
- **Command injection prevention** - Validate relics prefix to prevent injection (gt-l1xsa)
- **Path traversal prevention** - Validate clan names to prevent traversal (gt-wzxwm)
- **ReDoS prevention** - Escape user input in drums search (gt-qysj9)
- **Error handling** - Handle crypto/rand.Read errors in ID generation

#### Raid & Charge
- **Hook slot initialization** - Set banner slot when creating agent relics during charge (#124)
- **Cross-warband bead formatting** - Format cross-warband relics as external refs in raid tracking (#123)
- **Reliable rl calls** - Add `--no-daemon` and `RELICS_DIR` for reliable relics operations

#### Warband Inference
- **`hd warband status`** - Infer warband name from current working directory
- **`hd clan start --all`** - Infer warband from cwd for batch clan starts
- **`hd rally` in clan start** - Pass as initial prompt in clan start commands
- **Encampment default_agent** - Honor default agent setting for Warchief and Shaman

#### Session & Lifecycle
- **Hook persistence** - Hook persists across session interruption via `in_progress` lookup (gt-ttn3h)
- **Raider cleanup** - Clean up stale worktrees and git tracking
- **`hd done` redirect** - Use ResolveRelicsDir for redirect file support

#### Build & CI
- **Embedded rituals** - Sync and commit rituals for `go install @latest`
- **CI lint fixes** - Resolve lint and build errors
- **Flaky test fix** - Sync database before relics integration tests

## [0.2.0] - 2026-01-04

Major release featuring the Raid Warmap, two-level relics architecture, and significant multi-agent improvements.

### Added

#### Raid Warmap (Web UI)
- **`hd warmap` command** - Launch web-based monitoring UI for Horde (#71)
- **Raider Workers section** - Real-time activity monitoring with tmux session timestamps
- **Forge Merge Queue display** - Always-visible MR queue status
- **Dynamic work status** - Raid status columns with live updates
- **HTMX auto-refresh** - 10-second refresh interval for real-time monitoring

#### Two-Level Relics Architecture
- **Encampment-level relics** (`~/horde/.relics/`) - `hq-*` prefix for Warchief drums and cross-warband coordination
- **Warband-level relics** - Project-specific issues with warband prefixes (e.g., `gt-*`)
- **`hd migrate-agents` command** - Migration tool for two-level architecture (#nnub1)
- **TownRelicsPrefix constant** - Centralized `hq-` prefix handling
- **Prefix-based routing** - Commands auto-route to correct warband via `routes.jsonl`

#### Multi-Agent Support
- **Pluggable agent registry** - Multi-agent support with configurable providers (#107)
- **Multi-warband management** - `hd warband start/stop/restart/status` for batch operations (#11z8l)
- **`hd clan stop` command** - Stop clan sessions cleanly
- **`muster` alias** - Alternative to `start` for all role subcommands
- **Batch charging** - `hd charge` supports multiple relics to a warband in one command (#l9toz)

#### Ephemeral Raider Model
- **Immediate recycling** - Raiders recycled after each work unit (#81)
- **Updated scout ritual** - Witness ritual adapted for ephemeral model
- **`totem-raider-work` ritual** - Updated for ephemeral raider lifecycle (#si8rq.4)

#### Cost Tracking
- **`hd costs` command** - Session cost tracking and reporting
- **Relics-based storage** - Costs stored in relics instead of JSONL (#f7jxr)
- **Stop hook integration** - Auto-record costs on session end
- **Tmux session auto-detection** - Costs hook finds correct session

#### Conflict Resolution
- **Conflict resolution workflow** - Ritual-based conflict handling for raiders (#si8rq.5)
- **Merge-slot gate** - Forge integration for ordered conflict resolution
- **`hd done --phase-complete`** - Gate-based phase handoffs (#si8rq.7)

#### Communication & Coordination
- **`hd drums archive` multi-ID** - Archive multiple messages at once (#82)
- **`hd drums --all` flag** - Clear all drums for agent ergonomics (#105q3)
- **Raid stranded detection** - Detect and feed stranded raids (#8otmd)
- **`hd raid --tree`** - Show raid + child status tree
- **`hd raid check`** - Cross-warband auto-close for completed raids (#00qjk)

#### Developer Experience
- **Shell completion** - Installation instructions for bash/zsh/fish (#pdrh0)
- **`hd rally --hook`** - LLM runtime session handling flag
- **`hd doctor` enhancements** - Session-hooks check, repo-fingerprint validation (#nrgm5)
- **Binary age detection** - `hd status` shows stale binary warnings (#42whv)
- **Circuit breaker** - Automatic handling for stuck agents (#72cqu)

#### Infrastructure
- **SessionStart hooks** - Deployed during `hd install` for Warchief role
- **`hq-dog-role` relics** - Encampment-level dog role initialization (#2jjry)
- **Watchdog chain docs** - Boot/Shaman lifecycle documentation (#1847v)
- **Integration tests** - CI workflow for `hd install` and `hd warband add` (#htlmp)
- **Local repo reference clones** - Save disk space with `--reference` cloning

### Changed

- **Handoff migrated to skills** - `hd handoff` now uses skills format (#nqtqp)
- **Clan workers push to main** - Documentation clarifies no PR workflow for clan
- **Session names include encampment** - Warchief/Shaman sessions use encampment-prefixed names
- **Ritual semantics clarified** - Rituals are templates, not instructions
- **Witness reports stopped** - No more routine Warchief reports (saves tokens)

### Fixed

#### Daemon & Session Stability
- **Thread-safety** - Added locks for agent session resume support
- **Orphan daemon prevention** - File locking prevents duplicate daemons (#108)
- **Zombie tmux cleanup** - Kill zombie sessions before recreating (#vve6k)
- **Tmux exact matching** - `HasSession` uses exact match to prevent prefix collisions
- **Health check fallback** - Prevents killing healthy sessions on tmux errors

#### Relics Integration
- **Warchief/warband path** - Use correct path for relics to prevent prefix mismatch (#38)
- **Agent bead creation** - Fixed during `hd warband add` (#32)
- **bd daemon startup** - Circuit breaker and restart logic (#2f0p3)
- **RELICS_DIR environment** - Correctly set for raider hooks and cross-warband work

#### Agent Workflows
- **Default branch detection** - `hd done` no longer hardcodes 'main' (#42)
- **Enter key retry** - Reliable Enter key delivery with retry logic (#53)
- **SendKeys debounce** - Increased to 500ms for reliability
- **MR bead closure** - Close relics after successful merge from queue (#52)

#### Installation & Setup
- **Embedded rituals** - Copy rituals to new installations (#86)
- **Vestigial cleanup** - Remove `warbands/` directory and `state.json` files
- **Symlink preservation** - Workspace detection preserves symlink paths (#3, #75)
- **Golangci-lint errors** - Resolved errcheck and gosec issues (#76)

### Contributors

Thanks to all contributors for this release:
- @kiwiupover - README updates (#109)
- @michaellady - Raid warmap (#71), ResolveRelicsDir fix (#54)
- @jsamuel1 - Dependency updates (#83)
- @dannomayernotabot - Witness fixes (#87), daemon race condition (#64)
- @markov-kernel - Warchief session hooks (#93), daemon init recommendation (#95)
- @rawwerks - Multi-agent support (#107)
- @jakehemmerle - Daemon orphan race condition (#108)
- @danshapiro - Install role slots (#106), warband relics dir (#61)
- @vessenes - Encampment session helpers (#91), install copy rituals (#86)
- @kustrun - Init bugs (#34)
- @austeane - README quickstart fix (#44)
- @Avyukth - Scout roles per-warband check (#26)

## [0.1.1] - 2026-01-02

### Fixed

- **Tmux keybindings scoped to Horde sessions** - C-b n/p no longer override default tmux behavior in non-GT sessions (#13)

### Added

- **OSS project files** - CHANGELOG.md, .golangci.yml, RELEASING.md
- **Version bump script** - `scripts/bump-version.sh` for releases
- **Documentation fixes** - Corrected `hd warband add` and `hd clan add` CLI syntax (#6)
- **Warband prefix routing** - Agent relics now use correct warband-specific prefixes (#11)
- **Relics init fix** - Warband relics initialization targets correct database (#9)

## [0.1.0] - 2026-01-02

### Added

Initial public release of Horde - a multi-agent workspace manager for Claude Code.

#### Core Architecture
- **Encampment structure** - Hierarchical workspace with warbands, clans, and raiders
- **Warband management** - `hd warband add/list/remove` for project containers
- **Clan workspaces** - `hd clan add` for persistent developer workspaces
- **Raider workers** - Transient agent workers managed by Witness

#### Agent Roles
- **Warchief** - Global coordinator for cross-warband work
- **Shaman** - Encampment-level lifecycle scout and heartbeat
- **Witness** - Per-warband raider lifecycle manager
- **Forge** - Merge queue processor with code review
- **Clan** - Persistent developer workspaces
- **Raider** - Transient worker agents

#### Work Management
- **Raid system** - `hd raid create/list/status` for tracking related work
- **Charge workflow** - `hd charge <bead> <warband>` to assign work to agents
- **Hook mechanism** - Work attached to agent hooks for pickup
- **Totem workflows** - Ritual-based multi-step task execution

#### Communication
- **Drums system** - `hd drums inbox/send/read` for agent messaging
- **Escalation protocol** - `hd escalate` with severity levels
- **Handoff mechanism** - `hd handoff` for context-preserving session cycling

#### Integration
- **Relics integration** - Issue tracking via relics (`bd` commands)
- **Tmux sessions** - Agent sessions in tmux with theming
- **GitHub CLI** - PR creation and merge queue via `gh`

#### Developer Experience
- **Status warmap** - `hd status` for encampment overview
- **Session cycling** - `C-b n/p` to navigate between agents
- **Activity feed** - `hd feed` for real-time event stream
- **Signal system** - `hd signal` for reliable message delivery to sessions

### Infrastructure
- **Daemon mode** - Background lifecycle management
- **npm package** - Cross-platform binary distribution
- **GitHub Actions** - CI/CD workflows for releases
- **GoReleaser** - Multi-platform binary builds
