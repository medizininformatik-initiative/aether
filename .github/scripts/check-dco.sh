#!/usr/bin/env bash
# Verifies that every commit in a range carries a Signed-off-by trailer that
# matches its author (Developer Certificate of Origin).
#
# Usage: check-dco.sh <base-sha> <head-sha>
set -euo pipefail

base="${1:?usage: check-dco.sh <base-sha> <head-sha>}"
head="${2:?usage: check-dco.sh <base-sha> <head-sha>}"

status=0

# Bots cannot certify the DCO, so their commits are exempt rather than
# self-signed. Renovate automerges dependency updates and must not be blocked.
#
# The match is on the full identity, not the name alone: a name on its own is
# free to set, so `git config user.name 'renovate[bot]'` would otherwise buy an
# exemption. Matching the noreply address as well raises the bar, but it does
# not make the exemption unforgeable — a determined author can set both fields.
# Treat this as a guard against accident, not against a hostile committer.
is_bot() {
  case "$1" in
    'renovate[bot] <29139614+renovate[bot]@users.noreply.github.com>') return 0 ;;
    'dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>') return 0 ;;
    'github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com>') return 0 ;;
    *) return 1 ;;
  esac
}

# Resolved up front so an unresolvable range aborts instead of looking like a
# clean run. A shallow clone is the usual cause; the workflow uses fetch-depth 0.
if ! commits="$(git rev-list --no-merges "$base..$head" 2>&1)"; then
  printf 'check-dco: cannot resolve range %s..%s\n%s\n' "$base" "$head" "$commits" >&2
  exit 2
fi

while read -r sha; do
  [ -n "$sha" ] || continue
  author="$(git log -1 --format='%an <%ae>' "$sha")"
  if is_bot "$author"; then
    continue
  fi
  expected="Signed-off-by: $author"
  if ! git log -1 --format='%B' "$sha" | grep -qxF "$expected"; then
    if [ "$status" -eq 0 ]; then
      echo "Commits without a valid sign-off:" >&2
    fi
    printf '  %s %s\n' "$(git rev-parse --short "$sha")" "$(git log -1 --format='%s' "$sha")" >&2
    printf '    missing: %s\n' "$expected" >&2
    status=1
  fi
done <<< "$commits"

if [ "$status" -ne 0 ]; then
  cat >&2 <<'EOF'

Every commit needs a Signed-off-by trailer that matches its author.
See https://developercertificate.org/ and CONTRIBUTING.md.

To fix the most recent commit:
  git commit --amend --signoff

To fix every commit on this branch:
  git rebase --signoff origin/main

Then force-push with:
  git push --force-with-lease
EOF
fi

exit "$status"
