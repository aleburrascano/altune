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

function hasContent(e: DeezerEnrichmentResponse): boolean {
  return e.bpm > 0 || e.explicit || e.label !== '' || e.genres.length > 0 || e.record_type !== '';
}

export function useDeezerEnrichment({
  kind,
  title,
  subtitle,
  enabled = true,
}: UseDeezerEnrichmentParams): UseDeezerEnrichmentReturn {
  const { value, isLoading, isError } = useEnrichmentQuery({
    queryKey: ['deezer-enrichment', kind, `${title}|${subtitle ?? ''}`],
    queryFn: () => getDeezerEnrichment({ kind, title, subtitle }),
    hasContent,
    enabled: enabled && title.trim() !== '',
  });

  return { enrichment: value, isLoading, isError };
}
