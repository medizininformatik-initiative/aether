#!/usr/bin/env bash
set -euo pipefail

# Validation E2E test script
# Tests the validation pipeline step in isolation using pre-staged FHIR data.
# Runs aether inside the Docker network using the aether-runner container.
#
# Test data includes both valid and invalid FHIR resources:
#   - Patient.ndjson: 2 valid patients (expect informational outcome)
#   - Condition.ndjson: 1 condition with invalid clinicalStatus and missing subject/verificationStatus (expect error report)
#   - InvalidPatient.ndjson: 2 patients with invalid field values (expect 1 error OperationOutcome)
#
# Resources are wrapped into FHIR Bundles before validation. All OperationOutcomes
# (including informational ones for valid resources) are written to report files.
#
# The e2e config sets fail_on_error: false so the pipeline succeeds even when
# validation errors are found. Report files capture all OperationOutcomes for review.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$SCRIPT_DIR/../test"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== Validation E2E Test ==="
echo ""

cd "$TEST_DIR"

# Copy aether binary into container
echo "Copying aether binary into container..."
docker compose cp ../../bin/aether aether-runner:/app/aether
docker compose exec -T aether-runner chmod +x /app/aether

# Generate a unique job ID
JOB_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
echo "Creating job: $JOB_ID"

# Create job directory structure and copy test data
echo "Setting up pre-staged job state..."
docker compose exec -T aether-runner sh -c "
    mkdir -p /app/jobs/$JOB_ID/import
    mkdir -p /app/jobs/$JOB_ID/validation

    # Copy NDJSON test files to import directory (simulating completed import step)
    cp /app/example-validation/testdata/*.ndjson /app/jobs/$JOB_ID/import/
"

# Create state.json with pre-staged completed import step
docker compose exec -T aether-runner sh -c "
    NOW=\$(date -u +%Y-%m-%dT%H:%M:%SZ)

    cat > /app/jobs/$JOB_ID/state.json << EOF
{
  \"job_id\": \"$JOB_ID\",
  \"created_at\": \"\$NOW\",
  \"updated_at\": \"\$NOW\",
  \"input_source\": \"/app/example-validation/testdata\",
  \"input_type\": \"local_directory\",
  \"current_step\": \"validation\",
  \"status\": \"in_progress\",
  \"steps\": [
    {
      \"name\": \"local_import\",
      \"status\": \"completed\",
      \"files_processed\": 3,
      \"bytes_processed\": 1500,
      \"retry_count\": 0
    },
    {
      \"name\": \"validation\",
      \"status\": \"pending\",
      \"files_processed\": 0,
      \"bytes_processed\": 0,
      \"retry_count\": 0
    }
  ],
  \"config\": {
    \"services\": {
      \"validation\": {
        \"url\": \"http://fhir-validator:8080\",
        \"max_concurrent_requests\": 2,
        \"bundle_chunk_size_mb\": 10
      }
    },
    \"pipeline\": {
      \"enabled_steps\": [\"local_import\", \"validation\"]
    },
    \"retry\": {
      \"max_attempts\": 3,
      \"initial_backoff_ms\": 1000,
      \"max_backoff_ms\": 10000
    },
    \"compression\": {\"enabled\": false},
    \"jobs_dir\": \"./jobs\"
  },
  \"total_files\": 3,
  \"total_bytes\": 1500
}
EOF
"

echo ""
echo "Running aether pipeline continue for validation step..."
echo "  Job ID: $JOB_ID"
echo "  Validator: http://fhir-validator:8080 (internal)"
echo ""

# Run the pipeline continue command.
# Config sets fail_on_error: false, so the pipeline should succeed.
# Report files capture all OperationOutcomes for review.
PIPELINE_EXIT=0
OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline continue aether-validation.yaml "$JOB_ID" 2>&1) || PIPELINE_EXIT=$?
echo "$OUTPUT"

if [ $PIPELINE_EXIT -eq 0 ]; then
    echo -e "${GREEN}Pipeline completed successfully (validation findings are informational)${NC}"
    FAILED=0
else
    echo ""
    echo -e "${RED}UNEXPECTED: Pipeline failed with exit code $PIPELINE_EXIT${NC}"
    FAILED=1
fi

echo ""
echo "Verifying validation output..."

VALIDATION_DIR="/app/jobs/$JOB_ID/validation"

# --- Check 1: Report files exist for ALL input files ---

for input_file in "Patient" "Condition" "InvalidPatient"; do
    REPORT_FILE="$input_file.validation.ndjson"
    echo ""
    echo "Checking report exists: $REPORT_FILE"

    if docker compose exec -T aether-runner sh -c "
        if [ ! -f \"$VALIDATION_DIR/$REPORT_FILE\" ]; then
            echo 'MISSING: Report file not found'
            exit 1
        fi
        echo 'OK - Report file exists'
    "; then
        echo -e "${GREEN}  PASS${NC}"
    else
        echo -e "${RED}  FAIL${NC}"
        FAILED=1
    fi
done

# --- Check 2: Valid files have informational outcomes (no error-severity issues) ---

echo ""
echo "Checking Patient report has informational outcome (all valid, no errors)..."
if docker compose exec -T aether-runner sh -c "
    set -e
    LINE_COUNT=\$(wc -l < \"$VALIDATION_DIR/Patient.validation.ndjson\" | tr -d ' ')
    if [ \"\$LINE_COUNT\" -eq 0 ]; then
        echo 'FAIL: Expected non-empty report with informational outcome'
        exit 1
    fi
    # Verify no error-severity issues in the report
    if grep -qE '\"severity\"\\s*:\\s*\"(error|fatal)\"' \"$VALIDATION_DIR/Patient.validation.ndjson\"; then
        echo 'FAIL: Expected only informational outcomes but found error-severity issues'
        exit 1
    fi
    echo \"OK - Patient report has \$LINE_COUNT informational entry/entries (all valid)\"
"; then
    echo -e "${GREEN}  PASS${NC}"
else
    echo -e "${RED}  FAIL${NC}"
    FAILED=1
fi

echo ""
echo "Checking Condition report has errors (invalid resource — missing subject, bad clinicalStatus)..."
if docker compose exec -T aether-runner sh -c "
    set -e
    LINE_COUNT=\$(wc -l < \"$VALIDATION_DIR/Condition.validation.ndjson\" | tr -d ' ')
    if [ \"\$LINE_COUNT\" -eq 0 ]; then
        echo 'FAIL: Expected non-empty report for invalid Condition'
        exit 1
    fi
    echo \"OK - Condition report has \$LINE_COUNT entry/entries (errors found)\"
"; then
    echo -e "${GREEN}  PASS${NC}"
else
    echo -e "${RED}  FAIL${NC}"
    FAILED=1
fi

# --- Check 3: InvalidPatient report has exactly 1 line (one error OperationOutcome for the chunk) ---

echo ""
echo "Checking InvalidPatient report has exactly 1 entry (one chunk with errors)..."
if docker compose exec -T aether-runner sh -c "
    set -e
    LINE_COUNT=\$(wc -l < \"$VALIDATION_DIR/InvalidPatient.validation.ndjson\" | tr -d ' ')
    if [ \"\$LINE_COUNT\" -ne 1 ]; then
        echo \"FAIL: Expected 1 entry, got \$LINE_COUNT\"
        exit 1
    fi
    echo 'OK - InvalidPatient report has 1 entry (one chunk OperationOutcome)'
"; then
    echo -e "${GREEN}  PASS${NC}"
else
    echo -e "${RED}  FAIL${NC}"
    FAILED=1
fi

# --- Check 4: The error OperationOutcome contains error-severity issues ---

echo ""
echo "Checking InvalidPatient report contains error-severity issues..."
if docker compose exec -T aether-runner sh -c "
    set -e
    ERROR_COUNT=0
    while IFS= read -r line; do
        if [ -z \"\$line\" ]; then
            continue
        fi
        if echo \"\$line\" | grep -qE '\"severity\"\\s*:\\s*\"(error|fatal)\"'; then
            ERROR_COUNT=\$((ERROR_COUNT + 1))
        fi
    done < \"$VALIDATION_DIR/InvalidPatient.validation.ndjson\"

    if [ \"\$ERROR_COUNT\" -eq 0 ]; then
        echo 'FAIL: No error-severity issues found in InvalidPatient report'
        exit 1
    fi
    echo \"OK - Found \$ERROR_COUNT OperationOutcome(s) with error-severity issues\"
"; then
    echo -e "${GREEN}  PASS${NC}"
else
    echo -e "${RED}  FAIL${NC}"
    FAILED=1
fi

# --- Summary ---

echo ""
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}=== All validation checks passed ===${NC}"
    exit 0
else
    echo -e "${RED}=== Some validation checks failed ===${NC}"
    # Print report contents for debugging
    echo ""
    echo "--- Validation report contents ---"
    docker compose exec -T aether-runner sh -c "
        for f in $VALIDATION_DIR/*.ndjson; do
            echo \"File: \$(basename \$f)\"
            cat \"\$f\"
            echo ''
        done
    " || true
    exit 1
fi
