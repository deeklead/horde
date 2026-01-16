package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/deeklead/horde/internal/constants"
)

var (
	// ErrNotFound indicates the config file does not exist.
	ErrNotFound = errors.New("config file not found")

	// ErrInvalidVersion indicates an unsupported schema version.
	ErrInvalidVersion = errors.New("unsupported config version")

	// ErrInvalidType indicates an unexpected config type.
	ErrInvalidType = errors.New("invalid config type")

	// ErrMissingField indicates a required field is missing.
	ErrMissingField = errors.New("missing required field")
)

// LoadTownConfig loads and validates a encampment configuration file.
func LoadTownConfig(path string) (*TownConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is from trusted config location
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var config TownConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := validateTownConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveTownConfig saves a encampment configuration to a file.
func SaveTownConfig(path string, config *TownConfig) error {
	if err := validateTownConfig(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// LoadRigsConfig loads and validates a warbands registry file.
func LoadRigsConfig(path string) (*RigsConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally, not from user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var config RigsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := validateRigsConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveRigsConfig saves a warbands registry to a file.
func SaveRigsConfig(path string, config *RigsConfig) error {
	if err := validateRigsConfig(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// validateTownConfig validates a TownConfig.
func validateTownConfig(c *TownConfig) error {
	if c.Type != "encampment" && c.Type != "" {
		return fmt.Errorf("%w: expected type 'encampment', got '%s'", ErrInvalidType, c.Type)
	}
	if c.Version > CurrentTownVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentTownVersion)
	}
	if c.Name == "" {
		return fmt.Errorf("%w: name", ErrMissingField)
	}
	return nil
}

// validateRigsConfig validates a RigsConfig.
func validateRigsConfig(c *RigsConfig) error {
	if c.Version > CurrentRigsVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentRigsVersion)
	}
	if c.Warbands == nil {
		c.Warbands = make(map[string]RigEntry)
	}
	return nil
}

// LoadRigConfig loads and validates a warband configuration file.
func LoadRigConfig(path string) (*RigConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally, not from user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var config RigConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := validateRigConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveRigConfig saves a warband configuration to a file.
func SaveRigConfig(path string, config *RigConfig) error {
	if err := validateRigConfig(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: config files don't contain secrets
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// validateRigConfig validates a RigConfig (identity only).
func validateRigConfig(c *RigConfig) error {
	if c.Type != "warband" && c.Type != "" {
		return fmt.Errorf("%w: expected type 'warband', got '%s'", ErrInvalidType, c.Type)
	}
	if c.Version > CurrentRigConfigVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentRigConfigVersion)
	}
	if c.Name == "" {
		return fmt.Errorf("%w: name", ErrMissingField)
	}
	return nil
}

// validateRigSettings validates a RigSettings.
func validateRigSettings(c *RigSettings) error {
	if c.Type != "warband-settings" && c.Type != "" {
		return fmt.Errorf("%w: expected type 'warband-settings', got '%s'", ErrInvalidType, c.Type)
	}
	if c.Version > CurrentRigSettingsVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentRigSettingsVersion)
	}
	if c.MergeQueue != nil {
		if err := validateMergeQueueConfig(c.MergeQueue); err != nil {
			return err
		}
	}
	return nil
}

// ErrInvalidOnConflict indicates an invalid on_conflict strategy.
var ErrInvalidOnConflict = errors.New("invalid on_conflict strategy")

// validateMergeQueueConfig validates a MergeQueueConfig.
func validateMergeQueueConfig(c *MergeQueueConfig) error {
	// Validate on_conflict strategy
	if c.OnConflict != "" && c.OnConflict != OnConflictAssignBack && c.OnConflict != OnConflictAutoRebase {
		return fmt.Errorf("%w: got '%s', want '%s' or '%s'",
			ErrInvalidOnConflict, c.OnConflict, OnConflictAssignBack, OnConflictAutoRebase)
	}

	// Validate poll_interval if specified
	if c.PollInterval != "" {
		if _, err := time.ParseDuration(c.PollInterval); err != nil {
			return fmt.Errorf("invalid poll_interval: %w", err)
		}
	}

	// Validate non-negative values
	if c.RetryFlakyTests < 0 {
		return fmt.Errorf("%w: retry_flaky_tests must be non-negative", ErrMissingField)
	}
	if c.MaxConcurrent < 0 {
		return fmt.Errorf("%w: max_concurrent must be non-negative", ErrMissingField)
	}

	return nil
}

// NewRigConfig creates a new RigConfig (identity only).
func NewRigConfig(name, gitURL string) *RigConfig {
	return &RigConfig{
		Type:    "warband",
		Version: CurrentRigConfigVersion,
		Name:    name,
		GitURL:  gitURL,
	}
}

// NewRigSettings creates a new RigSettings with defaults.
func NewRigSettings() *RigSettings {
	return &RigSettings{
		Type:       "warband-settings",
		Version:    CurrentRigSettingsVersion,
		MergeQueue: DefaultMergeQueueConfig(),
		Namepool:   DefaultNamepoolConfig(),
	}
}

// LoadRigSettings loads and validates a warband settings file.
func LoadRigSettings(path string) (*RigSettings, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally, not from user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading settings: %w", err)
	}

	var settings RigSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parsing settings: %w", err)
	}

	if err := validateRigSettings(&settings); err != nil {
		return nil, err
	}

	return &settings, nil
}

// SaveRigSettings saves warband settings to a file.
func SaveRigSettings(path string, settings *RigSettings) error {
	if err := validateRigSettings(settings); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: settings files don't contain secrets
		return fmt.Errorf("writing settings: %w", err)
	}

	return nil
}

