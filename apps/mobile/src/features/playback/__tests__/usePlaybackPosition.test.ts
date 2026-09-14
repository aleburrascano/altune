import { act, renderHook } from '@testing-library/react-native';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { usePlaybackPosition } from '../hooks/usePlaybackPosition';

const { __player } = jest.requireMock('react-native-track-player');

let mockForeground = true;
jest.mock('../hooks/useIsForeground', () => ({
  useIsForeground: () => mockForeground,
}));

const track: PlaybackTrack = {
  source: { kind: 'preview', previewUrl: 'https://cdn.example/1.mp3' },
  title: 'Track 1',
  artist: 'An Artist',
  artworkUrl: null,
  durationSeconds: 200,
};

beforeEach(() => {
  __player.reset();
  mockForeground = true;
  useQueueStore.getState().setResumePosition(0);
});

describe('usePlaybackPosition', () => {
  it('shows the resume point until native progress starts, then clears it', () => {
    act(() => useQueueStore.getState().setResumePosition(42_000));
    const { result, rerender } = renderHook(() => usePlaybackPosition(track));
    expect(result.current.positionMs).toBe(42_000);
    expect(result.current.livePositionMs).toBe(0);

    __player.setProgress({ position: 43 });
    rerender({});

    expect(result.current.positionMs).toBe(43_000);
    expect(useQueueStore.getState().resumePositionMs).toBe(0);
  });

  it('freezes the last foreground position while backgrounded', () => {
    __player.setProgress({ position: 10 });
    const { result, rerender } = renderHook(() => usePlaybackPosition(track));

    mockForeground = false;
    rerender({});
    __player.setProgress({ position: 0 });
    rerender({});

    expect(result.current.positionMs).toBe(10_000);
    expect(result.current.livePositionMs).toBe(0);

    mockForeground = true;
    rerender({});
    expect(result.current.positionMs).toBe(0);
  });

  it('prefers native duration and falls back to track metadata', () => {
    const { result, rerender } = renderHook(() => usePlaybackPosition(track));
    expect(result.current.durationMs).toBe(200_000);

    __player.setProgress({ duration: 180 });
    rerender({});
    expect(result.current.durationMs).toBe(180_000);
  });
});
