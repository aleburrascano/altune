import type * as apiClient from '@shared/apiClient';

export interface OfflinePort {
  fetchAudioUrls: typeof apiClient.fetchAudioUrls;
}
