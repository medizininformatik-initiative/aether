#!/bin/bash
set -euo pipefail

VERSION="${VALE_VERSION#v}"
CHECKSUM="${VALE_CHECKSUM}"

url="https://github.com/errata-ai/vale/releases/download/v${VERSION}/vale_${VERSION}_Linux_64-bit.tar.gz"
curl -sSfL "${url}" >vale.tar.gz
echo "${CHECKSUM} vale.tar.gz" | sha256sum -c

tar -xzf vale.tar.gz vale
mkdir -p ~/.local/bin/
mv vale ~/.local/bin/vale
