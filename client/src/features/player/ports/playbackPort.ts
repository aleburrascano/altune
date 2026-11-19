import type {
  audioRequestHeaders,
  audioStreamUrl,
  fetchAudioUrls,
  getAllTracks,
  getLyrics,
  getQueueState,
  recoverAudio,
  saveQueueState,
} from '@shared/apiClient';

export interface PlaybackPort {
  audioStreamUrl: typeof audioStreamUrl;
  audioRequestHeaders: typeof audioRequestHeaders;
  recoverAudio: typeof recoverAudio;
  fetchAudioUrls: typeof fetchAudioUrls;
  getQueueState: typeof getQueueState;
  saveQueueState: typeof saveQueueState;
  getLyrics: typeof getLyrics;
  getAllTracks: typeof getAllTracks;
}
