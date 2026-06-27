# Discovery tail-noise demotion — brainstorm

Status: **implemented (default-off) + A/B run 2026-06-27.** Result: safe + beneficial.

## Results (A/B 2026-06-27, deterministic fixtures)

Mechanism: `isLowConfidenceTail` + `rankWith`/`rankPipelineWith`, gated by
`WithTailDemotion()` / `TAIL_DEMOTION_ENABLED`. A/B = replay identical recorded
fixtures (exact 150 entities, hard 163 single-token), demotion off vs on.

**Mechanism fires:** 157/163 hard queries had ≥1 demoted result; 2728/7591
result-rows (36%) flagged as UGC-single-source-no-id noise and pushed below all
corroborated results.

**Safety — target-recall unchanged (no regression):**
| corpus | metric | off | on |
|---|---|---|---|
| exact | top-3 | 98.7% | 98.7% |
| hard  | top-3 | 76.7% | 76.7% |

The owned-niche-track-demoted risk did NOT materialize: no library target was a
demoted UGC result pushed out of top-3. (Replay absolutes sit below the committed
~81% hard baseline because the recording baked in some provider timeouts —
identical both arms, so the *delta* is the valid signal.)

**Benefit — visible top-5 noise (hard corpus):**
| | noise in top-5 | queries w/ noisy top-5 |
|---|---|---|
| off | 110 | 61/163 (37%) |
| on  | **46** | **21/163 (13%)** |

**58% of visible top-5 noise cleared.** Residual is concentrated in
genuinely-underground queries (e.g. `"hilarious"`: 8 results, 6 UGC) where no
cleaner result exists to promote — demotion correctly no-ops there rather than
fabricating quality. Where clean results exist (`"doja"`: 16 noise → 0 in top-5),
the tail is swept.

**Key learning:** target-recall (the standard eval) is BLIND to this change — the
noise sits below the answer, so cleaning it doesn't move "is the answer in top-3."
Measuring the benefit needed a tail-quality metric ("noise in top-5"). Worth
promoting that to a tracked `discoveryeval` signal.

**Recommendation:** ship. Flip `TAIL_DEMOTION_ENABLED=true` (or make default-on)
after one clean-fixture re-record confirms no regression vs committed
`baselines.json`. Code is in, default-off, all tests green.

---

Created 2026-06-27.

## Problem (sized from prod telemetry, 195 non-empty searches, Jun 25–27)

The discovery result **tail** is dominated by single-source noise — UGC reuploads,
"type beat" producer uploads, reaction videos, and Last.fm scrobble fragments —
that carry a (often wrong) thumbnail but **no metadata** (no album/year/ISRC).

| Signal | Value |
|---|---|
| Position-0 is multi-source (clean top) | 71% |
| Tail positions (1+) that are single-source | 947 / 1543 = **61%** |
| Single-source tail from Last.fm + SoundCloud | **72%** (Last.fm 40%, SoundCloud 32%) |
| Deezer/iTunes/MusicBrainz share of single-source tail | 28% (mostly legit-but-unmerged, not junk) |
| "Clean top, junk tail" (pos-0 multi + ≥3 single-source in pos 1–4) | **30% of searches** |

Representative (real prod query `"rest in bass encore"`): pos-0 is the correct
multi-source album; positions 1–19 are SoundCloud type-beats/edits + Last.fm
fragments, every one single-source, none with album/year/ISRC.

### Assumption that died during investigation
The junk results **have artwork** (SoundCloud gives every upload a thumbnail). So
filtering/ranking on *artwork presence* would not remove them, and "get more
artwork" is moot (coverage is already saturated by thumbnails). The real
distinguishing signal is **metadata absence + single-source + UGC provider**, not
artwork. This is why the original "filter results without covers" idea was rejected.

## Root cause

