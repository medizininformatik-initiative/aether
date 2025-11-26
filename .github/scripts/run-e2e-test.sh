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
docker compose exec -T aether-runner /app/aether pipeline start torch/queries/example-crtdl.json --config aether.yaml
