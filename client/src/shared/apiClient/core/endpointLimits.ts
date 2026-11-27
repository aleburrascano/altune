export const ENDPOINT_LIMITS = {
  album_tracks: { default: 250, max: 250 },
  artist_albums: { default: 50, max: 100 },
  artist_content_albums: { default: 100, max: 200 },
  artist_content_tracks: { default: 5, max: 50 },
  library_page: { max: 2000 },
  related_tracks: { default: 20, max: 50 },
  search: { default: 20, max: 50, max_offset: 200, max_query_runes: 200, max_query_words: 32 },
  search_history: { default: 10, max: 100 },
  suggest: { default: 5, max: 10 },
  top_tracks: { default: 5, max: 50 },
} as const;

type PagedEndpoint = {
  [E in keyof typeof ENDPOINT_LIMITS]: (typeof ENDPOINT_LIMITS)[E] extends { max: number }
    ? E
    : never;
}[keyof typeof ENDPOINT_LIMITS];

export function clampLimit(endpoint: PagedEndpoint, wanted: number): number {
  return Math.min(wanted, ENDPOINT_LIMITS[endpoint].max);
}
