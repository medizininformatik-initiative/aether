#!/bin/bash
# Run tests and generate patch coverage report comparing against origin/main

set -e

echo "Running tests with coverage..."
go test -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./tests/...

echo ""
echo "Converting to Cobertura format..."
gocover-cobertura < coverage.out > coverage.xml

echo ""
echo "Generating patch coverage report..."
diff-cover coverage.xml --compare-branch=origin/main
