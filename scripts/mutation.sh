#!/bin/bash
# Run gremlins mutation testing. The first argument selects the mode:
# "diff" tests only the lines that differ from MUTATION_REF, anything else
# tests every mutant.

set -euo pipefail

GREMLINS_VERSION="v0.6.0"

MODE="${1:-full}"
MUTATION_REF="${MUTATION_REF:-origin/main}"
PKG="${PKG:-./internal/...}"
MUTATION_OUTPUT="${MUTATION_OUTPUT:-mutation-report.json}"

if [ "$MODE" = "diff" ]; then
	# A failed git command also gives no output. Keep its exit status, or an
	# unknown reference makes the run stop with success and test nothing.
	if ! changed="$(git diff --merge-base --name-only "$MUTATION_REF" -- '*.go')"; then
		echo "Cannot compare the code with $MUTATION_REF." >&2
		exit 1
	fi

	if [ -z "$changed" ]; then
		echo "No Go file differs from $MUTATION_REF. Mutation testing is not necessary."
		exit 0
	fi
fi

if ! command -v gremlins >/dev/null; then
	echo "gremlins is not installed. Install it with:" >&2
	echo "  go install github.com/go-gremlins/gremlins/cmd/gremlins@$GREMLINS_VERSION" >&2
	exit 1
fi

# gremlins copies the whole module for every worker. On some machines /tmp is a
# small tmpfs, and a full run fills it.
TMPDIR="${MUTATION_TMPDIR:-$HOME/.cache/mutants-tmp}"
export TMPDIR
mkdir -p "$TMPDIR"

args=(unleash --output "$MUTATION_OUTPUT")
if [ "$MODE" = "diff" ]; then
	args+=(--diff "$MUTATION_REF")
fi

exec gremlins "${args[@]}" "$PKG"
