# ADR-0005: Mobile server-state — @tanstack/react-query

- **Status:** Accepted
- **Date:** 2026-05-27
- **Deciders:** solo + Claude
- **Context tags:** [tech-stack, mobile, dependency, policy]

## Context

The `view-library` feature introduces the first mobile screen that fetches data from the API. ADR-0002 mandated "no global state library without an ADR" — which is about *client* state. Server state (cached responses, request lifecycles, retry, invalidation) is a separate concern and the line was never explicit. This ADR makes it explicit: every mobile feature that reads from or writes to the backend uses React Query (`@tanstack/react-query`); no parallel `useState` + `useEffect` cargo cult.

What forced the decision *now*: Slice 9 of `view-library`'s plan introduces a `useLibrary` hook backed by `useInfiniteQuery`. The convention this hook establishes — wrap API calls in React Query, derive `isLoading`/`error`/`hasNextPage`/pagination accumulation from the hook — will be copied by the next mobile feature. Setting it deliberately now (rather than letting it accrete) keeps the codebase coherent.

## Decision

Adopt **`@tanstack/react-query`** as the canonical mobile server-state library. The rule is:

- Every mobile feature that talks to the API uses React Query (`useQuery`, `useInfiniteQuery`, `useMutation`).
- Hooks are named `use<Feature>` (e.g., `useLibrary`, `useTrack`) and live under `apps/mobile/src/features/<feature>/hooks/`.
- The shared API client lives at `apps/mobile/src/shared/api-client/` — typed functions that React Query hooks call. Tests mock the API client, not React Query itself.
- A single `QueryClientProvider` wraps the app at the Expo Router root (`apps/mobile/src/app/_layout.tsx`).

Configuration defaults: `staleTime: 30_000` (30s) is the starting point; per-hook overrides allowed when the data shape warrants. Retries default to React Query's defaults; refine when a real failure mode demands it.

## Alternatives considered

| Alternative | Why not |
|---|---|
| **Raw `useState` + `useEffect` in each feature** | Reinvents loading/error/cache invalidation per feature; we'd diverge in subtle ways. ADR-0002's "no global state without ADR" rule was written to prevent this kind of accretion. |
| **SWR** | Comparable to React Query in shape; smaller ecosystem; less TypeScript polish at the time of writing. Not worth swapping when React Query is the dominant choice in the Expo + RN community. |
| **Redux Toolkit Query (RTKQ)** | Forces Redux as a transitive dependency. Larger surface area than we need for v1. If Redux ever lands for true *client* state (theme, navigation modals), revisit — but probably not. |
| **Bespoke fetch wrapper** | Same shape as "raw useState + useEffect," dressed up. Provides no caching, no request dedup, no invalidation — features we'd reimplement badly. |

## Consequences

### What becomes easier
- Loading, error, and cache invalidation states come for free per hook.
- Server-state has one mental model across features.
- The plan-reviewer subagent has an unambiguous standard to grade against ("does this feature use React Query?").
- Pagination (`useInfiniteQuery`) supports `view-library`'s infinite-scroll AC out of the box.

### What becomes harder
- One more dependency to keep current (`@tanstack/react-query` minor versions ship frequently).
- Tests for hooks need a `QueryClientProvider` wrapper — easy boilerplate, but easy to forget; the plan-reviewer should grep for it.
- React Query's `staleTime`/`cacheTime`/refetch defaults are subtle. Defaults are fine for v1; tuning happens per feature only when a real symptom appears.

### What we're committing to (and the cost to reverse)
- **React Query for all server state.** Reversing means rewriting every hook in every feature; cost grows linearly with feature count. Cheap to reverse for v1 (one feature pending); expensive after the third feature lands.
- **QueryClientProvider at the Expo Router root.** Trivial to add or remove; no architectural impact.

## Vault references

- [vault: wiki/concepts/Vertical Slice Architecture.md] — server-state hooks live inside the feature slice; the shared client is the only cross-slice infrastructure.

(No dedicated React Query note in the software-architecture-design vault — this ADR is the project's local convention.)

## Related

- Predecessor: `docs/adr/0002-stack-expo-fastapi.md` — explicitly deferred mobile state-library decisions to a future ADR; this is that ADR.
- Driving feature: `docs/specs/view-library/spec.md` (mobile section), `docs/specs/view-library/plan.md` (slices 8–10).
- Triggering moment: slice 9 of `view-library` introduces the first `useInfiniteQuery` hook.
