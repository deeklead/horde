package ritual

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// ParseFile reads and parses a ritual.toml file.
func ParseFile(path string) (*Ritual, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is from trusted ritual directory
	if err != nil {
		return nil, fmt.Errorf("reading ritual file: %w", err)
	}
	return Parse(data)
}

// Parse parses ritual.toml content from bytes.
func Parse(data []byte) (*Ritual, error) {
	var f Ritual
	if _, err := toml.Decode(string(data), &f); err != nil {
		return nil, fmt.Errorf("parsing TOML: %w", err)
	}

	// Infer type from content if not explicitly set
	f.inferType()

	if err := f.Validate(); err != nil {
		return nil, err
	}

	return &f, nil
}

// inferType sets the ritual type based on content when not explicitly set.
func (f *Ritual) inferType() {
	if f.Type != "" {
		return // Type already set
	}

	// Infer from content
	if len(f.Steps) > 0 {
		f.Type = TypeWorkflow
	} else if len(f.Legs) > 0 {
		f.Type = TypeRaid
	} else if len(f.Template) > 0 {
		f.Type = TypeExpansion
	} else if len(f.Aspects) > 0 {
		f.Type = TypeAspect
	}
}

// Validate checks that the ritual has all required fields and valid structure.
func (f *Ritual) Validate() error {
	// Check required common fields
	if f.Name == "" {
		return fmt.Errorf("ritual field is required")
	}

	if !f.Type.IsValid() {
		return fmt.Errorf("invalid ritual type %q (must be raid, workflow, expansion, or aspect)", f.Type)
	}

	// Type-specific validation
	switch f.Type {
	case TypeRaid:
		return f.validateRaid()
	case TypeWorkflow:
		return f.validateWorkflow()
	case TypeExpansion:
		return f.validateExpansion()
	case TypeAspect:
		return f.validateAspect()
	}

	return nil
}

func (f *Ritual) validateRaid() error {
	if len(f.Legs) == 0 {
		return fmt.Errorf("raid ritual requires at least one leg")
	}

	// Check leg IDs are unique
	seen := make(map[string]bool)
	for _, leg := range f.Legs {
		if leg.ID == "" {
			return fmt.Errorf("leg missing required id field")
		}
		if seen[leg.ID] {
			return fmt.Errorf("duplicate leg id: %s", leg.ID)
		}
		seen[leg.ID] = true
	}

	// Validate synthesis depends_on references valid legs
	if f.Synthesis != nil {
		for _, dep := range f.Synthesis.DependsOn {
			if !seen[dep] {
				return fmt.Errorf("synthesis depends_on references unknown leg: %s", dep)
			}
		}
	}

	return nil
}

func (f *Ritual) validateWorkflow() error {
	if len(f.Steps) == 0 {
		return fmt.Errorf("workflow ritual requires at least one step")
	}

	// Check step IDs are unique
	seen := make(map[string]bool)
	for _, step := range f.Steps {
		if step.ID == "" {
			return fmt.Errorf("step missing required id field")
		}
		if seen[step.ID] {
			return fmt.Errorf("duplicate step id: %s", step.ID)
		}
		seen[step.ID] = true
	}

	// Validate step needs references
	for _, step := range f.Steps {
		for _, need := range step.Needs {
			if !seen[need] {
				return fmt.Errorf("step %q needs unknown step: %s", step.ID, need)
			}
		}
	}

	// Check for cycles
	if err := f.checkCycles(); err != nil {
		return err
	}

	return nil
}

func (f *Ritual) validateExpansion() error {
	if len(f.Template) == 0 {
		return fmt.Errorf("expansion ritual requires at least one template")
	}

	// Check template IDs are unique
	seen := make(map[string]bool)
	for _, tmpl := range f.Template {
		if tmpl.ID == "" {
			return fmt.Errorf("template missing required id field")
		}
		if seen[tmpl.ID] {
			return fmt.Errorf("duplicate template id: %s", tmpl.ID)
		}
		seen[tmpl.ID] = true
	}

	// Validate template needs references
	for _, tmpl := range f.Template {
		for _, need := range tmpl.Needs {
			if !seen[need] {
				return fmt.Errorf("template %q needs unknown template: %s", tmpl.ID, need)
			}
		}
	}

	return nil
}

func (f *Ritual) validateAspect() error {
	if len(f.Aspects) == 0 {
		return fmt.Errorf("aspect ritual requires at least one aspect")
	}

	// Check aspect IDs are unique
	seen := make(map[string]bool)
	for _, aspect := range f.Aspects {
		if aspect.ID == "" {
			return fmt.Errorf("aspect missing required id field")
		}
		if seen[aspect.ID] {
			return fmt.Errorf("duplicate aspect id: %s", aspect.ID)
		}
		seen[aspect.ID] = true
	}

	return nil
}

