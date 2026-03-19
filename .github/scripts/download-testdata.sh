#!/usr/bin/env bash
set -euo pipefail

# Download test data archive from cloud storage and verify checksums
# Usage: ./download-testdata.sh [SHARE_TOKEN]
#
# Arguments:
#   SHARE_TOKEN: The share token for the cloud storage (default: KkjxbpzaQABkNKB)

BASE_DIR="torch"
BASE_URL="https://speicherwolke.uni-leipzig.de/index.php/s"
SHARE="${1:-KkjxbpzaQABkNKB}"
ARCHIVE_NAME="testdata.tar.gz"

# Target directory for test data
DATA_DIR="${BASE_DIR}/testdata"
TEMP_ARCHIVE="${BASE_DIR}/${ARCHIVE_NAME}"

echo "Downloading test data archive to ${DATA_DIR}"

# Download the tar.gz archive using direct share link
echo "Downloading ${ARCHIVE_NAME}..."
if curl -sSfL --retry 3 "${BASE_URL}/${SHARE}/download" -o "${TEMP_ARCHIVE}"; then
    echo "Archive downloaded successfully"
else
    echo "Error: Could not download ${ARCHIVE_NAME}"
    exit 1
fi

# Create testdata directory if it doesn't exist
mkdir -p "${DATA_DIR}"

# Extract the archive
echo "Extracting archive..."
if tar -xzf "${TEMP_ARCHIVE}" -C "${BASE_DIR}"; then
    echo "Archive extracted successfully"
else
    echo "Error: Failed to extract archive"
    rm -f "${TEMP_ARCHIVE}"
    exit 1
fi

# Remove the temporary archive
rm -f "${TEMP_ARCHIVE}"

# Verify checksums if checksums file exists
if [[ -f "${DATA_DIR}/checksums.sha256" ]]; then
    echo "Verifying checksums..."
    cd "${DATA_DIR}"
    if sha256sum -c checksums.sha256 --ignore-missing; then
        echo "All checksums verified successfully"
    else
        echo "Warning: Checksum verification failed!"
        exit 1
    fi
else
    echo "No checksums.sha256 file found in archive, skipping verification"
fi

echo "Download finished successfully"
