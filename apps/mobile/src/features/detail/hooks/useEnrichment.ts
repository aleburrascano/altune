import { getEnrichment, type EnrichmentResponse } from '@shared/api-client/enrichment';
import type { DiscoveryKind } from '@shared/api-client/discovery';

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
