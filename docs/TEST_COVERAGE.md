# Test Coverage for Multi-Step Pipeline Execution

This document describes the comprehensive test suite created to verify that the pipeline correctly executes all enabled steps, that configuration is loaded properly, and that validation logic functions correctly.

## Test Files

### 1. `tests/unit/config_validation_test.go` ⭐
**New in this branch** - Unit tests for configuration and model validation.

#### TestProjectConfig_Validate()
**Critical validation test** - Verifies that `ProjectConfig.Validate()` correctly validates all configuration fields.

- **Empty enabled steps**: Ensures error when no pipeline steps are configured
- **First step validation**: Verifies that first enabled step MUST be an import step (torch, local_import, or http_import)
  - Tests rejection of non-import first steps (validation, dimp, etc.)
  - Tests acceptance of all three import step types
- **Unrecognized steps**: Validates error for invalid step names
- **Retry configuration**: Tests bounds checking for max_attempts (1-10), backoff values (positive), and backoff ordering
- **Jobs directory**: Ensures jobs_dir is required

**Test Cases**: 13 comprehensive test scenarios covering all validation error paths

#### TestValidateSplitConfig()
Validates Bundle splitting threshold configuration (1-100 MB range).

### 2. `tests/unit/import_step_test.go`
**New in this branch** - Unit tests for import step error handling.

#### TestExecuteImportStep_UnsupportedInputType()
Tests error handling for unknown/unsupported input types.

- Verifies proper error message for unsupported input types
- Ensures job state is updated correctly on error
- Tests error classification as non-transient

### 3. `tests/unit/pipeline_job_test.go` (Enhanced)
**Enhanced in this branch** - Added edge case tests for job lifecycle.

#### TestAdvanceToNextStep_NoMoreSteps()
Tests job completion when advancing past the last step.

- Verifies job is marked as completed
- Ensures current step is cleared

#### TestStartJob_EmptySteps()
Tests StartJob when job has no steps defined.

- Verifies job transitions to in_progress even with empty steps
- Edge case handling for malformed job configurations

#### TestCompleteJob()
Tests CompleteJob function behavior.

- Verifies job status transitions to completed
- Ensures current step is cleared

### 4. `tests/integration/pipeline_multistep_test.go`
Integration tests for multi-step pipeline execution.

#### TestPipelineMultiStep_AutomaticExecution ⭐
**Critical regression test** - Verifies that `pipeline start` automatically executes ALL enabled steps, not just import.

- Creates a job with both `import` and `dimp` steps enabled
- Executes import step
- **Verifies that the next step is DIMP** (not empty)
- Advances to DIMP and executes it
- Verifies pseudonymized files are created
- **This test would have caught the original bug**

#### TestPipelineMultiStep_StepSequencing
Verifies that `GetNextStep()` returns steps in the correct order.

- Tests that import → dimp → validation → flattening works correctly
- Tests that the last step returns empty string (no more steps)

#### TestPipelineMultiStep_OnlyImportEnabled
Verifies pipeline works correctly when only import is enabled.

- Creates job with only import step
- Verifies no next step exists after import
- Verifies job is marked as complete when no more steps

#### TestPipelineMultiStep_ConfigLoadingPreservesSteps
**Regression test for viper.Unmarshal bug** - Verifies config loading doesn't drop enabled steps.

- Writes a YAML config with multiple steps
- Loads config via `LoadConfig()`
- Verifies ALL steps are present (import, dimp, flattening)
- Verifies service URLs are loaded correctly

#### TestPipelineMultiStep_JobStatePersistedBetweenSteps
Verifies job state is correctly saved after each step execution.

- Executes import step and saves state
- Loads job from disk and verifies import step is complete
- Executes DIMP step and saves state
- Loads job again and verifies both steps are complete

---

### 5. `tests/unit/config_loading_test.go`
Unit tests specifically for configuration loading (the root cause of the bug).

#### TestConfigLoading_MultipleEnabledSteps ⭐
**Critical regression test** - Verifies viper doesn't drop enabled steps when loading YAML.

- Tests loading 4 enabled steps: import, dimp, validation, flattening
- Verifies ALL 4 steps are present in loaded config
- **This test would have caught the viper.Unmarshal bug**

#### TestConfigLoading_ServiceURLs
Verifies all service URLs are loaded correctly from YAML.

- Tests DIMP URL and Flattening URL
- Verifies exact values match what's in the config file

#### TestConfigLoading_RetrySettings
Verifies retry configuration values are loaded.

- Tests max_attempts, initial_backoff_ms, max_backoff_ms
- Ensures retry config isn't using defaults when values are specified

#### TestConfigLoading_JobsDirectory
Verifies custom jobs_dir path is loaded.

- Tests that non-default jobs directory is preserved

#### TestConfigLoading_EmptyServiceURLs
Verifies empty service URLs remain empty (not replaced with defaults).

- Important for optional services

