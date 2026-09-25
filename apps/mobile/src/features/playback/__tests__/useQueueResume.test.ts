import { act, renderHook } from '@testing-library/react-native';
import { AppState, type AppStateStatus } from 'react-native';
import TrackPlayer from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import type { ResolvedAudioUrl } from '@shared/api-client/audio';
import { fetchAudioUrls } from '@shared/api-client/audio';
import { getQueueState, saveQueueState } from '@shared/api-client/playback';
import type { QueueStateResponse, SaveQueueStateRequest } from '@shared/api-client/playback';
import { getAllTracks, getTracks } from '@shared/api-client/tracks';
import type { AcquisitionStatus, TrackResponse } from '@shared/api-client/types';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { useQueueResume } from '../hooks/useQueueResume';
import { loadNativeQueue } from '../loadNativeTrack';
import { usePlaybackErrorStore } from '../playbackErrorStore';

import { libraryTrack } from './fixtures';

jest.mock('@shared/api-client/playback', () => ({
  getQueueState: jest.fn(),
  saveQueueState: jest.fn(async () => undefined),
}));
jest.mock('@shared/api-client/tracks', () => ({ getTracks: jest.fn(), getAllTracks: jest.fn() }));
jest.mock('@shared/api-client/audio', () => ({
  audioStreamUrl: (id: string) => `https://api.example/audio/${id}`,
  audioRequestHeaders: jest.fn(async () => ({})),
  fetchAudioUrls: jest.fn(async () => []),
}));

const { __player } = jest.requireMock('react-native-track-player');

