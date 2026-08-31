# TORCH Progress Demo

This demo shows the extraction progress display without a real TORCH server.
A mock TORCH (`cmd/mocktorch`) simulates a cohort of 1200 patients in batches
and reports batch progress on the Task API.

## Steps

1. Build aether:

   ```sh
   make build
   ```

2. Start the mock TORCH in one terminal:

   ```sh
   make demo-torch-progress
   ```

3. Start the pipeline in a second terminal:

   ```sh
   cd examples/torch-progress-demo
   ../../bin/aether pipeline start config.yaml query.json
   ```

   The terminal shows a live progress line:

   ```
   TORCH extraction [######..........] 40% — 1/3 batches (100/300 patients), active: CONSENT_FETCH (1/5)
   ```

4. Optional: watch the persisted progress from a third terminal while the
   extraction runs:

   ```sh
   cd examples/torch-progress-demo
   ../../bin/aether pipeline status config.yaml <job-id>
   ```

   The `<job-id>` is in the output of `pipeline start`.

## Tuning

The mock accepts flags to change the shape and speed of the simulated
extraction:

```sh
go run ./cmd/mocktorch -cohort-size 1200 -batch-size 100 -polls-per-batch 5
```

The extraction duration is `batches x polls-per-batch x polling_interval`
(the interval comes from `config.yaml`).
