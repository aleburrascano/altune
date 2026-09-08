import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { repairActiveToStreaming } from '../audioPrefetch';
import { usePlaybackErrorStore } from '../playbackErrorStore';

const { __player } = jest.requireMock('react-native-track-player') as {
  __player: { failNext(method: string, error?: Error): void };
};

function previewTrack(previewUrl: string): PlaybackTrack {
  return {
    source: { kind: 'preview', previewUrl },
    title: 'A Title',
    artist: 'An Artist',
    artworkUrl: null,
  };
}

beforeEach(() => {
  usePlaybackErrorStore.getState().clear();
});

describe('repairActiveToStreaming — a native load failure surfaces a PlaybackError', () => {
  it('reports the failing track when TrackPlayer.load throws', async () => {
    const track = previewTrack('https://cdn.example/preview.mp3');
    __player.failNext('load', new Error('native load failed'));

    await repairActiveToStreaming(track);

    expect(usePlaybackErrorStore.getState().key).toBe(trackKey(track));
    expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
  });

  it('reports the failing track when TrackPlayer.play throws after a successful load', async () => {
    const track = previewTrack('https://cdn.example/preview.mp3');
    __player.failNext('play', new Error('native play failed'));

    await repairActiveToStreaming(track);

    expect(usePlaybackErrorStore.getState().key).toBe(trackKey(track));
    expect(usePlaybackErrorStore.getState().message).toBe('Could not load this track');
  });

  it('leaves the error store clean when load and play both succeed', async () => {
    const track = previewTrack('https://cdn.example/preview.mp3');

    await repairActiveToStreaming(track);

    expect(usePlaybackErrorStore.getState().key).toBeNull();
    expect(usePlaybackErrorStore.getState().message).toBeNull();
  });
});
