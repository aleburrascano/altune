`search` owns how search and browse results are found, merged and ranked across providers.

It owns the tables `discovery_events`, `discovery_favorites`, `discovery_search_history`, `discovery_search_history_cleared`, `entity_identity`, `unreleased_artists`, `unreleased_songs`. Only `search` writes them, and no module outside it reads them. `search` itself reads two tables it does not own: the `auth_user_ids` view, to erase rows of deleted identities, and `library`'s `tracks`, through `related_tracks_repo.go` (listed in `library`'s README).

## Must not

- Import another feature. `download`, `library` and `monitoring` reach `search` only through `app/bridges`, and what `search` produces for download is the shared `RecordingIdentity`, never a copy of its fields.
- Read config or the environment. `app` hands it `Deps`.
- Put a domain type on the wire. Wire shapes live only in `internal/adapters/handler/dto.go`, and only `internal/adapters/handler/errors.go` maps domain errors to `apierror` codes.
- Let `domain`, `service` or `ports` import I/O. Time, randomness and every outside system come in through a port.

## Shape

The door is `search.go`: `New(ctx, Deps)`, `Deps` (infrastructure only: the pool, Redis, the transport, the settings, the handler rate limits and the other outside parts, never a repository, because `New` builds its own), `Module` with `Routes()` (the `Bounded` group mounts `/discovery`, the `LongLived` group is empty), `PublicRoutes` for the unauthenticated auth-failure beacon, and the names `app`, the bridges and `cmd` use. The door also holds the composition: it builds the provider clients, the stores and the services. Everything else is under `internal/`:

- `domain/`: one file per entity or value object, and `errors.go` with every error the module defines. Errors from building the module are in the door.
- `ports/ports.go`: every interface the module needs from outside.
- `service/`: one file per use case; the tests in it run on fakes from `adapters/memory`.
- `adapters/handler/`: `routes.go`, `dto.go`, `errors.go` and the rate limits.
- `adapters/persistence/` and `adapters/memory/`: one repository per table (`event_repo`, `favorites_repo`, `history_repo`, `entity_identity_repo`, `unreleased_repo`), Postgres and memory side by side; `contracttest/` holds the suites that run against both (the Postgres half runs with `INTEGRATION=1`).
- `adapters/cache/`: the Redis caches.
- `adapters/<provider>/`: one folder per outside system: `applemusic`, `coverartarchive`, `deezer`, `discogs`, `fanarttv`, `fansheets`, `genius`, `itunes`, `musicbrainz`, `soundcloud`, `spotify`, `theaudiodb`, `ytmusic`, `artworkfallback`. `providers/` holds the plumbing they share (query syntax, request shaping, the schema audit target).
- `adapters/telemetry/`: provider call counters and their live snapshot.
- `searchtest/` stays at the root because `app`, its bridges and `cmd` import its names in their tests. It holds only those names, as aliases of what `adapters/memory` and the inner packages define; the module's own tests, builders included (`*_builders.go` in `adapters/memory`), use `adapters/memory` directly.
