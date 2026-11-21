Each folder here is one module, and its folder says its role.

- `internal/<feature>/` is a feature. A feature never imports another feature, so each one can change alone. When two features must work together, `internal/app` connects them through a port the feature owns.
- `internal/shared/<module>/` is shared. Any module may import it, and it never imports a feature or `internal/app`.
- `internal/app/` wires the server from the features and runs it. Only it may import every feature.

A package's tests live in `package <name>_test` and use only its exported API, so a test fails only when behaviour changes. `golangci-lint` checks both rules: `depguard` for the imports and `testpackage` for the tests, configured in `.golangci.strict.yml`.
