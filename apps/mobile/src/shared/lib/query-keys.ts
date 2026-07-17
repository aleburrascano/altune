/**
 * Query-key builders — the cache topology's single declaration.
 *
 * Every React Query key for the library/playlist families is built here, so the
 * feature hooks and the SSE patch layer (`@shared/events/*CachePatch`) agree on
 * keys by import, not by string coincidence. A key that stops being used gets
 * deleted here and find-references names every reader — the old `['library']`
 * infinite cache survived its deleted hook precisely because nothing owned it
 * (structure audit F3/F6).
 */

export const libraryKeys = {
  /** The full-library snapshot fetched by `useLibraryHome` (limit 2000). */
  home: ['library-home'] as const,
  /** Prefix matching every featuring query — for invalidation sweeps. */
  featuringPrefix: ['library', 'featuring'] as const,
  /** Saved tracks crediting a featured artist, keyed on the artist's stable identity. */
  featuring: (identity: string) => ['library', 'featuring', identity] as const,
};

export const playlistKeys = {
  /** The playlists list (`getPlaylists`). */
  list: ['playlists'] as const,
  /** Prefix matching every playlist detail — for setQueriesData/invalidate sweeps. */
  details: ['playlist'] as const,
  /** One playlist detail (`getPlaylist`). */
  detail: (playlistId: string) => ['playlist', playlistId] as const,
};
