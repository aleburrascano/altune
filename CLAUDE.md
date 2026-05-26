# Altune — project constitution

Universal coding discipline (Karpathy 4 principles, sacred-tests, verification, cited claims, brevity, knowledge-sources) lives in `~/.claude/CLAUDE.md`. This file is **project-specific only**.

@docs/architecture.md
@docs/ubiquitous-language.md
@docs/workflows/new-feature.md

## Project

Altune — music manager. Expo (React Native + TS) mobile + Python (FastAPI, hexagonal) backend. Solo + Claude, production-grade.

## Architectural rules

- **Hexagonal boundary:** `services/api/src/altune/domain/` imports nothing from `adapters/` or framework code. `application/` depends only on `domain/` and the ports it defines.
- **Vertical-slice:** feature work belongs in `apps/mobile/src/features/<feat>/` or under a bounded context in `services/api/src/altune/<domain|application|adapters>/<context>/`. Extraction to `shared/` requires **2+ real consumers** (YAGNI).
- **AIDEV-* anchors:** `# AIDEV-NOTE:`, `# AIDEV-DECISION:`, `# AIDEV-WARNING:` are durable — never strip them.

## Workflow

- Skills in `.claude/skills/` auto-fire on context; you do not need to invoke them by name unless overriding.
- Software-architecture-design vault MCP is the authoritative pattern reference — see `.claude/rules/vault-consultation.md`. Consult **before** any non-trivial design decision.

## Git

- Conventional Commits, scopes constrained by `commitlint.config.js`.
- **Never** append `Co-Authored-By: Claude` / `Co-Authored-By: AI` / `🤖 Generated with…`. The `commit-msg` hook strips them, but don't generate them in the first place.

## Nested context

Look for `CLAUDE.md` files closer to the file you're editing — feature- and layer-specific rules live near the code. See `docs/claude-md-map.md` for the index.