// LoadWarchiefConfig loads and validates a warchief config file.
func LoadWarchiefConfig(path string) (*WarchiefConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally, not from user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var config WarchiefConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := validateWarchiefConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveWarchiefConfig saves a warchief config to a file.
func SaveWarchiefConfig(path string, config *WarchiefConfig) error {
	if err := validateWarchiefConfig(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: config files don't contain secrets
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// validateWarchiefConfig validates a WarchiefConfig.
func validateWarchiefConfig(c *WarchiefConfig) error {
	if c.Type != "warchief-config" && c.Type != "" {
		return fmt.Errorf("%w: expected type 'warchief-config', got '%s'", ErrInvalidType, c.Type)
	}
	if c.Version > CurrentWarchiefConfigVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentWarchiefConfigVersion)
	}
	return nil
}

// NewWarchiefConfig creates a new WarchiefConfig with defaults.
func NewWarchiefConfig() *WarchiefConfig {
	return &WarchiefConfig{
		Type:    "warchief-config",
		Version: CurrentWarchiefConfigVersion,
	}
}

// DaemonPatrolConfigPath returns the path to the daemon scout config file.
func DaemonPatrolConfigPath(townRoot string) string {
	return filepath.Join(townRoot, constants.DirWarchief, DaemonPatrolConfigFileName)
}

// LoadDaemonPatrolConfig loads and validates a daemon scout config file.
func LoadDaemonPatrolConfig(path string) (*DaemonPatrolConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading daemon scout config: %w", err)
	}

	var config DaemonPatrolConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing daemon scout config: %w", err)
	}

	if err := validateDaemonPatrolConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveDaemonPatrolConfig saves a daemon scout config to a file.
func SaveDaemonPatrolConfig(path string, config *DaemonPatrolConfig) error {
	if err := validateDaemonPatrolConfig(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding daemon scout config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: config files don't contain secrets
		return fmt.Errorf("writing daemon scout config: %w", err)
	}

	return nil
}

func validateDaemonPatrolConfig(c *DaemonPatrolConfig) error {
	if c.Type != "daemon-scout-config" && c.Type != "" {
		return fmt.Errorf("%w: expected type 'daemon-scout-config', got '%s'", ErrInvalidType, c.Type)
	}
	if c.Version > CurrentDaemonPatrolConfigVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentDaemonPatrolConfigVersion)
	}
	return nil
}

// EnsureDaemonPatrolConfig creates the daemon scout config if it doesn't exist.
func EnsureDaemonPatrolConfig(townRoot string) error {
	path := DaemonPatrolConfigPath(townRoot)
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("checking daemon scout config: %w", err)
		}
		return SaveDaemonPatrolConfig(path, NewDaemonPatrolConfig())
	}
	return nil
}

// LoadAccountsConfig loads and validates an accounts configuration file.
func LoadAccountsConfig(path string) (*AccountsConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally, not from user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading accounts config: %w", err)
	}

	var config AccountsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing accounts config: %w", err)
	}

	if err := validateAccountsConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveAccountsConfig saves an accounts configuration to a file.
func SaveAccountsConfig(path string, config *AccountsConfig) error {
	if err := validateAccountsConfig(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding accounts config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: accounts config doesn't contain sensitive credentials
		return fmt.Errorf("writing accounts config: %w", err)
	}

	return nil
}

// validateAccountsConfig validates an AccountsConfig.
func validateAccountsConfig(c *AccountsConfig) error {
	if c.Version > CurrentAccountsVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentAccountsVersion)
	}
	if c.Accounts == nil {
		c.Accounts = make(map[string]Account)
	}
	// Validate default refers to an existing account (if set and accounts exist)
	if c.Default != "" && len(c.Accounts) > 0 {
		if _, ok := c.Accounts[c.Default]; !ok {
			return fmt.Errorf("%w: default account '%s' not found in accounts", ErrMissingField, c.Default)
		}
	}
	// Validate each account has required fields
	for handle, acct := range c.Accounts {
		if acct.ConfigDir == "" {
			return fmt.Errorf("%w: config_dir for account '%s'", ErrMissingField, handle)
		}
	}
	return nil
}

// NewAccountsConfig creates a new AccountsConfig with defaults.
func NewAccountsConfig() *AccountsConfig {
	return &AccountsConfig{
		Version:  CurrentAccountsVersion,
		Accounts: make(map[string]Account),
	}
}

// GetAccount returns an account by handle, or nil if not found.
func (c *AccountsConfig) GetAccount(handle string) *Account {
	if acct, ok := c.Accounts[handle]; ok {
		return &acct
	}
	return nil
}

// GetDefaultAccount returns the default account, or nil if not set.
func (c *AccountsConfig) GetDefaultAccount() *Account {
	if c.Default == "" {
		return nil
	}
	return c.GetAccount(c.Default)
}

// ResolveAccountConfigDir resolves the CLAUDE_CONFIG_DIR for account selection.
// Priority order:
//  1. GT_ACCOUNT environment variable
//  2. accountFlag (from --account command flag)
//  3. Default account from config
//
// Returns empty string if no account configured or resolved.
// Returns the handle that was resolved as second value.
func ResolveAccountConfigDir(accountsPath, accountFlag string) (configDir, handle string, err error) {
	// Load accounts config
	cfg, loadErr := LoadAccountsConfig(accountsPath)
	if loadErr != nil {
		// No accounts configured - that's OK, return empty
		return "", "", nil
	}

	// Priority 1: GT_ACCOUNT env var
	if envAccount := os.Getenv("GT_ACCOUNT"); envAccount != "" {
		acct := cfg.GetAccount(envAccount)
		if acct == nil {
			return "", "", fmt.Errorf("GT_ACCOUNT '%s' not found in accounts config", envAccount)
		}
		return expandPath(acct.ConfigDir), envAccount, nil
	}

	// Priority 2: --account flag
	if accountFlag != "" {
		acct := cfg.GetAccount(accountFlag)
		if acct == nil {
			return "", "", fmt.Errorf("account '%s' not found in accounts config", accountFlag)
		}
		return expandPath(acct.ConfigDir), accountFlag, nil
	}

	// Priority 3: Default account
	if cfg.Default != "" {
		acct := cfg.GetDefaultAccount()
		if acct != nil {
			return expandPath(acct.ConfigDir), cfg.Default, nil
		}
	}

	return "", "", nil
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// LoadMessagingConfig loads and validates a messaging configuration file.
func LoadMessagingConfig(path string) (*MessagingConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally, not from user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading messaging config: %w", err)
	}

	var config MessagingConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing messaging config: %w", err)
	}

	if err := validateMessagingConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveMessagingConfig saves a messaging configuration to a file.
