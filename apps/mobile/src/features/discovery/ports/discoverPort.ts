import type * as discovery from '@shared/api-client/discovery';

export interface DiscoverPort {
  searchDiscovery: typeof discovery.searchDiscovery;
  suggestDiscovery: typeof discovery.suggestDiscovery;
  listSearchHistory: typeof discovery.listSearchHistory;
}
