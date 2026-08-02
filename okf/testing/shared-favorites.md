---
type: TestSelection
title: Test selection — shared/favorites
description: Which of the twenty taxonomy categories apply to the mobile Favorites slice. Status - not started; the slice shipped with the Favorites feature and has no suite yet.
resource: apps/mobile/src/shared/favorites/
tags: [testing, mobile, shared, favorites]
---

SLICE: `apps/mobile/src/shared/favorites/`
TAXONOMY: [test-taxonomy](../playbooks/test-taxonomy.md)

**STATUS: not started.** The slice shipped on 2026-08-02 with the Favorites feature and carries no tests. The verdicts below are the selection made at authoring time, recorded now so the suite starts from a decision rather than from scratch. Concept: [shared-favorites](../mobile/shared-favorites.md).

## SELECTED

- **Table** — `isFavorite` over the `(kind, favorite_key)` set: present, absent, same key under a different kind, empty list, list not yet loaded (`data` undefined). The kind-discrimination row is the one that matters: an artist and a track can normalize to the same key when a self-titled track exists, and the composite is what keeps them apart.
- **Reducer** — `patched` is the slice's only state transition. Add to empty, add to non-empty, add something already present, remove present, remove absent, and the `total` field staying equal to `items.length` in every arm.
- **Optimistic update / rollback** — the reason this slice exists as more than three fetch wrappers. `onMutate` patches, `onError` restores the exact snapshot, `onSettled` invalidates. Cases: a failed add leaves the heart empty (not filled), a failed remove leaves it filled, and a rejection after a *second* toggle restores the snapshot that second toggle captured, not the original one.
- **Concurrency / ordering** — double-tapping the heart fires two mutations whose `isFavorite` reads both happen against the patched cache. Assert what the cache holds after both settle, and that the second mutation's `onMutate` snapshot is the first's optimistic state.
- **Cross-surface contract** — `Favorite`, `FavoritesResponse` and `FavoriteRef` field names derived at test time from `FavoriteDTO` / `FavoriteRequest` / `FavoritesResponse` in `services/go-api/internal/discovery/adapters/handler/favorites_endpoints.go`, and `favorite_key` from `SearchResultDTO` in `search_endpoints.go`. Never restated as literals.
- **Invariant / architecture** — nothing outside `shared/favorites/` imports `useFavorites.ts` or `ui/FavoriteButton.tsx` directly; `index.ts` is the only seam. And the load-bearing one: no file under `apps/mobile/` derives a favorite key — no call to a normalizer feeding a `favorite_key` comparison.
- **Failure injection** — `listFavorites` failing must leave every heart empty and never crash a result row; `addFavorite` / `removeFavorite` failing is the rollback case above.
- **Adversarial** — `favorite_key` arrives from the wire and is used as a `Set` member and a cache-patch discriminator. Empty string, a key containing the `|` separator the composite uses, and a missing `favorite_key` on a search result (which must render no heart rather than one keyed on `undefined`).
- **Accessibility** — `FavoriteButton`'s label flips between "Favorite X" and "Unfavorite X" with state; a heart whose label never changes is unusable with a screen reader.
- **Regression** — every survivor this run produces.

## REJECTED

- **Property** — no invariant here is worth generating over. The permutation law that justifies the category in `shared/playback` has no analogue: the state is a flat set, and its one law (`total === items.length`) is asserted directly under Reducer.
- **Persistence round-trip** — no disk I/O. The durable copy is the `discovery_favorites` table; this slice only holds the TanStack cache.
- **Liveness** — `FavoriteButton` reads through `useFavorites`, which subscribes to the query, so liveness is the optimistic-update category above rather than a separate claim.
- **Timing** — no debounce, no timer, no dwell.
- **Legacy / compat** — the endpoint shipped with this slice; there is no older payload shape to accept.
