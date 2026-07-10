# Testing

Testing guidelines and strategies for Aether development.

Aether follows Test-Driven Development (TDD) principles with comprehensive unit, integration, and contract testing.

## Testing Philosophy

- **Tests First**: Write tests before implementation (Red → Green → Refactor)
- **Comprehensive Coverage**: Unit, integration, and contract tests work together
- **Isolated Tests**: Unit tests run without external services
- **Integration Tests**: Real services (TORCH, DIMP) via Docker Compose
- **Contract Tests**: Verify HTTP API specifications match implementation

## Testing Framework

Aether uses:
- **Testing library**: Go's standard `testing` package
- **Assertions**: `testify/assert` for fluent assertions
- **Mocking**: Interface-based mocking for external services
- **Integration tests**: Docker Compose for service dependencies

## Test Organization

```
tests/
├── unit/                 # Unit tests (pure functions)
│   ├── import_test.go
│   ├── pipeline_test.go
│   └── dimp_test.go
├── integration/          # Integration tests (with services)
│   ├── torch_integration_test.go
│   └── dimp_integration_test.go
└── contract/             # HTTP service contracts
    ├── torch_api_contract_test.go
    └── dimp_api_contract_test.go
```

## Unit Testing

Unit tests validate pure functions in isolation without external dependencies.

### Running Unit Tests

```bash
# Run all unit tests
make test-unit

# Run specific test suite
go test -v ./tests/unit/...

# Run specific test
go test -v ./tests/unit/ -run TestImportStep
```

### Unit Test Example

```go
func TestImportStep(t *testing.T) {
    // Arrange
    config := &models.Config{...}
    job := &models.PipelineJob{...}

    // Act
    result, err := ImportStep(context.Background(), job, config)

    // Assert
    assert.NoError(t, err)
    assert.NotNil(t, result)
    assert.Equal(t, 100, result.EntryCount)
}
```

### Unit Test Best Practices

- **Name tests clearly**: `TestFunctionName` or `TestFunctionName_Scenario`
- **Use table-driven tests** for multiple scenarios:

```go
testCases := []struct {
    name    string
    input   string
    want    int
    wantErr bool
}{
    {"valid", "data.ndjson", 100, false},
    {"invalid", "invalid.txt", 0, true},
}

for _, tc := range testCases {
    t.Run(tc.name, func(t *testing.T) {
        got, err := Process(tc.input)
        assert.Equal(t, tc.want, got)
        assert.Equal(t, tc.wantErr, err != nil)
    })
}
```

- **Mock external dependencies**:

```go
// Define interface-based mock
type mockTORCHClient struct{}

func (m *mockTORCHClient) Extract(ctx context.Context, crtdlPath string) (io.Reader, error) {
    return strings.NewReader("sample data"), nil
}

// Use mock in test
func TestExtractWithMock(t *testing.T) {
    client := &mockTORCHClient{}
    // ... test with mock
}
```

## Integration Testing

Integration tests validate the entire pipeline with real services.

### Prerequisites

- Docker & Docker Compose installed
- Services configured in `.github/test/docker-compose.yml`

### Running Integration Tests

```bash
# Start test environment
cd .github/test
make start

# In another terminal, run tests
cd ../..
make test-integration

# Stop services
cd .github/test
make stop
```

### Service-Specific Integration Tests

```bash
# Test TORCH integration only
cd .github/test
make torch-up
make torch-test
make torch-down

# Test DIMP integration only
cd .github/test
make dimp-up
make dimp-test
make dimp-down
```

### Integration Test Example

```go
func TestTORCHIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Create real client (connects to Docker service)
    client := services.NewTORCHClient("http://localhost:8080")

    // Test with real CRTDL query
    result, err := client.Extract(context.Background(), "queries/test.json")
    assert.NoError(t, err)
    assert.NotNil(t, result)
}
```

## Contract Testing

Contract tests verify HTTP API specifications between services.

### Running Contract Tests

```bash
# Start services first
cd .github/test && make start

# Run contract tests
cd ../..
make test-contract

# Stop services
cd .github/test && make stop
```

### Example Contract Test

```go
func TestTORCHAPIContract(t *testing.T) {
    // Verify TORCH API returns expected response format
    resp, err := http.Get("http://localhost:8080/fhir/result/test")
    assert.NoError(t, err)
    assert.Equal(t, http.StatusOK, resp.StatusCode)

    // Verify response contains expected fields
    var data map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&data)
    assert.Contains(t, data, "status")
    assert.Contains(t, data, "results")
}
```

## Running All Tests

### Quick Local Tests (Unit Only)

```bash
make test      # or: go test ./...
```

### Full Test Suite (Unit + Integration + Contract)

```bash
make test-with-services
```

### Check Coverage

```bash
make coverage
```

Generates coverage report in `coverage/` directory.

## Test Organization & TDD Workflow

### TDD Cycle

1. **Write failing test** (RED):
   ```bash
   vim tests/unit/feature_test.go
   go test -v ./tests/unit/ -run TestNewFeature
   ```

2. **Implement minimum code** (GREEN):
   ```bash
   vim internal/pipeline/feature.go
   go test -v ./tests/unit/ -run TestNewFeature
   ```

3. **Refactor** (REFACTOR):
   ```bash
   # Improve code quality while keeping tests green
   go test ./...
   ```

### Example: Adding a New Step

```bash
# 1. Write tests first
vim tests/unit/validation_test.go
# Add TestValidationStep, TestValidationStep_InvalidData, etc.

# 2. Run tests (should fail - RED)
go test -v ./tests/unit/ -run TestValidation

# 3. Implement the step
vim internal/pipeline/validation.go
# Implement ValidateStep function

# 4. Run tests (should pass - GREEN)
go test -v ./tests/unit/ -run TestValidation

# 5. Refactor and ensure all tests pass
make test
```

## Debugging Tests

### Verbose Test Output

```bash
go test -v ./...
```

### Run Specific Test with Debugging

```bash
go test -v ./tests/unit/ -run TestImportStep
```

### Profile Test Performance

```bash
go test -cpuprofile=cpu.prof -memprofile=mem.prof ./...
go tool pprof cpu.prof
```

### Check for Race Conditions

```bash
go test -race ./...
```

## Troubleshooting Test Failures

### "connection refused" Errors

Ensure test services are running:
```bash
cd .github/test
make start
```

### Import Tests Fail

Check that jobs directory exists and has write permissions:
```bash
mkdir -p ./jobs
chmod 755 ./jobs
```

### TORCH Tests Fail

Verify TORCH service is running and sample data exists:
```bash
docker ps | grep torch
ls .github/test/torch/queries/
```

## Next Steps

- [Architecture](./architecture.md) - Understand system design
- [Contributing](./contributing.md) - How to contribute changes
- [Coding Guidelines](./coding-guidelines.md) - Code style and standards