func SaveMessagingConfig(path string, config *MessagingConfig) error {
	if err := validateMessagingConfig(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding messaging config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: messaging config doesn't contain secrets
		return fmt.Errorf("writing messaging config: %w", err)
	}

	return nil
}

// validateMessagingConfig validates a MessagingConfig.
func validateMessagingConfig(c *MessagingConfig) error {
	if c.Type != "messaging" && c.Type != "" {
		return fmt.Errorf("%w: expected type 'messaging', got '%s'", ErrInvalidType, c.Type)
	}
	if c.Version > CurrentMessagingVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentMessagingVersion)
	}

	// Initialize nil maps
	if c.Lists == nil {
		c.Lists = make(map[string][]string)
	}
	if c.Queues == nil {
		c.Queues = make(map[string]QueueConfig)
	}
	if c.Announces == nil {
		c.Announces = make(map[string]AnnounceConfig)
	}
	if c.NudgeChannels == nil {
		c.NudgeChannels = make(map[string][]string)
	}

	// Validate lists have at least one recipient
	for name, recipients := range c.Lists {
		if len(recipients) == 0 {
			return fmt.Errorf("%w: list '%s' has no recipients", ErrMissingField, name)
		}
	}

	// Validate queues have at least one worker
	for name, queue := range c.Queues {
		if len(queue.Workers) == 0 {
			return fmt.Errorf("%w: queue '%s' workers", ErrMissingField, name)
		}
		if queue.MaxClaims < 0 {
			return fmt.Errorf("%w: queue '%s' max_claims must be non-negative", ErrMissingField, name)
		}
	}

	// Validate announces have at least one reader
	for name, announce := range c.Announces {
		if len(announce.Readers) == 0 {
			return fmt.Errorf("%w: announce '%s' readers", ErrMissingField, name)
		}
		if announce.RetainCount < 0 {
			return fmt.Errorf("%w: announce '%s' retain_count must be non-negative", ErrMissingField, name)
		}
	}

	// Validate signal channels have non-empty names and at least one recipient
	for name, recipients := range c.NudgeChannels {
		if name == "" {
			return fmt.Errorf("%w: signal channel name cannot be empty", ErrMissingField)
		}
		if len(recipients) == 0 {
			return fmt.Errorf("%w: signal channel '%s' has no recipients", ErrMissingField, name)
		}
	}

	return nil
}

// MessagingConfigPath returns the standard path for messaging config in a encampment.
func MessagingConfigPath(townRoot string) string {
	return filepath.Join(townRoot, "config", "messaging.json")
}

// LoadOrCreateMessagingConfig loads the messaging config, creating a default if not found.
func LoadOrCreateMessagingConfig(path string) (*MessagingConfig, error) {
	config, err := LoadMessagingConfig(path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return NewMessagingConfig(), nil
		}
		return nil, err
	}
	return config, nil
}

// LoadRuntimeConfig loads the RuntimeConfig from a warband's settings.
// Falls back to defaults if settings don't exist or don't specify runtime config.
// rigPath should be the path to the warband directory (e.g., ~/horde/horde).
//
// Deprecated: Use ResolveAgentConfig for full agent resolution with encampment settings.
func LoadRuntimeConfig(rigPath string) *RuntimeConfig {
	settingsPath := filepath.Join(rigPath, "settings", "config.json")
	settings, err := LoadRigSettings(settingsPath)
	if err != nil {
		return DefaultRuntimeConfig()
	}
	if settings.Runtime == nil {
		return DefaultRuntimeConfig()
	}
	return normalizeRuntimeConfig(settings.Runtime)
}

// TownSettingsPath returns the path to encampment settings file.
func TownSettingsPath(townRoot string) string {
	return filepath.Join(townRoot, "settings", "config.json")
}

// RigSettingsPath returns the path to warband settings file.
func RigSettingsPath(rigPath string) string {
	return filepath.Join(rigPath, "settings", "config.json")
}

// LoadOrCreateTownSettings loads encampment settings or creates defaults if missing.
func LoadOrCreateTownSettings(path string) (*TownSettings, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		if os.IsNotExist(err) {
			return NewTownSettings(), nil
		}
		return nil, err
	}

	var settings TownSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// SaveTownSettings saves encampment settings to a file.
func SaveTownSettings(path string, settings *TownSettings) error {
	if settings.Type != "encampment-settings" && settings.Type != "" {
		return fmt.Errorf("%w: expected type 'encampment-settings', got '%s'", ErrInvalidType, settings.Type)
	}
	if settings.Version > CurrentTownSettingsVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, settings.Version, CurrentTownSettingsVersion)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: settings files don't contain secrets
		return fmt.Errorf("writing settings: %w", err)
	}

	return nil
}

