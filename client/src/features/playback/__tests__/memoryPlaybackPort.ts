import { ApiError } from '@shared/failure/errors';
import type { ResolvedAudioUrl } from '@shared/apiClient/audio';
import type { LyricsResponse } from '@shared/apiClient/lyrics';
import type { QueueStateResponse } from '@shared/apiClient/playback';
import { MAX_ALL_TRACKS } from '@shared/apiClient/tracks';
import type { TrackResponse } from '@shared/apiClient/trackResponse';

import type { PlaybackPort } from '@features/playback/ports/playbackPort';

const NOT_FOUND_STATUS = 404;
const MEMORY_API_BASE = 'https://memory.test';

export interface MemoryPlaybackSeed {
  audioUrls?: Record<string, { url: string; version: string }>;
  tracks?: TrackResponse[];
  lyrics?: Record<string, LyricsResponse>;
  queueState?: QueueStateResponse;
  prefetchEnabled?: boolean;
}

function notFound(what: string): ApiError {
  return new ApiError(NOT_FOUND_STATUS, `${what} not found`);
}

export function createMemoryPlaybackPort(seed: MemoryPlaybackSeed = {}): PlaybackPort {
  const audioUrls = seed.audioUrls ?? {};
  const tracks = [...(seed.tracks ?? [])];
  const lyrics = seed.lyrics ?? {};
  const serverPrefetch = seed.prefetchEnabled ?? true;
  let queueState = seed.queueState;
  let prefetchEnabled = true;

  return {
    audioStreamUrl: (trackId) => `${MEMORY_API_BASE}/v1/tracks/${trackId}/audio`,
    audioRequestHeaders: async () => ({}),
    recoverAudio: async (trackId) => {
      await Promise.resolve();
      if (audioUrls[trackId] === undefined) throw notFound(`track ${trackId}`);
    },
    isAudioPrefetchEnabled: () => prefetchEnabled,
    fetchAudioUrls: async (trackIds) => {
      if (trackIds.length === 0) return [];
      await Promise.resolve();
      prefetchEnabled = serverPrefetch;
      return trackIds.flatMap((trackId): ResolvedAudioUrl[] => {
        const found = audioUrls[trackId];
        return found === undefined ? [] : [{ trackId, ...found }];
      });
    },
    getQueueState: async () => {
      await Promise.resolve();
      if (queueState === undefined) throw notFound('queue state');
      return queueState;
    },
    saveQueueState: async (body) => {
      await Promise.resolve();
      queueState = { ...body };
    },
    getLyrics: async ({ title }) => {
      await Promise.resolve();
      const found = lyrics[title];
      if (found === undefined) throw notFound(`lyrics for ${title}`);
      return found;
    },
    getAllTracks: async () => {
      await Promise.resolve();
      return {
        items: tracks.slice(0, MAX_ALL_TRACKS),
        truncated: tracks.length > MAX_ALL_TRACKS,
      };
    },
  };
}
