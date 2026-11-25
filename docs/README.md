# docs/

Durable outcomes only. **If it's here, it's current**, except the dated snapshots
under `design-review/`, which say the commit they describe. History and pre-decision
thinking do not live here — they live in git and in the Gitea issue they belong to.

| Directory | What lands here |
|---|---|
| [`providers/`](providers/) | How each external provider integration works today |
| [`conventions/`](conventions/) | House rules, each with the lint or check that enforces it |
| [`features/`](features/) | In-flight feature plans. The `close` skill deletes a folder once its feature ships |
| [`design-review/`](design-review/) | Dated design reviews: findings with evidence as of one commit, not kept current. Their open work lives in the Gitea issues each README links |

Plus the glossary: [`ubiquitous-language.md`](ubiquitous-language.md) — the binding
vocabulary, kept current.

And the diagrams: [`diagrams/`](diagrams/), one file per area, Mermaid drawn from
the code. They are hand-kept, so a PR that changes a drawn flow updates its diagram.
Module boundaries in `services/go-api` are checked by `golangci-lint` (see `services/go-api/internal/README.md`), and in `client` by fallow and eslint (see `client/src/README.md`). In `services/overseer` they are checked by `golangci-lint` too (see `services/overseer/internal/README.md`).

**What does NOT go here:** brainstorms, plans, specs, ideation, handoffs. That
thinking belongs to a ticket — the Gitea issue is the spec. `docs/` holds only
what outlives the ticket. When you reach for a doc that isn't a provider note, a
convention, a diagram, or the glossary, it's a sign the thing wants to be
an issue instead.
