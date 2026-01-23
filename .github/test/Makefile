# Makefile for Aether Test Infrastructure
# Located in .github/test/

.PHONY: help services start stop torch-up torch-down torch-logs torch-test dimp-up dimp-down dimp-test flattener-up flattener-down flattener-logs test-with-services download-testdata upload-testdata e2e-test e2e-test-flattening clean-test-data

# Default target
.DEFAULT_GOAL := help

## help: Display this help message
help:
	@echo "Aether Test Infrastructure"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available Services:"
	@echo "  torch             TORCH data extraction service"
	@echo "  dimp              DIMP pseudonymization service"
	@echo "  flattener         fhir-flattener CSV transformation service"
	@echo ""
	@echo "Targets:"
	@echo "  start             Start all services (TORCH + DIMP + Flattener)"
	@echo "  stop              Stop all services"
	@echo "  torch-up          Start TORCH test service"
	@echo "  torch-down        Stop TORCH test service"
	@echo "  torch-logs        Show TORCH service logs"
	@echo "  torch-test        Test TORCH extraction (requires running services)"
	@echo "  dimp-up           Start DIMP test service"
	@echo "  dimp-down         Stop DIMP test service"
	@echo "  dimp-logs         Show DIMP service logs"
	@echo "  dimp-restart      Restart DIMP service"
	@echo "  dimp-test         Run integration tests with DIMP service"
	@echo "  flattener-up      Start fhir-flattener service"
	@echo "  flattener-down    Stop fhir-flattener service"
	@echo "  flattener-logs    Show fhir-flattener service logs"
	@echo "  test-with-services Run all tests with required services"
	@echo "  download-testdata Download test data (only if not present)"
	@echo "  upload-testdata   Upload test data to TORCH (requires services)"
	@echo "  e2e-test          Run end-to-end test (downloads, uploads, runs pipeline)"
	@echo "  e2e-test-flattening Run flattening E2E test with pre-staged example data"
	@echo "  clean-test-data   Clean test data and temporary files"

## services: Alias for help
services: help

## start: Start all services (TORCH + DIMP)
start:
	@echo "Starting all test services..."
	@docker compose up --wait
	@echo "✓ All services started"
	@echo ""
	@echo "Services running:"
	@echo "  TORCH: Check port with 'docker compose port torch 8080'"
	@echo "  DIMP:  http://localhost:8083/fhir"
	@docker compose -f compose.yaml exec -T --user=root torch chown -R 1001:1001 /app/output

## stop: Stop all services
stop:
	@echo "Stopping all test services..."
	@docker compose down
	@echo "✓ All services stopped"

## torch-up: Start TORCH test service
torch-up:
	@echo "Starting TORCH test service..."
	@docker compose up -d torch torch_db
	@echo "✓ TORCH service started"
	@echo ""
	@echo "Get TORCH port:"
	@echo "  docker compose port torch 8080"

## torch-down: Stop TORCH test service
torch-down:
	@echo "Stopping TORCH test service..."
	@docker compose stop torch torch_db
	@echo "✓ TORCH service stopped"

## torch-logs: Show TORCH service logs
torch-logs:
	@echo "Showing TORCH service logs..."
	@docker compose logs -f torch

## torch-test: Test TORCH extraction (requires running services)
torch-test:
	@echo "Testing TORCH extraction..."
	@if ! docker compose ps torch | grep -q "Up"; then \
		echo "Error: TORCH service is not running. Start it with 'make torch-up' or 'make start'"; \
		exit 1; \
	fi
	@echo "Executing CRTDL query..."
	@cd .. && ./scripts/execute-crtdl.sh
	@echo "✓ TORCH extraction test complete"

## dimp-up: Start DIMP test service
dimp-up:
	@echo "Starting DIMP test service..."
	cd dimp && docker compose up -d
	@echo "✓ DIMP service running at http://localhost:8083"
	@echo ""
	@echo "Test the service:"
	@echo "  curl http://localhost:8083/fhir/$$de-identify"

## dimp-down: Stop DIMP test service
dimp-down:
	@echo "Stopping DIMP test service..."
	cd dimp && docker compose down
	@echo "✓ DIMP service stopped"

