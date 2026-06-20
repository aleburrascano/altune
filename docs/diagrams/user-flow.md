# Altune — User Flow (Happy Path)

```mermaid
sequenceDiagram
    actor User
    participant Mobile as 📱 Mobile App
    participant API as 🖥️ Go API
    participant Providers as 🎵 Music Providers
    participant YtDlp as 📥 yt-dlp
    participant Storage as 💾 Object Storage
    participant DB as 🗄️ Postgres

    Note over User,DB: 1. DISCOVER MUSIC
    User->>Mobile: Type search query
    Mobile->>Mobile: Debounce 300ms
    Mobile->>API: GET /v1/discovery/search?q=...
    API->>Providers: Fan out to Deezer, MB, iTunes, etc.
    Providers-->>API: Results per provider
    API->>API: Dedup + rank + enrich artwork/popularity
    API-->>Mobile: Merged results with confidence scores
    Mobile->>User: Show blended results<br/>(Top Result + sections)

    Note over User,DB: 2. VIEW DETAIL
    User->>Mobile: Tap a result
    Mobile->>Mobile: Stash result in handoff
    Mobile->>User: Show detail screen<br/>(track/album/artist)

    Note over User,DB: 3. SAVE TO LIBRARY
    User->>Mobile: Tap "Save to Library"
    Mobile->>Mobile: Optimistic update<br/>(pending placeholder in cache)
    Mobile->>API: POST /v1/tracks
    API->>DB: Insert track (status=pending)
    API-->>Mobile: 201 Created
    API->>API: Schedule acquisition

    Note over User,DB: 4. AUDIO ACQUISITION (background)
    API->>YtDlp: Search for matching audio
    YtDlp-->>API: Candidates ranked by score
    API->>API: Select best match
    API->>YtDlp: Download audio
    YtDlp-->>API: Audio file
    API->>API: Tag with ID3 metadata
    API->>Storage: Upload to object storage
    Storage-->>API: audio_ref
    API->>DB: Update track (status=ready, audio_ref)

    Note over User,DB: ⚠️ GAP: No push notification to mobile
    rect rgb(255, 240, 240)
        Mobile->>Mobile: Poll every 30s OR<br/>user pull-to-refresh
        Mobile->>API: GET /v1/tracks
        API-->>Mobile: Track now shows status=ready
        Mobile->>User: Track playable ✓
    end

    Note over User,DB: 5. PLAY FROM LIBRARY
    User->>Mobile: Tap track in library
    Mobile->>Mobile: Build queue from source list
    Mobile->>API: GET /v1/tracks/{id}/stream
    API->>Storage: Fetch audio file
    Storage-->>API: Audio stream
    API-->>Mobile: Audio stream (chunked)
    Mobile->>User: 🎶 Audio playing

    Note over User,DB: 6. QUEUE MANAGEMENT
    User->>Mobile: Reorder / shuffle / repeat
    Mobile->>Mobile: Update Zustand queue store
    Mobile->>API: PUT /v1/playback/queue-state
    API->>DB: Upsert queue snapshot
    Note right of Mobile: Queue persists for<br/>resume on app reopen
```

## The SSE Gap (highlighted in red)

The red box shows where real-time events would improve UX:

| Current (polling) | With SSE |
|---|---|
| User saves track → waits up to 30s to see "ready" | Server pushes `track.status_changed` → instant UI update |
| Retry acquisition → no feedback | Server pushes progress events → loading indicator |
| Multi-device changes → stale until refresh | Server pushes `library.changed` → auto-refresh |

Domain events already exist (`TrackAddedToLibrary`, status changes). The missing piece is a **transport to push them to the client**.
