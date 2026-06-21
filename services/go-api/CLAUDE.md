# Go API — local rules

Parent rules: `<repo>/CLAUDE.md`, `~/.claude/CLAUDE.md`. This file covers the Go API service and its bounded contexts.

## Discovery pipeline — testing discipline

The discovery search pipeline has been iterated heavily. The #1 risk is **regressions that pass presence tests but fail positioning tests** — "is the correct result somewhere in the top 10" is far too weak a check.

**The product bar is top-3, not strict #1.** The current pipeline (the rebuilt Merge → Rank, ADR-0007 strangler addendum) deliberately optimizes for *"the right answer is visible in the top 3"*, accepting top-1 tradeoffs (e.g. an artist named "Humble" can outrank `HUMBLE.` when both match the query exactly — the Kendrick track sits at #2/#3 and passes). The regression gate is the **top-K library eval** (`discoveryeval -mode eval -top-k 3`); the canonical queries below are a fast spot-check, asserted against the top-3, not #1.

### Before claiming a pipeline change works

1. **Rebuild and restart the server.** Code changes don't take effect until you do:
   ```bash
   cd services/go-api && go build -o ./tmp/api.exe ./cmd/api
   # Stop the old process, start the new one
   ```

2. **Run the positioning test suite.** Use `/test-search` or direct API calls. The correct result must appear in the **top 3** (blended view):
   ```
   "Humble"              → top-3 contains the Kendrick track "HUMBLE."
   "Scorpion"            → top-3 contains the Drake album
   "Bohemian Rhapsody"   → top-3 contains "Queen"
   "Circles"             → top-3 contains the Post Malone track
   "Drake"               → top-3 contains the artist "Drake"
   "Bad Bunny"           → top-3 contains the artist "Bad Bunny"
   "Blinding Lights"     → top-3 contains the Weeknd track
   "Tay-K Megaman"       → top-3 contains "Megaman" by "Tay-K"
   "Kendrick Lamar Humble" → top-3 contains "HUMBLE." by "Kendrick Lamar"
   ```
   Test both blended (no `kinds` param) AND filtered views (`kinds=album`, `kinds=track`). For strict-#1 polish, `track>album>artist` is a held-in-reserve, non-query-fit tiebreak — not the gate.

3. **Test ambiguous single-word queries.** These are the hardest case — "Humble", "Scorpion", "Circles", "Aurora", "DAMN". They match artists, albums, AND tracks. The ranking leans on popularity to surface the right one inside the top 3.

4. **Check provider APIs directly** when debugging unexpected results:
   ```bash
   curl "https://api.deezer.com/search/track?q=Humble&limit=5&order=RANKING"
   ```
   Verify your assumptions about what providers return. Don't assume from memory.

### When auditing or modifying the pipeline

- **Read `ARCHITECTURE.md` first** — it has the Mermaid flow diagram and ranking key table.
- **Question existing stages before adding new ones.** Each session tends to add layers without testing removal. Ask: "if I remove this stage, do the positioning tests still pass?" If yes, the stage isn't earning its keep.
- **No hardcoded workarounds.** If a specific query fails, fix the algorithm — don't add the word to a bank or special-case list. Those rot immediately.
- **Log the math.** Enable debug logging (`LOG_LEVEL=debug`) and check `search.ranking` logs to see computed scores for each result. If a result ranked wrong, the logs show exactly which signal caused it.

### Known pipeline design decisions

- **Popularity > multi-source** in ranking. Artists merge easily (name match) and accumulate sources. Tracks rarely merge (need ISRC/MBID). Without this ordering, niche multi-provider artists beat massively popular single-provider tracks.
- **Deezer rank is higher = more popular.** Not a position. `scoreDeezerRank` uses it directly, not inverted.
- **Albums have no provider popularity data.** Deezer album search returns `nb_fan=0`. The pipeline uses kind-local Deezer position as a fallback (`positionalPopularity`).
- **Pre-correction is disabled.** It rewrote valid queries ("Bohemian Rhapsody" → "bohemian rapsody") from vocabulary pollution. Post-correction (zero-results-only) is sufficient.
- **`ApplyPopularityDominance`** replaced `PromoteKind`. Only fires when a different-kind result has a decisive popularity gap (20+ points or 3x) over the current #1 within the top-5 window.

## Build and test

```bash
cd services/go-api

# Build
go build -o ./tmp/api.exe ./cmd/api

# Unit tests
go test ./internal/discovery/... -count=1

# Vet
go vet ./internal/discovery/...

# Run locally (needs .env with DB/Redis)
./tmp/api.exe
# or with hot reload:
air
```

## Key files

- `internal/discovery/ARCHITECTURE.md` — pipeline flow diagram and ranking key
- `internal/discovery/service/search.go` — `Service` search orchestrator (fanOut + mergeRankEnrich)
- `internal/discovery/service/merge.go` — Merge (Layer 2): identifier + canonical-title entity resolution
- `internal/discovery/service/rank.go` — Rank (Layer 3): continuous-relevance sort + eligibility gates
- `internal/discovery/service/diversity.go` — EnforceDiversity, CollapseArtistDuplicates (post-rank shaping)
- `internal/discovery/adapters/providers/` — one file per provider (Deezer, Last.fm, MusicBrainz, iTunes, SoundCloud, TheAudioDB)

The v1 ranking pipeline (`search_music.go`, `dedup.go`, `intent.go`, `popularity.go`, `quality_scorer.go`) was deleted when the strangler rebuild collapsed back into this package — see the ADR-0007 collapse addendum (2026-06-21). The "Known pipeline design decisions" below that reference `FuseAndRank` / `ApplyPopularityDominance` / `PromoteKind` describe that retired pipeline and are kept only as history.
