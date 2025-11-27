<p align="center">
  <img src="aether.png" alt="Aether Logo" width="200"/>
</p>

# Aether

A command-line tool for processing FHIR healthcare data through TORCH extraction and DIMP pseudonymization.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![Documentation](https://img.shields.io/badge/Documentation-GitHub%20Pages-success?logo=github)](https://medizininformatik-initiative.github.io/aether/)
[![codecov](https://codecov.io/gh/medizininformatik-initiative/aether/branch/main/graph/badge.svg)](https://codecov.io/gh/medizininformatik-initiative/aether)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/medizininformatik-initiative/aether/badge)](https://scorecard.dev/viewer/?uri=github.com/medizininformatik-initiative/aether)

## What Does Aether Do?

Aether helps you:
1. **Extract** patient data from a TORCH server using CRTDL query files
2. **Pseudonymize** the data using a DIMP service to protect patient privacy

## Installation

Aether is available as binary for Linux, macOS and Windows.

For Linux and macOS, an install script is provided. It downloads the binary and verifies GitHub attestations using the GitHub CLI tool (if installed):

```bash
curl -sSfL https://raw.githubusercontent.com/medizininformatik-initiative/aether/main/install.sh | sh
sudo mv aether /usr/local/bin/
```

For manual installation or Windows, download the [latest release](https://github.com/medizininformatik-initiative/aether/releases).

Verify installation:
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

## More Information

- [Full Documentation](https://medizininformatik-initiative.github.io/aether/)
- [Configuration Options](https://medizininformatik-initiative.github.io/aether/getting-started/configuration)

## License

Apache License 2.0 - see [LICENSE](LICENSE)
