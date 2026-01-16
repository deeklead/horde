package drums

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/deeklead/horde/internal/relics"
	"github.com/deeklead/horde/internal/runtime"
)

// timeNow is a function that returns the current time. It can be overridden in tests.
var timeNow = time.Now

// Common errors
var (
	ErrMessageNotFound = errors.New("message not found")
	ErrEmptyInbox      = errors.New("inbox is empty")
)

// Wardrums manages messages for an identity via relics.
type Wardrums struct {
	identity string // relics identity (e.g., "horde/raiders/Toast")
	workDir  string // directory to run rl commands in
	relicsDir string // explicit .relics directory path (set via RELICS_DIR)
	path     string // for legacy JSONL mode (clan workers)
	legacy   bool   // true = use JSONL files, false = use relics
}

// NewMailbox creates a wardrums for the given JSONL path (legacy mode).
// Used by clan workers that have local JSONL inboxes.
func NewMailbox(path string) *Wardrums {
	return &Wardrums{
		path:   filepath.Join(path, "inbox.jsonl"),
		legacy: true,
	}
}

// NewMailboxRelics creates a wardrums backed by relics.
func NewMailboxRelics(identity, workDir string) *Wardrums {
	return &Wardrums{
		identity: identity,
		workDir:  workDir,
		legacy:   false,
	}
}

// NewMailboxFromAddress creates a relics-backed wardrums from a GGT address.
// Follows .relics/redirect for clan workers and raiders using shared relics.
func NewMailboxFromAddress(address, workDir string) *Wardrums {
	relicsDir := relics.ResolveRelicsDir(workDir)
	return &Wardrums{
		identity: addressToIdentity(address),
		workDir:  workDir,
		relicsDir: relicsDir,
		legacy:   false,
	}
}

// NewMailboxWithRelicsDir creates a wardrums with an explicit relics directory.
func NewMailboxWithRelicsDir(address, workDir, relicsDir string) *Wardrums {
	return &Wardrums{
		identity: addressToIdentity(address),
		workDir:  workDir,
		relicsDir: relicsDir,
		legacy:   false,
	}
}

// Identity returns the relics identity for this wardrums.
func (m *Wardrums) Identity() string {
	return m.identity
}

// Path returns the JSONL path for legacy mailboxes.
func (m *Wardrums) Path() string {
	return m.path
}

// List returns all open messages in the wardrums.
func (m *Wardrums) List() ([]*Message, error) {
	if m.legacy {
		return m.listLegacy()
	}
	return m.listRelics()
}

func (m *Wardrums) listRelics() ([]*Message, error) {
	// Single query to relics - returns both persistent and wisp messages
	// Wisps are stored in same DB with wisp=true flag, filtered from JSONL export
	messages, err := m.listFromDir(m.relicsDir)
	if err != nil {
		return nil, err
	}

	// Sort by timestamp (newest first)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.After(messages[j].Timestamp)
	})

	return messages, nil
}

// listFromDir queries messages from a relics directory.
// Returns messages where identity is the assignee OR a CC recipient.
// Includes both open and bannered messages (bannered = auto-assigned handoff drums).
// If all queries fail, returns the last error encountered.
func (m *Wardrums) listFromDir(relicsDir string) ([]*Message, error) {
	seen := make(map[string]bool)
	var messages []*Message
	var lastErr error
	anySucceeded := false

	// Get all identity variants to query (handles legacy vs normalized formats)
	identities := m.identityVariants()

	// Query for each identity variant in both open and bannered statuses
	for _, identity := range identities {
		for _, status := range []string{"open", "bannered"} {
			msgs, err := m.queryMessages(relicsDir, "--assignee", identity, status)
			if err != nil {
				lastErr = err
			} else {
				anySucceeded = true
				for _, msg := range msgs {
					if !seen[msg.ID] {
						seen[msg.ID] = true
						messages = append(messages, msg)
					}
				}
			}
		}
	}

	// Query for CC'd messages (open only)
	for _, identity := range identities {
		ccMsgs, err := m.queryMessages(relicsDir, "--label", "cc:"+identity, "open")
		if err != nil {
			lastErr = err
		} else {
			anySucceeded = true
			for _, msg := range ccMsgs {
				if !seen[msg.ID] {
					seen[msg.ID] = true
					messages = append(messages, msg)
				}
			}
		}
	}

	// If ALL queries failed, return the last error
	if !anySucceeded && lastErr != nil {
		return nil, fmt.Errorf("all wardrums queries failed: %w", lastErr)
	}

	return messages, nil
}

