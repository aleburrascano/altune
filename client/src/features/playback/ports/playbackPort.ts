import type * as audio from '@shared/apiClient/audio';
import type * as lyrics from '@shared/apiClient/lyrics';
import type * as playback from '@shared/apiClient/playback';
import type * as tracks from '@shared/apiClient/tracks';

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
