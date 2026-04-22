# Testing Guide: Pipeline Resumption

This guide shows how to test User Story 2 (Pipeline Resumption) features.

## Prerequisites

✅ Test data exists in `test-data/` (13 FHIR files, 5.6MB)
✅ Binary built at `bin/aether`

## Quick Start

### 1. Start a Pipeline Job

```bash
# Start a new pipeline job importing from test-data
./bin/aether pipeline start --input ./test-data

# Example output:
# Creating new pipeline job...
# ✓ Created job: abc-123-def
# Starting import...
# [====================] 100% (13/13 files, 5.6MB)
# ✓ Import completed
```

**Save the Job ID** (e.g., `abc-123-def`) from the output!

---

## Test Scenarios

### Scenario 1: List All Jobs

```bash
./bin/aether job list
```

**Expected Output:**
```
JOB ID                                STATUS          STEP                 FILES    RETRIES  AGE
------------------------------------------------------------------------------------------------------------------------
abc-123-def                           → in_progress   dimp                 13       0        5s

Total: 1 jobs
```

**What to verify:**
- ✓ Job appears in list
- ✓ Status shows current state
- ✓ Retry count is displayed
- ✓ File count matches (13 files)

---

### Scenario 2: Check Job Status

```bash
./bin/aether pipeline status <job-id>
```

**Expected Output:**
```
Job ID: abc-123-def
Status: in_progress
Input: ./test-data
Created: 2025-10-09 10:30:00

Steps:
  ✓ import        - completed (13 files, 5.6 MB) [0 retries]
    dimp          - pending
    flattening     - pending

Total Files: 13
Total Data: 5.6 MB
Duration: 5s
```

**What to verify:**
- ✓ Import step shows "completed"
- ✓ Next step (dimp) shows "pending"
- ✓ Retry count shown for each step
- ✓ File counts and sizes accurate

---

### Scenario 3: Resume Pipeline (Terminal Restart Simulation)

This tests the key feature: **resuming after terminal close**.

```bash
# Step 1: Check current job state
./bin/aether job list

# Step 2: Resume the pipeline from where it left off
./bin/aether pipeline continue <job-id>
```

**Expected Output:**
```
Loading job abc-123-def...
Current status: in_progress
Current step: import

Advancing to next step: dimp

Resuming pipeline execution...
Next step: dimp

DIMP step not yet implemented - job will remain at this step
You can manually execute DIMP processing and then continue the pipeline

Use 'aether pipeline status abc-123-def' to check progress
```

**What to verify:**
- ✓ Job loads successfully from disk
- ✓ Pipeline identifies next step correctly
- ✓ State preserved across "sessions"

---

### Scenario 4: Test State Persistence

This verifies that job state survives across CLI invocations.

```bash
# Step 1: Start a job
JOB_ID=$(./bin/aether pipeline start --input ./test-data | grep "Created job" | awk '{print $4}')
echo "Job ID: $JOB_ID"

# Step 2: Check status immediately
./bin/aether pipeline status $JOB_ID

# Step 3: "Close terminal" (just wait a few seconds)
sleep 5

# Step 4: List jobs again (simulating new terminal session)
./bin/aether job list

# Step 5: Continue pipeline
./bin/aether pipeline continue $JOB_ID
```

**What to verify:**
- ✓ Job ID persists in `jobs/` directory
- ✓ Job appears in list after delay
- ✓ State.json file contains accurate data
- ✓ Pipeline can continue from saved state

---

### Scenario 5: Check Job Directory Structure

```bash
# Replace <job-id> with your actual job ID
ls -la jobs/<job-id>/
```

**Expected Structure:**
```
jobs/<job-id>/
├── state.json          # Job state (status, steps, retry counts)
├── import/             # Imported FHIR files (13 files)
├── pseudonymized/      # DIMP output (when implemented)
├── csv/                # CSV output (when implemented)
└── parquet/            # Parquet output (when implemented)
```

**Inspect state.json:**
```bash
cat jobs/<job-id>/state.json | jq .
```

**What to verify:**
- ✓ All 13 files copied to `import/` directory
- ✓ state.json contains job metadata
- ✓ Steps array shows import as completed
- ✓ Retry counts are 0 for successful steps

---

## Advanced Testing

### Test Retry Count Tracking

The retry logic is already implemented for import steps. To test:

1. **Check retry count in job list:**
   ```bash
   ./bin/aether job list
   # Look at RETRIES column
   ```

2. **Inspect state.json retry information:**
   ```bash
   cat jobs/<job-id>/state.json | jq '.steps[] | {name, retry_count, last_error}'
   ```

