# shared — seam map

Code used by more than one feature. Imported as `@shared/<folder>/...`. Features may import
anything here; **nothing in `src/shared` may import from `src/features`** (enforced by the
`import/no-restricted-paths` zone in `apps/mobile/eslint.config.js` and the `shared` boundary
zone in `apps/mobile/.fallowrc.json`, which also fails on import cycles; tests are exempt).
The subfolder rules below are convention, not tooling.
Code that only one feature uses belongs in that feature, not here.

## Subfolders

| Folder         | Responsibility                                                                                           |
| -------------- | -------------------------------------------------------------------------------------------------------- |
| `api-client/`  | The only HTTP boundary to the Go API: `apiFetch`, per-domain endpoints, wire types, decoders and errors. |
| `auth/`        | Supabase session: client, `useSession`/`useSignOut`, the session-expired flag and the sign-out registry. |
| `lib/`         | Small domain-agnostic helpers: react-query keys, formatting, error copy, view mapping, detail handoff.   |
| `ui/`          | Design system: theme tokens, primitives, motion, tab bar, screen boundary, accessibility announcements.  |
| `events/`      | Server-sent events: the SSE client, the event router, and the per-domain cache/store patches.            |
| `acquisition/` | Client state of track downloads on the server: in-flight download list, per-track status, their UI bar.  |
| `offline/`     | On-device pinned (offline) audio: the pinned store, its on-disk index, files and download worker.        |
| `playback/`    | The playback _model_: `PlaybackContext` contract, queue store and track mapping (no native player).      |
| `favorites/`   | Favorite toggle: `useFavorites` query/mutation and `FavoriteButton`.                                     |
| `playlists/`   | Playlist mutations and the add-to-playlist / create-playlist sheets used from several screens.           |
| `telemetry/`   | Discovery event recording: session id, `recordEvent`, and the persisted, per-user retry outbox.          |

### Notable files

- `api-client/index.ts` — `apiFetch` and `apiBase`; attaches the Supabase token, applies the
  request deadline (`deadline.ts`), and marks the session expired on a 401.
- `api-client/wireDecoders.ts` — primitive narrowers (`asRecord`, `asString`, ...). Each domain's
  response parsers live beside its endpoints (`tracks.ts`, `library.ts`, `discovery.ts`,
  `playlists.ts`). `api-client/parse.ts` is only a compatibility barrel re-exporting them; new code
  should import from the owning file.
- `api-client/ids.ts` — branded ids (`TrackId`, ...) and `isSafeId`; `types.ts` — response shapes.
- `api-client/trackAcquisition.ts` — `toPending` / `toReady` / `toFailed`, the only way to build a
  track's acquisition state (`TrackAcquisition`, a union keyed on `acquisition_status`). Cache
  patches take the whole triple, so a non-failed track never keeps stale failure text.
- `auth/signOutCleanup.ts` — the `onSignOut` registry, `hasSignedInUser`, and the session epoch
  mutations use to drop late callbacks from a previous user.
- `events/applyServerEvent.ts` — thin router. It merges `RESYNC_HANDLERS` (`resyncEvents.ts`),
  `ACQUISITION_HANDLERS` (`acquisitionEvents.ts`) and `PLAYLIST_HANDLERS` (`playlistEvents.ts`);
  the handler table is typed so an event in `eventTypes.ts` with no handler fails to compile.
  `trackCachePatch.ts` / `playlistCachePatch.ts` hold the react-query cache edits the handlers
  share; `useServerEvents.ts` owns the connection.
- `offline/pinnedStore.ts` — the zustand store and the folder's public port (`usePinnedStore`,
  `claimPinnedDownloads`, `pinnedUri`, `repinIfPinned`, byte totals). It composes
  `pinnedIndex.ts` (persisted index + owner marker), `pinnedFiles.ts` (expo-file-system adapter)
  and `pinnedDownloadWorker.ts` (sequential download queue). Import the store, not the parts.
