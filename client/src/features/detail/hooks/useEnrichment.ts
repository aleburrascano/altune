import type { DetailPort } from '@/features/detail/ports/detailPort';
import {
  useProviderEnrichment,
  type EnrichmentHookConfig,
  type EnrichmentParams,
} from '@/features/detail/hooks/useProviderEnrichment';

type MusicBrainzEnrichment = Awaited<ReturnType<DetailPort['getEnrichment']>>;

const CONFIG: EnrichmentHookConfig<MusicBrainzEnrichment> = {
  keyPrefix: 'enrichment',
  provider: 'musicbrainz',
  fetch: (port, { kind, title, subtitle, mbid, signal }) =>
    port.getEnrichment({ kind, title, subtitle, mbid, signal }),
  mbidAware: true,
};

export function useEnrichment(params: EnrichmentParams) {
  return useProviderEnrichment(CONFIG, params);
}
