import { createEnrichmentHook } from './createEnrichmentHook';

export const useLastFmEnrichment = createEnrichmentHook({
  keyPrefix: 'lastfm-enrichment',
  provider: 'lastfm',
  fetch: (port, { kind, title, subtitle, signal }) =>
    port.getLastFmEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
});
