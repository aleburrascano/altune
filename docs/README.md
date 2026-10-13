# docs/

Durable outcomes only. **If it's here, it's current.** History and pre-decision
thinking do not live here — they live in git and in the Gitea issue they belong to.

| Directory | What lands here |
|---|---|
| [`providers/`](providers/) | How each external provider integration works today |
| [`conventions/`](conventions/) | House rules, each with the lint or check that enforces it |
| [`features/`](features/) | In-flight feature plans. The `close` skill deletes a folder once its feature ships |

Plus the glossary: [`ubiquitous-language.md`](ubiquitous-language.md) — the binding
vocabulary, kept current.

And the diagrams: [`diagrams/`](diagrams/), one file per area, Mermaid drawn from
the code. They are hand-kept, so a PR that changes a drawn flow updates its diagram.
Module cycles are CI-checked separately by `npm run cycles`.

**What does NOT go here:** brainstorms, plans, specs, ideation, handoffs. That
thinking belongs to a ticket — the Gitea issue is the spec. `docs/` holds only
what outlives the ticket. When you reach for a doc that isn't a provider note, a
convention, a diagram, or the glossary, it's a sign the thing wants to be
an issue instead.
