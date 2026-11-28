`playback` owns the queue snapshot a user resumes playback from.

It owns the table `playback_queue_state`. Only `playback` writes it.

## Must not

- Own the catalog. It shows the current track on resume through the `TrackSource` port; `app/bridges/playbackfromlibrary` serves that port from `library`, and playback never imports `library`.
- Read config or the environment. `app` hands it timeouts and the save rate limit through `Deps`.
- Put a domain type on the wire. Wire shapes live only in `internal/adapters/handler/dto.go`, and only `internal/adapters/handler/errors.go` maps domain errors to `apierror` codes.

## Who reads our tables

No module reads `playback_queue_state`.

`playback` itself reads `auth_user_ids` (a view over `auth.users`, migration 052) in `internal/adapters/persistence/queue_state_repo.go`, to find queue states whose account is gone and erase them.

## Shape

The door is `playback.go`: `New(Deps)`, `Deps`, `Module` with `Routes()` (the same `Bounded` and `LongLived` shape as library; playback has no long-lived routes), and the few names `app` and the bridge use. `playbacktest/` holds the door names only tests use. Everything else is under `internal/`:

- `domain/`: `queue_state.go`, `queue_position.go`, `queue_source.go`, `repeat_mode.go`, `errors.go`.
- `ports/ports.go`: every interface the module needs from outside.
- `service/`: one file per use case (`save_queue_state.go`, `save_queue_position.go`, `resume_queue_state.go`, `forget_queue_state.go`, `erase_deleted_users.go`), plus the now-playing reader, a query use case.
- `adapters/handler/`: `routes.go`, `dto.go`, `errors.go` and the rate limit.
- `adapters/telemetry/metrics.go`: the metrics registry the module and the live-metrics snapshot share.
- `adapters/catalog/`: the circuit breaker and lookup timeout around the `TrackSource` call (`guarded_source.go`, with the errors its constructor returns).
- `adapters/persistence/queue_state_repo.go` and `adapters/memory/queue_state_repo.go`; the contract test that runs both is `adapters/memory/queue_state_repo_contract_test.go` (Postgres half runs with `INTEGRATION=1`).
