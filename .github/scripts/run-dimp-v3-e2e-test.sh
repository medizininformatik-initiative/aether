#!/usr/bin/env bash
set -euo pipefail

# DIMP experimental v3 E2E test script
# Tests the experimental v3alpha1 endpoint of the FHIR-Pseudonymizer against the
# real service, with pre-staged FHIR data. Runs aether inside the Docker network
# using the aether-runner container.
#
# The test runs the same input twice:
#   - job V3:      dimp config with experimental_v3.anonymization_config set.
#                  Aether sends example-dimp-v3/anonymization.yaml with each
#                  request to /v3alpha1/fhir/$de-identify.
#   - job DEFAULT: dimp config without experimental_v3. Aether uses
#                  /fhir/$de-identify and the service applies the rules from
#                  dimp/anonymization.yaml, the file it read at start.
#
# The two anonymization files hold opposite rules for gender, birthDate, name
# and id. So the two outputs must differ. This proves that the service applies
# the rules from the request, and not its own file.
#
# `pipeline continue` runs the steps with the config that state.json holds. Thus
# each job embeds its own dimp config, and aether-dimp-v3.yaml gives only the
# jobs directory.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$SCRIPT_DIR/../test"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

FAILED=0

echo "=== DIMP experimental v3 E2E Test ==="
echo ""

cd "$TEST_DIR"

# Copy aether binary into container
echo "Copying aether binary into container..."
docker compose cp ../../bin/aether aether-runner:/app/aether
docker compose exec -T aether-runner chmod +x /app/aether

V3_JOB_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
DEFAULT_JOB_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')

