# Altune — project constitution

Universal coding discipline (simplicity, sacred-tests, verification, brevity, knowledge-sources) lives in `~/.claude/CLAUDE.md`. This file is **project-specific only**.

@docs/architecture.md
@docs/ubiquitous-language.md

## Project

Altune — music manager. Expo (React Native + TS) mobile + Go (hexagonal, modular monolith) backend. Solo + Claude, production-grade.

## Architectural rules

- **Hexagonal boundary:** `services/go-api/internal/<context>/domain/` imports nothing from `adapters/` or framework code. Service layer depends only on domain and the ports it defines.
- **Vertical-slice:** feature work belongs in `apps/mobile/src/features/<feat>/` or under a bounded context in `services/go-api/internal/<context>/`. Extraction to `shared/` requires **2+ real consumers** (YAGNI).
- **AIDEV-* anchors:** `# AIDEV-NOTE:`, `# AIDEV-DECISION:`, `# AIDEV-WARNING:` are durable — never strip them.

## Tools

**Context7**: Always check before answering from memory for these fast-moving libraries: Expo SDK, React Native, React Navigation, TanStack Query, Zustand, Reanimated, Go standard library, chi, sqlx.

**Software-architecture-design vault MCP** is the authoritative pattern reference — see `.claude/rules/vault-consultation.md`. Consult before any non-trivial design decision.

## Workflow

- Invoke skills by name (`/feature-spec`, `/feature-plan`, `/common-ground`, `/audit-codebase`) or use CE plugin skills (`/ce-brainstorm`, `/ce-plan`, `/ce-work`).

## Git

- Conventional Commits, scopes constrained by `commitlint.config.js`.
- **Never** append `Co-Authored-By: Claude` / `Co-Authored-By: AI` / `🤖 Generated with…`. The `commit-msg` hook strips them, but don't generate them in the first place.

## Nested context

Look for `CLAUDE.md` files closer to the file you're editing — feature- and layer-specific rules live near the code. See `docs/claude-md-map.md` for the index.
