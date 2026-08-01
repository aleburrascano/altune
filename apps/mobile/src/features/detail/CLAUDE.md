# detail — feature-local router

Read-only detail screen for a tapped discovery result (`view-result-detail` spec, reworked by `detail-screens-rework`), fed by an in-memory handoff with no per-item backend fetch. A track can be saved to the library with an optimistic UI and a visible acquire lifecycle. One vertical scroll owned by `ui/DetailScaffold.tsx`: full-bleed artwork banner → action row → fact row → sections.

Layout:

- `ui/DetailScreen.tsx` — resolves the handoff and dispatches to one per-kind body; owns the banner title/secondary line and the back handler, nothing else.
- `ui/DetailScaffold.tsx` — the one screen skeleton: floating app bar, banner, `actions`/`facts` slots, section children. `ui/DetailActions.tsx` — the grow-to-fill primary pill plus `SecondaryAction`. `ui/DetailFacts.tsx` — the labelled fact row. `ui/Section.tsx` — the one section header.
- `ui/TrackDetailBody.tsx`, `ui/AlbumDetailBody.tsx`, `ui/ArtistDetailBody.tsx` — fill the scaffold's slots; `ui/TrackSaveControl.tsx`, `ui/SaveGlyph.tsx`, `ui/AlbumTrackRow.tsx`, `ui/DiscographySections.tsx`, `ui/RelatedTracksSection.tsx`, `ui/LastFmEnrichmentSection.tsx`, `ui/DetailSkeleton.tsx`, `ui/helpers.ts`.
- `extras.ts` — `resolveFeatured`, `extractFeaturedFromText`. `extras-accessors.ts` — narrowing for the untyped wire map.
- `play-source.ts` — `resolvePlaySource`, `isResultPlaying`. `save-control-state.ts` — lifecycle state + labels. `save-cache.ts` — the create-request mapper and the optimistic placeholder. `hooks/useOwnedTrack.ts` — server ownership stamp overlaid with the live acquisition status.
- `navigation.ts` — `openDetail`. `hooks/` — `useSaveTrack`, `useLateralNav`, `useAlbumTracks`, `useArtistContent`, `useDetailEnrichments`, `useEnrichResult`, `useOwnedTrack`, `useAlbumDetailState`, `useArtistDetailState`.
- `__tests__/` — `play-source`; the rest is rebuilt per `okf/playbooks/test-taxonomy.md`.

Dependencies: `@shared/lib/detail-handoff` (the discover↔detail seam), `@shared/api-client/{tracks,discovery,enrichment}`, `@shared/ui/primitives/*` (imported directly, not the barrel), `@shared/playlists` (the picker), `@tanstack/react-query`. No cross-feature imports.

## Rules

- Open a detail screen only through `openDetail` — it writes the handoff, then pushes.
- Redirect to `/discover` on an empty handoff; never render a detail screen without one.
- Keep logic in the pure helpers and the JSX a thin wrapper.
- Narrow every `extras` key before use; absent and empty values are omitted.
- Read ownership from the server stamp (`owned_track_id` / `owned_acquisition_status`), never by scanning a library cache.
- Fetch artist top-tracks and albums in one call; never merge per-provider discographies on the device.
- Never save a Track with a null artist — the control disables and `onSave` short-circuits.
- Add an unowned Track to a playlist by saving it first and adding the returned id; never send an optimistic placeholder id to the server.
- Never let `useEnrichResult` match on title alone, and never let it overwrite stored library `extras`.
- Never reset `searchingRef` after a successful push, or lateral nav duplicates screens.
- Never let Deezer contribute new titles when the MB identity is verified and non-empty.
- Never surface a provider's items when its payload status is not ok; error only when every provider failed.
- Never nest a ScrollView — `DetailScaffold` owns the only one.
- Keep the back button outside the ScrollView, and check `router.canGoBack()` before `router.back()`.
- Render every kind through `DetailScaffold`; a body supplies slot content and never its own header, action layout or section heading.
- Put only intrinsic facts in the fact row — anything navigable belongs in a section row, and an absent value omits its cell rather than rendering empty.
- Resolve ownership through `useOwnedTrack(extras, identity)` so an unowned track goes live on save; never read it from a static handoff alone.
- Show one discography rail behind record-type chips, never a stacked rail per type.
- Keep the collapsing app-bar title hidden from accessibility; it duplicates the banner title.
- Never add a detail action that has no backing behaviour in the feature's hooks.
- Read the play/pause state from `isResultPlaying` — every source the result can play, not just the one `resolvePlaySource` would start next.
- Every tappable element needs `accessibilityRole` + `accessibilityLabel`; touch targets ≥48pt.
- Never rename a load-bearing testID without updating `docs/specs/view-result-detail/`.

Load-bearing testIDs — scaffold: `detail-header`, `detail-back`, `detail-banner-title`, `detail-menu`, `detail-artist-link`. Track: `detail-track-info`, `detail-track-facts`, `detail-play`, `detail-preview`, `detail-save`, `detail-add-to-playlist`, `detail-save-error`, `detail-info-album`, `detail-info-featuring`, `detail-lateral-error`. Album: `detail-tracklist{,-loading,-error,-empty}`, `detail-track-<n>`, `detail-track-save-<n>`, `detail-album-meta` (the fact row), `detail-album-play`, `detail-save-all`, `detail-more-from-album`. Artist: `detail-artist-content`, `detail-artist-facts`, `detail-artist-play`, `detail-top-tracks-{loading,error}`, `detail-top-track-<n>`, `detail-top-track-save-<n>`, `detail-show-all-tracks`, `detail-albums-{loading,error}`, `detail-{album,single,ep}-<n>`, `detail-artist-about`.

Routing: a stack screen nested in each tab — `app/(tabs)/discover/detail.tsx` and `app/(tabs)/library/detail.tsx` render the same component, which uses `useSegments()` to build correct push paths.

Why each rule exists: `okf/mobile/detail-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
