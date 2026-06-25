/**
 * useEnrichmentQuery — the shared skeleton behind every detail-open enrichment
 * hook (MusicBrainz / Deezer / Discogs / Last.fm / lyrics).
 *
 * Each provider hook varies only in four steps: its query key, its fetch fn, its
 * "is this enabled and valid" gate, and its `hasContent` predicate. This owns the
 * fixed flow they all share — the 24h cache, the single `useQuery` call, and the
 * "empty payload ⇒ null so the section hides" filter — and takes the varying
 * steps as values. Template Method, function-value form
 * (.claude/rules/design-patterns/behavioral/template-method.md).
 */

import { useQuery, type QueryKey } from '@tanstack/react-query';

// Enrichment is off the search path and effectively static — one cached call per
// detail open (spec AC#8). 24h stale time across every provider.
const ENRICHMENT_STALE_TIME = 1000 * 60 * 60 * 24;

type EnrichmentQuery<T> = {
  queryKey: QueryKey;
  queryFn: () => Promise<T>;
  // hasContent reports whether a payload carries anything worth rendering; an
  // unresolved entity comes back empty and is collapsed to null.
  hasContent: (data: T) => boolean;
  enabled: boolean;
};

type EnrichmentQueryResult<T> = {
  value: T | null;
  isLoading: boolean;
  isError: boolean;
};

export function useEnrichmentQuery<T>({
  queryKey,
  queryFn,
  hasContent,
  enabled,
}: EnrichmentQuery<T>): EnrichmentQueryResult<T> {
  const { data, isLoading, isError } = useQuery({
    queryKey,
    queryFn,
    enabled,
    staleTime: ENRICHMENT_STALE_TIME,
  });

  return {
    value: data && hasContent(data) ? data : null,
    isLoading,
    isError,
  };
}
