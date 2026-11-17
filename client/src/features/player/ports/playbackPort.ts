import type {
  audioRequestHeaders,
  audioStreamUrl,
  fetchAudioUrls,
  getAllTracks,
  getLyrics,
  getQueueState,
  isAudioPrefetchEnabled,
  recoverAudio,
  saveQueueState,
} from '@shared/apiClient';

export interface PlaybackPort {
  audioStreamUrl: typeof audioStreamUrl;
  audioRequestHeaders: typeof audioRequestHeaders;
  recoverAudio: typeof recoverAudio;
  isAudioPrefetchEnabled: typeof isAudioPrefetchEnabled;
  fetchAudioUrls: typeof fetchAudioUrls;
  getQueueState: typeof getQueueState;
  saveQueueState: typeof saveQueueState;
  getLyrics: typeof getLyrics;
  getAllTracks: typeof getAllTracks;
}
