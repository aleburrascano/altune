---
title: "feat: Discovery coverage — direct SoundCloud client + deferred normalization experiments"
type: feat
status: planned
date: 2026-06-21
origin: docs/brainstorms/2026-06-20-discovery-rebuild-architecture.md
depends_on: docs/plans/2026-06-20-003-refactor-discovery-strangler-rebuild-plan.md
---

# feat: Discovery coverage — direct SoundCloud client + deferred normalization experiments

## Why this exists (read first)

The discovery **ranking** rebuild is done and validated: `discovery2` is query-fit-free
(identifiers + canonical equality for merge; one continuous token-sort relevance signal for rank)
and it **beats v1 on the product bar** (full-catalog head-to-head 2026-06-21: **v2 99.0% top-3 vs v1
98.9%**, 18 vs 20 failures). See plan 003's "Course correction" and ADR-0007's strangler addendum.

So the algorithm is no longer the bottleneck. The residual failures are **coverage** — *"the exact
track is not in the candidate set at all."* The honest analysis of the 17 shared (v1∩v2) failures:

- **~7 are eval-matcher artifacts** — the symbol-only artist `¥$` normalizes to empty, so the matcher
  can't validate a *correct* result. (Fix is eval tooling — see Deferred §B.)
- **~5 are coverage gaps for unreleased music.** *(Confirmed by the product owner:* these — e.g. the
  "Lil Tecca" entries, "Ken Carson — Olympics" — are **unreleased/leaked tracks manually imported**
  into the library from download-link sheets as tagged mp3s. No mainstream provider *indexes* them,
  so discovery can't surface them.) The doctrine: **deterministic coverage is the product; ML is a
  contingent upgrade — ML cannot rank or find what was never fetched.** Coverage is strictly upstream
  of ML. So coverage comes before plan 004 (ML).

The lever: **SoundCloud.** Unreleased/leaked tracks and the underground long tail live there. But the
current adapter is shallow, and SoundCloud's official API is paid (we won't pay).

## Current state of the SoundCloud adapter

`internal/discovery/adapters/providers/soundcloud.go` shells out to **`yt-dlp scsearch5:<query>`**
with `--flat-playlist`. So we already use reverse-engineered access (via yt-dlp), not the paid API —
but it's thin:

- **only 5 results** (`scsearch5`),
- **flat metadata** (`--flat-playlist` — no genre/tags/ISRC, coarse `playback_count`),
- **slow** — a Python subprocess spawned per search (the 5s `SearchTimeout` exists for this).

## The work: a direct `api-v2` SoundCloud client

Build a dedicated SoundCloud client that hits SoundCloud's **internal** `api-v2.soundcloud.com` — the
same JSON API the website uses — directly, instead of via yt-dlp. This is the same approach the
`ytmusic` library (already a dependency) takes for YouTube Music, and what yt-dlp does internally.

It is an **outbound adapter swap behind the existing `SearchProvider` port** — no new service, no
architecture change (hexagonal slot already exists). Non-query-fit: pure coverage/data, no ranking
constants.

**Capabilities to add over `scsearch5`:**
1. **`client_id` auto-resolution** — fetch the SoundCloud web page, parse the JS bundle for the
   current `client_id` (it rotates/expires); cache it; re-resolve on 401/403.
2. **Deeper search** — `GET /search/tracks?q=&limit=&offset=` with pagination, not just 5.
3. **Richer metadata** — full track objects (genre, tags, accurate playback/likes/reposts, artwork,
   permalink) → better relevance + dedup signals.
4. **`GET /resolve?url=`** — paste a SoundCloud link, get the track. Directly serves the product
   owner's workflow ("I find a sheet of links and import them"); also a candidate for the acquisition
   path.
5. **Speed** — an HTTP call, no subprocess; tighten the timeout back toward the fan-out default.

**Verify:** run `discoveryeval --query "Ken Carson Olympics"` (and similar underground/unreleased
queries) against the new client vs the `scsearch5` adapter; confirm depth + that the underground
long tail surfaces. Then a full `--pipeline v2` eval to confirm no regression and measure coverage
gains. Gate: hold ≥ the 2026-06-21 baseline (99.0% top-3).

**Honest costs / risks:**
- **`client_id` rotation** — breaks when SoundCloud changes their site; ongoing maintenance (the tax
  yt-dlp pays too). Mitigate with auto-resolution + a fallback to the yt-dlp adapter.
- **ToS grey area** — reverse-engineering the internal API is against ToS; accepted for self-hosted
  personal/family use (the product's posture), but named explicitly.
- **Rate limits** — `api-v2` throttles; reuse the existing per-provider cache + circuit breaker.
- **Still public-only** — truly private/unlisted tracks never surface; publicly-uploaded leaks do.

## Deferred experiments (carried from the 2026-06-21 session)

### A. textnorm symbol-keeping (needs eval validation)
The two query-fit word lists (leading articles, feature tokens) were **already removed** from
`textnorm.NormalizeForMatch` (committed 2026-06-21). The proposed **symbol-keeping** (so `¥$` doesn't
normalize to empty) was **deferred**: it also keeps hyphens, which glues separator quirks
(`07-The Best …`) into one token and breaks tokenized matching (the eval matcher's track-number-prefix
handling + v1 intent both regressed in unit tests). It needs a careful eval run, not a rushed change.
If pursued, the principled version distinguishes *separator* punctuation (→ space, preserves
tokenization) from *symbol content* (kept), rather than blanket-keeping everything.

### B. Eval-matcher hardening for symbol-only artists
The `¥$` failures are an **eval-tooling artifact**, not a pipeline bug — `matchesEntity`
(`library_eval.go`) requires the normalized artist tokens to match, but `¥$` → empty. Harden the
matcher to validate symbol-only artists (e.g. a symbol-preserving fallback when the normalized artist
is empty). This recovers ~7 phantom failures and reveals v2's true top-3 ≈ 99.4%. Eval tooling only —
does **not** touch the live pipeline.

## Scope boundaries
- No ML (plan 004 — strictly after coverage).
- No new top-level service — SoundCloud is one outbound adapter.
- No ranking changes — ranking is settled (query-fit-free, beats v1).
- The acquisition pipeline (yt-dlp→OCI) is a separate thread; the `resolve?url=` endpoint may feed it
  later but is out of scope here.

## Sources
- Coverage doctrine + scraping strategy: `docs/brainstorms/2026-06-20-discovery-rebuild-architecture.md` §5, §7.
- Ranking rebuild + verdict: `docs/plans/2026-06-20-003-...strangler-rebuild...md`; ADR-0007 strangler addendum.
- Current adapter: `services/go-api/internal/discovery/adapters/providers/soundcloud.go`.
- Eval harness: `cmd/discoveryeval` (`--query` diagnostic mode + `--pipeline v1|v2`).