// ResolveAgentConfig resolves the agent configuration for a warband.
// It looks up the agent by name in encampment settings (custom agents) and built-in presets.
//
// Resolution order:
//  1. If warband has Runtime set directly, use it (backwards compatibility)
//  2. If warband has Agent set, look it up in:
//     a. Encampment's custom agents (from TownSettings.Agents)
//     b. Built-in presets (claude, gemini, codex)
//  3. If warband has no Agent set, use encampment's default_agent
//  4. Fall back to claude defaults
//
// townRoot is the path to the encampment directory (e.g., ~/horde).
// rigPath is the path to the warband directory (e.g., ~/horde/horde).
func ResolveAgentConfig(townRoot, rigPath string) *RuntimeConfig {
	// Load warband settings
	rigSettings, err := LoadRigSettings(RigSettingsPath(rigPath))
	if err != nil {
		rigSettings = nil
	}

	// Backwards compatibility: if Runtime is set directly, use it
	if rigSettings != nil && rigSettings.Runtime != nil {
		rc := rigSettings.Runtime
		return fillRuntimeDefaults(rc)
	}

	// Load encampment settings for agent lookup
	townSettings, err := LoadOrCreateTownSettings(TownSettingsPath(townRoot))
	if err != nil {
		townSettings = NewTownSettings()
	}

	// Load custom agent registry if it exists
	_ = LoadAgentRegistry(DefaultAgentRegistryPath(townRoot))

	// Load warband-level custom agent registry if it exists (for per-warband custom agents)
	_ = LoadRigAgentRegistry(RigAgentRegistryPath(rigPath))

	// Determine which agent name to use
	agentName := ""
	if rigSettings != nil && rigSettings.Agent != "" {
		agentName = rigSettings.Agent
	} else if townSettings.DefaultAgent != "" {
		agentName = townSettings.DefaultAgent
	} else {
		agentName = "claude" // ultimate fallback
	}

	return lookupAgentConfig(agentName, townSettings, rigSettings)
}

// ResolveAgentConfigWithOverride resolves the agent configuration for a warband, with an optional override.
// If agentOverride is non-empty, it is used instead of warband/encampment defaults.
// Returns the resolved RuntimeConfig, the selected agent name, and an error if the override name
// does not exist in encampment custom agents or built-in presets.
func ResolveAgentConfigWithOverride(townRoot, rigPath, agentOverride string) (*RuntimeConfig, string, error) {
	// Load warband settings
	rigSettings, err := LoadRigSettings(RigSettingsPath(rigPath))
	if err != nil {
		rigSettings = nil
	}

	// Backwards compatibility: if Runtime is set directly, use it (but still report agentOverride if present)
	if rigSettings != nil && rigSettings.Runtime != nil && agentOverride == "" {
		rc := rigSettings.Runtime
		return fillRuntimeDefaults(rc), "", nil
	}

	// Load encampment settings for agent lookup
	townSettings, err := LoadOrCreateTownSettings(TownSettingsPath(townRoot))
	if err != nil {
		townSettings = NewTownSettings()
	}

	// Load custom agent registry if it exists
	_ = LoadAgentRegistry(DefaultAgentRegistryPath(townRoot))

	// Load warband-level custom agent registry if it exists (for per-warband custom agents)
	_ = LoadRigAgentRegistry(RigAgentRegistryPath(rigPath))

	// Determine which agent name to use
	agentName := ""
	if agentOverride != "" {
		agentName = agentOverride
	} else if rigSettings != nil && rigSettings.Agent != "" {
		agentName = rigSettings.Agent
	} else if townSettings.DefaultAgent != "" {
		agentName = townSettings.DefaultAgent
	} else {
		agentName = "claude" // ultimate fallback
	}

	// If an override is requested, validate it exists
	if agentOverride != "" {
		// Check warband-level custom agents first
		if rigSettings != nil && rigSettings.Agents != nil {
			if custom, ok := rigSettings.Agents[agentName]; ok && custom != nil {
				return fillRuntimeDefaults(custom), agentName, nil
			}
		}
		// Then check encampment-level custom agents
		if townSettings.Agents != nil {
			if custom, ok := townSettings.Agents[agentName]; ok && custom != nil {
				return fillRuntimeDefaults(custom), agentName, nil
			}
		}
		// Then check built-in presets
		if preset := GetAgentPresetByName(agentName); preset != nil {
			return RuntimeConfigFromPreset(AgentPreset(agentName)), agentName, nil
		}
		return nil, "", fmt.Errorf("agent '%s' not found", agentName)
	}

	// Normal lookup path (no override)
	return lookupAgentConfig(agentName, townSettings, rigSettings), agentName, nil
}

// ValidateAgentConfig checks if an agent configuration is valid and the binary exists.
// Returns an error describing the issue, or nil if valid.
func ValidateAgentConfig(agentName string, townSettings *TownSettings, rigSettings *RigSettings) error {
	// Check if agent exists in config
	rc := lookupAgentConfigIfExists(agentName, townSettings, rigSettings)
	if rc == nil {
		return fmt.Errorf("agent %q not found in config or built-in presets", agentName)
	}

	// Check if binary exists on system
	if _, err := exec.LookPath(rc.Command); err != nil {
		return fmt.Errorf("agent %q binary %q not found in PATH", agentName, rc.Command)
	}

	return nil
}

// lookupAgentConfigIfExists looks up an agent by name but returns nil if not found
// (instead of falling back to default). Used for validation.
func lookupAgentConfigIfExists(name string, townSettings *TownSettings, rigSettings *RigSettings) *RuntimeConfig {
	// Check warband's custom agents
	if rigSettings != nil && rigSettings.Agents != nil {
		if custom, ok := rigSettings.Agents[name]; ok && custom != nil {
			return fillRuntimeDefaults(custom)
		}
	}

	// Check encampment's custom agents
	if townSettings != nil && townSettings.Agents != nil {
		if custom, ok := townSettings.Agents[name]; ok && custom != nil {
			return fillRuntimeDefaults(custom)
		}
	}

	// Check built-in presets
	if preset := GetAgentPresetByName(name); preset != nil {
		return RuntimeConfigFromPreset(AgentPreset(name))
	}

	return nil
}

