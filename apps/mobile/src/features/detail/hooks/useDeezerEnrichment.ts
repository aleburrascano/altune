import { createEnrichmentHook } from './createEnrichmentHook';

export const useDeezerEnrichment = createEnrichmentHook({
  keyPrefix: 'deezer-enrichment',
  provider: 'deezer',
  fetch: (port, { kind, title, subtitle, signal }) =>
    port.getDeezerEnrichment({ kind, title, subtitle, signal }),
  mbidAware: false,
});
