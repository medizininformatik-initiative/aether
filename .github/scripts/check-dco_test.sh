#!/usr/bin/env bash
# Tests for check-dco.sh. Each test builds a throwaway git repository, so the
# script is exercised through its real interface: a commit range in a real repo.
#
# Usage: .github/scripts/check-dco_test.sh
set -uo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-dco.sh"
failures=0

# Creates an empty repository with a deterministic identity and prints its path.
new_repo() {
  local dir
  dir="$(mktemp -d)"
  git -C "$dir" init --quiet --initial-branch=main
  git -C "$dir" config user.name "Ada Lovelace"
  git -C "$dir" config user.email "ada@example.com"
  git -C "$dir" commit --quiet --allow-empty -m "base"
  printf '%s' "$dir"
}

# commit <repo> <message> [author-name] [author-email]
commit() {
  local repo="$1" message="$2" name="${3:-Ada Lovelace}" email="${4:-ada@example.com}"
  git -C "$repo" -c user.name="$name" -c user.email="$email" \
    commit --quiet --allow-empty -m "$message" --author "$name <$email>"
}

# run_check <repo> <base-ref> -> sets OUTPUT and STATUS
run_check() {
  local repo="$1" base="$2"
  OUTPUT="$(cd "$repo" && "$SCRIPT" "$(git -C "$repo" rev-parse "$base")" "$(git -C "$repo" rev-parse HEAD)" 2>&1)"
  STATUS=$?
}

# check <name> <expected-status> <repo> <base-ref>
check() {
  local name="$1" expected="$2" repo="$3" base="$4"
  run_check "$repo" "$base"
  if [ "$STATUS" -eq "$expected" ]; then
    echo "PASS: $name"
  else
    echo "FAIL: $name (expected exit $expected, got $STATUS)"
    printf '%s\n' "$OUTPUT" | sed 's/^/      /'
    failures=$((failures + 1))
  fi
}

# --- behaviour 1: a commit signed off by its author passes -------------------
test_signed_commit_passes() {
  local repo
  repo="$(new_repo)"
  commit "$repo" "feat: a change

Signed-off-by: Ada Lovelace <ada@example.com>"
  check "signed commit passes" 0 "$repo" HEAD~1
  rm -rf "$repo"
}

test_signed_commit_passes

# assert_output_contains <name> <needle>  (uses OUTPUT from the last run_check)
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

# --- behaviour 2: a commit with no sign-off fails and is named ---------------
test_unsigned_commit_fails() {
  local repo short
  repo="$(new_repo)"
  commit "$repo" "feat: no sign-off here"
  check "unsigned commit fails" 1 "$repo" HEAD~1
  short="$(git -C "$repo" rev-parse --short HEAD)"
  assert_output_contains "unsigned commit is named" "$short"
  assert_output_contains "unsigned commit shows subject" "feat: no sign-off here"
  assert_output_contains "failure explains the fix" "--signoff"
  rm -rf "$repo"
}

test_unsigned_commit_fails

# --- behaviour 3: a sign-off by someone other than the author fails ----------
test_mismatched_signoff_fails() {
  local repo
  repo="$(new_repo)"
  commit "$repo" "feat: signed by the wrong person

Signed-off-by: Grace Hopper <grace@example.com>"
  check "sign-off not matching the author fails" 1 "$repo" HEAD~1
  rm -rf "$repo"
}

test_mismatched_signoff_fails

# --- behaviour 4: merge commits carry no sign-off and are ignored ------------
# GitHub generates the merge commit, so no human can sign it off.
test_merge_commit_ignored() {
  local repo base
  repo="$(new_repo)"
  base="$(git -C "$repo" rev-parse HEAD)"
  git -C "$repo" checkout --quiet -b side
  commit "$repo" "feat: side work

Signed-off-by: Ada Lovelace <ada@example.com>"
  git -C "$repo" checkout --quiet main
  commit "$repo" "feat: main work

Signed-off-by: Ada Lovelace <ada@example.com>"
  git -C "$repo" -c user.name="Ada Lovelace" -c user.email="ada@example.com" \
    merge --quiet --no-ff -m "Merge branch 'side'" side
  check "merge commit without sign-off is ignored" 0 "$repo" "$base"
  rm -rf "$repo"
}

test_merge_commit_ignored