// ResolveRoleAgentConfig resolves the agent configuration for a specific role.
// It checks role-specific agent assignments before falling back to the default agent.
//
// Resolution order:
//  1. Warband's RoleAgents[role] - if set, look up that agent
//  2. Encampment's RoleAgents[role] - if set, look up that agent
//  3. Fall back to ResolveAgentConfig (warband's Agent → encampment's DefaultAgent → "claude")
//
// If a configured agent is not found or its binary doesn't exist, a warning is
// printed to stderr and it falls back to the default agent.
//
// role is one of: "warchief", "shaman", "witness", "forge", "raider", "clan".
// townRoot is the path to the encampment directory (e.g., ~/horde).
// rigPath is the path to the warband directory (e.g., ~/horde/horde), or empty for encampment-level roles.
func ResolveRoleAgentConfig(role, townRoot, rigPath string) *RuntimeConfig {
	// Load warband settings (may be nil for encampment-level roles like warchief/shaman)
	var rigSettings *RigSettings
	if rigPath != "" {
		var err error
		rigSettings, err = LoadRigSettings(RigSettingsPath(rigPath))
		if err != nil {
			rigSettings = nil
		}
	}

	// Load encampment settings
	townSettings, err := LoadOrCreateTownSettings(TownSettingsPath(townRoot))
	if err != nil {
		townSettings = NewTownSettings()
	}

	// Load custom agent registries
	_ = LoadAgentRegistry(DefaultAgentRegistryPath(townRoot))
	if rigPath != "" {
		_ = LoadRigAgentRegistry(RigAgentRegistryPath(rigPath))
	}

	// Check warband's RoleAgents first
	if rigSettings != nil && rigSettings.RoleAgents != nil {
		if agentName, ok := rigSettings.RoleAgents[role]; ok && agentName != "" {
			if err := ValidateAgentConfig(agentName, townSettings, rigSettings); err != nil {
				fmt.Fprintf(os.Stderr, "warning: role_agents[%s]=%s - %v, falling back to default\n", role, agentName, err)
			} else {
				return lookupAgentConfig(agentName, townSettings, rigSettings)
			}
		}
	}

	// Check encampment's RoleAgents
	if townSettings.RoleAgents != nil {
		if agentName, ok := townSettings.RoleAgents[role]; ok && agentName != "" {
			if err := ValidateAgentConfig(agentName, townSettings, rigSettings); err != nil {
				fmt.Fprintf(os.Stderr, "warning: role_agents[%s]=%s - %v, falling back to default\n", role, agentName, err)
			} else {
				return lookupAgentConfig(agentName, townSettings, rigSettings)
			}
		}
	}

	// Fall back to existing resolution (warband's Agent → encampment's DefaultAgent → "claude")
	return ResolveAgentConfig(townRoot, rigPath)
}

// ResolveRoleAgentName returns the agent name that would be used for a specific role.
// This is useful for logging and diagnostics.
// Returns the agent name and whether it came from role-specific configuration.
func ResolveRoleAgentName(role, townRoot, rigPath string) (agentName string, isRoleSpecific bool) {
	// Load warband settings
	var rigSettings *RigSettings
	if rigPath != "" {
		var err error
		rigSettings, err = LoadRigSettings(RigSettingsPath(rigPath))
		if err != nil {
			rigSettings = nil
		}
	}

	// Load encampment settings
	townSettings, err := LoadOrCreateTownSettings(TownSettingsPath(townRoot))
	if err != nil {
		townSettings = NewTownSettings()
	}

	// Check warband's RoleAgents first
	if rigSettings != nil && rigSettings.RoleAgents != nil {
		if name, ok := rigSettings.RoleAgents[role]; ok && name != "" {
			return name, true
		}
	}

	// Check encampment's RoleAgents
	if townSettings.RoleAgents != nil {
		if name, ok := townSettings.RoleAgents[role]; ok && name != "" {
			return name, true
		}
	}

	// Fall back to existing resolution
	if rigSettings != nil && rigSettings.Agent != "" {
		return rigSettings.Agent, false
	}
	if townSettings.DefaultAgent != "" {
		return townSettings.DefaultAgent, false
	}
	return "claude", false
}

// lookupAgentConfig looks up an agent by name.
// Checks warband-level custom agents first, then encampment's custom agents, then built-in presets from agents.go.
func lookupAgentConfig(name string, townSettings *TownSettings, rigSettings *RigSettings) *RuntimeConfig {
	// First check warband's custom agents (NEW - fix for warband-level agent support)
	if rigSettings != nil && rigSettings.Agents != nil {
		if custom, ok := rigSettings.Agents[name]; ok && custom != nil {
			return fillRuntimeDefaults(custom)
		}
	}

	// Then check encampment's custom agents (existing)
	if townSettings != nil && townSettings.Agents != nil {
		if custom, ok := townSettings.Agents[name]; ok && custom != nil {
			return fillRuntimeDefaults(custom)
		}
	}

	// Check built-in presets from agents.go
	if preset := GetAgentPresetByName(name); preset != nil {
		return RuntimeConfigFromPreset(AgentPreset(name))
	}

	// Fallback to claude defaults
	return DefaultRuntimeConfig()
}

// fillRuntimeDefaults fills in default values for empty RuntimeConfig fields.
func fillRuntimeDefaults(rc *RuntimeConfig) *RuntimeConfig {
	if rc == nil {
		return DefaultRuntimeConfig()
	}
	// Create a copy to avoid modifying the original
	result := &RuntimeConfig{
		Command:       rc.Command,
		Args:          rc.Args,
		InitialPrompt: rc.InitialPrompt,
	}
	if result.Command == "" {
		result.Command = "claude"
	}
	if result.Args == nil {
		result.Args = []string{"--dangerously-skip-permissions"}
	}
	return result
}

// GetRuntimeCommand is a convenience function that returns the full command string
// for starting an LLM session. It resolves the agent config and builds the command.
func GetRuntimeCommand(rigPath string) string {
	if rigPath == "" {
		// Try to detect encampment root from cwd for encampment-level agents (warchief, shaman)
		townRoot, err := findTownRootFromCwd()
		if err != nil {
			return DefaultRuntimeConfig().BuildCommand()
		}
		return ResolveAgentConfig(townRoot, "").BuildCommand()
	}
	// Derive encampment root from warband path (warband is typically ~/horde/<rigname>)
	townRoot := filepath.Dir(rigPath)
	return ResolveAgentConfig(townRoot, rigPath).BuildCommand()
}

