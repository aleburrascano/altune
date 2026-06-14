# OCI Object Storage Migration

> Spec for `oci-object-storage-v1` — version 1, drafted 2026-06-13.
> Authors: solo + Claude.
> Status: Ready-for-plan.

## Problem

Audio streaming is slow and fragile. The current `SshAudioStore` adapter fetches files via SCP from a block volume on the OCI instance — every stream request pays SSH connection setup + full file download to a temp directory before a single byte reaches the mobile client. This double-hop (client → API → SCP → temp file → response) adds seconds of latency per track and depends on a persistent SSH connection to a single compute instance.

## User value

Tracks start playing faster. Audio streaming is more reliable (HTTPS from managed infrastructure vs. SCP from a single VM). The block volume and its mount/NFS complexity can be retired.

## Scope tier / MVP cut

- **Minimal (ship this):** Replace `SshAudioStore` with `ObjectStorageAudioStore` backed by an OCI Object Storage bucket via boto3 (S3-compatible API). Migrate existing files. Stream audio bytes directly from Object Storage through the API (streaming proxy — no temp files). Same endpoint, zero client changes.
- **Deferred to post-launch:** Pre-signed URL direct streaming (bypasses API entirely), CDN layer, multi-region replication, storage lifecycle policies, content-addressed dedup with reference counting.
- **Justified exceptions:** None.

The Acceptance criteria below cover the **minimal tier only**.

## Acceptance criteria

1. **AC#1** — Given a track with `acquisition_status == READY` and a valid `audio_ref`, when the client requests `GET /v1/tracks/{id}/audio`, then audio bytes stream from OCI Object Storage through the API via `StreamingResponse` with `Content-Type: audio/mpeg` and no intermediate temp file is written to disk.

2. **AC#2** — Given the `ObjectStorageAudioStore` adapter is configured, when the acquisition pipeline's `StoreStep` calls `store(source_path, audio_ref)`, then the file is uploaded to the OCI Object Storage bucket with `audio_ref` as the object key and `Content-Type: audio/mpeg`.

3. **AC#3** — Given an `audio_ref` pointing to an object in the bucket, when `exists(audio_ref)` is called, then it returns `True` (via S3 `head_object`). When the object does not exist, it returns `False`.

4. **AC#4** — Given the OCI S3 config env vars (`OCI_S3_ENDPOINT`, `OCI_S3_ACCESS_KEY`, `OCI_S3_SECRET_KEY`, `OCI_S3_BUCKET`, `OCI_S3_REGION`) are set, when the app starts, then `ObjectStorageAudioStore` is wired as the active `AudioStore` and takes priority over SSH and filesystem stores.

5. **AC#5** — Given the 1,595 existing MP3 files on the OCI instance at `/mnt/music/aleburrascano123@gmail.com/`, when the migration script runs, then each file is uploaded to the bucket with its relative path (minus the email prefix) as the object key, matching the existing `audio_ref` values in the database.

6. **AC#6** — Given the migration is complete, when comparing bucket object count to DB tracks where `audio_ref IS NOT NULL`, then counts match.

7. **AC#7** — Given the `ObjectStorageAudioStore` is active and a track's `audio_ref` points to a missing object, when the stream endpoint is called, then `ReconcileTrackStatus` marks the track as `FAILED` with reason "Audio file missing from storage" — same behavior as the current SSH store.

8. **AC#8** — Given OCI S3 config is not set but SSH config is, when the app starts, then `SshAudioStore` is wired as fallback (rollback path).

## Out of scope

- Pre-signed URL direct streaming (client bypasses API) — deferred; current design keeps API as auth gatekeeper.
- Go microservices migration — separate effort, parked.
- CDN or edge caching in front of the bucket.
- Multi-region bucket replication.
- Formal cross-user dedup with reference counting — implicit dedup via shared `audio_ref` paths is sufficient for now.
- Retiring the block volume or SSH store code — can happen after confidence is established.
- DB migration — `audio_ref` values remain unchanged.

## Design considerations

- [vault: wiki/concepts/Hexagonal Architecture.md] — the `AudioStore` protocol in `application/catalog/ports.py` is the adapter boundary. The new adapter implements the same port; no domain or application layer changes needed.
- [vault: wiki/concepts/Adapter Pattern.md] — `ObjectStorageAudioStore` is a new outbound adapter, same shape as `SshAudioStore` and `FilesystemAudioStore`.

High-level approach:

- This is a **read + write** path in the **catalog** bounded context.
- It **does not** require a new aggregate or value object.
- It **requires extending the `AudioStore` port** with an async `stream(audio_ref)` method that yields byte chunks, to support `StreamingResponse` without temp files. The existing `resolve_local_path` method remains for backward compatibility.
- It **introduces a new external dependency**: `boto3` for S3-compatible access to OCI Object Storage.

### Port extension

The `AudioStore` protocol gains one new method:

