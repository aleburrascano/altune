# Discovery — Architecture

Visual map of the discovery bounded context. Use this to trace data flow when debugging search quality issues.

## Search Flow

```mermaid
flowchart TD
    subgraph "Client"
        TYPING["User types"] --> SUGGEST_REQ["GET /suggest?q=partial"]
        TYPING --> DEBOUNCE["300ms debounce"]
        DEBOUNCE --> SEARCH_REQ["GET /search?q=query"]
        SUGGEST_REQ --> SUGGEST_RESP["Suggestion dropdown"]
        SUGGEST_RESP -->|tap suggestion| SEARCH_REQ
    end

    subgraph "Handler Layer"
        SEARCH_REQ --> HANDLER["discoveryHandler.handleSearch"]
        SUGGEST_REQ --> SUGGEST_H["discoveryHandler.handleSuggest"]
    end

    subgraph "Service Layer"
        HANDLER --> SEARCH_SVC["SearchMusicService.Execute"]
        SUGGEST_H --> SUGGEST_SVC["SuggestService.Execute"]

        SEARCH_SVC --> CLEAN["CleanQuery\n(strip 'official video', 'lyrics', etc.)"]
        CLEAN --> INTENT["DetectIntent(query, vocab)"]
        INTENT --> SCATTER["Scatter to providers"]

        SCATTER -->|"1500ms timeout each\n(3s for Tidal)"| PROVIDERS

        SCATTER --> COLLECT["Collect results + statuses"]
        COLLECT --> FUSE["FuseAndRank"]

        FUSE --> MERGE["Identifier merge\n(ISRC / MBID only)"]
        MERGE --> POP_NORM["NormalizePopularity\n(log-scale, 0-100)"]
        POP_NORM --> RECENCY["Recency boost\n(x1.1 if <=30 days)"]
        RECENCY --> SCORE["Score: relevance + intent boost\n+ exact/prefix/multi-field bonuses\n+ RRF (equal weights)"]
        SCORE --> GATE["Gate: word-share + browseable source"]
        GATE --> SORT["Sort: relevance-band(0.05)\n-> demoted -> popularity\n-> multi-source -> quality\n-> RRF -> alpha"]
        SORT --> COLLAPSE["CollapseVersions\n(group remix/live variants)"]
        COLLAPSE --> POP_DOM["ApplyPopularityDominance\n(cross-kind top-5 check)"]
        POP_DOM --> DIVERSITY["EnforceDiversity\n(max 3 per artist in top 10)"]

        DIVERSITY --> COLLAPSE_ART["CollapseArtistDuplicates"]
        COLLAPSE_ART --> DISAMBIG["ApplyArtistDisambiguation"]
        DISAMBIG --> CLICK["Click boost\n(boost previously-clicked results)"]
        CLICK --> ENRICH["Enrich top 50\n(artwork + popularity)"]
        ENRICH --> RERANK["Rerank\n(same key minus quality)"]

        RERANK --> ZERO{zero results?}
        ZERO -->|yes| CORRECT["CorrectionService\n(trigram + phonetic vs vocabulary)"]
        CORRECT -->|corrected query| SCATTER
        CORRECT -->|no match| ZERO_RESP["Return empty + no correction"]
        ZERO -->|no| RELATED["FindRelatedService\n(top 5 results, 2s timeout)"]

        RELATED -->|parallel| LOCAL_DB["Local DB\n(cross-user album/artist match)"]
        RELATED -->|parallel| DZ_ALBUM["Deezer album tracks\n(via deezer_album_id)"]
        RELATED -->|parallel| DZ_ARTIST["Deezer artist albums\n(via deezer source ID)"]

        RELATED --> LIMIT["Limit to user's count"]

        LIMIT --> HISTORY_SAVE["Save search history"]
        LIMIT --> VOCAB_INGEST["Ingest to vocabulary\n(top 5 + separate artist entries)"]
        LIMIT --> RESPONSE["Return SearchOutput\n(results + related groups)"]
    end

    subgraph "Providers (adapters/providers/)"
        PROVIDERS["8 Search Providers"]
        DEEZER["Deezer\n+nb_fan, rank, ISRC\n+StructuredSearcher"]
        LASTFM["Last.fm\n+listeners, albums, top tracks"]
        MUSICBRAINZ["MusicBrainz\n+MBID, ISRC\n+StructuredSearcher"]
        ITUNES["iTunes\n+metadata, genre"]
        SOUNDCLOUD["SoundCloud (yt-dlp)\n+playback_count"]
        AUDIODB["TheAudioDB\n+artist images"]
        TIDAL["Tidal\n+ISRC (inactive — no client_credentials)"]
        YTMUSIC["YouTube Music\n+massive catalog, no auth"]
        PROVIDERS --> DEEZER
        PROVIDERS --> LASTFM
        PROVIDERS --> MUSICBRAINZ
        PROVIDERS --> ITUNES
        PROVIDERS --> SOUNDCLOUD
        PROVIDERS --> AUDIODB
        PROVIDERS --> TIDAL
        PROVIDERS --> YTMUSIC
    end

    subgraph "Vocabulary (Redis)"
        VOCAB[("Vocabulary Store\nterms sorted set\ntrigram index\nmetaphone index\nentry hashes")]
        CHARTS["Chart Refresh\n(Deezer + Last.fm charts)\nevery 6 hours"] -->|BulkAdd| VOCAB
        VOCAB_INGEST -->|Add| VOCAB
        SUGGEST_SVC -->|SuggestByPrefix\nFindClosest| VOCAB
        CORRECT -->|FindClosest| VOCAB
        INTENT -->|SuggestByPrefix| VOCAB
    end

    subgraph "Caches (Redis)"
        QUERY_CACHE[("Query Cache\n10min TTL")]
        ARTWORK_CACHE[("Artwork Cache\n14d pos / 1d neg")]
        POP_CACHE[("Popularity Cache\n7d pos / 2h neg")]
        MBID_CACHE[("MBID Cache\n30d pos / 1d neg")]
    end
```

