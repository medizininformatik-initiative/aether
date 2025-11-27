<p align="center">
  <img src="aether.png" alt="Aether Logo" width="200"/>
</p>

# Aether

A command-line tool for processing FHIR healthcare data through TORCH extraction and DIMP pseudonymization.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Documentation](https://img.shields.io/badge/Documentation-GitHub%20Pages-success?logo=github)](https://medizininformatik-initiative.github.io/aether/)

## What Does Aether Do?

Aether helps you:
1. **Extract** patient data from a TORCH server using CRTDL query files
2. **Pseudonymize** the data using a DIMP service to protect patient privacy

## Installation

### Download a Release (Recommended)

1. Go to the [Releases page](https://github.com/medizininformatik-initiative/aether/releases)
2. Download the file for your system:
   - **Linux**: `aether-X.X.X-linux-amd64.tar.gz`
   - **macOS Intel**: `aether-X.X.X-darwin-amd64.tar.gz`
   - **macOS Apple Silicon**: `aether-X.X.X-darwin-arm64.tar.gz`
   - **Windows**: `aether-X.X.X-windows-amd64.zip`

3. Extract and install:

   **Linux/macOS:**
   ```bash
   tar -xzf aether-*.tar.gz
   sudo mv aether /usr/local/bin/
   ```

   **Windows:** Extract the zip and add the folder to your PATH.

4. Verify installation:
   ```bash
   aether --help
   ```

## Configuration

Create a file named `aether.yaml` in your working directory:

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"

  dimp:
    url: "http://your-dimp-server:32861/fhir"

pipeline:
  enabled_steps:
    - torch
    - dimp

jobs_dir: "./jobs"
```

Replace the URLs and credentials with your actual server details.

## Usage

### Run a Pipeline

```bash
aether pipeline start your-query.crtdl
```

This will:
1. Send your CRTDL query to TORCH
2. Wait for extraction to complete
3. Download the FHIR data
4. Send it to DIMP for pseudonymization
5. Save the results in the `jobs` folder

### Check Status

```bash
# List all jobs
aether job list

# Check a specific job
aether pipeline status <job-id>
```

### Resume a Failed Pipeline

If something goes wrong, you can resume:

```bash
aether pipeline continue <job-id>
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| "TORCH server unreachable" | Check `base_url` in your config and network connection |
| "Authentication failed" | Verify your username and password |
| "DIMP service unavailable" | Check the DIMP URL and that the service is running |

## More Information

- [Full Documentation](https://medizininformatik-initiative.github.io/aether/)
- [Configuration Options](https://medizininformatik-initiative.github.io/aether/getting-started/configuration)

## License

Apache License 2.0 - see [LICENSE](LICENSE)