```python
async def stream(self, audio_ref: str) -> AsyncIterator[bytes] | None:
    """Yield chunks of audio bytes. Returns None if not found."""
```

- `ObjectStorageAudioStore` implements this by streaming the S3 `GetObject` response body.
- `SshAudioStore` and `FilesystemAudioStore` implement this by reading from their existing sources (optional — can raise `NotImplementedError` and fall through to `resolve_local_path`).
- The streaming endpoint in the catalog router checks for `stream()` first; falls back to `resolve_local_path()` + `FileResponse` if not available.

### Adapter internals

`ObjectStorageAudioStore` wraps a boto3 S3 client configured with:
- Custom endpoint URL (OCI's S3-compatible endpoint, e.g., `https://<namespace>.compat.objectstorage.<region>.oraclecloud.com`)
- Customer Secret Key credentials (access key + secret key, generated in OCI Console)
- Bucket name and region from config

Methods:
- `store()` — `put_object` with `ContentType=audio/mpeg`
- `exists()` — `head_object`, catch `ClientError` 404 → `False`
- `stream()` — `get_object`, yield `Body.read(chunk_size)` in an async generator (run in thread pool since boto3 is sync)
- `resolve_local_path()` — download to temp via `download_fileobj`, return path (backward compat for acquisition pipeline)

### Streaming endpoint change

The catalog router's `GET /v1/tracks/{id}/audio` handler:
1. Validates track exists, is READY, has `audio_ref` (unchanged)
2. Tries `audio_store.stream(audio_ref)` → if it returns an iterator, use `StreamingResponse`
3. Falls back to `audio_store.resolve_local_path(audio_ref)` → `FileResponse` (for SSH/filesystem stores)
4. On not-found from either path → reconcile + 404 (unchanged)

### Configuration

New fields in `platform/config.py` (Pydantic Settings):

```
OCI_S3_ENDPOINT    — S3-compatible endpoint URL
OCI_S3_ACCESS_KEY  — Customer Secret Key access key
OCI_S3_SECRET_KEY  — Customer Secret Key secret key
OCI_S3_BUCKET      — bucket name (e.g., "altune-audio")
OCI_S3_REGION      — OCI region (e.g., "eu-frankfurt-1")
```

Wiring priority in `app.py`: OCI S3 > SSH > Filesystem.

### Migration script

A standalone script run on the OCI instance (`151.145.41.81`):
1. Walk `/mnt/music/aleburrascano123@gmail.com/` recursively
2. For each `.mp3`, compute the object key by stripping the email prefix
3. Upload via OCI CLI (`oci os object put`) or boto3
4. Set `Content-Type: audio/mpeg`
5. Skip files already uploaded (idempotent via `head_object` check)
6. Log progress to stdout

### Cutover plan

1. Deploy new adapter code with SSH config still active
2. Run migration script — files exist in both locations
3. Switch env vars from SSH to OCI S3 config → app restarts with `ObjectStorageAudioStore`
4. Verify streaming for several tracks
5. Rollback: flip env vars back to SSH config

## Dependencies

- **Bounded contexts**: catalog (existing)
- **Other features**: None
- **External services**: OCI Object Storage bucket + Customer Secret Key credentials
- **Library/framework additions**: `boto3` (S3-compatible SDK)

## Risks / open questions

- **Risk**: boto3 is synchronous; wrapping in `asyncio.to_thread` adds overhead per stream request — mitigation: chunked streaming amortizes the thread-pool cost; benchmark against SCP baseline to confirm improvement.
- **Risk**: OCI S3-compatible endpoint may have subtle incompatibilities with boto3 — mitigation: test `put_object`, `get_object`, `head_object`, `delete_object` during bucket setup before writing adapter code.
- **Open question**: Optimal chunk size for streaming (8KB? 64KB? 256KB?) — to resolve via: benchmark on the OCI instance with realistic file sizes (average MP3 ~5-8MB).
- **Open question**: Should the migration script run as Python (boto3) or shell (OCI CLI)? — to resolve via: preference for OCI CLI since it's already installed on the instance and requires less setup.

## Telemetry

- **Log events**: `audio_stream_started` (existing, add `store_type=object_storage`), `audio_upload_completed` (on `store()`), `audio_object_missing` (on failed `exists()` / `stream()`)
- **Metrics**: stream latency (time-to-first-byte from Object Storage), upload duration, `head_object` latency
- **Alerts**: None for MVP — deferred to post-launch observability spec

## Related

- [vault: wiki/concepts/Hexagonal Architecture.md], [vault: wiki/concepts/Adapter Pattern.md]
- Predecessor: `docs/brainstorms/go-microservices-handoff.md` (handoff doc that motivated this spec)
- Related specs: `docs/specs/resilience-v1/spec.md` (ReconcileTrackStatus use case), `docs/specs/import-legacy-library/spec.md` (acquisition pipeline + audio_ref)
