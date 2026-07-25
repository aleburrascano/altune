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
    hasContent: (e) => e.has_content,
    enabled: enabled && (title.trim() !== '' || (mbid ?? '') !== ''),
  });

  return { enrichment: value, isLoading, isError };
}