// Regression (#818): a malformed queue-state response is rejected at one named parse
// boundary and logged, instead of crashing mid-restore or silently restoring a queue
// with an unrecognized source reported as the library.
describe('restoring the saved queue', () => {
  // Put the API doubles back to the defaults this block was written against.
  beforeEach(() => {
    (getQueueState as jest.Mock).mockReset();
    (saveQueueState as jest.Mock).mockReset().mockImplementation(async () => undefined);
    (fetchAudioUrls as jest.Mock).mockReset().mockImplementation(async () => []);
  });

  const mockedGetQueueState = getQueueState as jest.Mock;
  const mockedGetTracks = getTracks as jest.Mock;
  const mockedGetAllTracks = getAllTracks as jest.Mock;

  function trackResponse(
    id: string,
    acquisitionStatus: AcquisitionStatus = 'ready',
  ): TrackResponse {
    return {
      id,
      title: `Title ${id}`,
      artist: 'Artist',
      album: null,
      duration_seconds: 200,
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: acquisitionStatus,
      artwork_url: null,
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
    } as TrackResponse;
  }

  function validWire(): Record<string, unknown> {
    return {
      track_ids: ['x', 'y'],
      current_index: 1,
      position_ms: 5000,
      shuffled: false,
      repeat_mode: 'off',
      source: { kind: 'playlist', playlist_id: 'p1', name: 'Chill' },
      natural_order: ['x', 'y'],
    };
  }

  async function settlePendingWork(): Promise<void> {
    await act(async () => {
      for (let i = 0; i < 40; i++) await Promise.resolve();
    });
  }

  async function settleRestore(): Promise<void> {
    renderHook(() => useQueueResume());
    await settlePendingWork();
  }

  async function restore(body: unknown): Promise<void> {
    mockedGetQueueState.mockResolvedValue(body);
    await settleRestore();
  }

  let warn: jest.SpyInstance;

  beforeEach(() => {
    useQueueStore.getState().clearQueue();
    mockedGetQueueState.mockReset();
    mockedGetTracks.mockReset();
    mockedGetAllTracks.mockReset().mockResolvedValue(['x', 'y'].map((id) => trackResponse(id)));
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  function warnedMalformed(): boolean {
    return warn.mock.calls.some(([message]) =>
      String(message).includes('rejected a malformed saved queue state'),
    );
  }

  describe('useQueueResume restore — queue-state parse boundary', () => {
    it('restores a well-formed saved queue without warning', async () => {
      await restore(validWire());

      const s = useQueueStore.getState();
      expect(s.tracks).toHaveLength(2);
      expect(s.currentIndex).toBe(1);
      expect(s.source).toEqual({ kind: 'playlist', playlistId: 'p1', name: 'Chill' });
      expect(warn).not.toHaveBeenCalled();
    });

    // Regression (#1736): a source kind a newer app version added must cost the restore only
    // the source, not the queue and position saved beside it.
    it('restores the saved queue with no source when source.kind is unrecognized', async () => {
      await restore({ ...validWire(), source: { kind: 'album' } });

      const s = useQueueStore.getState();
      expect(s.tracks).toHaveLength(2);
      expect(s.currentIndex).toBe(1);
      expect(s.source).toBeNull();
      expect(warnedMalformed()).toBe(false);
    });

    it.each<[string, (w: Record<string, unknown>) => unknown]>([
      ['missing track_ids', ({ track_ids: _drop, ...rest }) => rest],
      ['non-array track_ids', (w) => ({ ...w, track_ids: 'x,y' })],
      ['non-string track id', (w) => ({ ...w, track_ids: ['x', 7] })],
      ['fractional current_index', (w) => ({ ...w, current_index: 0.5 })],
      ['negative current_index', (w) => ({ ...w, current_index: -1 })],
      ['string current_index', (w) => ({ ...w, current_index: '1' })],
      ['non-array natural_order', (w) => ({ ...w, natural_order: { 0: 'x' } })],
      ['non-object body', () => 'queue'],
    ])('rejects and logs a response with %s, restoring nothing', async (_label, mutate) => {
      await restore(mutate(validWire()));

      expect(warnedMalformed()).toBe(true);
      expect(mockedGetAllTracks).not.toHaveBeenCalled();
      const s = useQueueStore.getState();
      expect(s.tracks).toHaveLength(0);
      expect(s.source).toBeNull();
    });

    it('accepts a legacy row with no natural_order by rebuilding from the play order', async () => {
      const legacy = validWire();
      delete legacy.natural_order;
      await restore({ ...legacy, source: null });

      const s = useQueueStore.getState();
      expect(s.tracks).toHaveLength(2);
      expect(s.currentIndex).toBe(1);
      expect(warn).not.toHaveBeenCalled();
    });
  });

  // Regression (#1743): the restore catch logged a bare string over a multi-stage chain, so
  // "my queue never resumes" could not be told from the logs apart from a dead network.
  describe('useQueueResume restore — a failure names its stage and carries the error', () => {
    function restoreFailureFields(): unknown {
      const call = warn.mock.calls.find(
        ([message]) => message === '[playback] failed to restore the saved queue',
      );
      return call?.[1];
    }

    it('blames the fetch stage when reading the saved queue state throws', async () => {
      const offline = new Error('network down');
      mockedGetQueueState.mockRejectedValue(offline);

      await settleRestore();

      expect(restoreFailureFields()).toEqual({
        stage: 'fetch',
        error: { kind: 'unknown', message: 'network down' },
      });
    });

    it('blames the tracks stage when the library read behind rehydration throws', async () => {
      const unavailable = new Error('tracks 503');
      mockedGetAllTracks.mockRejectedValue(unavailable);

      await restore(validWire());

      expect(restoreFailureFields()).toEqual({
        stage: 'tracks',
        error: { kind: 'unknown', message: 'tracks 503' },
      });
    });

    it('blames the native stage when handing the rebuilt queue to the player throws', async () => {
      const addRejected = new Error('native add rejected');
      __player.failNext('add', addRejected);

      await restore(validWire());

      expect(restoreFailureFields()).toEqual({
        stage: 'native',
        error: { kind: 'unknown', message: 'native add rejected' },
      });
    });
  });

  // Regression (#1726): the rehydration placeholder is a "now playing" card with nothing in the
  // native player behind it, so a restore that stops before the native load left play, pause and
  // seek as silent no-ops against a track the store still reported as current.
  describe('useQueueResume restore — an unbacked placeholder is taken back down', () => {
    function savedWithCurrentTrack(): Record<string, unknown> {
      return {
        ...validWire(),
        current_track: {
          id: 'y',
          title: 'Title y',
          artist: 'Artist',
          artwork_url: null,
          duration_seconds: 200,
          acquisition_status: 'ready',
        },
      };
    }

    function clearedPlaceholderFields(): unknown {
      const call = warn.mock.calls.find(
        ([message]) => message === '[playback] cleared the unbacked resume placeholder',
      );
      return call?.[1];
    }

    function expectNothingPlaying(): void {
      const s = useQueueStore.getState();
      expect(s.currentTrack()).toBeNull();
      expect(s.tracks).toHaveLength(0);
      expect(__player.calls('add')).toHaveLength(0);
    }

    it('clears the placeholder it showed when the library fetch comes back empty', async () => {
      let resolveLibrary!: (tracks: TrackResponse[]) => void;
      mockedGetAllTracks.mockReturnValue(
        new Promise<TrackResponse[]>((resolve) => {
          resolveLibrary = resolve;
        }),
      );
      mockedGetQueueState.mockResolvedValue(savedWithCurrentTrack());

      await settleRestore();
      expect(useQueueStore.getState().currentTrack()?.title).toBe('Title y');

      resolveLibrary([]);
      await settlePendingWork();

      expectNothingPlaying();
      expect(clearedPlaceholderFields()).toEqual({ stage: 'tracks' });
    });

    it('clears the placeholder when no saved track is ready to rebuild from', async () => {
      mockedGetAllTracks.mockResolvedValue(['x', 'y'].map((id) => trackResponse(id, 'pending')));

      await restore(savedWithCurrentTrack());

      expectNothingPlaying();
      expect(clearedPlaceholderFields()).toEqual({ stage: 'rebuild' });
    });

    it('keeps the rebuilt queue when the restore runs through to the native load', async () => {
      await restore(savedWithCurrentTrack());

      expect(useQueueStore.getState().tracks).toHaveLength(2);
      expect(__player.calls('add')).not.toHaveLength(0);
      expect(clearedPlaceholderFields()).toBeUndefined();
    });
  });

  // Regression (#1740): the restore read one fixed 2000-track page of the library, so a saved
  // queue referencing anything past that page came back short with nothing logged.
  describe('useQueueResume restore — a saved queue larger than one library page', () => {
    const LIBRARY_PAGE = 2000;
    const LIBRARY_SIZE = 2500;

    function libraryTracks(size: number): TrackResponse[] {
      return Array.from({ length: size }, (_, i) => trackResponse(`t${i}`));
    }

    function savedWholeLibrary(
      tracks: TrackResponse[],
      currentIndex: number,
    ): Record<string, unknown> {
      const ids = tracks.map((t) => t.id);
      return { ...validWire(), track_ids: ids, natural_order: ids, current_index: currentIndex };
    }

    function missingFromLibraryFields(): unknown {
      const call = warn.mock.calls.find(
        ([message]) =>
          message ===
          '[playback] saved queue tracks missing from the library read; restoring without',
      );
      return call?.[1];
    }

    it('restores every saved track when the library spans more than one page', async () => {
      const tracks = libraryTracks(LIBRARY_SIZE);
      mockedGetAllTracks.mockResolvedValue(tracks);
      // What a single first page would answer — a restore reading only that page rebuilds
      // without the tail, and the store below says so.
      mockedGetTracks.mockResolvedValue({ items: tracks.slice(0, LIBRARY_PAGE), has_more: true });

      await restore(savedWholeLibrary(tracks, LIBRARY_SIZE - 1));

      const s = useQueueStore.getState();
      expect(s.tracks).toHaveLength(LIBRARY_SIZE);
      expect(s.currentTrack()?.title).toBe(`Title t${LIBRARY_SIZE - 1}`);
      expect(warn).not.toHaveBeenCalled();
    });

    it('warns with the count of saved tracks the library read did not return', async () => {
      const tracks = libraryTracks(LIBRARY_SIZE);
      const firstPage = tracks.slice(0, LIBRARY_PAGE);
      mockedGetAllTracks.mockResolvedValue(firstPage);
      mockedGetTracks.mockResolvedValue({ items: firstPage, has_more: true });

      await restore(savedWholeLibrary(tracks, 0));

      expect(useQueueStore.getState().tracks).toHaveLength(LIBRARY_PAGE);
      expect(missingFromLibraryFields()).toEqual({
        missing: LIBRARY_SIZE - LIBRARY_PAGE,
        saved: LIBRARY_SIZE,
      });
    });
  });
});

describe('a restore whose native load fails', () => {
  // Put the API doubles back to the defaults this block was written against.
  beforeEach(() => {
    (getQueueState as jest.Mock).mockReset();
    (saveQueueState as jest.Mock).mockReset().mockImplementation(async () => undefined);
    (fetchAudioUrls as jest.Mock).mockReset().mockImplementation(async () => []);
  });

  function trackResponse(id: string): TrackResponse {
    return {
      id,
      title: `Title ${id}`,
      artist: 'Artist',
      album: null,
      duration_seconds: 200,
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: 'ready',
      artwork_url: null,
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
    } as TrackResponse;
  }

  const wire = {
    track_ids: ['x', 'y'],
    current_index: 1,
    position_ms: 5000,
    shuffled: false,
    repeat_mode: 'off',
    source: { kind: 'playlist', playlist_id: 'p1', name: 'Chill' },
    natural_order: ['x', 'y'],
  };

  async function restore(): Promise<void> {
    (getQueueState as jest.Mock).mockResolvedValue(wire);
    renderHook(() => useQueueResume());
    await act(async () => {
      for (let i = 0; i < 40; i++) await Promise.resolve();
    });
  }

  beforeEach(() => {
    useQueueStore.getState().clearQueue();
    usePlaybackErrorStore.getState().clear();
    (getAllTracks as jest.Mock).mockReset().mockResolvedValue(['x', 'y'].map(trackResponse));
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  describe('useQueueResume restore, native stage failure (#2702)', () => {
    it('reports the failure against the current track when the native load rejects', async () => {
      __player.failNext('add', new Error('native add rejected'));

      await restore();

      const current = useQueueStore.getState().currentTrack();
      expect(current).not.toBeNull();
      const error = usePlaybackErrorStore.getState();
      expect(error.key).toBe(trackKey(current!));
      expect(error.message).toBe('native add rejected');
    });

    it('reports nothing when the native load succeeds', async () => {
      await restore();

      expect(usePlaybackErrorStore.getState().key).toBeNull();
    });

    it('reports nothing when the queue is replaced during the native load that then rejects', async () => {
      (TrackPlayer.add as jest.Mock).mockImplementationOnce(async () => {
        useQueueStore.getState().loadQueue([{ ...trackResponse('z'), id: 'z' } as never], 0, null);
        throw new Error('native add rejected late');
      });

      await restore();

      expect(useQueueStore.getState().currentTrack()).not.toBeNull();
      expect(usePlaybackErrorStore.getState().key).toBeNull();
    });
  });
});

// Regression (#814): a queue-state save racing a queue load must never pair the new
// queue's track_ids/current_index with a position_ms read off the previous track.
// Drives the real loadNativeQueue + queueStore against a small model of the native
// player, and triggers saves the way the app does (AppState → background).
describe('saving the queue state', () => {
  // Put the API doubles back to the defaults this block was written against.
  beforeEach(() => {
    (getQueueState as jest.Mock).mockReset();
    (saveQueueState as jest.Mock).mockReset();
    (fetchAudioUrls as jest.Mock).mockReset().mockImplementation(async () => []);
  });

  type NativeItem = { id: string };
  const player = TrackPlayer as unknown as Record<string, jest.Mock>;
  const mockedSave = saveQueueState as jest.MockedFunction<typeof saveQueueState>;
  const mockedFetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  // Native player model: a queue, an active index and a position (seconds).
  let nativeQueue: NativeItem[];
  let nativeIndex: number;
  let nativePosition: number;

  function modelNativePlayer(): void {
    nativeQueue = [];
    nativeIndex = -1;
    nativePosition = 0;
    player.reset!.mockImplementation(async () => {
      nativeQueue = [];
      nativeIndex = -1;
      nativePosition = 0;
    });
    player.add!.mockImplementation(async (items: NativeItem | NativeItem[]) => {
      nativeQueue = [...nativeQueue, ...(Array.isArray(items) ? items : [items])];
      if (nativeIndex < 0) nativeIndex = 0;
    });
    player.skip!.mockImplementation(async (index: number) => {
      nativeIndex = index;
      nativePosition = 0;
    });
    player.seekTo!.mockImplementation(async (seconds: number) => {
      nativePosition = seconds;
    });
    player.getActiveTrack!.mockImplementation(async () => nativeQueue[nativeIndex]);
    player.getProgress!.mockImplementation(async () => ({
      position: nativePosition,
      duration: 300,
      buffered: 300,
    }));
  }

  let appStateListeners: ((state: AppStateStatus) => void)[];

  function tracks(prefix: string, count: number): PlaybackTrack[] {
    return Array.from({ length: count }, (_, i) =>
      libraryTrack({ source: { kind: 'library', trackId: asTrackId(`${prefix}${i}`) } }),
    );
  }

  async function flush(): Promise<void> {
    await act(async () => {
      for (let i = 0; i < 20; i++) await Promise.resolve();
    });
  }

  async function backgroundApp(): Promise<void> {
    await act(async () => {
      for (const l of [...appStateListeners]) l('background');
    });
    await flush();
  }

  function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((r) => {
      resolve = r;
    });
    return { promise, resolve };
  }

  beforeEach(async () => {
    modelNativePlayer();
    appStateListeners = [];
    jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
      appStateListeners.push(listener as (state: AppStateStatus) => void);
      return { remove: jest.fn() };
    });
    (getQueueState as jest.Mock).mockResolvedValue({
      track_ids: [],
      current_index: 0,
      position_ms: 0,
      shuffled: false,
      repeat_mode: 'off',
      source: null,
      natural_order: [],
    });
    mockedSave.mockReset().mockResolvedValue(undefined);
    mockedFetchUrls.mockReset().mockResolvedValue([]);
    useQueueStore.getState().clearQueue();

    // Queue A is loaded and playing a1 at 42s.
    const a = tracks('a', 3);
    useQueueStore.getState().loadQueue(a, 1, null);
    await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 1, { autoplay: false });
    nativePosition = 42;
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  describe('useQueueResume save — position and queue snapshot stay consistent', () => {
    it('saves the current queue with the native position when no load is in flight', async () => {
      renderHook(() => useQueueResume());
      await flush();

      await backgroundApp();

      expect(mockedSave).toHaveBeenCalledTimes(1);
      expect(mockedSave.mock.calls[0]![0]).toMatchObject({
        track_ids: ['a0', 'a1', 'a2'],
        current_index: 1,
        position_ms: 42_000,
      });
    });

    it('never pairs a freshly started queue with the previous track position', async () => {
      renderHook(() => useQueueResume());
      await flush();

      // User starts queue B at b2; the native load is held on its URL round trip.
      const urls = deferred<ResolvedAudioUrl[]>();
      mockedFetchUrls.mockReturnValueOnce(urls.promise);
      const b = tracks('b', 4);
      useQueueStore.getState().loadQueue(b, 2, null);
      const load = loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 2, {
        autoplay: false,
      });

      // Save fires before the native player has even been reset (still on a1 @ 42s)…
      await backgroundApp();
      // …and again while the load waits on the network with an empty native queue.
      await backgroundApp();

      for (const [body] of mockedSave.mock.calls) {
        expect(body).not.toMatchObject({
          track_ids: ['b0', 'b1', 'b2', 'b3'],
          position_ms: 42_000,
        });
      }
      // Neither moment has a consistent (queue, position) pair, so neither saves.
      expect(mockedSave).not.toHaveBeenCalled();

      urls.resolve([]);
      await act(async () => {
        await load;
      });
      nativePosition = 7;

      await backgroundApp();

      expect(mockedSave).toHaveBeenCalledTimes(1);
      expect(mockedSave.mock.calls[0]![0]).toMatchObject({
        track_ids: ['b0', 'b1', 'b2', 'b3'],
        current_index: 2,
        position_ms: 7_000,
      });
    });

    it('waits out a native load op instead of reading between its add and its seek', async () => {
      renderHook(() => useQueueResume());
      await flush();

      // Resume-style load of B at b0 from 30s; the native add is slow to settle.
      const added = deferred<void>();
      const b = tracks('b', 2);
      useQueueStore.getState().loadQueue(b, 0, null);
      const addImpl = player.add!.getMockImplementation()!;
      player.add!.mockImplementationOnce(async (items: NativeItem[]) => {
        await addImpl(items);
        await added.promise;
      });
      const load = loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, {
        autoplay: false,
        startPositionMs: 30_000,
      });
      await flush();
      expect(nativeQueue.map((t) => t.id)).toEqual(['library:b0', 'library:b1']);

      // b0 is already active at 0s, but the seek to 30s has not run yet.
      await backgroundApp();
      added.resolve();
      await act(async () => {
        await load;
      });
      await flush();

      expect(mockedSave).toHaveBeenCalledTimes(1);
      expect(mockedSave.mock.calls[0]![0]).toMatchObject({
        track_ids: ['b0', 'b1'],
        current_index: 0,
        position_ms: 30_000,
      });
    });

    it('skips a save while the native player has not yet caught up with a store skip', async () => {
      renderHook(() => useQueueResume());
      await flush();

      // Store cursor moved to a2 but the native player is still on a1.
      useQueueStore.getState().syncCurrentIndex(2);

      await backgroundApp();

      expect(mockedSave).not.toHaveBeenCalled();
    });
  });

  // Regression (#815): the 15s interval save and the AppState save must not race so
  // that an older snapshot's PUT lands after a fresher one.
  describe('useQueueResume save — concurrent triggers land in snapshot order', () => {
    let server: { position_ms: number } | null;
    let inFlight: number;
    let maxInFlight: number;
    let pendingPuts: { resolve: () => void }[];

    // Server model: a PUT is applied when its request settles, so the last to settle wins.
    function modelServer(): void {
      server = null;
      inFlight = 0;
      maxInFlight = 0;
      pendingPuts = [];
      mockedSave.mockImplementation((body) => {
        inFlight += 1;
        maxInFlight = Math.max(maxInFlight, inFlight);
        const put = deferred<void>();
        pendingPuts.push({ resolve: () => put.resolve() });
        return put.promise.then(() => {
          inFlight -= 1;
          server = { position_ms: body.position_ms };
        });
      });
    }

    beforeEach(() => {
      jest.useFakeTimers();
      modelServer();
    });

    afterEach(() => {
      jest.useRealTimers();
    });

    it('keeps the later snapshot on the server when the older PUT resolves last', async () => {
      renderHook(() => useQueueResume());
      await flush();

      // Interval save reads a1 @ 42s and its PUT is slow.
      await act(async () => {
        jest.advanceTimersByTime(15_000);
      });
      await flush();
      expect(pendingPuts).toHaveLength(1);

      // Playback moves on; the app backgrounds while that PUT is still in flight.
      nativePosition = 50;
      await backgroundApp();
      // Any PUT started for the fresher snapshot settles first…
      for (const put of pendingPuts.slice(1)) put.resolve();
      await flush();
      // …then the stale one.
      pendingPuts[0]!.resolve();
      await flush();
      // Settle whatever follow-up save the serialization scheduled.
      for (const put of pendingPuts.slice(1)) put.resolve();
      await flush();

      expect(server).toEqual({ position_ms: 50_000 });
      expect(maxInFlight).toBe(1);
    });

    it('coalesces triggers that arrive during one in-flight save into one follow-up save', async () => {
      renderHook(() => useQueueResume());
      await flush();

      await backgroundApp();
      nativePosition = 60;
      await backgroundApp();
      await backgroundApp();
      await act(async () => {
        jest.advanceTimersByTime(15_000);
      });
      await flush();
      expect(pendingPuts).toHaveLength(1);

      pendingPuts[0]!.resolve();
      await flush();
      expect(pendingPuts).toHaveLength(2);
      pendingPuts[1]!.resolve();
      await flush();

      expect(mockedSave.mock.calls.map(([body]) => body.position_ms)).toEqual([42_000, 60_000]);
      expect(server).toEqual({ position_ms: 60_000 });
    });

    // Regression (#1743): the save catch logged a context-free string, so a failed save
    // was indistinguishable from any other in the logs.
    it('logs the rejection the save PUT threw', async () => {
      renderHook(() => useQueueResume());
      await flush();
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      const networkDown = new Error('network down');
      mockedSave.mockRejectedValueOnce(networkDown);

      await backgroundApp();

      expect(warn).toHaveBeenCalledWith('[playback] failed to save queue state', {
        error: { kind: 'unknown', message: 'network down' },
      });
    });

    it('saves again after a failed save instead of wedging the guard', async () => {
      renderHook(() => useQueueResume());
      await flush();
      jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      mockedSave.mockRejectedValueOnce(new Error('network down'));

      await backgroundApp();

      nativePosition = 70;
      await backgroundApp();
      pendingPuts[0]!.resolve();
      await flush();

      expect(server).toEqual({ position_ms: 70_000 });
    });
  });

  // Regression (#817): with the same track queued twice, save and restore must track the
  // copy that is actually playing, not the first (save) or last (restore) id match.
  describe('useQueueResume — duplicate track ids round-trip to the playing copy', () => {
    function trackResponse(id: string): TrackResponse {
      return {
        id: asTrackId(id),
        title: id,
        artist: 'Artist',
        album: null,
        duration_seconds: 300,
        added_at: '2026-01-01T00:00:00Z',
        acquisition_status: 'ready',
        artwork_url: null,
        failure_reason: null,
        year: null,
        genre: null,
        track_number: null,
        album_artist: null,
        isrc: null,
        audio_ref: null,
      };
    }

    function queueOf(...ids: string[]): PlaybackTrack[] {
      return ids.map((id) => libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } }));
    }

    async function saveWhilePlaying(
      startIndex: number,
      shuffled: boolean,
    ): Promise<SaveQueueStateRequest> {
      useQueueStore.getState().clearQueue();
      // x is queued twice; with shuffle the play order interleaves the copies differently.
      useQueueStore.getState().loadQueue(queueOf('x', 'y', 'x', 'z'), startIndex, null);
      if (shuffled) {
        useQueueStore.setState({ playOrder: [2, 1, 3, 0], shuffled: true });
      }
      await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), startIndex, {
        autoplay: false,
      });
      nativePosition = 33;

      const { unmount } = renderHook(() => useQueueResume());
      await flush();
      await backgroundApp();
      unmount();
      expect(mockedSave).toHaveBeenCalledTimes(1);
      return mockedSave.mock.calls[0]![0];
    }

    async function restore(body: QueueStateResponse): Promise<void> {
      useQueueStore.getState().clearQueue();
      modelNativePlayer();
      (getQueueState as jest.Mock).mockResolvedValue(body);
      (getAllTracks as jest.Mock).mockResolvedValue(['x', 'y', 'z'].map(trackResponse));
      renderHook(() => useQueueResume());
      await flush();
      await flush();
    }

    function playingCopy(): { index: number; ids: string[] } {
      const s = useQueueStore.getState();
      return {
        index: s.currentIndex,
        ids: orderedQueueTracks(s).map((t) =>
          t.source.kind === 'library' ? t.source.trackId : '',
        ),
      };
    }

    it.each([
      ['natural order', false, (b: SaveQueueStateRequest) => b],
      ['play order alone', false, (b: SaveQueueStateRequest) => ({ ...b, natural_order: [] })],
      ['shuffled natural order', true, (b: SaveQueueStateRequest) => b],
    ])(
      'saves and restores the second copy of a duplicated track when it is playing (%s)',
      async (_label, shuffled, wire) => {
        // Unshuffled: x y [x] z. Shuffled play order [2,1,3,0] is x y z [x] — start at 3.
        const startIndex = shuffled ? 3 : 2;
        const body = await saveWhilePlaying(startIndex, shuffled);

        expect(body.current_index).toBe(startIndex);
        expect(body.track_ids[body.current_index]).toBe('x');

        await restore(wire(body));

        const restored = playingCopy();
        expect(restored.ids).toEqual(body.track_ids);
        expect(restored.index).toBe(startIndex);
        expect(nativeIndex).toBe(startIndex);
        expect(nativePosition).toBe(33);
      },
    );
  });
});

