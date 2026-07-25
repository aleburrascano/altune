# backfillsingles — router

`main.go` + `main_test.go`. One-shot: gives every album-less track its title as the album, matching the rule `domain.NewTrack` now applies to new tracks.

```bash
go run ./cmd/backfillsingles              # dry run
go run ./cmd/backfillsingles -user <uuid> # one user
go run ./cmd/backfillsingles -apply       # write
```

## Rules

- Never set `Album` directly — `SetAlbum` is the only writer, so the dedup key moves with it.
- Never abort the run on a `23505`; that track already has a twin and is reported as skipped.
- Never touch `audio_ref` — the stored key stays historical and keeps streaming.
- Default to a dry run; writing requires `-apply`.

Why each rule exists: `okf/backend/catalog/track.md`.
