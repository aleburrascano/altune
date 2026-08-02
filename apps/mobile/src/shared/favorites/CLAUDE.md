# shared/favorites — router

The user's Favorites: a saved set of artists, albums and tracks that lifts matching results in Discovery search.

Layout:

- `useFavorites.ts` — the one query (`discoveryKeys.favorites`) plus the optimistic add/remove toggle.
- `ui/FavoriteButton.tsx` — the heart; the only surface that toggles a Favorite.
- `index.ts` — the public seam (`useFavorites`, `FavoriteButton`).

Invariants:

- A Favorite's identity is the server's `favorite_key`, echoed from the wire — never compute or normalize it on the device.
- Favoriting an artist lifts that artist's tracks and albums too; the client never models that fan-out, the server does.
- Every write is optimistic and rolls back on error; the list is invalidated on settle.
- Feature UIs reach Favorites only through `index.ts` — nothing imports `useFavorites.ts` or the button file directly.

Tests: none yet; author per `okf/playbooks/test-taxonomy.md` — the per-category verdict is already recorded in `okf/testing/shared-favorites.md`.

Dependencies: `@shared/api-client/favorites`, `@shared/lib/query-keys`, `@shared/ui` (+ `primitives/IconButton`), `@tanstack/react-query`, `expo-haptics`.

Knowledge base: `okf/mobile/shared-favorites.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
