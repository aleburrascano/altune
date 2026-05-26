# ADR-0001: Monorepo layout — `apps/mobile/` + `services/api/`

- **Status:** Accepted
- **Date:** 2026-05-25
- **Deciders:** solo + Claude
- **Context tags:** [arch, layer]

## Context

Altune is a music manager with an Expo (React Native + TypeScript) mobile client and a Python (FastAPI) backend. The repository hosts both. We need a layout that:

- Keeps mobile and API code clearly separated but co-versioned (so a breaking API change and the mobile change land together).
- Allows independent build/test pipelines per app/service.
- Permits future addition of clients (web, desktop) or services (workers, ingestion) without restructuring.
- Plays well with Claude Code's nested CLAUDE.md discovery and path-scoped `.claude/rules/`.

## Decision

Adopt a two-folder monorepo:

```
apps/
└── mobile/     # Expo (RN + TS)
services/
└── api/        # Python (FastAPI)
```

Top-level holds: `docs/`, `.claude/`, root config (gitignore, gitattributes, commitlint, package.json with workspaces). Future clients go under `apps/`; future services go under `services/`. No `packages/` shared-code folder until 2+ apps or services genuinely share code (YAGNI — extract on demand).

## Alternatives considered

| Alternative | Why not |
|---|---|
| Separate repos (mobile-repo + api-repo) | Breaking API changes can't ship atomically with mobile changes; doubles the doc/CLAUDE.md infrastructure; less Claude-friendly because cross-cutting context (architecture.md, ubiquitous-language.md) duplicates. |
| Flat (no apps/services prefix) — `mobile/` + `api/` at root | Works initially but doesn't scale when a second client or service arrives; renaming later costs more than the `apps/`/`services/` prefix costs now. |
| Single Python repo with mobile as a sub-package | Conflates runtimes; tooling assumes single language; collides with Expo's expectations about working-directory layout. |

## Consequences

### What becomes easier
- Atomic cross-stack changes (mobile + API in one commit/PR).
- Shared docs (`docs/`) and shared Claude infrastructure (`.claude/`) — no duplication.
- Nested CLAUDE.md discovery works naturally: top-level constitution → `apps/mobile/CLAUDE.md` → `apps/mobile/src/features/<feat>/CLAUDE.md`.

### What becomes harder
- Per-app/service CI configuration is slightly more involved (matrix workflows, working-directory filters).
- Editor / language-server setup must point at the right sub-roots (`tsconfig.json` in `apps/mobile/`, `pyproject.toml` in `services/api/`).

### What we're committing to
- This shape is the foundation; reversing means re-orchestrating the entire repo. Low pain to add new `apps/<x>/` or `services/<y>/`; high pain to remove or merge `apps/` and `services/`.

## Vault references

- [vault: wiki/concepts/Modular Monolith.md] — broader context on monolith-vs-services trade
- [vault: wiki/concepts/Microservices Architecture.md] — when/if to split `services/api/`
- [vault: wiki/concepts/Conway's Law.md] — solo dev = solo team = monolith-friendly default

## Related

- Plan: `~/.claude/plans/using-the-claude-code-serene-pine.md` §"Target repository layout"
- Architecture: `docs/architecture.md`
