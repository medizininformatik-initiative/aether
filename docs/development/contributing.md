# Contributing

Contributions are welcome. Please follow this guide to contribute to Aether.

## How to Obtain the Software

Clone the repository using Git:

```bash
git clone https://github.com/medizininformatik-initiative/aether.git
cd aether
```

For specific versions, see [Releases](https://github.com/medizininformatik-initiative/aether/releases).

## Providing Feedback

### Bug Reports and Enhancement Suggestions

- **Issues**: [GitHub Issues](https://github.com/medizininformatik-initiative/aether/issues)
- **Security**: See our [Security Policy](https://github.com/medizininformatik-initiative/aether/security/policy)

### Discussions

For questions, ideas, and community engagement:
- [GitHub Discussions](https://github.com/medizininformatik-initiative/aether/discussions)

## Contributing to the Software

### Requirements

- Fork the repository
- Create feature branches
- Follow [coding standards](./coding-guidelines.md)
- Open pull requests linked to existing issues

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

## Requirements for Acceptable Contributions

All contributions must meet these standards:

1. **Code Formatting**: All Go code must be formatted using `gofmt`
2. **Static Analysis**: Code must pass all CI checks including linting
3. **Test Coverage**: Maintain or improve test coverage on changed code
4. **Code Quality**: Write readable code with comments explaining "why", not "what"

See [Coding Guidelines](./coding-guidelines.md) for detailed standards.

## AI-Assisted Contributions

AI tools are allowed. We don't ask you to disclose their use — but you own
the result either way.

**DCO — you sign, you own it.** Every commit must carry a `Signed-off-by`
line (`git commit -s`). By signing off you certify the
[Developer Certificate of Origin](https://developercertificate.org/): you
understand the code, you have the right to submit it, and **you are
responsible for it** — regardless of how it was written. Never let an AI
tool add its own `Signed-off-by`; only a human can certify the DCO.

You must understand your own changes. If you can't explain what your patch
does and why, it will be closed.

## Build Tools

- Go 1.26+ ([download](https://go.dev/dl/))
- Make
- Git
- Docker & Docker Compose (for integration tests)

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

### Code Quality Checks

Before committing, ensure code quality:

```bash
# Format, vet, and run tests
make check

# Run all tests
make test

# Check coverage
make coverage

# Lint documentation prose (only needed when you touch docs/)
make lint-docs
```

## Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/) with these rules:

### Format

```
<type>: <subject>

[optional body]
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

### Subject Line Rules

- Maximum 72 characters
- Use imperative mood ("Add feature" not "Added feature")
- No period at the end

### Examples

**Good:**

```
feat: Add validation step to pipeline
fix: Correct retry backoff calculation
docs: Update TORCH integration guide
refactor: Simplify state persistence logic
test: Add table-driven tests for TORCH import step
```

**Avoid:**

```
fix stuff
update code
Fixed the bug
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
make check      # Format, vet, and test
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

- **All tests pass** - Including unit, integration, and contract tests
- **Code coverage maintained** - No decrease in coverage
- **Functional programming** - Immutability, pure functions, explicit side effects
- **KISS principle** - Simple, understandable code
- **Documentation** - Comments explaining "why", not "what"
- **No unnecessary dependencies** - Use standard library first

### Review Cycle

1. **Submit PR** → Automatic CI checks run
2. **Address feedback** → Maintainers may request changes
3. **Update PR** → Push additional commits with fixes
4. **Approval** → PR approved and ready to merge
5. **Merge** → Squash commits and merge to main

## Development Tips

### Running Specific Tests

```bash
# Run tests for a specific suite
go test -v ./tests/unit/...

# Run specific test function
go test -v ./tests/unit/ -run TestImportStep

# Run with verbose output
go test -v -count=1 ./...

# Run with race detector
go test -race ./...
```

### Debugging

```bash
# Enable debug logging (the -v/--verbose flag)
./bin/aether -v pipeline start aether.yaml crtdl.json

# Run with CPU profile
go test -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof
```

### Testing with Services

```bash
# Start test environment
cd .github/test
make start

# Run full test suite
cd ../..
make test-with-services

# Stop services
cd .github/test
make stop
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
git commit -m "feat: Add validation step to pipeline"
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

# Check prose style and terminology
make lint-docs

# Commit documentation changes
git commit -m "docs: Update TORCH integration guide"
```

#### Prose Linting

`make lint-docs` runs [Vale](https://vale.sh/) over `docs/` and gates on
error-level alerts, exactly as CI does. Run `vale docs` directly to also see
warnings and suggestions, which are advisory and never block a PR.

Configuration lives in `.vale.ini` (Google style, with the rules that do not fit
this project turned off and the reason recorded inline) and `.vale/styles/aether/`:

- `Terms.yml` — project terminology, including `Aether` casing, `OAuth 2.0`, and
  `cancel` over `abort`
- `config/vocabularies/aether/accept.txt` — domain vocabulary

Vale flags unknown words, so a new domain term (a service name, a FHIR acronym)
fails the run until you add it to `accept.txt`. That is deliberate: it keeps the
vocabulary current instead of letting typos through.

## Code Standards

### What We Value

- **Clarity**: Code should be easy to understand
- **Simplicity**: Simple solutions over complex ones
- **Testability**: Code is written to be tested
- **Immutability**: Data structures don't change
- **Composability**: Functions work well together

### What We Avoid

- Unnecessary complexity
- Global state or side effects outside services
- Comments that just repeat the code
- External dependencies without discussion
- Inconsistent error handling

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

## License

Aether is released under the [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

By contributing to this project, you agree that your contributions will be licensed under the same license.

## Next Steps

- [Testing Guidelines](./testing.md) - Write effective tests
- [Coding Guidelines](./coding-guidelines.md) - Code style and standards
- [Architecture](./architecture.md) - System design overview
