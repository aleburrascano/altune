# Module shape

`services/go-api` and `client` have left module-spec. The go-api rules are in `services/go-api/internal/README.md`, and `golangci-lint` checks them. The client rules are in `client/src/README.md`, and fallow, eslint and Stryker check them. module-spec still owns the module shape of `services/overseer`. There each module is a folder with a `module.yaml`, and its door is `index.ts` in TypeScript or the `.go` files at its root in Go.

| Root | Modules | Baseline |
|---|---|---|
| `services/overseer` | `internal/<module>`, `internal/buckets/<bucket>` | `services/overseer/module-spec.baseline.json` + `.d/` |

Run `bash scripts/module-spec.sh check`. `scripts/precheck.sh` runs it on every commit, and its errors link to the rule that explains them. A branch that changes a module clears every baselined line of that module.

## Baseline

Each root keeps its baseline as fragments (`"baseline": "fragments"` in its `module-spec.json`). The baseline is `module-spec.baseline.json` plus every fragment in `module-spec.baseline.d/`: each fragment lists the keys one change removed or re-keyed after a file move. Only the kit reads them, so never read or edit either by hand:

- `bash scripts/module-spec.sh baseline` records a burn-down. It adds one uniquely named fragment per root that changed and leaves `module-spec.baseline.json` as it is, so two burn-down branches never conflict on the baseline. Commit the new fragment. A fragment never grows the baseline, so the kit compacts instead (rewrites the file and deletes every fragment) when `--accept-new` adds a key, when the cross-module clones rise, or when a key a fragment removed comes back. Such a branch can conflict like before.
- `node_modules/.bin/module-spec baseline --list --root <root>` prints the baseline, one key per line, with the counts on stderr. `bash scripts/violations.sh` prints it for every root as JSON lines.
- `bash scripts/module-spec.sh baseline --compact` folds the fragments back into `module-spec.baseline.json` and leaves the same baseline. You do not need to run it: the `compact` job in `.gitea/workflows/module-spec-main.yml` runs it every Sunday at 06:40 UTC on the `host` runner (clear of the nightly eval and of the `precheck` slots). It runs `bash scripts/ci/module-spec-main.sh compact`, which refuses to push if `baseline --list` prints anything different afterwards or if compact touched a file outside the baseline. When fragments were folded, it opens a `tooling/module-spec-compact-<sha>` PR that merges when green, like any other. To see what it would fold without opening a PR, dispatch the workflow with `open-pr` set to `false`.

Gitea merges a branch that is behind main, and CI checks a PR against its own merge base, so rebase only when the PR shows a conflict. As a result, two PRs that each pass can merge into a violation that neither of them had. The `check` job in the same workflow catches this: on every push to main it runs `bash scripts/ci/module-spec-main.sh check`, which is `bash scripts/module-spec.sh check --since HEAD`. With `--since HEAD` the whole tree is checked against the baseline and nothing on main counts as a branch change. Do not leave `--since` out: the kit would then measure from `origin/main`, or from a local `main`, which on the runner's checkout can be months old. When the job goes red, it names the merge commit and gives the command that reproduces the failure on it.

`scripts/__tests__/violations.test.sh` reads the baseline through the kit, so on GitHub it skips (`MODULE_SPEC_RUNS_ELSEWHERE`). Gitea precheck runs it with `bash scripts/ci/kit-self-tests.sh` whenever a PR changes `scripts/violations.*`.
