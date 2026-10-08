# Module shape

Every module in the same stack has the same shape, so a reader who knows one knows all of them. The folder tree names what the app does (screaming architecture), and each layer has one job (hexagonal: ports and adapters). go-api is a modular monolith of bounded contexts, and the mobile app is vertical slices with the same layers inside each slice.

Every rule here has an ID and a check. The check fails on a new violation. The violations that exist today are listed in a baseline file, one line per violation. A listed violation that no longer occurs also fails, with "remove it from the baseline", so the list only shrinks and one fix can never pay for a new break. The refactor is done when the tier 1 baselines (discovery, acquisition, playback) are empty.

## go-api: `services/go-api/internal/<module>/`

A module is every folder under `internal/` except `app/` (the composition root, wiring only) and `shared/` (the kernel every module may import).

| ID | Rule |
|---|---|
| G1 | The module root holds `domain/`, `ports/`, `service/`, `adapters/` and `ARCHITECTURE.md`, and nothing else. No `.go` file sits at the root. |
| G2 | Imports point inward. `domain/` imports the standard library and `shared/` only. `ports/` adds `domain/`. `service/` adds `ports/`. Only `adapters/` imports drivers, HTTP, pgx or another module. The rule is derived from the folder name, so a new module is covered with no config edit. |
| G3 | A module reaches another module only from `adapters/<other>bridge/`, and that package imports the other module's `ports/` or `service/`. A package named `*bridge` that imports nothing from its module is misnamed. |
| G4 | An error a caller branches on is declared in `domain/errors.go`. A handler writes every error through `httputil.HandleServiceError`. `http.Error` and a bare error `WriteHeader` appear only in `shared/httputil/`. |
| G5 | One handler per resource in `adapters/handler/`, named `<resource>_handler.go`, built by `New<Resource>Handler`. |
| G6 | Fakes used by more than one test file live in the module's test package, `<module>/<module>test/`, as `catalog/catalogtest/` does. |
| G7 | Logs use the `slog.*Context` calls only. |

## Reaching auth

Every handler needs the request user ID, the token-expiry context and `ErrTokenExpired`. Auth has `ports/` (metrics, revoker) and no `service/`, and these functions sit at the auth root. G3 accepts an `authbridge` only if it imports `internal/auth/ports` or `internal/auth/service`.

| Option | Files touched | 401 body | `net/http` in `auth/service` |
|---|---|---|---|
| (a) move `context.go` to `auth/service/` | `context.go`, `context_test.go`, the `ContextWith*` calls in `middleware.go`, one `authbridge` per consumer module | identical: `RequireUserID` and `writeUnauthorized` stay at the root | no: the file imports `context`, `errors`, `time`, `shared` |
| (b) request-identity interface in `auth/ports`, implemented at the root | new `ports` file, `middleware.go`, every bridge, a wiring change in `app/` | identical | no |
| (c) move request identity to `shared/` | `context.go`, `shared/`, the 29 caller files; `RequireUserID` cannot follow without `net/http` and the 401 body in the kernel, so it splits | identical only if the writer stays in auth, which keeps the G3 line | no, but the kernel grows |

**Chosen: (a).** An `authbridge` imports `altune/go-api/internal/auth/service`. It is the smallest move, adds no interface with one implementer, keeps the 401 body byte for byte, and keeps `net/http` out of `auth/service`. The bridge wraps `auth.RequireUserID` from the root for the 401 path until `middleware.go` leaves the root (already G1 drift, baseline lines 1 to 11).

Checked on a throwaway branch against the live repo: `internal/observe/adapters/authbridge/` importing `auth/service` produces no G3 line. The same package importing only `internal/auth` produces `G3 internal/observe/adapters/authbridge imports nothing from auth ports or service`.

Follow-up tickets, in order:

1. Move `internal/auth/context.go` and its test to `internal/auth/service/`; point `middleware.go` at it.
2. `observe/adapters/authbridge/`, then move `observe/handler` imports to it. Clears the `G3 ... imports auth outside adapters/authbridge` lines for `observe/handler/gate.go`, `sse.go` and `streams.go` (baseline lines 96, 100 and 101 as re-recorded by tickets 2 and 3).
3. `eventstream` bridge. Clears `internal/eventstream/handler.go` (line 94, same re-recording).
4. One bridge each for catalog, discovery, playback and acquisition. Clears the other 17 auth lines.

