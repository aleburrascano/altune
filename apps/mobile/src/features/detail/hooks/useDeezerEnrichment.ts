import { getDeezerEnrichment, type DeezerEnrichmentResponse } from '@shared/api-client/enrichment';
import type { DiscoveryKind } from '@shared/api-client/discovery';

import { useEnrichmentQuery } from './useEnrichmentQuery';

type UseDeezerEnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  enabled?: boolean;
};

type UseDeezerEnrichmentReturn = {
  enrichment: DeezerEnrichmentResponse | null;
  isLoading: boolean;
  isError: boolean;
};

export function useDeezerEnrichment({
  kind,
  title,
  subtitle,
  enabled = true,
}: UseDeezerEnrichmentParams): UseDeezerEnrichmentReturn {
  const { value, isLoading, isError } = useEnrichmentQuery({
    queryKey: ['deezer-enrichment', kind, `${title}|${subtitle ?? ''}`],
    queryFn: () => getDeezerEnrichment({ kind, title, subtitle }),
    hasContent: (e) => e.has_content,
    enabled: enabled && title.trim() !== '',
  });

  return { enrichment: value, isLoading, isError };
}
