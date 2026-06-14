# oci-object-storage-v1 — implementation plan

Spec: docs/specs/oci-object-storage-v1/spec.md

## Slices

### Slice 1: Add boto3 dependency
- Acceptance criterion: prerequisite for all slices
- Files:
  - `services/api/pyproject.toml` (add `boto3` to dependencies)
  - `services/api/pyproject.toml` mypy overrides (add `boto3.*` to ignore_missing_imports)
- Failing test first: N/A — dependency management, no behavior to test
- Verify: `cd services/api && uv sync --all-extras && uv run python -c "import boto3; print(boto3.__version__)"`

### Slice 2: Add OCI S3 config fields to Settings
- Acceptance criterion: AC#4 (config env vars recognized by app)
- Files:
  - `services/api/src/altune/platform/config.py` (add `oci_s3_endpoint`, `oci_s3_access_key`, `oci_s3_secret_key`, `oci_s3_bucket`, `oci_s3_region`)
  - `services/api/.env.example` (add commented OCI S3 vars)
- Failing test first: `test_settings_loads_oci_s3_config` in `services/api/tests/unit/altune/platform/test_config.py`
- Verify: `cd services/api && uv run pytest tests/unit/altune/platform/test_config.py -v -k oci`

### Slice 3: Extend AudioStore port with `stream()` method
- Acceptance criterion: AC#1 (streaming without temp files — port contract)
- Files:
  - `services/api/src/altune/application/catalog/ports.py` (add `stream` method to `AudioStore` Protocol)
  - `services/api/tests/_doubles/fake_audio_store.py` (add `stream` to FakeAudioStore)
- Failing test first: `test_fake_audio_store_stream_returns_chunks` in `services/api/tests/unit/altune/application/catalog/test_audio_store_port.py`
- Verify: `cd services/api && uv run pytest tests/unit/altune/application/catalog/test_audio_store_port.py -v`

### Slice 4: Implement ObjectStorageAudioStore adapter
- Acceptance criterion: AC#1, AC#2, AC#3 (store, exists, stream)
- Files:
  - `services/api/src/altune/adapters/outbound/audio/object_storage_store.py` (new)
- Failing test first: `test_object_storage_store_put_and_exists` in `services/api/tests/unit/altune/adapters/outbound/audio/test_object_storage_store.py`
- Verify: `cd services/api && uv run pytest tests/unit/altune/adapters/outbound/audio/test_object_storage_store.py -v`

### Slice 5: Wire ObjectStorageAudioStore in app startup
- Acceptance criterion: AC#4, AC#8 (wiring priority: OCI S3 > SSH > Filesystem)
- Files:
  - `services/api/src/altune/platform/app.py` (add OCI S3 branch before SSH branch)
- Failing test first: `test_app_wires_object_storage_store_when_oci_config_set` in `services/api/tests/unit/altune/platform/test_app_wiring.py`
- Verify: `cd services/api && uv run pytest tests/unit/altune/platform/test_app_wiring.py -v -k oci`

### Slice 6: Update streaming endpoint to use StreamingResponse
- Acceptance criterion: AC#1 (stream from Object Storage without temp file), AC#7 (reconcile on missing)
- Files:
  - `services/api/src/altune/adapters/inbound/http/catalog/router.py` (stream_audio handler — try stream() first, fall back to resolve_local_path)
- Failing test first: `test_stream_audio_uses_streaming_response_when_stream_available` in `services/api/tests/e2e/test_audio_stream.py`
- Verify: `cd services/api && uv run pytest tests/e2e/test_audio_stream.py -v`

### Slice 7: Write migration script
- Acceptance criterion: AC#5, AC#6 (migrate 1,595 files, counts match)
- Files:
  - `scripts/migrate-audio-to-oci.sh` (new — OCI CLI script to run on the instance)
- Failing test first: N/A — one-time ops script run on remote instance, not testable in CI
- Verify: manual — run on OCI instance, compare `oci os object list --bucket-name altune-audio | wc -l` with DB count

### Slice 8: Update .env.example and documentation
- Acceptance criterion: AC#4 (config documented)
- Files:
  - `services/api/.env.example` (if not already updated in slice 2)
  - `docs/specs/oci-object-storage-v1/spec.md` (status → Shipped)
- Failing test first: N/A — documentation only
- Verify: visual review

## Risks

- **boto3 sync in async context** [vault: wiki/concepts/Hexagonal Architecture.md anti-pattern: "overly thin adapters"] — boto3 is synchronous. Every S3 call wraps in `asyncio.to_thread`. The adapter is not thin — it handles client setup, streaming chunking, thread-pool wrapping, and error translation. Acceptable overhead given the alternative (temp file + SCP).
- **OCI S3 compatibility** — test `put_object`, `get_object`, `head_object` manually against the real bucket before relying on CI. Add `boto3.*` to mypy ignore list (no stubs).
- **Chunk size** — default to 64KB; adjustable via constant. Benchmark later if needed.

## ADR candidates

- None — this is a straightforward adapter swap within existing architecture. boto3 is a well-known dependency, not an architectural decision.
