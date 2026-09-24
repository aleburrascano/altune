import { getLastFmEnrichment } from '@shared/api-client/enrichment';

import { createEnrichmentHook } from './createEnrichmentHook';

export const useLastFmEnrichment = createEnrichmentHook({
  keyPrefix: 'lastfm-enrichment',
  provider: 'lastfm',
  fetch: ({ kind, title, subtitle, signal }) =>
    getLastFmEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
});
