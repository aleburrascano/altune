`download` owns how a saved track gets its playable audio file.

It owns the tables `acquisition_cooldowns`, `acquisition_jobs`, `acquisition_outcomes`, `acquisition_rejections`, `acquisition_upgrade_state`. Only `download` writes them. Other modules may read them. Today `library` reads `acquisition_jobs.attempts` in `adapters/persistence/track_repo.go`, to fence its track writes.

## Must not

- Import another feature. `library` and `search` reach it only through `app/bridges`, and what `search` hands it for a recording is the shared `songfields.RecordingIdentity`, whole.
- Read config or the environment. `app` hands it `Deps`.
- Do I/O in `domain`, `service` or `ports`. Time, randomness and every outside system come in through a port.

## Shape

The root holds the door files (`download.go` and the `*_door.go` files that re-export what `app`, the bridges and `cmd` use) and `internal/`. Folding the door into `download.go` alone is the next step. Everything else is under `internal/`, one package per layer:

- `domain/`: the value objects, the errors adapters need, source-key and audio-quality rules.
- `ports/ports.go`: every interface the module needs from outside, and the types in their signatures.
- `service/`: the acquisition pipeline and scheduler, candidate ranking, the audio verdict, the title match, replacement (upgrade) and the MusicBrainz recording resolver. Its tests run on fakes from `adapters/memory`.
- `adapters/persistence/`: one store per table it owns (`job_queue`, `cooldown_store`, `outcome_store`, `rejection_store`, `upgrade_store`).
- `adapters/memory/`: the in-memory ledger and job queue.
- `contracttest/`: the suites that run against both the Postgres and the memory adapter (the Postgres half runs with `INTEGRATION=1`).
- `adapters/handler/`: the retry and reacquire routes.
- `adapters/<system>/`: one folder per outside system or tool: `ytdlp`, `ytmsearch`, `youtubemusic`, `savedlink`, `chromaprint`, `ffmpegtag`, `id3`, `scratchfiles`, `previewcheck`, `fakesources`, and `qualitycheck` (the offline evaluation behind `cmd/acquisitioneval`).
- `downloadtest/` stays at the root because `app`, its bridges and `cmd` import its builders in their tests.
