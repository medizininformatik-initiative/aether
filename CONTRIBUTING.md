# Contributing to Aether

Thank you for your interest in contributing to Aether! We welcome all contributions, from bug reports and documentation improvements to new features and code optimizations.

## Quick Start

Contributions follow a standard GitHub workflow:

1. **Fork** the repository
2. **Create a feature branch** (`git checkout -b feature/your-feature`)
3. **Make your changes**
4. **Run tests** (`make test`)
5. **Submit a pull request**

## Full Contribution Guide

For detailed instructions on development setup, workflow, code style, testing requirements, and more, please see our complete [Contributing Guide](https://medizininformatik-initiative.github.io/aether/stable/development/contributing.html).

## Prerequisites

- Go 1.26+
- Make
- Git
- Docker & Docker Compose (for integration tests)

## Quick Commands

```bash
# Build the project
make build

# Run tests
make test

# Run a specific test
go test -run TestName ./tests/unit/...

# Lint code
make lint

# Build documentation
cd docs && npm install && npm run docs:build
```

## Requirements for Acceptable Contributions

- All code must be formatted with `gofmt`
- All CI checks must pass
- Maintain or improve test coverage
- Follow [Coding Guidelines](https://medizininformatik-initiative.github.io/aether/stable/development/coding-guidelines.html)

## License

Aether is released under the [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0). By contributing to this project, you agree that your contributions will be licensed under the same license.

## Questions?

- **Documentation**: Check out our [documentation site](https://medizininformatik-initiative.github.io/aether/)
- **Issues**: Search existing [GitHub issues](https://github.com/medizininformatik-initiative/aether/issues)
- **Discussions**: Start a [GitHub discussion](https://github.com/medizininformatik-initiative/aether/discussions)

Thank you for helping make Aether better!