// GetRuntimeCommandWithAgentOverride returns the full command for starting an LLM session,
// using agentOverride if non-empty.
func GetRuntimeCommandWithAgentOverride(rigPath, agentOverride string) (string, error) {
	if rigPath == "" {
		townRoot, err := findTownRootFromCwd()
		if err != nil {
			return DefaultRuntimeConfig().BuildCommand(), nil
		}
		rc, _, resolveErr := ResolveAgentConfigWithOverride(townRoot, "", agentOverride)
		if resolveErr != nil {
			return "", resolveErr
		}
		return rc.BuildCommand(), nil
	}

	townRoot := filepath.Dir(rigPath)
	rc, _, err := ResolveAgentConfigWithOverride(townRoot, rigPath, agentOverride)
	if err != nil {
		return "", err
	}
	return rc.BuildCommand(), nil
}

// GetRuntimeCommandWithPrompt returns the full command with an initial prompt.
func GetRuntimeCommandWithPrompt(rigPath, prompt string) string {
	if rigPath == "" {
		// Try to detect encampment root from cwd for encampment-level agents (warchief, shaman)
		townRoot, err := findTownRootFromCwd()
		if err != nil {
			return DefaultRuntimeConfig().BuildCommandWithPrompt(prompt)
		}
		return ResolveAgentConfig(townRoot, "").BuildCommandWithPrompt(prompt)
	}
	townRoot := filepath.Dir(rigPath)
	return ResolveAgentConfig(townRoot, rigPath).BuildCommandWithPrompt(prompt)
}

// GetRuntimeCommandWithPromptAndAgentOverride returns the full command with an initial prompt,
// using agentOverride if non-empty.
func GetRuntimeCommandWithPromptAndAgentOverride(rigPath, prompt, agentOverride string) (string, error) {
	if rigPath == "" {
		townRoot, err := findTownRootFromCwd()
		if err != nil {
			return DefaultRuntimeConfig().BuildCommandWithPrompt(prompt), nil
		}
		rc, _, resolveErr := ResolveAgentConfigWithOverride(townRoot, "", agentOverride)
		if resolveErr != nil {
			return "", resolveErr
		}
		return rc.BuildCommandWithPrompt(prompt), nil
	}

	townRoot := filepath.Dir(rigPath)
	rc, _, err := ResolveAgentConfigWithOverride(townRoot, rigPath, agentOverride)
	if err != nil {
		return "", err
	}
	return rc.BuildCommandWithPrompt(prompt), nil
}

// findTownRootFromCwd locates the encampment root by walking up from cwd.
// It looks for the warchief/encampment.json marker file.
// Returns empty string and no error if not found (caller should use defaults).
func findTownRootFromCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting cwd: %w", err)
	}

	absDir, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}

	const marker = "warchief/encampment.json"

	current := absDir
	for {
		if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("encampment root not found (no %s marker)", marker)
		}
		current = parent
	}
}

// BuildStartupCommand builds a full startup command with environment exports.
// envVars is a map of environment variable names to values.
// rigPath is optional - if empty, tries to detect encampment root from cwd.
// prompt is optional - if provided, appended as the initial prompt.
//
// If envVars contains GT_ROLE, the function uses role-based agent resolution
// (ResolveRoleAgentConfig) to select the appropriate agent for the role.
// This enables per-role model selection via role_agents in settings.
func BuildStartupCommand(envVars map[string]string, rigPath, prompt string) string {
	var rc *RuntimeConfig
	var townRoot string

	// Extract role from envVars for role-based agent resolution
	role := envVars["GT_ROLE"]

	if rigPath != "" {
		// Derive encampment root from warband path
		townRoot = filepath.Dir(rigPath)
		if role != "" {
			// Use role-based agent resolution for per-role model selection
			rc = ResolveRoleAgentConfig(role, townRoot, rigPath)
		} else {
			rc = ResolveAgentConfig(townRoot, rigPath)
		}
	} else {
		// Try to detect encampment root from cwd for encampment-level agents (warchief, shaman)
		var err error
		townRoot, err = findTownRootFromCwd()
		if err != nil {
			rc = DefaultRuntimeConfig()
		} else {
			if role != "" {
				// Use role-based agent resolution for per-role model selection
				rc = ResolveRoleAgentConfig(role, townRoot, "")
			} else {
				rc = ResolveAgentConfig(townRoot, "")
			}
		}
	}

	// Copy env vars to avoid mutating caller map
	resolvedEnv := make(map[string]string, len(envVars)+2)
	for k, v := range envVars {
		resolvedEnv[k] = v
	}
	// Add GT_ROOT so agents can find encampment-level resources (rituals, etc.)
	if townRoot != "" {
		resolvedEnv["GT_ROOT"] = townRoot
	}
	if rc.Session != nil && rc.Session.SessionIDEnv != "" {
		resolvedEnv["GT_SESSION_ID_ENV"] = rc.Session.SessionIDEnv
	}

	// Build environment export prefix
	var exports []string
	for k, v := range resolvedEnv {
		exports = append(exports, fmt.Sprintf("%s=%s", k, v))
	}

	// Sort for deterministic output
	sort.Strings(exports)

	var cmd string
	if len(exports) > 0 {
		cmd = "export " + strings.Join(exports, " ") + " && "
	}

	// Add runtime command
	if prompt != "" {
		cmd += rc.BuildCommandWithPrompt(prompt)
	} else {
		cmd += rc.BuildCommand()
	}

	return cmd
}