Each ticket removes its baseline lines in the same commit; the check fails if it does not.

## Third-party imports in domain and service

Ten discovery lines (baseline: `google/uuid` in `domain/records.go` and four service files, `x/sync/errgroup` in four service files, `x/sync/singleflight` in `service/search.go`) break G2. The same imports sit in catalog (uuid in `domain/playlist.go`, `domain/track.go`, `service/track_dto.go`; `x/text/unicode/norm` in `domain/dedup.go`, `domain/source_url.go`) and acquisition (uuid in `service/step_store.go`, norm in `service/title_qualifiers.go`).

What the code does today, read before choosing: every `errgroup.Group` in discovery is a bounded fan-out. `SetLimit`, then `g.Go` with a func that always returns nil, then `_ = g.Wait()`. None uses `errgroup.WithContext` and none returns an error, so no call site cancels on first error today. Only the limit and the wait are behaviour. Panics are caught inside the goroutine by `RecoverGoroutine`. `search.go` uses `singleflight.Group.DoChan` keyed by `cache.key(queryNorm, kinds)`.

| Option | Files touched | Behaviour risk | Checker change |
|---|---|---|---|
| (a) `shared/` value type `shared.Id` plus `shared/fanout` and `shared/inflight` wrappers | 10 discovery files, plus 3 new `shared/` files; the 6 catalog and acquisition uuid files later; adapters convert with `.UUID()` | low: the wrappers delegate to errgroup and singleflight, and `Id.String()` is `uuid.String()` | none |
| (b) stdlib only (`crypto/rand` id, `sync.WaitGroup` plus semaphore, hand-rolled dedupe) | the same 10 files, plus a hand-written UUID v4 formatter and parser and a dedupe map | high: a rewritten limit, a rewritten in-flight map, a rewritten UUID format, all for no gain | none |
| (c) id-generator port | 10 files, plus a port, an adapter and wiring in `app/` | low, but a port with one implementer that only calls `uuid.New()`; the cleanliness-over-applied tell. It also fixes nothing for uuid used as a type (`uuid.Nil`, parameters) or for errgroup | none |
| (d) checker allow-list for three packages | `scripts/moduleshape/check.go` and the baseline | none | yes, an owner call; it makes G2 say "stdlib and shared only, except these", which lets the next import in |

**Chosen: (a).** `shared/userid.go` already wraps uuid behind `shared.UserId` and `shared/textnorm` already wraps `x/text/unicode/norm`, so the kernel holding thin wrappers over third-party types is the existing pattern. (a) needs no checker change, no port and no rewrite of concurrency code.

The three pieces:

- `shared.Id` in `shared/id.go`: `type Id struct{ value uuid.UUID }`, with `NewId()` (`uuid.New()`), `ParseId(string)` (`uuid.Parse`), `String()`, `IsZero()` (replaces `== uuid.Nil`), `UUID()` and `IdFrom(uuid.UUID)` for adapters, as `UserId` does. A struct holding one comparable array stays comparable, so `==` and map keys keep working.
- `shared/fanout`: `type Group struct{ g errgroup.Group }` with `SetLimit(int)`, `Go(func() error)` and `Wait() error`. The zero value works, so `var g fanout.Group` and `new(fanout.Group)` replace the errgroup lines one for one.
- `shared/inflight`: `type Group struct{ g singleflight.Group }` with `DoChan(key string, fn func() (any, error)) <-chan Result`, where `type Result = singleflight.Result`.

**Look-alike.** A `shared/` file importing `golang.org/x/sync/errgroup` itself adds no G2 line, because `shared` is not a restricted layer and the checker only restricts `domain`, `ports` and `service`. This is **allowed, and it is the point of the option**: `shared/` is the one place a third-party import is taken on purpose. The limit is review, not the checker. A `shared/` package takes a third-party import only to wrap it behind a type that names the job (`Id`, `fanout.Group`, `inflight.Group`, `textnorm`), and module code never receives the third-party type back. `inflight.Result` is an alias for the one struct field set callers read (`Val`, `Err`, `Shared`) and is the only exception.

