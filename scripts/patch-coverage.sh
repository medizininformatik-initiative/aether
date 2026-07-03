#!/bin/bash
# Run tests and generate patch coverage report comparing against origin/main

set -e

echo "Running tests with coverage..."
go test -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./tests/...

echo "Converting to Cobertura format..."
gocover-cobertura < coverage.out > coverage.xml

echo "Generating patch coverage report..."
# Exclude servicestest (hand-written test doubles) to match codecov.yml's ignore:
# it is test-support code, so its coverage is noise and must not gate the patch.
diff-cover coverage.xml --compare-branch=origin/main --exclude "internal/services/servicestest/*" | grep -v '^$'
