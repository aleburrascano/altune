# Ubiquitous language

Shared vocabulary across code, tests, conversation, and documentation. When a term is used in `services/api/src/altune/domain/`, it lives here with a precise meaning.

Reference: `[vault: wiki/concepts/Ubiquitous Language.md]`, `[vault: wiki/concepts/Domain-Driven Design.md]`.

## Rules

1. **One term, one meaning.** If "playlist" means two different things in two contexts, name them differently (e.g., `UserPlaylist` vs. `SmartPlaylist`).
2. **Code matches glossary.** Class names, method names, variable names use these terms verbatim. The `terminology-drift` hook flags drift.
3. **Glossary entries match code.** If a term appears here but not in the code, either delete it (premature) or build the type (overdue).
4. **Defined per bounded context** when a term diverges. Most terms are global; some need context-qualified entries (see "Per-context overrides" below).

## Adding a term

When `/feature-spec` or domain modeling introduces a new term:
1. Add it here in the same commit (the `terminology-drift` hook will flag if you don't).
2. Use the format below.
3. If the term overrides a global definition in a specific context, add a "Per-context overrides" entry.

## Format

```
- **TermName** — definition in 1–3 sentences. Cross-link to vault if applicable.
```

---

## Glossary

### Canonical (terms with corresponding code)

- **Track** — a single audio recording with metadata (title, artist, optional album, optional duration). Aggregate root of the **catalog** bounded context. Identity is a `TrackId` (UUID); equality and hashing are by id. Owned by a user (`UserId`). Defined in `services/api/src/altune/domain/catalog/track.py`. Introduced by spec `docs/specs/view-library/spec.md`.

### Future (illustrative — to be added when the spec that introduces them ships)

- **Artist** — a creator of tracks. Identified by name + optional disambiguator (year, MBID).
- **Album** — a grouping of tracks released together by an artist.
- **Library** — a user's personal collection. Each user has exactly one library. The library references tracks from the catalog; it does not own them.
- **Playlist** — an ordered, named subsequence of tracks within a user's library. User-curated. Not the same as a Queue.
- **Queue** — the runtime playback sequence. Ephemeral by default; persisted only when saved as a Playlist.
- **Play** — the event of a track being listened to (registered at threshold, e.g., 30s or 50% of duration).
- **Favorite** — a user-applied boolean marker on a track within their library.

---

## Per-context overrides

When the same term means different things in different contexts, define each:

```
- **TermName** (in <Context>) — context-specific meaning.
```

_(empty — populated when context divergence happens)_

---

## Anti-patterns

- **Synonyms drift** — "song" and "track" used interchangeably. Pick one; ban the other.
- **Implementation leakage** — "TrackRow" or "TrackDTO" in glossary. Those are infrastructure, not domain.
- **Vague entries** — "User: a person who uses the app." Useless. If a term is in the glossary, it earns its place with a precise definition.
- **Stale entries** — terms that were once in the code but were renamed/removed. Delete or mark deprecated.

## Banned terms

Terms that **must not** appear in altune code (caught by the `terminology-drift` hook and by code review):

- **Song** — synonym of `Track`. The legacy `music-manager` used "song" as its primary noun (`songs` table, `Song` class). altune uses `Track` exclusively. The forthcoming `migrate-songs-v1` spec is the one place "song" appears, and only as the *name of the source data* during the import — never as a type or column in altune.
