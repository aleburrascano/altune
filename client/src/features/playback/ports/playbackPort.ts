import type * as audio from '@shared/api-client/audio';
import type * as lyrics from '@shared/api-client/lyrics';
import type * as playback from '@shared/api-client/playback';
import type * as tracks from '@shared/api-client/tracks';

export interface PlaybackPort {
  audioStreamUrl: typeof audio.audioStreamUrl;
  audioRequestHeaders: typeof audio.audioRequestHeaders;
  recoverAudio: typeof audio.recoverAudio;
  isAudioPrefetchEnabled: typeof audio.isAudioPrefetchEnabled;
  fetchAudioUrls: typeof audio.fetchAudioUrls;
  getQueueState: typeof playback.getQueueState;
  saveQueueState: typeof playback.saveQueueState;
  getLyrics: typeof lyrics.getLyrics;
  getAllTracks: typeof tracks.getAllTracks;
}