## Artist Detail Flow

```mermaid
flowchart TD
    TAP["User taps artist result"] --> HANDLER["handleArtistTopTracks\nhandleArtistAlbums"]

    subgraph "Top Tracks"
        HANDLER --> TOP["GetArtistContentService.GetTopTracks"]
        TOP --> DZ_TOP["Deezer /artist/{id}/top\n(limit 10)"]
        DZ_TOP --> TOP_RESP["Return tracks"]
    end

    subgraph "Albums (with consensus)"
        HANDLER --> ALBUMS["GetArtistContentService.GetAlbums"]
        ALBUMS --> DZ_ALB["Deezer /artist/{id}/albums\n(limit 100)"]
        DZ_ALB --> DEDUP["dedupAlbums\n(by title + artist)"]
        DEDUP --> CONSENSUS["ConsensusService.BuildConsensus"]

        CONSENSUS --> PARALLEL["Query consensus providers\n(no hardcoded timeout, parallel)"]
        PARALLEL --> LFM["Last.fm\nartist.getTopAlbums"]
        PARALLEL --> MB["MusicBrainz\nrelease-groups"]
        PARALLEL --> DISCOGS["Discogs\nartist releases"]
        PARALLEL --> ITUNES_C["iTunes\nalbum search"]
        PARALLEL --> YTM_C["YouTube Music\nalbum search"]

        LFM --> COUNT["Count providers with data"]
        MB --> COUNT
        DISCOGS --> COUNT
        ITUNES_C --> COUNT
        YTM_C --> COUNT

        COUNT --> MATCH["Match albums by title\n(NormalizeForMatch + TSR >= 85)"]
        MATCH --> MERGE_ALL["Merge ALL providers into union\n(every provider is equal source)"]
        MERGE_ALL --> CLASSIFY["Classify each album"]
        CLASSIFY --> CONF["2+ providers → confirmed"]
        CLASSIFY --> UNCONF["1 provider → unconfirmed, keep"]

        CONSENSUS --> MB_CHECK["MB identity contradiction\n(different MBID → remove)"]
        MB_CHECK --> MB_VALID["Cross-validate: zero MB overlap\n→ discard MB identity"]
        MB_CHECK --> MB_AUTH{"MB has 10+ confirmed?"}
        MB_AUTH -->|yes| AUTH_FILTER["MB authority filter:\nreject unconfirmed albums"]
        MB_AUTH -->|no| KEEP_ALL["Keep all unconfirmed"]

        CONF --> FINAL["Final results"]
        AUTH_FILTER --> FINAL
        KEEP_ALL --> FINAL

        FINAL --> LIMIT_ALB["Apply limit (default 50)"]
        LIMIT_ALB --> ALB_RESP["Return with consensus_status in extras"]
    end

    subgraph "Album Tracks (with Deezer fallback)"
        ALB_RESP -->|user taps album| TRACK_REQ["GET /albums/{provider}/{id}/tracks\n+title & artist query params"]
        TRACK_REQ --> PROV_CHECK{provider supported?}
        PROV_CHECK -->|yes| FETCH_TRACKS["Fetch tracks from provider"]
        PROV_CHECK -->|no| DZ_FALLBACK["Deezer search fallback:\nsearch by title + artist"]
        FETCH_TRACKS --> EMPTY_CHECK{empty results?}
        EMPTY_CHECK -->|yes + title provided| DZ_FALLBACK
        EMPTY_CHECK -->|no| TRACK_RESP["Return tracks"]
        DZ_FALLBACK --> DZ_MATCH["First Deezer album match\n→ fetch its tracks"]
        DZ_MATCH --> TRACK_RESP
    end
```

