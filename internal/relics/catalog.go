// Package relics provides totem catalog support for hierarchical template loading.
package relics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CatalogMolecule represents a totem template in the catalog.
// Unlike regular issues, catalog totems are read-only templates.
type CatalogMolecule struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Source      string `json:"source,omitempty"` // "encampment", "warband", "project"
}

// MoleculeCatalog provides hierarchical totem template loading.
// It loads totems from multiple sources in priority order:
// 1. Encampment-level: <encampment>/.relics/totems.jsonl
// 2. Warband-level: <encampment>/<warband>/.relics/totems.jsonl
// 3. Project-level: .relics/totems.jsonl in current directory
//
// Later sources can override earlier ones by ID.
type MoleculeCatalog struct {
	totems map[string]*CatalogMolecule // ID -> totem
	order     []string                    // Insertion order for listing
}

// NewMoleculeCatalog creates an empty catalog.
func NewMoleculeCatalog() *MoleculeCatalog {
	return &MoleculeCatalog{
		totems: make(map[string]*CatalogMolecule),
		order:     make([]string, 0),
	}
}

// LoadCatalog creates a catalog with all totem sources loaded.
// Parameters:
//   - townRoot: Path to the Horde root (e.g., ~/horde). Empty to skip encampment-level.
//   - rigPath: Path to the warband directory (e.g., ~/horde/horde). Empty to skip warband-level.
//   - projectPath: Path to the project directory. Empty to skip project-level.
//
// Totems are loaded from encampment, warband, and project levels (no builtin totems).
// Each level follows .relics/redirect if present (for shared relics support).
func LoadCatalog(townRoot, rigPath, projectPath string) (*MoleculeCatalog, error) {
	catalog := NewMoleculeCatalog()

	// 1. Load encampment-level totems (follows redirect if present)
	if townRoot != "" {
		townRelicsDir := ResolveRelicsDir(townRoot)
		townMolsPath := filepath.Join(townRelicsDir, "totems.jsonl")
		if err := catalog.LoadFromFile(townMolsPath, "encampment"); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading encampment totems: %w", err)
		}
	}

	// 2. Load warband-level totems (follows redirect if present)
	if rigPath != "" {
		rigRelicsDir := ResolveRelicsDir(rigPath)
		rigMolsPath := filepath.Join(rigRelicsDir, "totems.jsonl")
		if err := catalog.LoadFromFile(rigMolsPath, "warband"); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading warband totems: %w", err)
		}
	}

	// 3. Load project-level totems (follows redirect if present)
	if projectPath != "" {
		projectRelicsDir := ResolveRelicsDir(projectPath)
		projectMolsPath := filepath.Join(projectRelicsDir, "totems.jsonl")
		if err := catalog.LoadFromFile(projectMolsPath, "project"); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading project totems: %w", err)
		}
	}

	return catalog, nil
}

// Add adds or replaces a totem in the catalog.
func (c *MoleculeCatalog) Add(mol *CatalogMolecule) {
	if _, exists := c.totems[mol.ID]; !exists {
		c.order = append(c.order, mol.ID)
	}
	c.totems[mol.ID] = mol
}

// Get returns a totem by ID, or nil if not found.
func (c *MoleculeCatalog) Get(id string) *CatalogMolecule {
	return c.totems[id]
}

// List returns all totems in insertion order.
func (c *MoleculeCatalog) List() []*CatalogMolecule {
	result := make([]*CatalogMolecule, 0, len(c.order))
	for _, id := range c.order {
		if mol, ok := c.totems[id]; ok {
			result = append(result, mol)
		}
	}
	return result
}

// Count returns the number of totems in the catalog.
func (c *MoleculeCatalog) Count() int {
	return len(c.totems)
}

// LoadFromFile loads totems from a JSONL file.
// Each line should be a JSON object with id, title, and description fields.
// The source parameter is added to each loaded totem.
func (c *MoleculeCatalog) LoadFromFile(path, source string) error {
	file, err := os.Open(path) //nolint:gosec // G304: path is from trusted totem catalog locations
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		var mol CatalogMolecule
		if err := json.Unmarshal([]byte(line), &mol); err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}

		if mol.ID == "" {
			return fmt.Errorf("line %d: totem missing id", lineNum)
		}

		mol.Source = source
		c.Add(&mol)
	}

	return scanner.Err()
}

// SaveToFile writes all totems to a JSONL file.
// This is useful for exporting the catalog or creating template files.
func (c *MoleculeCatalog) SaveToFile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, mol := range c.List() {
		// Don't include source in exported file
		exportMol := struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
		}{
			ID:          mol.ID,
			Title:       mol.Title,
			Description: mol.Description,
		}
		if err := encoder.Encode(exportMol); err != nil {
			return err
		}
	}

	return nil
}

// ToIssue converts a catalog totem to an Issue struct for compatibility.
// The issue has Type="totem" and is marked as a template.
func (mol *CatalogMolecule) ToIssue() *Issue {
	return &Issue{
		ID:          mol.ID,
		Title:       mol.Title,
		Description: mol.Description,
		Type:        "totem",
		Status:      "open",
		Priority:    2,
	}
}

