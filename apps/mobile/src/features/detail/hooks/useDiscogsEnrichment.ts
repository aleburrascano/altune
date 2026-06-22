/**
 * useDiscogsEnrichment — fetch an album's Discogs detail enrichment on open.
 *
 * Resolves credits / styles / label-catalog / community for an album from its
 * title + artist. Album-scoped (Discogs credits hang off the master, not the
 * artist), so the caller gates this to `kind === 'album'`. An unresolved album
 * comes back empty and is surfaced as `enrichment: null` so the section hides.
 * Off the search path — one cached call per open (docs/providers/discogs.md).
 */

import { useQuery } from '@tanstack/react-query';

import {
  getDiscogsEnrichment,
  type DiscogsEnrichmentResponse,
} from '@shared/api-client/discovery';

type UseDiscogsEnrichmentParams = {
  album: string;
  artist?: string | null | undefined;
  enabled?: boolean;
};

type UseDiscogsEnrichmentReturn = {
  enrichment: DiscogsEnrichmentResponse | null;
  isLoading: boolean;
  isError: boolean;
};

// hasContent reports whether a payload carries anything worth rendering. An
// unresolved album (master_id:0 + empty everything) is treated as "nothing".
function hasContent(e: DiscogsEnrichmentResponse): boolean {
  return (
    e.credits.length > 0 ||
    e.styles.length > 0 ||
    e.labels.length > 0 ||
    e.formats.length > 0 ||
    e.companies.length > 0 ||
    e.community.have > 0 ||
    e.community.rating > 0
  );
}

export function useDiscogsEnrichment({
  album,
  artist,
  enabled = true,
}: UseDiscogsEnrichmentParams): UseDiscogsEnrichmentReturn {
  const canFetch = enabled && album.trim() !== '';

  const { data, isLoading, isError } = useQuery({
    queryKey: ['discogs-enrichment', `${album}|${artist ?? ''}`],
    queryFn: () => getDiscogsEnrichment({ album, artist }),
    enabled: canFetch,
    staleTime: 1000 * 60 * 60 * 24,
  });

  return {
    enrichment: data && hasContent(data) ? data : null,
    isLoading,
    isError,
  };
}