## dimp-logs: Show DIMP service logs
dimp-logs:
	@echo "Showing DIMP service logs..."
	cd dimp && docker compose logs -f

## dimp-restart: Restart DIMP test service
dimp-restart: dimp-down dimp-up
	@echo "✓ DIMP service restarted"

## flattener-up: Start fhir-flattener service
flattener-up:
	@echo "Starting fhir-flattener service..."
	cd flattener && docker compose up -d
	@echo "✓ fhir-flattener service running at http://localhost:8088"
	@echo ""
	@echo "Test the service:"
	@echo "  curl http://localhost:8088/health"

## flattener-down: Stop fhir-flattener service
flattener-down:
	@echo "Stopping fhir-flattener service..."
	cd flattener && docker compose down
	@echo "✓ fhir-flattener service stopped"

## flattener-logs: Show fhir-flattener service logs
flattener-logs:
	@echo "Showing fhir-flattener service logs..."
	cd flattener && docker compose logs -f

## dimp-test: Run integration tests with DIMP service
dimp-test:
	@echo "Running integration tests with DIMP service..."
	@echo "Starting DIMP service..."
	@cd dimp && docker compose up -d
	@echo "Waiting for service to be ready..."
	@sleep 3
	@echo "Running tests..."
	@cd ../.. && go test -v ./tests/integration/pipeline_dimp_test.go ./tests/integration/pipeline_dimp_resume_test.go
	@echo "Stopping DIMP service..."
	@cd dimp && docker compose down
	@echo "✓ DIMP tests complete"

## test-with-services: Run all tests with required services
test-with-services:
	@echo "Starting all test services..."
	@cd dimp && docker compose up -d
	@echo "Waiting for services to be ready..."
	@sleep 3
	@echo "Running full test suite..."
	@cd ../.. && go test -v ./tests/integration/...
	@echo "Stopping test services..."
	@cd dimp && docker compose down
	@echo "✓ All tests complete"

## download-testdata: Download test data (only if not present)
download-testdata:
	@if [ -d "torch/testdata" ] && [ -n "$$(ls -A torch/testdata 2>/dev/null)" ]; then \
		echo "Test data already exists in torch/testdata, skipping download"; \
		echo "To force re-download, run: rm -rf torch/testdata && make download-testdata"; \
	else \
		echo "Downloading test data..."; \
		../scripts/download-testdata.sh; \
		echo "✓ Test data downloaded to torch/testdata"; \
	fi

## upload-testdata: Upload test data to TORCH (requires services)
upload-testdata: download-testdata
	@echo "Uploading test data to TORCH..."
	@if ! docker compose ps torch-hds | grep -q "Up"; then \
		echo "Error: TORCH services are not running. Start them with 'make start'"; \
		exit 1; \
	fi
	../scripts/upload-testdata.sh
	@echo "✓ Test data uploaded to TORCH"

## e2e-test: Run end-to-end test (downloads, uploads, runs pipeline)
e2e-test: download-testdata
	@echo "Running end-to-end test..."
	@if ! docker compose ps | grep -q "Up"; then \
		echo "Starting all services..."; \
		docker compose up --wait; \
	fi
	@echo "Uploading test data..."
	@cd .. && ./scripts/upload-testdata.sh
	@echo "Running E2E test script..."
	../scripts/run-e2e-test.sh
	@echo "✓ End-to-end test complete"

## e2e-test-flattening: Run flattening E2E test with pre-staged example data
e2e-test-flattening:
	@echo "Running flattening E2E test..."
	@echo "Starting fhir-flattener and aether-runner services..."
	@docker compose up -d --wait aether-runner fhir-flattener
	@echo "Running flattening E2E test script..."
	@../scripts/run-flattening-e2e-test.sh
	@echo "✓ Flattening E2E test complete"

## clean-test-data: Clean test data and temporary files
clean-test-data:
	@echo "Cleaning test data..."
	@rm -rf ../../jobs/*
	@rm -rf torch/testdata
	@echo "✓ Test data cleaned"
