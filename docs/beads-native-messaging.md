# Relics-Native Messaging

This document describes the relics-native messaging system for Horde, which replaces the file-based messaging configuration with persistent relics stored in the encampment's `.relics` directory.

## Overview

Relics-native messaging introduces three new bead types for managing communication:

- **Groups** (`hd:group`) - Named collections of addresses for drums distribution
- **Queues** (`hd:queue`) - Work queues where messages can be claimed by workers
- **Channels** (`hd:channel`) - Pub/sub broadcast streams with message retention

All messaging relics use the `hq-` prefix because they are encampment-level entities that span warbands.

## Bead Types

### Groups (`hd:group`)

Groups are named collections of addresses used for drums distribution. When you send to a group, the message is delivered to all members.

**Bead ID format:** `hq-group-<name>` (e.g., `hq-group-ops-team`)

**Fields:**
- `name` - Unique group name
- `members` - Comma-separated list of addresses, patterns, or nested group names
- `created_by` - Who created the group (from BD_ACTOR)
- `created_at` - ISO 8601 timestamp

**Member types:**
- Direct addresses: `horde/clan/max`, `warchief/`, `shaman/`
- Wildcard patterns: `*/witness`, `horde/*`, `horde/clan/*`
- Special patterns: `@encampment`, `@clan`, `@witnesses`
- Nested groups: Reference other group names

### Queues (`hd:queue`)

Queues are work queues where messages wait to be claimed by workers. Unlike groups, each message goes to exactly one claimant.

**Bead ID format:** `hq-q-<name>` (encampment-level) or `hd-q-<name>` (warband-level)

**Fields:**
- `name` - Queue name
- `status` - `active`, `paused`, or `closed`
- `max_concurrency` - Maximum concurrent workers (0 = unlimited)
- `processing_order` - `fifo` or `priority`
- `available_count` - Items ready to process
- `processing_count` - Items currently being processed
- `completed_count` - Items completed
- `failed_count` - Items that failed

### Channels (`hd:channel`)

Channels are pub/sub streams for broadcasting messages. Messages are retained according to the channel's retention policy.

**Bead ID format:** `hq-channel-<name>` (e.g., `hq-channel-alerts`)

**Fields:**
- `name` - Unique channel name
- `subscribers` - Comma-separated list of subscribed addresses
- `status` - `active` or `closed`
- `retention_count` - Number of recent messages to retain (0 = unlimited)
- `retention_hours` - Hours to retain messages (0 = forever)
- `created_by` - Who created the channel
- `created_at` - ISO 8601 timestamp

## CLI Commands

### Group Management

```bash
# List all groups
hd drums group list

# Show group details
hd drums group show <name>

# Create a new group with members
hd drums group create <name> [members...]
hd drums group create ops-team horde/witness horde/clan/max

# Add member to group
hd drums group add <name> <member>

# Remove member from group
hd drums group remove <name> <member>

# Delete a group
hd drums group delete <name>
```

### Channel Management

```bash
# List all channels
hd drums channel
hd drums channel list

# View channel messages
hd drums channel <name>
hd drums channel show <name>

# Create a channel with retention policy
hd drums channel create <name> [--retain-count=N] [--retain-hours=N]
hd drums channel create alerts --retain-count=100

# Delete a channel
hd drums channel delete <name>
```

### Sending Messages

The `hd drums send` command now supports groups, queues, and channels:

```bash
# Send to a group (expands to all members)
hd drums send my-group -s "Subject" -m "Body"

# Send to a queue (single message, workers claim)
hd drums send queue:my-queue -s "Work item" -m "Details"

# Send to a channel (broadcast with retention)
hd drums send channel:my-channel -s "Announcement" -m "Content"

# Direct address (unchanged)
hd drums send horde/clan/max -s "Hello" -m "World"
```

## Address Resolution

When sending drums, addresses are resolved in this order:

1. **Explicit prefix** - If address starts with `group:`, `queue:`, or `channel:`, use that type directly
2. **Contains `/`** - Treat as agent address or pattern (direct delivery)
3. **Starts with `@`** - Special pattern (`@encampment`, `@clan`, etc.) or relics-native group
4. **Name lookup** - Search for group → queue → channel by name

If a name matches multiple types (e.g., both a group and a channel named "alerts"), the resolver returns an error and requires an explicit prefix.

## Key Implementation Files

| File | Description |
|------|-------------|
| `internal/relics/relics_group.go` | Group bead CRUD operations |
| `internal/relics/relics_queue.go` | Queue bead CRUD operations |
| `internal/relics/relics_channel.go` | Channel bead + retention logic |
| `internal/drums/resolve.go` | Address resolution logic |
| `internal/cmd/mail_group.go` | Group CLI commands |
| `internal/cmd/mail_channel.go` | Channel CLI commands |
| `internal/cmd/mail_send.go` | Updated send with resolver |

## Retention Policy

Channels support two retention mechanisms:

- **Count-based** (`--retain-count=N`): Keep only the last N messages
- **Time-based** (`--retain-hours=N`): Delete messages older than N hours

Retention is enforced:
1. **On-write**: After posting a new message, old messages are pruned
2. **On-scout**: Shaman scout runs `PruneAllChannels()` as a backup cleanup

The scout uses a 10% buffer to avoid thrashing (only prunes if count > retainCount × 1.1).

## Examples

### Create a team distribution group

```bash
# Create a group for the ops team
hd drums group create ops-team horde/witness horde/clan/max shaman/

# Send to the group
hd drums send ops-team -s "Team meeting" -m "Tomorrow at 10am"

# Add a new member
hd drums group add ops-team horde/clan/dennis
```

### Set up an alerts channel

```bash
# Create an alerts channel that keeps last 50 messages
hd drums channel create alerts --retain-count=50

# Send an alert
hd drums send channel:alerts -s "Build failed" -m "See CI for details"

# View recent alerts
hd drums channel alerts
```

### Create nested groups

```bash
# Create role-based groups
hd drums group create witnesses */witness
hd drums group create leads horde/clan/max horde/clan/dennis

# Create a group that includes other groups
hd drums group create all-hands witnesses leads warchief/
```
