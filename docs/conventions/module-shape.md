# Module shape

module-spec owns the module shape of every code root. Each module is a folder with a `module.yaml`, and its door is `index.ts` in TypeScript or the `.go` files at its root in Go.

| Root | Modules | Baseline |
|---|---|---|
| `services/go-api` | `internal/<feature>`, `internal/app`, `internal/shared/<module>` | `services/go-api/module-spec.baseline.json` |
| `services/overseer` | `internal/<module>`, `internal/buckets/<bucket>` | `services/overseer/module-spec.baseline.json` |
| `client` | `src/features/<feature>`, `src/shared/<module>`, `src/mobile` (the app) | `client/module-spec.baseline.json` |

The expo-router routes in `client/src/app/` belong to no module, since expo-router builds URLs from that folder.

Run `bash scripts/module-spec.sh check`. `scripts/precheck.sh` runs it on every commit, and its errors link to the rule that explains them. A branch that changes a module clears every baselined line of that module.
