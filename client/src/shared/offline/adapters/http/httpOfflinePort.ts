import { fetchAudioUrls } from '@shared/apiClient/audio';

import type { OfflinePort } from '@shared/offline/ports/offlinePort';

export const httpOfflinePort: OfflinePort = {
  fetchAudioUrls: (...args) => fetchAudioUrls(...args),
};
