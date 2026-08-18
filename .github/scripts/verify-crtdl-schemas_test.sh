#!/usr/bin/env bash
# Tests for verify-crtdl-schemas.sh. Each test builds a throwaway manifest, a
# throwaway schema directory, and a throwaway upstream mirror that the script
# reads over file:// URLs. The script is therefore exercised through its real
# interface: a manifest, local files, and a remote.
#
# Usage: .github/scripts/verify-crtdl-schemas_test.sh
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/verify-crtdl-schemas.sh"
failures=0

REPOSITORY="example-org/example-backend"
UPSTREAM_PATH="src/main/resources/schema"

# new_case <ref> <drift_ref> -> prints a case directory
#
# The case directory holds:
#   manifest.json  the pin
#   local/         the vendored copies
#   remote/        the upstream mirror, one tree per ref
new_case() {
  local ref="$1" drift_ref="$2" dir
  dir="$(mktemp -d)"
  mkdir -p "$dir/local"
  mkdir -p "$dir/remote/$REPOSITORY/$ref/$UPSTREAM_PATH"
  mkdir -p "$dir/remote/$REPOSITORY/$drift_ref/$UPSTREAM_PATH"
  printf '%s' "$dir"
}

# put_schema <case> <file> <content>
#
# Writes the same content to the vendored copy and to both remote trees, then
# records the checksum in the manifest. A test that wants a difference
# overwrites one copy afterwards.
#
# The content ends with a newline, as the real schema files do. A download that
# loses the last newline then gives a different checksum, and the tests see it.
put_schema() {
  local dir="$1" file="$2" content="$3"
  printf '%s\n' "$content" >"$dir/local/$file"
  local ref
  for ref in "$dir"/remote/"$REPOSITORY"/*; do
    printf '%s\n' "$content" >"$ref/$UPSTREAM_PATH/$file"
  done
}

# write_manifest <case> <ref> <drift_ref> <file>...
#
# The checksums come from the vendored copies, so a manifest always matches the
# local files unless a test edits one afterwards.
write_manifest() {
  local dir="$1" ref="$2" drift_ref="$3"
  shift 3
  local files_json="{}" file sum
  for file in "$@"; do
    sum="$(sha256sum "$dir/local/$file" | cut -d' ' -f1)"
    files_json="$(printf '%s' "$files_json" | jq --arg f "$file" --arg s "$sum" '.[$f] = $s')"
  done
  jq -n \
    --arg repository "$REPOSITORY" \
    --arg path "$UPSTREAM_PATH" \
    --arg ref "$ref" \
    --arg drift_ref "$drift_ref" \
    --argjson files "$files_json" \
    '{repository: $repository, path: $path, ref: $ref, drift_ref: $drift_ref, files: $files}' \
    >"$dir/manifest.json"
}

# run_script <case> [args...] -> sets OUTPUT and STATUS
run_script() {
  local dir="$1"
  shift
  OUTPUT="$(SCHEMA_MANIFEST="$dir/manifest.json" \
    SCHEMA_DIR="$dir/local" \
    SCHEMA_BASE_URL="file://$dir/remote" \
    "$SCRIPT" "$@" 2>&1)"
  STATUS=$?
}

# check <name> <expected-status>  (uses STATUS from the last run_script)
check() {
  local name="$1" expected="$2"
  if [ "$STATUS" -eq "$expected" ]; then
    echo "PASS: $name"
  else
    echo "FAIL: $name (expected exit $expected, got $STATUS)"
    printf '%s\n' "$OUTPUT" | sed 's/^/      /'
    failures=$((failures + 1))
  fi
}

# --- behaviour 1: files that match the pin and the upstream ref pass ---------
test_matching_schemas_pass() {
  local dir
  dir="$(new_case v1.0.0 develop)"
  put_schema "$dir" "crtdl-schema.json" '{"id":"crtdl"}'
  put_schema "$dir" "ccdl-schema.json" '{"id":"ccdl"}'
  write_manifest "$dir" v1.0.0 develop "crtdl-schema.json" "ccdl-schema.json"
  run_script "$dir"
  check "matching schemas pass" 0
  rm -rf "$dir"
}

test_matching_schemas_pass

# assert_output_contains <name> <needle>  (uses OUTPUT from the last run_script)
assert_output_contains() {
  local name="$1" needle="$2"
  if printf '%s' "$OUTPUT" | grep -qF -- "$needle"; then
    echo "PASS: $name"
  else
    echo "FAIL: $name (output does not contain '$needle')"
    printf '%s\n' "$OUTPUT" | sed 's/^/      /'
    failures=$((failures + 1))
  fi
}

# --- behaviour 2: an edited vendored copy fails and is named -----------------
test_edited_local_copy_fails() {
  local dir
  dir="$(new_case v1.0.0 develop)"
  put_schema "$dir" "crtdl-schema.json" '{"id":"crtdl"}'
  put_schema "$dir" "ccdl-schema.json" '{"id":"ccdl"}'
  write_manifest "$dir" v1.0.0 develop "crtdl-schema.json" "ccdl-schema.json"
  printf '%s\n' '{"id":"crtdl","extra":true}' >"$dir/local/crtdl-schema.json"
  run_script "$dir"
  check "edited vendored copy fails" 1
  assert_output_contains "edited copy is named" "crtdl-schema.json"
  rm -rf "$dir"
}

test_edited_local_copy_fails

# --- behaviour 3: a rewritten upstream ref fails ----------------------------
# The vendored copy still matches the manifest, so only the comparison with
# upstream can find this.
test_rewritten_upstream_ref_fails() {
  local dir
  dir="$(new_case v1.0.0 develop)"
  put_schema "$dir" "crtdl-schema.json" '{"id":"crtdl"}'
  write_manifest "$dir" v1.0.0 develop "crtdl-schema.json"
  printf '%s\n' '{"id":"crtdl","rewritten":true}' \
    >"$dir/remote/$REPOSITORY/v1.0.0/$UPSTREAM_PATH/crtdl-schema.json"
  run_script "$dir"
  check "rewritten upstream ref fails" 1
  assert_output_contains "rewritten ref is named" "v1.0.0"
  rm -rf "$dir"
}

test_rewritten_upstream_ref_fails

# --- behaviour 4: a change on the drift ref warns but does not fail ---------
# The pin is still correct, so the build must not break. The warning is the
# only signal that upstream moved.
test_drift_warns_without_failing() {
  local dir
  dir="$(new_case v1.0.0 develop)"
  put_schema "$dir" "crtdl-schema.json" '{"id":"crtdl"}'
  write_manifest "$dir" v1.0.0 develop "crtdl-schema.json"
  printf '%s\n' '{"id":"crtdl","added":true}' \
    >"$dir/remote/$REPOSITORY/develop/$UPSTREAM_PATH/crtdl-schema.json"
  run_script "$dir"
  check "drift does not fail the build" 0
  assert_output_contains "drift names the file" "crtdl-schema.json"
  assert_output_contains "drift names the ref" "develop"
  rm -rf "$dir"
}

test_drift_warns_without_failing

# --- behaviour 5: --drift-only gives drift its own exit code ----------------
# The scheduled job opens an issue on this code, so it must differ from both
# the clean code and the mismatch code.
test_drift_only_exit_code() {
  local dir
  dir="$(new_case v1.0.0 develop)"
  put_schema "$dir" "crtdl-schema.json" '{"id":"crtdl"}'
  write_manifest "$dir" v1.0.0 develop "crtdl-schema.json"
  run_script "$dir" --drift-only
  check "--drift-only passes when upstream is unchanged" 0

  printf '%s\n' '{"id":"crtdl","added":true}' \
    >"$dir/remote/$REPOSITORY/develop/$UPSTREAM_PATH/crtdl-schema.json"
  run_script "$dir" --drift-only
  check "--drift-only reports drift with exit 3" 3
  rm -rf "$dir"
}

test_drift_only_exit_code

# --- behaviour 6: a download that fails is not a clean run ------------------
# A network fault must not look the same as "the pin is correct".
test_download_failure_fails_closed() {
  local dir
  dir="$(new_case v1.0.0 develop)"
  put_schema "$dir" "crtdl-schema.json" '{"id":"crtdl"}'
  write_manifest "$dir" v1.0.0 develop "crtdl-schema.json"
  rm "$dir/remote/$REPOSITORY/v1.0.0/$UPSTREAM_PATH/crtdl-schema.json"
  run_script "$dir"
  check "unreachable upstream file fails closed" 2
  rm -rf "$dir"
}

test_download_failure_fails_closed

# --- behaviour 7: a missing manifest fails closed ---------------------------
test_missing_manifest_fails() {
  local dir
  dir="$(new_case v1.0.0 develop)"
  run_script "$dir" --local-only
  check "missing manifest fails closed" 2
  rm -rf "$dir"
}

test_missing_manifest_fails

# --- behaviour 8: the vendored schemas match their own manifest -------------
# This runs against the real files with no override, so an edit to a vendored
# copy or to the manifest breaks the suite.
test_repository_schemas_match_the_pin() {
  OUTPUT="$("$SCRIPT" --local-only 2>&1)"
  STATUS=$?
  check "vendored schemas match the pinned checksums" 0
}

test_repository_schemas_match_the_pin

if [ "$failures" -gt 0 ]; then
  echo "$failures test(s) failed"
  exit 1
fi
echo "all tests passed"
