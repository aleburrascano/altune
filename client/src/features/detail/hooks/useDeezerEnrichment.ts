import type { DetailPort } from '@/features/detail/ports/detailPort';
import {
  useProviderEnrichment,
  type EnrichmentHookConfig,
  type EnrichmentParams,
} from '@/features/detail/hooks/useProviderEnrichment';

type DeezerEnrichment = Awaited<ReturnType<DetailPort['getDeezerEnrichment']>>;

const CONFIG: EnrichmentHookConfig<DeezerEnrichment> = {
  keyPrefix: 'deezer-enrichment',
  provider: 'deezer',
  fetch: (port, { kind, title, subtitle, signal }) =>
    port.getDeezerEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
};

export function useDeezerEnrichment(params: EnrichmentParams) {
  return useProviderEnrichment(CONFIG, params);
}
