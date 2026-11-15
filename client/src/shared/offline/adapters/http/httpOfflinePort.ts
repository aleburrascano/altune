import { fetchAudioUrls } from '@shared/apiClient';

import type { OfflinePort } from '@shared/offline/ports/offlinePort';

export const httpOfflinePort: OfflinePort = {
  fetchAudioUrls: (...args) => fetchAudioUrls(...args),
};
