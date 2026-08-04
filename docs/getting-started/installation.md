# Installation

Aether is available as binary for Linux, macOS and Windows.

## Linux / macOS (Recommended)

An install script is provided. It downloads the binary and verifies GitHub attestations using the GitHub CLI tool (if installed):

```bash
curl -sSfL https://raw.githubusercontent.com/medizininformatik-initiative/aether/main/install.sh | sh
sudo mv aether /usr/local/bin/
```

To install a specific version, pass the version number **without** the `v` prefix:

```bash
curl -sSfL https://raw.githubusercontent.com/medizininformatik-initiative/aether/main/install.sh | sh -s -- 0.3.0
sudo mv aether /usr/local/bin/
```

> **Note:** Use `0.3.0`, not `v0.3.0`. The `v` prefix is added automatically by the install script.

Without sudo:
```bash
curl -sSfL https://raw.githubusercontent.com/medizininformatik-initiative/aether/main/install.sh | sh
mkdir -p ~/.local/bin
mv aether ~/.local/bin/

# Add to PATH if not already (add to ~/.bashrc or ~/.zshrc)
export PATH="$HOME/.local/bin:$PATH"
```

## Manual Installation / Windows

1. Download the [latest release](https://github.com/medizininformatik-initiative/aether/releases) for your system:
   - **Linux**: `aether-X.X.X-linux-amd64.tar.gz`
   - **macOS Intel**: `aether-X.X.X-darwin-amd64.tar.gz`
   - **macOS Apple Silicon**: `aether-X.X.X-darwin-arm64.tar.gz`
   - **Windows**: `aether-X.X.X-windows-amd64.zip`

2. Extract and move to a directory in your PATH

## Development Builds

`install-dev.sh` installs an unsigned build from a pull request or from `main`. Use it to test a change before it is released. Do not use it in production.

The script requires the [GitHub CLI](https://github.com/cli/cli), because a workflow artifact needs an authenticated request.

```bash
# Newest main build
curl -sSfL https://raw.githubusercontent.com/medizininformatik-initiative/aether/main/install-dev.sh | sh

# Build of pull request 663
curl -sSfL https://raw.githubusercontent.com/medizininformatik-initiative/aether/main/install-dev.sh | sh -s -- 663

sudo mv aether /usr/local/bin/
```

With the repository checked out, call the script directly:

```bash
./install-dev.sh main
./install-dev.sh 663
```

A build artifact expires after 14 days. Closing a pull request deletes its artifact immediately.

## Verify

```bash
aether --help
```

## Next Steps

- [Quick Start](./quick-start.md) - Run your first pipeline
- [Configuration](./configuration.md) - Set up your services
