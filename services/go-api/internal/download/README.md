`download` gets a saved track its playable audio file: it finds the best source, checks it is the right recording, stores it and records how it went.

It owns the tables `acquisition_cooldowns`, `acquisition_jobs`, `acquisition_outcomes`, `acquisition_rejections`, `acquisition_upgrade_state`. Only `download` writes them. Other modules may read them. Today `library` reads `acquisition_jobs.attempts` in `library/internal/adapters/persistence/track_repo.go`, to fence its track writes. The Postgres adapters are the only code that touches them: one repository per table (`cooldown_repo.go`, `job_repo.go`, `outcome_repo.go`, `rejection_repo.go`, `upgrade_state_repo.go`).

## Must not

- Import another feature. `library` and `search` reach it only through `app/bridges`, and what `search` hands it for a recording is the shared `songfields.RecordingIdentity`, whole.
- Read config or the environment. `app` hands it `Deps`, and the audio sources come in as `Deps.Sources`; `app` builds the live ones with `download.LiveSources`.
- Do I/O in `domain`, `service` or `ports`. Time, randomness and every outside system come in through a port.
- Let `service` or `domain` know a wire format. HTTP codes live in `adapters/handler/errors.go` and stored JSON shapes in `adapters/evidence`.
- Decide anything about a track in `library`. `download` is handed the library half of the track ledger through `Deps.Library` (read a track, complete, fail, revert, the status rules, and whether a completion's confidence and evidence are accepted), and joins its own upgrade state to it inside `New`.

## Shape

The root holds `download.go` (the door: `New(ctx, Deps) (*Module, error)`, `LiveSources`, `Deps`, `Module` with `Routes()`, `Scheduler()`, `On()`, `Upgrade()`, `CheckSources()`, `HasSourceChecks()`, `YouTubeSignedOutSince()`, `Retention()`, and the few names `app`, the bridges and `cmd` use), `downloadtest/` and `internal/`. `New` sweeps stale temp dirs, builds the track ledger, the scheduler, the upgrade lane and the source canary from the sources it is given. Acquisition is on only when there are sources and an audio store. Otherwise `New` still returns a module, without a scheduler, without routes and without an upgrade plan. Everything else is under `internal/`, one package per layer:

- `domain/`: the value objects and entities, one file each, and `errors.go`, which holds every error the module raises (no HTTP type is imported here).
- `ports/ports.go`: every interface the module needs from outside, and nothing else.
- `service/`: the acquisition pipeline and scheduler, candidate ranking, the audio verdict, the title match, replacement (upgrade), the MusicBrainz recording resolver, the track ledger (`track_ledger.go`, the library half plus the upgrade state), the in-place recorder, the fingerprint health tracker and the source canary runner. Its tests run on fakes from `adapters/memory`.
- `adapters/persistence/`: the five Postgres repositories, `<table>_repo.go`.
- `adapters/memory/`: the in-memory twin of each repository (`cooldown_repo`, `job_repo`, `outcome_repo`, `rejection_repo`, `upgrade_state_repo`), the in-memory ledger, library half, audio store and rules, and `noop.go` with the `Noop*` stand-ins.
- `contracttest/`: one suite per repository (`<table>_repo_contract.go`) plus the ledger suite. Each runs against the memory twin and against Postgres; the Postgres half runs with `INTEGRATION=1`.
- `adapters/handler/`: `routes.go` (`Routes`, mounted by `app`), `acquisition_handler.go` (the retry and reacquire handlers) and `errors.go` (the one place a domain error becomes a wire code and status). The handlers read no body, so there is no `dto.go`.
- `adapters/evidence/`: the codec that turns acquisition and in-place evidence into the JSON stored on the track and back. The stored keys are fixed by its tests.
- `adapters/<system>/`: one folder per outside system or tool: `ytdlp`, `ytmsearch`, `youtubemusic`, `savedlink`, `chromaprint`, `ffmpegtag`, `id3`, `scratchfiles`, `previewcheck`, `fakesources`, and `qualitycheck` (the offline evaluation behind `cmd/acquisitioneval`; its `golden_*.json` files are embedded and shipped, not test fixtures).
- `downloadtest/` stays at the root because `app`, its bridges and `cmd` import its builders and fakes in their tests.

The root tests use only the public door.
