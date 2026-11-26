Each folder here is one module, and its folder says its role.

- `internal/<feature>/` is a feature: `search`, `library`, `download` or `playback`. A feature never imports another feature or a platform module, so each one can change alone. When two features must work together, `internal/app` connects them through a port the feature owns, in `internal/app/bridges/<b>from<a>`: that bridge serves the ports of `<b>` from `<a>`.
- `internal/platform/<module>/` serves the whole server: `auth`, `eventstream`, `monitoring`, `leader`, `requestmetrics` and `outbound`. A platform module imports only shared modules, and only `internal/app` and the programs in `cmd/` import it.
- `internal/shared/<module>/` is shared. Any module may import it, and it never imports a feature, a platform module or `internal/app`.
- `internal/app/` wires the server from the features and platform modules and runs it. Only it and the programs in `cmd/` may import features.

A package's tests live in `package <name>_test` and use only its exported API, so a test fails only when behaviour changes.

Only `internal/shared/config` reads the environment in production code. Tests, `internal/shared/testdb` and the programs in `cmd/` and `scripts/` may read it too.

`go run ./scripts/lintarch` checks the module rules, all declared in `services/go-api/architecture.json`: every folder here must have a role there (a new module fails until it does), a module imports only the roles its role may, core, service, domain, ports and shell packages import no I/O, database driver packages are imported only from `adapters` packages, and only the module that owns a table writes it. The role and edge rules run over the whole tree on every push and in CI; `layer`, `driver` and `table` join them as each reaches zero. Database driver packages are imported only from `adapters` packages, from the driver's homes (`shared/database`, `shared/testdb`) and, for the pool type alone, from the composition root named in `driver_composition`. `golangci-lint` checks `testpackage` for tests and `forbidigo` for the environment, configured in `.golangci.strict.yml`.
