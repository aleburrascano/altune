# View result detail

> Spec for `view-result-detail` — version 1, drafted 2026-05-30.
> Authors: solo + Claude.
> Status: Clarify-gated.

## Problem

A user searches in Discover, finds a track / album / artist, taps it — and nothing happens.
Tapping records click telemetry but deliberately does not navigate (locked by discover-music-v2
AC#6; discover-music-v3 defers the "Track detail screen" to a successor). There is also no way to
move a discovered result into the Library tab, which today cannot be populated at all. The user
can find music but can neither inspect it nor keep it.

## User value

Tapping any discovery result opens a detail screen with that item's information. On a **track**
detail screen the user can **Save** it, and it immediately appears in their Library — the first
bridge from Discover into a personal collection. This is the front door to the larger owned-library
vision (acquire audio, stream it back) without committing to that backend yet.

## Scope tier / MVP cut

Minimal tier. The detail screen is fed entirely by the `SearchResult` already in hand at tap time
(no per-item backend fetch). The only write is a metadata-only Save.

- **Minimal (ship this):** a polymorphic detail screen for all three kinds (track / album / artist),
  reachable by tapping a Discover result; track detail has a working **Save-to-library** (metadata
  only); saved tracks appear in the Library tab marked `pending`.
- **Named deliverables this spec requires** (not optional infra — the write path needs them):
  1. An Alembic migration extending the `tracks` table with `artwork_url`, `acquisition_status`,
     and a `dedup_key` column + a `UNIQUE(user_id, dedup_key)` index (the idempotency backstop).
  2. Extending the existing `GET /v1/tracks` read contract (DTO + mobile type + `LibraryRow`) to
     carry `acquisition_status`, so `pending` survives a refetch (not optimistic-only).
- **Deferred to post-launch:** audio acquisition (yt-dlp → OCI), full-track streaming + 30s preview
  playback, album tracklist / artist discography data and lateral browse, edit / delete /
  re-download, lyrics, per-item enrichment fetch, client-supplied idempotency keys.
- **Justified exceptions:** server-side **save dedup** is pulled in now — needed because a track
  saved twice creating duplicate Library rows is a visible correctness bug on first use, and mobile
  retries make double-submits likely [vault: wiki/concepts/Idempotency.md]. Implemented as a DB
  unique constraint (natural idempotency), not a key store.

The Acceptance criteria below cover the minimal tier only.

## Acceptance criteria

1. **AC#1** — Given a Discover results list, when the user taps any result (track / album / artist),
   then a detail screen pushes over the tab bar (tab bar hidden), an explicit header back control
   (`detail-back`; the app uses `headerShown: false`, so gesture-back alone is undiscoverable)
   returns to Discover. The existing click record fires fire-and-forget per ADR-0007; navigation
   happens unconditionally and does not await it.
2. **AC#2** — Given a tapped result, when the detail screen renders (`detail-header`), then it shows
   artwork (circular for artist, square otherwise), title, subtitle, and a kind label — using only
   the in-memory `SearchResult` passed in, with no additional network request.
3. **AC#3** — Given a **track** result, when its detail renders, then for each of the provider
   `extras` keys actually present it shows: `duration_seconds` (rendered M:SS), `album`, `isrc`, and
   `popularity` (Deezer-only, may be absent); a key absent from `extras` is omitted entirely (no
   blank row). The exact key set is the provider adapters' track-extras contract
   (`duration_seconds`, `album`, `isrc`, `popularity`) — to be re-verified against the adapters in
   the plan; no field is invented.
4. **AC#4** — Given an **album** or **artist** result, when its detail renders, then it shows the
   header plus available fields (albums may carry `year` / `track_count` in `extras`) and an explicit
   placeholder section (`detail-tracklist-placeholder` / `detail-discography-placeholder`), and
   renders without throwing when `extras` is empty. No Save action is present.
5. **AC#5** — Given a track detail screen, when the user taps **Save** (`detail-save`), then
   `POST /v1/tracks` is called with a body mapped from the `SearchResult` as: `title ← title`,
   `artist ← subtitle`, `album ← extras['album']` (nullable), `duration_seconds ← extras['duration_seconds']`
   (nullable), `artwork_url ← image_url` (nullable); the server creates a `Track` with
   `acquisition_status = pending` and returns it (HTTP 201).
6. **AC#6** — Given a Save in progress, when the mutation is pending, then the Save button shows a
   disabled/loading state (blocks double-submit) and the track appears in the Library tab
   immediately (optimistic, with `pending` status); on failure the optimistic entry is removed and an
   error Banner is shown on the detail screen and the button returns to idle.
7. **AC#7** — Given a track already saved by this user, when the same result is saved again, then no
   duplicate Library row is created: the server computes `dedup_key` = the lower-cased,
   whitespace-trimmed/collapsed join of `title`, `artist`, and `album` (null album → empty string),
   and the `UNIQUE(user_id, dedup_key)` constraint makes the second insert return the **existing**
   track (HTTP 200) rather than a second row. The optimistic UI reconciles to the existing track,
   not a duplicate.
8. **AC#8** — Given the `Track` aggregate, when constructed, then `acquisition_status` defaults to
   `pending`, `artwork_url` is optional, and the existing non-empty title/artist and non-negative
   duration invariants still hold.
9. **AC#9** — Given a track result whose `subtitle` (artist) is null/empty, when its detail renders,
   then the Save button is disabled (the non-empty-artist `Track` invariant cannot be satisfied), so
   no invalid `POST` is attempted.
10. **AC#10** — Given saved tracks, when the Library tab fetches `GET /v1/tracks`, then each
    `TrackResponse` carries `acquisition_status`, and `LibraryRow` renders the `pending` marker — so
    the status persists across an app refetch, not just during the optimistic window.

## Out of scope

- Audio acquisition (yt-dlp), transcoding, OCI storage — `acquire-track` spec.
- Full-track streaming and 30s preview playback, audio player, lock-screen controls — `stream-playback` / `preview-playback` specs.
- Album tracklists, artist discography, and lateral browse (album→tracks, artist→albums) — `catalog-browse` spec.
- Edit / delete / re-download / favorite of a saved track.
- Lyrics, credits, bios, related artists.
- Provider link-outs ("open in Deezer/iTunes"); providers are metadata-only.
- Deep-linking directly to a detail screen (cold start). Detail is reachable only from a live
  Discover tap; on cold start with an empty handoff, the route redirects to Discover.

## Design considerations

- [vault: wiki/concepts/Aggregate.md] — `Track` is the aggregate root of the catalog context; the
  new `artwork_url` field and `acquisition_status` value object are added to the root, which keeps
  enforcing its invariants. Save goes through the root, not around it.
- [vault: wiki/concepts/Idempotency.md] — `POST /v1/tracks` is a non-idempotent create; v1 uses
  **natural idempotency enforced by a DB unique constraint** (`UNIQUE(user_id, dedup_key)`), not an
  application check-then-insert (which races) and not a client idempotency-key store (deferred infra).
  On constraint conflict the handler returns the existing track (200) instead of erroring.
- [vault: wiki/concepts/Eventual Consistency.md] — AC#6 is the Optimistic-UI pattern: show the
  expected row, reconcile/rollback on the server response. The brief inconsistency window is a
  deliberate choice; a dedup hit reconciles to the existing track, not a second row.
- [vault: wiki/concepts/REST.md] / [vault: wiki/topics/API Design Overview.md] — `POST` to the
  existing `/v1/tracks` resource collection, mirroring the current `GET /v1/tracks` shape.

High-level approach:

- This is a **mixed** path: a read-only screen (no backend) plus a write (Save) in the `catalog`
  bounded context.
- **Result handoff (named, not "context or cache"):** a small in-feature module
  (`apps/mobile/src/features/detail/detail-handoff.ts`) holds the last-tapped `SearchResult` set on
  tap; `DetailScreen` reads it on mount. Navigation is `router.push('/detail')`. If the handoff is
  empty (cold start / deep link), the screen redirects to Discover. Chosen over query-cache readback
  for simplicity and a deterministic cold-start failure mode.
- It **does** introduce a new value object (`AcquisitionStatus`, single member `pending` =
  "saved to library; audio not yet acquired") and a new use case + outbound port method
  (`AddTrackToLibrary` / `TrackRepository.add`); it extends the existing `Track` aggregate. New term
  goes in `docs/ubiquitous-language.md` with that exact definition.
- It **does not** introduce a new external dependency — no ADR required. (No audio library yet.)
- Mobile: a new vertical slice `apps/mobile/src/features/detail/`.

## Dependencies

- **Bounded contexts**: `catalog` (Track aggregate + `GET /v1/tracks` exist; this spec adds the write
  side and a migration); `discovery` (provides `SearchResult`).
- **Other features**: `discover` (tap entry point; tap handler gains navigation + the handoff write);
  `library` (reuses the `['library']` query for optimistic insert + display of `pending`).
- **External services**: none.
- **Library/framework additions**: none. Requires one Alembic migration (see Scope deliverables).

## Risks / open questions

- **Risk**: sparse album/artist screens look unfinished — accepted; `catalog-browse` fills them; placeholders set expectation.
- **Risk**: dedup normalization could collapse two genuinely distinct recordings with identical title/artist/album — mitigation: album is in the key; acceptable at pre-launch scale.
- **Risk**: in-memory handoff lost on cold start to the detail route — mitigation: deep-linking is out of scope; empty handoff redirects to Discover (AC-covered behavior).
- **Risk**: track-extras key names drift from what adapters emit — mitigation: AC#3 names the contract and the plan re-verifies against the Deezer/iTunes/MusicBrainz adapters before writing tests.

## Telemetry

- **Log events**: existing `ResultClicked` preserved. Add domain event `TrackAddedToLibrary`
  (past-tense, immutable, carries `occurred_at`), raised by the `AddTrackToLibrary` use case on a
  fresh save. A dedup hit raises no `TrackAddedToLibrary`; it is counted separately (below).
- **Metrics**: save success / failure counts; dedup-hit count (emitted on the 200-existing path).
- **Alerts**: none pre-launch.

## Related

- `[vault: wiki/concepts/Aggregate.md]`, `[vault: wiki/concepts/Idempotency.md]`, `[vault: wiki/concepts/Eventual Consistency.md]`, `[vault: wiki/concepts/REST.md]`, `[vault: wiki/topics/API Design Overview.md]`
- Related ADRs: ADR-0007 (discovery), ADR-0009 (visual refresh) — no new ADR required.
- Predecessor specs: `docs/specs/view-library/spec.md` (named this as the deferred `view-track-detail` successor), `docs/specs/discover-music-v3/spec.md`.
- Successor specs (planned, not written): `acquire-track`, `stream-playback`, `preview-playback`, `catalog-browse`.
