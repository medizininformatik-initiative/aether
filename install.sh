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
curl -sSfLO "https://github.com/$repo/releases/download/v$version/$archive_filename.sha256"

echo "Verify checksum..."
if command -v sha256sum > /dev/null; then
  sha256sum -c "$archive_filename.sha256"
elif command -v shasum > /dev/null; then
  shasum -a 256 -c "$archive_filename.sha256"
else
  echo "Warning: No checksum tool found, skipping verification"
fi

if command -v gh > /dev/null; then
  echo "Verify GitHub attestation..."
  gh attestation verify --repo "$repo" "$archive_filename"
else
  echo "Skipping attestation verification. Install GitHub CLI for additional security: https://github.com/cli/cli"
fi

tar xzf "$archive_filename"
rm "$archive_filename" "$archive_filename.sha256"

binary_name="aether-$os-$arch"
mv "$binary_name" aether

echo "Please use \`sudo mv ./aether /usr/local/bin/aether\` to move aether into PATH"