**Throwaway-branch run** against the live repo, with `shared/id.go` holding `type Id struct{ value uuid.UUID }` and `domain/records.go` changed to import `altune/go-api/internal/shared` with `ID shared.Id`, nothing else touched: `go run ./scripts/moduleshape` printed `fixed: remove it from the baseline: G2 internal/discovery/domain/records.go:7 domain imports github.com/google/uuid` and no new G2 line. It exited 1 only because of that fixed line, as the check is meant to. Adding `shared/lookalike.go` importing `golang.org/x/sync/errgroup` on top of that changed nothing: still the one fixed line, no G2 line for `shared/`. (One unrelated `G1 internal/discovery/.claude unexpected root entry` line showed up during that run from a scratch folder of the sandbox. A clean tree reports `456 violations, all in baseline`, and it was gone after the revert.) The branch was reverted.

**Behaviour that stays identical.**

- Fan-out limit and wait. `fanout.Group` holds an `errgroup.Group` and forwards the three methods, so `SetLimit` still blocks `Go` at the limit and `Wait` still waits for every goroutine and returns the first error. First-error cancel does not change either: the call sites never used it (no `WithContext`, every func returns nil, `Wait` result dropped). If a future caller needs cancel-on-error, it adds `fanout.WithContext` in `shared/`, and `get_album_tracks.go` and `artwork_fill.go` do not need it now. Their goroutines keep `defer RecoverGoroutine(...)` unchanged.
- In-flight dedupe. `inflight.Group.DoChan` forwards to `singleflight.Group.DoChan` with the same key, so two concurrent identical searches (same `queryNorm`, same kinds) share one call to `resolveCold`: one provider fan-out, not two, and both callers read the same `rankedResolution`, each cloning `ranked` as today. `search.go` keeps `context.WithoutCancel`, the recover, and the `ctx.Done()` select. Ticket 5 adds a test that starts two identical `Execute` calls against a provider that blocks until both have entered, then asserts the provider was called once. That test must pass on the unchanged code first, then after the swap.
- UUID wire format. `Id.String()` returns `uuid.UUID.String()`, the lowercase 8-4-4-4-12 form, and `ParseId` is `uuid.Parse`. `SearchHistoryEntry.ID` and `SearchOutput.SearchId` (a string already) print and parse the same. Persistence adapters keep their pgx and uuid code and convert at the edge with `.UUID()` and `IdFrom`. A table test pins `NewId().String()` against a `uuid.Parse` round trip and `ParseId` against `uuid.Parse` on malformed input (same error).

Follow-up tickets, in order. Each removes its baseline lines in the same commit.

1. Add `shared/id.go`, `shared/fanout/` and `shared/inflight/` with tests (round trip, zero value, `SetLimit` bound, one call under two concurrent `DoChan`). Clears nothing; later tickets need it.
2. Discovery ids, history side: `domain/records.go:7` and `service/record_history.go:12` (`SearchHistoryEntry.ID` and `uuid.New()`), with the history persistence adapter converting at the edge. Clears 2 lines.
3. Discovery ids, search side: `service/record_event.go:14`, `service/result_cache.go:8` and `service/search.go:15` (`continues`, `searchId`, `uuid.Nil` becomes `IsZero`). Clears 3 lines.
4. Discovery fan-out: `service/artwork_fill.go:15`, `service/get_album_tracks.go:11`, `service/eval/coverage_signal_b.go:10` and `service/eval/parallel.go:6` move to `fanout.Group`. Clears 4 lines.
5. Discovery dedupe: the concurrent-identical-search test first, then `service/search.go:16` moves to `inflight.Group`. Clears 1 line.

That is 2 + 3 + 4 + 1 = 10 discovery lines, 0 unassigned. Catalog and acquisition fit the same pieces; they are separate tickets after these, not part of this count:

- uuid with `shared.Id`: catalog `domain/playlist.go:8`, `domain/track.go:12`, `service/track_dto.go:7`; acquisition `service/step_store.go:13`.
- `x/text/unicode/norm` with the existing `shared/textnorm` (not new work for `shared.Id`): catalog `domain/dedup.go:7`, `domain/source_url.go:8`; acquisition `service/title_qualifiers.go:9`. If `textnorm` lacks the exact call, add one function there.
- Not covered here: `acquisition/service/eval/harness.go:4` and `:5` import adapters, which is a different G2 cause.

