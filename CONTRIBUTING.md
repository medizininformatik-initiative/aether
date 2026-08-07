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

# Lint documentation prose
make lint-docs

# Build documentation
cd docs && npm install && npm run docs:build
```

## Testing a Development Build

CI builds a binary for every pull request and for every push to `main`. `install-dev.sh` downloads such a build, so a reviewer does not have to compile the code:

```bash
./install-dev.sh 663   # build of pull request 663
./install-dev.sh main  # newest main build
```

The script requires the [GitHub CLI](https://github.com/cli/cli). These builds are unsigned and are for tests only. Use `install.sh` to install a release.

## Requirements for Acceptable Contributions

- All code must be formatted with `gofmt`
- All CI checks must pass
- Maintain or improve test coverage
- Follow [Coding Guidelines](https://medizininformatik-initiative.github.io/aether/stable/development/coding-guidelines.html)

## AI-Assisted Contributions

AI tools are allowed. We don't ask you to disclose their use — but you own
the result either way.

**DCO — you sign, you own it.** Every commit must carry a `Signed-off-by`
line (`git commit -s`). By signing off you certify the
[Developer Certificate of Origin](https://developercertificate.org/): you
understand the code, you have the right to submit it, and **you are
responsible for it** — regardless of how it was written. Never let an AI
tool add its own `Signed-off-by`; only a human can certify the DCO.

CI enforces this. The `DCO` check fails a pull request when any commit lacks
a `Signed-off-by` line matching its author. Merge commits and commits from
dependency bots are exempt. To fix a branch that already exists:

```bash
git rebase --signoff origin/main
git push --force-with-lease
```

You must understand your own changes. If you can't explain what your patch
does and why, it will be closed.

## License

Aether is released under the [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0). By contributing to this project, you agree that your contributions will be licensed under the same license.

## Questions?

- **Documentation**: Check out our [documentation site](https://medizininformatik-initiative.github.io/aether/)
- **Issues**: Search existing [GitHub issues](https://github.com/medizininformatik-initiative/aether/issues)
- **Discussions**: Start a [GitHub discussion](https://github.com/medizininformatik-initiative/aether/discussions)

Thank you for helping make Aether better!