---

### Test Multiple Jobs

```bash
# Create 3 jobs
./bin/aether pipeline start --input ./test-data
sleep 2
./bin/aether pipeline start --input ./test-data
sleep 2
./bin/aether pipeline start --input ./test-data

# List all jobs (should show 3)
./bin/aether job list

# Verify sorting (newest first)
```

**What to verify:**
- ✓ All jobs listed
- ✓ Sorted by creation time (newest first)
- ✓ Each has unique Job ID

---

## Running Automated Tests

```bash
# Run all unit tests
go test ./tests/unit/... -v

# Run validation tests specifically
go test ./tests/unit/config_validation_test.go -v -run TestProjectConfig_Validate

# Run import error handling tests
go test ./tests/unit/import_step_test.go -v

# Run all integration tests
go test ./tests/integration/... -v

# Run all tests together
go test ./tests/... -v

# Run specific test
go test ./tests/integration/pipeline_resume_test.go -v
```

**All tests should PASS** ✅

---

## Testing Configuration Validation

### Scenario: Test Configuration Validation

This tests the validation logic for project configuration, especially the critical business rule that **the first enabled step must be an import step**.

```bash
# Run the comprehensive validation test suite
go test ./tests/unit/config_validation_test.go -v -run TestProjectConfig_Validate
```

**What is tested:**
- ✅ First step must be torch_import, local_import, or http_import
- ✅ Rejection of non-import first steps (validation, dimp, etc.)
- ✅ Empty enabled steps array error handling
- ✅ Unrecognized step names
- ✅ Retry configuration bounds (max_attempts: 1-10)
- ✅ Backoff validation (positive values, proper ordering)
- ✅ Required fields (jobs_dir)

**Example Test Cases:**
1. **Valid config** - First step is local_import ✓
2. **Invalid config** - First step is validation ✗ (Error: "first enabled step must be an import step")
3. **Invalid config** - Empty steps array ✗ (Error: "at least one pipeline step must be enabled")

---

## Testing Error Handling

### Scenario: Test Unknown Input Type

```bash
# Test handling of unsupported input types
go test ./tests/unit/import_step_test.go -v
```

**What is tested:**
- ✅ Error message for unsupported input types
- ✅ Job state updates correctly on error
- ✅ Error classification (transient vs non-transient)

---

## Testing Job Lifecycle Edge Cases

### Scenario: Test Job State Transitions

```bash
# Test edge cases in job lifecycle
go test ./tests/unit/pipeline_job_test.go -v -run "TestAdvanceToNextStep_NoMoreSteps|TestStartJob_EmptySteps|TestCompleteJob"
```

**What is tested:**
- ✅ Job completion when no more steps remain
- ✅ Starting a job with empty steps array
- ✅ Job status transitions during completion

---

## Verification Checklist

After running the scenarios above, verify:

- [ ] ✅ `job list` command shows all jobs with retry counts
- [ ] ✅ `pipeline status` command displays detailed step information
- [ ] ✅ `pipeline continue` command resumes from correct step
- [ ] ✅ Job state persists in `jobs/<job-id>/state.json`
- [ ] ✅ Import step completes successfully with all 13 files
- [ ] ✅ Files copied to `jobs/<job-id>/import/` directory
- [ ] ✅ Multiple jobs can coexist
- [ ] ✅ Jobs sorted by creation time in list
- [ ] ✅ All automated tests pass

---

## Troubleshooting

### Job not found
```bash
# Check if jobs directory exists
ls -la jobs/

# Check if state.json exists for job
ls -la jobs/<job-id>/state.json
```

### Import fails
```bash
# Check test data exists
ls -la test-data/

# Verify files are valid NDJSON
head -1 test-data/*.ndjson
```

### Build errors
```bash
# Rebuild binary
go build -o bin/aether cmd/aether/main.go

# Check for compilation errors
go build ./...
```

---

## Next Steps

Once you've verified User Story 2 works:

1. ✅ Pipeline resumption functional
2. ✅ State persistence working
3. ✅ Retry tracking operational
4. 🚀 Ready to implement User Story 3 (DIMP) or User Story 4 (Format Conversion)

---

## File Locations

- **Binary**: `bin/aether`
- **Test Data**: `test-data/` (13 FHIR files)
- **Config**: `config/aether.example.yaml`
- **Jobs**: `jobs/<job-id>/`
- **Tests**: `tests/unit/`, `tests/integration/`

---

Happy testing! 🎉
