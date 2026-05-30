# view-library — feature-local context

Mobile screen that renders the current user's track library as a paginated, infinite-scroll list with designed empty + error states. Reads the catalog API (`GET /v1/tracks`). The library is read-only here, but the `view-result-detail` save flow writes into this feature's `['library']` React Query cache optimistically (see `features/detail/save-cache.ts`).

## view-result-detail update

- **`LibraryRow` shows a `Pending` marker** (testID `library-row-pending-<id>`) when `track.acquisition_status === 'pending'` — a saved track whose audio hasn't been acquired yet. Omitted for any other status.
- `TrackResponse` gained `acquisition_status` + `artwork_url` (shared type); the row only consumes `acquisition_status`.

## Key terms

- **Track** — single audio recording with title + artist (+ optional album, duration). Mirror of `services/api/src/altune/domain/catalog/Track`. Defined globally in `docs/ubiquitous-language.md`.
- **`apiBase`** — the resolved API URL the bundle was built with. Comes from `EXPO_PUBLIC_API_URL` (Expo bakes env vars at bundle time, not runtime — restart `expo start --clear` after changing `.env`).
- **`has_more`** — server-derived pagination terminal flag. When false, `useInfiniteQuery`'s `getNextPageParam` returns `undefined` and React Query stops calling the queryFn.

## Patterns specific here

- **State machine lives in `state.ts` as a pure function** (`_viewForState`), separate from JSX. Loading > error > empty > list. Tests assert the helper directly; the JSX branches are trivial wrappers. Same pattern for `useLibrary.ts` (`_nextOffsetFromPage`, `_flattenPages`). Reason: jest-expo + RNTL was historically painful in this workspace; pure helpers stay testable regardless. See the AIDEV-NOTEs in those files.
- **React Query is the only server-state mechanism** per ADR-0005. Hook is named `useLibrary` to match the convention `use<Feature>`. New screens that fetch data add a sibling hook here, not a `useState`+`useEffect` pair.
- **TestIDs are load-bearing** for AC#5 / AC#6:
  - `library-empty` — designed empty state
  - `library-error` + `library-retry` — designed error state with retry
  - `library-loading` — initial-load spinner
  - `library-row-<track-id>` — each row
  Never rename these without updating `docs/specs/view-library/spec.md`.
- **`items[].id` is the FlatList `keyExtractor`** — the server guarantees uniqueness per user per session. Don't switch to index keys.
- **`ngrok-skip-browser-warning` header** is sent on every request (`shared/api-client/index.ts`). Required when the bundle is pointed at an ngrok-free tunnel for phone-on-LAN dev. Harmless against any other host. Drop when the API moves off ngrok in the dev loop.

## Known gotchas

- **React 19 + RN 0.81 dropped the global `JSX` namespace.** Component return types are `ReactElement` (imported from 'react'), not `JSX.Element`. SDK 51 → 54 upgrade caught this.
- **`EXPO_PUBLIC_API_URL` only applies at bundle build time.** Changing `.env` mid-session requires `npx expo start --clear`. Without `--clear`, Metro reuses the bundle with the stale URL — the symptom is "infinite loading spinner, no logs on the API".
- **Android emulator can't reach `127.0.0.1` of the host** — use `10.0.2.2` instead. iOS simulator can. Physical iPhone via Expo Go needs the host LAN IP, and Windows Firewall must allow inbound on the API port (or use ngrok to bypass).

<!-- AUTO-MAINTAINED:BEGIN -->
<!-- /update-nested-claude-md regenerates this block after every 3rd commit touching this folder.
     Do not hand-edit this block — your changes will be lost on next regeneration.
     Hand-edit above (Key terms / Patterns / Gotchas). -->

## Auto-maintained

### Files

- `state.ts` — `_viewForState` pure helper deriving `'loading' | 'error' | 'empty' | 'list'` from hook state.
- `hooks/useLibrary.ts` — `useInfiniteQuery` wrapper; `_nextOffsetFromPage` + `_flattenPages` helpers.
- `ui/LibraryScreen.tsx` — entrypoint; switches on `_viewForState` output; FlatList for the happy path.
- `ui/LibraryRow.tsx` — single track row (title + artist).

### Public API surface

- `LibraryScreen` (default export) — consumed by `apps/mobile/src/app/(tabs)/library.tsx` (Expo Router tab page). `app/index.tsx` now redirects to `/discover` (the default tab), not here.
- `useLibrary()` — re-usable by future features that show track lists (deep-link previews, etc.).

### Dependencies on other features / shared

- `@shared/api-client/tracks` — `getTracks` typed function.
- `@shared/ui` — design-system primitives (ADR-0008): `Screen`, `Text` (header `displayL` title), `Button` (retry), `Skeleton` (loading rows). Rows are text-forward (no album art in v1).
- `@shared/api-client/types` — `TrackResponse`, `ListTracksResponse` wire types.
- `@tanstack/react-query` — via the root `QueryClientProvider` in `src/app/_layout.tsx`.
- No cross-feature imports (per vertical-slice rule).

### Test files

- `__tests__/useLibrary.test.ts` — pagination helpers (6 tests).
- `__tests__/LibraryScreen.test.ts` — view state machine (6 tests).
- (RNTL component tests deferred; see `jest.config.js` history if curious about the jest-expo blocker that's now resolved.)

<!-- AUTO-MAINTAINED:END -->
