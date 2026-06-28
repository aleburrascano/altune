# Spec — Durable identity & deterministic artwork resolution

Status: Accepted (2026-06-28)
Context: discovery
Related: ADR-0011 (identity-based merge), `docs/specs/musicbrainz-enrichment/spec.md`, memory `discovery-search-consistency`

## Problem

Searching an **ambiguous artist name** ("Che" — several distinct artists share it) returns a
result whose artwork is **non-deterministic**: correct on one search, a different artist's photo
or blank on the next, then frozen by the cache. Reproduced via `discoveryeval -query "Che"`:

| Run | Artist `src` | Identity bridge | Artwork path |
|----|----|----|----|
| MB present | `[deezer musicbrainz]` | MBID + 4 bridged ids stamped | identity-first → Discogs (**correct**) |
| MB absent (×3 cold) | `[deezer]` | none | no identity → deezer's own "Che" face / blank |

**Root cause.** Artwork correctness hinges on **MusicBrainz answering synchronously during the
fanOut**. MB is the identity keystone *and* the flakiest provider (≈1 req/s, trips its circuit
under light load). When MB is in the merge, the result carries an MBID; `stampIdentities` bridges
it to provider ids (`xref`); artwork resolves identity-first for the exact entity. When MB drops
out, the result is provider-only with **no MBID**, `stampIdentities` skips it (`if mbid == ""
continue`), and artwork falls back to **name-based** resolution — which for an ambiguous name
returns the wrong entity, or the artist→track fallback grabs an unrelated cover. The **app-wide
cache then freezes** whichever outcome occurred until TTL. This is the "fixed it, then it broke
the next day" the user observed.

Secondary: the artist **discography** is returned in raw provider order with inconsistent years
(Last.fm albums carry no date at all), so the detail screen shows albums out of chronological
order with blank years.

## Goals

1. Artist/album artwork resolves to the **correct entity deterministically**, independent of
   whether MusicBrainz answered on this particular search.
2. The cache never **freezes a low-confidence guess** as if it were authoritative.
3. When identity is genuinely unknown, show an **honest placeholder**, never a same-name stranger.
4. Discography is **ordered newest-first with populated years on the backend**; the client only
   displays.

Non-goals: personalization/ranking changes; new artwork providers; changing the merge algorithm.

## Design

Three composing moves on the identity/artwork side (A is the spine; B and C hang off it), plus an
independent discography fix.

### A — Durable identity store (the spine)

A persisted **reverse identity map** keyed by `(provider, external_id) → { mbid, xref, kind,
resolved_at }`. It records the cross-provider bridge graph the merge *already computes
ephemerally* (`stampIdentities` writes `xref` to `Extras`). Keying on stable **provider ids**, not
fuzzy names, is what makes it correct for ambiguous names: `deezer:12345 → MBID 0a68…` pins the
exact Che, where a `name → mbid` map would mis-pin a different Che. This is the persisted form of
the provider Anti-Corruption Layer [vault: wiki/concepts/Bounded Context.md].

- **Storage:** Postgres table `entity_identity` as durable source of truth (survives Redis
  flushes — the whole point), with a Redis read-through cache in front (Cache-Aside)
  [vault: wiki/concepts/Resiliency Patterns.md]. New `IdentityStore` port; Postgres adapter in
  `adapters/persistence/`, Redis read-through in `adapters/cache/`.
- **Write path:** in `stampIdentities`, whenever MB *is* present and a bridge fires for a result,
  `PersistBridges(ctx, mbid, kind, xref)` writes one row per `(provider, external_id)` (idempotent
  upsert). Every good search permanently teaches the mapping.
- **Read path:** in `enrich.enrichOne`, before choosing the artwork path, look up each result's
  own `(provider, external_id)` (from `result.Sources`) in the store. On hit, attach `mbid` +
  `xref` to the result so identity-first resolution fires **even though MB is absent from this
  fanOut**. Replaces the brittle name-keyed `mbidIndex` lookup at `enrich.go:82-88` as the primary,
  with `mbidIndex` kept as a secondary fallback.

### B — Quality-gate the artwork cache

Stop the cache freezing guesses. Resolution already reports a `source`; extend `resolveArtwork` to
also report a **confidence**: `identity` (resolved via MBID/xref), `name` (name-based), or
`fallback` (artist→track). Cache policy:

- `identity` → long TTL (authoritative).
- `name` / `fallback` → short TTL, and **an identity result always overwrites** a stored
  name/fallback result (never the reverse). A name-based guess can no longer pin the entity.
- Demote the artist→track fallback (`enrich.go:136-139`) to **last resort behind the placeholder**
  decision in C — it is the "confidently wrong" source.

