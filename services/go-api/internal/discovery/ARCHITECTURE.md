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

        SEARCH_SVC --> NORMALIZE["NormalizeForMatch(query)"]
        SEARCH_SVC --> PRE_CORRECT["Pre-query correction\n(trigram + phonetic vs vocabulary)"]
        PRE_CORRECT --> CLEAN["CleanQuery\n(strip 'official video', 'lyrics', etc.)"]
        CLEAN --> INTENT["DetectIntent(query, vocab)"]
        INTENT --> SCATTER["Scatter to providers"]

        SCATTER --> |"1500ms timeout each\n+ structured queries\nfor MB/Deezer when\nintent detected"| PROVIDERS

        SCATTER --> COLLECT["Collect results + statuses"]
        COLLECT --> FUSE["FuseAndRank"]

        FUSE --> MERGE["Identifier merge\n(ISRC / MBID / artist name)"]
        MERGE --> POP_NORM["NormalizePopularity\n(log-scale → 0-100)"]
        POP_NORM --> RECENCY["Recency boost\n(×1.1 if ≤30 days)"]
        RECENCY --> SCORE["Score: relevance + intent boost\n+ exact/prefix/multi-field bonuses\n+ RRF"]
        SCORE --> GATE["Gate: word-share + browseable source"]
        GATE --> SORT["Sort: relevance-band(0.05) → demoted →\nmulti-source → popularity →\nquality → RRF → alpha"]
        SORT --> COLLAPSE["CollapseVersions\n(strip remix/live suffixes, group)"]

        COLLAPSE --> ENRICH["Enrich top 25\n(artwork only)"]
        ENRICH --> RERANK["Rerank\n(same key minus quality)"]

        RERANK --> ZERO{zero results?}
        ZERO -->|yes| CORRECT["CorrectionService\n(trigram + phonetic vs vocabulary)"]
        CORRECT -->|corrected query| SCATTER
        CORRECT -->|no match| ZERO_RESP["Return empty + no correction"]
        ZERO -->|no| LIMIT["Limit to user's count"]

        LIMIT --> HISTORY_SAVE["Save search history"]
        LIMIT --> VOCAB_INGEST["Ingest to vocabulary\n(top 5 + separate artist entries)"]
        LIMIT --> RESPONSE["Return SearchOutput\n(+ corrected_query if applicable)"]
    end

    subgraph "Providers (adapters/providers/)"
        PROVIDERS["6 Search Providers"]
        DEEZER["Deezer\n+nb_fan, rank, ISRC\n+StructuredSearcher"]
        LASTFM["Last.fm\n+listeners"]
        MUSICBRAINZ["MusicBrainz\n+MBID, ISRC\n+StructuredSearcher"]
        ITUNES["iTunes\n+metadata"]
        SOUNDCLOUD["SoundCloud (yt-dlp)\n+playback_count"]
        AUDIODB["TheAudioDB\n+artist images"]
        PROVIDERS --> DEEZER & LASTFM & MUSICBRAINZ & ITUNES & SOUNDCLOUD & AUDIODB
    end

    subgraph "Vocabulary (Redis)"
        VOCAB[("Vocabulary Store\nterms sorted set\ntrigram index\nmetaphone index\nentry hashes")]
        CHARTS["Chart Refresh\n(Deezer + Last.fm charts)\nevery 6 hours"] -->|BulkAdd| VOCAB
        VOCAB_INGEST -->|Add| VOCAB
        SUGGEST_SVC -->|SuggestByPrefix\nFindClosest| VOCAB
        CORRECT -->|FindClosest\n(trigram + phonetic)| VOCAB
        INTENT -->|SuggestByPrefix| VOCAB
    end

    subgraph "Caches (Redis)"
        QUERY_CACHE[("Query Cache\n10min TTL")]
        ARTWORK_CACHE[("Artwork Cache\n14d pos / 1d neg")]
        POP_CACHE[("Popularity Cache\n7d pos / 2h neg")]
        MBID_CACHE[("MBID Cache\n30d pos / 1d neg")]
    end
```

## Ranking Key (sort order)

```
Position  Signal              Direction   Source
────────  ──────────────────  ──────────  ──────────────────────
1         Relevance band      DESC        TokenSortRatio (0.1 granularity)
2         Demoted             ASC         record_type not in {album,single,ep}
3         Multi-source        DESC        len(providers) > 1
4         Popularity          DESC        NormalizePopularity (0-100, log-scale)
5         Quality score       DESC        completeness + agreement + tier + fetch
6         RRF                 DESC        Σ 1/(60 + rank) per provider
7         Subtitle            ASC         alphabetical tiebreak
8         Title               ASC         alphabetical tiebreak
```

After enrichment, `Rerank` uses the same key minus quality score.

## File Map

```
internal/discovery/
├── domain/
│   ├── types.go              # SearchResult, SearchQuery, SourceRef, enums
│   ├── events.go             # SearchPerformed, ResultClicked
│   └── vocabulary.go         # VocabularyEntry
├── ports/
│   └── ports.go              # 12 port interfaces (SearchProvider, VocabularyStore, etc.)
├── service/
│   ├── search_music.go       # SearchMusicService — main orchestrator
│   ├── dedup.go              # FuseAndRank, Rerank, CollapseVersions, recency boost
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
│   ├── get_album_tracks.go   # Album content fetch
│   ├── get_artist_content.go # Artist top-tracks/albums fetch
│   └── url_router.go         # URL-paste provider detection
└── adapters/
    ├── handler/
    │   └── discovery_handler.go  # HTTP routes (search, suggest, history, clicks, content)
    ├── providers/
    │   ├── deezer.go         # Search + Charts + Artwork + Content
    │   ├── lastfm.go         # Search + Charts
    │   ├── musicbrainz.go    # Search (recordings, artists, releases)
    │   ├── itunes.go         # Search
    │   ├── soundcloud.go     # Search via yt-dlp
    │   ├── theaudiodb.go     # Search (artists) + Artwork
    │   ├── genius.go         # Artwork
    │   ├── fanarttv.go       # Artwork (MBID-based)
    │   ├── artwork_chain.go  # Chained artwork resolver
    │   └── wikidata.go       # MBID resolution (Deezer → MB)
    ├── cache/
    │   ├── query_cache.go        # 10min per-provider query cache
    │   ├── artwork_cache.go      # 14d artwork cache
    │   ├── popularity_cache.go   # 7d popularity cache
    │   ├── mbid_cache.go         # 30d MBID cache
    │   ├── vocabulary_store.go   # Trigram-indexed vocabulary (prefix + fuzzy)
    │   └── fetch_success.go      # Provider reliability tracking
    └── persistence/
        ├── history_repo.go   # Search history (Postgres)
        └── click_repo.go     # Click tracking (Postgres)
```
