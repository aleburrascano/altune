import type * as discovery from '@shared/apiClient/discovery';

export interface DiscoverPort {
  searchDiscovery: typeof discovery.searchDiscovery;
  suggestDiscovery: typeof discovery.suggestDiscovery;
  listSearchHistory: typeof discovery.listSearchHistory;
}
