import TrackPlayer from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import { recordEvent } from '@shared/telemetry/recordEvent';
import type { PlaybackTrack } from '@shared/playback/types';

import {
  forgetAllSwaps,
  repairActiveToStreaming,
  swapUpcomingToLocal,
  wasSwappedToLocal,
} from '../nativeTrackSwap';
import { usePlaybackErrorStore } from '../playbackErrorStore';
import { _resetPlaybackHealthForTest, flushPlaybackHealth } from '../playbackHealth';
import { resetPlaybackForSignOut } from '../service';

import { libraryTrack, previewTrack } from './fixtures';

jest.mock('@shared/telemetry/recordEvent', () => ({
  recordEvent: jest.fn().mockResolvedValue(undefined),
}));
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

// Only the presign-failure trace and the sign-out race stub fetchAudioUrls; every other test
// runs the real one against the fetch double.
const realFetchAudioUrls: typeof fetchAudioUrls = jest.requireActual(
  '@shared/api-client/audio',
).fetchAudioUrls;

beforeEach(() => {
  (fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>).mockImplementation(
    realFetchAudioUrls,
  );
});

describe('a native load failure surfaces a PlaybackError', () => {
  const { __player } = jest.requireMock('react-native-track-player');

  beforeEach(() => {
    usePlaybackErrorStore.getState().clear();
  });

  describe('repairActiveToStreaming — a native load failure surfaces a PlaybackError', () => {
    it('reports the failing track when TrackPlayer.load throws', async () => {
      const track = previewTrack({
        source: { kind: 'preview', previewUrl: 'https://cdn.example/preview.mp3' },
      });
      __player.failNext('load', new Error('native load failed'));

      await repairActiveToStreaming(track);

      expect(usePlaybackErrorStore.getState().key).toBe(trackKey(track));
      expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
    });

    it('reports the failing track when TrackPlayer.play throws after a successful load', async () => {
      const track = previewTrack({
        source: { kind: 'preview', previewUrl: 'https://cdn.example/preview.mp3' },
      });
      __player.failNext('play', new Error('native play failed'));

      await repairActiveToStreaming(track);

      expect(usePlaybackErrorStore.getState().key).toBe(trackKey(track));
      expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
    });

    it('leaves the error store clean when load and play both succeed', async () => {
      const track = previewTrack({
        source: { kind: 'preview', previewUrl: 'https://cdn.example/preview.mp3' },
      });

      await repairActiveToStreaming(track);

      expect(usePlaybackErrorStore.getState().key).toBeNull();
      expect(usePlaybackErrorStore.getState().message).toBeNull();
    });
  });
});

describe('swapping an upcoming slot to a cached file', () => {
  const player = TrackPlayer as unknown as {
    getQueue: jest.Mock;
    getActiveTrackIndex: jest.Mock;
    remove: jest.Mock;
    add: jest.Mock;
  };

  interface NativeEntry {
    id: string;
    url: string;
  }

  // Drives the swap against a small model of the native queue, so a test can assert what the queue
  // holds afterwards rather than which calls were made to it.
  function modelNativeQueue(tracks: readonly PlaybackTrack[]): NativeEntry[] {
    const queue: NativeEntry[] = tracks.map((t) => ({
      id: trackKey(t),
      url: `https://stream.example/${t.title}`,
    }));
    player.getQueue.mockImplementation(async () => [...queue]);
    player.remove.mockImplementation(async (index: number) => {
      queue.splice(index, 1);
    });
    player.add.mockImplementation(async (entry: NativeEntry, index: number) => {
      queue.splice(index, 0, entry);
    });
    return queue;
  }

  let warn: jest.SpyInstance;

  beforeEach(() => {
    forgetAllSwaps();
    usePlaybackErrorStore.getState().clear();
    useQueueStore.getState().clearQueue();
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
    for (const mock of [player.getQueue, player.remove, player.add]) mock.mockReset();
  });

  describe('swapUpcomingToLocal — replacing an upcoming native slot with a cached file', () => {
    it('does nothing when the track is not an upcoming slot', async () => {
      const track = libraryTrack({ title: 'Track trk-1' });
      player.getQueue.mockResolvedValueOnce([{ id: 'library:other' }]);

      await swapUpcomingToLocal(track, 'file:///cache/trk-1.mp3');

      expect(player.remove).not.toHaveBeenCalled();
      expect(player.add).not.toHaveBeenCalled();
      expect(wasSwappedToLocal(asTrackId('trk-1'))).toBe(false);
    });

    it('removes the upcoming slot and refills it with the local file, marking it swapped', async () => {
      const track = libraryTrack({ title: 'Track trk-1' });
      player.getQueue.mockResolvedValueOnce([{ id: 'library:active' }, { id: trackKey(track) }]);

      await swapUpcomingToLocal(track, 'file:///cache/trk-1.mp3');

      expect(player.remove).toHaveBeenCalledWith(1);
      expect(player.add.mock.calls[0][0]).toMatchObject({ url: 'file:///cache/trk-1.mp3' });
      expect(player.add.mock.calls[0][1]).toBe(1);
      expect(wasSwappedToLocal(asTrackId('trk-1'))).toBe(true);
    });

    it('swaps a slot sitting at index 0 when there is no active track yet', async () => {
      const track = libraryTrack({ title: 'Track trk-1' });
      player.getQueue.mockResolvedValueOnce([{ id: trackKey(track) }]);

      await swapUpcomingToLocal(track, 'file:///cache/trk-1.mp3');

      expect(player.remove).toHaveBeenCalledWith(0);
      expect(player.add.mock.calls[0][1]).toBe(0);
    });

    it('falls through to a streaming re-add when the local re-add fails, without surfacing an error', async () => {
      const track = previewTrack({ title: 'A Preview' });
      player.getQueue.mockResolvedValueOnce([{ id: 'library:active' }, { id: trackKey(track) }]);
      player.add.mockRejectedValueOnce(new Error('local add failed'));

      await swapUpcomingToLocal(track, 'file:///cache/p.mp3');

      expect(player.add).toHaveBeenCalledTimes(2);
      expect(usePlaybackErrorStore.getState().key).toBeNull();
    });

    it('surfaces a PlaybackError only when the streaming re-add also fails', async () => {
      const track = previewTrack({ title: 'A Preview' });
      player.getQueue.mockResolvedValueOnce([{ id: 'library:active' }, { id: trackKey(track) }]);
      player.add.mockRejectedValueOnce(new Error('local add failed'));
      player.add.mockRejectedValueOnce(new Error('streaming add failed'));

      await swapUpcomingToLocal(track, 'file:///cache/p.mp3');

      expect(usePlaybackErrorStore.getState().key).toBe(trackKey(track));
      expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
    });
  });

  // Regression (#1723): the swap removes the slot before it refills it, so a streaming re-add that
  // fails after the local one left native one entry shorter than the queue store, and every
  // index-based op after it addressed the wrong track.
  describe('swapUpcomingToLocal — when both re-adds of the emptied slot fail', () => {
    it('holds the original entry again, leaving native aligned with the queue store', async () => {
      const active = libraryTrack({ title: 'Now Playing' });
      const track = previewTrack({ title: 'A Preview' });
      useQueueStore.getState().loadQueue([active, track], 0, null);
      const nativeQueue = modelNativeQueue([active, track]);
      const beforeSwap = [...nativeQueue];
      player.add.mockRejectedValueOnce(new Error('local add failed'));
      player.add.mockRejectedValueOnce(new Error('streaming add failed'));

      await swapUpcomingToLocal(track, 'file:///cache/p.mp3');

      expect(nativeQueue).toEqual(beforeSwap);
      expect(nativeQueue.map((entry) => entry.id)).toEqual(
        orderedQueueTracks(useQueueStore.getState()).map(trackKey),
      );
    });

    it('traces the track left missing when putting the original entry back also fails', async () => {
      const track = previewTrack({ title: 'A Preview' });
      const nativeQueue = modelNativeQueue([libraryTrack({ title: 'Now Playing' }), track]);
      player.add.mockRejectedValueOnce(new Error('local add failed'));
      player.add.mockRejectedValueOnce(new Error('streaming add failed'));
      player.add.mockRejectedValueOnce(new Error('restoring add failed'));

      await swapUpcomingToLocal(track, 'file:///cache/p.mp3');

      expect(nativeQueue.map((entry) => entry.id)).toEqual([trackKey(libraryTrack())]);
      expect(warn).toHaveBeenCalledWith(
        '[playback] swap slot restore failed',
        expect.objectContaining({ trackId: trackKey(track) }),
      );
    });
  });
});

