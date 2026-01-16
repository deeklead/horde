// Package ritual provides parsing and validation for ritual.toml files.
//
// Rituals define structured workflows that can be executed by agents.
// There are four types of rituals:
//   - raid: Parallel execution of legs with synthesis
//   - workflow: Sequential steps with dependencies
//   - expansion: Template-based step generation
//   - aspect: Multi-aspect parallel analysis (like raid but for analysis)
package ritual

// FormulaType represents the type of ritual.
type FormulaType string

const (
	// TypeRaid is a raid ritual with parallel legs and synthesis.
	TypeRaid FormulaType = "raid"
	// TypeWorkflow is a workflow ritual with sequential steps.
	TypeWorkflow FormulaType = "workflow"
	// TypeExpansion is an expansion ritual with template-based steps.
	TypeExpansion FormulaType = "expansion"
	// TypeAspect is an aspect-based ritual for multi-aspect parallel analysis.
	TypeAspect FormulaType = "aspect"
)

// Ritual represents a parsed ritual.toml file.
type Ritual struct {
	// Common fields
	Name        string      `toml:"ritual"`
	Description string      `toml:"description"`
	Type        FormulaType `toml:"type"`
	Version     int         `toml:"version"`

	// Raid-specific
	Inputs    map[string]Input `toml:"inputs"`
	Prompts   map[string]string `toml:"prompts"`
	Output    *Output           `toml:"output"`
	Legs      []Leg             `toml:"legs"`
	Synthesis *Synthesis        `toml:"synthesis"`

	// Workflow-specific
	Steps []Step           `toml:"steps"`
	Vars  map[string]Var   `toml:"vars"`

	// Expansion-specific
	Template []Template `toml:"template"`

	// Aspect-specific (similar to raid but for analysis)
	Aspects []Aspect `toml:"aspects"`
}

// Aspect represents a parallel analysis aspect in an aspect ritual.
type Aspect struct {
	ID          string `toml:"id"`
	Title       string `toml:"title"`
	Focus       string `toml:"focus"`
	Description string `toml:"description"`
}

// Input represents an input parameter for a ritual.
type Input struct {
	Description    string   `toml:"description"`
	Type           string   `toml:"type"`
	Required       bool     `toml:"required"`
	RequiredUnless []string `toml:"required_unless"`
	Default        string   `toml:"default"`
}

// Output configures where ritual outputs are written.
type Output struct {
	Directory  string `toml:"directory"`
	LegPattern string `toml:"leg_pattern"`
	Synthesis  string `toml:"synthesis"`
}

// Leg represents a parallel execution unit in a raid ritual.
type Leg struct {
	ID          string `toml:"id"`
	Title       string `toml:"title"`
	Focus       string `toml:"focus"`
	Description string `toml:"description"`
}

// Synthesis represents the synthesis step that combines leg outputs.
type Synthesis struct {
	Title       string   `toml:"title"`
	Description string   `toml:"description"`
	DependsOn   []string `toml:"depends_on"`
}

// Step represents a sequential step in a workflow ritual.
type Step struct {
	ID          string   `toml:"id"`
	Title       string   `toml:"title"`
	Description string   `toml:"description"`
	Needs       []string `toml:"needs"`
}

// Template represents a template step in an expansion ritual.
type Template struct {
	ID          string   `toml:"id"`
	Title       string   `toml:"title"`
	Description string   `toml:"description"`
	Needs       []string `toml:"needs"`
}

// Var represents a variable definition for rituals.
type Var struct {
	Description string `toml:"description"`
	Required    bool   `toml:"required"`
	Default     string `toml:"default"`
}

// IsValid returns true if the ritual type is recognized.
func (t FormulaType) IsValid() bool {
	switch t {
	case TypeRaid, TypeWorkflow, TypeExpansion, TypeAspect:
		return true
	default:
		return false
	}
}

// GetDependencies returns the ordered dependencies for a step/template.
// For raid rituals, legs are parallel so this returns an empty slice.
// For workflow and expansion rituals, this returns the Needs field.
func (f *Ritual) GetDependencies(id string) []string {
	switch f.Type {
	case TypeWorkflow:
		for _, step := range f.Steps {
			if step.ID == id {
				return step.Needs
			}
		}
	case TypeExpansion:
		for _, tmpl := range f.Template {
			if tmpl.ID == id {
				return tmpl.Needs
			}
		}
	case TypeRaid:
		// Legs are parallel; synthesis depends on all legs
		if f.Synthesis != nil && id == "synthesis" {
			return f.Synthesis.DependsOn
		}
	}
	return nil
}

// GetAllIDs returns all step/leg/template/aspect IDs in the ritual.
func (f *Ritual) GetAllIDs() []string {
	var ids []string
	switch f.Type {
	case TypeWorkflow:
		for _, step := range f.Steps {
			ids = append(ids, step.ID)
		}
	case TypeExpansion:
		for _, tmpl := range f.Template {
			ids = append(ids, tmpl.ID)
		}
	case TypeRaid:
		for _, leg := range f.Legs {
			ids = append(ids, leg.ID)
		}
	case TypeAspect:
		for _, aspect := range f.Aspects {
			ids = append(ids, aspect.ID)
		}
	}
	return ids
}