## Artwork Resolution Flow

```mermaid
flowchart TD
    NEED["Result needs artwork"] --> OWN{"Deezer own image\npresent and not placeholder?"}
    OWN -->|yes| DONE["Use Deezer image"]
    OWN -->|no| CHAIN["Artwork chain"]

    CHAIN --> CAA["Cover Art Archive\n(MBID-keyed, up to 1200px)"]
    CAA -->|found| DONE2["Use CAA image"]
    CAA -->|miss| FANART["Fanart.tv\n(MBID-keyed, HD)"]
    FANART -->|found| DONE3["Use Fanart.tv image"]
    FANART -->|miss| GENIUS["Genius\n(name search)"]
    GENIUS -->|found| DONE4["Use Genius image"]
    GENIUS -->|miss| TADB["TheAudioDB\n(name search)"]
    TADB -->|found| DONE5["Use TheAudioDB image"]
    TADB -->|miss| DZ_SEARCH["Deezer\n(name search fallback)"]
    DZ_SEARCH -->|found| DONE6["Use Deezer fallback"]
    DZ_SEARCH -->|miss| ITUNES_ART["iTunes\n(name search)"]
    ITUNES_ART -->|found| DONE7["Use iTunes image"]
    ITUNES_ART -->|miss| YT["YouTube\n(channel thumbnail, last resort)"]
    YT -->|found| DONE8["Use YouTube image"]
    YT -->|miss| NONE["No artwork"]
```

## Ranking Key (sort order)

```
Position  Signal              Direction   Source
────────  ──────────────────  ──────────  ──────────────────────
1         Relevance band      DESC        TokenSortRatio (0.05 granularity)
2         Demoted             ASC         record_type not in {album,single,ep}
3         Popularity          DESC        NormalizePopularity (0-100, log-scale)
4         Multi-source        DESC        len(providers) > 1
5         Quality score       DESC        completeness + agreement + tier + fetch
6         RRF                 DESC        Σ 1/(60 + rank) — equal weight all providers
7         Subtitle            ASC         alphabetical tiebreak
8         Title               ASC         alphabetical tiebreak
```

After enrichment, `Rerank` uses the same key minus quality score.

## Diagnostic Logging

Enable with `LOG_LEVEL=debug`. Each pipeline stage emits a structured log entry:

```
pipeline.query_clean      — input, output, changed
pipeline.intent_detect    — detected (bool)
pipeline.fuse_and_rank    — raw count, merged count (in search.merged)
pipeline.collapse_artist  — input_count, output_count
pipeline.click_boost      — count
pipeline.enrich           — count
pipeline.rerank           — count
consensus.providers_responded — artist, responded, total_providers
consensus.complete        — artist, confirmed, unconfirmed, rejected
```

## File Map

