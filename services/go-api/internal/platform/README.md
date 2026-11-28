Each folder here is one platform module: something the whole server needs that no single feature owns. `app` and the programs in `cmd/` import every platform module except `outbound`, which features import too, and a platform module imports only `shared/` (and `outbound`).

## Shape

Every module is a door file named after it, `<module>.go`, and `internal/`. Nothing else sits at the root except `<module>test/`, which holds the few names code outside the module needs in its tests. The door holds the constructor, the few types and functions other modules call, and nothing a caller does not use (`go run ./scripts/lintdoors --exports` fails on a door name no production code outside the module uses, for platform modules as for features).

`internal/` holds only the layers the module actually has. The full set is the feature template (`domain/`, `service/`, `ports/ports.go`, `adapters/<outside system>/`), and a layer with nothing to put in it is not created:

- No tables, no `adapters/persistence` and no `adapters/memory`. Only `monitoring` owns a table (`client_events`), so only it has a repository, a memory fake and a contract suite (`internal/contracttest`) that runs against both.
- No business rules, no `domain/`. `outbound` only moves bytes and applies limits, and `auth` keeps the rules about which token is valid in `domain/` because it has them. `outbound` therefore has no `domain/` and declares the errors its adapters raise next to them.
- No routes, no `adapters/handler`. `leader`, `requestmetrics` and `outbound` serve no route.
- No use cases that outlive one call, no `service/`. `outbound` and `requestmetrics` are adapters over an outside system (the network, the HTTP server hook).

| Module | Tree under `internal/` | Door |
|---|---|---|
| `auth` | `domain/`, `ports/`, `adapters/{handler,supabase,telemetry}` | `auth.go`, `authtest/` |
| `eventstream` | `domain/`, `ports/`, `adapters/handler` | `eventstream.go` |
| `leader` | `domain/`, `ports/`, `service/` | `leader.go`, `leadertest/` |
| `monitoring` | `domain/`, `ports/`, `service/`, `contracttest/`, `adapters/{handler,notifier,persistence,memory}` | `monitoring.go`, `monitoringtest/` |
| `outbound` | `adapters/{egress,pacing,retry,youtubemusic,ytdlp}` | `outbound.go`, `outboundtest/` |
| `requestmetrics` | `domain/`, `adapters/observer` | `requestmetrics.go`, `requestmetricstest/` |

## Where the template does not fit a small module

- A snapshot that another module serializes (`requestmetrics.Snapshot`, `leader.Counters`) keeps its json tags in `domain/`: the module has no handler to hold a DTO, and the module that serves it writes the type straight into its response. Moving the tags would need a mirror type in the consumer for no change on the wire.
- A module with one constructor that is not `New(Deps)` (`leader.NewElection`, `auth` with its middleware and verifier constructors) keeps it: the call sites in `app` pass what the module needs by name, and a `Deps` struct would only rename the arguments.
- `app/alerts`, `app/livemetrics` and `app/providertransport` are not moved into `monitoring` and `outbound` (rule 23 of #4027). `providertransport` calls `search`, and `search` already imports `outbound`, so the move is an import cycle. `alerts` and `livemetrics` read `search`, `library`, `playback`, `auth`, `leader`, `requestmetrics` and `app/jobs`, and a platform module may import only `shared/`.
