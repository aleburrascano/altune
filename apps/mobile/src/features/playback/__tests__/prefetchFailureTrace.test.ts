// Regression for issue #822: a failed prefetch or presign falls back to live streaming, but it
// must leave a diagnostic trace (which track, which stage) instead of being swallowed silently.

import * as FileSystem from 'expo-file-system';
import TrackPlayer from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import * as audioCache from '../audioCache';
import { MAX_PREFETCH_FILE_BYTES } from '../audioCache';
import { forgetAllSwaps, prefetchNext } from '../audioPrefetch';
import { loadNativeQueue } from '../loadNativeTrack';

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

type DownloadWithProgress = (
  url: string,
  dest: { uri: string },
  options?: { onProgress?: (data: { bytesWritten: number; totalBytes: number }) => void },
) => Promise<{ uri: string }>;

const { File } = FileSystem as unknown as { File: { downloadFileAsync: DownloadWithProgress } };
const player = TrackPlayer as unknown as { getQueue: jest.Mock; add: jest.Mock };
const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

function track(trackId: string): PlaybackTrack {
  return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
}

function resolved(trackId: string): ResolvedAudioUrl {
  return { trackId, url: `https://cdn.example/${trackId}.mp3`, version: 'v1' };
}

let warn: jest.SpyInstance;

beforeEach(() => {
  forgetAllSwaps();
  useQueueStore.getState().clearQueue();
  fetchUrls.mockReset();
  fetchUrls.mockImplementation(async (ids) => ids.map(resolved));
  player.getQueue.mockResolvedValue([]);
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  jest.restoreAllMocks();
});

describe('prefetchNext — failure trace', () => {
  it('logs the track and stage when fetchAudioUrls throws', async () => {
    const boom = new Error('network down');
    fetchUrls.mockRejectedValue(boom);
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await expect(prefetchNext(0)).resolves.toBeUndefined();

    expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
      stage: 'resolve',
      trackId: 't1',
      error: boom,
    });
  });

  it('logs a download that fails on its own (e.g. disk full)', async () => {
    const diskFull = new Error('ENOSPC');
    jest.spyOn(File, 'downloadFileAsync').mockRejectedValueOnce(diskFull);
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await prefetchNext(0);

    expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
      stage: 'download',
      trackId: 't1',
      error: diskFull,
    });
  });

  it('logs a download abandoned for growing past the byte cap, with the cause', async () => {
    jest.spyOn(File, 'downloadFileAsync').mockImplementationOnce(((_url, _dest, options) => {
      options?.onProgress?.({ bytesWritten: MAX_PREFETCH_FILE_BYTES + 1, totalBytes: -1 });
      return new Promise<{ uri: string }>(() => {});
    }) as DownloadWithProgress);
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await prefetchNext(0);

    expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
      stage: 'download',
      trackId: 't1',
      error: new Error('prefetch download aborted: oversized'),
    });
  });

  it('logs the swap stage when installing the downloaded file throws', async () => {
    const boom = new Error('cache dir not readable');
    jest.spyOn(audioCache, 'evict').mockImplementation(() => {
      throw boom;
    });
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await prefetchNext(0);

    expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
      stage: 'swap',
      trackId: 't1',
      error: boom,
    });
  });

  it('stays quiet when a download is superseded by a skip', async () => {
    useQueueStore.getState().loadQueue(['t0', 't1', 't2', 't3'].map(track), 0, null);
    jest
      .spyOn(File, 'downloadFileAsync')
      .mockImplementationOnce(() => new Promise<{ uri: string }>(() => {}));

    const first = prefetchNext(0);
    for (let i = 0; i < 20; i++) await Promise.resolve();
    useQueueStore.getState().skipToIndex(1);
    await prefetchNext(1);
    await first;

    expect(warn).not.toHaveBeenCalledWith('[playback] prefetch failed', expect.anything());
  });

  it('stays quiet on a successful prefetch', async () => {
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await prefetchNext(0);

    expect(warn).not.toHaveBeenCalled();
  });
});

describe('loadNativeQueue — presign failure trace', () => {
  it('logs the track ids it could not presign and still loads the queue', async () => {
    const boom = new Error('presign 503');
    fetchUrls.mockRejectedValue(boom);
    const tracks = [track('t0'), track('t1')];
    useQueueStore.getState().loadQueue(tracks, 0, null);

    await loadNativeQueue(tracks, 0, { autoplay: false });

    expect(warn).toHaveBeenCalledWith('[playback] presign failed', {
      trackIds: ['t0', 't1'],
      error: boom,
    });
    expect(player.add).toHaveBeenCalled();
  });
});
