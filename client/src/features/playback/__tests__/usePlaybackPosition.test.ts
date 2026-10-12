import { act, renderHook } from '@testing-library/react-native';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';

import { usePlaybackPosition } from '@features/playback/hooks/usePlaybackPosition';

const { __player } = jest.requireMock('react-native-track-player');

type AppStateHandler = (state: string) => void;

const mockAppState = { currentState: 'active', handlers: [] as AppStateHandler[] };

jest.mock('react-native/Libraries/AppState/AppState', () => ({
  default: {
    get currentState() {
      return mockAppState.currentState;
    },
    addEventListener: (_type: string, handler: AppStateHandler) => {
      mockAppState.handlers.push(handler);
      return {
        remove: () => {
          mockAppState.handlers = mockAppState.handlers.filter((h) => h !== handler);
        },
      };
    },
  },
}));

function setForeground(foreground: boolean): void {
  mockAppState.currentState = foreground ? 'active' : 'background';
  act(() => {
    for (const handler of [...mockAppState.handlers]) handler(mockAppState.currentState);
  });
}

const track: PlaybackTrack = {
  source: { kind: 'preview', previewUrl: 'https://cdn.example/1.mp3' },
  title: 'Track 1',
  artist: 'An Artist',
  artworkUrl: null,
  durationSeconds: 200,
};

beforeEach(() => {
  __player.reset();
  mockAppState.handlers = [];
  mockAppState.currentState = 'active';
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

    setForeground(false);
    __player.setProgress({ position: 0 });
    rerender({});

    expect(result.current.positionMs).toBe(10_000);
    expect(result.current.livePositionMs).toBe(0);

    setForeground(true);
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
