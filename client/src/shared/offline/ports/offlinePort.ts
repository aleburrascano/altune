import type * as audio from '@shared/apiClient/audio';

export interface OfflinePort {
  fetchAudioUrls: typeof audio.fetchAudioUrls;
}
