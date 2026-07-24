export const libraryKeys = {
  home: ['library-home'] as const,
  featuringPrefix: ['library', 'featuring'] as const,
  featuring: (identity: string) => ['library', 'featuring', identity] as const,
};

export const discoveryKeys = {
  history: ['discovery', 'history'] as const,
  searchPrefix: ['discovery', 'search'] as const,
  search: (query: string) => ['discovery', 'search', query] as const,
  suggest: (query: string) => ['discovery', 'suggest', query] as const,
  lyrics: (title: string, artist: string) => ['discovery', 'lyrics', title, artist] as const,
};

export const playlistKeys = {
  list: ['playlists'] as const,
  details: ['playlist'] as const,
  detail: (playlistId: string) => ['playlist', playlistId] as const,
};