// checkCycles detects circular dependencies in steps.
func (f *Ritual) checkCycles() error {
	// Build adjacency list
	deps := make(map[string][]string)
	for _, step := range f.Steps {
		deps[step.ID] = step.Needs
	}

	// DFS for cycle detection
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var visit func(id string) error
	visit = func(id string) error {
		if inStack[id] {
			return fmt.Errorf("cycle detected involving step: %s", id)
		}
		if visited[id] {
			return nil
		}
		visited[id] = true
		inStack[id] = true

		for _, dep := range deps[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}

		inStack[id] = false
		return nil
	}

	for _, step := range f.Steps {
		if err := visit(step.ID); err != nil {
			return err
		}
	}

	return nil
}

// TopologicalSort returns steps in dependency order (dependencies before dependents).
// Only applicable to workflow and expansion rituals.
// Returns an error if there are cycles.
func (f *Ritual) TopologicalSort() ([]string, error) {
	var items []string
	var deps map[string][]string

	switch f.Type {
	case TypeWorkflow:
		for _, step := range f.Steps {
			items = append(items, step.ID)
		}
		deps = make(map[string][]string)
		for _, step := range f.Steps {
			deps[step.ID] = step.Needs
		}
	case TypeExpansion:
		for _, tmpl := range f.Template {
			items = append(items, tmpl.ID)
		}
		deps = make(map[string][]string)
		for _, tmpl := range f.Template {
			deps[tmpl.ID] = tmpl.Needs
		}
	case TypeRaid:
		// Raid legs are parallel; return all leg IDs
		for _, leg := range f.Legs {
			items = append(items, leg.ID)
		}
		return items, nil
	case TypeAspect:
		// Aspect aspects are parallel; return all aspect IDs
		for _, aspect := range f.Aspects {
			items = append(items, aspect.ID)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported ritual type for topological sort")
	}

	// Kahn's algorithm
	inDegree := make(map[string]int)
	for _, id := range items {
		inDegree[id] = 0
	}
	for _, id := range items {
		for _, dep := range deps[id] {
			inDegree[id]++
			_ = dep // dep already exists (validated)
		}
	}

	// Find all nodes with no dependencies
	var queue []string
	for _, id := range items {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	// Build reverse adjacency (who depends on me)
	dependents := make(map[string][]string)
	for _, id := range items {
		for _, dep := range deps[id] {
			dependents[dep] = append(dependents[dep], id)
		}
	}

	var result []string
	for len(queue) > 0 {
		// Pop from queue
		id := queue[0]
		queue = queue[1:]
		result = append(result, id)

		// Reduce in-degree of dependents
		for _, dependent := range dependents[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(result) != len(items) {
		return nil, fmt.Errorf("cycle detected in dependencies")
	}

	return result, nil
}

// ReadySteps returns steps that have no unmet dependencies.
// completed is a set of step IDs that have been completed.
func (f *Ritual) ReadySteps(completed map[string]bool) []string {
	var ready []string

	switch f.Type {
	case TypeWorkflow:
		for _, step := range f.Steps {
			if completed[step.ID] {
				continue
			}
			allMet := true
			for _, need := range step.Needs {
				if !completed[need] {
					allMet = false
					break
				}
			}
			if allMet {
				ready = append(ready, step.ID)
			}
		}
	case TypeExpansion:
		for _, tmpl := range f.Template {
			if completed[tmpl.ID] {
				continue
			}
			allMet := true
			for _, need := range tmpl.Needs {
				if !completed[need] {
					allMet = false
					break
				}
			}
			if allMet {
				ready = append(ready, tmpl.ID)
			}
		}
	case TypeRaid:
		// All legs are ready unless already completed
		for _, leg := range f.Legs {
			if !completed[leg.ID] {
				ready = append(ready, leg.ID)
			}
		}
	case TypeAspect:
		// All aspects are ready unless already completed
		for _, aspect := range f.Aspects {
			if !completed[aspect.ID] {
				ready = append(ready, aspect.ID)
			}
		}
	}

	return ready
}

// GetStep returns a step by ID, or nil if not found.
func (f *Ritual) GetStep(id string) *Step {
	for i := range f.Steps {
		if f.Steps[i].ID == id {
			return &f.Steps[i]
		}
	}
	return nil
}

// GetLeg returns a leg by ID, or nil if not found.
func (f *Ritual) GetLeg(id string) *Leg {
	for i := range f.Legs {
		if f.Legs[i].ID == id {
			return &f.Legs[i]
		}
	}
	return nil
}

// GetTemplate returns a template by ID, or nil if not found.
func (f *Ritual) GetTemplate(id string) *Template {
	for i := range f.Template {
		if f.Template[i].ID == id {
			return &f.Template[i]
		}
	}
	return nil
}

// GetAspect returns an aspect by ID, or nil if not found.
func (f *Ritual) GetAspect(id string) *Aspect {
	for i := range f.Aspects {
		if f.Aspects[i].ID == id {
			return &f.Aspects[i]
		}
	}
	return nil
}
