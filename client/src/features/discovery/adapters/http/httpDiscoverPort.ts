import { listSearchHistory, searchDiscovery, suggestDiscovery } from '@shared/apiClient/discovery';

import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

export const httpDiscoverPort: DiscoverPort = {
  searchDiscovery: (...args) => searchDiscovery(...args),
  suggestDiscovery: (...args) => suggestDiscovery(...args),
  listSearchHistory: (...args) => listSearchHistory(...args),
};
