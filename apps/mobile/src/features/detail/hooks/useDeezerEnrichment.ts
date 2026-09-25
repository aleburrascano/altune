import { getDeezerEnrichment } from '@shared/api-client/enrichment';

import { createEnrichmentHook } from './createEnrichmentHook';

export const useDeezerEnrichment = createEnrichmentHook({
  keyPrefix: 'deezer-enrichment',
  provider: 'deezer',
  fetch: ({ kind, title, subtitle, signal }) =>
    getDeezerEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
});
