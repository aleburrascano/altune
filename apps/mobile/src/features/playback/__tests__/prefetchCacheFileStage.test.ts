import { fetchAudioUrls } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';

import * as audioCache from '../audioCache';
import { prefetchNext } from '../audioPrefetch';
import { forgetAllSwaps } from '../nativeTrackSwap';

import { libraryTrack } from './fixtures';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

jest.mock('@shared/api-client/audio', () => ({
  ...jest.requireActual('@shared/api-client/audio'),
  fetchAudioUrls: jest.fn(),
}));

const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

function track(trackId: string) {
  return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
}

describe('prefetchNext cache file construction failure', () => {
  it('is traced as the download stage', async () => {
    forgetAllSwaps();
    useQueueStore.getState().clearQueue();
    fetchUrls.mockImplementation(async (ids) =>
      ids.map((id) => ({ trackId: id, url: `https://cdn.example/${id}.mp3`, version: 'v1' })),
    );
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    jest.spyOn(audioCache, 'findCached').mockReturnValue(null);
    jest.spyOn(audioCache, 'cacheDir').mockImplementation(() => {
      throw new Error('no cache dir');
    });
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await prefetchNext(0);

    expect(warn).toHaveBeenCalledWith(
      '[playback] prefetch failed',
      expect.objectContaining({ stage: 'download', trackId: 't1' }),
    );
    jest.restoreAllMocks();
  });
});
