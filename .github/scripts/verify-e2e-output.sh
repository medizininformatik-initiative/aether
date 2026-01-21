#!/usr/bin/env bash
set -euo pipefail

# Verification script for E2E test outputs
# Validates that the aether pipeline produced expected output structure and content

JOBS_DIR="${JOBS_DIR:-./jobs}"
STRICT_MODE="${STRICT_MODE:-false}"  # Set to 'true' to fail on warnings

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

ERRORS=0
WARNINGS=0

error() {
    echo -e "${RED}❌ ERROR: $1${NC}" >&2
    ERRORS=$((ERRORS + 1))
}

warn() {
    echo -e "${YELLOW}⚠️  WARNING: $1${NC}"
    WARNINGS=$((WARNINGS + 1))
}

success() {
    echo -e "${GREEN}✅ $1${NC}"
}

info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

# Check if jobs directory exists
check_jobs_dir() {
    info "Checking jobs directory: ${JOBS_DIR}"

    if [[ ! -d "${JOBS_DIR}" ]]; then
        error "Jobs directory does not exist: ${JOBS_DIR}"
        return 1
    fi

    success "Jobs directory exists"
    return 0
}

# Find all job directories
find_jobs() {
    if [[ ! -d "${JOBS_DIR}" ]]; then
        return 1
    fi

    # List all directories in jobs dir that look like UUIDs
    find "${JOBS_DIR}" -mindepth 1 -maxdepth 1 -type d
}

# Verify a single job's structure and content
verify_job() {
    local job_dir="$1"
    local job_id=$(basename "${job_dir}")

    echo ""
    info "=== Verifying Job: ${job_id} ==="

    # Check state.json exists
    local state_file="${job_dir}/state.json"
    if [[ ! -f "${state_file}" ]]; then
        error "Job ${job_id}: state.json not found"
        return 1
    fi
    success "Job ${job_id}: state.json exists"

    # Parse state.json to get job info
    if ! jq empty "${state_file}" 2>/dev/null; then
        error "Job ${job_id}: state.json is not valid JSON"
        return 1
    fi
    success "Job ${job_id}: state.json is valid JSON"

    local job_status=$(jq -r '.status' "${state_file}")
    local input_source=$(jq -r '.input_source' "${state_file}")
    local total_files=$(jq -r '.total_files // 0' "${state_file}")
    local total_bytes=$(jq -r '.total_bytes // 0' "${state_file}")

    info "Job ${job_id}: Status=${job_status}, Files=${total_files}, Bytes=${total_bytes}"
    info "Job ${job_id}: Input source=${input_source}"

    # Check if job completed successfully
    if [[ "${job_status}" != "completed" ]]; then
        warn "Job ${job_id}: Status is '${job_status}' (expected 'completed')"
    else
        success "Job ${job_id}: Status is 'completed'"
    fi

    # Verify import directory exists
    local import_dir="${job_dir}/import"
    if [[ ! -d "${import_dir}" ]]; then
        error "Job ${job_id}: import directory not found"
        return 1
    fi
    success "Job ${job_id}: import directory exists"

    # Count NDJSON files in import directory (both uncompressed and compressed)
    local ndjson_count=$(find "${import_dir}" \( -name "*.ndjson" -o -name "*.ndjson.zst" \) -type f | wc -l)
    if [[ ${ndjson_count} -eq 0 ]]; then
        error "Job ${job_id}: No NDJSON files found in import directory"
    else
        success "Job ${job_id}: Found ${ndjson_count} NDJSON file(s) in import directory"
    fi

    # Verify NDJSON files contain valid content
    local valid_files=0
    while IFS= read -r ndjson_file; do
        if [[ -f "${ndjson_file}" ]]; then
            # Check if file is not empty
            if [[ ! -s "${ndjson_file}" ]]; then
                warn "Job ${job_id}: NDJSON file is empty: $(basename "${ndjson_file}")"
                continue
            fi

            # Check if file contains FHIR resourceType (basic validation)
            # Handle both compressed (.zst) and uncompressed files
            if [[ "${ndjson_file}" == *.zst ]]; then
                # Decompress and check for resourceType
                if zstd -dcq "${ndjson_file}" 2>/dev/null | rg -q '"resourceType"'; then
                    valid_files=$((valid_files + 1))
                else
                    warn "Job ${job_id}: NDJSON file may not contain valid FHIR resources: $(basename "${ndjson_file}")"
                fi
            else
                if rg -q '"resourceType"' "${ndjson_file}"; then
                    valid_files=$((valid_files + 1))
                else
                    warn "Job ${job_id}: NDJSON file may not contain valid FHIR resources: $(basename "${ndjson_file}")"
                fi
            fi
        fi
    done < <(find "${import_dir}" \( -name "*.ndjson" -o -name "*.ndjson.zst" \) -type f)

    if [[ ${valid_files} -gt 0 ]]; then
        success "Job ${job_id}: ${valid_files} valid NDJSON file(s) verified"
    fi

    # Check for pseudonymized directory (if DIMP step was enabled)
    local pseudo_dir="${job_dir}/pseudonymized"
    if [[ -d "${pseudo_dir}" ]]; then
        info "Job ${job_id}: pseudonymized directory exists"
        local pseudo_count=$(find "${pseudo_dir}" \( -name "*.ndjson" -o -name "*.ndjson.zst" \) -type f | wc -l)
        if [[ ${pseudo_count} -gt 0 ]]; then
            success "Job ${job_id}: Found ${pseudo_count} pseudonymized file(s)"
        else
            warn "Job ${job_id}: pseudonymized directory exists but contains no NDJSON files"
        fi
    fi

    # Verify file counts match state.json
    if [[ ${ndjson_count} -ne ${total_files} ]]; then
        warn "Job ${job_id}: File count mismatch - found ${ndjson_count} files, state.json reports ${total_files}"
    fi

    return 0
}

# Main verification logic
main() {
    echo "========================================="
    echo "  Aether E2E Output Verification"
    echo "========================================="
    echo ""

    # Check jobs directory exists
    if ! check_jobs_dir; then
        echo ""
        error "Cannot proceed without jobs directory"
        exit 1
    fi

    # Find all jobs
    local job_dirs=($(find_jobs))
    local job_count=${#job_dirs[@]}

    if [[ ${job_count} -eq 0 ]]; then
        warn "No jobs found in ${JOBS_DIR}"
        echo ""
        echo "========================================="
        echo "  Summary: ${WARNINGS} warning(s)"
        echo "========================================="
        exit 0
    fi

    info "Found ${job_count} job(s) to verify"

    # Verify each job
    for job_dir in "${job_dirs[@]}"; do
        verify_job "${job_dir}"
    done

    # Print summary
    echo ""
    echo "========================================="
    echo "  Verification Summary"
    echo "========================================="
    echo "Jobs verified: ${job_count}"
    echo "Errors: ${ERRORS}"
    echo "Warnings: ${WARNINGS}"
    echo "========================================="

    # Exit based on results
    if [[ ${ERRORS} -gt 0 ]]; then
        error "Verification failed with ${ERRORS} error(s)"
        exit 1
    elif [[ ${WARNINGS} -gt 0 ]]; then
        if [[ "${STRICT_MODE}" == "true" ]]; then
            error "Verification failed in strict mode with ${WARNINGS} warning(s)"
            exit 1
        else
            warn "Verification completed with ${WARNINGS} warning(s)"
            exit 0
        fi
    else
        echo ""
        success "All verifications passed!"
        exit 0
    fi
}

# Run main
main "$@"
