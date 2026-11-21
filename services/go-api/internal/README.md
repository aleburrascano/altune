Each folder here is one module, and its folder says its role.

- `internal/<feature>/` is a feature. A feature never imports another feature, so each one can change alone. When two features must work together, `internal/app` connects them through a port the feature owns.
- `internal/shared/<module>/` is shared. Any module may import it, and it never imports a feature or `internal/app`.
- `internal/app/` wires the server from the features and runs it. Only it and the programs in `cmd/` may import features.

A package's tests live in `package <name>_test` and use only its exported API, so a test fails only when behaviour changes.

Only `internal/shared/config` reads the environment in production code. Tests, `internal/shared/sharedtest` and the programs in `cmd/` and `scripts/` may read it too.

`golangci-lint` checks these rules, configured in `.golangci.strict.yml`: `depguard` for imports, `testpackage` for tests and `forbidigo` for the environment. For now they check only new code, because older code still breaks them.
