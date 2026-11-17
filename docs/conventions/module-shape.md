# Module shape

module-spec owns the module shape of every code root. Each module is a folder with a `module.yaml`, and its door is `index.ts` in TypeScript or the `.go` files at its root in Go.

| Root | Modules | Baseline |
|---|---|---|
| `services/go-api` | `internal/<feature>`, `internal/app`, `internal/shared/<module>` | `services/go-api/module-spec.baseline.json` + `.d/` |
| `services/overseer` | `internal/<module>`, `internal/buckets/<bucket>` | `services/overseer/module-spec.baseline.json` + `.d/` |
| `client` | `src/features/<feature>`, `src/shared/<module>`, `src/mobile` (the app) | `client/module-spec.baseline.json` + `.d/` |

The expo-router routes in `client/src/app/` belong to no module, since expo-router builds URLs from that folder.

Run `bash scripts/module-spec.sh check`. `scripts/precheck.sh` runs it on every commit, and its errors link to the rule that explains them. A branch that changes a module clears every baselined line of that module.

## Baseline

Each root keeps its baseline as fragments (`"baseline": "fragments"` in its `module-spec.json`). The baseline is `module-spec.baseline.json` plus every fragment in `module-spec.baseline.d/`: each fragment lists the keys one change removed or re-keyed after a file move. Only the kit reads them, so never read or edit either by hand:

- `bash scripts/module-spec.sh baseline` records a burn-down. It adds one uniquely named fragment per root that changed and leaves `module-spec.baseline.json` as it is, so two burn-down branches never conflict on the baseline. Commit the new fragment. A fragment never grows the baseline, so the kit compacts instead (rewrites the file and deletes every fragment) when `--accept-new` adds a key, when the cross-module clones rise, or when a key a fragment removed comes back. Such a branch can conflict like before.
- `node_modules/.bin/module-spec baseline --list --root <root>` prints the baseline, one key per line, with the counts on stderr. `bash scripts/violations.sh` prints it for every root as JSON lines.
- `bash scripts/module-spec.sh baseline --compact` folds the fragments back into `module-spec.baseline.json` and leaves the same baseline.

Gitea merges a branch that is behind main, and CI checks a PR against its own merge base, so rebase only when the PR shows a conflict.
