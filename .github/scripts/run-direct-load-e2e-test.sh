#!/usr/bin/env bash
set -euo pipefail

# E2E test script for direct resource load mode
# Tests torch → send pipeline that uploads FHIR resources directly to a FHIR server

echo "Copying aether binary into container..."
docker compose cp ../../bin/aether aether-runner:/app/aether
docker compose exec -T aether-runner chmod +x /app/aether

echo ""
echo "Running aether E2E test (direct_resource_load) inside Docker network..."
echo "  TORCH: http://torch-proxy:80 (internal)"
echo "  Blaze (send target): http://blaze-fhir:8080/fhir (internal)"
echo ""
echo "Pipeline: torch → send (direct_resource_load)"

# Run aether inside the Docker network where it can resolve internal hostnames
# Capture output to extract job ID
OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline start torch/queries/example-crtdl.json --config aether-direct-load.yaml 2>&1) || true
echo "$OUTPUT"

# Extract job ID from output (format: "Created pipeline job: <YYYYMMDD_HHMM_UUID or UUID>")
JOB_ID=$(echo "$OUTPUT" | grep -oP 'Created pipeline job: \K[a-f0-9_-]+' || true)

if [ -z "$JOB_ID" ]; then
    echo "Failed to extract job ID from output"
    exit 1
fi

echo ""
echo "Job ID: $JOB_ID"

# Wait for completion (this is a simple torch → send pipeline, no wait steps)
# Check if the output indicates success
if ! echo "$OUTPUT" | grep -q "Pipeline completed successfully"; then
    echo "Pipeline did not complete successfully in initial run"

    # Try continuing (in case of any intermediate state)
    for i in {1..5}; do
        echo ""
        echo "Checking job status (attempt $i)..."
        STATUS_OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline status "$JOB_ID" --config aether-direct-load.yaml 2>&1) || true
        echo "$STATUS_OUTPUT"

        if echo "$STATUS_OUTPUT" | grep -q "completed"; then
            echo "Job completed"
            break
        fi

        if echo "$STATUS_OUTPUT" | grep -q "failed"; then
            echo "Job failed"
            exit 1
        fi

        sleep 2
    done
fi

echo ""
echo "Verifying send step: checking Blaze FHIR server for directly uploaded FHIR resources..."

# Verify actual FHIR resources were uploaded (not Binary/DocumentReference like transfer_load mode)
SEND_VERIFY=$(docker compose exec -T aether-runner sh -c '
    # Install curl and jq if not present (debian minimal image)
    apt-get update -qq && apt-get install -y -qq curl jq >/dev/null 2>&1 || true

    # Helper function to get resource count
    get_count() {
        curl --silent --show-error "http://blaze-fhir:8080/fhir/$1?_summary=count" | jq -r ".total // 0"
    }

    # Check for actual FHIR resources that would be extracted by TORCH
    PATIENT_COUNT=$(get_count "Patient")
    CONDITION_COUNT=$(get_count "Condition")
    LIST_COUNT=$(get_count "List")
    BUNDLE_COUNT=$(get_count "Bundle")

    # For direct_resource_load, we should NOT have Binary/DocumentReference (that is transfer_load)
    BINARY_COUNT=$(get_count "Binary")
    DOC_REF_COUNT=$(get_count "DocumentReference")

    echo "=== Direct Resource Load Verification ==="
    echo "Patient count: $PATIENT_COUNT"
    echo "Condition count: $CONDITION_COUNT"
    echo "List count: $LIST_COUNT (TORCH outputs List for patient cohort)"
    echo "Bundle count: $BUNDLE_COUNT"
    echo "Binary count: $BINARY_COUNT (should be 0 for direct load)"
    echo "DocumentReference count: $DOC_REF_COUNT (should be 0 for direct load)"

    # Calculate total actual resources (include List since TORCH outputs it)
    TOTAL_RESOURCES=$((PATIENT_COUNT + CONDITION_COUNT + LIST_COUNT + BUNDLE_COUNT))
    echo "Total FHIR resources uploaded: $TOTAL_RESOURCES"

    # Verify we have at least some actual FHIR resources
    if [ "$TOTAL_RESOURCES" -lt 1 ]; then
        echo ""
        echo "DEBUG: Checking all resources on Blaze..."
        curl --silent --show-error "http://blaze-fhir:8080/fhir/metadata" | jq -r ".rest[0].resource[].type" 2>/dev/null | head -20
        echo ""
        echo "FAIL: Expected at least 1 FHIR resource"
        exit 1
    fi

    # Verify no transfer_load artifacts
    if [ "$BINARY_COUNT" -gt 0 ] || [ "$DOC_REF_COUNT" -gt 0 ]; then
        echo "FAIL: Found Binary/DocumentReference - this looks like transfer_load mode, not direct_resource_load"
        exit 1
    fi

    echo "PASS: Direct resource load verified - found $TOTAL_RESOURCES FHIR resources"
') || {
    echo "$SEND_VERIFY"
    echo "Send step verification failed"
    exit 1
}
echo "$SEND_VERIFY"

echo ""
echo "E2E test (direct_resource_load) completed for job: $JOB_ID"
