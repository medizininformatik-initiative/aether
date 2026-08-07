# aether — Agent Instructions

aether is a Go CLI that runs a data-transfer pipeline for MII Data Use Projects.
It imports FHIR data (from TORCH, a local directory, or an HTTP URL), pseudonymizes
it via the DIMP service, validates it, flattens it to CSV via the fhir-flattener
service, and sends it to a DSF transfer server. Job state lives on the filesystem
under `jobs/<job-id>/`.

## Commands

- Build: `make build` (or `go build -o bin/aether cmd/aether/main.go`)
- All tests: `make test`
- Unit tests: `make test-unit` — integration: `make test-integration` — contract: `make test-contract`
- Single test: `go test -run TestName ./tests/unit/...`
- Coverage: `make coverage` (writes coverage.html)
- Format: `make fmt`; import ordering: `golangci-lint fmt ./...`
- Lint: `make lint` (golangci-lint), `make vet`
- Docs lint: `make lint-docs` (Vale, same gate as CI)
- Run: `aether pipeline start <config.yaml> <input>` / `status <job-id>` / `continue <job-id>`

## Project Structure

```
cmd/           # CLI entry points (Cobra commands)
internal/
  lib/         # Shared utilities (compression, validation, retry, logging)
  models/      # Data structures (job, step, config, transitions)
  pipeline/    # Step execution (import, dimp, validation, wait, flattening, send)
  services/    # External-service clients and state management
config/        # Example configuration (aether.example.yaml)
docs/          # VitePress documentation site
tests/         # unit/, integration/, contract/, compat/
jobs/          # Runtime job data (not committed)
```

Pipeline step names: `torch`, `local_import`, `http_import`, `dimp`, `validation`,
`wait`, `flattening`, `send` (see `internal/models/step.go`).

## Code Style

- Follow standard Go conventions (gofmt, Effective Go).
- Import ordering (enforced by gci): standard library → external → internal
  (`github.com/medizininformatik-initiative/aether`), separated by blank lines.
  Auto-fix with `golangci-lint fmt ./...`.
- Wrap errors with context: `fmt.Errorf("context: %w", err)`.
- Tests use testify (`assert`, `require`).
- Use typed constants for domain values (`StepName`, `StepStatus`, `ErrorType`).
- Validate state transitions with `CanTransitionTo()` methods.
- Comments describe current behavior, not history. Do not reference issue numbers,
  regressions, or "previously" in code or test comments.
- Keep comments minimal. Comment only what the code cannot say itself (the WHY).
  Delete comments that restate the code.
- Write all prose in ASD-STE100 Simplified Technical English: comments, docstrings,
  commit messages, PR titles and descriptions, and issue text.

## Configuration Changes

When you add a configuration option:

1. Add the field to the struct in `internal/models/config.go` with `yaml`, `json`,
   and `mapstructure` tags (e.g. `` `yaml:"field_name" json:"field_name" mapstructure:"field_name"` ``).
2. Set its default in `models.DefaultConfig()`. `LoadConfig` starts from the
   defaults and overlays only keys present in the YAML file.
3. Add a loading test in `tests/unit/config_loading_test.go`.

Every config key is also overridable via environment variables with the `AETHER_`
prefix (e.g. `services.torch.base_url` → `AETHER_SERVICES_TORCH_BASE_URL`).

## Workflow

- Develop test-first: write one failing test, add the minimal code to pass it,
  repeat. Do not write all tests first and then all code.
- Branch naming: `<issue-number>-<short-title>` (e.g. `614-add-agent-instructions`).
- Commits: Conventional Commits (`feat:`, `fix:`, `chore:`, …) with `--signoff`.
- Pull requests: do not include a "Test plan" section in the description.
- Docs: the VitePress build fails on dead links; run `make lint-docs` before
  committing documentation changes.
