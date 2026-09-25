// Regression (#816): reorderUpcomingNative resolves signed URLs before it takes the
// native queue lock. Native can auto-advance (PlaybackActiveTrackChanged) while that
// resolve is in flight; removeUpcomingTracks then trims relative to the NEW active
// track, and re-adding the stale `upcoming` list duplicated the track that just
// became active. Drives the real reorder against a small model of the native queue.

import TrackPlayer from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { reorderUpcomingNative } from '../loadNativeTrack';

import { libraryTrack } from './fixtures';

jest.mock('@shared/api-client/audio', () => ({
  audioStreamUrl: (id: string) => `https://api.example/audio/${id}`,
  audioRequestHeaders: jest.fn(async () => ({})),
  fetchAudioUrls: jest.fn(async () => []),
}));

type NativeItem = { id: string };
const player = TrackPlayer as unknown as Record<string, jest.Mock>;
const mockedFetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

let nativeQueue: NativeItem[];
let nativeIndex: number;
// How many times the tail was handed to native: one remove + add per rebuild.
let tailRebuilds: number;

function modelNativePlayer(items: readonly PlaybackTrack[], active: number): void {
  nativeQueue = items.map((t) => ({ id: trackKey(t) }));
  nativeIndex = active;
  tailRebuilds = 0;
  player.add!.mockImplementation(async (added: NativeItem | NativeItem[]) => {
    nativeQueue = [...nativeQueue, ...(Array.isArray(added) ? added : [added])];
  });
  player.removeUpcomingTracks!.mockImplementation(async () => {
    tailRebuilds += 1;
    nativeQueue = nativeQueue.slice(0, nativeIndex + 1);
  });
  player.getActiveTrack!.mockImplementation(async () => nativeQueue[nativeIndex]);
  player.getActiveTrackIndex!.mockImplementation(async () => nativeIndex);
}

// Native finishing the active track on its own: the index moves and the service
// syncs the store cursor from the PlaybackActiveTrackChanged payload.
function nativeAutoAdvance(): void {
  nativeIndex += 1;
  const active = nativeQueue[nativeIndex]!;
  useQueueStore.getState().syncCurrentIndex(nativeIndex, active.id);
}

function deferredUrls(): () => void {
  let release!: () => void;
  const gate = new Promise<ResolvedAudioUrl[]>((resolve) => {
    release = () => resolve([]);
  });
  mockedFetchUrls.mockImplementationOnce(() => gate);
  return release;
}

async function flush(): Promise<void> {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

function track(id: string): PlaybackTrack {
  return libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } });
}

const A = track('a');
const B = track('b');
const C = track('c');
const D = track('d');

afterEach(() => {
  for (const name of ['add', 'removeUpcomingTracks', 'getActiveTrack', 'getActiveTrackIndex']) {
    player[name]!.mockReset();
  }
  mockedFetchUrls.mockReset();
  mockedFetchUrls.mockImplementation(async () => []);
  useQueueStore.getState().clearQueue();
});

describe('reorderUpcomingNative racing a native auto-advance (#816)', () => {
  it('does not duplicate the track native advanced to while URLs were resolving', async () => {
    useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
    useQueueStore.getState().reorderQueue(2, 1); // store order: A*, C, B, D
    modelNativePlayer([A, B, C, D], 0);
    const release = deferredUrls();

    const reorder = reorderUpcomingNative([C, B, D]);
    await flush();
    nativeAutoAdvance(); // native: A -> B before the reorder reaches the lock
    release();
    await reorder;

    const keys = nativeQueue.map((item) => item.id);
    expect(new Set(keys).size).toBe(keys.length);
    expect(keys).toEqual([A, B, D].map(trackKey));
    expect(nativeQueue[nativeIndex]!.id).toBe(trackKey(B));
    // Native's upcoming tracks match the store's view after its cursor synced to B.
    const s = useQueueStore.getState();
    expect(keys.slice(nativeIndex + 1)).toEqual(
      orderedQueueTracks(s)
        .slice(s.currentIndex + 1)
        .map(trackKey),
    );
  });

  it('re-adds the whole reordered list when native did not advance', async () => {
    modelNativePlayer([A, B, C, D], 0);
    const release = deferredUrls();

    const reorder = reorderUpcomingNative([C, B, D]);
    await flush();
    release();
    await reorder;

    expect(nativeQueue.map((item) => item.id)).toEqual([A, C, B, D].map(trackKey));
  });

  it('falls back to the whole list when the active track cannot be read', async () => {
    modelNativePlayer([A, B, C, D], 0);
    player.getActiveTrack!.mockRejectedValue(new Error('bridge down'));

    await reorderUpcomingNative([C, B, D]);

    expect(nativeQueue.map((item) => item.id)).toEqual([A, C, B, D].map(trackKey));
  });

  it('keeps a queued-twice copy of the playing track when native did not advance', async () => {
    modelNativePlayer([A, B, A], 0);

    await reorderUpcomingNative([A, B]);

    expect(nativeQueue.map((item) => item.id)).toEqual([A, A, B].map(trackKey));
  });
});

// Regression (#1735): every "Move Up" tap and shuffle toggle handed its own upcoming list
// to reorderUpcomingNative, so a burst cost one presign round trip and one full tail
// rebuild per tap — each pushing an order the next tap had already replaced.
describe('reorderUpcomingNative coalescing a burst of reorder taps (#1735)', () => {
  function moveInStore(fromIndex: number, toIndex: number): readonly PlaybackTrack[] {
    return useQueueStore.getState().reorderQueue(fromIndex, toIndex);
  }

  function storedOrder(): string[] {
    return orderedQueueTracks(useQueueStore.getState()).map(trackKey);
  }

  beforeEach(() => {
    useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
    modelNativePlayer([A, B, C, D], 0);
  });

  it('rebuilds the native tail once for three moves fired in one tick', async () => {
    await Promise.all([
      reorderUpcomingNative(moveInStore(1, 3)),
      reorderUpcomingNative(moveInStore(1, 2)),
      reorderUpcomingNative(moveInStore(3, 1)),
    ]);

    expect(tailRebuilds).toBe(1);
    expect(nativeQueue.map((item) => item.id)).toEqual([A, B, D, C].map(trackKey));
    expect(nativeQueue.map((item) => item.id)).toEqual(storedOrder());
  });

  it('collapses moves made while a rebuild is in flight onto one trailing rebuild', async () => {
    const release = deferredUrls();

    const firstMove = reorderUpcomingNative(moveInStore(1, 3));
    await flush();
    const duringRebuild = [
      reorderUpcomingNative(moveInStore(3, 1)),
      reorderUpcomingNative(moveInStore(2, 3)),
      reorderUpcomingNative(moveInStore(1, 2)),
    ];
    release();
    await Promise.all([firstMove, ...duringRebuild]);

    expect(tailRebuilds).toBe(2);
    expect(nativeQueue.map((item) => item.id)).toEqual([A, D, B, C].map(trackKey));
    expect(nativeQueue.map((item) => item.id)).toEqual(storedOrder());
  });

  it('resolves a superseded caller only once native holds the newer order', async () => {
    const release = deferredUrls();

    const supersededMove = reorderUpcomingNative(moveInStore(1, 3));
    await flush();
    void reorderUpcomingNative(moveInStore(1, 2));
    release();
    await supersededMove;

    expect(nativeQueue.map((item) => item.id)).toEqual([A, D, C, B].map(trackKey));
    expect(nativeQueue.map((item) => item.id)).toEqual(storedOrder());
  });
});