# stage_job <job-id> <experimental-v3-json>
# Creates the job directory, copies the test data into import/ and writes a
# state.json with a completed local_import step and a pending dimp step.
stage_job() {
    local job_id="$1"
    local experimental_v3="$2"

    docker compose exec -T aether-runner sh -c "
        mkdir -p /app/jobs/$job_id/import
        mkdir -p /app/jobs/$job_id/dimp
        cp /app/example-dimp-v3/testdata/*.ndjson /app/jobs/$job_id/import/
    "

    docker compose exec -T aether-runner sh -c "
        NOW=\$(date -u +%Y-%m-%dT%H:%M:%SZ)

        cat > /app/jobs/$job_id/state.json << EOF
{
  \"job_id\": \"$job_id\",
  \"created_at\": \"\$NOW\",
  \"updated_at\": \"\$NOW\",
  \"input_source\": \"/app/example-dimp-v3/testdata\",
  \"input_type\": \"local_directory\",
  \"current_step\": \"dimp\",
  \"status\": \"in_progress\",
  \"steps\": [
    {
      \"name\": \"local_import\",
      \"status\": \"completed\",
      \"files_processed\": 1,
      \"bytes_processed\": 300,
      \"retry_count\": 0
    },
    {
      \"name\": \"dimp\",
      \"status\": \"pending\",
      \"files_processed\": 0,
      \"bytes_processed\": 0,
      \"retry_count\": 0
    }
  ],
  \"config\": {
    \"services\": {
      \"dimp\": {
        \"url\": \"http://fhir-pseudonymizer:8080\",
        \"bundle_split_threshold_mb\": 10$experimental_v3
      }
    },
    \"pipeline\": {
      \"enabled_steps\": [\"local_import\", \"dimp\"]
    },
    \"retry\": {
      \"max_attempts\": 3,
      \"initial_backoff_ms\": 1000,
      \"max_backoff_ms\": 10000
    },
    \"compression\": {\"enabled\": false},
    \"jobs_dir\": \"./jobs\"
  },
  \"total_files\": 1,
  \"total_bytes\": 300
}
EOF
    "
}

# run_job <job-id> <label>
run_job() {
    local job_id="$1"
    local label="$2"
    local exit_code=0
    local output

    echo ""
    echo "Running the dimp step for the $label job..."
    echo "  Job ID: $job_id"
    output=$(docker compose exec -T aether-runner /app/aether pipeline continue aether-dimp-v3.yaml "$job_id" 2>&1) || exit_code=$?
    echo "$output"

    if [ $exit_code -eq 0 ]; then
        echo -e "${GREEN}  Pipeline completed successfully ($label)${NC}"
    else
        echo -e "${RED}  FAIL: pipeline failed with exit code $exit_code ($label)${NC}"
        FAILED=1
    fi
}

# check <description> <shell-command>
check() {
    local description="$1"
    local command="$2"

    echo ""
    echo "Checking: $description"
    if docker compose exec -T aether-runner sh -c "$command"; then
        echo -e "${GREEN}  PASS${NC}"
    else
        echo -e "${RED}  FAIL${NC}"
        FAILED=1
    fi
}

echo "Setting up pre-staged job state..."
stage_job "$V3_JOB_ID" ',
        "experimental_v3": {
          "anonymization_config": "/app/example-dimp-v3/anonymization.yaml"
        }'
stage_job "$DEFAULT_JOB_ID" ""

run_job "$V3_JOB_ID" "v3"
run_job "$DEFAULT_JOB_ID" "default"

V3_OUT="/app/jobs/$V3_JOB_ID/dimp/dimped_Patient.ndjson"
DEFAULT_OUT="/app/jobs/$DEFAULT_JOB_ID/dimp/dimped_Patient.ndjson"

echo ""
echo "Verifying DIMP output..."

# --- Check 1: both runs wrote one resource for each input resource ---

check "the v3 run wrote 2 resources" "
    set -e
    test -f \"$V3_OUT\" || { echo 'MISSING: $V3_OUT'; exit 1; }
    COUNT=\$(grep -c . \"$V3_OUT\")
    test \"\$COUNT\" -eq 2 || { echo \"FAIL: expected 2 resources, got \$COUNT\"; exit 1; }
    echo 'OK - 2 resources'
"

check "the default run wrote 2 resources" "
    set -e
    test -f \"$DEFAULT_OUT\" || { echo 'MISSING: $DEFAULT_OUT'; exit 1; }
    COUNT=\$(grep -c . \"$DEFAULT_OUT\")
    test \"\$COUNT\" -eq 2 || { echo \"FAIL: expected 2 resources, got \$COUNT\"; exit 1; }
    echo 'OK - 2 resources'
"

# --- Check 2: the v3 output obeys the rules that aether sent ---

check "the v3 output has no gender (redact rule from the request)" "
    if grep -qF '\"gender\"' \"$V3_OUT\"; then
        echo 'FAIL: gender is still present'
        exit 1
    fi
    echo 'OK - gender is redacted'
"

check "the v3 output keeps the exact birthDate (keep rule from the request)" "
    set -e
    grep -qF '\"birthDate\":\"1980-01-01\"' \"$V3_OUT\" || { echo 'FAIL: 1980-01-01 not found'; exit 1; }
    grep -qF '\"birthDate\":\"1975-11-30\"' \"$V3_OUT\" || { echo 'FAIL: 1975-11-30 not found'; exit 1; }
    echo 'OK - both dates are exact'
"

check "the v3 output keeps the name and the id (keep rules from the request)" "
    set -e
    grep -qF '\"family\":\"Doe\"' \"$V3_OUT\" || { echo 'FAIL: family name not found'; exit 1; }
    grep -qF '\"id\":\"v3-patient-1\"' \"$V3_OUT\" || { echo 'FAIL: id not found'; exit 1; }
    echo 'OK - name and id are unchanged'
"

# --- Check 3: the default output obeys the rules that the service read at start ---

check "the default output keeps the gender (no rule in the file of the service)" "
    set -e
    grep -qF '\"gender\":\"male\"' \"$DEFAULT_OUT\" || { echo 'FAIL: gender not found'; exit 1; }
    echo 'OK - gender is present'
"

check "the default output generalizes the birthDate to the year" "
    set -e
    grep -qF '\"birthDate\":\"1980\"' \"$DEFAULT_OUT\" || { echo 'FAIL: generalized date not found'; exit 1; }
    echo 'OK - birthDate is the year only'
"

check "the default output redacts the name" "
    if grep -qF '\"family\"' \"$DEFAULT_OUT\"; then
        echo 'FAIL: family name is still present'
        exit 1
    fi
    echo 'OK - name is redacted'
"

# --- Check 4: the two outputs differ, so the request config wins ---

check "the two outputs differ" "
    if cmp -s \"$V3_OUT\" \"$DEFAULT_OUT\"; then
        echo 'FAIL: both endpoints gave the same output'
        exit 1
    fi
    echo 'OK - the request config gives a different result'
"

# --- Summary ---

echo ""
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}=== All DIMP experimental v3 checks passed ===${NC}"
    exit 0
else
    echo -e "${RED}=== Some DIMP experimental v3 checks failed ===${NC}"
    echo ""
    echo "--- DIMP output contents ---"
    docker compose exec -T aether-runner sh -c "
        echo 'v3 output:'
        cat \"$V3_OUT\" 2>/dev/null || echo '(missing)'
        echo ''
        echo 'default output:'
        cat \"$DEFAULT_OUT\" 2>/dev/null || echo '(missing)'
    " || true
    exit 1
fi
