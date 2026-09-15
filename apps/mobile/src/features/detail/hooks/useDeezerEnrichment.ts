import { getDeezerEnrichment } from '@shared/api-client/enrichment';

import { createEnrichmentHook } from './createEnrichmentHook';

export const useDeezerEnrichment = createEnrichmentHook({
  keyPrefix: 'deezer-enrichment',
  fetch: ({ kind, title, subtitle }) => getDeezerEnrichment({ kind, title, subtitle }),
  mbidAware: false,
});