## Reaching catalog

Acquisition consumes `catalog/domain.Track` and drives its guards (`MarkReady`, `MarkFailed`, `FailAcquisition`, `RevertToPending`, glossary: `docs/ubiquitous-language.md:38`). The baseline has 25 catalog G3 lines (`services/go-api/module-shape.baseline:42-66`, one more than the pitch counted; line 41 is the auth line, out of scope until the auth `context.go` move above). They sit in 3 `ports/` files, 14 `service/` files (13 plus `service/eval/case.go`), 3 handler files and 3 persistence files. Five service files and `ports/writer.go` also take `catalog/ports` (`OrphanedAudioRecorder`, `OrphanedAudio`) in `acquire.go`, `orphan_queue.go` and `step_store.go`.

G2 stops `service/` importing `adapters/`, so the bridge cannot be what `service/` and `ports/` call. They need types acquisition owns, with the bridge implementing them.

What acquisition uses, from `grep` over the non-test files:

- Identity and vocabulary: `TrackId` (151 uses, most of them `NewTrackId` in tests), `FailureCode` with the `Failure*` constants, `JoinFailureReason`, `SplitFailureReason`, `FailureMessage`. These are values with no behaviour that changes with catalog's rules.
- The aggregate: `Track` read-only for 20-odd fields (title, artist, album, duration, ISRC, `AudioRef`, `RejectedSourceKeys`, `Version`, `AcquisitionStatus`) and written through `SetDuration`, `SetAcquisitionProvenance`, `RejectAudioSource`, `SetAudioSource`, `SetAcquisitionConfidence`, `ReplaceAudio`, `MarkReady`, `FailAcquisition`, `RevertToPending`, then one `Update(track, expectedVersion)`.
- Status and provenance: `AcquisitionStatus` (+ `Pending/Ready/Failed`), `AcquisitionProvenance` (+ 3 values), `ErrIllegalAcquisitionTransition`.
- The catalog port `OrphanedAudioRecorder` and its `OrphanedAudio` row.

