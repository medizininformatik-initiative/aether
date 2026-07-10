# Coding Guidelines

Code style and standards for Aether development. All code must follow these guidelines to maintain consistency and quality across the project.

## Go Code Style

### General Principles

- **Follow the Go conventions**: Use standard Go formatting and idioms
- **Simplicity first**: Prefer clear, simple code over clever solutions
- **Explicit over implicit**: Make intentions obvious to readers
- **Error handling**: Always handle errors explicitly
- **No global state**: Use dependency injection for dependencies

### Formatting

Use standard Go formatting tools:

```bash
# Format code (and order imports via golangci-lint fmt / gci)
gofmt -w .
golangci-lint fmt ./...

# Format, vet, and run all tests
make check
```

- **Indentation**: Tabs (not spaces)
- **Line length**: No strict limit, but keep lines readable (consider 80-100 chars for clarity)
- **Blank lines**: Use sparingly to separate logical groups

### Example: Well-Formatted Code

```go
package pipeline

import (
    "context"
    "errors"
    "fmt"

    "github.com/medizininformatik-initiative/aether/internal/models"
    "github.com/medizininformatik-initiative/aether/internal/services"
)

// ImportStep processes FHIR NDJSON files.
func ImportStep(ctx context.Context, job *models.PipelineJob, config *models.Config) error {
    if job == nil {
        return errors.New("job cannot be nil")
    }

    entries := 0
    err := services.ImportFiles(ctx, job.DataPath, func(resource string) error {
        entries++
        return nil
    })
    if err != nil {
        return fmt.Errorf("import failed: %w", err)
    }

    return nil
}
```

## Naming Conventions

### Package Names

- Use **lowercase**, single-word names
- Reflect the package's purpose

```go
// ✅ Good
package pipeline
package models
package services

// ❌ Bad
package Pipeline
package Models
package my_services
```

### Function Names

- Use **CamelCase**
- **Exported functions** (public): Start with **UPPERCASE**
- **Unexported functions** (private): Start with **lowercase**

```go
// ✅ Exported
func ImportStep(ctx context.Context, job *models.PipelineJob) error { ... }
func ProcessBundle(bundle *models.Bundle) error { ... }

// ✅ Unexported
func validateInput(data string) error { ... }
func parseNDJSON(reader io.Reader) ([]string, error) { ... }
```

### Variable Names

- Use **descriptive names** (avoid single letters except in loops/contexts)
- Use **CamelCase** for local variables

```go
// ✅ Good
for i, entry := range entries {
    ...
}

config := &models.Config{}
importedCount := 0
userData := make(map[string]interface{})

// ❌ Bad
for i, e := range entries { ... }  // 'e' is unclear
c := &models.Config{}              // 'c' is unclear
ic := 0                            // 'ic' is unclear
```

### Constants

- Use **MixedCaps** (not `UPPERCASE_WITH_UNDERSCORES`) — idiomatic Go, and what
  this codebase uses everywhere
- Prefer **typed constants** for enumerated values (e.g. `StepName`, `StepStatus`)
- Export with an uppercase first letter, keep package-private with lowercase

```go
// ✅ Good
const (
    DefaultTimeoutMinutes = 30
    MaxRetryAttempts      = 5
)

// ✅ Good: typed constants for an enumerated set
type StepName string

const (
    StepTorchImport StepName = "torch"
    StepDIMP        StepName = "dimp"
)

func doWork() {
    const maxBundleSize = 10 * 1024 * 1024  // Local constant
    ...
}

// ❌ Bad: screaming snake case is not idiomatic Go
const MAX_RETRY_ATTEMPTS = 5
```

### Interface Names

- Use **Reader/Writer convention** for single-method interfaces
- Use **-er suffix** for behavior-describing interfaces

```go
// ✅ Good
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Extractor interface {
    Extract(ctx context.Context, query string) (io.Reader, error)
}

// ❌ Bad
type IExtractor interface { ... }     // Don't use I- prefix
type ExtractorInterface interface { } // Don't use -Interface suffix
```

## Code Organization

### File Structure

Organize code logically within files:

