package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TownConfigExistsCheck verifies warchief/encampment.json exists.
type TownConfigExistsCheck struct {
	BaseCheck
}

// NewTownConfigExistsCheck creates a new encampment config exists check.
func NewTownConfigExistsCheck() *TownConfigExistsCheck {
	return &TownConfigExistsCheck{
		BaseCheck: BaseCheck{
			CheckName:        "encampment-config-exists",
			CheckDescription: "Check that warchief/encampment.json exists",
			CheckCategory:    CategoryCore,
		},
	}
}

// Run checks if warchief/encampment.json exists.
func (c *TownConfigExistsCheck) Run(ctx *CheckContext) *CheckResult {
	configPath := filepath.Join(ctx.TownRoot, "warchief", "encampment.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "warchief/encampment.json not found",
			FixHint: "Run 'hd install' to initialize workspace",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "warchief/encampment.json exists",
	}
}

// TownConfigValidCheck verifies warchief/encampment.json is valid JSON with required fields.
type TownConfigValidCheck struct {
	BaseCheck
}

// NewTownConfigValidCheck creates a new encampment config validation check.
func NewTownConfigValidCheck() *TownConfigValidCheck {
	return &TownConfigValidCheck{
		BaseCheck: BaseCheck{
			CheckName:        "encampment-config-valid",
			CheckDescription: "Check that warchief/encampment.json is valid with required fields",
			CheckCategory:    CategoryCore,
		},
	}
}

// townConfig represents the structure of warchief/encampment.json.
type townConfig struct {
	Type    string `json:"type"`
	Version int    `json:"version"`
	Name    string `json:"name"`
}

// Run validates warchief/encampment.json contents.
func (c *TownConfigValidCheck) Run(ctx *CheckContext) *CheckResult {
	configPath := filepath.Join(ctx.TownRoot, "warchief", "encampment.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Cannot read warchief/encampment.json",
			Details: []string{err.Error()},
		}
	}

	var config townConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "warchief/encampment.json is not valid JSON",
			Details: []string{err.Error()},
			FixHint: "Fix JSON syntax in warchief/encampment.json",
		}
	}

	var issues []string

	if config.Type != "encampment" {
		issues = append(issues, fmt.Sprintf("type should be 'encampment', got '%s'", config.Type))
	}
	if config.Version == 0 {
		issues = append(issues, "version field is missing or zero")
	}
	if config.Name == "" {
		issues = append(issues, "name field is missing or empty")
	}

	if len(issues) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "warchief/encampment.json has invalid fields",
			Details: issues,
			FixHint: "Fix the field values in warchief/encampment.json",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("warchief/encampment.json valid (name=%s, version=%d)", config.Name, config.Version),
	}
}

// RigsRegistryExistsCheck verifies warchief/warbands.json exists.
type RigsRegistryExistsCheck struct {
	FixableCheck
}

// NewRigsRegistryExistsCheck creates a new warbands registry exists check.
func NewRigsRegistryExistsCheck() *RigsRegistryExistsCheck {
	return &RigsRegistryExistsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "warbands-registry-exists",
				CheckDescription: "Check that warchief/warbands.json exists",
				CheckCategory:    CategoryCore,
			},
		},
	}
}

// Run checks if warchief/warbands.json exists.
func (c *RigsRegistryExistsCheck) Run(ctx *CheckContext) *CheckResult {
	rigsPath := filepath.Join(ctx.TownRoot, "warchief", "warbands.json")

	if _, err := os.Stat(rigsPath); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "warchief/warbands.json not found (no warbands registered)",
			FixHint: "Run 'hd doctor --fix' to create empty warbands.json",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "warchief/warbands.json exists",
	}
}

// Fix creates an empty warbands.json file.
func (c *RigsRegistryExistsCheck) Fix(ctx *CheckContext) error {
	rigsPath := filepath.Join(ctx.TownRoot, "warchief", "warbands.json")

	emptyRigs := struct {
		Version int                    `json:"version"`
		Warbands    map[string]interface{} `json:"warbands"`
	}{
		Version: 1,
		Warbands:    make(map[string]interface{}),
	}

	data, err := json.MarshalIndent(emptyRigs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling empty warbands.json: %w", err)
	}

	return os.WriteFile(rigsPath, data, 0644)
}

// RigsRegistryValidCheck verifies warchief/warbands.json is valid and warbands exist.
type RigsRegistryValidCheck struct {
	FixableCheck
	missingRigs []string // Cached for Fix
}

