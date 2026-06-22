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
        HANDLER --> SEARCH_SVC["Service.Execute\n(rebuilt Merge/Rank core)"]
        SUGGEST_H --> SUGGEST_SVC["SuggestService.Execute"]

        SEARCH_SVC --> CLEAN["CleanQuery\n(strip 'official video', 'lyrics', etc.)"]
        CLEAN --> SCATTER["fanOut to providers"]

        SCATTER -->|"1500ms timeout each"| PROVIDERS

        SCATTER --> MERGE["Merge (Layer 2)\nidentifier (ISRC/MBID) then\nexact canonical title+artist\nNO version vocab, NO fuzzy threshold"]
        MERGE --> RANK["Rank (Layer 3)\ngate: shares-query-word + browseable\nsort: continuous relevance -> popularity\n-> multi-source -> RRF (k=60)"]
        RANK --> DIVERSITY["EnforceDiversity\n(max 3 per artist in top 10)"]
        DIVERSITY --> COLLAPSE_ART["CollapseArtistDuplicates"]
        COLLAPSE_ART --> DISAMBIG["applyArtistDisambiguation\n(MusicBrainz identity)"]
        DISAMBIG --> ENRICH["enrich top 50\n(artwork only)"]

        ENRICH --> ZERO{zero results?}
        ZERO -->|yes| CORRECT["tryCorrection\n(aggressive vocab correction)"]
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
        PROVIDERS["7 Search Providers"]
        DEEZER["Deezer\n+nb_fan, rank, ISRC\n+StructuredSearcher"]
        LASTFM["Last.fm\n+listeners, albums, top tracks"]
        MUSICBRAINZ["MusicBrainz\n+MBID, ISRC\n+StructuredSearcher"]
        ITUNES["iTunes\n+metadata, genre"]
        SOUNDCLOUD["SoundCloud (yt-dlp)\n+playback_count"]
        AUDIODB["TheAudioDB\n+artist images"]
        YTMUSIC["YouTube Music\n+massive catalog, no auth"]
        PROVIDERS --> DEEZER
        PROVIDERS --> LASTFM
        PROVIDERS --> MUSICBRAINZ
        PROVIDERS --> ITUNES
        PROVIDERS --> SOUNDCLOUD
        PROVIDERS --> AUDIODB
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

The rebuilt `Rank` (Layer 3) sorts by **continuous** relevance — no bands, no tiers,
no intent contract, no quality score. Eligibility gates (shares-query-word +
browseable-source) drop non-matches before sorting.

```
Position  Signal         Direction   Source
────────  ─────────────  ──────────  ──────────────────────
1         Relevance      DESC        max(TokenSortRatio(q,title), TokenSortRatio(q,"artist title")) / 100
2         Popularity     DESC        extras["popularity"] (provider-supplied, max across sources)
3         Multi-source   DESC        len(distinct providers) > 1
4         RRF            DESC        Σ 1/(60 + best_rank) — equal weight, within-tie tiebreak only
5         Subtitle       ASC         alphabetical tiebreak
6         Title          ASC         alphabetical tiebreak
```

Enrichment (artwork) does not reorder, so there is no post-enrichment rerank.

## Diagnostic Logging

Enable with `LOG_LEVEL=debug`. Each pipeline stage emits a structured log entry:

```
search.v2.start           — query
search.v2.provider_failed — provider, status, error
search.v2.correcting      — original, corrected, confidence
search.v2.complete        — query, results, partial, corrected, related_groups
consensus.v2.complete     — artist, total, confirmed, unconfirmed, rejected, responded
```

## File Map

```
internal/discovery/
├── domain/
│   ├── types.go              # SearchResult, SearchQuery, SourceRef, RelatedGroup, enums
│   ├── identity.go           # ArtistIdentityProfile, AlbumVerdict (used by consensus MB check)
│   ├── events.go             # SearchPerformed, ResultClicked
│   └── vocabulary.go         # VocabularyEntry
├── ports/
│   └── ports.go              # Port interfaces (SearchProvider, ArtistContentProvider, ClickSignalProvider, etc.)
├── service/
│   ├── search.go             # Service — search orchestrator (fanOut + mergeRankEnrich + SearchOutput)
│   ├── merge.go              # Merge (Layer 2) — identifier + canonical-title entity resolution
│   ├── rank.go               # Rank (Layer 3) — continuous-relevance sort + eligibility gates
│   ├── enrich.go             # artwork enrichment (top 50, parallel)
│   ├── disambiguation.go     # applyArtistDisambiguation (MusicBrainz identity)
│   ├── search_correction.go  # tryCorrection (zero-result aggressive vocab correction)
│   ├── vocab.go              # ingestVocabulary (learn query + strong results)
│   ├── telemetry.go          # emitSearchEvent (search_performed, async best-effort)
│   ├── diversity.go          # EnforceDiversity, CollapseArtistDuplicates + extras helpers
│   ├── consensus.go          # ConsensusService — multi-provider album consensus + MB contradiction
│   ├── fuzzy.go              # levenshteinDistance (delegates to shared/textnorm; NormalizeForMatch + TokenSortRatio now live in shared/textnorm)
│   ├── correction.go         # CorrectionService (trigram Jaccard + phonetic)
│   ├── query_clean.go        # CleanQuery (strip YouTube noise)
│   ├── metaphone.go          # DoubleMetaphone, MetaphoneKey (phonetic codes)
│   ├── suggest.go            # SuggestService (prefix + fuzzy fallback)
│   ├── vocabulary_refresh.go # Background chart ingestion (6h ticker)
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
