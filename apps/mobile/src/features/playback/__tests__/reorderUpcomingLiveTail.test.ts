import TrackPlayer from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { insertNativeTrackNext, reorderUpcomingNative } from '../loadNativeTrack';

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

function modelNativePlayer(items: readonly PlaybackTrack[], active: number): void {
  nativeQueue = items.map((t) => ({ id: trackKey(t) }));
  nativeIndex = active;
  player.add!.mockImplementation(async (added: NativeItem | NativeItem[], at?: number) => {
    const list = Array.isArray(added) ? added : [added];
    const position = at ?? nativeQueue.length;
    nativeQueue = [...nativeQueue.slice(0, position), ...list, ...nativeQueue.slice(position)];
  });
  player.getQueue!.mockImplementation(async () => nativeQueue);
  player.remove!.mockImplementation(async (index: number) => {
    nativeQueue = nativeQueue.filter((_, i) => i !== index);
  });
  player.removeUpcomingTracks!.mockImplementation(async () => {
    nativeQueue = nativeQueue.slice(0, nativeIndex + 1);
  });
  player.getActiveTrack!.mockImplementation(async () => nativeQueue[nativeIndex]);
  player.getActiveTrackIndex!.mockImplementation(async () => nativeIndex);
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
const E = track('e');

function nativeKeys(): string[] {
  return nativeQueue.map((item) => item.id);
}

afterEach(() => {
  for (const name of [
    'add',
    'remove',
    'getQueue',
    'removeUpcomingTracks',
    'getActiveTrack',
    'getActiveTrackIndex',
  ]) {
    player[name]!.mockReset();
  }
  mockedFetchUrls.mockReset();
  mockedFetchUrls.mockImplementation(async () => []);
  useQueueStore.getState().clearQueue();
});

describe('reorderUpcomingNative applies the live store queue at the lock (#2703)', () => {
  it('keeps native equal to the store when the user skips back during the slide', async () => {
    useQueueStore.getState().loadQueue([A, B, C, D], 1, null);
    modelNativePlayer([A, B, C, D], 1);
    const release = deferredUrls();

    const slide = reorderUpcomingNative([C, D]);
    await flush();
    nativeIndex = 0;
    useQueueStore.getState().syncCurrentIndex(0, trackKey(A));
    release();
    await slide;

    expect(nativeKeys()).toEqual(orderedQueueTracks(useQueueStore.getState()).map(trackKey));
  });

  it('keeps a track played next during the rebuild', async () => {
    useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
    modelNativePlayer([A, B, C, D], 0);
    const release = deferredUrls();

    const slide = reorderUpcomingNative([B, C, D]);
    await flush();
    useQueueStore.getState().playNext(E);
    const insert = insertNativeTrackNext(E, 1);
    release();
    await Promise.all([slide, insert]);

    expect(nativeKeys()).toEqual([A, E, B, C, D].map(trackKey));
  });

  it('does not re-add a track removed during the rebuild', async () => {
    useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
    modelNativePlayer([A, B, C, D], 0);
    const release = deferredUrls();

    const slide = reorderUpcomingNative([B, C, D]);
    await flush();
    useQueueStore.getState().removeFromQueue(2);
    await TrackPlayer.remove(2);
    release();
    await slide;

    expect(nativeKeys()).toEqual([A, B, D].map(trackKey));
  });
});
