// Regression for issue #826: a trackId (or audio version) carrying path syntax must never let a
// prefetch download land outside the audio cache directory.

import * as FileSystem from 'expo-file-system';
import { posix } from 'path';

import { parseTrackId, type TrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { cacheDir } from '../audioCache';
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
  // A cast, not asTrackId: the hostile ids below must reach prefetch past the brand (#944).
  return libraryTrack({ source: { kind: 'library', trackId: trackId as TrackId } });
}

function normalizedPath(uri: string): string {
  return posix.normalize(uri.replace(/^file:\/\//, ''));
}

async function prefetchSecondTrack(trackId: string, version = 'v1'): Promise<string[]> {
  useQueueStore.getState().loadQueue([track('active'), track(trackId)], 0, null);
  __http.reply('POST /v1/audio-urls', {
    json: { urls: [{ track_id: trackId, url: 'https://cdn.example/a.mp3', version }] },
  });
  await prefetchNext(0);
  return Object.keys(__fs.allFiles());
}

beforeEach(() => {
  useQueueStore.getState().clearQueue();
});

describe('prefetchNext — cache path containment', () => {
  it('downloads a well-formed track id into the cache directory', async () => {
    const files = await prefetchSecondTrack('0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a');

    expect(files).toEqual([`${cacheDir().uri}/0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a.v1.mp3`]);
  });

  it.each(['../../../document/evil', '..', 'a/../../b'])(
    'keeps every written file inside cacheDir() for trackId %p',
    async (trackId) => {
      const files = await prefetchSecondTrack(trackId);

      const root = `${normalizedPath(cacheDir().uri)}/`;
      for (const uri of files) expect(normalizedPath(uri).startsWith(root)).toBe(true);
      expect(files).toEqual([]);
    },
  );

  it('rejects a server-supplied version carrying path syntax', async () => {
    const files = await prefetchSecondTrack('trk-2', '../../../document/evil');

    expect(files).toEqual([]);
  });
});

describe('parseTrackId', () => {
  it('accepts a UUID and the opaque token ids the client already uses', () => {
    expect(parseTrackId('0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a')).toEqual({
      ok: true,
      id: '0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a',
    });
    expect(parseTrackId('trk_42').ok).toBe(true);
  });

  it.each(['', '../x', 'a/b', 'a?b', 'a#b', 'a%2Fb', 'a.b', 'x'.repeat(129)])(
    'rejects %p with a typed failure',
    (value) => {
      expect(parseTrackId(value)).toEqual({
        ok: false,
        error: { kind: 'invalid-track-id', value },
      });
    },
  );
});
