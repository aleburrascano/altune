import TrackPlayer from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import {
  forgetAllSwaps,
  swapUpcomingToLocal,
  wasSwappedToLocal,
} from '../audioPrefetch';
import { usePlaybackErrorStore } from '../playbackErrorStore';

const player = TrackPlayer as unknown as {
  getQueue: jest.Mock;
  getActiveTrackIndex: jest.Mock;
  remove: jest.Mock;
  add: jest.Mock;
};

function libraryTrack(trackId: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId(trackId) },
    title: `Track ${trackId}`,
    artist: 'An Artist',
    artworkUrl: null,
  };
}

function previewTrack(previewUrl: string): PlaybackTrack {
  return {
    source: { kind: 'preview', previewUrl },
    title: 'A Preview',
    artist: 'An Artist',
    artworkUrl: null,
  };
}

beforeEach(() => {
  forgetAllSwaps();
  usePlaybackErrorStore.getState().clear();
});

describe('swapUpcomingToLocal — replacing an upcoming native slot with a cached file', () => {
  it('does nothing when the track is not an upcoming slot', async () => {
    const track = libraryTrack('trk-1');
    player.getQueue.mockResolvedValueOnce([{ id: 'library:other' }]);

    await swapUpcomingToLocal(track, 'file:///cache/trk-1.mp3');

    expect(player.remove).not.toHaveBeenCalled();
    expect(player.add).not.toHaveBeenCalled();
    expect(wasSwappedToLocal('trk-1')).toBe(false);
  });

  it('removes the upcoming slot and refills it with the local file, marking it swapped', async () => {
    const track = libraryTrack('trk-1');
    player.getQueue.mockResolvedValueOnce([{ id: 'library:active' }, { id: trackKey(track) }]);

    await swapUpcomingToLocal(track, 'file:///cache/trk-1.mp3');

    expect(player.remove).toHaveBeenCalledWith(1);
    expect(player.add.mock.calls[0][0]).toMatchObject({ url: 'file:///cache/trk-1.mp3' });
    expect(player.add.mock.calls[0][1]).toBe(1);
    expect(wasSwappedToLocal('trk-1')).toBe(true);
  });

  it('swaps a slot sitting at index 0 when there is no active track yet', async () => {
    const track = libraryTrack('trk-1');
    player.getQueue.mockResolvedValueOnce([{ id: trackKey(track) }]);

    await swapUpcomingToLocal(track, 'file:///cache/trk-1.mp3');

    expect(player.remove).toHaveBeenCalledWith(0);
    expect(player.add.mock.calls[0][1]).toBe(0);
  });

  it('falls through to a streaming re-add when the local re-add fails, without surfacing an error', async () => {
    const track = previewTrack('https://cdn.example/p.mp3');
    player.getQueue.mockResolvedValueOnce([{ id: 'library:active' }, { id: trackKey(track) }]);
    player.add.mockRejectedValueOnce(new Error('local add failed'));

    await swapUpcomingToLocal(track, 'file:///cache/p.mp3');

    expect(player.add).toHaveBeenCalledTimes(2);
    expect(usePlaybackErrorStore.getState().key).toBeNull();
  });

  it('surfaces a PlaybackError only when the streaming re-add also fails', async () => {
    const track = previewTrack('https://cdn.example/p.mp3');
    player.getQueue.mockResolvedValueOnce([{ id: 'library:active' }, { id: trackKey(track) }]);
    player.add.mockRejectedValueOnce(new Error('local add failed'));
    player.add.mockRejectedValueOnce(new Error('streaming add failed'));

    await swapUpcomingToLocal(track, 'file:///cache/p.mp3');

    expect(usePlaybackErrorStore.getState().key).toBe(trackKey(track));
    expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
  });
});
