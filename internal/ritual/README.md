# Ritual Package

TOML-based workflow definitions with validation, cycle detection, and execution planning.

## Overview

The ritual package parses and validates structured workflow definitions, enabling:

- **Type inference** - Automatically detect ritual type from content
- **Validation** - Check required fields, unique IDs, valid references
- **Cycle detection** - Prevent circular dependencies
- **Topological sorting** - Compute dependency-ordered execution
- **Ready computation** - Find steps with satisfied dependencies

## Installation

```go
import "github.com/OWNER/horde/internal/ritual"
```

## Quick Start

```go
// Parse a ritual file
f, err := ritual.ParseFile("workflow.ritual.toml")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Ritual: %s (type: %s)\n", f.Name, f.Type)

// Get execution order
order, _ := f.TopologicalSort()
fmt.Printf("Execution order: %v\n", order)

// Track and execute
completed := make(map[string]bool)
for len(completed) < len(order) {
    ready := f.ReadySteps(completed)
    // Execute ready steps (can be parallel)
    for _, id := range ready {
        step := f.GetStep(id)
        fmt.Printf("Executing: %s\n", step.Title)
        completed[id] = true
    }
}
```

## Ritual Types

### Workflow

Sequential steps with explicit dependencies. Steps execute when all `needs` are satisfied.

```toml
ritual = "release"
description = "Standard release process"
type = "workflow"

[vars.version]
description = "Version to release"
required = true

[[steps]]
id = "test"
title = "Run Tests"
description = "Execute test suite"

[[steps]]
id = "build"
title = "Build Artifacts"
needs = ["test"]

[[steps]]
id = "publish"
title = "Publish Release"
needs = ["build"]
```

### Raid

Parallel legs that execute independently, with optional synthesis.

```toml
ritual = "security-scan"
type = "raid"

[[legs]]
id = "sast"
title = "Static Analysis"
focus = "Code vulnerabilities"

[[legs]]
id = "deps"
title = "Dependency Audit"
focus = "Vulnerable packages"

[[legs]]
id = "secrets"
title = "Secret Detection"
focus = "Leaked credentials"

[synthesis]
title = "Security Report"
description = "Combine all findings"
depends_on = ["sast", "deps", "secrets"]
```

### Expansion

Template-based rituals for parameterized workflows.

```toml
ritual = "component-review"
type = "expansion"

[[template]]
id = "analyze"
title = "Analyze {{component}}"

[[template]]
id = "test"
title = "Test {{component}}"
needs = ["analyze"]
```

### Aspect

Multi-aspect parallel analysis (similar to raid).

```toml
ritual = "code-review"
type = "aspect"

[[aspects]]
id = "security"
title = "Security Review"
focus = "OWASP Top 10"

[[aspects]]
id = "performance"
title = "Performance Review"
focus = "Complexity and bottlenecks"

[[aspects]]
id = "maintainability"
title = "Maintainability Review"
focus = "Code clarity and documentation"
```

## API Reference

### Parsing

```go
// Parse from file
f, err := ritual.ParseFile("path/to/ritual.toml")

// Parse from bytes
f, err := ritual.Parse([]byte(tomlContent))
```

### Validation

Validation is automatic during parsing. Errors are descriptive:

```go
f, err := ritual.Parse(data)
// Possible errors:
// - "ritual field is required"
// - "invalid ritual type \"foo\""
// - "duplicate step id: build"
// - "step \"deploy\" needs unknown step: missing"
// - "cycle detected involving step: a"
```

### Execution Planning

```go
// Get dependency-sorted order
order, err := f.TopologicalSort()

// Find ready steps given completed set
completed := map[string]bool{"test": true, "lint": true}
ready := f.ReadySteps(completed)

// Lookup individual items
step := f.GetStep("build")
leg := f.GetLeg("sast")
tmpl := f.GetTemplate("analyze")
aspect := f.GetAspect("security")
```

### Dependency Queries

```go
// Get all item IDs
ids := f.GetAllIDs()

// Get dependencies for a specific item
deps := f.GetDependencies("build")  // Returns ["test"]
```

## Embedded Rituals

The package embeds common rituals for Horde workflows:

```go
// Provision embedded rituals to a relics workspace
count, err := ritual.ProvisionFormulas("/path/to/workspace")

// Check ritual health (outdated, modified, etc.)
report, err := ritual.CheckFormulaHealth("/path/to/workspace")

// Update rituals safely (preserves user modifications)
updated, skipped, reinstalled, err := ritual.UpdateFormulas("/path/to/workspace")
```

## Testing

```bash
go test ./internal/ritual/... -v
```

The package has 130% test coverage (1,200 lines of tests for 925 lines of code).

## Dependencies

- `github.com/BurntSushi/toml` - TOML parsing (stable, widely-used)

## License

MIT License - see repository LICENSE file.
