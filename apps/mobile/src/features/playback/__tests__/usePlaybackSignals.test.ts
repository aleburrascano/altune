// A queue that ends on its last track fires PlaybackActiveTrackChanged and
// PlaybackQueueEnded for the same track. Only one completion may reach telemetry,
// so the hook remembers the track it just reported by its canonical trackKey.

import { act, renderHook } from '@testing-library/react-native';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { usePlaybackSignals } from '../hooks/usePlaybackSignals';

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
});
