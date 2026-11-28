`go-api` is the backend of altune: one Go module, built from feature modules and platform modules that all follow the same shape.

This page holds the rules of that shape. Each rule names the check that enforces it, and a rule is added here only when its check exists and passes. A check's error message names the section below to read. They all run in `scripts/precheck.sh` and in CI at zero violations, with no baseline, allowlist or ignored path.

`internal/README.md` says what each folder under `internal/` is. This page says what a module looks like inside.

The shape checks are `go run ./scripts/lintdoors --shape .`, run from this folder. They read the folders and the Go files of every feature module (`internal/<feature>/`) and every platform module (`internal/platform/<module>/`). `shared/` and `app` are not modules in this sense.

## The shape

```
internal/<module>/
  <module>.go          the door: New, and the few names other modules call
  README.md            line 1 says the module's one job
  CLAUDE.md
  <module>test/        optional: builders and fakes that code outside the module imports in tests
  internal/
    domain/            entities, value objects, errors.go
    service/           one file per use case
    ports/ports.go     every interface the module needs from outside
    contracttest/      suites that run against every implementation of a port
    adapters/
      handler/         routes.go, dto.go, errors.go
      persistence/     <table>_repo.go
      memory/          <table>_repo.go, the fake beside each one
      <outside system>/
```

A module has only the layers it uses. A module with no tables has no `persistence` or `memory`, and a module with no business rules has no `domain/`.

### Rule 1: one shape per module

The module's root holds only `<module>.go`, `README.md`, `CLAUDE.md`, `internal/`, an optional `<module>test/` and the door's own test, `<module>_test.go`. Go's `internal/` makes the rest unimportable from outside. `<module>test/` is for code outside the module that imports it in tests.

Enforced by `lintdoors --shape`, which fails on any other file or folder at the root, and on a module with no `internal/`.

### Rule 2: one door file

The door is the one file `<module>.go`. It declares `New`, the few interfaces other modules call and the value types they use, and nothing else. Every door name has a caller outside the module.

Enforced by `lintdoors --shape` (a second `.go` file at the root, no door file, or a door without `func New`) and by `lintdoors --exports` (a door name no production code outside the module uses).

### Rule 5: the inside imports nothing from adapters

`domain/`, `service/` and `ports/` never import an adapter of their own module. The outside comes in through a port. The same three packages import no I/O package either, and `domain/` no logging.

Enforced by `lintdoors --shape` (the adapter imports) and by the `depguard` rules `layer` and `core-logging` in `.golangci.strict.yml` (I/O and logging).

### Rule 6: only the owner writes its tables

Enforced by `go run ./scripts/lintarch`, which reads the SQL in string literals and fails when a module writes a table it does not own, or a written table has no owner in `architecture.json`.

### Rule 7: one package per layer

Under `internal/`, only `domain`, `service`, `ports`, `contracttest` and `adapters` exist, with one folder per outside system under `adapters/`. Nothing nests below a layer or below an adapter, except `testdata`. A new concept is a new file in the layer, not a new package.

Enforced by `lintdoors --shape`.

### Rule 10: one file map

- `domain/errors.go`: the module's errors. It must exist when `domain/` does.
- `ports/ports.go`: the interfaces the module needs from outside. It must exist when `ports/` does.
- `adapters/handler/errors.go`: domain errors mapped to `apierror` codes. It must exist when `handler/` does.
- `adapters/handler/routes.go`: the routes the module mounts. It must exist when the handler imports `chi`.
- `adapters/handler/dto.go`: the wire shapes. It must exist when the handler has any json-tagged type.
- `adapters/persistence/<table>_repo.go`, with `adapters/memory/<table>_repo.go` beside it.
- `<module>.go`: the door.

Two more things are checked. A `json:` tag is allowed only in an adapter (in the handler only in `dto.go`; in a cache or other storage adapter for a stored shape; in a provider adapter for the reply it decodes), never in `domain/`, `service/`, `ports/`, the door or `<module>test/`. A type another module turns into JSON has no tags in `domain/`: the module that serializes it maps it to its own DTO. And `apierror` is imported only under `adapters/handler`.

Enforced by `lintdoors --shape`. It checks that the files exist and the two rules above. It does not read what a file holds, so keeping errors, interfaces and routes in their own files is the convention a reviewer holds.

### Rule 13: complete at construction

Every dependency and setting goes into `New`. A method named `With...` on a pointer receiver changes a built value and fails. A `With...` method on a value receiver returns a changed copy and is fine, and so are the standard library's `WithAttrs` and `WithGroup`.

Enforced by `lintdoors --shape`.

### Rule 14: modules meet only in app/bridges

Enforced by `go-arch-lint check` (`.go-arch-lint.yml`): a feature imports no other feature, a platform module imports only shared modules.

### Rule 15: context is never stored in a struct

Enforced by `containedctx` in `.golangci.strict.yml`.

### Rule 16: an error is wrapped once, where it crosses a door

Enforced by `wrapcheck` in `.golangci.strict.yml`.

### Rule 18: no package-level mutable state

No package-level `var` that non-test code assigns, increments, indexes into on the left of an assignment, or passes to `delete` or `clear`, whether from its own package, from another package of the module through an import, or inside a function literal. And none whose declared type or initializer names a `sync` or `atomic` type. State lives in what `New` builds. A table that is only read, and a `var` that a test alone reassigns, are fine.

Enforced by `lintdoors --shape`. It does not see a change made through a method call (`registry.Register(...)`) or a lock hidden inside a struct a constructor returns. `gochecknoglobals` does not fit: it flags every package-level `var` (263 findings across the modules, nearly all read-only lookup tables and function aliases) and cannot tell them from state that changes.

### Rule 20: package names are one plain word

Every package under `internal/` and its folder are one lowercase word, with no underscore or capital, and none is `util`, `utils`, `common`, `helper`, `helpers` or `misc`.

Enforced by `lintdoors --shape`. `lintnames` checks identifiers on changed lines and does not read package names.

### Rule 21: every module README opens with its one job

Line 1 of a module's `README.md` is a sentence that ends in a full stop and says what the module owns.

Enforced by `lintdoors --shape`.

### Rule 23: app is the composition root and nothing else

`internal/app` builds every module in order and runs the server. It holds `app.go` (builds everything), `config.go` (reads the settings once and hands each module its part), one `<module>_wiring.go` per module (`library`, `search`, `download`, `playback`, `platform`), `jobs.go` (schedules the jobs), `shutdown.go`, `server.go`, and `bridges/`. Anything that decides something lives in the module that owns the decision: alert rules and metric definitions in `platform/monitoring`, each table's erase and prune in the module that owns the table. App only calls them. Test files and `testdata/` may sit beside them.

Enforced by `lintdoors --shape`: a file or folder in `internal/app` that is not listed fails.
