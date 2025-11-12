# Installation

This guide walks you through installing Aether on your system.

## Prerequisites

Optional tools:
- TORCH server access (for FHIR data extraction)
- DIMP service (for pseudonymization features)

## Installation from Release (Recommended)

The easiest way to install Aether is to download a precompiled release binary for your platform.

### 1. Download the Release

Visit the [Aether releases page](https://github.com/medizininformatik-initiative/aether/releases) and download the appropriate binary for your platform:

- **macOS (Intel)**: `aether-0.1.0-darwin-amd64.tar.gz`
- **macOS (ARM/Apple Silicon)**: `aether-0.1.0-darwin-arm64.tar.gz`
- **Linux (x86-64)**: `aether-0.1.0-linux-amd64.tar.gz`
- **Windows (Intel)**: `aether-0.1.0-windows-amd64.zip`
- **Windows (ARM)**: `aether-0.1.0-windows-arm64.zip`

### 2. Extract the Archive

**macOS and Linux:**

```bash
tar -xzf aether-0.1.0-linux-amd64.tar.gz
# Or for macOS: tar -xzf aether-0.1.0-darwin-amd64.tar.gz
```

**Windows:**

```powershell
Expand-Archive aether-0.1.0-windows-amd64.zip -DestinationPath .
```

This creates an `aether` binary in your current directory.

### 3. Install to System PATH

**Option A: System-wide installation (requires sudo, macOS/Linux)**

```bash
sudo mv aether /usr/local/bin/
```

**Option B: User-local installation (no sudo required)**

**macOS and Linux:**

```bash
mkdir -p ~/.local/bin
mv aether ~/.local/bin/

# Ensure ~/.local/bin is in your PATH:
echo $PATH | grep '.local/bin'

# If not present, add to your shell configuration (~/.bashrc, ~/.zshrc, etc.):
export PATH="$HOME/.local/bin:$PATH"
```

**Windows:**

Move `aether.exe` to a directory in your PATH, or create a folder and add it to your PATH environment variable.

## Installation from Source

For developers who want to build Aether from source or contribute to the project.

### Prerequisites

Ensure you have:

- **Go 1.25 or later**
- **make** (for building from source)
- **git** (for cloning the repository)

### 1. Clone the Repository

```bash
git clone https://github.com/medizininformatik-initiative/aether.git
cd aether
```

### 2. Build Aether

Using the Makefile:

```bash
make build
```

This compiles the Aether binary for your system.

### 3. Install to System PATH

**Option A: System-wide installation (requires sudo)**

```bash
sudo make install
# Installs to /usr/local/bin
```

**Option B: User-local installation (no sudo required)**

```bash
make install-local
# Installs to ~/.local/bin
# Ensure ~/.local/bin is in your PATH
```

To verify ~/.local/bin is in your PATH:

```bash
echo $PATH | grep '.local/bin'
```

If not present, add to your shell configuration file (~/.bashrc, ~/.zshrc, etc.):

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Verification

Verify the installation by checking the version:

```bash
aether --help
```

You should see the help output with available commands and options.

## Shell Completions (Optional)

For improved command-line experience, install shell completions.

### For Zsh (oh-my-zsh)

Automatic installation:

```bash
./scripts/install-completions.sh
```

Or manual installation:

```bash
mkdir -p ~/.oh-my-zsh/custom/plugins/aether
aether completion zsh > ~/.oh-my-zsh/custom/plugins/aether/_aether
```

Then add 'aether' to the plugins array in ~/.zshrc:

```bash
plugins=(... aether)
```

Reload your shell:

```bash
exec zsh
```

### For Bash

```bash
aether completion bash | sudo tee /etc/bash_completion.d/aether
```

Then reload your shell:

```bash
exec bash
```

### For Fish or Other Shells

For comprehensive shell completion setup for fish, bash, zsh, and other shells:

```bash
./scripts/install-completions.sh
```

Or see manual instructions:

```bash
aether completion --help
```

## Next Steps

- [Quick Start Guide](./quick-start.md) - Get started with your first pipeline
- [Configuration Guide](./configuration.md) - Learn how to configure Aether for your environment