#### TestConfigLoading_PartialServiceURLs
Verifies mixed empty and non-empty service URLs.

- Tests DIMP URL present, CSV empty, Parquet present
- Ensures selective service configuration works

#### TestConfigLoading_NoConfigFile
Verifies error handling when specified config file doesn't exist.

- Tests that explicit file path that doesn't exist returns error

#### TestConfigLoading_StepOrder
Verifies steps are loaded in the exact order specified in YAML.

- Tests non-standard order: import → validation → dimp → parquet → csv
- Ensures order is preserved exactly as in config

#### TestConfigLoading_MinimalConfig
Verifies minimal valid configuration loads successfully.

- Tests config with only required fields
- Ensures defaults work for optional fields

---

## Running the Tests

```bash
# Run validation tests
go test ./tests/unit/config_validation_test.go -v -run TestProjectConfig_Validate

# Run import error handling tests
go test ./tests/unit/import_step_test.go -v

# Run enhanced job lifecycle tests
go test ./tests/unit/pipeline_job_test.go -v

# Run all multi-step integration tests
go test ./tests/integration/pipeline_multistep_test.go -v

# Run all config loading unit tests
go test ./tests/unit/config_loading_test.go -v

# Run all unit tests
go test ./tests/unit/... -v

# Run all tests in the test suite
go test ./tests/... -v
```

## Test Summary

| Test File | Tests | Status | Notes |
|-----------|-------|--------|-------|
| `config_validation_test.go` | 13+ tests | ✅ All Pass | **New in this branch** |
| `import_step_test.go` | 1 test | ✅ All Pass | **New in this branch** |
| `pipeline_job_test.go` | 12+ tests | ✅ All Pass | 3 tests added |
| `pipeline_multistep_test.go` | 5 tests | ✅ All Pass | |
| `config_loading_test.go` | 9 tests | ✅ All Pass | |
| **Total** | **40+ tests** | ✅ **All Pass** | |

## Critical Tests (Regression Prevention & Validation)

These tests specifically target bugs, edge cases, and validation requirements:

### Regression Prevention
1. **TestPipelineMultiStep_AutomaticExecution** - Ensures pipeline executes all enabled steps, not just import
2. **TestConfigLoading_MultipleEnabledSteps** - Ensures viper doesn't drop enabled steps during config loading
3. **TestPipelineMultiStep_ConfigLoadingPreservesSteps** - Integration test combining both fixes

### Validation & Error Handling (New in this branch)
4. **TestProjectConfig_Validate** - Comprehensive validation of configuration fields, especially:
   - First step must be an import step (critical business rule)
   - Retry configuration bounds checking
   - Required fields validation
5. **TestExecuteImportStep_UnsupportedInputType** - Error handling for unknown input types
6. **TestAdvanceToNextStep_NoMoreSteps** - Job completion edge case
7. **TestStartJob_EmptySteps** - Malformed job configuration handling

## Test Coverage

The test suite covers:

### Pipeline Execution
- ✅ Automatic multi-step execution
- ✅ Step sequencing and order
- ✅ Job state persistence between steps
- ✅ Step advancement logic
- ✅ Job lifecycle (start, complete, fail)

### Configuration & Validation
- ✅ Config loading (all fields)
- ✅ Service URLs (empty, partial, full)
- ✅ **Config validation (first step, retry bounds, required fields)** ⭐ New
- ✅ YAML parsing correctness

### Error Handling
- ✅ **Unknown input type handling** ⭐ New
- ✅ Network errors and retries
- ✅ File system errors
- ✅ Validation errors

### Edge Cases
- ✅ Only import step enabled
- ✅ **Empty steps array** ⭐ New
- ✅ **Last step completion** ⭐ New
- ✅ No config file
- ✅ Minimal config
- ✅ Unrecognized step names

## Continuous Integration

These tests should be run in CI/CD pipelines to prevent regression and ensure validation correctness.

Recommended CI commands:
```bash
# Run all unit tests (includes new validation tests)
go test ./tests/unit/... -v

# Run critical regression tests
go test ./tests/unit/config_loading_test.go ./tests/integration/pipeline_multistep_test.go -v

# Run validation tests specifically
go test ./tests/unit/config_validation_test.go ./tests/unit/import_step_test.go -v
```

## Coverage Improvements (This Branch)

### Files with Improved Coverage
1. **internal/models/validation.go** - Added 13 test cases for `ProjectConfig.Validate()`
   - Increased coverage of validation error paths
   - **Specifically tests the "first step must be an import" requirement**

2. **internal/pipeline/import.go** - Added test for unsupported input type error handling
   - Covers the default case in import step switch statement

3. **internal/pipeline/job.go** - Added 3 edge case tests
   - Job completion when no more steps
   - Starting job with empty steps array
   - Job completion verification

### Test Statistics
- **Before**: ~26 tests
- **After**: 40+ tests
- **New tests**: 17+
- **Files affected**: 3 implementation files with improved coverage
