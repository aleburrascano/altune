import { listSearchHistory, searchDiscovery, suggestDiscovery } from '@shared/api-client/discovery';

import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

export const httpDiscoverPort: DiscoverPort = {
  searchDiscovery: (...args) => searchDiscovery(...args),
  suggestDiscovery: (...args) => suggestDiscovery(...args),
  listSearchHistory: (...args) => listSearchHistory(...args),
};
