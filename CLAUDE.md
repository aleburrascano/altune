# Altune

Production-grade music manager. Expo (RN + TS) mobile in `apps/mobile/` + Go hexagonal modular monolith in `services/go-api/`.

## Issue workflow

Agile and issue-driven; use the `gh` CLI for all issue and PR work. Epics are parent issues split into `ready`/`blocked` child tasks. The label taxonomy is the source of truth in `.github/labels.yml`.

## Working rules

- Branch for every change; never commit to `main`. It is protected: a PR is required and the `gate` check must pass (admins included).
- Conventional Commits: a type plus a required scope from `commitlint.config.js`; subject lower-case and ≤72 characters.
- End commit messages with the session's attribution trailer; never add a `Co-Authored-By: Claude` or "Generated with" trailer.
