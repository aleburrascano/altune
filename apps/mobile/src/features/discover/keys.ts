/**
 * discoveryKeys — the single definition of the feature's React Query keys.
 *
 * Every cache interaction (read, invalidate, cancel, optimistic set) imports
 * from here. As scattered literals, a typo'd key doesn't fail compilation —
 * it silently no-ops an invalidation and surfaces as a stale-cache bug
 * (discover structure audit F3).
 */

export const discoveryKeys = {
  history: ['discovery', 'history'] as const,
  /** Prefix matching every search key — for cancelQueries over all searches. */
  searchPrefix: ['discovery', 'search'] as const,
  search: (query: string) => ['discovery', 'search', query] as const,
  suggest: (query: string) => ['discovery', 'suggest', query] as const,
};
