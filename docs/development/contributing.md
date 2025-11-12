# Contributing

Contributions welcome! Please follow this workflow to contribute to Aether.

## Getting Started

### Prerequisites

- Go 1.25+ ([download](https://go.dev/dl/))
- Make
- Git
- Docker & Docker Compose (for integration tests)

### Setting Up Your Development Environment

1. **Fork and clone the repository:**

```bash
git clone https://github.com/YOUR_USERNAME/aether.git
cd aether
```

2. **Add upstream remote:**

```bash
git remote add upstream https://github.com/medizininformatik-initiative/aether.git
git fetch upstream
```

3. **Install dependencies and build:**

```bash
make build
```

4. **Run tests to verify setup:**

```bash
make test
```

## Development Workflow

### Before Starting

1. **Ensure your repository is up-to-date:**

```bash
git checkout main
git pull upstream main
```

2. **Run all tests to ensure baseline:**

```bash
make test
```

### Creating a Feature Branch

```bash
# Create a descriptive branch name
git checkout -b feature/your-feature-name
# Examples: feature/add-validation-step, fix/retry-logic, docs/architecture
```

### TDD Development Cycle

Follow strict Test-Driven Development:

1. **Write failing test** (RED phase):
   ```bash
   vim internal/pipeline/your_feature_test.go
   # Write test that describes desired behavior
   go test -v ./internal/pipeline/ -run TestYourFeature
   # Should fail
   ```

2. **Implement minimum code** (GREEN phase):
   ```bash
   vim internal/pipeline/your_feature.go
   # Implement minimum logic to pass test
   go test -v ./internal/pipeline/ -run TestYourFeature
   # Should pass
   ```

3. **Refactor** (REFACTOR phase):
   ```bash
   # Improve code quality while keeping tests green
   make test
   # All tests should still pass
   ```

### Code Quality Checks

Before committing, ensure code quality:

```bash
# Format and lint
make check

# Run all tests
make test

# Check coverage
make coverage
```

### Commit Message Format

Write clear, descriptive commit messages:

```bash
# Good commit messages
git commit -m "feat: add validation step to pipeline"
git commit -m "fix: correct retry backoff calculation"
git commit -m "docs: update TORCH integration guide"
git commit -m "refactor: simplify state persistence logic"
git commit -m "test: add table-driven tests for TORCH import step"

# Avoid vague messages
# ❌ git commit -m "fix stuff"
# ❌ git commit -m "update code"
# ✅ git commit -m "fix: handle empty FHIR bundles gracefully"
```

## Pull Request Process

### Before Opening a PR

1. **Rebase on main:**

```bash
git fetch upstream
git rebase upstream/main
```

2. **Push your branch:**

```bash
git push origin feature/your-feature-name
```

3. **Ensure all checks pass locally:**

```bash
make check      # Format and lint
make test       # Unit tests
make coverage   # Check coverage
```

### Creating a Pull Request

1. **Open a PR on GitHub** with:
   - **Title**: Clear description of what the change does
   - **Description**: Reference the issue (if applicable), explain the change
   - **Examples**:
     - "Add validation step to pipeline"
     - "Fix retry backoff exponential calculation"
     - "Update TORCH integration documentation"

2. **PR Description Template:**

```markdown
## Summary
Brief description of the change.

## Motivation
Why is this change needed?

## Changes
- Detailed list of changes
- One per line

## Testing
How was this tested?
- Unit test: `TestNewFeature`
- Integration test: Test with TORCH + DIMP
- Manual testing: Steps to verify

## Checklist
- [ ] All tests pass locally (`make test`)
- [ ] Code coverage maintained (`make coverage`)
- [ ] Follows functional programming principles
- [ ] No unnecessary external dependencies
- [ ] Documentation updated (if needed)
```

### Code Review Expectations

Your PR will be reviewed for:

- ✅ **All tests pass** - Including unit, integration, and contract tests
- ✅ **Code coverage maintained** - No decrease in coverage
- ✅ **Functional programming** - Immutability, pure functions, explicit side effects
- ✅ **KISS principle** - Simple, understandable code
- ✅ **Documentation** - Comments explaining "why", not "what"
- ✅ **No unnecessary dependencies** - Use standard library first

### Review Cycle

1. **Submit PR** → Automatic CI checks run
2. **Address feedback** → Maintainers may request changes
3. **Update PR** → Push additional commits with fixes
4. **Approval** → PR approved and ready to merge
5. **Merge** → Squash commits and merge to main

## Development Tips

### Running Specific Tests

```bash
# Run tests for specific package
go test -v ./internal/pipeline/...

# Run specific test function
go test -v ./internal/pipeline/ -run TestImportStep

# Run with verbose output
go test -v -count=1 ./...

# Run with race detector
go test -race ./...
```

### Debugging

```bash
# Enable debug logging
AETHER_LOG_LEVEL=debug ./bin/aether pipeline start test.crtdl

# Run with CPU profile
go test -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof
```

### Testing with Services

```bash
# Start test environment
cd .github/test
make services-up

# Run full test suite
cd ../..
make test-with-services

# Stop services
cd .github/test
make services-down
```

## Common Tasks

### Adding a New Pipeline Step

**Note**: The import step types (torch, local_import, http_import) are already implemented. This section is for adding new processing steps after import.

1. **Create model** in `internal/models/step.go`
2. **Write tests** in `tests/unit/{step_name}_test.go`
3. **Implement step** in `internal/pipeline/{step_name}.go`
4. **Update CLI** in `cmd/pipeline.go` if needed
5. **Update docs** in `docs/guides/pipeline-steps.md`

Example:

```bash
# 1. Write test first (TDD!)
vim tests/unit/validation_test.go

# 2. Run test (should fail)
go test -v ./tests/unit/ -run TestValidation

# 3. Implement
vim internal/pipeline/validation.go

# 4. Run test (should pass)
go test -v ./tests/unit/ -run TestValidation

# 5. Verify all tests still pass
make test

# 6. Commit with descriptive message
git commit -m "feat: add validation step to pipeline"
```

### Fixing a Bug

1. **Create test** that reproduces the bug
2. **Verify test fails** (confirms bug exists)
3. **Implement fix** in the code
4. **Verify test passes**
5. **Ensure no regressions** with `make test`

### Updating Documentation

```bash
# Update relevant .md files in docs/
vim docs/guides/torch-integration.md

# Build docs locally to verify (if available)
npm run docs:dev

# Commit documentation changes
git commit -m "docs: update TORCH integration guide"
```

## Code Standards

### What We Value

- **Clarity**: Code should be easy to understand
- **Simplicity**: Simple solutions over complex ones
- **Testability**: Code is written to be tested
- **Immutability**: Data structures don't change
- **Composability**: Functions work well together

### What We Avoid

- ❌ Unnecessary complexity
- ❌ Global state or side effects outside services
- ❌ Comments that just repeat the code
- ❌ External dependencies without discussion
- ❌ Inconsistent error handling

See [Coding Guidelines](./coding-guidelines.md) for detailed standards.

## Getting Help

- **Questions?** Open a discussion on GitHub
- **Found a bug?** Create an issue with reproduction steps
- **Have an idea?** Open an issue to discuss before implementing

## Recognition

Contributors are recognized in:
- Commit history (your name in git)
- Project README (for significant contributions)
- Release notes (for features/fixes included in releases)

## Next Steps

- [Testing Guidelines](./testing.md) - Write effective tests
- [Coding Guidelines](./coding-guidelines.md) - Code style and standards
- [Architecture](./architecture.md) - System design overview
