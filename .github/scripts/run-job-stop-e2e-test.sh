#!/usr/bin/env bash
set -euo pipefail

# E2E test for 'aether job stop' against a real TORCH server.
# Run it from .github/test after run-e2e-test.sh: it uses the aether binary
# and the curl that this script puts into the aether-runner container.
#
# 1. Start a pipeline in the background and wait for its TORCH job handle.
# 2. Stop the job from a second process with 'aether job stop'.
# 3. Make sure that TORCH cancelled the extraction (status URL gives 410).
# 4. Continue the job and make sure that it submits a new TORCH extraction.

RUNNER=(docker compose exec -T aether-runner)
LOG_FILE="$(mktemp -t aether-job-stop-XXXXXX.log)"

fail() {
    echo "❌ $*" >&2
    echo "--- pipeline start output ---" >&2
    cat "$LOG_FILE" >&2 || true
    exit 1
}

state() {
    "${RUNNER[@]}" cat "/app/jobs/$JOB_ID/state.json"
}

status_code() {
    "${RUNNER[@]}" curl -s -o /dev/null -w '%{http_code}' \
        -H "Accept: application/fhir+json" "$1"
}

"${RUNNER[@]}" sh -c 'command -v curl >/dev/null || (apt-get update -qq && apt-get install -y -qq curl >/dev/null)'

# The paused FHIR server holds the TORCH extraction in progress, so the stop
# always comes before the extraction ends.
docker compose pause torch-hds
trap 'docker compose unpause torch-hds >/dev/null 2>&1 || true' EXIT

echo "➡️ Starting pipeline in the background..."
"${RUNNER[@]}" /app/aether pipeline start aether.yaml torch/queries/example-crtdl.json >"$LOG_FILE" 2>&1 &
PIPELINE_PID=$!

JOB_ID=""
for _ in $(seq 1 60); do
    JOB_ID=$(grep -oP 'Created pipeline job: \K[a-f0-9_-]+' "$LOG_FILE" || true)
    [[ -n "$JOB_ID" ]] && break
    sleep 0.5
done
[[ -n "$JOB_ID" ]] || fail "No job ID in the pipeline output after 30s"
echo "✅ Job ID: $JOB_ID"

echo "⏳ Waiting for the TORCH job handle..."
OLD_TORCH_JOB=""
for _ in $(seq 1 120); do
    OLD_TORCH_JOB=$(state | jq -r '.torch_job_id // empty' || true)
    [[ -n "$OLD_TORCH_JOB" ]] && break
    kill -0 "$PIPELINE_PID" 2>/dev/null || fail "Pipeline ended before it submitted to TORCH"
    sleep 0.5
done
[[ -n "$OLD_TORCH_JOB" ]] || fail "No TORCH job handle after 60s"
OLD_STATUS_URL=$(state | jq -r '.torch_extraction_url')
echo "✅ TORCH job: $OLD_TORCH_JOB ($OLD_STATUS_URL)"

code=$(status_code "$OLD_STATUS_URL")
[[ "$code" == "202" ]] || fail "TORCH job is not in progress before the stop (HTTP $code)"

echo "🛑 Stopping the job..."
"${RUNNER[@]}" /app/aether job stop aether.yaml "$JOB_ID" || fail "aether job stop failed"

set +e
wait "$PIPELINE_PID"
PIPELINE_EXIT=$?
set -e
[[ "$PIPELINE_EXIT" == "143" ]] || fail "pipeline start ended with exit code $PIPELINE_EXIT, expected 143"
echo "✅ pipeline start ended with exit code 143"

JOB_STATUS=$(state | jq -r '.status')
[[ "$JOB_STATUS" == "stopped" ]] || fail "Job status is '$JOB_STATUS', expected 'stopped'"
echo "✅ Job status: stopped"

code=$(status_code "$OLD_STATUS_URL")
[[ "$code" == "410" ]] || fail "TORCH status of the stopped job is HTTP $code, expected 410 (cancelled)"
echo "✅ TORCH cancelled job $OLD_TORCH_JOB (HTTP 410)"

docker compose unpause torch-hds

echo "➡️ Continuing the stopped job..."
CONTINUE_OUTPUT=$("${RUNNER[@]}" /app/aether pipeline continue aether.yaml "$JOB_ID" 2>&1) || true
echo "$CONTINUE_OUTPUT"

echo "$CONTINUE_OUTPUT" | grep -q "TORCH job handle is gone" \
    || fail "pipeline continue did not detect the cancelled TORCH job"

TORCH_STEP=$(state | jq -r '.steps[] | select(.name == "torch") | .status')
[[ "$TORCH_STEP" == "completed" ]] || fail "torch step is '$TORCH_STEP' after continue, expected 'completed'"

NEW_TORCH_JOB=$(state | jq -r '.torch_job_id')
[[ -n "$NEW_TORCH_JOB" && "$NEW_TORCH_JOB" != "$OLD_TORCH_JOB" ]] \
    || fail "No new TORCH extraction after continue (TORCH job: '$NEW_TORCH_JOB')"
echo "✅ New TORCH extraction $NEW_TORCH_JOB completed"

rm -f "$LOG_FILE"
echo "🎉 Job stop E2E test passed"