// identityVariants returns all identity formats to query.
// For encampment-level agents (warchief/, shaman/), also includes the variant without
// trailing slash for backwards compatibility with legacy messages.
func (m *Wardrums) identityVariants() []string {
	variants := []string{m.identity}

	// Encampment-level agents may have legacy messages without trailing slash
	if m.identity == "warchief/" {
		variants = append(variants, "warchief")
	} else if m.identity == "shaman/" {
		variants = append(variants, "shaman")
	}

	return variants
}

// queryMessages runs a rl list query with the given filter flag and value.
func (m *Wardrums) queryMessages(relicsDir, filterFlag, filterValue, status string) ([]*Message, error) {
	args := []string{"list",
		"--type", "message",
		filterFlag, filterValue,
		"--status", status,
		"--json",
	}

	stdout, err := runBdCommand(args, m.workDir, relicsDir)
	if err != nil {
		return nil, err
	}

	// Parse JSON output
	var relicsMsgs []RelicsMessage
	if err := json.Unmarshal(stdout, &relicsMsgs); err != nil {
		// Empty inbox returns empty array or nothing
		if len(stdout) == 0 || string(stdout) == "null" {
			return nil, nil
		}
		return nil, err
	}

	// Convert to GGT messages - wisp status comes from relics issue.wisp field
	var messages []*Message
	for _, bm := range relicsMsgs {
		messages = append(messages, bm.ToMessage())
	}

	return messages, nil
}

func (m *Wardrums) listLegacy() ([]*Message, error) {
	file, err := os.Open(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }() // non-fatal: OS will close on exit

	var messages []*Message
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue // Skip malformed lines
		}
		messages = append(messages, &msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Sort by timestamp (newest first)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.After(messages[j].Timestamp)
	})

	return messages, nil
}

// ListUnread returns unread (open) messages.
func (m *Wardrums) ListUnread() ([]*Message, error) {
	if m.legacy {
		all, err := m.List()
		if err != nil {
			return nil, err
		}
		var unread []*Message
		for _, msg := range all {
			if !msg.Read {
				unread = append(unread, msg)
			}
		}
		return unread, nil
	}
	// For relics, inbox only returns open (unread) messages
	return m.List()
}

// Get returns a message by ID.
func (m *Wardrums) Get(id string) (*Message, error) {
	if m.legacy {
		return m.getLegacy(id)
	}
	return m.getRelics(id)
}

func (m *Wardrums) getRelics(id string) (*Message, error) {
	// Single DB query - wisps and persistent messages in same store
	return m.getFromDir(id, m.relicsDir)
}

// getFromDir retrieves a message from a relics directory.
func (m *Wardrums) getFromDir(id, relicsDir string) (*Message, error) {
	args := []string{"show", id, "--json"}

	stdout, err := runBdCommand(args, m.workDir, relicsDir)
	if err != nil {
		if bdErr, ok := err.(*bdError); ok && bdErr.ContainsError("not found") {
			return nil, ErrMessageNotFound
		}
		return nil, err
	}

	// rl show --json returns an array
	var bms []RelicsMessage
	if err := json.Unmarshal(stdout, &bms); err != nil {
		return nil, err
	}
	if len(bms) == 0 {
		return nil, ErrMessageNotFound
	}

	// Wisp status comes from relics issue.wisp field via ToMessage()
	return bms[0].ToMessage(), nil
}

func (m *Wardrums) getLegacy(id string) (*Message, error) {
	messages, err := m.List()
	if err != nil {
		return nil, err
	}
	for _, msg := range messages {
		if msg.ID == id {
			return msg, nil
		}
	}
	return nil, ErrMessageNotFound
}

