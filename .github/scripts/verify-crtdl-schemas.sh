#!/usr/bin/env bash
# Verifies that the vendored CRTDL and CCDL schemas are unchanged copies of the
# upstream files at the pinned ref, and reports when upstream moves on.
#
# Usage: verify-crtdl-schemas.sh [--local-only|--drift-only]
#
#   (no option)   Compare the vendored copies with the pinned checksums and
#                 with upstream at the pinned ref. Then warn about drift.
#   --local-only  Compare with the pinned checksums only. No network.
#   --drift-only  Compare the pinned checksums with upstream on the drift ref
#                 only. Exit 3 when they differ.
#
# Exit codes: 0 clean, 1 mismatch, 2 setup or download error, 3 drift
# (--drift-only only).
#
# Env: SCHEMA_MANIFEST  path to the manifest (default: the vendored one)
#      SCHEMA_DIR       directory of the vendored copies (default: the
#                       directory of the manifest)
#      SCHEMA_BASE_URL  root of the raw file URLs
set -euo pipefail

mode="all"
case "${1:-}" in
  --local-only) mode="local" ;;
  --drift-only) mode="drift" ;;
  "") ;;
  *)
    printf 'usage: verify-crtdl-schemas.sh [--local-only|--drift-only]\n' >&2
    exit 2
    ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
manifest="${SCHEMA_MANIFEST:-$script_dir/../../internal/lib/crtdl/schema/upstream.json}"
base_url="${SCHEMA_BASE_URL:-https://raw.githubusercontent.com}"

if [ ! -f "$manifest" ]; then
  printf 'verify-crtdl-schemas: no manifest at %s\n' "$manifest" >&2
  exit 2
fi

schema_dir="${SCHEMA_DIR:-$(dirname "$manifest")}"
repository="$(jq -r '.repository' "$manifest")"
upstream_path="$(jq -r '.path' "$manifest")"
ref="$(jq -r '.ref' "$manifest")"
drift_ref="$(jq -r '.drift_ref' "$manifest")"

download="$(mktemp)"
trap 'rm -f "$download"' EXIT

# remote_sum <ref> <file> -> sets REMOTE_SUM
#
# The download goes to a file, not to a variable: a command substitution
# removes the trailing newline, and the checksum would then never match.
#
# The result goes in a variable rather than on stdout, so that a failed
# download can end the whole run. From a command substitution it could only
# end its own subshell, and an empty answer reads as a mismatch.
remote_sum() {
  local at_ref="$1" file="$2" url
  url="$base_url/$repository/$at_ref/$upstream_path/$file"
  if ! curl -sSfL --retry 3 -o "$download" "$url"; then
    printf 'verify-crtdl-schemas: cannot download %s\n' "$url" >&2
    exit 2
  fi
  REMOTE_SUM="$(sha256sum "$download" | cut -d' ' -f1)"
}

status=0
drift=0

while read -r file expected; do
  if [ "$mode" != "drift" ]; then
    if [ ! -f "$schema_dir/$file" ]; then
      printf 'verify-crtdl-schemas: %s is missing from %s\n' "$file" "$schema_dir" >&2
      status=1
      continue
    fi
    local_sum="$(sha256sum "$schema_dir/$file" | cut -d' ' -f1)"
    if [ "$local_sum" != "$expected" ]; then
      printf 'verify-crtdl-schemas: %s does not match the pinned checksum\n' "$file" >&2
      status=1
      continue
    fi
  fi

  if [ "$mode" = "all" ]; then
    remote_sum "$ref" "$file"
    if [ "$REMOTE_SUM" != "$expected" ]; then
      printf 'verify-crtdl-schemas: %s does not match upstream at %s\n' "$file" "$ref" >&2
      status=1
    fi
  fi

  # The comparison with the pinned ref always gives the same answer, because
  # both sides are fixed. Only this second comparison shows that upstream
  # moved on.
  if [ "$mode" != "local" ]; then
    remote_sum "$drift_ref" "$file"
    if [ "$REMOTE_SUM" != "$expected" ]; then
      printf 'verify-crtdl-schemas: warning: %s differs from upstream %s\n' "$file" "$drift_ref" >&2
      drift=1
    fi
  fi
done < <(jq -r '.files | to_entries[] | "\(.key) \(.value)"' "$manifest")

if [ "$status" -ne 0 ]; then
  cat >&2 <<EOF

The schemas in $schema_dir are copies of upstream files. See SOURCE.md in that
directory for the update steps.
EOF
  exit "$status"
fi

if [ "$mode" = "drift" ] && [ "$drift" -ne 0 ]; then
  exit 3
fi

exit 0
