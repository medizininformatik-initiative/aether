# Upgrade Guide

Breaking changes and migration steps between releases. Read the section for the
version you are upgrading to before updating your scripts and existing jobs.

## v0.9 → v0.10

v0.10 has two breaking changes. Both require action; neither alters your FHIR data.

### 1. `aether.yaml` is now a required positional argument

The `--config` flag has been removed, and Aether no longer discovers a config file
implicitly. v0.9 searched `--config` → `./aether.yaml` → `~/.config/aether/aether.yaml`.
v0.10 requires you to pass the config path explicitly, as the **first positional
argument** of every command that needs configuration.

**Before (v0.9):**

```bash
# Relied on the --config flag or implicit ./aether.yaml discovery
aether pipeline start your-crtdl.json
aether pipeline status <job-id>
aether pipeline continue <job-id>
aether job list

# or with the explicit flag
aether --config /path/aether.yaml pipeline start your-crtdl.json
```

**After (v0.10):**

```bash
aether pipeline start    aether.yaml your-crtdl.json
aether pipeline status   aether.yaml <job-id>
aether pipeline continue aether.yaml <job-id>
aether job list          aether.yaml
```

What to update:

- **Scripts, cron jobs, and CI**: insert the config path as the first argument of each
  `aether` subcommand, and remove any `--config` flag — it no longer exists.
- **No implicit discovery**: a bare `./aether.yaml` or `~/.config/aether/aether.yaml` is
  not picked up automatically. Point at the file explicitly.

### 2. DIMP output directory renamed `pseudonymized/` → `dimp/`

The DIMP step now writes to `jobs/<job-id>/dimp/` instead of
`jobs/<job-id>/pseudonymized/`, matching the step name and the rest of the per-step
layout. The file contents are unchanged — only the directory name moved.

What to update:

- **Downstream tooling and scripts** that read pseudonymized output by path must switch
  `jobs/<job-id>/pseudonymized/` → `jobs/<job-id>/dimp/`.
- **In-flight jobs from v0.9**: a job whose DIMP step already ran has its data under
  `pseudonymized/`, but v0.10 looks under `dimp/`. To resume such a job, rename the
  directory first:

  ```bash
  mv jobs/<job-id>/pseudonymized jobs/<job-id>/dimp
  ```

  Jobs started fresh on v0.10 need no action.

### Worth knowing (non-breaking)

- **Per-job logs**: each job now writes its own log file into its job directory, so logs
  live alongside that job's data.
- **TORCH URL handling**: scheme-less, host-prefixed URLs returned by TORCH are handled
  correctly, fixing double-host URLs.
- **Extraction reporting**: status now shows the extraction summary instead of a stale
  cohort-size diagnostic.

See the [v0.10.0 release notes](https://github.com/medizininformatik-initiative/aether/releases/tag/v0.10.0)
for the full changelog.