// MarkRead marks a message as read.
func (m *Wardrums) MarkRead(id string) error {
	if m.legacy {
		return m.markReadLegacy(id)
	}
	return m.markReadRelics(id)
}

func (m *Wardrums) markReadRelics(id string) error {
	// Single DB - wisps and persistent messages in same store
	return m.closeInDir(id, m.relicsDir)
}

// closeInDir closes a message in a specific relics directory.
func (m *Wardrums) closeInDir(id, relicsDir string) error {
	args := []string{"close", id}
	// Pass session ID for work attribution if available
	if sessionID := runtime.SessionIDFromEnv(); sessionID != "" {
		args = append(args, "--session="+sessionID)
	}

	_, err := runBdCommand(args, m.workDir, relicsDir)
	if err != nil {
		if bdErr, ok := err.(*bdError); ok && bdErr.ContainsError("not found") {
			return ErrMessageNotFound
		}
		return err
	}

	return nil
}

func (m *Wardrums) markReadLegacy(id string) error {
	messages, err := m.List()
	if err != nil {
		return err
	}

	found := false
	for _, msg := range messages {
		if msg.ID == id {
			msg.Read = true
			found = true
		}
	}

	if !found {
		return ErrMessageNotFound
	}

	return m.rewriteLegacy(messages)
}

// MarkReadOnly marks a message as read WITHOUT archiving/closing it.
// For relics mode, this adds a "read" label to the message.
// For legacy mode, this sets the Read field to true.
// The message remains in the inbox but is displayed as read.
func (m *Wardrums) MarkReadOnly(id string) error {
	if m.legacy {
		return m.markReadLegacy(id)
	}
	return m.markReadOnlyRelics(id)
}

func (m *Wardrums) markReadOnlyRelics(id string) error {
	// Add "read" label to mark as read without closing
	args := []string{"label", "add", id, "read"}

	_, err := runBdCommand(args, m.workDir, m.relicsDir)
	if err != nil {
		if bdErr, ok := err.(*bdError); ok && bdErr.ContainsError("not found") {
			return ErrMessageNotFound
		}
		return err
	}

	return nil
}

// MarkUnreadOnly marks a message as unread (removes "read" label).
// For relics mode, this removes the "read" label from the message.
// For legacy mode, this sets the Read field to false.
func (m *Wardrums) MarkUnreadOnly(id string) error {
	if m.legacy {
		return m.markUnreadLegacy(id)
	}
	return m.markUnreadOnlyRelics(id)
}

func (m *Wardrums) markUnreadOnlyRelics(id string) error {
	// Remove "read" label to mark as unread
	args := []string{"label", "remove", id, "read"}

	_, err := runBdCommand(args, m.workDir, m.relicsDir)
	if err != nil {
		if bdErr, ok := err.(*bdError); ok && bdErr.ContainsError("not found") {
			return ErrMessageNotFound
		}
		// Ignore error if label doesn't exist
		if bdErr, ok := err.(*bdError); ok && bdErr.ContainsError("does not have label") {
			return nil
		}
		return err
	}

	return nil
}

// MarkUnread marks a message as unread (reopens in relics).
func (m *Wardrums) MarkUnread(id string) error {
	if m.legacy {
		return m.markUnreadLegacy(id)
	}
	return m.markUnreadRelics(id)
}

func (m *Wardrums) markUnreadRelics(id string) error {
	args := []string{"reopen", id}

	_, err := runBdCommand(args, m.workDir, m.relicsDir)
	if err != nil {
		if bdErr, ok := err.(*bdError); ok && bdErr.ContainsError("not found") {
			return ErrMessageNotFound
		}
		return err
	}

	return nil
}

func (m *Wardrums) markUnreadLegacy(id string) error {
	messages, err := m.List()
	if err != nil {
		return err
	}

	found := false
	for _, msg := range messages {
		if msg.ID == id {
			msg.Read = false
			found = true
		}
	}

	if !found {
		return ErrMessageNotFound
	}

	return m.rewriteLegacy(messages)
}

