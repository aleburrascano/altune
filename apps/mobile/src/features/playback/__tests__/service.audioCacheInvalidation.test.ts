// The invalidator registry is typed in `TrackId`, but a cast can still smuggle a raw string
// through it, so the service re-parses before the prefetch cache sees an id. These pin both
// halves of that seam.

import * as FileSystem from 'expo-file-system';

import {
  _resetAudioCacheInvalidatorsForTest,
  invalidateAudioCaches,
} from '@shared/acquisition/audioCacheInvalidation';
import { asTrackId, type TrackId } from '@shared/api-client/ids';

import { playbackService } from '../service';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

const { __fs } = FileSystem as unknown as {
  __fs: { seedFile(uri: string, contents: string): void; allFiles(): Record<string, string> };
};

const CACHE_DIR_URI = 'file:///cache/audio-prefetch';

function cachedNames(): string[] {
  return Object.keys(__fs.allFiles())
    .map((uri) => uri.slice(CACHE_DIR_URI.length + 1))
    .sort();
}

beforeEach(async () => {
  _resetAudioCacheInvalidatorsForTest();
  await playbackService();
});

describe('playbackService — the registered audio cache invalidator', () => {
  it('evicts every cached file of a track whose id has the branded shape', () => {
    __fs.seedFile(`${CACHE_DIR_URI}/t1.v1.mp3`, 'a');
    __fs.seedFile(`${CACHE_DIR_URI}/t1.v2.mp3`, 'b');
    __fs.seedFile(`${CACHE_DIR_URI}/t2.v1.mp3`, 'c');

    invalidateAudioCaches(asTrackId('t1'));

    expect(cachedNames()).toEqual(['t2.v1.mp3']);
  });

  it('leaves the cache untouched for an id the brand rejects', () => {
    __fs.seedFile(`${CACHE_DIR_URI}/t1.v1.mp3`, 'a');

    invalidateAudioCaches('../../t1' as TrackId);

    expect(cachedNames()).toEqual(['t1.v1.mp3']);
  });
});