```go
package pipeline

import (
    // Standard library
    "context"
    "errors"
    "fmt"

    // External packages
    "github.com/vendor/package"

    // Internal packages
    "github.com/medizininformatik-initiative/aether/internal/models"
    "github.com/medizininformatik-initiative/aether/internal/services"
)

// Interfaces
type Processor interface {
    Process(ctx context.Context, data string) error
}

// Types
type ImportStep struct {
    config *models.Config
}

// Constructors
func NewImportStep(config *models.Config) *ImportStep {
    return &ImportStep{config: config}
}

// Methods
func (s *ImportStep) Process(ctx context.Context, data string) error {
    ...
}

// Helper functions
func processEntry(entry string) error {
    ...
}
```

### Imports

- **Group imports**: Standard library, external, then internal
- **Organize**: Alphabetically within groups
- **No blank imports**: Except when explicitly used (add comment)

```go
// ✅ Good
import (
    "context"
    "fmt"

    "github.com/vendor/package"

    "github.com/medizininformatik-initiative/aether/internal/models"
)

// ❌ Bad
import (
    "github.com/vendor/package"
    "context"
    "github.com/medizininformatik-initiative/aether/internal/models"
    "fmt"
)
```

## Functions & Methods

### Function Signatures

Keep functions focused and simple:

```go
// ✅ Good: Clear, focused, testable
func ImportStep(ctx context.Context, job *models.PipelineJob, config *models.Config) error {
    ...
}

// ❌ Bad: Too many parameters
func DoEverything(ctx, config, job, service1, service2, service3 string, flags ...interface{}) (interface{}, interface{}, error) {
    ...
}
```

### Error Handling

Always handle errors explicitly:

```go
// ✅ Good
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}

// ❌ Bad
if err != nil {
    panic(err)                    // Never panic in library code
}

if err != nil {
    fmt.Println("error:", err)    // Don't silently ignore
}

// ❌ Bad (Go anti-pattern)
value, _ := someFunc()            // Never discard errors with _
```

### Return Values

- **Errors last**: Always put error as the last return value
- **Named returns**: Use sparingly; only for clarity

```go
// ✅ Good
func ExtractData(ctx context.Context, url string) (io.Reader, error) {
    ...
}

// ✅ Named returns for clarity
func ProcessJob(ctx context.Context, job *Job) (count int, err error) {
    ...
}

// ❌ Bad: Error not last
func ExtractData(ctx context.Context, url string) (error, io.Reader) {
    ...
}
```

## Comments

### Function Documentation

- Use **godoc style**: Start with function name
- Explain **why**, not **what** (code shows what)
- Keep comments concise

```go
// ✅ Good
// ImportStep processes FHIR NDJSON files from the given path.
// It validates each entry and stores normalized bundles in the job directory.
func ImportStep(ctx context.Context, job *PipelineJob, config *Config) error {
    ...
}

// ❌ Bad
// imports the data
func ImportStep(ctx context.Context, job *PipelineJob, config *Config) error {
    ...
}
```

### Inline Comments

Explain non-obvious logic:

```go
// ✅ Good: Explains WHY
// Retry with exponential backoff for transient errors
// (network timeouts, temporary unavailability)
for attempt := 0; attempt < maxAttempts; attempt++ {
    if err := tryOperation(); err == nil {
        return nil
    }
    time.Sleep(backoffDuration(attempt))
}

// ❌ Bad: Just repeats the code
// loop through entries
for _, entry := range entries {
    process(entry)  // process the entry
}
```

### Package Documentation

Every package should have package-level documentation:

```go
// Package pipeline orchestrates FHIR data processing.
//
// The pipeline executes a series of steps in order:
// 1. TORCH extraction (if enabled)
// 2. Import: Parse and validate FHIR
// 3. DIMP: Pseudonymization
// 4. Additional transformation steps
package pipeline
```

## Functional Programming Principles

### Immutability

- Prefer immutable data structures
- Use pointers only when necessary for efficiency
- Don't mutate input parameters

```go
// ✅ Good: Returns new value, doesn't mutate input
func NormalizeEntry(entry *FHIREntry) *FHIREntry {
    normalized := &FHIREntry{
        ID:        entry.ID,
        Type:      entry.Type,
        Timestamp: time.Now(),
    }
    return normalized
}

// ❌ Bad: Mutates input
func NormalizeEntry(entry *FHIREntry) {
    entry.Timestamp = time.Now()
}
```

### Pure Functions

