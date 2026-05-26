# <Feature Name>

> Spec for `<feat-name>` — version 1, drafted YYYY-MM-DD.
> Authors: solo + Claude.
> Status: Draft | Clarify-gated | Ready-for-plan | Planned | Shipped.

## Problem

<2–4 sentences on the user pain. No solutions yet. Quote the user if possible.>

## User value

<What changes for the user when this ships? Concrete, observable.>

## Acceptance criteria

Each one is testable. Each one will become at least one automated test.

1. **AC#1** — Given <precondition>, when <action>, then <observable outcome>.
2. **AC#2** — …
3. **AC#3** — …

## Out of scope

Explicit non-goals. Things people might assume but we're not doing:

- …
- …

## Design considerations

Patterns + trade-offs surfaced by the vault lookup (see `.claude/rules/vault-consultation.md`):

- [vault: wiki/concepts/<pattern>.md] — why it applies here
- [vault: wiki/topics/<topic>.md] — broader context

High-level approach (not implementation detail — that's the plan):

- This is a <read | write | mixed> path in the `<context>` bounded context.
- It <does | does not> require a new aggregate / value object / port.
- It <does | does not> introduce a new external dependency (if yes, ADR required).

## Dependencies

What this feature requires that must already exist or be built first:

- **Bounded contexts**: <list, or "none">
- **Other features**: <list, or "none">
- **External services**: <list, or "none">
- **Library/framework additions**: <list, or "none">

## Risks / open questions

- **Risk**: <what could go wrong> — mitigation: <if any>
- **Open question**: <thing we don't know yet> — to resolve via: <how>

## Telemetry

What we'd log / measure in production to know this works:

- **Log events**: <list — typically tied to domain events>
- **Metrics**: <list — typically rates, latencies, error counts>
- **Alerts**: <conditions that should page>

## Related

- `[vault: ...]` references applied above
- Related ADRs: `docs/adr/NNNN-...`
- Predecessor feature specs (if this is iterative): `docs/specs/<other>/spec.md`
