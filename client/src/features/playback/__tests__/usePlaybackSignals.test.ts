import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { signInTestUser } from '@testing/signInTestUser';

import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';

import { usePlaybackSignals } from '@features/playback/hooks/usePlaybackSignals';

import { previewTrack } from '@features/playback/__tests__/fixtures';

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS_ROUTE = 'POST /v1/discovery/events';
const HTTP_ACCEPTED = 202;

interface WrapperProps {
  children: React.ReactNode;
}

function wrapper({ children }: WrapperProps) {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return React.createElement(QueryClientProvider, { client: queryClient }, children);
}

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
  renderHook(() => usePlaybackSignals({ track: null, positionMs: 0, durationMs: 0 }), { wrapper });
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

async function recordedTypes(): Promise<string[]> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  return (__http.requests as { path: string; body: string }[])
    .filter((request) => request.path === '/v1/discovery/events')
    .map((request) => (JSON.parse(request.body) as { type: string }).type);
}

describe('usePlaybackSignals — one telemetry event per track that stops playing', () => {
  beforeEach(async () => {
    await signInTestUser();
    __http.reset();
    __http.reply(EVENTS_ROUTE, { status: HTTP_ACCEPTED });
    __player.reset();
  });

  it('drops the queue-ended event for the track it already reported', async () => {
    listenTo([first, second]);

    emitActiveTrackChanged(0);
    emitQueueEnded(0);

    expect(await recordedTypes()).toEqual(['skip']);
  });

  it('reports the queue-ended event for a track it has not reported', async () => {
    listenTo([first, second]);

    emitActiveTrackChanged(0);
    emitQueueEnded(1);

    expect(await recordedTypes()).toEqual(['skip', 'completed']);
  });

  it('does not emit play for a new track from the previous track position', async () => {
    useQueueStore.setState({ tracks: [first, second], playOrder: [0, 1], source: null });
    const { rerender } = renderHook(
      (props: { track: PlaybackTrack; positionMs: number }) =>
        usePlaybackSignals({ ...props, durationMs: 200000 }),
      { initialProps: { track: first, positionMs: 45000 }, wrapper },
    );

    rerender({ track: second, positionMs: 45000 });

    expect(await recordedTypes()).toEqual([]);
  });

  it('emits play once when the new track own position crosses the threshold', async () => {
    useQueueStore.setState({ tracks: [first, second], playOrder: [0, 1], source: null });
    const { rerender } = renderHook(
      (props: { track: PlaybackTrack; positionMs: number }) =>
        usePlaybackSignals({ ...props, durationMs: 200000 }),
      { initialProps: { track: first, positionMs: 45000 }, wrapper },
    );

    rerender({ track: second, positionMs: 45000 });
    rerender({ track: second, positionMs: 1000 });
    rerender({ track: second, positionMs: 46000 });
    rerender({ track: second, positionMs: 47000 });

    expect(await recordedTypes()).toEqual(['play']);
  });

  it('reports queue-ended completed for a track skipped in an earlier run', async () => {
    listenTo([first, second]);

    emitActiveTrackChanged(0);
    emitQueueEnded(0);
    emitQueueEnded(0);

    expect(await recordedTypes()).toEqual(['skip', 'completed']);
  });
});