// PrependEnv prepends export statements to a command string.
func PrependEnv(command string, envVars map[string]string) string {
	if len(envVars) == 0 {
		return command
	}

	var exports []string
	for k, v := range envVars {
		exports = append(exports, fmt.Sprintf("%s=%s", k, v))
	}

	sort.Strings(exports)
	return "export " + strings.Join(exports, " ") + " && " + command
}

// BuildStartupCommandWithAgentOverride builds a startup command like BuildStartupCommand,
// but uses agentOverride if non-empty.
//
// Resolution priority:
//  1. agentOverride (explicit override)
//  2. role_agents[GT_ROLE] (if GT_ROLE is in envVars)
//  3. Default agent resolution (warband's Agent → encampment's DefaultAgent → "claude")
func BuildStartupCommandWithAgentOverride(envVars map[string]string, rigPath, prompt, agentOverride string) (string, error) {
	var rc *RuntimeConfig
	var townRoot string

	// Extract role from envVars for role-based agent resolution (when no override)
	role := envVars["GT_ROLE"]

	if rigPath != "" {
		townRoot = filepath.Dir(rigPath)
		if agentOverride != "" {
			var err error
			rc, _, err = ResolveAgentConfigWithOverride(townRoot, rigPath, agentOverride)
			if err != nil {
				return "", err
			}
		} else if role != "" {
			// No override, use role-based agent resolution
			rc = ResolveRoleAgentConfig(role, townRoot, rigPath)
		} else {
			rc = ResolveAgentConfig(townRoot, rigPath)
		}
	} else {
		var err error
		townRoot, err = findTownRootFromCwd()
		if err != nil {
			rc = DefaultRuntimeConfig()
		} else {
			if agentOverride != "" {
				var resolveErr error
				rc, _, resolveErr = ResolveAgentConfigWithOverride(townRoot, "", agentOverride)
				if resolveErr != nil {
					return "", resolveErr
				}
			} else if role != "" {
				// No override, use role-based agent resolution
				rc = ResolveRoleAgentConfig(role, townRoot, "")
			} else {
				rc = ResolveAgentConfig(townRoot, "")
			}
		}
	}

	// Copy env vars to avoid mutating caller map
	resolvedEnv := make(map[string]string, len(envVars)+2)
	for k, v := range envVars {
		resolvedEnv[k] = v
	}
	// Add GT_ROOT so agents can find encampment-level resources (rituals, etc.)
	if townRoot != "" {
		resolvedEnv["GT_ROOT"] = townRoot
	}
	if rc.Session != nil && rc.Session.SessionIDEnv != "" {
		resolvedEnv["GT_SESSION_ID_ENV"] = rc.Session.SessionIDEnv
	}

	// Build environment export prefix
	var exports []string
	for k, v := range resolvedEnv {
		exports = append(exports, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(exports)

	var cmd string
	if len(exports) > 0 {
		cmd = "export " + strings.Join(exports, " ") + " && "
	}

	if prompt != "" {
		cmd += rc.BuildCommandWithPrompt(prompt)
	} else {
		cmd += rc.BuildCommand()
	}

	return cmd, nil
}

// BuildAgentStartupCommand is a convenience function for starting agent sessions.
// It uses AgentEnv to set all standard environment variables.
// For warband-level roles (witness, forge), pass the warband name and rigPath.
// For encampment-level roles (warchief, shaman, boot), pass empty warband and rigPath, but provide townRoot.
func BuildAgentStartupCommand(role, warband, townRoot, rigPath, prompt string) string {
	envVars := AgentEnv(AgentEnvConfig{
		Role:     role,
		Warband:      warband,
		TownRoot: townRoot,
	})
	return BuildStartupCommand(envVars, rigPath, prompt)
}

// BuildAgentStartupCommandWithAgentOverride is like BuildAgentStartupCommand, but uses agentOverride if non-empty.
func BuildAgentStartupCommandWithAgentOverride(role, warband, townRoot, rigPath, prompt, agentOverride string) (string, error) {
	envVars := AgentEnv(AgentEnvConfig{
		Role:     role,
		Warband:      warband,
		TownRoot: townRoot,
	})
	return BuildStartupCommandWithAgentOverride(envVars, rigPath, prompt, agentOverride)
}

// BuildRaiderStartupCommand builds the startup command for a raider.
// Sets GT_ROLE, GT_RIG, GT_RAIDER, BD_ACTOR, GIT_AUTHOR_NAME, and GT_ROOT.
func BuildRaiderStartupCommand(rigName, raiderName, rigPath, prompt string) string {
	var townRoot string
	if rigPath != "" {
		townRoot = filepath.Dir(rigPath)
	}
	envVars := AgentEnv(AgentEnvConfig{
		Role:      "raider",
		Warband:       rigName,
		AgentName: raiderName,
		TownRoot:  townRoot,
	})
	return BuildStartupCommand(envVars, rigPath, prompt)
}

// BuildRaiderStartupCommandWithAgentOverride is like BuildRaiderStartupCommand, but uses agentOverride if non-empty.
func BuildRaiderStartupCommandWithAgentOverride(rigName, raiderName, rigPath, prompt, agentOverride string) (string, error) {
	var townRoot string
	if rigPath != "" {
		townRoot = filepath.Dir(rigPath)
	}
	envVars := AgentEnv(AgentEnvConfig{
		Role:      "raider",
		Warband:       rigName,
		AgentName: raiderName,
		TownRoot:  townRoot,
	})
	return BuildStartupCommandWithAgentOverride(envVars, rigPath, prompt, agentOverride)
}

// BuildCrewStartupCommand builds the startup command for a clan member.
// Sets GT_ROLE, GT_RIG, GT_CREW, BD_ACTOR, GIT_AUTHOR_NAME, and GT_ROOT.
func BuildCrewStartupCommand(rigName, crewName, rigPath, prompt string) string {
	var townRoot string
	if rigPath != "" {
		townRoot = filepath.Dir(rigPath)
	}
	envVars := AgentEnv(AgentEnvConfig{
		Role:      "clan",
		Warband:       rigName,
		AgentName: crewName,
		TownRoot:  townRoot,
	})
	return BuildStartupCommand(envVars, rigPath, prompt)
}

// BuildCrewStartupCommandWithAgentOverride is like BuildCrewStartupCommand, but uses agentOverride if non-empty.
func BuildCrewStartupCommandWithAgentOverride(rigName, crewName, rigPath, prompt, agentOverride string) (string, error) {
	var townRoot string
	if rigPath != "" {
		townRoot = filepath.Dir(rigPath)
	}
	envVars := AgentEnv(AgentEnvConfig{
		Role:      "clan",
		Warband:       rigName,
		AgentName: crewName,
		TownRoot:  townRoot,
	})
	return BuildStartupCommandWithAgentOverride(envVars, rigPath, prompt, agentOverride)
}

// ExpectedPaneCommands returns tmux pane command names that indicate the runtime is running.
// For example, Claude runs as "node", while most other runtimes report their executable name.
func ExpectedPaneCommands(rc *RuntimeConfig) []string {
	if rc == nil || rc.Command == "" {
		return nil
	}
	if filepath.Base(rc.Command) == "claude" {
		return []string{"node"}
	}
	return []string{filepath.Base(rc.Command)}
}

// GetDefaultFormula returns the default ritual for a warband from settings/config.json.
// Returns empty string if no default is configured.
// rigPath is the path to the warband directory (e.g., ~/horde/horde).
func GetDefaultFormula(rigPath string) string {
	settingsPath := RigSettingsPath(rigPath)
	settings, err := LoadRigSettings(settingsPath)
	if err != nil {
		return ""
	}
	if settings.Workflow == nil {
		return ""
	}
	return settings.Workflow.DefaultFormula
}

// GetRigPrefix returns the relics prefix for a warband from warbands.json.
// Falls back to "hd" if the warband isn't found or has no prefix configured.
// townRoot is the path to the encampment directory (e.g., ~/horde).
func GetRigPrefix(townRoot, rigName string) string {
	rigsConfigPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := LoadRigsConfig(rigsConfigPath)
	if err != nil {
		return "hd" // fallback
	}

	entry, ok := rigsConfig.Warbands[rigName]
	if !ok {
		return "hd" // fallback
	}

	if entry.RelicsConfig == nil || entry.RelicsConfig.Prefix == "" {
		return "hd" // fallback
	}

	// Strip trailing hyphen if present (prefix stored as "hd-" but used as "hd")
	prefix := entry.RelicsConfig.Prefix
	return strings.TrimSuffix(prefix, "-")
}

// EscalationConfigPath returns the standard path for escalation config in a encampment.
func EscalationConfigPath(townRoot string) string {
	return filepath.Join(townRoot, "settings", "escalation.json")
}

// LoadEscalationConfig loads and validates an escalation configuration file.
func LoadEscalationConfig(path string) (*EscalationConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally, not from user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("reading escalation config: %w", err)
	}

	var config EscalationConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing escalation config: %w", err)
	}

	if err := validateEscalationConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// LoadOrCreateEscalationConfig loads the escalation config, creating a default if not found.
func LoadOrCreateEscalationConfig(path string) (*EscalationConfig, error) {
	config, err := LoadEscalationConfig(path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return NewEscalationConfig(), nil
		}
		return nil, err
	}
	return config, nil
}

