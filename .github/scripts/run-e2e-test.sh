#!/usr/bin/env bash
set -euo pipefail

# E2E test script - runs aether inside the Docker network
# This eliminates the need for URL rewriting since aether can resolve internal hostnames

echo "Copying aether binary into container..."
docker compose cp ../../bin/aether aether-runner:/app/aether
docker compose exec -T aether-runner chmod +x /app/aether

echo "Initializing VFPS namespace..."
# Create the patient-identifiers namespace required by the anonymization rules
# Run from inside the aether-runner container to use internal hostname
docker compose exec -T aether-runner sh -c '
    # Install curl if not present (debian minimal image)
    apt-get update -qq && apt-get install -y -qq curl >/dev/null 2>&1 || true

    curl --silent --show-error --request POST \
        --url "http://vfps:8080/v1/namespaces" \
        --header "content-type: application/json" \
        --data "{
          \"name\": \"patient-identifiers\",
          \"pseudonymGenerationMethod\": \"PSEUDONYM_GENERATION_METHOD_UNSPECIFIED\",
          \"pseudonymLength\": 32,
          \"pseudonymPrefix\": \"string\",
          \"pseudonymSuffix\": \"string\",
          \"description\": \"string\"
        }" || echo "VFPS namespace may already exist"
'

echo ""
echo "Running aether E2E test inside Docker network..."
echo "  TORCH: http://torch-proxy:80 (internal)"
echo "  DIMP: http://fhir-pseudonymizer:8080 (internal)"
echo "  Blaze (send target): http://blaze-fhir:8080/fhir (internal)"

# Run aether inside the Docker network where it can resolve internal hostnames
# Capture output to extract job ID
OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline start aether.yaml torch/queries/example-crtdl.json 2>&1) || true
echo "$OUTPUT"

# Extract job ID from output (format: "Created pipeline job: <YYYYMMDD_HHMM_UUID or UUID>")
JOB_ID=$(echo "$OUTPUT" | grep -oP 'Created pipeline job: \K[a-f0-9_-]+' || true)

if [ -z "$JOB_ID" ]; then
    echo "Failed to extract job ID from output"
    exit 1
fi

echo ""
echo "Job ID: $JOB_ID"

# Check if pipeline paused at wait step
if echo "$OUTPUT" | grep -q "Pipeline paused at wait step"; then
    echo ""
    echo "Pipeline paused at wait step, copying files to wait directory..."

    # Copy files from import/ to import_wait/ to satisfy the wait condition
    docker compose exec -T aether-runner sh -c "cp /app/jobs/$JOB_ID/import/* /app/jobs/$JOB_ID/import_wait/ 2>/dev/null || true"

    # Continue pipeline until completion (pipeline continue only runs one step at a time)
    # Loop until job status is 'completed' or an error occurs
    while true; do
        echo ""
        echo "Continuing pipeline..."
        CONTINUE_OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline continue aether.yaml "$JOB_ID" 2>&1) || {
            echo "$CONTINUE_OUTPUT"
            # Check if it's just a pause at another wait step
            if echo "$CONTINUE_OUTPUT" | grep -q "Pipeline paused at wait step\|Pipeline still paused"; then
                echo "Pipeline paused again, copying files..."
                # Find and copy files from the latest *_wait directory
                docker compose exec -T aether-runner sh -c "
                    for dir in /app/jobs/$JOB_ID/*_wait; do
                        if [ -d \"\$dir\" ] && [ -z \"\$(ls -A \$dir 2>/dev/null)\" ]; then
                            src_dir=\$(echo \$dir | sed 's/_wait\$//')
                            cp \$src_dir/* \$dir/ 2>/dev/null || true
                        fi
                    done
                "
                continue
            fi
            echo "Pipeline failed"
            exit 1
        }
        echo "$CONTINUE_OUTPUT"

        # Check if pipeline completed
        if echo "$CONTINUE_OUTPUT" | grep -q "Job completed successfully\|Job already completed"; then
            break
        fi

        # Check if paused at another wait step
        if echo "$CONTINUE_OUTPUT" | grep -q "Pipeline paused at wait step"; then
            echo "Pipeline paused at another wait step, copying files..."
            docker compose exec -T aether-runner sh -c "
                for dir in /app/jobs/$JOB_ID/*_wait; do
                    if [ -d \"\$dir\" ] && [ -z \"\$(ls -A \$dir 2>/dev/null)\" ]; then
                        src_dir=\$(echo \$dir | sed 's/_wait\$//')
                        cp \$src_dir/* \$dir/ 2>/dev/null || true
                    fi
                done
            "
            continue
        fi
    done
fi

echo ""
echo "Verifying CRTDL preprocessing..."

# Check that enriched-crtdl.json was created (enrichments are enabled in e2e config)
if docker compose exec -T aether-runner sh -c "test -f /app/jobs/$JOB_ID/enriched-crtdl.json"; then
    echo "✓ enriched-crtdl.json exists"

    # Verify Patient.identifier:PseudonymisierterIdentifier was added (enrichment applied)
    # The enrichment adds this attribute which is required for DIMP pseudonymization
    if docker compose exec -T aether-runner sh -c "grep -q 'Patient.identifier:PseudonymisierterIdentifier' /app/jobs/$JOB_ID/enriched-crtdl.json"; then
        echo "✓ enriched-crtdl.json has Patient.identifier:PseudonymisierterIdentifier (enrichment applied)"
    else
        echo "✗ ERROR: enriched-crtdl.json does not have Patient.identifier:PseudonymisierterIdentifier"
        exit 1
    fi
else
    echo "✗ ERROR: enriched-crtdl.json was not created (preprocessing may not have run)"
    exit 1
fi

echo ""
echo "Verifying send step: checking Blaze FHIR server for transferred resources..."

# Verify DocumentReference was created on the Blaze FHIR server
SEND_VERIFY=$(docker compose exec -T aether-runner sh -c '
    DOC_REF_RESPONSE=$(curl --silent --show-error "http://blaze-fhir:8080/fhir/DocumentReference?_summary=count")
    DOC_REF_COUNT=$(echo "$DOC_REF_RESPONSE" | grep -o "\"total\":[0-9]*" | grep -o "[0-9]*" || echo "0")
    BINARY_RESPONSE=$(curl --silent --show-error "http://blaze-fhir:8080/fhir/Binary?_summary=count")
    BINARY_COUNT=$(echo "$BINARY_RESPONSE" | grep -o "\"total\":[0-9]*" | grep -o "[0-9]*" || echo "0")

    echo "DocumentReference count: $DOC_REF_COUNT"
    echo "Binary count: $BINARY_COUNT"

    if [ "$DOC_REF_COUNT" -lt 1 ]; then
        echo "FAIL: Expected at least 1 DocumentReference on transfer server"
        exit 1
    fi
    if [ "$BINARY_COUNT" -lt 1 ]; then
        echo "FAIL: Expected at least 1 Binary on transfer server"
        exit 1
    fi
    echo "PASS: Send step verified successfully"
') || {
    echo "$SEND_VERIFY"
    echo "Send step verification failed"
    exit 1
}
echo "$SEND_VERIFY"

echo ""
echo "E2E test completed for job: $JOB_ID"
