# Installation

## Download

1. Go to the [Releases page](https://github.com/medizininformatik-initiative/aether/releases)
2. Download the file for your system:
   - **Linux**: `aether-X.X.X-linux-amd64.tar.gz`
   - **macOS Intel**: `aether-X.X.X-darwin-amd64.tar.gz`
   - **macOS Apple Silicon**: `aether-X.X.X-darwin-arm64.tar.gz`
   - **Windows**: `aether-X.X.X-windows-amd64.zip`

## Install

### Linux / macOS

```bash
# Extract the archive
tar -xzf aether-*.tar.gz

# Move to a directory in your PATH
sudo mv aether /usr/local/bin/
```

**Without sudo:**
```bash
mkdir -p ~/.local/bin
mv aether ~/.local/bin/

# Add to PATH if not already (add to ~/.bashrc or ~/.zshrc)
export PATH="$HOME/.local/bin:$PATH"
```

### Windows

1. Extract the zip file
2. Move `aether.exe` to a folder in your PATH, or add its location to your PATH

## Verify

```bash
aether --help
```

You should see the available commands.

## Next Steps

- [Quick Start](./quick-start.md) - Run your first pipeline
- [Configuration](./configuration.md) - Set up your services
