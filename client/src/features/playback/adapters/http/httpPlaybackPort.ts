import {
  audioRequestHeaders,
  audioStreamUrl,
  fetchAudioUrls,
  isAudioPrefetchEnabled,
  recoverAudio,
} from '@shared/api-client/audio';
import { getLyrics } from '@shared/api-client/lyrics';
import { getQueueState, saveQueueState } from '@shared/api-client/playback';
import { getAllTracks } from '@shared/api-client/tracks';

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
