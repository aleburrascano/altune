import { createEnrichmentHook } from './createEnrichmentHook';

export const useEnrichment = createEnrichmentHook({
  keyPrefix: 'enrichment',
  provider: 'musicbrainz',
  fetch: (port, { kind, title, subtitle, mbid, signal }) =>
    port.getEnrichment({ kind, title, subtitle, mbid, signal }),
  mbidAware: true,
});
