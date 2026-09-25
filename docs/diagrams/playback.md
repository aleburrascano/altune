# Playback

## Modules

How the mobile playback modules depend on each other (`apps/mobile/src/features/playback`).

```mermaid
flowchart TD
    ui["Screens<br/>Library · Playlist · Detail · Mini/Full player"]

    subgraph react [React tree]
        hooks["useQueuePlayback · usePlayback<br/>public playback API"]
        provider["TrackPlayerPlaybackProvider<br/>derivePlaybackState → idle · loading · playing · paused · ended · error"]
        resume["useQueueResume<br/>save every 15s · restore on launch"]
        signals["usePlaybackSignals · playbackHealth"]
    end

    subgraph stores [zustand stores]
        queueStore[("queueStore")]
        errStore[("playbackErrorStore")]
    end

    subgraph bridge [Native bridge]
        actions["createNativePlaybackActions<br/>play · startQueue · retry · skip · seek"]
        loader["loadNativeTrack / loadNativeQueue<br/>queue lock · load token: newest load wins"]
        presign["presignWindow<br/>25 URLs ahead, slides at 5 left"]
    end

    subgraph headless [Headless service · runs with the app in background]
        service["playbackService<br/>lock-screen controls · ducking · errors · track change"]
        prefetch["audioPrefetch + nativeTrackSwap<br/>next track to disk, swapped into its slot"]
    end

    pins[("Offline pins")]
    rntp{{"react-native-track-player<br/>native audio"}}

    subgraph server [Go API]
        audioUrls["POST /v1/audio-urls"]
        queueState["/v1/playback/queue-state"]
        recover["POST /v1/tracks/{id}/audio/recover"]
        events["telemetry events"]
    end

    ui --> hooks
    hooks -->|mutate queue first| queueStore
    hooks --> provider
    provider -.->|reads| queueStore & errStore
    provider --> actions --> loader
    loader -->|1 pinned file| pins
    loader -->|2 presigned URL| audioUrls
    loader --- presign
    loader -->|reset · add · skip · play| rntp
    rntp -.->|native state| provider
    rntp -.->|native events| service
    service -->|on track change| prefetch --> rntp
    prefetch --> audioUrls
    service -->|on track change| presign
    service -->|on error| errStore
    service -->|on error| recover
    resume <-->|GET / PUT| queueState
    resume -->|rebuild queue, paused| loader
    signals -->|play · skip · completed| events
```

## Tap play

Runtime order when a user plays from a list: native queue load, prefetch, and error recovery.

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant Q as useQueuePlayback
    participant QS as queueStore
    participant L as loadNativeQueue
    participant API as Go API
    participant N as track-player (native)
    participant S as playbackService
    participant P as audioPrefetch

    U->>Q: tap track in a list
    Q->>QS: loadQueue (bumps generation)
    Q->>L: startQueue(tracks, index) · clears old error
    L->>L: claim load token · ensure player set up
    L->>N: reset (under queue lock)
    L->>API: POST /v1/audio-urls (next 25 tracks, 2.5s timeout)
    alt presign ok
        API-->>L: presigned URLs + audio version
    else presign fails
        API--xL: error · fall back to authed stream endpoint
    end
    L->>N: add tracks up to index+100<br/>URL per track: pinned file > presigned > GET /v1/tracks/{id}/audio
    L->>N: skip(index) · play()
    Note over L,N: every await checks the load token: a newer tap cancels this load

    N-)S: PlaybackActiveTrackChanged
    S->>QS: syncCurrentIndex (match by track key)
    par prefetch next track
        S->>P: prefetchNext(index)
        P->>API: POST /v1/audio-urls (next track)
        alt already in audioCache
            P->>N: swap local file into next slot
        else not cached
            P->>P: download from presigned URL (150 MB cap, .part then rename)
            P->>N: swap in, only if still next
        end
    and keep URLs fresh
        opt within 5 tracks of the presigned end
            S->>L: reorderUpcomingNative · presign next batch
        end
    end

    opt native PlaybackError
        N-)S: PlaybackError
        S->>S: classify · write playbackErrorStore → UI shows error
        alt slot held a prefetched file
            S->>N: repair: re-presign and reload streaming
        else library track, within retry budget
            S-)API: POST /v1/tracks/{id}/audio/recover
        end
    end
```

## Player states

The states `derivePlaybackState` can return and what moves between them.

```mermaid
stateDiagram-v2
    [*] --> idle
    idle --> active: play / playFromList sets the track
    active --> idle: stop · sign-out

    state "track loaded" as active {
        [*] --> loading
        [*] --> paused: queue restored on launch
        loading --> playing: native buffered
        playing --> paused: pause · ducking · sleep timer
        paused --> playing: resume
        playing --> loading: seek · skip · rebuffer
        playing --> ended: queue finished
        ended --> playing: Play again
        loading --> error: load failed
        playing --> error: native PlaybackError
        error --> loading: Retry, or Skip if not_found / decode
    }

    note right of active
        Derived each render, not stored.
        Checked in order: no track = idle,
        error for this track = error,
        then native state = loading / ended / playing / paused
    end note
```
