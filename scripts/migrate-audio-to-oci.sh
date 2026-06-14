#!/usr/bin/env bash
# migrate-audio-to-oci.sh — one-time migration of audio files from block volume
# to OCI Object Storage. Run ON the OCI instance (151.145.41.81).
#
# Prerequisites:
#   - OCI CLI installed and configured (`oci setup config`)
#   - Object Storage bucket created (e.g., altune-audio)
#   - Run: chmod +x migrate-audio-to-oci.sh
#
# Usage:
#   ./migrate-audio-to-oci.sh <bucket-name> <namespace> [source-dir]
#
# Example:
#   ./migrate-audio-to-oci.sh altune-audio <your-namespace> /mnt/music/aleburrascano123@gmail.com

set -euo pipefail

BUCKET="${1:?Usage: $0 <bucket-name> <namespace> [source-dir]}"
NAMESPACE="${2:?Usage: $0 <bucket-name> <namespace> [source-dir]}"
SOURCE_DIR="${3:-/mnt/music/aleburrascano123@gmail.com}"

if [ ! -d "$SOURCE_DIR" ]; then
    echo "ERROR: Source directory does not exist: $SOURCE_DIR"
    exit 1
fi

UPLOADED=0
SKIPPED=0
FAILED=0
TOTAL=$(find "$SOURCE_DIR" -type f -name "*.mp3" | wc -l)

echo "=== OCI Object Storage Migration ==="
echo "Bucket:     $BUCKET"
echo "Namespace:  $NAMESPACE"
echo "Source:     $SOURCE_DIR"
echo "Total MP3s: $TOTAL"
echo "==================================="
echo ""

find "$SOURCE_DIR" -type f -name "*.mp3" | while read -r filepath; do
    # Strip source dir prefix to get the object key (matches audio_ref in DB)
    object_key="${filepath#"$SOURCE_DIR"/}"

    # Check if object already exists (idempotent re-runs)
    if oci os object head \
        --bucket-name "$BUCKET" \
        --namespace-name "$NAMESPACE" \
        --name "$object_key" \
        >/dev/null 2>&1; then
        SKIPPED=$((SKIPPED + 1))
        echo "SKIP [$((UPLOADED + SKIPPED + FAILED))/$TOTAL] $object_key (already exists)"
        continue
    fi

    # Upload
    if oci os object put \
        --bucket-name "$BUCKET" \
        --namespace-name "$NAMESPACE" \
        --name "$object_key" \
        --file "$filepath" \
        --content-type "audio/mpeg" \
        --no-multipart \
        --force \
        >/dev/null 2>&1; then
        UPLOADED=$((UPLOADED + 1))
        echo " OK  [$((UPLOADED + SKIPPED + FAILED))/$TOTAL] $object_key"
    else
        FAILED=$((FAILED + 1))
        echo "FAIL [$((UPLOADED + SKIPPED + FAILED))/$TOTAL] $object_key"
    fi
done

echo ""
echo "=== Migration Complete ==="
echo "Uploaded: $UPLOADED"
echo "Skipped:  $SKIPPED"
echo "Failed:   $FAILED"
echo "========================="

if [ "$FAILED" -gt 0 ]; then
    echo "WARNING: $FAILED files failed to upload. Re-run to retry."
    exit 1
fi
