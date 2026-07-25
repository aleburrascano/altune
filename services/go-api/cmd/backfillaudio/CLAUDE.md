# backfillaudio — router

`main.go` + `main_test.go`. Reconciles tracks that hold audio in storage but were never marked ready — files placed in the bucket out-of-band (a manual copy, a migration from another app) rather than by the acquisition pipeline.

```bash
go run ./cmd/backfillaudio                            # dry run, album UNRELEASED
go run ./cmd/backfillaudio -album "" -user <uuid>     # every album, one user
go run ./cmd/backfillaudio -list                      # dump every object key in storage
go run ./cmd/backfillaudio -verify -album ""          # ready tracks whose ref no longer resolves
go run ./cmd/backfillaudio -apply                     # write
```

## Rules

- Never hand-write the object key — it comes from `acquisitionService.BuildAudioRef`, once per storage layout.
- Never mark a track ready without an `Exists` hit for the exact key.
- Never mutate a track outside `MarkReady` + `repo.Update`.
- Never match a track to an object by anything but its exact derived key — no fuzzy title matching.
- Default to a dry run; writing requires `-apply`. `-list` and `-verify` never write.

Why each rule exists: `okf/backend/catalog/index.md`.