describe('presign failure trace', () => {
  // Regression for issue #822: a failed prefetch or presign falls back to live streaming, but it
  // must leave a diagnostic trace (which track, which stage) instead of being swallowed silently.
  // Issue #1741 closed the two paths inside nativeTrackSwap that still swallowed theirs: a native
  // remove that fails mid-swap, and a presign that fails while repairing the active track.
  // Issue #1720 made the trace carry a redacted, classified failure instead of the raw rejection.

  const player = TrackPlayer as unknown as { getQueue: jest.Mock; add: jest.Mock; load: jest.Mock };
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
});

describe('sign-out racing a streaming repair', () => {
  const { __player } = jest.requireMock('react-native-track-player');

  const mockFetchAudioUrls = jest.fn();

  beforeEach(() => {
    (fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>).mockImplementation(
      (...args: unknown[]) => mockFetchAudioUrls(...args),
    );
  });

  const A_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-of-a') } });

  function heldPresign() {
    let release: (v: { url: string }[]) => void = () => undefined;
    mockFetchAudioUrls.mockReturnValueOnce(new Promise((resolve) => (release = resolve)));
    return (url: string) => release([{ url }]);
  }

  describe('streaming repair in flight at sign-out (#2704)', () => {
    beforeEach(() => mockFetchAudioUrls.mockReset());

    it('does not load or play the previous user track after the reset', async () => {
      const resolvePresign = heldPresign();
      const repair = repairActiveToStreaming(A_TRACK);
      await resetPlaybackForSignOut();
      resolvePresign('https://cdn.example/a.mp3');
      await repair;

      expect(__player.calls('load')).toHaveLength(0);
      expect(__player.calls('play')).toHaveLength(0);
    });

    it('a repair started after sign-out still loads and plays', async () => {
      await resetPlaybackForSignOut();
      heldPresign()('https://cdn.example/a.mp3');
      await repairActiveToStreaming(A_TRACK);

      expect(__player.calls('load')).toHaveLength(1);
      expect(__player.calls('play')).toHaveLength(1);
    });
  });
});

describe('swap-path presign health tally', () => {
  // Regression for #2526: a swap-path presign failure is counted in the playback health tally.
  const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;
  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    _resetPlaybackHealthForTest();
    (recordEvent as jest.Mock).mockClear();
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('counts a swap-path presign failure', async () => {
    fetchUrls.mockRejectedValue(new Error('presign 503'));
    await repairActiveToStreaming(
      libraryTrack({ source: { kind: 'library', trackId: asTrackId('t1') } }),
    );
    flushPlaybackHealth();
    expect((recordEvent as jest.Mock).mock.calls[0]![0].payload).toMatchObject({
      presign_failed: 1,
    });
  });
});
