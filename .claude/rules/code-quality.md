---
paths:
  - "services/go-api/**"
  - "services/overseer/**"
  - "apps/mobile/src/**"
---

# Code quality invariants

Enforced by lint (see `services/go-api/.golangci.yml`, `services/go-api/.golangci.strict.yml`, and `apps/mobile/eslint.config.js`): function/type size (`funlen`, `max-lines-per-function`), complexity (`cyclop`, `gocognit`, `complexity`), guard clauses / no `else` after early return (`revive` `indent-error-flow`+`superfluous-else`, `no-else-return`), import direction points inward (`depguard`), banned vague names like `data`/`handler`/`manager`/`util` (`id-denylist`), Go formatting (`gofumpt`). Don't restate these.

Judgment rules a linter can't check:

- Every type has one reason to change. Name that reason or split it — beyond just fitting the size limit.
- Wrap domain primitives in value objects (IDs, emails, money). No raw strings or ints for domain concepts.
- Tell, Don't Ask: command objects rather than query-then-decide across a boundary.
- Let design patterns emerge from refactoring (Rule of Three). Don't force them.
- Every public API has a clear contract readable from the signature: what it accepts, what it returns, what errors it can produce.

Format Go with `bash services/go-api/scripts/guardrails.sh fmt` (golangci-lint's bundled gofumpt, the gate's own formatter). Never standalone `gofumpt -w` — it groups imports the opposite way and CI rejects it.

## Local verification, worktree-specific (services/go-api)

- `services/go-api/scripts/guardrails.sh` crashes in `staticcheck` in a crew worktree. Run
  `~/go/bin/golangci-lint run --config .golangci.strict.yml --disable staticcheck,unused <pkgs>`
  instead, adding `--new-from-rev=origin/main` to hide pre-existing `funlen` hits on unchanged code.
- Format Go with `golangci-lint fmt -c .golangci.strict.yml` (what CI runs). Plain `gofumpt -d`/`-w`
  gives a false pass — it groups `altune/go-api` imports as stdlib, and CI rejects the result.
- `guardrails.sh fmt` skips new, untracked `.go` files. `git add` a new file before running it.
- `bash services/go-api/deploy/blue-green_test.sh` is not wired into CI (only `smoke_test.sh` is);
  run it locally before shipping any change under `services/go-api/deploy/`.
- golangci-lint's build cache is shared across worktrees and can report files from another
  worktree. Set `GOLANGCI_LINT_CACHE` to a worktree-local path.

## Local verification, worktree-specific (services/overseer)

- `golangci-lint run` and `nilaway ./...` from `services/overseer` also scan `web/node_modules`
  (flatted ships `.go` files there) once `npm ci` has run in `web/`. Scope both to
  `./internal/... ./cmd/...`.
- `services/overseer/web` vitest returns empty content for `?raw` CSS imports; add `@types/node`
  as a dev dependency so tests can read CSS via `node:fs` instead.
