#!/bin/bash
# Run gremlins mutation testing. The first argument selects the mode:
# "diff" tests only the lines that differ from MUTATION_REF, anything else
# tests every mutant.

set -euo pipefail

GREMLINS_VERSION="v0.6.0"

MODE="${1:-full}"
MUTATION_REF="${MUTATION_REF:-origin/main}"
PKG="${PKG:-./internal}"
MUTATION_OUTPUT="${MUTATION_OUTPUT:-mutation-report.json}"

# gremlins takes a directory. With a Go package pattern such as ./internal/...
# it finds no mutants.
if [ ! -d "$PKG" ]; then
	echo "PKG must be a directory, for example ./internal. The value is $PKG." >&2
	exit 1
fi

if [ "$MODE" = "diff" ]; then
	# A failed git command also gives no output. Keep its exit status, or an
	# unknown reference makes the run stop with success and test nothing.
	# gremlins mutates no test file, no deleted file and nothing outside PKG.
	if ! changed="$(git diff --merge-base --name-only --diff-filter=d "$MUTATION_REF" -- \
		"${PKG%/}/*.go" ':(exclude)*_test.go')"; then
		echo "Cannot compare the code with $MUTATION_REF." >&2
		exit 1
	fi

	if [ -z "$changed" ]; then
		echo "No Go file in $PKG differs from $MUTATION_REF. Mutation testing is not necessary."
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
target="$PKG"
if [ "$MODE" = "diff" ]; then
	args+=(--diff "$MUTATION_REF")
	# gremlins compares the paths of the diff, which start at the module root,
	# with paths that start at the given directory. With a subdirectory, no path
	# matches and gremlins skips every mutant.
	target="."
	# From the module root, gremlins mutates the changed lines in all
	# directories. Exclude the changed files outside PKG.
	outside="$(git diff --merge-base --name-only "$MUTATION_REF" -- \
		'*.go' ':(exclude)*_test.go' ":(exclude)${PKG%/}")"
	while IFS= read -r file; do
		[ -n "$file" ] || continue
		args+=(--exclude-files "^$(printf '%s' "$file" | sed 's/[][\\.*^$+?(){}|]/\\&/g')\$")
	done <<<"$outside"
fi

# If gremlins finds no mutants, it writes no report and stops with success.
# Remove an old report, so that its absence shows this case.
rm -f "$MUTATION_OUTPUT"
status=0
gremlins "${args[@]}" "$target" || status=$?

if [ "$status" -ne 0 ] && [ ! -f "$MUTATION_OUTPUT" ]; then
	exit "$status"
fi
if [ ! -f "$MUTATION_OUTPUT" ]; then
	echo "gremlins found no mutants in $PKG." >&2
	exit 1
fi

# gremlins stops with this status when the test efficacy is not above the
# threshold. Without a killed or lived mutant, the efficacy is 0. If no mutant
# has a test result, the change has nothing to test, for example a comment.
EFFICACY_THRESHOLD_EXIT=10
if [ "$status" -eq "$EFFICACY_THRESHOLD_EXIT" ] &&
	! grep -Eq '"mutants_(killed|lived|not_covered)":[1-9]' "$MUTATION_OUTPUT"; then
	echo "No mutant on the changed lines has a test result. Mutation testing is not necessary."
	exit 0
fi
exit "$status"
