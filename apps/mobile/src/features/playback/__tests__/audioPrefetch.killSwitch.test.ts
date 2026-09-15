// Regression for issue #824: the server can pull audio prefetching remotely (AUDIO_PREFETCH_ENABLED
// on the API, surfaced as `prefetch_enabled` on every /v1/audio-urls response). With the switch
// off, `prefetchNext` must be a no-op so playback falls back to straight streaming.

import * as FileSystem from 'expo-file-system';

import { asTrackId } from '@shared/api-client/ids';
import { fetchAudioUrls } from '@shared/api-client/audio';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { prefetchNext } from '../audioPrefetch';

import { libraryTrack } from './fixtures';

const { __http } = require('../../../../jest/doubles/fetch.js');
const { __fs } = FileSystem as unknown as { __fs: { allFiles(): Record<string, string> } };

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

function track(trackId: string): PlaybackTrack {
  return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
}

function replyAudioUrls(prefetchEnabled?: boolean): void {
  __http.reply('POST /v1/audio-urls', {
    json: {
      urls: [{ track_id: 'trk-2', url: 'https://cdn.example/a.mp3', version: 'v1' }],
      ...(prefetchEnabled === undefined ? {} : { prefetch_enabled: prefetchEnabled }),
    },
  });
}

// Plays the resolve a track load does, so the client learns the server's current switch state.
async function serverSays(prefetchEnabled: boolean | undefined): Promise<void> {
  replyAudioUrls(prefetchEnabled);
  await fetchAudioUrls(['trk-1']);
  __http.reset();
}

beforeEach(async () => {
  await serverSays(true);
  useQueueStore.getState().clearQueue();
  useQueueStore.getState().loadQueue([track('trk-1'), track('trk-2')], 0, null);
});

describe('prefetchNext — remote kill switch', () => {
  it('prefetches the next track while the switch is on (the default)', async () => {
    replyAudioUrls(true);

    await prefetchNext(0);

    expect(Object.keys(__fs.allFiles())).toHaveLength(1);
  });

  it('treats a server that does not send the flag as enabled', async () => {
    await serverSays(undefined);
    replyAudioUrls();

    await prefetchNext(0);

    expect(Object.keys(__fs.allFiles())).toHaveLength(1);
  });

  it('is a no-op once the server has turned prefetching off: no request, no download', async () => {
    await serverSays(false);
    replyAudioUrls(false);

    await prefetchNext(0);

    expect(__http.countFor('POST /v1/audio-urls')).toBe(0);
    expect(Object.keys(__fs.allFiles())).toEqual([]);
  });

  it('downloads nothing when its own resolve reports the switch turned off', async () => {
    replyAudioUrls(false);

    await prefetchNext(0);

    expect(Object.keys(__fs.allFiles())).toEqual([]);
  });

  it('resumes prefetching when the server turns the switch back on', async () => {
    await serverSays(false);
    await prefetchNext(0);
    await serverSays(true);
    replyAudioUrls(true);

    await prefetchNext(0);

    expect(Object.keys(__fs.allFiles())).toHaveLength(1);
  });
});
