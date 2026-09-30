# System

## Context

Who uses Altune and which outside systems it depends on.

```mermaid
flowchart TD
    listener(["Listener<br/>finds, saves and plays music"])
    operator(["Operator · owner<br/>watches and runs the system"])

    altune["<b>Altune</b><br/>self-hosted music manager:<br/>discover across providers, build a library you own, stream it"]

    subgraph ext [External systems]
        supa["Supabase<br/>Auth + hosted Postgres"]
        meta["Metadata providers<br/>Deezer · MusicBrainz · Last.fm · Spotify · Apple Music<br/>SoundCloud · YT Music · Amazon · artwork sources"]
        sources["Audio sources<br/>YouTube / YT Music / SoundCloud via yt-dlp<br/>Qobuz / Tidal / Deezer via streamrip"]
        acoustid["AcoustID<br/>fingerprint → recording"]
        github["GitHub<br/>Actions CI/CD · Releases · kill switches"]
        gitea["Gitea · altune-git<br/>feedback issues · CI"]
        altstore["AltStore<br/>iOS sideload channel"]
        ociusage["OCI Usage API<br/>infra spend"]
    end

    listener -->|search · save · play| altune
    operator -->|dashboards · overseer| altune
    altune -->|sign-in, JWT verify, data| supa
    altune -->|search · enrich · artwork| meta
    altune -->|download audio| sources
    altune -->|verify identity| acoustid
    altune -->|read kill switches| github
    altune -->|file feedback issues| gitea
    github -->|deploy over SSH| altune
    altune -->|alerts logged, read on demand| operator
    altstore -->|installs app| listener
    github -->|.ipa + source JSON| altstore
    altune -->|read spend| ociusage

    classDef sys fill:#1168bd,color:#fff,stroke:#0b4884
    classDef person fill:#08427b,color:#fff,stroke:#052e56
    classDef external fill:#999,color:#fff,stroke:#6b6b6b
    class altune sys
    class listener,operator person
    class supa,meta,sources,acoustid,github,gitea,altstore,ociusage external
```

## Containers

What runs where: the phone, the OCI VM, Supabase and the audio bucket, and how they talk.

```mermaid
flowchart TD
    listener(["Listener"])
    operator(["Operator"])

    mobile["<b>Mobile app</b><br/>Expo · React Native<br/>track-player, offline pins<br/>same app built for web"]

    subgraph vm [OCI VM · one Docker Compose project per env, prod + staging]
        caddy["<b>Caddy</b><br/>TLS · routes · blue/green switch<br/>serves the web build"]
        api["<b>go-api</b> · blue + green<br/>Go modular monolith<br/>catalog · acquisition · discovery · playback<br/>auth · feedback · observe API<br/>+ in-process jobs, leader-elected"]
        tools[["yt-dlp · streamrip · ffmpeg · fpcalc<br/>subprocesses in the go-api image"]]
        overseer["<b>Overseer</b><br/>Go + React SPA<br/>owner control room"]
        redis[("<b>Redis</b><br/>cache only · 256 MB LRU<br/>results · identity · artwork · rate limits")]
        sqlite[("SQLite<br/>Overseer history")]
    end

    pg[("<b>Postgres</b> · Supabase<br/>tracks · playlists · queue state<br/>events · identity · leader lock")]
    bucket[("<b>OCI Object Storage</b><br/>audio files")]
    auth["Supabase Auth"]
    providers["Metadata providers + AcoustID"]
    sources["Audio sources"]
    extras["Gitea feedback issues · OCI Usage API"]
    ks["kill-switches.json<br/>raw.githubusercontent.com"]

    listener --> mobile
    mobile -->|HTTPS /v1/* + JWT · SSE /v1/events| caddy
    mobile -->|presigned GET, Range| bucket
    mobile -->|sign in · refresh| auth
    mobile -->|poll every 5 min| ks
    operator -->|/overseer| caddy
    caddy --> api
    caddy -->|/overseer| overseer
    overseer -->|/observe/* JSON + SSE via :8081| caddy
    overseer --> sqlite
    overseer -->|JWKS · token refresh| auth
    api -->|SQL, pgx| pg
    api --> redis
    api -->|store · stat · presign, S3 API| bucket
    api -->|verify JWT via JWKS| auth
    api -->|HTTPS/JSON| providers
    api --> tools -->|download| sources
    api --> extras
    overseer --> extras

    classDef container fill:#438dd5,color:#fff,stroke:#2e6295
    classDef store fill:#438dd5,color:#fff,stroke:#2e6295
    classDef person fill:#08427b,color:#fff,stroke:#052e56
    classDef external fill:#999,color:#fff,stroke:#6b6b6b
    class mobile,caddy,api,overseer,tools container
    class redis,sqlite,pg,bucket store
    class listener,operator person
    class auth,providers,sources,extras,ks external
```

## Components

Inside go-api: one module per bounded context, each hexagonal (`domain` · `ports` · `service` · `adapters`), wired together only in `app`. An arrow is a Go import from `internal/<module>`; every module also uses `shared` (config, database, Redis, logging, exec), left out to keep the picture readable.

```mermaid
flowchart TD
    caddy["Caddy"]

    subgraph goapi [go-api]
        app["<b>app</b><br/>wiring · routes · SSE<br/>leader-elected jobs · health"]
        auth["<b>auth</b><br/>JWT middleware<br/>failed-login throttle"]
        catalog["<b>catalog</b><br/>library · tracks · playlists<br/>audio store"]
        acquisition["<b>acquisition</b><br/>find · download<br/>fingerprint · tag"]
        discovery["<b>discovery</b><br/>multi-provider search<br/>enrich · rank · cache"]
        playback["<b>playback</b><br/>queue · now playing"]
        feedback["<b>feedback</b><br/>user feedback → issue"]
        observe["<b>observe</b><br/>alerts · eval meter<br/>event tap for Overseer"]
    end

    pg[("Postgres")]
    redis[("Redis")]
    bucket[("Object Storage")]
    jwks["Supabase Auth · JWKS"]
    providers["Metadata providers"]
    tools[["yt-dlp · streamrip · ffmpeg · fpcalc<br/>+ AcoustID"]]
    gitea["Gitea issues"]

    caddy --> app
    app --> auth & catalog & acquisition & discovery & playback & feedback & observe
    acquisition --> catalog
    acquisition --> discovery
    playback --> catalog
    observe --> acquisition
    observe --> discovery

    auth -->|verify JWT| jwks
    catalog -->|SQL| pg
    catalog -->|S3 API, presign| bucket
    acquisition -->|SQL| pg
    acquisition -->|subprocess| tools
    discovery -->|SQL| pg
    discovery -->|cache| redis
    discovery -->|HTTPS/JSON| providers
    playback -->|SQL| pg
    feedback -->|HTTPS| gitea

    classDef component fill:#85bbf0,color:#000,stroke:#5d82a8
    classDef store fill:#438dd5,color:#fff,stroke:#2e6295
    classDef external fill:#999,color:#fff,stroke:#6b6b6b
    class app,auth,catalog,acquisition,discovery,playback,feedback,observe component
    class pg,redis,bucket store
    class caddy,jwks,providers,tools,gitea external
```