// Delete removes a message.
func (m *Wardrums) Delete(id string) error {
	if m.legacy {
		return m.deleteLegacy(id)
	}
	return m.MarkRead(id) // relics: just acknowledge/close
}

func (m *Wardrums) deleteLegacy(id string) error {
	messages, err := m.List()
	if err != nil {
		return err
	}

	var filtered []*Message
	found := false
	for _, msg := range messages {
		if msg.ID == id {
			found = true
		} else {
			filtered = append(filtered, msg)
		}
	}

	if !found {
		return ErrMessageNotFound
	}

	return m.rewriteLegacy(filtered)
}

// Archive moves a message to the archive file and removes it from inbox.
func (m *Wardrums) Archive(id string) error {
	// Get the message first
	msg, err := m.Get(id)
	if err != nil {
		return err
	}

	// Append to archive file
	if err := m.appendToArchive(msg); err != nil {
		return err
	}

	// Delete from inbox
	return m.Delete(id)
}

// ArchivePath returns the path to the archive file.
func (m *Wardrums) ArchivePath() string {
	if m.legacy {
		return m.path + ".archive"
	}
	// For relics, use archive.jsonl in the same directory as relics
	return filepath.Join(m.relicsDir, "archive.jsonl")
}

func (m *Wardrums) appendToArchive(msg *Message) error {
	archivePath := m.ArchivePath()

	// Ensure directory exists
	dir := filepath.Dir(archivePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Open for append
	file, err := os.OpenFile(archivePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gosec // G302: archive is non-sensitive operational data
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = file.WriteString(string(data) + "\n")
	return err
}

// ListArchived returns all messages in the archive file.
func (m *Wardrums) ListArchived() ([]*Message, error) {
	archivePath := m.ArchivePath()

	file, err := os.Open(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var messages []*Message
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue // Skip malformed lines
		}
		messages = append(messages, &msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

// PurgeArchive removes messages from the archive, optionally filtering by age.
// If olderThanDays is 0, removes all archived messages.
func (m *Wardrums) PurgeArchive(olderThanDays int) (int, error) {
	messages, err := m.ListArchived()
	if err != nil {
		return 0, err
	}

	if len(messages) == 0 {
		return 0, nil
	}

	// If no age filter, remove all
	if olderThanDays <= 0 {
		if err := os.Remove(m.ArchivePath()); err != nil && !os.IsNotExist(err) {
			return 0, err
		}
		return len(messages), nil
	}

	// Filter by age
	cutoff := timeNow().AddDate(0, 0, -olderThanDays)
	var keep []*Message
	purged := 0

	for _, msg := range messages {
		if msg.Timestamp.Before(cutoff) {
			purged++
		} else {
			keep = append(keep, msg)
		}
	}

	// Rewrite archive with remaining messages
	if len(keep) == 0 {
		if err := os.Remove(m.ArchivePath()); err != nil && !os.IsNotExist(err) {
			return 0, err
		}
	} else {
		if err := m.rewriteArchive(keep); err != nil {
			return 0, err
		}
	}

	return purged, nil
}

func (m *Wardrums) rewriteArchive(messages []*Message) error {
	archivePath := m.ArchivePath()
	tmpPath := archivePath + ".tmp"

	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			_ = file.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		_, _ = file.WriteString(string(data) + "\n")
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, archivePath)
}

// SearchOptions specifies search parameters.
type SearchOptions struct {
	Query       string // Regex pattern to search for
	FromFilter  string // Optional: only match messages from this sender
	SubjectOnly bool   // Only search subject
	BodyOnly    bool   // Only search body
}

// Search finds messages matching the given criteria.
// Returns messages from both inbox and archive.
// Query and FromFilter are treated as literal strings (not regex) to prevent ReDoS.
func (m *Wardrums) Search(opts SearchOptions) ([]*Message, error) {
	// Use QuoteMeta to escape special regex chars - prevents ReDoS attacks
	// and provides intuitive literal string matching for users
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(opts.Query))
	if err != nil {
		return nil, fmt.Errorf("invalid search pattern: %w", err)
	}

	var fromRe *regexp.Regexp
	if opts.FromFilter != "" {
		fromRe, err = regexp.Compile("(?i)" + regexp.QuoteMeta(opts.FromFilter))
		if err != nil {
			return nil, fmt.Errorf("invalid from pattern: %w", err)
		}
	}

	// Get inbox messages
	inbox, err := m.List()
	if err != nil {
		return nil, err
	}

	// Get archived messages
	archived, err := m.ListArchived()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Combine and search
	all := append(inbox, archived...)
	var matches []*Message

	for _, msg := range all {
		// Apply from filter
		if fromRe != nil && !fromRe.MatchString(msg.From) {
			continue
		}

		// Search in specified fields
		matched := false
		if opts.SubjectOnly {
			matched = re.MatchString(msg.Subject)
		} else if opts.BodyOnly {
			matched = re.MatchString(msg.Body)
		} else {
			// Search in both subject and body
			matched = re.MatchString(msg.Subject) || re.MatchString(msg.Body)
		}

		if matched {
			matches = append(matches, msg)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Timestamp.After(matches[j].Timestamp)
	})

	return matches, nil
}

// Count returns the total and unread message counts.
func (m *Wardrums) Count() (total, unread int, err error) {
	messages, err := m.List()
	if err != nil {
		return 0, 0, err
	}

	total = len(messages)
	// Count messages that are NOT marked as read (including via "read" label)
	for _, msg := range messages {
		if !msg.Read {
			unread++
		}
	}

	return total, unread, nil
}

// Append adds a message to the wardrums (legacy mode only).
// For relics mode, use Router.Send() instead.
func (m *Wardrums) Append(msg *Message) error {
	if !m.legacy {
		return errors.New("use Router.Send() to send messages via relics")
	}
	return m.appendLegacy(msg)
}

func (m *Wardrums) appendLegacy(msg *Message) error {
	// Ensure directory exists
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Open for append
	file, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }() // non-fatal: OS will close on exit

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = file.WriteString(string(data) + "\n")
	return err
}

// rewriteLegacy rewrites the wardrums with the given messages.
func (m *Wardrums) rewriteLegacy(messages []*Message) error {
	// Sort by timestamp (oldest first for JSONL)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	// Write to temp file
	tmpPath := m.path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			_ = file.Close()         // best-effort cleanup
			_ = os.Remove(tmpPath)   // best-effort cleanup
			return err
		}
		_, _ = file.WriteString(string(data) + "\n") // non-fatal: partial write is acceptable
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath) // best-effort cleanup
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, m.path)
}

