<p align="center">
  <img src="aether.png" alt="Aether Logo" width="200"/>
</p>

# Aether

A command-line interface for orchestrating Data Use Process (DUP) pipelines for medical FHIR data.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![Documentation](https://img.shields.io/badge/Documentation-GitHub%20Pages-success?logo=github)](https://medizininformatik-initiative.github.io/aether/)
[![codecov](https://codecov.io/gh/medizininformatik-initiative/aether/branch/main/graph/badge.svg)](https://codecov.io/gh/medizininformatik-initiative/aether)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/medizininformatik-initiative/aether/badge)](https://scorecard.dev/viewer/?uri=github.com/medizininformatik-initiative/aether)



## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
  - [For End Users](#for-end-users)
  - [For Developers](#for-developers)
- [Common Use Cases](#common-use-cases)
- [TORCH Integration](#torch-integration)
- [Architecture](#architecture)
- [Configuration](#configuration)
- [Development](#development)
  - [Architecture & Testing Strategy](#architecture--testing-strategy)
  - [Advanced Development Tasks](#advanced-development-tasks)
  - [Contributing Workflow](#contributing-workflow)
- [Design Principles](#design-principles)
- [Documentation](#documentation)
- [Roadmap](#roadmap)
- [FAQ](#faq)
- [Contributing](#contributing)
- [License](#license)

## Overview

Aether is a CLI tool designed for medical researchers and data engineers to process FHIR (Fast Healthcare Interoperability Resources) data through configurable pipeline steps. It provides session-independent job management, automatic retry mechanisms, and real-time progress tracking.

### Key Features

- **TORCH Integration**: Direct FHIR data extraction from TORCH servers using CRTDL query files
- **FHIR Data Import**: Import from local directories, HTTP URLs, or TORCH extractions with progress indicators
- **Session-Independent**: Resume pipelines across terminal sessions with file-based state persistence
- **DIMP Pseudonymization**: Optional de-identification and pseudonymization via DIMP HTTP service
- **Configurable Pipelines**: Enable/disable processing steps per project requirements
- **Hybrid Retry Strategy**: Automatic retries for transient errors, manual intervention for validation failures
- **Real-time Progress**: Progress bars with ETA, throughput, and completion percentages
- **Medical Data Focus**: Purpose-built for TORCH FHIR extractions and medical research workflows

## Quick Start

<p align="center">
  <img src="https://private-user-images.githubusercontent.com/8888869/500970240-8544ec9c-1f76-4775-9c1f-ae9f160fda2b.gif?jwt=eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJnaXRodWIuY29tIiwiYXVkIjoicmF3LmdpdGh1YnVzZXJjb250ZW50LmNvbSIsImtleSI6ImtleTUiLCJleHAiOjE3NjA0NDkxNjEsIm5iZiI6MTc2MDQ0ODg2MSwicGF0aCI6Ii84ODg4ODY5LzUwMDk3MDI0MC04NTQ0ZWM5Yy0xZjc2LTQ3NzUtOWMxZi1hZTlmMTYwZmRhMmIuZ2lmP1gtQW16LUFsZ29yaXRobT1BV1M0LUhNQUMtU0hBMjU2JlgtQW16LUNyZWRlbnRpYWw9QUtJQVZDT0RZTFNBNTNQUUs0WkElMkYyMDI1MTAxNCUyRnVzLWVhc3QtMSUyRnMzJTJGYXdzNF9yZXF1ZXN0JlgtQW16LURhdGU9MjAyNTEwMTRUMTMzNDIxWiZYLUFtei1FeHBpcmVzPTMwMCZYLUFtei1TaWduYXR1cmU9YjcwYTE3YWNlYmY2NDY0NWE0YzVmODliMjJlOTRiMmExOWRiMmFjZjJjMjNjZGRiMmNkZTZiYzQ1OWYyZTI0ZiZYLUFtei1TaWduZWRIZWFkZXJzPWhvc3QifQ.XP8HJwLZJwhDP24SjPy660GWEzIzStZbGawitHO3nBA" alt="Aether Quick Start Demo" width="800"/>
</p>

### For End Users

**Prerequisites:**
- Go 1.25 or later (for building from source)
- Optional: TORCH server access (for FHIR data extraction)
- Optional: DIMP service (for pseudonymization)

**1. Install Aether:**

From source:
```bash
git clone https://github.com/medizininformatik-initiative/aether.git
cd aether
make build
sudo make install  # installs to /usr/local/bin
```

Without sudo (installs to `~/.local/bin`):
```bash
make build
make install-local
# Ensure ~/.local/bin is in your PATH
```

Verify installation:
```bash
aether --help
```

**Optional: Install shell completions:**

For oh-my-zsh users:
```bash
# Automatic installation
./scripts/install-completions.sh

# Or manual installation
mkdir -p ~/.oh-my-zsh/custom/plugins/aether
aether completion zsh > ~/.oh-my-zsh/custom/plugins/aether/_aether
# Add 'aether' to plugins array in ~/.zshrc: plugins=(... aether)
# Then: exec zsh
```

For other shells (bash, fish, or standard zsh):
```bash
# Automatic installation
./scripts/install-completions.sh

# Or see manual instructions
aether completion --help
```

**2. Create minimal configuration:**

```bash
# Copy example config
cp config/aether.example.yaml aether.yaml
```

For basic local testing (no external services), edit `aether.yaml`:
```yaml
pipeline:
  enabled_steps:
    - local_import  # For local directory import
    # Or use: torch, http_import depending on your data source

jobs_dir: "./jobs"
```

**3. Run your first pipeline:**

Process local FHIR data:
```bash
# Create test data directory
mkdir -p test-data

# Start pipeline (will create a job ID)
aether pipeline start ./test-data/

# Monitor progress
aether pipeline status <job-id>

# List all jobs
aether job list
```

**4. Enable TORCH integration (optional):**

Add to `aether.yaml`:
```yaml
services:
  torch:
    base_url: "http://localhost:8080"
    username: "your-username"
    password: "your-password"
```

Then run with CRTDL file:
```bash
aether pipeline start query.crtdl
```

**Need help?** See the [Quickstart Guide](specs/001-dup-pipeline-we/quickstart.md) for detailed usage instructions.

### For Developers

**Prerequisites:**
- Go 1.25+ ([download](https://go.dev/dl/))
- Make
- Docker & Docker Compose (for integration tests)
- Git

**1. Clone and setup:**

```bash
git clone https://github.com/medizininformatik-initiative/aether.git
cd aether

# Install dependencies and build
make build

# Run tests to verify setup
make test
```

**2. Run with test environment:**

```bash
# Start TORCH and DIMP test services
cd .github/test
make services-up

# In another terminal, build and test
cd ../..
make build
./bin/aether pipeline start .github/test/torch/queries/example-crtdl.json

# Monitor the pipeline
./bin/aether job list
./bin/aether pipeline status <job-id>

# Stop services when done
cd .github/test
make services-down
```

**3. Development workflow:**

```bash
# Run tests continuously during development
make test

# Run specific test suites
make test-unit           # Unit tests only
make test-integration    # Integration tests
make test-contract       # API contract tests

# Check code coverage
make coverage

# Format and lint
make check

# Build for multiple platforms
make build-all
```

**4. Project structure:**

```
aether/
├── cmd/                 # CLI commands (pipeline, job)
├── internal/
│   ├── models/          # Domain models (Job, Step, Config)
│   ├── pipeline/        # Pipeline orchestration logic
│   ├── services/        # External services (I/O, HTTP)
│   └── ui/              # Progress indicators
├── .github/test/        # Test infrastructure & Docker Compose
├── config/              # Example configurations
├── specs/               # Feature specifications
└── Makefile             # Build and test targets
```

**5. Common tasks:**

```bash
# Add a new pipeline step
# 1. Add step logic in internal/pipeline/
# 2. Add tests in internal/pipeline/*_test.go
# 3. Update internal/models/step.go if needed
# 4. Run: make test

# Debug a failing test
go test -v ./internal/pipeline/... -run TestSpecificTest

# Profile the application
go test -cpuprofile=cpu.prof -memprofile=mem.prof ./...
go tool pprof cpu.prof
```

**6. Troubleshooting:**

| Issue | Solution |
|-------|----------|
| `make: command not found` | Install Make: `sudo apt install make` (Ubuntu) or `brew install make` (macOS) |
| Tests fail with "connection refused" | Ensure test services are running: `cd .github/test && make services-up` |
| `go: version 1.21 required` | Update Go: [https://go.dev/dl/](https://go.dev/dl/) |
| Import tests fail | Check `jobs_dir` exists and has write permissions |

**Need more details?** See [Development](#development) section below and [CLAUDE.md](CLAUDE.md) for coding guidelines.

## Common Use Cases

### 1. Local FHIR Data Processing
**Scenario:** You have FHIR NDJSON files on disk and want to pseudonymize them.

```bash
# Configure only the steps you need
# aether.yaml:
pipeline:
  enabled_steps:
    - local_import  # Or torch, http_import based on your source
    - dimp

services:
  dimp_url: "http://localhost:8083/fhir"

# Run pipeline
aether pipeline start /path/to/fhir/files/
```

### 2. TORCH Cohort Extraction
**Scenario:** Extract patient cohort from TORCH using a CRTDL query.

```bash
# Configure TORCH credentials
# aether.yaml:
services:
  torch:
    base_url: "http://torch.hospital.org"
    username: "researcher"
    password: "secret"

pipeline:
  enabled_steps:
    - torch  # TORCH import via CRTDL or direct TORCH URL

# Submit CRTDL query
aether pipeline start cohort-definition.crtdl
```

### 3. Reprocess Existing TORCH Results
**Scenario:** Re-run pipeline on previously extracted TORCH data.

```bash
# Use TORCH result URL instead of CRTDL
aether pipeline start http://torch.hospital.org/fhir/result/abc-123

# Or download and process locally
curl http://torch.hospital.org/fhir/result/abc-123 -o results/
aether pipeline start ./results/
```

### 4. HTTP Data Import
**Scenario:** Process FHIR data from a web endpoint.

```bash
# Direct URL import
aether pipeline start https://fhir.server.org/export/Patient.ndjson

# Multiple resources (start multiple jobs)
aether pipeline start https://fhir.server.org/export/Observation.ndjson
aether pipeline start https://fhir.server.org/export/Condition.ndjson
```

### 5. Development & Testing
**Scenario:** Test pipeline changes with mock services.

```bash
# Start test environment
cd .github/test && make services-up

# Build and test
make build
./bin/aether pipeline start .github/test/torch/queries/example-crtdl.json

# Watch logs
./bin/aether pipeline status <job-id> --follow
```

### TORCH Integration

Aether supports direct data extraction from TORCH servers using CRTDL (Cohort Representation for Trial Data Linking) files, eliminating manual file downloads.

**What is TORCH?**
TORCH is a FHIR-based data extraction service that allows researchers to define patient cohorts using CRTDL query files and retrieve matching FHIR data automatically.

**Input Methods:**

1. **CRTDL File Extraction** (recommended for new queries):
   ```bash
   aether pipeline start cohort-query.crtdl
   ```
   - Submits CRTDL to TORCH server
   - Polls extraction status until complete
   - Downloads resulting FHIR NDJSON files
   - Continues with pipeline processing

2. **TORCH Result URL** (for reusing existing extractions):
   ```bash
   aether pipeline start http://localhost:8080/fhir/result/abc123
   ```
   - Skips extraction submission
   - Downloads files directly from result URL
   - Useful for resuming or reprocessing data

**Configuration:**

Add TORCH settings to `aether.yaml`:
```yaml
services:
  torch:
    base_url: "http://localhost:8080"
    username: "your-username"
    password: "your-password"
    extraction_timeout_minutes: 30
    polling_interval_seconds: 5
```

**CRTDL File Format:**

CRTDL files must contain:
- `cohortDefinition`: Patient inclusion/exclusion criteria
- `dataExtraction`: FHIR resource types and attributes to extract

See [TORCH quickstart](specs/002-import-via-torch/quickstart.md) for examples and detailed workflow.

**Error Handling:**

- **Server unreachable**: Clear error within 5 seconds
- **Authentication failure**: Fails early with credential error
- **Extraction timeout**: Configurable timeout (default 30 minutes)
- **Empty results**: Gracefully handles zero-patient cohorts
- **Malformed CRTDL**: Validates syntax before submission

**Backward Compatibility:**

Existing workflows using local directories or HTTP URLs continue to work without changes:
```bash
# Still supported
aether pipeline start ./test-data/
aether pipeline start https://example.com/fhir/export
```

## Architecture

Aether follows functional programming principles with clear separation of concerns:

```
┌─────────────┐
│   CLI User  │
└──────┬──────┘
       │ Commands (pipeline, job)
       ▼
┌─────────────────────────────────────────┐
│          Aether CLI (Go)                │
│  ┌─────────────────────────────────┐   │
│  │  Cobra Commands                 │   │
│  │  - pipeline start/continue/status│  │
│  │  - job list                     │   │
│  └──────────┬──────────────────────┘   │
│             ▼                            │
│  ┌─────────────────────────────────┐   │
│  │  Pipeline Orchestrator          │   │
│  │  (Pure Functions + State)       │   │
│  └──────────┬──────────────────────┘   │
│             ▼                            │
│  ┌─────────────────────────────────┐   │
│  │  Services (Side Effects)        │   │
│  │  - HTTP Client (DIMP)           │   │
│  │  - File I/O (Import/Save)       │   │
│  │  - State Persistence (JSON)     │   │
│  └──────────┬──────────────────────┘   │
└─────────────┼──────────────────────────┘
              │
    ┌─────────┴─────────┐
    ▼                   ▼
┌───────┐           ┌───────────┐
│ DIMP  │           │ Filesystem│
│Service│           │  (Jobs)   │
└───────┘           └───────────┘
```

### Project Structure

```
aether/
├── cmd/                      # CLI entry points
│   ├── root.go               # Root command
│   ├── pipeline.go           # Pipeline commands
│   └── job.go                # Job management commands
├── internal/
│   ├── models/               # Domain models (immutable)
│   │   ├── job.go            # PipelineJob, JobStatus
│   │   ├── step.go           # PipelineStep, StepStatus
│   │   ├── config.go         # ProjectConfig
│   │   └── validation.go     # Model validation
│   ├── pipeline/             # Pipeline orchestration
│   │   ├── job.go            # Job initialization
│   │   ├── import.go         # Import step dispatcher (torch/local/http)
│   │   └── dimp.go           # DIMP step
│   ├── services/             # Side effects (I/O, HTTP)
│   │   ├── importer.go       # Local file import
│   │   ├── downloader.go     # HTTP download
│   │   ├── dimp_client.go    # DIMP HTTP client
│   │   ├── state.go          # State persistence
│   │   └── config.go         # Configuration loader
│   ├── ui/                   # Progress indicators
│   │   ├── progress.go       # Progress bars
│   │   ├── eta.go            # ETA calculation
│   │   └── throughput.go     # Throughput display
│   └── lib/                  # Pure utilities
│       ├── retry.go          # Retry logic
│       ├── fhir.go           # FHIR parsing
│       └── logging.go        # Logging
├── tests/
│   ├── unit/                 # Unit tests
│   ├── integration/          # Integration tests
│   └── contract/             # HTTP service contracts
├── config/
│   └── aether.example.yaml   # Example configuration
└── jobs/                     # Runtime job data (gitignored)
```

## Configuration

Aether uses YAML configuration files with support for CLI flag overrides:

```yaml
services:
  dimp_url: "http://localhost:8083/fhir"
  csv_conversion_url: "http://localhost:9000/convert/csv"
  parquet_conversion_url: "http://localhost:9000/convert/parquet"

pipeline:
  enabled_steps:
    - torch              # TORCH import via CRTDL or direct TORCH URL
    - local_import       # Import from local directory
    - http_import        # Import from HTTP URL
    - dimp
    # - validation  (placeholder, not implemented)
    # - csv_conversion  (service not available yet)
    # - parquet_conversion  (service not available yet)

retry:
  max_attempts: 5
  initial_backoff_ms: 1000
  max_backoff_ms: 30000

jobs_dir: "./jobs"
```

**Key Configuration Options:**
- `services.*_url`: HTTP service endpoints for processing steps
- `pipeline.enabled_steps`: List of steps to execute (order matters)
- `retry.max_attempts`: Maximum automatic retry attempts for transient errors
- `jobs_dir`: Directory for job state and data storage

## Development

### Advanced Development Tasks

**Cross-platform builds:**
```bash
make build-all           # All platforms
make build-linux         # Linux amd64
make build-mac           # macOS amd64
make build-mac-arm       # macOS arm64 (M1/M2)
```

**Service-specific testing:**
```bash
# Test DIMP integration only
cd .github/test
make dimp-up
make dimp-test
make dimp-down

# Test TORCH integration only
make torch-up
make torch-test
make torch-down

# Run all integration tests with services
make test-with-services
```

**Debugging techniques:**
```bash
# Enable verbose logging
AETHER_LOG_LEVEL=debug ./bin/aether pipeline start test.crtdl

# Run specific test with verbose output
go test -v -run TestImportStep ./internal/pipeline/

# Profile memory usage
go test -memprofile=mem.prof ./internal/pipeline/
go tool pprof -http=:8080 mem.prof

# Check for race conditions
go test -race ./...
```

**Local end-to-end testing:**

See [`.github/test/README.md`](.github/test/README.md) for:
- Complete Docker Compose environment (TORCH + DIMP)
- Sample CRTDL queries and test data
- Step-by-step E2E workflow
- Service configuration examples

### Contributing Workflow

**1. Before starting:**
```bash
# Ensure clean state
git checkout main
git pull origin main
make test  # All tests should pass
```

**2. Create feature branch:**
```bash
git checkout -b feature/your-feature-name
```

**3. TDD cycle:**
```bash
# 1. Write failing test
vim internal/pipeline/your_feature_test.go

# 2. Run test (should fail - RED)
go test -v ./internal/pipeline/ -run TestYourFeature

# 3. Implement minimum code to pass
vim internal/pipeline/your_feature.go

# 4. Run test (should pass - GREEN)
go test -v ./internal/pipeline/ -run TestYourFeature

# 5. Refactor and ensure tests still pass
make test
```

**4. Pre-commit checks:**
```bash
make check      # Format and lint
make coverage   # Ensure coverage doesn't drop
make test       # All tests pass
```

**5. Commit and push:**
```bash
git add .
git commit -m "feat: add your feature description"
git push origin feature/your-feature-name
```

**6. Create pull request:**
- Ensure CI passes
- Request review from maintainers
- Address review comments

**Code review checklist:**
- [ ] All tests pass
- [ ] Code coverage maintained or improved
- [ ] Follows functional programming principles
- [ ] Documentation updated (if needed)
- [ ] No external dependencies added unnecessarily

## Design Principles

Aether follows three core principles defined in the [project constitution](.specify/memory/constitution.md):

### I. Functional Programming
- **Immutability**: Data structures are immutable by default
- **Pure Functions**: Business logic uses pure functions whenever possible
- **Explicit Side Effects**: I/O and mutations isolated to service boundaries
- **Function Composition**: Complex logic built from small, composable functions

### II. Test-Driven Development (TDD)
- Tests written first, implementation follows
- Red-Green-Refactor cycle strictly enforced
- Comprehensive coverage (unit, integration, contract tests)

### III. Keep It Simple, Stupid (KISS)
- Single binary, no microservices
- File-based state, no database
- Standard library-first approach
- External services handle domain complexity

## Documentation

### User Guides
- **[Shell Completions](docs/shell-completions.md)**: Install and configure tab completion for bash, zsh, fish

### Core Pipeline (001-dup-pipeline-we)
- **[Feature Specification](specs/001-dup-pipeline-we/spec.md)**: Complete functional requirements
- **[Implementation Plan](specs/001-dup-pipeline-we/plan.md)**: Technical architecture and decisions
- **[Quickstart Guide](specs/001-dup-pipeline-we/quickstart.md)**: Detailed usage instructions
- **[Data Model](specs/001-dup-pipeline-we/data-model.md)**: Domain entities and schemas
- **[API Contracts](specs/001-dup-pipeline-we/contracts/)**: HTTP service specifications

### TORCH Integration (002-import-via-torch)
- **[TORCH Specification](specs/002-import-via-torch/spec.md)**: TORCH extraction requirements
- **[TORCH Implementation Plan](specs/002-import-via-torch/plan.md)**: TORCH integration design
- **[TORCH Quickstart](specs/002-import-via-torch/quickstart.md)**: CRTDL extraction workflow
- **[TORCH API Contract](specs/002-import-via-torch/contracts/torch-api.md)**: TORCH API specification

## Roadmap

### Completed ✅
- **FHIR Data Import**: Local directory and HTTP URL support with progress tracking
- **Session-Independent Operation**: File-based state persistence for cross-session resumption
- **DIMP Pseudonymization**: Integration with DIMP service for de-identification
- **Hybrid Retry Strategy**: Automatic retries for transient errors, manual for validation failures
- **Progress Indicators**: Real-time progress bars with ETA, throughput, and completion percentages
- **Job Management**: List, status, and continue operations for all jobs
- **Manual Step Execution**: Run individual pipeline steps via `job run --step` command
- **Concurrent Job Safety**: File locks prevent multiple processes from corrupting job state
- **Functional Programming Compliance**: Immutable data structures with pure functions

### Planned 📋
- **Format Conversion**: CSV and Parquet output (requires external services)
- **Enhanced Validation**: FHIR schema validation step (requires external service)

See `specs/*/tasks.md` for detailed implementation tracking.

## FAQ

### General

**Q: Can I run Aether without external services?**
A: Yes! Configure only the `local_import` step in `pipeline.enabled_steps` to process local FHIR files without any external dependencies.

**Q: Where is job data stored?**
A: Jobs are stored in the `jobs_dir` directory (default: `./jobs/`) as JSON state files. Each job gets a UUID subdirectory containing state and processed data.

**Q: Can I run multiple pipelines in parallel?**
A: Yes, but not on the same job. Each job has a file lock to prevent corruption. You can run multiple jobs simultaneously on different data sources.

**Q: How do I resume a failed pipeline?**
A: Use `aether pipeline continue <job-id>`. The pipeline will resume from the last completed step using the file-based state.

**Q: What happens if I lose connection to external services?**
A: Aether uses a hybrid retry strategy: automatic retries for transient errors (network issues), manual intervention for validation failures. Check job status and retry with `pipeline continue`.

### TORCH Integration

**Q: What is CRTDL?**
A: Clinical Resource Transfer Definition Language- a JSON format for defining patient cohorts and data extraction requirements in TORCH.

**Q: Can I reuse TORCH extractions?**
A: Yes! Use the TORCH result URL: `aether pipeline start http://torch-server/fhir/result/abc-123`

**Q: How long do TORCH extractions take?**
A: Depends on cohort size. Aether polls every 5 seconds (configurable) with a default 30-minute timeout. Large cohorts may need a higher timeout in config.

**Q: What if my TORCH credentials are wrong?**
A: Aether fails early with a clear authentication error. Check your `aether.yaml` credentials and TORCH server URL.

### Development

**Q: Can I contribute without knowing Go?**
A: You can help with documentation, testing, and bug reports. For code contributions, basic Go knowledge is needed, but we welcome learning developers!

**Q: What's the difference between unit and integration tests?**
A:
- **Unit tests**: Test pure functions in isolation (no I/O, no external services)
- **Integration tests**: Test with real services (TORCH, DIMP) via Docker Compose
- **Contract tests**: Verify HTTP API specifications match implementation

### Configuration

**Q: Can I use environment variables for secrets?**
A: Not directly in current version. Recommended: Use file permissions to protect `aether.yaml` (chmod 600) or mount secrets in containerized deployments.

**Q: What's the minimal configuration?**
A:
```yaml
pipeline:
  enabled_steps:
    - local_import  # Or torch, http_import based on your data source
jobs_dir: "./jobs"
```

**Q: How do I change the polling interval for TORCH?**
A: Add to `aether.yaml`:
```yaml
services:
  torch:
    polling_interval_seconds: 10  # Default: 5
```

**Q: Can I disable progress bars?**
A: Not currently configurable, but progress output is automatically disabled in non-TTY environments (e.g., CI pipelines, log files).

## Contributing

Contributions welcome! Please follow the [Contributing Workflow](#contributing-workflow) in the Development section.

**Quick checklist:**
- ✅ All tests pass (`make test`)
- ✅ Code coverage maintained (`make coverage`)
- ✅ Follows functional programming principles (immutability, pure functions)
- ✅ No unnecessary external dependencies

See [CLAUDE.md](CLAUDE.md) for detailed coding guidelines and [Development](#development) for the full workflow.

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.

## Acknowledgments

Built for medical research workflows with TORCH FHIR extractions. Designed for session-independent operation to support long-running data processing tasks.

---

**Status**: Active Development | **Branch**: `001-dup-pipeline-we` | **Core Features**: Complete ✅