- Functions should have no side effects
- Same input should produce same output
- Keep I/O separate from logic

```go
// ✅ Good: Pure function
func CalculateChecksum(data string) string {
    hash := sha256.Sum256([]byte(data))
    return hex.EncodeToString(hash[:])
}

// ✅ Good: Side effect isolated in service
func SaveResults(ctx context.Context, service *FileService, data string) error {
    return service.WriteFile(ctx, "results.json", data)
}

// ❌ Bad: Mixed concerns
func ProcessAndSave(data string) error {
    hash := CalculateChecksum(data)
    return ioutil.WriteFile("results.json", []byte(hash), 0644)  // I/O in business logic
}
```

### Function Composition

Build complex logic from simple functions:

```go
// ✅ Good: Simple functions that compose
func FilterValidEntries(entries []Entry) []Entry {
    var valid []Entry
    for _, e := range entries {
        if IsValid(e) {
            valid = append(valid, e)
        }
    }
    return valid
}

func TransformEntries(entries []Entry) []Entry {
    var transformed []Entry
    for _, e := range entries {
        transformed = append(transformed, Transform(e))
    }
    return transformed
}

// Usage: Compose functions
result := TransformEntries(FilterValidEntries(input))

// ❌ Bad: Does too much in one function
func ProcessEntries(entries []Entry) []Entry {
    var result []Entry
    for _, e := range entries {
        if IsValid(e) {
            t := Transform(e)
            result = append(result, t)
        }
    }
    return result
}
```

## Error Handling

### Error Messages

- Be **specific** about what went wrong
- Include **context** for debugging
- Use **fmt.Errorf** with **%w** for wrapping

```go
// ✅ Good
if err := validateCRTDL(crtdlPath); err != nil {
    return fmt.Errorf("invalid CRTDL query: %w", err)
}

// ✅ Good: Provides context
if fileSize > maxSize {
    return fmt.Errorf("bundle too large: %d bytes (max: %d)", fileSize, maxSize)
}

// ❌ Bad: Too vague
return errors.New("error")
return errors.New("failed")

// ❌ Bad: Lost error chain
if err != nil {
    return errors.New("failed to import")  // Original error lost
}
```

### Error Types

Define custom errors for specific cases:

```go
// ✅ Good
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation error in %s: %s", e.Field, e.Message)
}

// Usage
if !IsValidInput(input) {
    return &ValidationError{
        Field:   "cohort",
        Message: "cohort criteria missing",
    }
}
```

## Testing Best Practices

### Test Naming

```go
// ✅ Good: Clear, descriptive test names
func TestImportStep_ValidNDJSON_Success(t *testing.T) { ... }
func TestImportStep_InvalidNDJSON_ReturnsError(t *testing.T) { ... }
func TestImportStep_EmptyDirectory_HandledGracefully(t *testing.T) { ... }

// ❌ Bad
func TestImportStep(t *testing.T) { ... }
func Test1(t *testing.T) { ... }
```

### Test Structure (AAA Pattern)

```go
func TestImportStep(t *testing.T) {
    // Arrange: Set up test data
    job := &PipelineJob{
        DataPath: "testdata/valid",
    }
    config := &Config{}

    // Act: Perform the action
    err := ImportStep(context.Background(), job, config)

    // Assert: Verify results
    assert.NoError(t, err)
    assert.Equal(t, 42, job.EntryCount)
}
```

## Common Mistakes to Avoid

### ❌ Don't

- Use `panic` in library code
- Create unexported interfaces (name should start with lowercase)
- Mutate function parameters
- Use `interface{}` when specific types would work
- Ignore errors
- Use global variables or state
- Add unnecessary external dependencies

### ✅ Do

- Return errors explicitly
- Use specific types
- Keep functions small and focused
- Document exported APIs
- Write tests for all exported functions
- Use dependency injection
- Keep external dependencies minimal

## Performance Considerations

- **Don't optimize prematurely**: Write clear code first
- **Profile before optimizing**: Use `pprof` to identify bottlenecks
- **Benchmark critical paths**: Use `testing.B` for benchmarks
- **Stream large data**: Don't load entire files into memory

## Next Steps

- [Testing Guidelines](./testing.md) - Write effective tests
- [Contributing](./contributing.md) - How to contribute changes
- [Architecture](./architecture.md) - System design overview
