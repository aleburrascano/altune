import { act, renderHook } from '@testing-library/react-native';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { usePlaybackSignals } from '../native/usePlaybackSignals';

import { previewTrack } from './fixtures';

const mockMutate = jest.fn();

jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: mockMutate }),
}));

const { __player } = jest.requireMock('react-native-track-player');

const first = previewTrack({
  source: { kind: 'preview', previewUrl: 'https://cdn.example/1.mp3' },
  title: 'First',
});
const second = previewTrack({
  source: { kind: 'preview', previewUrl: 'https://cdn.example/2.mp3' },
  title: 'Second',
});

function listenTo(tracks: readonly PlaybackTrack[]): void {
  useQueueStore.setState({ tracks, playOrder: tracks.map((_, i) => i), source: null });
  renderHook(() => usePlaybackSignals({ track: null, positionMs: 0, durationMs: 0 }));
}

function emitActiveTrackChanged(lastIndex: number): void {
  act(() => {
    __player.emit('PlaybackActiveTrackChanged', {
      type: 'PlaybackActiveTrackChanged',
      lastIndex,
      lastPosition: 5,
    });
  });
}

function emitQueueEnded(track: number): void {
  act(() => {
    __player.emit('PlaybackQueueEnded', { type: 'PlaybackQueueEnded', track, position: 5 });
  });
}

function recordedTypes(): string[] {
  return mockMutate.mock.calls.map(([event]) => event.type);
}

describe('usePlaybackSignals — one telemetry event per track that stops playing', () => {
  beforeEach(() => {
    __player.reset();
    mockMutate.mockClear();
  });

  it('drops the queue-ended event for the track it already reported', () => {
    listenTo([first, second]);

    emitActiveTrackChanged(0);
    emitQueueEnded(0);

    expect(recordedTypes()).toEqual(['skip']);
  });

  it('reports the queue-ended event for a track it has not reported', () => {
    listenTo([first, second]);

    emitActiveTrackChanged(0);
    emitQueueEnded(1);

    expect(recordedTypes()).toEqual(['skip', 'completed']);
  });

  it('does not emit play for a new track from the previous track position', () => {
    useQueueStore.setState({ tracks: [first, second], playOrder: [0, 1], source: null });
    const { rerender } = renderHook(
      (props: { track: PlaybackTrack; positionMs: number }) =>
        usePlaybackSignals({ ...props, durationMs: 200000 }),
      { initialProps: { track: first, positionMs: 45000 } },
    );

    rerender({ track: second, positionMs: 45000 });

    expect(recordedTypes()).toEqual([]);
  });

  it('emits play once when the new track own position crosses the threshold', () => {
    useQueueStore.setState({ tracks: [first, second], playOrder: [0, 1], source: null });
    const { rerender } = renderHook(
      (props: { track: PlaybackTrack; positionMs: number }) =>
        usePlaybackSignals({ ...props, durationMs: 200000 }),
      { initialProps: { track: first, positionMs: 45000 } },
    );

    rerender({ track: second, positionMs: 45000 });
    rerender({ track: second, positionMs: 1000 });
    rerender({ track: second, positionMs: 46000 });
    rerender({ track: second, positionMs: 47000 });

    expect(recordedTypes()).toEqual(['play']);
  });

  it('reports queue-ended completed for a track skipped in an earlier run', () => {
    listenTo([first, second]);

    emitActiveTrackChanged(0);
    emitQueueEnded(0);
    emitQueueEnded(0);

    expect(recordedTypes()).toEqual(['skip', 'completed']);
  });
});
