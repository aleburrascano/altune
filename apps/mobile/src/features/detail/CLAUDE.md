# detail — feature-local router

Read-only detail screen for a tapped discovery result (`view-result-detail` spec), fed by an in-memory handoff with no per-item backend fetch. A track can be saved to the library with an optimistic UI and a visible acquire lifecycle. One vertical scroll: header → per-kind body → optional `Disclosure`.

Layout:

- `ui/DetailScreen.tsx` — entrypoint and header; `ui/PlayButton.tsx`, `ui/TrackSaveControl.tsx`, `ui/SaveGlyph.tsx`; per-kind bodies and `DiscographySections`.
- `extras.ts` — `resolveFeatured`, `extractFeaturedFromText`. `extras-accessors.ts` — narrowing for the untyped wire map.
- `play-source.ts` — `resolvePlaySource`. `save-control-state.ts` — lifecycle state + labels. `save-cache.ts` — the create-request mapper and the optimistic placeholder. `hooks/useOwnedTrack.ts` — server ownership stamp overlaid with the live acquisition status.
- `navigation.ts` — `openDetail`. `hooks/` — `useSaveTrack`, `useLateralNav`, `useAlbumTracks`, `useArtistContent`, `useDetailEnrichments`, `useEnrichResult`, `useOwnedTrack`, `useAlbumDetailState`, `useArtistDetailState`.
- `__tests__/` — `extras`, `play-source`, `save-control-state`, `save-cache`, `owned-playback`, `useSaveTrack`, `useLateralNav`, `DetailScreen`.

Dependencies: `@shared/lib/detail-handoff` (the discover↔detail seam), `@shared/api-client/{tracks,discovery,enrichment}`, `@shared/ui/primitives/*` (imported directly, not the barrel), `@tanstack/react-query`. No cross-feature imports.

## Rules

- Open a detail screen only through `openDetail` — it writes the handoff, then pushes.
- Redirect to `/discover` on an empty handoff; never render a detail screen without one.
- Keep logic in the pure helpers and the JSX a thin wrapper.
- Narrow every `extras` key before use; absent and empty values are omitted.
- Read ownership from the server stamp (`owned_track_id` / `owned_acquisition_status`), never by scanning a library cache.
- Fetch artist top-tracks and albums in one call; never merge per-provider discographies on the device.
- Never save a Track with a null artist — the control disables and `onSave` short-circuits.
- Never let `useEnrichResult` match on title alone, and never let it overwrite stored library `extras`.
- Never reset `searchingRef` after a successful push, or lateral nav duplicates screens.
- Never let Deezer contribute new titles when the MB identity is verified and non-empty.
- Never surface a provider's items when its payload status is not ok; error only when every provider failed.
- Never nest a ScrollView — album and artist detail use one.
- Keep the back button outside the ScrollView, and check `router.canGoBack()` before `router.back()`.
- Every tappable element needs `accessibilityRole` + `accessibilityLabel`; touch targets ≥48pt.
- Never rename a load-bearing testID without updating `docs/specs/view-result-detail/`.

Load-bearing testIDs — header: `detail-header`, `detail-back`, `detail-artist-link`. Track: `detail-track-info`, `detail-play`, `detail-preview`, `detail-save`, `detail-save-error`, `detail-info-album`, `detail-info-featuring`, `detail-lateral-error`. Album: `detail-tracklist{,-loading,-error,-empty}`, `detail-track-<n>`, `detail-track-save-<n>`, `detail-album-meta`, `detail-more-from-album`. Artist: `detail-artist-content`, `detail-top-tracks-{loading,error}`, `detail-top-track-<n>`, `detail-top-track-save-<n>`, `detail-show-all-tracks`, `detail-albums-{loading,error}`, `detail-{album,single,ep}-<n>`, `detail-artist-about`.

Routing: a stack screen nested in each tab — `app/(tabs)/discover/detail.tsx` and `app/(tabs)/library/detail.tsx` render the same component, which uses `useSegments()` to build correct push paths.

Why each rule exists: `okf/mobile/detail-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
