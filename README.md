<p align="center">
  <img src="aether.png" alt="Aether Logo" width="200"/>
</p>

# Aether

A command-line tool for processing FHIR healthcare data through configurable pipeline steps.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![Documentation](https://img.shields.io/badge/Documentation-GitHub%20Pages-success?logo=github)](https://medizininformatik-initiative.github.io/aether/)
[![codecov](https://codecov.io/gh/medizininformatik-initiative/aether/branch/main/graph/badge.svg)](https://codecov.io/gh/medizininformatik-initiative/aether)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/medizininformatik-initiative/aether/badge)](https://scorecard.dev/viewer/?uri=github.com/medizininformatik-initiative/aether)
[![OpenSSF Best Practices](https://img.shields.io/cii/level/11528?logo=linuxfoundation&label=ossf%20best%20practices)](https://www.bestpractices.dev/projects/11528)

Aether is a Go command-line tool that orchestrates a configurable FHIR data pipeline for the [Medizininformatik-Initiative](https://www.medizininformatik-initiative.de/) (MII). It chains steps such as TORCH/CRTDL import, DIMP pseudonymization, bundle splitting, flattening to CSV, and send — with `wait` checkpoints for manual inspection and zstd compression between stages. Aether is built for data stewards and integration engineers who move MII FHIR data between research and clinical systems.

## What Does Aether Do?

Aether helps you:
1. **Extract** patient data from a TORCH server using CRTDL query files
2. **Pseudonymize** the data using a DIMP service to protect patient privacy
3. **Flatten** FHIR data to CSV using SQL-on-FHIR ViewDefinitions
4. **Send** processed data to FHIR servers or DSF transfer systems

## Installation

For Linux and macOS:

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

Create an `aether.yaml` file (any path is fine — pass it as the first positional arg):

```yaml
services:
  torch:
    base_url: "https://your-torch-server.org"
    username: "your-username"
    password: "your-password"

  dimp:
    url: "http://your-dimp-server:32861"  # server root; /fhir appended by client

pipeline:
  enabled_steps:
    - torch
    - dimp

jobs_dir: "./jobs"
```

## Usage

Every subcommand takes the path to your `aether.yaml` as the first positional argument.

### Run a Pipeline

```bash
aether pipeline start aether.yaml your-crtdl.json
```

### Check Status

```bash
aether job list aether.yaml
aether pipeline status aether.yaml <job-id>
```

### Resume a Pipeline

```bash
aether pipeline continue aether.yaml <job-id>
```

## More Information

- [Full Documentation](https://medizininformatik-initiative.github.io/aether/)
- [Configuration Options](https://medizininformatik-initiative.github.io/aether/v0.10.1/getting-started/configuration.html)
- [Pipeline Steps](https://medizininformatik-initiative.github.io/aether/v0.10.1/guides/pipeline-steps.html)

## License

Apache License 2.0 - see [LICENSE](LICENSE)
