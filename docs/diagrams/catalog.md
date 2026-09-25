# Catalog and acquisition

## Track acquisition states

The states a saved Track can be in, and the domain method that moves it (`services/go-api/internal/catalog/domain/track.go`).

```mermaid
---
config:
  layout: elk
---
stateDiagram-v2
    [*] --> pending: save · POST /v1/tracks

    pending --> ready: MarkReady<br/>file stored
    pending --> failed: FailAcquisition · pipeline failed<br/>MarkFailed · queue refused<br/>sweep · stuck 15 min

    ready --> failed: MarkFailed<br/>file missing
    ready --> pending: RevertToPending<br/>reacquire

    failed --> pending: RevertToPending<br/>user retry
    failed --> ready: MarkReady<br/>backfill CLI

    note right of ready
        Only state with audio_ref, so the only streamable one.
        ReplaceAudio swaps the file and stays ready.
        Every settle mints a new AudioVersion.
    end note
```

## Save to play

From tapping Save to hearing the track, including the retry path.

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant App as Mobile app
    participant API as go-api (catalog)
    participant W as Acquisition worker<br/>(in-process goroutine)
    participant Src as yt-dlp / streamrip
    participant OS as OCI Object Storage
    participant DB as Postgres

    U->>App: tap Save on a track
    App->>App: optimistic pending row · outbox library_add
    App->>API: POST /v1/tracks + Idempotency-Key
    API->>DB: insert Track (pending) · dedup on key
    alt new row
        API->>W: Schedule (queue depth 4× concurrency, 5s timeout)
        opt queue full or timeout
            API->>DB: MarkFailed(acquisition_refused)
        end
        API-->>App: 201 Created
    else duplicate or replay
        API-->>App: 200 OK, no new job
    end

    W->>W: wait for slot (ACQUISITION_CONCURRENCY=5, max 5 min)
    W-)App: SSE track_acquisition_started
    Note over App,W: every SSE event goes through the in-process event bus, served at GET /v1/events
    W->>Src: search: up to 4 query variants
    W->>W: select: identity score ≥ 60, Topic channels first
    loop up to 8 candidates
        W->>Src: download (5 min timeout)
        W->>W: check duration · decode (ffprobe) · fingerprint (AcoustID)
    end
    W-)App: SSE track_acquisition_progress (finding → downloading → finishing)
    W->>OS: store user/artist/album/title.ext
    W->>DB: MarkReady(audio_ref) · new AudioVersion (CAS, 5 retries)
    alt success
        W-)App: SSE track_acquisition_completed
        App->>App: row → ready · downloads bar shows done
    else any step failed (10 min total budget)
        W->>DB: FailAcquisition(code) · rollback deletes stored file
        W-)App: SSE track_acquisition_failed
        App->>App: row → failed · Retry offered
    end
    Note over App,API: SSE only while app is foregrounded. Library poll covers pending rows as backup.

    U->>App: tap play
    App->>API: POST /v1/audio-urls
    API->>OS: presign GET (TTL ≤ 1h)
    API-->>App: presigned URL + version
    App->>OS: stream audio (HTTP Range)
    Note over App,OS: fallback: GET /v1/tracks/{id}/audio through go-api

    opt user taps Retry on a failed row
        App->>API: POST /v1/tracks/{id}/retry
        API->>DB: check failed + 60s cooldown
        API-->>App: 202 (409 not failed, 429 cooling down)
        API->>W: Schedule → RevertToPending → run pipeline again
    end
```
