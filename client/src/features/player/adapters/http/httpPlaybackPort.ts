import {
  audioRequestHeaders,
  audioStreamUrl,
  fetchAudioUrls,
  recoverAudio,
  getLyrics,
  getQueueState,
  saveQueueState,
  getAllTracks,
} from '@shared/apiClient';
import type { PlaybackPort } from '@features/player/ports/playbackPort';

export const httpPlaybackPort: PlaybackPort = {
  audioStreamUrl: (...args) => audioStreamUrl(...args),
  audioRequestHeaders: (...args) => audioRequestHeaders(...args),
  recoverAudio: (...args) => recoverAudio(...args),
  fetchAudioUrls: (...args) => fetchAudioUrls(...args),
  getQueueState: (...args) => getQueueState(...args),
  saveQueueState: (...args) => saveQueueState(...args),
  getLyrics: (...args) => getLyrics(...args),
  getAllTracks: (...args) => getAllTracks(...args),
};
