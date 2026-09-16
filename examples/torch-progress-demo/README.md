# TORCH Progress Demo

This demo shows the extraction progress display without a real TORCH server.
A mock TORCH (`cmd/mocktorch`) simulates a cohort of 1200 patients in batches
and reports batch progress on the Task API.

## Steps

1. Run the demo from the repository root:

   ```sh
   make demo-torch-progress
   ```

   The target builds aether, starts the mock TORCH on `:8086`, runs the
   pipeline, and then stops the mock and deletes the job data.

   The terminal shows a live progress line:

   ```
   TORCH extraction [######..........] 40% — 1/3 batches (100/300 patients), active: CONSENT_FETCH (1/5)
   ```

2. Optional: watch the persisted progress from a second terminal while the
   extraction runs:

   ```sh
   ./bin/aether pipeline status examples/torch-progress-demo/config.yaml <job-id>
   ```

   The target prints the job directory at the start. Set `AETHER_JOBS_DIR` to
   that directory in the second terminal. The `<job-id>` is the name of the
   only directory in it.

## Tuning

To change the shape and speed of the simulated extraction, start the mock alone
with its flags, then start the pipeline in a second terminal:

```sh
go run ./cmd/mocktorch -cohort-size 1200 -batch-size 100 -polls-per-batch 5
./bin/aether pipeline start examples/torch-progress-demo/config.yaml \
  examples/torch-progress-demo/query.json
```

The extraction duration is `batches x polls-per-batch x polling_interval`
(the interval comes from `config.yaml`).
