---
paths:
  - "services/go-api/**"
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
