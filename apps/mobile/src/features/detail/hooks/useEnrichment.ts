/**
 * useEnrichment — fetch a result's MusicBrainz detail enrichment on detail open.
 *
 * Resolves genres / year / rating / external-ids and an HD cover. Gated to
 * results that carry a title (or an MBID); an unresolved entity comes back empty
 * and is surfaced as `enrichment: null` so the section hides. Off the search
 * path — one cached call per open (spec AC#8).
 */

import {
  getEnrichment,
  type DiscoveryKind,
  type EnrichmentResponse,
} from '@shared/api-client/discovery';

import { useEnrichmentQuery } from './useEnrichmentQuery';

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
  const { value, isLoading, isError } = useEnrichmentQuery({
    queryKey: ['enrichment', kind, mbid && mbid !== '' ? mbid : `${title}|${subtitle ?? ''}`],
    queryFn: () => getEnrichment({ kind, title, subtitle, mbid }),
    hasContent,
    enabled: enabled && (title.trim() !== '' || (mbid ?? '') !== ''),
  });

  return { enrichment: value, isLoading, isError };
}
