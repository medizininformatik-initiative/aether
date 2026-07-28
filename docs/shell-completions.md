# Shell Completions for Aether

Aether provides tab completion for bash, zsh, fish, and PowerShell shells.

## Quick Installation

### Automatic Installation (Recommended)

```bash
# Build aether first
make build

# Install completions for your shell
make install-completions
# or
./scripts/install-completions.sh
```

The script will detect your shell and install completions automatically.

## Manual Installation

### Zsh (oh-my-zsh)

**Method 1: Plugin (Recommended for oh-my-zsh users)**

```bash
# Create plugin directory
mkdir -p ~/.oh-my-zsh/custom/plugins/aether

# Generate completion file
aether completion zsh > ~/.oh-my-zsh/custom/plugins/aether/_aether

# Add to your ~/.zshrc plugins array
plugins=(... aether)

# Reload shell
exec zsh
```

**Method 2: Standard zsh completion directory**

```bash
# Create completion directory
mkdir -p ~/.zsh/completions

# Generate completion file
aether completion zsh > ~/.zsh/completions/_aether

# Add to ~/.zshrc (if not already present)
echo 'fpath=(~/.zsh/completions $fpath)' >> ~/.zshrc
echo 'autoload -U compinit && compinit' >> ~/.zshrc

# Reload shell
exec zsh
```

### Bash

**Linux:**
```bash
# System-wide (requires sudo)
aether completion bash | sudo tee /etc/bash_completion.d/aether

# User-only (no sudo)
mkdir -p ~/.bash_completion.d
aether completion bash > ~/.bash_completion.d/aether
echo 'source ~/.bash_completion.d/aether' >> ~/.bashrc

# Reload shell
source ~/.bashrc
```

**macOS (with Homebrew):**
```bash
aether completion bash > $(brew --prefix)/etc/bash_completion.d/aether
source ~/.bashrc
```

### Fish

```bash
# Create completion directory
mkdir -p ~/.config/fish/completions

# Generate completion file
aether completion fish > ~/.config/fish/completions/aether.fish

# Restart fish shell
```

### PowerShell

```powershell
# One-time load
aether completion powershell | Out-String | Invoke-Expression

# Persistent (add to profile)
aether completion powershell > aether.ps1
# Then add '. /path/to/aether.ps1' to your PowerShell profile
```

## What Gets Completed?

Aether completions provide suggestions for:

- **Commands**: `pipeline`, `job`, `completion`
- **Subcommands**:
  - `pipeline start`, `pipeline status`, `pipeline continue`
  - `job list`, `job run`
  - `completion bash`, `completion zsh`, `completion fish`, `completion powershell`
- **Flags**: `--verbose`, `--help`, `--version`, `--no-progress`, `--dir`, `--allow-http-crtdl`, `--step`, `--force`
- **File paths**: Autocomplete paths for `pipeline start` input

## Examples

```bash
# Type and press TAB
aether <TAB>
# Shows: pipeline  job  completion  help

aether pipeline <TAB>
# Shows: start  status  continue

aether pipeline start <TAB>
# Shows: files and directories in current path

aether --<TAB>
# Shows: --verbose  --help  --version
```

## Troubleshooting

### Completions not working after installation

**Zsh/oh-my-zsh:**
```bash
# Clear completion cache
rm -f ~/.zcompdump*
compinit

# Or restart shell
exec zsh
```

**Bash:**
```bash
# Reload completions
source ~/.bashrc
```

### "command not found: compdef" error (Zsh)

Enable zsh completions first:
```bash
echo "autoload -U compinit; compinit" >> ~/.zshrc
source ~/.zshrc
```

### oh-my-zsh not recognizing plugin

1. Verify plugin directory exists: `ls ~/.oh-my-zsh/custom/plugins/aether/`
2. Check `_aether` file exists in that directory
3. Verify `aether` is in your plugins array: `grep "plugins=" ~/.zshrc`
4. Restart shell: `exec zsh`

## Uninstallation

### oh-my-zsh
```bash
# Remove plugin directory
rm -rf ~/.oh-my-zsh/custom/plugins/aether

# Remove from plugins array in ~/.zshrc
# Edit and remove 'aether' from: plugins=(... aether ...)

# Reload
exec zsh
```

### Standard zsh
```bash
rm ~/.zsh/completions/_aether
# Remove fpath line from ~/.zshrc if no longer needed
```

### Bash
```bash
# Linux system-wide
sudo rm /etc/bash_completion.d/aether

# Linux user-only
rm ~/.bash_completion.d/aether

# macOS
rm $(brew --prefix)/etc/bash_completion.d/aether
```

### Fish
```bash
rm ~/.config/fish/completions/aether.fish
```

## Development

If you're developing Aether and testing completions:

```bash
# Rebuild and reinstall completions
make build && make install-completions

# Test completion generation
./bin/aether completion zsh | head -30

# Test without installing
source <(./bin/aether completion zsh)
```

## See Also

- [Cobra Shell Completions](https://cobra.dev/docs/user_guide/#shell-completions)
- [zsh Completion System](http://zsh.sourceforge.net/Doc/Release/Completion-System.html)
- [bash Programmable Completion](https://www.gnu.org/software/bash/manual/html_node/Programmable-Completion.html)
