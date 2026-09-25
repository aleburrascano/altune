// Regression for issue #822: a failed prefetch or presign falls back to live streaming, but it
// must leave a diagnostic trace (which track, which stage) instead of being swallowed silently.
// Issue #1741 closed the two paths inside nativeTrackSwap that still swallowed theirs: a native
// remove that fails mid-swap, and a presign that fails while repairing the active track.
// Issue #1720 made the trace carry a redacted, classified failure instead of the raw rejection.

import * as FileSystem from 'expo-file-system';
import TrackPlayer from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { ApiError } from '@shared/errors';
import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import * as audioCache from '../audioCache';
import { MAX_PREFETCH_FILE_BYTES } from '../audioCache';
import { prefetchNext } from '../audioPrefetch';
import { loadNativeQueue } from '../loadNativeTrack';
import { forgetAllSwaps, repairActiveToStreaming } from '../nativeTrackSwap';

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
const player = TrackPlayer as unknown as { getQueue: jest.Mock; add: jest.Mock; load: jest.Mock };
const { __player } = jest.requireMock('react-native-track-player') as {
  __player: { failNext: (method: string, error: Error) => void };
};
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
      error: { kind: 'unknown', message: 'network down' },
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
      error: { kind: 'unknown', message: 'ENOSPC' },
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
      error: { kind: 'unknown', message: 'prefetch download aborted: oversized' },
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
      error: { kind: 'unknown', message: 'cache dir not readable' },
    });
  });

  it('logs the swap stage when removing the upcoming native slot fails', async () => {
    const boom = new Error('native remove failed');
    const [active, next] = [track('t0'), track('t1')];
    useQueueStore.getState().loadQueue([active, next], 0, null);
    player.getQueue.mockResolvedValue([{ id: trackKey(active) }, { id: trackKey(next) }]);
    __player.failNext('remove', boom);

    await prefetchNext(0);

    expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
      stage: 'swap',
      trackId: 't1',
      error: { kind: 'unknown', message: 'native remove failed' },
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

// Issue #1720: a failed native download names the URL it could not fetch, so logging the
// rejection whole put a live presigned URL — signature and token query params intact — into
// Metro/adb output, crash reports and device bug reports.
describe('failure traces — a presigned URL never reaches the log', () => {
  const SIGNED_URL =
    'https://audio.altune.example/tracks/t1.m4a?X-Amz-Signature=deadbeefcafe&token=s3cr3t-token';

  function everythingLogged(): string {
    return JSON.stringify(warn.mock.calls);
  }

  it('redacts the signed URL a failed download names', async () => {
    jest
      .spyOn(File, 'downloadFileAsync')
      .mockRejectedValueOnce(new Error(`Unable to open ${SIGNED_URL} (403)`));
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await prefetchNext(0);

    expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
      stage: 'download',
      trackId: 't1',
      error: { kind: 'unknown', message: 'Unable to open [redacted url] (403)' },
    });
    expect(everythingLogged()).not.toContain('deadbeefcafe');
  });

  it('redacts a signed URL a presign rejection carries, keeping its classification', async () => {
    fetchUrls.mockRejectedValue(new ApiError(403, `GET ${SIGNED_URL} denied`));
    const tracks = [track('t0'), track('t1')];
    useQueueStore.getState().loadQueue(tracks, 0, null);

    await loadNativeQueue(tracks, 0, { autoplay: false });

    expect(warn).toHaveBeenCalledWith('[playback] presign failed', {
      trackIds: ['t0', 't1'],
      error: { kind: 'auth', message: 'GET [redacted url] denied' },
    });
    expect(everythingLogged()).not.toContain('s3cr3t-token');
  });

  it('redacts a rejection thrown as a bare string', async () => {
    jest.spyOn(File, 'downloadFileAsync').mockRejectedValueOnce(`download of ${SIGNED_URL} failed`);
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await prefetchNext(0);

    expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
      stage: 'download',
      trackId: 't1',
      error: { kind: 'unknown', message: 'download of [redacted url] failed' },
    });
    expect(everythingLogged()).not.toContain('deadbeefcafe');
  });

  it('logs only the type of a rejection that is not an Error, never its fields', async () => {
    jest.spyOn(File, 'downloadFileAsync').mockRejectedValueOnce({ requestUrl: SIGNED_URL });
    useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

    await prefetchNext(0);

    expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
      stage: 'download',
      trackId: 't1',
      error: { kind: 'unknown', message: 'non-Error rejection: object' },
    });
    expect(everythingLogged()).not.toContain('deadbeefcafe');
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
      error: { kind: 'unknown', message: 'presign 503' },
    });
    expect(player.add).toHaveBeenCalled();
  });
});

describe('repairActiveToStreaming — presign failure trace', () => {
  it('logs the track it could not presign and still repairs to an authenticated stream', async () => {
    const boom = new Error('presign 503');
    fetchUrls.mockRejectedValue(boom);

    await repairActiveToStreaming(track('t1'));

    expect(warn).toHaveBeenCalledWith('[playback] presign failed', {
      trackIds: ['t1'],
      error: { kind: 'unknown', message: 'presign 503' },
    });
    expect(player.load.mock.calls[0][0]).toMatchObject({
      headers: { Authorization: 'Bearer tok' },
    });
  });
});
