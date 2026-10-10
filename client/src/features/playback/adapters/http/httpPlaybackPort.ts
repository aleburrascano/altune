import {
  audioRequestHeaders,
  audioStreamUrl,
  fetchAudioUrls,
  isAudioPrefetchEnabled,
  recoverAudio,
} from '@shared/apiClient/audio';
import { getLyrics } from '@shared/apiClient/lyrics';
import { getQueueState, saveQueueState } from '@shared/apiClient/playback';
import { getAllTracks } from '@shared/apiClient/tracks';

import type { PlaybackPort } from '@features/playback/ports/playbackPort';

export const httpPlaybackPort: PlaybackPort = {
  audioStreamUrl: (...args) => audioStreamUrl(...args),
  audioRequestHeaders: (...args) => audioRequestHeaders(...args),
  recoverAudio: (...args) => recoverAudio(...args),
  isAudioPrefetchEnabled: (...args) => isAudioPrefetchEnabled(...args),
  fetchAudioUrls: (...args) => fetchAudioUrls(...args),
  getQueueState: (...args) => getQueueState(...args),
  saveQueueState: (...args) => saveQueueState(...args),
  getLyrics: (...args) => getLyrics(...args),
  getAllTracks: (...args) => getAllTracks(...args),
};
