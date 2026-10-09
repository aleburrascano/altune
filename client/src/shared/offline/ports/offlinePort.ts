import type * as audio from '@shared/api-client/audio';

export interface OfflinePort {
  fetchAudioUrls: typeof audio.fetchAudioUrls;
}
