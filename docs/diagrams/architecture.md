# Altune — System Architecture

```mermaid
graph TB
    subgraph Mobile["📱 Mobile (Expo + React Native)"]
        direction TB
        App["app/ routes"]
        Auth["features/auth"]
        Discover["features/discover"]
        Detail["features/detail"]
        Library["features/library"]
        Playback["features/playback"]
        Settings["features/settings"]
        Shared["shared/ (api-client, ui, playback store)"]
        
        App --> Auth & Discover & Detail & Library & Playback & Settings
        Auth & Discover & Detail & Library & Playback --> Shared
    end

    subgraph API["🖥️ Go API (Hexagonal Architecture)"]
        direction TB
        
        subgraph Inbound["Inbound Adapters (HTTP Handlers)"]
            TrackH["Track Handler"]
            PlaylistH["Playlist Handler"]
            StreamH["Stream Handler"]
            DiscoveryH["Discovery Handler"]
            QueueH["Queue Handler"]
            RetryH["Retry Handler"]
        end
        
        subgraph Services["Application Services"]
            AddTrack["AddTrack"]
            DeleteTrack["DeleteTrack"]
            ListTracks["ListTracks"]
            StreamTrack["StreamTrack"]
            Playlists["Playlists"]
            SearchMusic["SearchMusic"]
            Acquire["AcquireTrackAudio"]
            SaveQueue["SaveQueueState"]
        end
        
        subgraph Domain["Domain Layer (Pure)"]
            CatalogDom["catalog/domain<br/>Track, Playlist, AcquisitionStatus"]
            DiscoveryDom["discovery/domain<br/>SearchResult, SearchQuery, Confidence"]
            PlaybackDom["playback/domain<br/>QueueState, RepeatMode"]
        end
        
        subgraph Outbound["Outbound Adapters"]
            TrackRepo["Track Repository<br/>(Postgres)"]
            PlaylistRepo["Playlist Repository<br/>(Postgres)"]
            QueueRepo["Queue State Repository<br/>(Postgres)"]
            AudioStore["Audio Store<br/>(OCI Object Storage)"]
            
            subgraph Providers["Discovery Providers"]
                Deezer
                MusicBrainz
                iTunes
                LastFM["Last.fm"]
                SoundCloud
                YouTube
                Discogs
                TheAudioDB
            end
            
            subgraph Caches["Redis Caches"]
                QueryCache["Query Cache"]
                ArtworkCache["Artwork Cache"]
                PopularityCache["Popularity Cache"]
                MbidCache["MBID Cache"]
                IdentityCache["Identity Cache"]
            end
            
            YtDlp["yt-dlp<br/>(Audio Searcher)"]
            ArtworkChain["Artwork Chain<br/>(Fanart.tv, Genius, Wikidata)"]
        end
        
        Inbound --> Services
        Services --> Domain
        Services --> Outbound
    end

    subgraph External["External Services"]
        Supabase["Supabase<br/>(Auth + Postgres)"]
        Redis["Upstash Redis"]
        OCI["OCI Object Storage"]
    end

    Mobile -->|"HTTP/JSON<br/>(Bearer JWT)"| Inbound
    TrackRepo & PlaylistRepo & QueueRepo --> Supabase
    Caches --> Redis
    AudioStore --> OCI
    YtDlp -->|"Download audio"| YouTube
    Providers -->|"Search APIs"| Deezer & MusicBrainz & iTunes & LastFM & SoundCloud
```

## Key Principles

- **Dependencies point inward**: adapters → services → domain. Domain imports nothing from outer layers.
- **Ports defined in application layer**: services depend on interfaces, adapters implement them.
- **Mobile vertical slices**: each feature folder owns its UI, hooks, API calls. Cross-feature reuse goes through shared/.
- **Single deployment**: one Go binary, one Expo app. No microservices yet.
