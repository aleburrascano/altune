# Altune — project constitution

Universal coding discipline (simplicity, sacred-tests, verification, brevity, knowledge-sources) lives in `~/.claude/CLAUDE.md`. This file is **project-specific only**.

@docs/architecture.md
@docs/ubiquitous-language.md

## Project

Altune — music manager. Expo (React Native + TS) mobile + Go (hexagonal, modular monolith) backend. Solo + Claude, production-grade.

## Architectural rules

Hexagonal-boundary and vertical-slice rules live in `docs/architecture.md` (imported above) — don't restate them here.

- **AIDEV-* anchors:** `# AIDEV-NOTE:`, `# AIDEV-DECISION:`, `# AIDEV-WARNING:` are durable — never strip them.

## Tools

**Context7**: Always check before answering from memory for these fast-moving libraries: Expo SDK, React Native, React Navigation, TanStack Query, Zustand, Reanimated, Go standard library, chi, sqlx.

**Software-architecture-design vault MCP** is the authoritative pattern reference — see `.claude/rules/vault-consultation.md`. Consult before any non-trivial design decision.

## Workflow

- Invoke skills by name (`/feature-spec`, `/feature-plan`, `/common-ground`, `/audit-codebase`, `/ce-brainstorm`).

## Git

- Conventional Commits, scopes constrained by `commitlint.config.js`.
- **Never** append `Co-Authored-By: Claude` / `Co-Authored-By: AI` / `🤖 Generated with…`. The `commit-msg` hook strips them, but don't generate them in the first place.

## Nested context

Look for `CLAUDE.md` files closer to the file you're editing — feature- and layer-specific rules live near the code. See `docs/claude-md-map.md` for the index.

<!-- waymark:okf-context:start -->
## OKF context

This repo has an `okf/` knowledge bundle describing domain concepts (APIs, data models, playbooks).

- Before editing a file, call `find_concept_by_resource` via the okf MCP server to check if a concept describes it — read it for context first.
- Use `list_concepts(type?, tags?)` to browse, `read_concept(path)` for full detail.
- If the okf MCP server is unavailable, fall back to reading `okf/` directly with Read/Grep — and say so explicitly in your response.
- Do not hand-edit `verified_commit` or `okf/log.md` — the `okf-staleness-fix` skill manages these automatically when it finalizes a fix.
- If a commit is blocked by the OKF staleness pre-commit hook, use the `okf-staleness-fix` skill to draft and verify the concept update before recommitting.
<!-- waymark:okf-context:end -->
