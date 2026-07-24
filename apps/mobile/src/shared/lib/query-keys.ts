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

/**
 * Discovery cache keys. Promoted here from `features/discover/keys.ts` when
 * Settings became a second consumer (it owns "Clear search history", which must
 * invalidate `history` after the server delete) — the 2+-consumers promotion
 * rule, and no cross-feature import.
 */
export const discoveryKeys = {
  history: ['discovery', 'history'] as const,
  /** Prefix matching every search key — for cancelQueries over all searches. */
  searchPrefix: ['discovery', 'search'] as const,
  search: (query: string) => ['discovery', 'search', query] as const,
  suggest: (query: string) => ['discovery', 'suggest', query] as const,
  /** Lyrics for one track, keyed on the (title, artist) pair the endpoint takes. */
  lyrics: (title: string, artist: string) => ['discovery', 'lyrics', title, artist] as const,
};

export const playlistKeys = {
  /** The playlists list (`getPlaylists`). */
  list: ['playlists'] as const,
  /** Prefix matching every playlist detail — for setQueriesData/invalidate sweeps. */
  details: ['playlist'] as const,
  /** One playlist detail (`getPlaylist`). */
  detail: (playlistId: string) => ['playlist', playlistId] as const,
};
