import { getLastFmEnrichment, type LastFmEnrichmentResponse } from '@shared/api-client/enrichment';
import type { DiscoveryKind } from '@shared/api-client/discovery';

import { useEnrichmentQuery } from './useEnrichmentQuery';

type UseLastFmEnrichmentParams = {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
  enabled?: boolean;
};

type UseLastFmEnrichmentReturn = {
  enrichment: LastFmEnrichmentResponse | null;
  isLoading: boolean;
  isError: boolean;
};

function hasContent(e: LastFmEnrichmentResponse): boolean {
  return (
    e.listeners > 0 || e.playcount > 0 || e.tags.length > 0 || e.bio !== '' || e.similar.length > 0
  );
}

export function useLastFmEnrichment({
  kind,
  title,
  subtitle,
  enabled = true,
}: UseLastFmEnrichmentParams): UseLastFmEnrichmentReturn {
  const { value, isLoading, isError } = useEnrichmentQuery({
    queryKey: ['lastfm-enrichment', kind, `${title}|${subtitle ?? ''}`],
    queryFn: () => getLastFmEnrichment({ kind, title, subtitle }),
    hasContent,
    enabled: enabled && title.trim() !== '',
  });

  return { enrichment: value, isLoading, isError };
}