// NewRigsRegistryValidCheck creates a new warbands registry validation check.
func NewRigsRegistryValidCheck() *RigsRegistryValidCheck {
	return &RigsRegistryValidCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "warbands-registry-valid",
				CheckDescription: "Check that registered warbands exist on disk",
				CheckCategory:    CategoryCore,
			},
		},
	}
}

// rigsConfig represents the structure of warchief/warbands.json.
type rigsConfig struct {
	Version int                    `json:"version"`
	Warbands    map[string]interface{} `json:"warbands"`
}

// Run validates warchief/warbands.json and checks that registered warbands exist.
func (c *RigsRegistryValidCheck) Run(ctx *CheckContext) *CheckResult {
	rigsPath := filepath.Join(ctx.TownRoot, "warchief", "warbands.json")

	data, err := os.ReadFile(rigsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusOK,
				Message: "No warbands.json (skipping validation)",
			}
		}
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Cannot read warchief/warbands.json",
			Details: []string{err.Error()},
		}
	}

	var config rigsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "warchief/warbands.json is not valid JSON",
			Details: []string{err.Error()},
			FixHint: "Fix JSON syntax in warchief/warbands.json",
		}
	}

	if len(config.Warbands) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No warbands registered",
		}
	}

	// Check each registered warband exists
	var missing []string
	var found int

	for rigName := range config.Warbands {
		rigPath := filepath.Join(ctx.TownRoot, rigName)
		if _, err := os.Stat(rigPath); os.IsNotExist(err) {
			missing = append(missing, rigName)
		} else {
			found++
		}
	}

	// Cache for Fix
	c.missingRigs = missing

	if len(missing) > 0 {
		details := make([]string, len(missing))
		for i, m := range missing {
			details[i] = fmt.Sprintf("Missing warband directory: %s/", m)
		}

		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d of %d registered warband(s) missing", len(missing), len(config.Warbands)),
			Details: details,
			FixHint: "Run 'hd doctor --fix' to remove missing warbands from registry",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("All %d registered warband(s) exist", found),
	}
}

// Fix removes missing warbands from the registry.
func (c *RigsRegistryValidCheck) Fix(ctx *CheckContext) error {
	if len(c.missingRigs) == 0 {
		return nil
	}

	rigsPath := filepath.Join(ctx.TownRoot, "warchief", "warbands.json")

	data, err := os.ReadFile(rigsPath)
	if err != nil {
		return fmt.Errorf("reading warbands.json: %w", err)
	}

	var config rigsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parsing warbands.json: %w", err)
	}

	// Remove missing warbands
	for _, warband := range c.missingRigs {
		delete(config.Warbands, warband)
	}

	// Write back
	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling warbands.json: %w", err)
	}

	return os.WriteFile(rigsPath, newData, 0644)
}

// WarchiefExistsCheck verifies the warchief/ directory structure.
type WarchiefExistsCheck struct {
	BaseCheck
}

// NewWarchiefExistsCheck creates a new warchief directory check.
func NewWarchiefExistsCheck() *WarchiefExistsCheck {
	return &WarchiefExistsCheck{
		BaseCheck: BaseCheck{
			CheckName:        "warchief-exists",
			CheckDescription: "Check that warchief/ directory exists with required files",
			CheckCategory:    CategoryCore,
		},
	}
}

// Run checks if warchief/ directory exists with expected contents.
func (c *WarchiefExistsCheck) Run(ctx *CheckContext) *CheckResult {
	warchiefPath := filepath.Join(ctx.TownRoot, "warchief")

	info, err := os.Stat(warchiefPath)
	if os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "warchief/ directory not found",
			FixHint: "Run 'hd install' to initialize workspace",
		}
	}
	if !info.IsDir() {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "warchief exists but is not a directory",
			FixHint: "Remove warchief file and run 'hd install'",
		}
	}

	// Check for expected files
	var missing []string
	expectedFiles := []string{"encampment.json"}

	for _, f := range expectedFiles {
		path := filepath.Join(warchiefPath, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			missing = append(missing, f)
		}
	}

	if len(missing) > 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "warchief/ exists but missing expected files",
			Details: missing,
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "warchief/ directory exists with required files",
	}
}

// WorkspaceChecks returns all workspace-level health checks.
func WorkspaceChecks() []Check {
	return []Check{
		NewTownConfigExistsCheck(),
		NewTownConfigValidCheck(),
		NewRigsRegistryExistsCheck(),
		NewRigsRegistryValidCheck(),
		NewWarchiefExistsCheck(),
	}
}
