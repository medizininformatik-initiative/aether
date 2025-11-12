#!/bin/sh

# Usage: install.sh <version>

set -e

repo="medizininformatik-initiative/aether"

fetch_latest_version() {
  tag=$(curl -sD - "https://github.com/$repo/releases/latest" | grep location | tr -d '\r' | cut -d/ -f8)
  echo "${tag#v}"
}

version="${1:-$(fetch_latest_version)}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')

arch=$(uname -m)
case $arch in
  x86_64) arch="amd64" ;;
  aarch64) arch="arm64" ;;
esac

archive_filename="aether-$version-$os-$arch.tar.gz"

echo "Download $archive_filename..."
curl -sSfLO "https://github.com/$repo/releases/download/v$version/$archive_filename"

tar xzf "$archive_filename"
rm "$archive_filename"

if command -v gh > /dev/null
then
  echo "Verify aether binary..."
  gh attestation verify --repo "$repo" --predicate-type https://spdx.dev/Document/v2.3 aether
else
  echo "Skip aether binary verification. Please install the GitHub CLI tool from https://github.com/cli/cli."
fi

echo "Please use \`sudo mv ./aether /usr/local/bin/aether\` to move aether into PATH"
