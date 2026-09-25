import type { PlaylistId } from '@shared/api-client/ids';

export const libraryKeys = {
  summary: ['library', 'summary'] as const,
  tracksPrefix: ['library', 'tracks'] as const,
  tracks: (query: string, sort: string) => ['library', 'tracks', query, sort] as const,
  tracksAll: (query: string, sort: string) => ['library', 'tracks-all', query, sort] as const,
  lookupPrefix: ['library', 'lookup'] as const,
  lookup: (query: string) => ['library', 'lookup', query] as const,
  albumsPrefix: ['library', 'albums'] as const,
  // A capped read is a different response from an uncapped one, so it caches under
  // its own key instead of being served the shorter default page (#1668).
  albums: (query: string, sort: string, limit?: number) =>
    limit === undefined
      ? (['library', 'albums', query, sort] as const)
      : (['library', 'albums', query, sort, limit] as const),
  artistsPrefix: ['library', 'artists'] as const,
  artists: (query: string, sort: string) => ['library', 'artists', query, sort] as const,
  featuringPrefix: ['library', 'featuring'] as const,
  featuring: (identity: string) => ['library', 'featuring', identity] as const,
};

export const discoveryKeys = {
  history: ['discovery', 'history'] as const,
  favorites: ['discovery', 'favorites'] as const,
  searchPrefix: ['discovery', 'search'] as const,
  search: (query: string) => ['discovery', 'search', query] as const,
  suggest: (query: string) => ['discovery', 'suggest', query] as const,
  lyrics: (title: string, artist: string) => ['discovery', 'lyrics', title, artist] as const,
};

export function isSearchKeyFor(queryKey: readonly unknown[], query: string): boolean {
  return queryKey[2] === query;
}

export const detailKeys = {
  albumTracksPrefix: ['album-tracks'] as const,
  albumTracks: (provider: string, externalId: string, mbExternalId: string | undefined) =>
    [...detailKeys.albumTracksPrefix, provider, externalId, mbExternalId ?? ''] as const,
};

export const playlistKeys = {
  list: ['playlists'] as const,
  // The library grid walks the collection a page at a time, so its cache entry holds
  // pages where list holds one response. It sits *under* list so that every existing
  // invalidation of list reaches the grid too (#1708).
  paged: ['playlists', 'paged'] as const,
  details: ['playlist'] as const,
  detail: (playlistId: PlaylistId) => ['playlist', playlistId] as const,
};