```
internal/discovery/
├── domain/
│   ├── types.go              # SearchResult, SearchQuery, SourceRef, RelatedGroup, enums (incl. ProviderTidal)
│   ├── identity.go           # ArtistIdentityProfile, AlbumVerdict (used by consensus MB check)
│   ├── events.go             # SearchPerformed, ResultClicked
│   └── vocabulary.go         # VocabularyEntry
├── ports/
│   └── ports.go              # Port interfaces (SearchProvider, ArtistContentProvider, ClickSignalProvider, etc.)
├── service/
│   ├── search_music.go       # SearchMusicService — main search orchestrator + click boost
│   ├── consensus.go          # ConsensusService — multi-provider album consensus + MB contradiction
│   ├── dedup.go              # FuseAndRank, Rerank, CollapseVersions, PopularityDominance, Diversity
│   ├── normalize.go          # NormalizeForMatch (8-step canonicalization)
│   ├── fuzzy.go              # TokenSortRatio, levenshteinDistance
│   ├── popularity.go         # NormalizePopularity (log-scale, multi-provider)
│   ├── correction.go         # CorrectionService (trigram Jaccard + phonetic)
│   ├── intent.go             # DetectIntent (vocabulary-based artist+track split)
│   ├── query_clean.go        # CleanQuery (strip YouTube noise)
│   ├── metaphone.go          # DoubleMetaphone, MetaphoneKey (phonetic codes)
│   ├── suggest.go            # SuggestService (prefix + fuzzy fallback)
│   ├── vocabulary_refresh.go # Background chart ingestion (6h ticker)
│   ├── quality_scorer.go     # ComputeQualityScore, IsDemoted
│   ├── circuit_breaker.go    # Per-provider circuit breaker
│   ├── record_click.go       # RecordClickService
│   ├── list_history.go       # ListSearchHistoryService
│   ├── find_related.go       # FindRelatedService — entity relationship enrichment
│   ├── get_album_tracks.go   # Album content fetch + Deezer search fallback for non-Deezer albums
│   ├── get_artist_content.go # Artist top-tracks/albums with consensus integration
│   └── url_router.go         # URL-paste provider detection
└── adapters/
    ├── handler/
    │   └── discovery_handler.go  # HTTP routes (search, suggest, history, clicks, content)
    ├── providers/
    │   ├── deezer.go         # Search + Charts + Artwork + Content + ISRC fetch
    │   ├── lastfm.go         # Search + Charts + Artist albums/top tracks
    │   ├── musicbrainz.go    # Search + Album validation + Identity resolution
    │   ├── itunes.go         # Search + Artwork + Album lookup
    │   ├── soundcloud.go     # Search via yt-dlp
    │   ├── theaudiodb.go     # Search (artists) + Artwork
    │   ├── tidal.go          # Search + Artist content (OAuth 2.0 — inactive, no client_credentials for 3rd party)
    │   ├── ytmusic.go        # YouTube Music — Search + Artist albums/top tracks (raitonoberu/ytmusic, no auth)
    │   ├── coverartarchive.go # Artwork (MBID-keyed album covers, up to 1200px)
    │   ├── genius.go         # Artwork (name search)
    │   ├── fanarttv.go       # Artwork (MBID-based HD)
    │   ├── discogs.go        # Artwork + Discography enrichment
    │   ├── artwork_chain.go  # Chained artwork resolver (ID-first, name-search last)
    │   ├── wikidata.go       # MBID resolution (Deezer ID → MB via SPARQL)
    │   └── youtube.go        # Artwork (channel thumbnails, last resort)
    ├── cache/
    │   ├── query_cache.go        # 10min per-provider query cache
    │   ├── artwork_cache.go      # 14d artwork cache
    │   ├── popularity_cache.go   # 7d popularity cache
    │   ├── mbid_cache.go         # 30d MBID cache
    │   ├── discogs_cache.go      # Discogs artist resolution cache
    │   ├── vocabulary_store.go   # Trigram-indexed vocabulary (prefix + fuzzy)
    │   └── fetch_success.go      # Provider reliability tracking
    └── persistence/
        ├── history_repo.go   # Search history (Postgres)
        └── click_repo.go     # Click tracking + ClickSignalProvider (Postgres)
```
