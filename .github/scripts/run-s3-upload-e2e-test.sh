#!/usr/bin/env bash
set -euo pipefail

# E2E test script for S3 upload mode
# Tests torch → send pipeline that uploads files to MinIO (S3-compatible)

echo "Copying aether binary into container..."
docker compose cp ../../bin/aether aether-runner:/app/aether
docker compose exec -T aether-runner chmod +x /app/aether

echo ""
echo "Running aether E2E test (s3_upload) inside Docker network..."
echo "  TORCH: http://torch-proxy:80 (internal)"
echo "  MinIO: http://minio:9000 (internal)"
echo "  Bucket: aether-test"
echo ""
echo "Pipeline: torch → send (s3_upload)"

# Run aether inside the Docker network where it can resolve internal hostnames
OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline start torch/queries/example-crtdl.json --config aether-s3-upload.yaml 2>&1) || true
echo "$OUTPUT"

# Extract job ID from output (format: "Created pipeline job: <uuid>")
JOB_ID=$(echo "$OUTPUT" | grep -oP 'Created pipeline job: \K[a-f0-9-]+' || true)

if [ -z "$JOB_ID" ]; then
    echo "Failed to extract job ID from output"
    exit 1
fi

echo ""
echo "Job ID: $JOB_ID"

# Check if pipeline completed successfully
if ! echo "$OUTPUT" | grep -q "Pipeline completed successfully"; then
    echo "Pipeline did not complete successfully in initial run"

    # Try continuing (in case of any intermediate state)
    for i in {1..5}; do
        echo ""
        echo "Checking job status (attempt $i)..."
        STATUS_OUTPUT=$(docker compose exec -T aether-runner /app/aether pipeline status "$JOB_ID" --config aether-s3-upload.yaml 2>&1) || true
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
echo "Verifying S3 upload: checking MinIO bucket for uploaded files..."

# Verify files were uploaded to MinIO
S3_VERIFY=$(docker compose exec -T aether-runner sh -c '
    # Install curl if not present (debian minimal image)
    apt-get update -qq && apt-get install -y -qq curl jq >/dev/null 2>&1 || true

    echo "=== S3 Upload Verification ==="

    # List objects in the bucket using the S3 ListObjectsV2 API
    # MinIO supports the standard S3 API
    RESPONSE=$(curl --silent --show-error \
        "http://minio:9000/aether-test?list-type=2" \
        -H "Host: minio:9000" \
        --user minioadmin:minioadmin \
        2>&1) || {
        echo "FAIL: Could not connect to MinIO"
        exit 1
    }

    # Count objects using the S3 XML response
    # <KeyCount>N</KeyCount> in ListObjectsV2 response
    OBJECT_COUNT=$(echo "$RESPONSE" | grep -oP "<KeyCount>\K[0-9]+" || echo "0")

    echo "Objects in bucket: $OBJECT_COUNT"

    # List individual keys
    echo ""
    echo "Uploaded files:"
    echo "$RESPONSE" | grep -oP "<Key>\K[^<]+" | while read -r key; do
        echo "  s3://aether-test/$key"
    done

    # Verify at least one file was uploaded
    if [ "$OBJECT_COUNT" -lt 1 ]; then
        echo ""
        echo "FAIL: Expected at least 1 file in S3 bucket"
        exit 1
    fi

    echo ""
    echo "PASS: S3 upload verified - found $OBJECT_COUNT files in bucket"
') || {
    echo "$S3_VERIFY"
    echo "S3 upload verification failed"
    exit 1
}
echo "$S3_VERIFY"

# Verify upload manifest was created
echo ""
echo "Verifying upload manifest..."
MANIFEST_CHECK=$(docker compose exec -T aether-runner sh -c "
    MANIFEST_FILE=\"jobs/${JOB_ID}/upload_manifest.json\"
    if [ -f \"\$MANIFEST_FILE\" ]; then
        FILE_COUNT=\$(jq '.files | length' \"\$MANIFEST_FILE\")
        COMPLETED=\$(jq '.completed_at // \"null\"' \"\$MANIFEST_FILE\")
        echo \"Upload manifest found:\"
        echo \"  Files recorded: \$FILE_COUNT\"
        echo \"  Completed: \$COMPLETED\"
        if [ \"\$COMPLETED\" = '\"null\"' ] || [ \"\$COMPLETED\" = 'null' ]; then
            echo \"FAIL: Manifest not marked as completed\"
            exit 1
        fi
        echo \"PASS: Upload manifest is valid and complete\"
    else
        echo \"FAIL: Upload manifest not found at \$MANIFEST_FILE\"
        exit 1
    fi
") || {
    echo "$MANIFEST_CHECK"
    echo "Upload manifest verification failed"
    exit 1
}
echo "$MANIFEST_CHECK"

echo ""
echo "E2E test (s3_upload) completed for job: $JOB_ID"
