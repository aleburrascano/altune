#!/usr/bin/env python3
"""Migrate audio files from block volume to OCI Object Storage via boto3.

OCI Object Storage doesn't support AWS chunked transfer encoding, so we use
put_object with raw bytes and disable chunked encoding via config.

Usage:
    export OCI_S3_ENDPOINT=https://<ns>.compat.objectstorage.<region>.oraclecloud.com
    export OCI_S3_ACCESS_KEY=<key>
    export OCI_S3_SECRET_KEY=<secret>
    export OCI_S3_BUCKET=altune-audio
    export OCI_S3_REGION=<region>
    python3 migrate-audio-boto3.py [source-dir]
"""
import os
import sys

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError

ENDPOINT = os.environ["OCI_S3_ENDPOINT"]
ACCESS_KEY = os.environ["OCI_S3_ACCESS_KEY"]
SECRET_KEY = os.environ["OCI_S3_SECRET_KEY"]
BUCKET = os.environ["OCI_S3_BUCKET"]
REGION = os.environ["OCI_S3_REGION"]
SOURCE_DIR = sys.argv[1] if len(sys.argv) > 1 else "/mnt/music/aleburrascano123@gmail.com"

s3 = boto3.client(
    "s3",
    endpoint_url=ENDPOINT,
    aws_access_key_id=ACCESS_KEY,
    aws_secret_access_key=SECRET_KEY,
    region_name=REGION,
    config=Config(
        s3={"payload_signing_enabled": True},
        signature_version="s3v4",
        request_checksum_calculation="when_required",
        response_checksum_validation="when_required",
    ),
)

uploaded = 0
skipped = 0
failed = 0
files = []

for root, dirs, filenames in os.walk(SOURCE_DIR):
    for f in filenames:
        if f.lower().endswith(".mp3"):
            files.append(os.path.join(root, f))

total = len(files)
print(f"=== OCI Object Storage Migration (boto3) ===")
print(f"Bucket:  {BUCKET}")
print(f"Source:  {SOURCE_DIR}")
print(f"Total:   {total}")
print(f"=============================================\n")

for filepath in files:
    key = os.path.relpath(filepath, SOURCE_DIR)
    progress = uploaded + skipped + failed + 1

    try:
        s3.head_object(Bucket=BUCKET, Key=key)
        skipped += 1
        print(f"SKIP [{progress}/{total}] {key}")
        continue
    except ClientError:
        pass

    try:
        with open(filepath, "rb") as f:
            s3.put_object(
                Bucket=BUCKET,
                Key=key,
                Body=f,
                ContentType="audio/mpeg",
            )
        uploaded += 1
        print(f" OK  [{progress}/{total}] {key}")
    except Exception as e:
        failed += 1
        print(f"FAIL [{progress}/{total}] {key} — {e}")

print(f"\n=== Migration Complete ===")
print(f"Uploaded: {uploaded}")
print(f"Skipped:  {skipped}")
print(f"Failed:   {failed}")
print(f"=========================")

if failed > 0:
    sys.exit(1)
