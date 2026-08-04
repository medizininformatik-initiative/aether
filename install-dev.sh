#!/bin/sh

# Usage: install-dev.sh [<pr-number>|main]
#
# Installs an unsigned development build from GitHub Actions.
# Use install.sh instead to install a release.

set -e

repo="medizininformatik-initiative/aether"
ref="${1:-main}"

case $ref in
  main) ;;
  *[!0-9]*)
    echo "Usage: install-dev.sh [<pr-number>|main]" >&2
    exit 1
    ;;
esac

if ! command -v gh > /dev/null; then
  echo "Error: GitHub CLI (gh) is required to download workflow artifacts." >&2
  echo "Install it from https://github.com/cli/cli" >&2
  exit 1
fi

if ! gh auth status > /dev/null 2>&1; then
  echo "Error: GitHub CLI is not authenticated. Run \`gh auth login\`." >&2
  exit 1
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $os in
  mingw* | msys* | cygwin*) os="windows" ;;
esac

arch=$(uname -m)
case $arch in
  x86_64) arch="amd64" ;;
  aarch64) arch="arm64" ;;
esac

binary_name="aether-$os-$arch"
target="aether"
if [ "$os" = "windows" ]; then
  binary_name="$binary_name.exe"
  target="aether.exe"
fi

artifact_exists() {
  [ -n "$(gh api "/repos/$repo/actions/artifacts?name=$1" \
    --jq '[.artifacts[] | select(.expired == false)] | .[0].id // empty')" ]
}

if [ "$ref" = "main" ]; then
  source_description="main"
  run_id=""
  # The newest run gives no artifact if its build job failed or is still active.
  for candidate in $(gh run list --repo "$repo" --workflow CI --branch main \
    --event push --limit 10 --json databaseId --jq '.[].databaseId'); do
    if artifact_exists "build-artifacts-$candidate"; then
      run_id="$candidate"
      break
    fi
  done
  artifact_name="build-artifacts-$run_id"
else
  source_description="pull request $ref"
  artifact_name="build-artifacts-pr-$ref"
  # The artifact carries its run, so a run that is still in progress also works.
  run_id=$(gh api "/repos/$repo/actions/artifacts?name=$artifact_name" \
    --jq '[.artifacts[] | select(.expired == false)] | .[0].workflow_run.id // empty')
fi

if [ -z "$run_id" ]; then
  echo "Error: no build artifact found for $source_description." >&2
  echo "The build job is possibly still in progress or it failed." >&2
  echo "An artifact also expires after 14 days, and closing a pull request deletes it." >&2
  exit 1
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

echo "Warning: this is an unsigned development build. Do not use it in production."
echo "Download $artifact_name from run $run_id ($source_description)..."

gh run download "$run_id" --repo "$repo" --name "$artifact_name" --dir "$tmp_dir"

if [ ! -f "$tmp_dir/$binary_name" ]; then
  echo "Error: the artifact does not contain $binary_name." >&2
  exit 1
fi

mv "$tmp_dir/$binary_name" "./$target"
chmod +x "./$target"

echo "Please use \`sudo mv ./$target /usr/local/bin/$target\` to move aether into PATH"
