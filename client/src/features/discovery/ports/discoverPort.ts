import type * as apiClient from '@shared/apiClient';

export interface DiscoverPort {
  searchDiscovery: typeof apiClient.searchDiscovery;
  suggestDiscovery: typeof apiClient.suggestDiscovery;
  listSearchHistory: typeof apiClient.listSearchHistory;
  recordSearchHistory: typeof apiClient.recordSearchHistory;
  searchUnreleasedTracks: typeof apiClient.searchUnreleasedTracks;
}