Multi-source is already a ranking signal, but only as a **tiebreak after
relevance**. Junk titles stuff the query words (`"…rest in bass encore type beat"`),
scoring high on token-similarity, so they out-rank on relevance before multi-source
breaks the tie. (Rank order today: relevance → popularity[inert] → multi-source →
RRF → title tiebreak — see `internal/discovery/CLAUDE.md`.)

## Hypothesis to test

A **uniform demotion prior** for results that are *single-source from a UGC/scrobble
provider (SoundCloud, Last.fm) AND carry no identifier (no ISRC/MBID/album)*.

Why this shape:
- **It only re-orders when a non-UGC alternative exists** to rise above the noise.
  For a genuinely underground query where every result is SoundCloud-only, the
  demotion is uniform → **no-op on relative order**. This is what protects the
  diverse/niche tail ([[multi-user-music-diversity]]).
- Provider-class (curated catalog vs UGC/scrobble) is a **stable** distinction, not
  a rotting word-bank or special-case list (which the discovery discipline bans).

Open fork to A/B both ways:
- **(a) Provider-class trigger** — single-source from SoundCloud/Last.fm.
- **(b) Metadata-completeness trigger** — no identifiers (generalizes beyond provider;
  reuses the `completenessOf` notion already in `merge.go`).
- Likely best: the **intersection** (single-source + UGC + no-identifier).

## The risk that gates it (do not skip)

This is the **popularity-regression failure mode** in new clothes: if an owned niche
track only surfaces as single-source UGC, demoting it below a famous multi-source
track regresses the **bare-single-token-title (hard) corpus**. The popularity
attempt (2026-06-24) looked elegant and regressed the hard eval 81%→75% on a
same-sample A/B. Same trap here.

→ The hard corpus is the guard. Ship only if a **same-sample A/B** shows the exact
corpus eval (top-3) does not regress, gated against committed `baselines.json`.

## A/B plan (eval-gated, deterministic)

1. **Baseline**: run `discoveryeval -mode eval` and `-mode eval -corpus hard`,
   record current top-3. Use **recorded fixtures** (`-fixtures <dir>` replay) for a
   deterministic, identical sample — no live providers in the A/B.
2. **Implement** the demotion as an opt-in ranking option (functional option on the
   service / a flag in the rank pipeline), default off.
3. **A/B on the identical fixture sample** (`-limit`, no `-random`): off vs on.
4. **Gate**: keep only if exact-corpus top-3 holds AND tail single-source rate drops.
   The hard corpus must not regress.
5. If it passes, also sanity-check the canonical spot-checks (CLAUDE.md) blended +
   filtered.

## Eval keys note

- The A/B itself needs **no new keys** — run against recorded fixtures (replay).
- Live keys matter only for `-record` (refresh fixtures) and live smoke runs. In the
  **ranking** search path the only app-shared *key* is **Last.fm**; MusicBrainz is
  IP/User-Agent throttled (no per-key separation), SoundCloud resolves its own
  client_id, Deezer/iTunes are keyless. Artwork-only keyed providers (Discogs,
  Fanart.tv, Genius) are skipped in ranking eval (`rankingOnly`).
- Separate eval keys need no code change: real env vars override `.env`
  (`godotenv.Load` does not overwrite existing env), so
  `LASTFM_API_KEY=evalkey go run ./cmd/discoveryeval …`, or source a `.env.eval`.

## Secondary findings (separate threads)

- **Non-determinism**: identical queries seconds apart returned different
  top results (`"perfect night holiday remix"` ×3, `"lucki"` ×2) — the
  fanOut-completion-order / cache-warm issue in [[discovery-search-consistency]].
  Independent of this work but compounds the "looks wrong" perception.
- **No behavioral telemetry**: `discovery_events` holds only `search_performed`
  (no play/click/library_add), so "wrong pick" rate can't be measured yet — the
  `RecordEventService` client events aren't landing.
- **Possible new coverage signal**: "tail single-source rate" is a useful quality
  gauge; could become a `discoveryeval` signal so Mission Control tracks it over time.