# --- behaviour 5: bot-authored commits are exempt ----------------------------
# Only a human can certify the DCO, so a bot signing itself off would certify
# nothing. Renovate automerges dependency updates and must not be blocked.
test_bot_commit_skipped() {
  local repo
  repo="$(new_repo)"
  commit "$repo" "chore(deps): update something" \
    "renovate[bot]" "29139614+renovate[bot]@users.noreply.github.com"
  check "renovate commit without sign-off is skipped" 0 "$repo" HEAD~1
  rm -rf "$repo"
}

test_bot_commit_skipped

# A human commit must not be exempt just because the bot list exists.
test_human_still_checked_alongside_bots() {
  local repo base
  repo="$(new_repo)"
  base="$(git -C "$repo" rev-parse HEAD)"
  commit "$repo" "chore(deps): update something" \
    "dependabot[bot]" "49699333+dependabot[bot]@users.noreply.github.com"
  commit "$repo" "feat: human change with no sign-off"
  check "human commit still fails next to a bot commit" 1 "$repo" "$base"
  rm -rf "$repo"
}

test_human_still_checked_alongside_bots

# The exemption must key on the bot's full identity. A human who sets only
# user.name to a bot name must not slip past the check.
test_bot_name_alone_is_not_exempt() {
  local repo
  repo="$(new_repo)"
  commit "$repo" "feat: pretending to be a bot" \
    "renovate[bot]" "attacker@example.com"
  check "bot name with a foreign email is not exempt" 1 "$repo" HEAD~1
  rm -rf "$repo"
}

test_bot_name_alone_is_not_exempt

# --- behaviour 6: every offender is reported, not only the first -------------
test_all_offenders_reported() {
  local repo base first
  repo="$(new_repo)"
  base="$(git -C "$repo" rev-parse HEAD)"
  commit "$repo" "feat: first unsigned"
  first="$(git -C "$repo" rev-parse --short HEAD)"
  commit "$repo" "feat: second unsigned"
  check "several unsigned commits fail" 1 "$repo" "$base"
  assert_output_contains "first offender reported" "$first"
  assert_output_contains "second offender reported" "feat: second unsigned"
  rm -rf "$repo"
}

test_all_offenders_reported

# The common real case: one commit signed, a later one missed after a rebase.
# Only the offender may be reported.
test_mixed_range_reports_only_the_offender() {
  local repo base offender
  repo="$(new_repo)"
  base="$(git -C "$repo" rev-parse HEAD)"
  commit "$repo" "feat: properly signed

Signed-off-by: Ada Lovelace <ada@example.com>"
  commit "$repo" "feat: missed the sign-off"
  offender="$(git -C "$repo" rev-parse --short HEAD)"
  check "mixed range fails" 1 "$repo" "$base"
  assert_output_contains "offender reported in mixed range" "$offender"
  if printf '%s' "$OUTPUT" | grep -qF -- "feat: properly signed"; then
    echo "FAIL: signed commit must not be reported as an offender"
    printf '%s\n' "$OUTPUT" | sed 's/^/      /'
    failures=$((failures + 1))
  else
    echo "PASS: signed commit not reported as an offender"
  fi
  rm -rf "$repo"
}

test_mixed_range_reports_only_the_offender

# --- behaviour 7: an empty range passes --------------------------------------
test_empty_range_passes() {
  local repo
  repo="$(new_repo)"
  check "empty range passes" 0 "$repo" HEAD
  rm -rf "$repo"
}

test_empty_range_passes

# --- behaviour 8: an unresolvable range fails closed -------------------------
# A wrong base sha (e.g. a shallow clone) must not be reported as "no offenders".
test_bad_range_fails() {
  local repo
  repo="$(new_repo)"
  OUTPUT="$(cd "$repo" && "$SCRIPT" 0000000000000000000000000000000000000000 HEAD 2>&1)"
  STATUS=$?
  if [ "$STATUS" -ne 0 ]; then
    echo "PASS: unresolvable range fails closed"
  else
    echo "FAIL: unresolvable range fails closed (expected non-zero, got $STATUS)"
    failures=$((failures + 1))
  fi
  rm -rf "$repo"
}

test_bad_range_fails

if [ "$failures" -gt 0 ]; then
  echo "$failures test(s) failed"
  exit 1
fi
echo "all tests passed"
