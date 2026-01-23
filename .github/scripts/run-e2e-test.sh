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

# Run aether inside the Docker network where it can resolve internal hostnames
# Capture output to extract job ID
OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline start torch/queries/example-crtdl.json --config aether.yaml 2>&1) || true
echo "$OUTPUT"

# Extract job ID from output (format: "Created pipeline job: <uuid>")
JOB_ID=$(echo "$OUTPUT" | grep -oP 'Created pipeline job: \K[a-f0-9-]+' || true)

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
        CONTINUE_OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline continue "$JOB_ID" --config aether.yaml 2>&1) || {
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
echo "E2E test completed for job: $JOB_ID"
