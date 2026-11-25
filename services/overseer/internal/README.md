Each folder here is one module, and its folder says its role.

- `internal/buckets/<bucket>/` and `internal/shell/` are features. A feature never imports another feature, so each one can change alone. When two features must work together, `internal/app` connects them.
- `internal/authn`, `config`, `core`, `goapi`, `history`, `oci`, `panicguard` and `webui` are shared. Any feature may import them, and they never import a feature or `internal/app`.
- `internal/app/` wires the server from the features and runs it. Besides the tests in `internal/guard`, only it and `cmd/overseer` may import features.
- `internal/guard/` holds the whole-tree contract and architecture tests. Its tests may import any module, and no other package imports it.

A package's tests live in `package <name>_test` and use only its exported API, so a test fails only when behaviour changes.

Only `cmd/overseer` reads the environment: it passes `os.Getenv` to `internal/config` and `internal/app`. Tests may read it too.

`golangci-lint` checks these rules over the whole tree, configured in `services/overseer/.golangci.strict.yml`: `depguard` for imports, `testpackage` for tests and `forbidigo` for the environment.
