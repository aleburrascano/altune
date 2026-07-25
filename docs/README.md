# docs/

Decisions and history. **This is a record, not a reference** — entries are written once and left alone, so a path or a claim inside an old spec may describe a version of the repo that no longer exists. For how the code works *today*, read [`okf/`](../okf/index.md); for the rules in force, read the nearest `CLAUDE.md`.

| Directory | What lands here |
|---|---|
| [`adr/`](adr/) | Architecture decision records — numbered, immutable once accepted |
| [`specs/`](specs/) | One folder per feature: spec + plan, written before the code |
| [`plans/`](plans/) | Standalone implementation plans not tied to a feature spec |
| [`workflows/`](workflows/) | The playbooks themselves: new-feature, bug-fix, refactor |
| [`solutions/`](solutions/) | Compound-engineering learnings captured after the fact |
| [`providers/`](providers/) | Per-provider integration notes (MusicBrainz, Last.fm, Discogs…) |
| [`handoffs/`](handoffs/) | Point-in-time state dumps for resuming long-running work |
| [`brainstorms/`](brainstorms/) | Expirable exploration — 30-day TTL, safe to prune |
| [`ideation/`](ideation/) | Product-level idea capture, upstream of a spec |
| [`notes/`](notes/) | Permanent notes that fit nowhere else |
| [`superpowers/`](superpowers/) | Skill and tooling notes for the Claude Code setup |

Loose files at this level: [`ubiquitous-language.md`](ubiquitous-language.md) is the binding glossary (not a record — keep it current); the two `discovery-detail-*` files are a handoff and a pipeline sketch.
