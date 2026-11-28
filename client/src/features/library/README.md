`library` owns how the saved library and its playlists are listed, sorted and edited.

The door, `index.ts`, exports only what `mobile` and the routes use: the screens (`LibraryScreen`, `PlaylistDetailScreen`, `FeaturingScreen`, `SidebarPlaylists`), `usePlaylistActions`, `LibraryProvider`, and the port `LibraryPort` with its adapter `httpLibraryPort`. Tests reach the parts directly.

- `core/`: the rules and types (`libraryGroups`, `libraryView`, `listPaging`, `gridColumns`, `libraryRoutes`, `libraryToDiscovery`, `pinnedStatusDisplay` and more). `errors.ts` holds the failure union and its wording helpers, and classifies the `@shared/failure` errors that the library port and the shared acquisition port raise. `queryKeys.ts` holds the track mutation keys; the query keys the library shares with other modules stay in `@shared/query`. Tracks and playlists are the `@shared/catalog` types. Library has no client store.
- `ports/ports.ts`: `LibraryPort`, the one port, filled by `mobile`.
- `adapters/http/`: `libraryHttp.ts` fills the port, and `mapping.ts` maps the `@shared/apiClient` album and artist groups to the `core` types. No other part imports `@shared/apiClient`.
- `view/`: `LibraryProvider.tsx` hands the port to the hooks and screens, the screens and their parts, and the `use<Thing>.ts` hooks, the only place that calls `useQuery` or `useMutation`. `trackMenuItems.ts` and `selectionActions.ts` build the menu and the bulk bar for the hooks that show them.
- `__tests__/`: every test.
