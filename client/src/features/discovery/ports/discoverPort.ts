import type * as discovery from '@shared/apiClient/discovery';
import type * as enrichment from '@shared/apiClient/enrichment';

export interface DiscoverPort {
  searchDiscovery: typeof discovery.searchDiscovery;
  suggestDiscovery: typeof discovery.suggestDiscovery;
  listSearchHistory: typeof discovery.listSearchHistory;
  recordSearchHistory: typeof discovery.recordSearchHistory;
  searchUnreleasedTracks: typeof enrichment.searchUnreleasedTracks;
}
