# Makefile for Aether DUP Pipeline CLI
#
# For test service management (DIMP, etc.), see .github/test/Makefile

# Project metadata
PROJECT_NAME := aether
BINARY_NAME := aether
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo "dev")
BUILD_DIR := bin
MAIN_PATH := cmd/aether/main.go

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT := $(GOCMD) fmt

# Build flags
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

# Platforms
PLATFORMS := linux darwin
ARCHITECTURES := amd64 arm64

.PHONY: all build build-all build-linux build-mac build-mac-arm build-windows build-windows-arm clean test test-unit test-integration test-contract coverage fmt vet vuln install help release

# Default target
all: clean fmt vet test build

## help: Display this help message
help:
	@echo "Aether - Data Use Process Pipeline CLI"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*##"; printf ""} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
	@echo ""
	@echo "For test service management (DIMP, etc.):"
	@echo "  cd .github/test && make help"

## build: Build binary for current platform
build:
	@echo "Building $(BINARY_NAME) for current platform..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

## build-linux: Build binary for Linux (amd64)
build-linux:
	@echo "Building $(BINARY_NAME) for Linux amd64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64"

## build-mac: Build binary for macOS (amd64)
build-mac:
	@echo "Building $(BINARY_NAME) for macOS amd64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64"

## build-mac-arm: Build binary for macOS (arm64/M1)
build-mac-arm:
	@echo "Building $(BINARY_NAME) for macOS arm64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64"

## build-windows: Build binary for Windows (amd64)
build-windows:
	@echo "Building $(BINARY_NAME) for Windows amd64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe"

## build-windows-arm: Build binary for Windows (arm64)
build-windows-arm:
	@echo "Building $(BINARY_NAME) for Windows arm64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.exe $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.exe"

## build-all: Build binaries for all platforms
build-all: build-linux build-mac build-mac-arm build-windows build-windows-arm
	@echo "All platform builds complete"

## clean: Remove build artifacts and test data
clean:
	@echo "Cleaning build artifacts..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -rf jobs/*
	rm -f coverage.out
	@echo "Clean complete"

## test: Run all tests
test:
	@echo "Running all tests..."
	$(GOTEST) -v ./...

## test-unit: Run unit tests only
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v ./tests/unit/...

## test-unit-coverage: Run unit tests with coverage
test-unit-coverage:
	@echo "Running unit tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage-unit.out -covermode=atomic -coverpkg=./... ./tests/unit/...

## test-integration: Run integration tests only
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v ./tests/integration/...

## test-integration-coverage: Run integration tests with coverage
test-integration-coverage:
	@echo "Running integration tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage-integration.out -covermode=atomic -coverpkg=./... ./tests/integration/...

## test-contract: Run contract tests only
test-contract:
	@echo "Running contract tests..."
	$(GOTEST) -v ./tests/contract/...

## coverage: Run tests with coverage report
coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -coverpkg=./internal/... -coverprofile=coverage.out ./tests/...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## coverage-merge: Merge unit and integration coverage files
coverage-merge:
	@echo "Merging coverage files..."
	@echo "mode: atomic" > coverage.out
	@tail -n +2 coverage-unit.out >> coverage.out 2>/dev/null || true
	@tail -n +2 coverage-integration.out >> coverage.out 2>/dev/null || true
	@echo "Coverage files merged into coverage.out"

## fmt: Format Go source code
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOCMD) vet ./...

## vuln: Run govulncheck for known vulnerabilities
vuln:
	@echo "Running govulncheck..."
	$(GOCMD) tool govulncheck ./...

## lint: Run golangci-lint (requires golangci-lint installed)
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

## install: Install binary to /usr/local/bin (requires sudo)
install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "Install complete. Run '$(BINARY_NAME) --help' to get started."

## install-local: Install binary to ~/.local/bin (no sudo required)
install-local: build
	@echo "Installing $(BINARY_NAME) to ~/.local/bin..."
	@mkdir -p ~/.local/bin
	cp $(BUILD_DIR)/$(BINARY_NAME) ~/.local/bin/
	@echo "Install complete. Ensure ~/.local/bin is in your PATH."
	@echo "Run '$(BINARY_NAME) --help' to get started."

## install-completions: Install shell completions (bash, zsh, fish)
install-completions: build
	@echo "Installing shell completions..."
	./scripts/install-completions.sh

## deps: Download and tidy dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOGET) -v ./...
	$(GOMOD) tidy
	@echo "Dependencies updated"

## verify: Verify dependencies
verify:
	@echo "Verifying dependencies..."
	$(GOMOD) verify

## run: Build and run with example command
run: build
	@echo "Running $(BINARY_NAME) with test data..."
	$(BUILD_DIR)/$(BINARY_NAME) pipeline start --input ./test-data/

## release: Build release binaries for all platforms
release: clean build-all
	@echo "Creating release packages..."
	@mkdir -p $(BUILD_DIR)/release
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz $(BINARY_NAME)-linux-amd64
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz $(BINARY_NAME)-darwin-amd64
	cd $(BUILD_DIR) && tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz $(BINARY_NAME)-darwin-arm64
	cd $(BUILD_DIR) && zip -q release/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(BINARY_NAME)-windows-amd64.exe
	cd $(BUILD_DIR) && zip -q release/$(BINARY_NAME)-$(VERSION)-windows-arm64.zip $(BINARY_NAME)-windows-arm64.exe
	@echo "Release packages created in $(BUILD_DIR)/release/"

## check: Run all checks (fmt, vet, test)
check: fmt vet test
	@echo "All checks passed!"

## dev: Quick development build and test
dev: fmt build test-unit
	@echo "Development build complete!"
