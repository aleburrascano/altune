# Altune

Production-grade music manager. Expo (RN + TS) mobile in `apps/mobile/` + Go hexagonal modular monolith in `services/go-api/`.

## Issue workflow

Agile and issue-driven; use the `gh` CLI for all issue and PR work. Epics are parent issues split into `ready`/`blocked` child tasks. The label taxonomy is the source of truth in `.github/labels.yml`.

## Working rules

- Branch for every change; never commit to `main`. It is protected: a PR is required and the `gate` check must pass (admins included).
- Conventional Commits: a type plus a required scope from `commitlint.config.js`; subject lower-case and ≤72 characters.
- End commit messages with the session's attribution trailer; never add a `Co-Authored-By: Claude` or "Generated with" trailer.

## Local verification, worktree-specific

- `npm run arch:check` fails in a fresh worktree with "Cannot read graft/.graph/wiring.json" —
  run `npm run arch` first (builds graft, then regenerates `docs/architecture.md`).
- `npm run arch` fails on arm64 under Node 24 (`tree-sitter` has no prebuilt binary). Use Node 20:
  `PATH=~/.nvm/versions/node/v20.20.2/bin:$PATH npm ci --ignore-scripts && npm rebuild tree-sitter* && node_modules/.bin/graft build && node scripts/arch-diagram.mjs`.
- The test-edit hook blocks appending to an existing test file. Put new cases in a new test file,
  or use `test-amend` when the ticket needs an existing one changed — never script around the hook.
