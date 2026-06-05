# domain/catalog — bounded-context local context

The catalog context owns the immutable identity-and-metadata side of music: the `Track` aggregate plus its value objects. Pure domain — **zero** framework/adapter imports (enforced by `.claude/rules/domain-layer.md`).

## Aggregate + value objects

- **Track** (`track.py`) — aggregate root. Fields: `id: TrackId`, `user_id: UserId`, `title`, `artist`, `album?`, `duration_seconds?`, `added_at`, `artwork_url?`, `acquisition_status: AcquisitionStatus = PENDING`, `year?`, `genre?`, `track_number?`, `album_artist?`, `isrc?`, `audio_ref?`. Invariants enforced at construction: non-empty title + artist, non-negative duration, positive year/track_number when present, bidirectional `audio_ref ↔ READY` status constraint. Identity + equality/hash are by `id` only.
- **TrackId** (`track_id.py`) — UUID identity wrapper.
- **AcquisitionStatus** (`acquisition_status.py`) — audio-acquisition lifecycle enum. Members: `PENDING` ("saved to library; audio not yet acquired"), `READY` ("audio acquired and available for streaming"). Wire-serialized lowercase via `.value`.
- **dedup_key** (`dedup.py`) — pure normalizer `dedup_key(title, artist, album) -> str`: casefold + whitespace-collapse, `\x1f`-joined, null album → `""`. The natural key behind save idempotency. **It is NOT a `Track` field** and is never threaded through the use case — each repository computes it itself from the track's own fields. One normalizer, two callers (in-memory fake + Postgres adapter) → identical dedup.
- **events.py** — `TrackAddedToLibrary` (past-tense, immutable, carries `occurred_at`, `track_id`, `user_id`). Emitted only on a fresh save (a dedup hit emits nothing).

## Conventions

- New domain terms must be added to `docs/ubiquitous-language.md` in the same commit (the `terminology-drift` hook enforces this).
- Keep `Track` small: `acquisition_status` is a value object; invariants flow through the root, not scattered.