### C — Honest placeholder over a wrong guess

When identity is absent **and** no usable provider-own image exists, emit no artwork and let the
client render a deterministic initials/blank avatar. "Blank but honest" beats "confident but
wrong." A future background pass can backfill once identity is learned. (Client change; smallest
surface — may land as a follow-up slice.)

### Discography (independent fix — backend owns ordering & years)

- **Backend sort:** in `GetArtistContentService.GetAlbums`, sort albums **by release date
  descending before truncation** (between consensus and the `limit` cut), so newest-first is the
  contract and the truncation keeps the newest.
- **Year normalization:** every album result carries a normalized `year` (int) in `extras`,
  derived from `release_date` when present. **Last.fm** `GetArtistAlbums` must populate a year/date
  (from its response where available; otherwise leave absent and let it sort last).
- **Client simplification:** remove the `dzValidated` conditional in `useArtistContent.ts:218`;
  the client always trusts backend order and just displays the `year`.

## Acceptance criteria

1. **AC1 (durable identity):** After one MB-present search for "Che" populates the store, a
   subsequent **MB-absent** cold search resolves the artist identity-first (MBID/xref attached from
   the store) and returns the correct (Discogs/identity) artwork. Verified by `discoveryeval`:
   populate → flush Redis → re-run → artist still carries identity and identity-sourced artwork.
2. **AC2 (no frozen guess):** A name-based/fallback artwork result is never served after an
   identity result exists for the same entity; identity overwrites name in the cache.
3. **AC3 (honest miss):** With no identity and no provider image, the result carries empty
   `image_url` (client placeholder), never a same-name stranger's photo from the track fallback.
4. **AC4 (discography order):** `GetAlbums` returns albums sorted newest-first; every Deezer/iTunes
   album carries a numeric `year`; the client renders years and does not re-sort.
5. **AC5 (regression gate):** `go test ./...` and `go vet ./...` pass; `discoveryeval -mode eval`
   and `-mode merge` baselines do not regress.

## Risks & review gates

- **DB migration is human-reviewed** (project rule `go-database.md`): `008_discovery_entity_identity.sql`
  is written in the repo's migration format and applied to **local dev only** here; production
  application via the migration tool requires human review before deploy.
- Table growth is bounded by catalog size (one row per (provider,external_id) ever seen) — small.
- Ubiquitous-language: add **EntityIdentity** / **IdentityStore** terms in the same change.

## Implementation status (2026-06-28)

**Shipped (backend green: `go build`/`go vet`/`go test ./...` = 1255 pass; mobile `tsc` + detail jest green):**
- **A — durable identity store:** migration `008_discovery_entity_identity.sql` (applied to local dev; **prod application pending human review**); `IdentityStore` port; `PgxIdentityStore` (Postgres source of truth) + `RedisIdentityStore` (read-through, nil-safe); write path in `stampIdentities` (backgrounded via `bgWg` + `context.WithoutCancel`); read path in `enrichOne`; wired in `search_wiring.go`. Integration round-trip test (`-tags=integration`) + unit test for the enrich read path.
- **B — cache quality-gating:** artwork resolution now reports an `ArtworkConfidence` (`identity` / `name` / `none`). The cache stores it and gives identity images the long TTL, name-resolved images a short provisional TTL (so a guess re-checks soon and can upgrade once identity is learned), and an **overwrite guard** so a weaker result (name guess, or a later failure) can never clobber a real higher-confidence image. (The mbid-in-key scheme already separates most identity vs name entries; the guard is correctness insurance.)
- **C (core):** removed the artist→track artwork fallback — an identity-less artist now yields empty `image_url` (honest placeholder) instead of a stranger's track cover.
- **Discography:** backend sorts albums newest-first + normalizes `year` before truncation; client always sorts the union (the `dzValidated` skip is gone); tests updated.
- **Observability:** `identity.durable_resolved` debug log fires exactly when the durable store recovers identity on an MB-absent search (the fix firing).

**Verified end-to-end (local replay, `discoveryeval -query "Che"`):** with MusicBrainz absent (`src=[deezer]`), the durable store resolved the artist's MBID from Postgres and artwork resolved (`resolved=true had_mbid=true`) where the same MB-absent run previously produced Deezer's empty-hash placeholder.

**Deferred (follow-ups):**
- **C — client initials avatar:** the client already renders a blank for empty `image_url` (honest, per the agreed "blank beats wrong"); a nicer initials/letter avatar is polish.
- **Write-path live demo with MB present** was blocked by MusicBrainz rate-limiting during the session; the write path is proven by the integration round-trip test instead.
