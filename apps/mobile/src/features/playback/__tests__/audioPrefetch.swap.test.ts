import TrackPlayer from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { forgetAllSwaps, swapUpcomingToLocal, wasSwappedToLocal } from '../nativeTrackSwap';
import { usePlaybackErrorStore } from '../playbackErrorStore';

import { libraryTrack, previewTrack } from './fixtures';

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
