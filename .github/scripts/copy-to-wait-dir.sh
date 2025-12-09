#!/usr/bin/env bash
set -euo pipefail

# Copy files from import directory to wait directory
# Usage: ./copy-to-wait-dir.sh <job-id>
# Run this from .github/test directory

if [ $# -lt 1 ]; then
    echo "Usage: $0 <job-id>"
    echo ""
    echo "This copies all NDJSON files from the import directory to the wait directory."
    echo "You can then modify the files in the wait directory before continuing."
    echo ""
    echo "Run this from .github/test directory"
    exit 1
fi

JOB_ID="$1"

echo "Copying files from import to wait directory inside Docker container..."
echo "  Job ID: $JOB_ID"
echo ""

# Run copy inside the container to avoid permission issues
docker compose exec -T aether-runner sh -c '
    IMPORT_DIR="/app/jobs/'"$JOB_ID"'/import"
    WAIT_DIR="/app/jobs/'"$JOB_ID"'/import_wait"

    if [ ! -d "$IMPORT_DIR" ]; then
        echo "Error: Import directory not found: $IMPORT_DIR"
        exit 1
    fi

    if [ ! -d "$WAIT_DIR" ]; then
        echo "Error: Wait directory not found: $WAIT_DIR"
        echo "Make sure the pipeline has reached the wait step."
        exit 1
    fi

    echo "Contents of import directory:"
    ls -la "$IMPORT_DIR"
    echo ""

    # Copy all NDJSON files (both compressed and uncompressed)
    cp "$IMPORT_DIR"/*.ndjson "$WAIT_DIR/" 2>/dev/null || true
    cp "$IMPORT_DIR"/*.ndjson.zst "$WAIT_DIR/" 2>/dev/null || true

    COUNT=$(ls "$WAIT_DIR"/*.ndjson "$WAIT_DIR"/*.ndjson.zst 2>/dev/null | wc -l)
    if [ "$COUNT" -eq 0 ]; then
        echo "No .ndjson or .ndjson.zst files found in import directory"
        exit 1
    fi

    echo "Files copied:"
    ls -la "$WAIT_DIR"
'

echo ""
echo "Files copied successfully!"
echo "You can now continue the pipeline with: ../scripts/continue-pipeline.sh $JOB_ID"
