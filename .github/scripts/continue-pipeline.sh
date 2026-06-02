#!/usr/bin/env bash
set -euo pipefail

# Continue a paused pipeline - runs aether inside the Docker network
# Usage: ./continue-pipeline.sh <job-id>

if [ $# -lt 1 ]; then
    echo "Usage: $0 <job-id>"
    echo ""
    echo "Example: $0 a8653ed0-e7d0-4bf2-8e04-1b48b610b8a2"
    echo ""
    echo "To find job IDs, check the jobs directory or run:"
    echo "  docker compose exec -T aether-runner ls /app/jobs"
    exit 1
fi

JOB_ID="$1"

echo "Continuing pipeline for job: $JOB_ID"
echo ""

# Check if binary exists in container
if ! docker compose exec -T aether-runner test -f /app/aether; then
    echo "Copying aether binary into container..."
    docker compose cp ../../bin/aether aether-runner:/app/aether
    docker compose exec -T aether-runner chmod +x /app/aether
fi

echo "Running aether pipeline continue inside Docker network..."
echo "  TORCH: http://torch-proxy:80 (internal)"
echo "  DIMP: http://fhir-pseudonymizer:8080 (internal)"
echo ""

# Run aether continue inside the Docker network
docker compose exec -T aether-runner /app/aether pipeline continue aether.yaml "$JOB_ID"
