# Altune

Music manager. Expo (RN + TS) mobile in `apps/mobile/` + Go hexagonal modular monolith in `services/go-api/`. Solo + Claude, production-grade.

## Knowledge base

`okf/` is the curated knowledge bundle (OKF format: markdown concepts, YAML frontmatter, one concept per file). Start at `okf/index.md` and descend only into the branch you need — before exploring an unfamiliar module, read its concept doc first. A pre-commit hook blocks commits that change a concept's `resource:` files without updating the concept.

## Always in force

- Domain terms come from `docs/ubiquitous-language.md`; code matches it verbatim, and a new term gets its glossary entry in the same commit. **"Song" is banned — the noun is `Track`.**
- `AIDEV-NOTE/DECISION/WARNING` anchors are durable — never strip them.
- Conventional Commits (scopes in `commitlint.config.js`); never write `Co-Authored-By: Claude` / `🤖 Generated with…` trailers.
- Check Context7 before answering from memory on: Expo SDK, React Native, React Navigation, TanStack Query, Zustand, Reanimated, Go stdlib, chi, sqlx.

## Patterns

`~/.claude/lexicon/` is the authoritative pattern reference. Manifests are **never auto-loaded** — Read the language manifest (`MANIFEST-go.md` / `MANIFEST-ts.md`) before proposing or rejecting any abstraction, and check `INDEX.md` for cross-cutting manifests (caching, event-driven, observability…) when the work touches those domains. Full entries at `~/.claude/lexicon/site/{path}/index.html` — Grep for `Avoid|Cost` and quote the cost line when tradeoffs matter. When proposing an abstraction: name its manifest pattern — or "no pattern — direct code" **plus the closest manifest entry and why it loses** (an unchecked "no pattern" is an assertion, not a verdict) — name the concrete second implementation ("flexibility" isn't one), and honor its _Cost:_ line — no cost line means unproven, not free. One constraint outranks any pattern: imports point one direction, the object graph is wired explicitly in the composition root, behavior lives with its data.

## CodeGraph

In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.
- **Shell** (always works): `codegraph explore "<symbol names or question>"` prints the same output.

If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision.

CodeGraph doesn't replace Serena MCP for: reference/implementation enumeration, type diagnostics, or renames/symbol-body edits — use Serena directly for those (see `~/.claude/rules/tool-routing.md`).