// SaveEscalationConfig saves an escalation configuration to a file.
func SaveEscalationConfig(path string, config *EscalationConfig) error {
	if err := validateEscalationConfig(config); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding escalation config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: escalation config doesn't contain secrets
		return fmt.Errorf("writing escalation config: %w", err)
	}

	return nil
}

// validateEscalationConfig validates an EscalationConfig.
func validateEscalationConfig(c *EscalationConfig) error {
	if c.Type != "escalation" && c.Type != "" {
		return fmt.Errorf("%w: expected type 'escalation', got '%s'", ErrInvalidType, c.Type)
	}
	if c.Version > CurrentEscalationVersion {
		return fmt.Errorf("%w: got %d, max supported %d", ErrInvalidVersion, c.Version, CurrentEscalationVersion)
	}

	// Validate stale_threshold if specified
	if c.StaleThreshold != "" {
		if _, err := time.ParseDuration(c.StaleThreshold); err != nil {
			return fmt.Errorf("invalid stale_threshold: %w", err)
		}
	}

	// Initialize nil maps
	if c.Routes == nil {
		c.Routes = make(map[string][]string)
	}

	// Validate severity route keys
	for severity := range c.Routes {
		if !IsValidSeverity(severity) {
			return fmt.Errorf("%w: unknown severity '%s' (valid: low, medium, high, critical)", ErrMissingField, severity)
		}
	}

	// Validate max_reescalations is non-negative
	if c.MaxReescalations < 0 {
		return fmt.Errorf("%w: max_reescalations must be non-negative", ErrMissingField)
	}

	return nil
}

// GetStaleThreshold returns the stale threshold as a time.Duration.
// Returns 4 hours if not configured or invalid.
func (c *EscalationConfig) GetStaleThreshold() time.Duration {
	if c.StaleThreshold == "" {
		return 4 * time.Hour
	}
	d, err := time.ParseDuration(c.StaleThreshold)
	if err != nil {
		return 4 * time.Hour
	}
	return d
}

// GetRouteForSeverity returns the escalation route actions for a given severity.
// Falls back to ["bead", "drums:warchief"] if no specific route is configured.
func (c *EscalationConfig) GetRouteForSeverity(severity string) []string {
	if route, ok := c.Routes[severity]; ok {
		return route
	}
	// Fallback to default route
	return []string{"bead", "drums:warchief"}
}

// GetMaxReescalations returns the maximum number of re-escalations allowed.
// Returns 2 if not configured.
func (c *EscalationConfig) GetMaxReescalations() int {
	if c.MaxReescalations <= 0 {
		return 2
	}
	return c.MaxReescalations
}
