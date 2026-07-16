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

`~/.claude/lexicon/` is the authoritative pattern reference. Per-language manifests load via the nested CLAUDE.md files; full entries at `~/.claude/lexicon/site/{path}/index.html` — read one only when the tradeoffs matter. When proposing an abstraction: name its manifest pattern (or "no pattern — direct code"), name the concrete second implementation ("flexibility" isn't one), and honor its _Cost:_ line — no cost line means unproven, not free. One constraint outranks any pattern: imports point one direction, the object graph is wired explicitly in the composition root, behavior lives with its data.
