// Regression (#814): a queue-state save racing a queue load must never pair the new
// queue's track_ids/current_index with a position_ms read off the previous track.
// Drives the real loadNativeQueue + queueStore against a small model of the native
// player, and triggers saves the way the app does (AppState → background).

import { act, renderHook } from '@testing-library/react-native';
import { AppState, type AppStateStatus } from 'react-native';
import TrackPlayer from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import type { ResolvedAudioUrl } from '@shared/api-client/audio';
import { fetchAudioUrls } from '@shared/api-client/audio';
import { getQueueState, saveQueueState } from '@shared/api-client/playback';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { useQueueResume } from '../hooks/useQueueResume';
import { loadNativeQueue } from '../loadNativeTrack';

import { libraryTrack } from './fixtures';

jest.mock('@shared/api-client/playback', () => ({
  getQueueState: jest.fn(),
  saveQueueState: jest.fn(),
}));
jest.mock('@shared/api-client/tracks', () => ({ getTracks: jest.fn() }));
jest.mock('@shared/api-client/audio', () => ({
  audioStreamUrl: (id: string) => `https://api.example/audio/${id}`,
  audioRequestHeaders: jest.fn(async () => ({})),
  fetchAudioUrls: jest.fn(async () => []),
}));

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
  (getQueueState as jest.Mock).mockResolvedValue({ track_ids: [] });
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
      expect(body).not.toMatchObject({ track_ids: ['b0', 'b1', 'b2', 'b3'], position_ms: 42_000 });
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

  it('saves again after a failed save instead of wedging the guard', async () => {
    renderHook(() => useQueueResume());
    await flush();
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    mockedSave.mockRejectedValueOnce(new Error('network down'));

    await backgroundApp();
    expect(warn).toHaveBeenCalledWith('[playback] failed to save queue state');

    nativePosition = 70;
    await backgroundApp();
    pendingPuts[0]!.resolve();
    await flush();

    expect(server).toEqual({ position_ms: 70_000 });
  });
});
