import {
  listSearchHistory,
  recordSearchHistory,
  searchDiscovery,
  suggestDiscovery,
  searchUnreleasedTracks,
} from '@shared/apiClient';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

export const httpDiscoverPort: DiscoverPort = {
  searchDiscovery: (...args) => searchDiscovery(...args),
  suggestDiscovery: (...args) => suggestDiscovery(...args),
  listSearchHistory: (...args) => listSearchHistory(...args),
  recordSearchHistory: (...args) => recordSearchHistory(...args),
  searchUnreleasedTracks: (...args) => searchUnreleasedTracks(...args),
};
