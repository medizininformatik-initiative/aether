#!/usr/bin/env bash
set -euo pipefail

# Flattening E2E test script
# Tests the flattening pipeline step in isolation using pre-staged FHIR data.
# Runs aether inside the Docker network using the aether-runner container.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$SCRIPT_DIR/../test"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== Flattening E2E Test ==="
echo ""

# Go to test directory (services should already be running via Makefile)
cd "$TEST_DIR"

# Copy aether binary into container
echo "Copying aether binary into container..."
docker compose cp ../../bin/aether aether-runner:/app/aether
docker compose exec -T aether-runner chmod +x /app/aether

# Generate a unique job ID
JOB_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
echo "Creating job: $JOB_ID"

# Create job directory structure inside the container
echo "Setting up pre-staged job state..."
docker compose exec -T aether-runner sh -c "
    mkdir -p /app/jobs/$JOB_ID/import
    mkdir -p /app/jobs/$JOB_ID/dimp

    # Copy NDJSON files to import directory (original data with Provenance)
    cp /app/example-flattening/testdata/fhir-data.ndjson /app/jobs/$JOB_ID/import/

    # Copy NDJSON files to dimp directory (simulating completed DIMP step)
    cp /app/example-flattening/testdata/fhir-data.ndjson /app/jobs/$JOB_ID/dimp/
"

# Create state.json with proper variable expansion
docker compose exec -T aether-runner sh -c "
    # Get current timestamp
    NOW=\$(date -u +%Y-%m-%dT%H:%M:%SZ)

    cat > /app/jobs/$JOB_ID/state.json << EOF
{
  \"job_id\": \"$JOB_ID\",
  \"created_at\": \"\$NOW\",
  \"updated_at\": \"\$NOW\",
  \"input_source\": \"/app/example-flattening/queries/example-crtdl.json\",
  \"input_type\": \"crtdl_file\",
  \"current_step\": \"flattening\",
  \"status\": \"in_progress\",
  \"steps\": [
    {
      \"name\": \"local_import\",
      \"status\": \"completed\",
      \"files_processed\": 4,
      \"bytes_processed\": 2900,
      \"retry_count\": 0
    },
    {
      \"name\": \"dimp\",
      \"status\": \"completed\",
      \"files_processed\": 4,
      \"bytes_processed\": 2900,
      \"retry_count\": 0
    },
    {
      \"name\": \"flattening\",
      \"status\": \"pending\",
      \"files_processed\": 0,
      \"bytes_processed\": 0,
      \"retry_count\": 0
    }
  ],
  \"config\": {
    \"services\": {
      \"dimp\": {\"url\": \"http://fhir-pseudonymizer:8080/fhir\"},
      \"flattening\": {
        \"service_url\": \"http://fhir-flattener:8000\",
        \"lookup_path\": \"/app/example-flattening/queries/flatten-lookup.json\",
        \"formats\": [\"csv\"],
        \"timeout\": 300000000000
      }
    },
    \"pipeline\": {
      \"enabled_steps\": [\"local_import\", \"dimp\", \"flattening\"]
    },
    \"retry\": {
      \"max_attempts\": 3,
      \"initial_backoff_ms\": 1000,
      \"max_backoff_ms\": 30000
    },
    \"compression\": {\"enabled\": false},
    \"jobs_dir\": \"./jobs\"
  },
  \"total_files\": 4,
  \"total_bytes\": 2900
}
EOF
"

echo ""
echo "Running aether pipeline continue for flattening step..."
echo "  Job ID: $JOB_ID"
echo "  Flattener: http://fhir-flattener:8000 (internal)"

# Run the pipeline continue command
OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline continue aether-flattening.yaml "$JOB_ID" 2>&1) || {
    echo -e "${RED}Pipeline failed:${NC}"
    echo "$OUTPUT"
    exit 1
}
echo "$OUTPUT"

# Check if pipeline completed
if ! echo "$OUTPUT" | grep -q "Job completed successfully\|completed"; then
    echo -e "${YELLOW}Pipeline may not have completed. Checking job status...${NC}"
    docker compose exec -T aether-runner /app/aether pipeline status aether-flattening.yaml "$JOB_ID"
fi

echo ""
echo "Comparing CSV output with expected files..."

# Run CSV comparison
EXPECTED_DIR="/app/example-flattening/expected"
ACTUAL_DIR="/app/jobs/$JOB_ID/csv"

FAILED=0

# Compare each expected CSV file
for expected_file in "MII PR Person Patient (Pseudonymisiert).csv" "Diagnosis.csv" "Procedure.csv"; do
    echo ""
    echo "Checking: $expected_file"

    # Run comparison inside container using the compare-csv.sh script
    if docker compose exec -T aether-runner sh -c "
        if [ ! -f \"$ACTUAL_DIR/$expected_file\" ]; then
            echo 'MISSING: Output file not found'
            exit 1
        fi

        # Extract headers
        expected_header=\$(head -1 \"$EXPECTED_DIR/$expected_file\")
        actual_header=\$(head -1 \"$ACTUAL_DIR/$expected_file\")

        if [ \"\$expected_header\" != \"\$actual_header\" ]; then
            echo 'HEADER MISMATCH'
            echo \"Expected: \$expected_header\"
            echo \"Actual:   \$actual_header\"
            exit 1
        fi

        # Sort and compare data rows (skip header)
        expected_data=\$(tail -n +2 \"$EXPECTED_DIR/$expected_file\" | sort)
        actual_data=\$(tail -n +2 \"$ACTUAL_DIR/$expected_file\" | sort)

        if [ \"\$expected_data\" != \"\$actual_data\" ]; then
            echo 'DATA MISMATCH'
            echo 'Expected rows:'
            tail -n +2 \"$EXPECTED_DIR/$expected_file\" | sort
            echo '---'
            echo 'Actual rows:'
            tail -n +2 \"$ACTUAL_DIR/$expected_file\" | sort
            exit 1
        fi

        echo 'OK'
    "; then
        echo -e "${GREEN}  PASS${NC}"
    else
        echo -e "${RED}  FAIL${NC}"
        FAILED=1
    fi
done

echo ""
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}=== All CSV comparisons passed ===${NC}"
    exit 0
else
    echo -e "${RED}=== Some CSV comparisons failed ===${NC}"
    exit 1
fi
