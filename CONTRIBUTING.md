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

For detailed instructions on development setup, workflow, code style, testing requirements, and more, please see our complete [Contributing Guide](https://medizininformatik-initiative.github.io/aether/development/contributing.html).

## Prerequisites

- Go 1.21+
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
make test TEST=TestName

# Format and lint code
make lint

# Build documentation
cd docs && npm install && npm run docs:build
```

## Code of Conduct

By participating in this project, you agree to abide by our community standards and treat all contributors with respect.

## Questions?

- **Documentation**: Check out our [documentation site](https://medizininformatik-initiative.github.io/aether/)
- **Issues**: Search existing [GitHub issues](https://github.com/medizininformatik-initiative/aether/issues)
- **Discussions**: Start a [GitHub discussion](https://github.com/medizininformatik-initiative/aether/discussions)

Thank you for helping make Aether better! 🚀
