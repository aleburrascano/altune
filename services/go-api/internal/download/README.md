`download` owns how a saved track gets its playable audio file.

It owns the tables `acquisition_cooldowns`, `acquisition_jobs`, `acquisition_outcomes`, `acquisition_rejections`, `acquisition_upgrade_state`. Only `download` writes them. Other modules may read them. Today `library` reads `acquisition_jobs.attempts` in `library/internal/adapters/persistence/track_repo.go`, to fence its track writes.

## Must not

- Import another feature. `library` and `search` reach it only through `app/bridges`, and what `search` hands it for a recording is the shared `songfields.RecordingIdentity`, whole.
- Read config or the environment. `app` hands it `Deps`.
- Do I/O in `domain`, `service` or `ports`. Time, randomness and every outside system come in through a port.
- Decide anything about a track in `library`. `download` is handed the library half of the track ledger through `Deps.Library` (read a track, complete, fail, revert, the status rules, and whether a completion's confidence and evidence are accepted), and joins its own upgrade state to it inside `New`.

## Shape

The root holds `download.go` (the door: `New(ctx, Deps) (*Module, error)`, `Deps`, `Module` with `Routes()`, `Scheduler()`, `On()`, `Upgrade()`, `CheckSources()`, `HasSourceChecks()`, `YouTubeSignedOutSince()`, `Retention()`, and the few names `app`, the bridges and `cmd` use), `downloadtest/` and `internal/`. `New` sweeps stale temp dirs, builds the sources, the track ledger, the scheduler, the upgrade lane and the source canary. Acquisition is on only when there are sources and an audio store. Otherwise `New` still returns a module, without a scheduler, without routes and without an upgrade plan. Everything else is under `internal/`, one package per layer:

- `domain/`: the value objects, the errors adapters need, source-key and audio-quality rules.
- `ports/ports.go`: every interface the module needs from outside, and the types in their signatures.
- `service/`: the acquisition pipeline and scheduler, candidate ranking, the audio verdict, the title match, replacement (upgrade), the MusicBrainz recording resolver, the track ledger (`track_ledger.go`, the library half plus the upgrade state), the in-place recorder, the fingerprint health tracker and the source canary runner. Its tests run on fakes from `adapters/memory`.
- `adapters/persistence/`: one store per table it owns (`job_queue`, `cooldown_store`, `outcome_store`, `rejection_store`, `upgrade_store`).
- `adapters/memory/`: the in-memory ledger, library half, upgrade state, audio store, rules and job queue.
- `contracttest/`: the suites that run against both the Postgres and the memory adapter (the Postgres half runs with `INTEGRATION=1`).
- `adapters/handler/`: the retry and reacquire handlers and `Routes`, which `app` mounts.
- `adapters/<system>/`: one folder per outside system or tool: `ytdlp`, `ytmsearch`, `youtubemusic`, `savedlink`, `chromaprint`, `ffmpegtag`, `id3`, `scratchfiles`, `previewcheck`, `fakesources`, and `qualitycheck` (the offline evaluation behind `cmd/acquisitioneval`).
- `downloadtest/` stays at the root because `app`, its bridges and `cmd` import its builders and fakes in their tests.

`export_test.go` lets the root tests reach the live source tiers, the InnerTube client and the canary builder without exporting them.
