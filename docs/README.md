# docs/

Durable outcomes only. **If it's here, it's current.** History and pre-decision
thinking do not live here — they live in git and in the GitHub issue they belong to.

Everything durable is one of three things:

| Directory | What lands here |
|---|---|
| [`adr/`](adr/) | Architecture decision records — numbered, append-only: a decision and its why |
| [`providers/`](providers/) | How each external provider integration works today |

Plus the glossary: [`ubiquitous-language.md`](ubiquitous-language.md) — the binding
vocabulary, kept current.

**What does NOT go here:** brainstorms, plans, specs, ideation, handoffs. That
thinking belongs to a ticket — the GitHub issue is the spec. `docs/` holds only
what outlives the ticket. When you reach for a doc that isn't a decision, a
provider note, or the glossary, it's a sign the thing wants to be
an issue instead, or a standing rule in `.claude/rules/`.
