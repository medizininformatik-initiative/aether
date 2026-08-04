#!/bin/bash
set -euo pipefail

# Downloads the flattening.zip asset of the pinned fhir-ontology-generator
# release and extracts it into ./flattening-lookup.
#
# Env: FLATTENING_VERSION  release tag, e.g. v4.2.0
#      FLATTENING_CHECKSUM sha256 of flattening.zip

url="https://github.com/medizininformatik-initiative/fhir-ontology-generator/releases/download/${FLATTENING_VERSION}/flattening.zip"
curl -sSfL --retry 3 "${url}" >flattening.zip
echo "${FLATTENING_CHECKSUM} flattening.zip" | sha256sum -c

unzip -o -q flattening.zip -d flattening-lookup
rm flattening.zip
