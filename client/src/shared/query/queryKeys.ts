import type { PlaylistId } from '@shared/apiClient/ids';

export const libraryKeys = {
  summary: ['library', 'summary'] as const,
  tracksPrefix: ['library', 'tracks'] as const,
  tracks: (query: string, sort: string) => ['library', 'tracks', query, sort] as const,
  tracksAll: (query: string, sort: string) => ['library', 'tracks-all', query, sort] as const,
  lookupPrefix: ['library', 'lookup'] as const,
  lookup: (query: string) => ['library', 'lookup', query] as const,
  albumsPrefix: ['library', 'albums'] as const,
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

const ENRICHMENT_ROOTS = {
  musicbrainz: 'enrichment',
  deezer: 'deezer-enrichment',
  lastfm: 'lastfm-enrichment',
} as const;

export const detailKeys = {
  albumTracksPrefix: ['album-tracks'] as const,
  albumTracks: (
    provider: string,
    externalId: string,
    mbExternalId: string | undefined,
    knownIds = '',
  ) =>
    [...detailKeys.albumTracksPrefix, provider, externalId, mbExternalId ?? '', knownIds] as const,
  enrichment: (provider: keyof typeof ENRICHMENT_ROOTS, kind: string, cacheKey: string) =>
    [ENRICHMENT_ROOTS[provider], kind, cacheKey] as const,
  resolveEntity: (kind: string, query: string, limit: number) =>
    ['resolve-entity', kind, query, limit] as const,
  albumDiscoveryTracks: (provider: string | undefined, externalId: string | undefined) =>
    ['album-discovery-tracks', provider, externalId] as const,
  artistContent: (provider: string, externalId: string, knownIds: string) =>
    ['artist-content', provider, externalId, knownIds] as const,
  relatedTracks: (externalId: string) => ['related-tracks', externalId] as const,
};

export const playlistKeys = {
  list: ['playlists'] as const,
  paged: ['playlists', 'paged'] as const,
  details: ['playlist'] as const,
  detail: (playlistId: PlaylistId) => ['playlist', playlistId] as const,
};
