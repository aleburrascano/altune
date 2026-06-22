/**
 * useEnrichment — fetch a result's MusicBrainz detail enrichment on detail open.
 *
 * Resolves genres / year / rating / external-ids and an HD cover. Gated to
 * results that carry a title (or an MBID); an unresolved entity comes back empty
 * and is surfaced as `enrichment: null` so the section hides. Off the search
 * path — one cached call per open (spec AC#8).
 */

import { useQuery } from '@tanstack/react-query';

import {
  getEnrichment,
  type DiscoveryKind,
  type EnrichmentResponse,
} from '@shared/api-client/discovery';

type UseEnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  mbid?: string | undefined;
  enabled?: boolean;
};

type UseEnrichmentReturn = {
  enrichment: EnrichmentResponse | null;
  isLoading: boolean;
  isError: boolean;
};

// hasContent reports whether a payload carries anything worth rendering. An
// unresolved entity (mbid:"" + empty everything) is treated as "nothing".
function hasContent(e: EnrichmentResponse): boolean {
  return (
    e.genres.length > 0 ||
    e.year > 0 ||
    e.rating > 0 ||
    e.artwork_url !== '' ||
    Object.keys(e.external_ids).length > 0
  );
}

export function useEnrichment({
  kind,
  title,
  subtitle,
  mbid,
  enabled = true,
}: UseEnrichmentParams): UseEnrichmentReturn {
  const canFetch = enabled && (title.trim() !== '' || (mbid ?? '') !== '');

  const { data, isLoading, isError } = useQuery({
    queryKey: ['enrichment', kind, mbid && mbid !== '' ? mbid : `${title}|${subtitle ?? ''}`],
    queryFn: () => getEnrichment({ kind, title, subtitle, mbid }),
    enabled: canFetch,
    staleTime: 1000 * 60 * 60 * 24,
  });

  return {
    enrichment: data && hasContent(data) ? data : null,
    isLoading,
    isError,
  };
}
