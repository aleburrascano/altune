import { trackKey } from '@shared/playback/trackKey';

import { repairActiveToStreaming } from '../nativeTrackSwap';
import { usePlaybackErrorStore } from '../playbackErrorStore';

import { previewTrack } from './fixtures';

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
