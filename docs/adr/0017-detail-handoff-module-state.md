# ADR-0017: Keep the detail handoff (and search state) as module-level state

- **Status:** Accepted
- **Date:** 2026-06-24
- **Deciders:** solo + Claude
- **Context tags:** [pattern | layer]

## Context

The discover→detail navigation seam is a module-level mutable holder:
`shared/lib/detail-handoff.ts` stashes the last-tapped `SearchResult` (client type
`DiscoveryResult`) in a `let _lastTapped`, written by `discover/tap` and
`library/useLibraryNavigation`, read by `DetailScreen` on mount. The discover
feature uses the same shape in `features/discover/search-state.ts` to preserve
the last query/input across a detail→back round-trip.

An `/improve-codebase-architecture` deepening pass (2026-06-24) flagged these as
shallow seams with low locality: navigation state living outside React and
outside any caller, with no explicit invalidation boundary (the holder persists
silently between navigations; a cold start happens to read `null`).

The friction is real. But on inspection the obvious "deepenings" are each worse
than the status quo, so this ADR records the decision to keep the pattern and
stops future reviews from re-litigating it.

## Decision

Keep the detail handoff and the discover search state as small module-level
holders exposing plain `set`/`get`/`clear` functions. Do **not** route the
tapped result through Expo Router params, and do **not** promote either holder
into a Zustand store or a context.

The detail screen is fed in-memory specifically to avoid a per-item backend
fetch (`view-result-detail` spec, Design Considerations). The holder is already
encapsulated behind functions — the field is not exported, callers cannot mutate
it directly.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Serialize the `SearchResult` into an Expo Router route param | A full result (sources, extras, image) is a large object to encode into a URL param. `rn-navigation.md` is explicit: "minimal navigation state — let Expo Router own it." This inverts that: it bloats the route to relocate state the router shouldn't carry. |
| Promote the holder into a Zustand store / React context | More machinery for a pattern that already works. The holder has set/get/clear and one read site; a store adds a provider, selectors, and subscription semantics with no behavioural gain. [vault: wiki/concepts/YAGNI Principle.md] — the ongoing complexity tax isn't justified by a hypothetical second reader. |
| Add read-once (`consume`) semantics to `getDetailHandoff` | `DetailScreen` reads on mount and may re-read across re-renders; read-once risks clearing state the screen still needs and trades a documented "stale until next write" model for a subtler one. [vault: wiki/concepts/KISS Principle.md]. |

## Consequences

### What becomes easier
- The detail screen stays a pure reader of an in-memory value — no fetch, no param decoding, no provider wiring.
- The seam is one tiny module per concern, trivially testable.

### What becomes harder
- The holder is mutable module state with no lifecycle: it persists between navigations and is only implicitly cleared by the next write or an explicit `clearDetailHandoff`. Accepted — the cold-start path (`null` → redirect to `/discover`) already handles the one case that matters.

### What we're committing to (and the cost to reverse)
- If a second client (web/desktop) or deep-linking-into-detail (a real URL for a result) lands, revisit: that is the point where route params or a persisted store stop being premature. Reversing this ADR is cheap — both holders are behind functions, so swapping the implementation is local to `detail-handoff.ts` / `search-state.ts` and their few call sites.

## Vault references

- [vault: wiki/concepts/YAGNI Principle.md]
- [vault: wiki/concepts/KISS Principle.md]

## Related

- Surfaced by: `/improve-codebase-architecture` deepening pass, 2026-06-24 (candidate 3)
- Spec: `docs/specs/view-result-detail/spec.md` (in-memory handoff, no per-item fetch)
- Rule: `.claude/rules/frontend/rn-navigation.md` (minimal navigation state)