- `acquisition/audioCacheInvalidation.ts` — registry of callbacks run when a track's audio changes.
- `lib/query-keys.ts` — every react-query key family; any code that reads or patches the cache uses it.

## Allowed dependencies

Production code only (tests may import across freely). This table matches the imports on `main`.
Anything not listed is not an intended dependency — add it here in the same PR if you need it.

| Folder         | May import from                                                                                                     |
| -------------- | ------------------------------------------------------------------------------------------------------------------- |
| `auth/`        | nothing in shared, except `useSession.ts` / `useSignOut.ts` → `acquisition`, `offline`, `telemetry` (resets, below) |
| `api-client/`  | `auth` (`supabaseClient`, `sessionExpired` only)                                                                    |
| `lib/`         | `api-client` (types only), `auth` (`signOutCleanup` only)                                                           |
| `ui/`          | `lib`                                                                                                               |
| `acquisition/` | `api-client`, `ui`                                                                                                  |
| `offline/`     | `api-client`, `auth` (`signOutCleanup` only)                                                                        |
| `playback/`    | `api-client`                                                                                                        |
| `telemetry/`   | `api-client`                                                                                                        |
| `favorites/`   | `api-client`, `lib`, `ui`                                                                                           |
| `playlists/`   | `api-client`, `lib`, `ui`                                                                                           |
| `events/`      | `api-client`, `auth` (`supabaseClient`), `lib`, `acquisition`, `offline`                                            |

The auth entries are the one place a low-level folder reaches into higher ones. The files on each
side are leaves (`supabaseClient.ts`, `sessionExpired.ts`, `signOutCleanup.ts` import nothing from
shared), so there is no import cycle; keep it that way.

## Intentional cross-module calls

These are deliberate couplings. Everything else should go through a folder's own public surface.

1. **Identity change resets per-user state** — `auth/useSession.ts` (on any user switch) and
   `auth/useSignOut.ts` call `queryClient.clear()`, `useDownloadStore.getState().reset()`,
   `useTrackStatusStore.getState().reset()`, `clearOutbox()` and then `runSignOutCleanups()`.
   `useSession` also calls `clearSessionExpired()`, `setOutboxOwner(userId)` and
   `claimPinnedDownloads(userId)`.
2. **Sign-out registry (`auth/signOutCleanup.onSignOut`)** — the preferred way for new per-user
   state to be cleared, instead of adding another import to `useSession`. Registered by
   `offline/pinnedStore.ts` (`unpinAll`) and `lib/detail-handoff.ts` (`clearDetailHandoff`); outside
   shared by `features/discover/search-state.ts`, `features/playback/registerPlaybackService.ts`
   and `features/playback/hooks/trackPlayerProvider.tsx`.
3. **401 marks the session expired** — `api-client/index.ts` calls `auth/sessionExpired.markSessionExpired()`.
4. **Authenticated transport** — `api-client/index.ts`, `api-client/audio.ts` and
   `events/useServerEvents.ts` read the token from `auth/supabaseClient`; `useServerEvents` also
   uses `api-client.apiBase`.
5. **Acquisition events drive stores** — `events/acquisitionEvents.ts` writes
   `acquisition/trackStatusStore` and `acquisition/downloadStore`, calls
   `acquisition/audioCacheInvalidation.invalidateAudioCaches` and `offline/pinnedStore.repinIfPinned`
   when a track's audio changes.
6. **Audio cache invalidation registry** — `features/playback/service.ts` registers its cache
   eviction via `registerAudioCacheInvalidator`, so `events` never imports the native player.
7. **Playback contract** — `playback/PlaybackContext.ts` is implemented by
   `features/playback/hooks/PlaybackProvider.tsx`; shared code and other features call
   `usePlayback()` and never the native player.
8. **App-root mounts** — `src/app/_layout.tsx` runs `events/useServerEvents()` and mounts
   `offline/OfflineReconcileBridge`.