describe('a save issued while another drains', () => {
  // Put the API doubles back to the defaults this block was written against.
  beforeEach(() => {
    (getQueueState as jest.Mock).mockReset().mockImplementation(async () => null);
    (saveQueueState as jest.Mock).mockReset();
    (fetchAudioUrls as jest.Mock).mockReset().mockImplementation(async () => []);
  });

  const player = TrackPlayer as unknown as Record<string, jest.Mock>;
  const mockedSave = saveQueueState as jest.MockedFunction<typeof saveQueueState>;

  describe('useQueueResume save — a save issued as the drain finishes is not dropped', () => {
    it.each(Array.from({ length: 30 }, (_, offset) => offset))(
      'saves the latest position when the second trigger lands %i ticks after the first',
      async (offset) => {
        let position = 1;
        const listeners: ((state: AppStateStatus) => void)[] = [];
        jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
          listeners.push(listener as (state: AppStateStatus) => void);
          return { remove: jest.fn() };
        });
        player.getActiveTrack!.mockImplementation(async () => ({ id: 'library:a0' }));
        player.getProgress!.mockImplementation(async () => ({
          position,
          duration: 300,
          buffered: 0,
        }));
        mockedSave.mockReset().mockResolvedValue(undefined);
        const tracks = [libraryTrack({ source: { kind: 'library', trackId: asTrackId('a0') } })];
        useQueueStore.getState().clearQueue();
        useQueueStore.getState().loadQueue(tracks, 0, null);
        await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });
        renderHook(() => useQueueResume());
        await act(async () => {
          for (let i = 0; i < 20; i++) await Promise.resolve();
        });

        await act(async () => {
          for (const l of [...listeners]) l('background');
          for (let tick = 0; tick < offset; tick++) await Promise.resolve();
          position = 99;
          for (const l of [...listeners]) l('background');
          for (let i = 0; i < 60; i++) await Promise.resolve();
        });

        const last = mockedSave.mock.calls.at(-1)![0];
        expect(last.position_ms).toBe(99_000);
      },
    );
  });
});