// ListByThread returns all messages in a given thread.
func (m *Wardrums) ListByThread(threadID string) ([]*Message, error) {
	if m.legacy {
		return m.listByThreadLegacy(threadID)
	}
	return m.listByThreadRelics(threadID)
}

func (m *Wardrums) listByThreadRelics(threadID string) ([]*Message, error) {
	args := []string{"message", "thread", threadID, "--json"}

	stdout, err := runBdCommand(args, m.workDir, m.relicsDir, "BD_IDENTITY="+m.identity)
	if err != nil {
		return nil, err
	}

	var relicsMsgs []RelicsMessage
	if err := json.Unmarshal(stdout, &relicsMsgs); err != nil {
		if len(stdout) == 0 || string(stdout) == "null" {
			return nil, nil
		}
		return nil, err
	}

	var messages []*Message
	for _, bm := range relicsMsgs {
		messages = append(messages, bm.ToMessage())
	}

	// Sort by timestamp (oldest first for thread view)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	return messages, nil
}

func (m *Wardrums) listByThreadLegacy(threadID string) ([]*Message, error) {
	messages, err := m.List()
	if err != nil {
		return nil, err
	}

	var thread []*Message
	for _, msg := range messages {
		if msg.ThreadID == threadID {
			thread = append(thread, msg)
		}
	}

	// Sort by timestamp (oldest first for thread view)
	sort.Slice(thread, func(i, j int) bool {
		return thread[i].Timestamp.Before(thread[j].Timestamp)
	})

	return thread, nil
}
