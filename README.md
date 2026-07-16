# Altune

Music manager. Greenfield rebuild — solo + Claude, production-mindset, evolvable workflow.

## Stack

- **Mobile:** Expo (React Native + TypeScript) — `apps/mobile/`
- **API:** Python (FastAPI, hexagonal architecture) — `services/api/`

## Working in this repo

The repo is set up for Claude-first development. The shape:

- Every feature follows `spec → plan → TDD → verify → review → compound` (see [`docs/workflows/new-feature.md`](docs/workflows/new-feature.md))
- Backend is **hexagonal**: `domain/` (pure) → `application/` (use cases + ports) → `adapters/` (drivers + driven)
- Frontend is **vertical-slice**: `apps/mobile/src/features/<feat>/` owns UI + hooks + api + tests for that feature
- Documentation auto-maintained by drift-detector hooks (see [`docs/workflows/new-feature.md`](docs/workflows/new-feature.md) §freshness)

## Quick reference

| Goal | Where |
|---|---|
| Add a new feature | `/feature-spec <name>` then follow [`docs/workflows/new-feature.md`](docs/workflows/new-feature.md) |
| Fix a bug | `/feature-spec`-light + [`docs/workflows/bug-fix.md`](docs/workflows/bug-fix.md) |
| Refactor | [`docs/workflows/refactor.md`](docs/workflows/refactor.md) |
| Decide an architecture question | `/brainstorm-tech-choice` → ADR in `docs/adr/` |
| Look up a pattern | Pattern lexicon (`~/.claude/lexicon/` — manifests load via nested `CLAUDE.md`; full entries under `site/`) |

## Conventions

- Commits: [Conventional Commits](https://www.conventionalcommits.org/) (template in `.gitmessage`, enforced by commitlint)
- No `Co-Authored-By: Claude` lines — stripped by `.git/hooks/commit-msg`
- Tests are **sacred** — fix implementation to match tests, not the reverse
- Document decisions in [`docs/adr/`](docs/adr/)
- Capture learnings in [`docs/solutions/`](docs/solutions/) via `/compound-learning`

## Layout

```
.
├── CLAUDE.md                  # project constitution (lean — see ~/.claude/CLAUDE.md for universal rules)
├── .claude/                   # skills · agents · hooks · path-scoped rules
├── docs/
│   ├── architecture.md
│   ├── ubiquitous-language.md
│   ├── workflows/             # new-feature, bug-fix, refactor playbooks
│   ├── specs/                 # one folder per feature
│   ├── adr/                   # architecture decision records
│   ├── solutions/             # compound-engineering learnings
│   ├── brainstorms/           # expirable exploration (30d TTL)
│   └── notes/                 # permanent (won't auto-prune)
├── apps/
│   └── mobile/                # Expo app
└── services/
    └── api/                   # Python backend
```

## Status

`v0.0.0-scaffold` — scaffolding only, no features yet. First feature spec starts the catalog.
