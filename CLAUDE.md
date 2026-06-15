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

## Tools — use the right tool for the job

**Serena MCP is the primary code navigation tool. Use it BEFORE Grep for code tasks.**

| Task | Use Serena | NOT Grep |
|------|-----------|----------|
| Find where a function/type/interface is defined | `find_declaration` or `find_symbol` | ~~Grep for function name~~ |
| Find all callers/users of a symbol | `find_referencing_symbols` | ~~Grep for symbol name~~ |
| Find who implements an interface | `find_implementations` | ~~Grep for interface name~~ |
| Understand a file's structure | `get_symbols_overview` (then Read specific parts) | ~~Read entire file~~ |
| Get type errors and warnings | `get_diagnostics_for_file` | ~~Run tsc/go vet manually~~ |
| Rename a symbol across codebase | `rename_symbol` | ~~Find-and-replace~~ |
| Replace a function body | `replace_symbol_body` | ~~Read + Edit~~ |

**Grep is for text search only** — string literals, comments, config values, error messages, things that aren't code symbols. If you're grepping for a function name, you're using the wrong tool.

**context7 MCP is the primary documentation tool. Use it BEFORE answering from memory.**

| Task | Use context7 | NOT training data |
|------|-------------|-------------------|
| API usage, function signatures, config options | `resolve-library-id` → `query-docs` | ~~Answer from memory~~ |
| Version-specific behavior or breaking changes | `query-docs` with version context | ~~Guess based on training~~ |
| "How do I do X with library Y" | `query-docs` for current patterns | ~~Recall old patterns~~ |

Libraries that change fast and MUST be checked: Expo SDK, React Native, React Navigation, TanStack Query, Zustand, Reanimated, Go standard library, chi, sqlx. Never assume your training data is current for these — check context7 first.

**Software-architecture-design vault MCP** is the authoritative pattern reference — see `.claude/rules/vault-consultation.md`. Consult before any non-trivial design decision.

## Workflow

- Invoke skills by name (`/feature-spec`, `/feature-plan`, `/common-ground`, `/audit-codebase`) or use CE plugin skills (`/ce-brainstorm`, `/ce-plan`, `/ce-work`).

## Git

- Conventional Commits, scopes constrained by `commitlint.config.js`.
- **Never** append `Co-Authored-By: Claude` / `Co-Authored-By: AI` / `🤖 Generated with…`. The `commit-msg` hook strips them, but don't generate them in the first place.

## Nested context

Look for `CLAUDE.md` files closer to the file you're editing — feature- and layer-specific rules live near the code. See `docs/claude-md-map.md` for the index.