| Option | Files touched | New types | Guards in one place | Diff |
|---|---|---|---|---|
| (a) acquisition-owned `ports.TrackRef` snapshot plus `ports.TrackLedger` (`Get`, `Complete`, `Fail`, `Revert`), `ports.OrphanedAudio`/`OrphanedAudioRecorder`, and `adapters/catalogbridge` over `catalog/ports.TrackRepository` | all 25 lines' files, 1 new bridge package, `app/` wiring | `TrackRef`, `TrackLedger`, `TrackId`, `FailureCode`, status and provenance enums, orphan types; ids and failure codes would be copied and converted both ways in the bridge | yes: the bridge calls catalog's `Track` methods, catalog's guards stay | large: ~690 `TrackId` uses plus the Track-shaped tests (63 `domain.Track`, 51 `domain.NewTrack`) change |
| (b) `TrackId`, `FailureCode` and the failure helpers move to `shared/`; (a) only for the Track aggregate and orphan types | same files, but ids and codes need no conversion; bridge converts only `Track`, status, provenance | `shared.TrackId`, `shared.FailureCode`; `TrackRef`, `TrackLedger`, orphan types in acquisition `ports/` | yes, same as (a) | ~49 files for the id rename (mechanical, cross-module), then the Track part; split into tickets under 200KB each |
| (c) amend G3: acquisition may import catalog `domain/` as a published language | `module-shape.md`, `check.go`, baseline | none | yes, trivially (acquisition mutates catalog's `Track` itself) | smallest, but weakens G3 for every module; `domain/` of the other module is exactly what G3 exists to keep out; `OrphanedAudioRecorder` is in `catalog/ports`, so (c) would also need to cover `ports/` |

**Chosen: (b).** Ids and failure codes are plain values that both sides already agree on and that other modules (discovery eval, auth failure throttle) also name, so `shared/` is the right owner and the bridge never converts them. `shared/userid.go` and `shared.Id` (previous section) set the precedent. The `Track` aggregate stays in catalog with its guards; acquisition sees a `TrackRef` and asks the ledger to transition, and the bridge calls the same `Track` methods, so the transition rules exist once. (a) alone pays a copy-and-convert for two types that have no catalog-specific behaviour. (c) is rejected: it is an owner call that opens G3 for the next module, and it does not cover `catalog/ports`.

The pieces:

- `shared/trackid.go`: `type TrackId struct{ value uuid.UUID }` with `NewTrackId`, `ParseTrackId`, `TrackIdFromUUID`, `UUID`, `String`, `IsZero`, moved from `catalog/domain/track.go:15-37` and the catalog call sites renamed to `shared.TrackId` (no alias left behind).
- `shared/failure.go`: `FailureCode`, the `Failure*` constants, `JoinFailureReason`, `SplitFailureReason`, `FailureMessage`, moved from `catalog/domain/failure.go` with its test.
- `acquisition/ports/track.go`: `TrackRef` (the fields acquisition reads, plus `Version`), `TrackStatus` and `Provenance` owned by acquisition, `ErrIllegalTransition`, and `TrackLedger`:
  - `Get(ctx, id, user) (TrackRef, error)`
  - `Complete(ctx, ref, Completion) error`, where `Completion` carries duration, provenance, confidence and evidence, source URL or rejected key, and audio ref. One call replaces the setters plus `MarkReady` plus `Update`, so the single optimistic-version write is kept.
  - `Fail(ctx, ref, reason) error`, `Revert(ctx, ref) error`
  - `AudioRefInUse` stays on `ports.AudioRefLookup` with `shared.TrackId`.
- `acquisition/ports/orphan.go`: `OrphanedAudio` and `OrphanedAudioRecorder` with `shared.TrackId` and `shared.UserId`.
- `acquisition/adapters/catalogbridge/`: imports `catalog/ports` (`TrackRepository`, `OrphanedAudioQueue`) and `catalog/domain`; maps `TrackRef` to `Track`, calls the guards, maps `ErrIllegalAcquisitionTransition` and `ErrTrackVersionConflict` to acquisition errors. Handlers and `persistence` use `ports` and `shared` only.

**Throwaway-branch run** (`go run ./scripts/moduleshape` from `services/go-api`, branch deleted afterwards, `work` clean):

- `internal/shared/trackid.go` holding `type TrackId struct{ value uuid.UUID }`, and `ports/cooldown.go` importing `altune/go-api/internal/shared` with `shared.TrackId` instead of `catalog/domain`: the check printed `fixed: remove it from the baseline: G3 internal/acquisition/ports/cooldown.go:4 imports catalog outside adapters/catalogbridge` and no new G3 line. The only other output was `N3 internal/shared/trackid.go matches no exported identifier`, from the scratch file having no methods, which the real move has.
- The same file back on `catalog/domain`: no new line, the G3 line stays matched by baseline line 48.

**Behaviour that stays identical.** `Complete` applies the same setters in the same order as `step_update_track.go:36-83`, so `ReplaceAudio` and `MarkReady` keep their order and error text, and one `Update` carries `expectedVersion`. A bridge test pins each guard: `Complete` on a track not pending returns `ErrIllegalTransition`, as `MarkReady` returns `ErrIllegalAcquisitionTransition` today. Wire values (`FailureCode` strings, `TrackId.String()`) do not change because the constants and uuid format move unchanged.

Follow-up tickets, in order. Each removes its baseline lines in the same commit and stays under 200KB of diff (the two renames are mechanical).

1. Move `TrackId` to `shared/trackid.go` and rename every user in one commit, because removing it from `catalog/domain` breaks each importer: 638 lines in 49 files outside tests of other modules and inside acquisition, about 110KB of diff (53KB of matching lines, removed and added). Clears nothing in acquisition by itself except where a file imported `catalog/domain` only for `TrackId`: `ports/cooldown.go`, `ports/queue.go`, `adapters/persistence/cooldown_store.go`, `cooldown_fallback.go` and `job_queue.go` (baseline lines 45-49), which this ticket removes.
2. Move `FailureCode`, the `Failure*` constants and the three helpers to `shared/failure.go` with their test, and rename catalog, discovery eval, auth and acquisition users (222 lines, about 30KB). Clears `service/failure_reason.go` (line 55).
3. Acquisition `ports/track.go` (`TrackRef`, `TrackLedger`), the bridge's ledger over `catalog/ports.TrackRepository` with the guard tests, and `ports/writer.go` loses `TrackRepository` and its `Track` import. Clears line 50. The ledger has no caller yet.
4. Acquisition-owned `ports/orphan.go`; `acquire.go`, `orphan_queue.go`, `step_store.go` switch to it, and `adapters/catalogbridge` gets the recorder adapter plus `app/` wiring. Clears the `catalog/ports` lines (52, 58, 65).
5. Service read side: `matching`, `audio_ref`, `retry_admission`, `reacquire`, `scheduler*`, `mem_job_queue`, `pipeline`, `buildsteps`, `failure_reason`, `eval/case.go` read `TrackRef`, `shared.TrackId` and `shared.FailureCode`. Clears the remaining lines in those files.
6. Service write side: `step_update_track.go` and the `acquire.go` fail path call `TrackLedger.Complete/Fail/Revert`; the three handlers (`command.go`, `reacquire_handler.go`, `retry_handler.go`) take `shared.TrackId`. Clears lines 42-44, 51 and 66.
7. Tests: acquisition fakes for `TrackLedger` in the module test package, tests drop `catalog/domain.NewTrack`. Clears no baseline line (the checker skips `_test.go`); needed so the tests stop importing catalog. Edit `ARCHITECTURE.md:22-24` to say acquisition reaches catalog through `catalogbridge` and `ports.TrackLedger`.

After 6, `grep -rn 'internal/catalog' internal/acquisition --include='*.go' --exclude='*_test.go'` shows only `adapters/catalogbridge/`.

## mobile: `apps/mobile/src/features/<feature>/`

| ID | Rule |
|---|---|
| M1 | The feature root holds `domain/`, `ports/`, `adapters/`, `hooks/`, `ui/`, `__tests__/`, `native/` and `web/` where needed, and `index.ts`. No other file sits at the root. |
| M2 | `index.ts` is the feature's public surface. `app/` and other features import `@features/<feature>` only, never a path inside it. |
| M3 | Imports point inward. `domain/` is pure (no `react`, `react-native`, `expo-*` or `@shared/ui` value import). `ports/` holds interfaces only and imports `domain/` and api-client types. `adapters/` implements a port and is the only folder that imports api-client network calls; the context that supplies the default adapter lives here too. `hooks/` imports `domain/` and `ports/`. `ui/` imports `hooks/` and `domain/`. |
| M4 | `hooks/` holds only `use*` files, and every `use*` file is in `hooks/`. `ui/` holds components only. |
| M5 | Query keys come from the one query-key factory in `@shared/query`. No literal `queryKey: [` elsewhere. |
| M6 | A failure the user sees goes through the shared failure display (MC-5 in [mobile-consistency.md](mobile-consistency.md)). |
| M7 | Test data comes from the factories in `@testing/factories`. |

## Names, both stacks

| ID | Rule |
|---|---|
| N1 | No file or folder is named `util`, `utils`, `helper`, `helpers`, `common`, `misc`, `lib`, `core`, `manager`, `base`, `generic`, `data`, `stuff`, `tmp`, `temp`, `new`, `old`, `types` or `constants`. Name it for the one thing it holds. `index.ts` as a barrel is allowed. |
| N2 | A module or feature folder is named for a domain concept in [ubiquitous-language.md](../ubiquitous-language.md), and the same concept has the same name in every stack. |
| N3 | A file holds one concept and is named for it: the main type or function it exports, in the stack's casing. |

## Where the checks live

- go-api: one Go test walks `internal/` with `go/parser` for G1 to G7 and N1 to N3. Baseline: `services/go-api/module-shape.baseline`.
- mobile: one jest test walks `src/features/` and `src/shared/` for M1, M2, M4 and N1 to N3. ESLint zones carry M3 and M5. Baseline: `apps/mobile/module-shape.baseline`.
- Both run in `scripts/precheck.sh`, so a PR that adds drift fails before review.
