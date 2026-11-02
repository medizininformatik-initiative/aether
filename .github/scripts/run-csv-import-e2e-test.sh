#!/usr/bin/env bash
set -euo pipefail

# CSV import E2E test script
# Tests the csv_import → send (s3_upload) pipeline against SeaweedFS. The input
# has CSV files in the top directory and in a subdirectory, plus a file that is
# not CSV. The test lists the objects of the job in the bucket and compares the
# keys and contents with the input.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$SCRIPT_DIR/../test"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

echo "=== CSV Import E2E Test ==="
echo ""

cd "$TEST_DIR"

echo "Copying aether binary into container..."
docker compose cp ../../bin/aether aether-runner:/app/aether
docker compose exec -T aether-runner chmod +x /app/aether

echo "Installing verification tools in container..."
docker compose exec -T aether-runner sh -c \
    'apt-get update -qq && apt-get install -y -qq curl >/dev/null'

echo "Preparing CSV input directory..."
docker compose exec -T aether-runner sh -c '
    set -e
    rm -rf /tmp/csv-input
    mkdir -p /tmp/csv-input/sub
    cp "/app/example-flattening/expected/MII PR Person Patient (Pseudonymisiert).csv" /tmp/csv-input/
    cp /app/example-flattening/expected/Diagnosis.csv /tmp/csv-input/
    cp /app/example-flattening/expected/Procedure.csv /tmp/csv-input/sub/
    # A data file that is not CSV: csv_import must skip it.
    echo "{\"resourceType\":\"Observation\"}" > /tmp/csv-input/Observation.ndjson
'

echo ""
echo "Running aether pipeline: csv_import → send (s3_upload)"
OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline start \
    aether-csv-import.yaml example-flattening/queries/example-crtdl.json 2>&1) || {
    echo -e "${RED}Pipeline failed:${NC}"
    echo "$OUTPUT"
    exit 1
}
echo "$OUTPUT"

if ! grep -q "Pipeline completed successfully" <<< "$OUTPUT"; then
    echo -e "${RED}FAIL: pipeline did not complete${NC}"
    exit 1
fi

JOB_ID=$(grep -oP 'Created pipeline job: \K[a-f0-9_-]+' <<< "$OUTPUT" || true)
if [ -z "$JOB_ID" ]; then
    echo -e "${RED}FAIL: could not read the job ID from the output${NC}"
    exit 1
fi

echo ""
echo "Verifying the objects of job $JOB_ID in s3://aether-test..."
# s3.json grants anonymous read and list on the test bucket, so plain GET
# requests are sufficient.
docker compose exec -T -e JOB_ID="$JOB_ID" aether-runner bash -c '
    set -euo pipefail
    BUCKET=http://seaweedfs:8333/aether-test

    LIST=$(curl -sf "$BUCKET?list-type=2&prefix=$JOB_ID/")
    ACTUAL=$(grep -oP "<Key>\K[^<]+" <<< "$LIST" | sed "s|^$JOB_ID/||" | sort)
    EXPECTED=$(printf "%s\n" "Diagnosis.csv" "MII PR Person Patient (Pseudonymisiert).csv" "sub/Procedure.csv")

    echo "Uploaded objects:"
    echo "$ACTUAL" | sed "s/^/  /"
    if [ "$ACTUAL" != "$EXPECTED" ]; then
        echo "FAIL: uploaded objects differ from expected objects:"
        echo "$EXPECTED" | sed "s/^/  /"
        exit 1
    fi

    while IFS= read -r f; do
        url="$BUCKET/$JOB_ID/${f// /%20}"
        if ! curl -sf "$url" | cmp -s - "/tmp/csv-input/$f"; then
            echo "FAIL: content of $f differs from the input"
            exit 1
        fi
    done <<< "$EXPECTED"
' || {
    echo -e "${RED}=== CSV import E2E test failed ===${NC}"
    exit 1
}

echo -e "${GREEN}=== CSV import E2E test passed ===${NC}"
