import { getEnrichment } from '@shared/api-client/enrichment';

import { createEnrichmentHook } from './createEnrichmentHook';

export const useEnrichment = createEnrichmentHook({
  keyPrefix: 'enrichment',
  provider: 'musicbrainz',
  fetch: ({ kind, title, subtitle, mbid, signal }) =>
    getEnrichment({ kind, title, subtitle, mbid, signal }),
  mbidAware: true,
});
