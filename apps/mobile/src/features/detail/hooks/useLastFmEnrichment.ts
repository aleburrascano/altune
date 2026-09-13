import { getLastFmEnrichment } from '@shared/api-client/enrichment';

import { createEnrichmentHook } from './createEnrichmentHook';

export const useLastFmEnrichment = createEnrichmentHook({
  keyPrefix: 'lastfm-enrichment',
  fetch: ({ kind, title, subtitle }) => getLastFmEnrichment({ kind, title, subtitle }),
  mbidAware: false,
});
