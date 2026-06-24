# Backend expansion — ideation

**Date:** 2026-06-22
**Focus:** "How can we expand the backend?" — open-ended, repo-grounded on `services/go-api/`.
**Status:** ideation only. Survivors route to `/ce-brainstorm`, not directly to planning.

## Grounding snapshot (verified this session)

Bounded contexts today: `discovery` (the mature one — multi-provider fan-out, merge→rank→diversity,
consensus, 5 enrichment sources, suggest, vocab refresh, history, clicks, related, events),
`catalog` (tracks + playlists + streaming + dedup + audio store), `acquisition` (yt-dlp →
OCI, background scheduler, retry), `playback` (queue snapshot), `auth` (Supabase JWT), `shared`.

Three confirmed gaps that anchor the strongest ideas:
- **Audio bytes proxy through the Go process** — `stream_handler.go` reads from the store and
  `http.ServeContent`s it. The server is on the bandwidth path for every play.
- **No user-side library state** — no favorites, play counts, or listening history. `Library`,
  `Play`, `Favorite` are still "Future" in the ubiquitous language.
- **Enrichment is ephemeral** — MB/Discogs/Last.fm/Deezer/lyrics write only to Redis caches,
  never back into the `Track`. Saved tracks stay metadata-thin even though the data was fetched.

---

## Survivors (ranked)

### 1. Pre-signed OCI streaming — take the server off the bandwidth path
**Warrant (direct):** `stream_handler.go:49` proxies bytes via `http.ServeContent`; the OCI object
store adapter already exists. **Why it matters:** every play currently flows audio through the Go
process — and the documented ngrok bandwidth exhaustion (free plan hit 2026-06-14) is exactly this
cost. Hand the client a short-lived pre-signed OCI URL (`HasOCIS3()` path) and `302` to it;
fall back to proxying only for the filesystem store. Server CPU/egress drops to near zero for the
hot path. Smallest change with the biggest operational payoff.

### 2. A real `library` context — favorites, play counts, listening history
**Warrant (direct):** the glossary lists `Library`/`Play`/`Favorite` as Future; no catalog code
implements them. The detail-screen memo notes playlists/favorites/playback must wire back into the
detail screen. **Why it matters:** this is the product's missing middle. Discovery → acquisition →
*library* → playback is the loop, and the library node is empty. A new bounded context owning
`Favorite` (boolean marker), `Play` (registered at a listen threshold), and a recently-played read
model unblocks "Liked", "Recently played", play-count sorting, and is the substrate every later
personalization feature reads from.

### 3. Enrich-on-save — turn ephemeral enrichment into durable catalog metadata
**Warrant (direct):** `Track` already carries optional `year`/`genre`/`album_artist`/`isrc`; the
enrichment services exist but persist only to Redis. **Why it matters:** compounding leverage. When
a track is saved, run the enrichment you already fetch and write genre/year/label/MBID onto the
`Track`. The owned library becomes self-describing and offline-complete, and it unlocks library-wide
browse (by genre, year, decade, label) with zero new provider calls. One write turns sunk
enrichment cost into a permanent asset.

### 4. Graph-based recommendations — "Because you saved X" without ML
**Warrant (reasoned + direct):** the Last.fm similar-artist graph and SoundCloud related-tracks are
already wired (`relatedProviders`, `LastFmEnrichment.similar`); the ML approach is explicitly
deferred, not chosen. **Why it matters:** recommendations are the obvious next surface after
discovery, and the app serves a household with deliberately diverse tastes — a genre-agnostic graph
walk (saved seeds → similar/related → filter what's already owned) fits that better than a trained
model and ships now. Lands the value ML was deferring without the team-size cost.

### 5. Acquisition correctness via audio fingerprinting
**Warrant (direct):** `AcquisitionStatus` enforces `ready ⇔ audio_ref`, and MusicBrainz is already
integrated. **Why it matters:** yt-dlp returns *a* result, not necessarily *the* track — for an
owned-library product, silently saving the wrong/garbage audio is the worst failure. Fingerprint
the download (Chromaprint/AcoustID) and verify against the expected MBID before flipping to
`ready`; mismatch → `failed` with a real `failure_reason` instead of a false success. Closes the
acquisition loop honestly.

### 6. Search-result caching on the fan-out
**Warrant (reasoned):** discovery fans out to 7+ providers per query; enrichment is cached but the
search itself is not. **Why it matters:** ambiguous single-word queries (the documented hard case)
and repeat queries re-pay the full multi-provider cost every time. A short-TTL Redis cache keyed on
`(query_norm, kinds)` cuts provider load and latency with no ranking change — and reduces exposure
to provider rate limits. Cheap, isolated, reversible.

### 7. Release radar — new releases from artists already in the library
**Warrant (direct):** `VocabularyRefreshService` already polls charts every 6h; artist-content
(discography) endpoints exist. **Why it matters:** reuses the periodic-poll + discography infra to
answer "what's new from artists I own" — a retention surface that's genre-agnostic and multi-user by
construction. Leverage on infrastructure already running.

---

## Considered and set aside (with reasons)

- **Click-through feedback into ranking** — `SearchClick` data is collected and unused, so the
  leverage is real, but the ranking pipeline is explicitly fragile ("don't add layers without
  testing removal") and a cross-user learned signal is hard to eval against the top-3 gate. Revisit
  once #2/#6 exist and there's an offline eval harness. *Promising but risky now.*
- **Prometheus metrics + OTel tracing** — genuinely absent (only slog + correlation id), and the
  observability rule mandates it. Set aside as *enablement, not expansion* — worth doing but it
  doesn't grow product surface, and the backend is already structurally tight.
- **Per-user rate limiting / edge admission control** — real for a publicly exposed multi-user
  self-hosted app, but it's hardening, not expansion. Fold into #1/#6 when those ship.
- **Scrobble-out to Last.fm** — niche, strictly depends on #2's `Play`. A rider on the library
  context, not a headline.
- **Library export/backup (M3U + originals)** — fits the owned-library philosophy but low urgency
  for a solo/household user. Hold.
- **Collaborative/shared playlists** — multi-user today means separate libraries; a sharing model
  is heavy design weight for the current stage. Premature.
- **Generic transactional outbox / job runner** — YAGNI; the in-process event bus + acquisition
  scheduler cover current needs.

---

## Suggested next step

The three Tier-1 survivors (#1 pre-signed streaming, #2 library context, #3 enrich-on-save) are the
highest leverage and most grounded. #2 is the biggest, so it's the natural `/ce-brainstorm` target
to scope precisely; #1 is small enough it may go straight to `/feature-spec`.
