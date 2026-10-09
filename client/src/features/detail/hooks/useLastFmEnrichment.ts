import type { DetailPort } from '@/features/detail/ports/detailPort';
import {
  useProviderEnrichment,
  type EnrichmentHookConfig,
  type EnrichmentParams,
} from '@/features/detail/hooks/useProviderEnrichment';

type LastFmEnrichment = Awaited<ReturnType<DetailPort['getLastFmEnrichment']>>;

const CONFIG: EnrichmentHookConfig<LastFmEnrichment> = {
  keyPrefix: 'lastfm-enrichment',
  provider: 'lastfm',
  fetch: (port, { kind, title, subtitle, signal }) =>
    port.getLastFmEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
};

export function useLastFmEnrichment(params: EnrichmentParams) {
  return useProviderEnrichment(CONFIG, params);
}
