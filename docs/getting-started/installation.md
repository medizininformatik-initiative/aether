# Installation

Aether is available as binary for Linux, macOS and Windows.

## Linux / macOS (Recommended)

An install script is provided. It downloads the binary and verifies GitHub attestations using the GitHub CLI tool (if installed):

```bash
curl -sSfL https://raw.githubusercontent.com/medizininformatik-initiative/aether/main/install.sh | sh
sudo mv aether /usr/local/bin/
```

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

## Verify

```bash
aether --help
```

## Next Steps

- [Quick Start](./quick-start.md) - Run your first pipeline
- [Configuration](./configuration.md) - Set up your services
