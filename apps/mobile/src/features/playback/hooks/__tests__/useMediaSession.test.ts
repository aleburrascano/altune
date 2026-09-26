import { act, renderHook } from '@testing-library/react-native';

import { usePlaybackRateStore } from '../../playbackRateStore';
import { libraryTrack } from '../../__tests__/fixtures';
import type { PlaybackContextValue } from '@shared/playback/types';
import { useMediaSession } from '../useMediaSession';

class FakeMediaSession {
  metadata: MediaMetadata | null = null;
  playbackState: MediaSessionPlaybackState = 'none';
  handlers = new Map<MediaSessionAction, MediaSessionActionHandler | null>();
  setPositionState = jest.fn();

  setActionHandler(action: MediaSessionAction, handler: MediaSessionActionHandler | null): void {
    this.handlers.set(action, handler);
  }

  fire(action: MediaSessionAction, details: Partial<MediaSessionActionDetails> = {}): void {
    this.handlers.get(action)?.({ action, ...details } as MediaSessionActionDetails);
  }
}

function fakeMetadataInit(this: MediaMetadata, init: MediaMetadataInit) {
  Object.assign(this, init);
}

function playbackFixture(overrides: Partial<PlaybackContextValue> = {}): PlaybackContextValue {
  return {
    status: 'playing',
    track: libraryTrack(),
    positionMs: 0,
    durationMs: 200_000,
    errorMessage: null,
    errorKind: null,
    play: jest.fn(),
    startQueue: jest.fn(),
    skipToQueueIndex: jest.fn(),
    reorderUpcoming: jest.fn(),
    appendToQueue: jest.fn(),
    insertNext: jest.fn(),
    skipNext: jest.fn(),
    skipPrevious: jest.fn(),
    removeQueueIndex: jest.fn(),
    pause: jest.fn(),
    resume: jest.fn(),
    seekTo: jest.fn(),
    setRate: jest.fn(),
    stop: jest.fn(),
    retry: jest.fn(),
    ...overrides,
  };
}

beforeEach(() => {
  usePlaybackRateStore.getState().setRate(1);
  (global as unknown as { MediaMetadata: unknown }).MediaMetadata = fakeMetadataInit;
});

describe('useMediaSession', () => {
  it('does nothing when the browser has no MediaSession support', () => {
    expect(() => renderHook(() => useMediaSession(playbackFixture(), undefined))).not.toThrow();
  });

  it('publishes the current track as media session metadata with artwork', () => {
    const session = new FakeMediaSession();
    const track = libraryTrack({ title: 'A Song', artist: 'A Band', artworkUrl: 'https://cdn/art.png' });

    renderHook(() => useMediaSession(playbackFixture({ track }), session as unknown as MediaSession));

    expect(session.metadata).toMatchObject({
      title: 'A Song',
      artist: 'A Band',
      artwork: [{ src: 'https://cdn/art.png', sizes: '512x512', type: '' }],
    });
  });

  it('omits artwork when the track has none', () => {
    const session = new FakeMediaSession();
    const track = libraryTrack({ artworkUrl: null });

    renderHook(() => useMediaSession(playbackFixture({ track }), session as unknown as MediaSession));

    expect(session.metadata).toMatchObject({ artwork: [] });
  });

  it.each([
    ['playing', 'playing'],
    ['paused', 'paused'],
    ['loading', 'paused'],
  ] as const)('reports the browser playback state %s for playback status %s', (status, expected) => {
    const session = new FakeMediaSession();

    renderHook(() => useMediaSession(playbackFixture({ status }), session as unknown as MediaSession));

    expect(session.playbackState).toBe(expected);
  });

  it('reports no active session once the track clears', () => {
    const session = new FakeMediaSession();

    renderHook(() => useMediaSession(playbackFixture({ track: null }), session as unknown as MediaSession));

    expect(session.metadata).toBeNull();
    expect(session.playbackState).toBe('none');
  });

  it('sets the position state from position, duration and playback rate', () => {
    const session = new FakeMediaSession();
    usePlaybackRateStore.getState().setRate(1.5);

    renderHook(() =>
      useMediaSession(playbackFixture({ positionMs: 42_000, durationMs: 200_000 }), session as unknown as MediaSession),
    );

    expect(session.setPositionState).toHaveBeenCalledWith({ duration: 200, position: 42, playbackRate: 1.5 });
  });

  it('skips setPositionState while the duration is not finite', () => {
    const session = new FakeMediaSession();

    renderHook(() =>
      useMediaSession(playbackFixture({ durationMs: Number.POSITIVE_INFINITY }), session as unknown as MediaSession),
    );

    expect(session.setPositionState).not.toHaveBeenCalled();
  });

  it('routes play and pause handlers to the playback controls', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture();

    renderHook(() => useMediaSession(playback, session as unknown as MediaSession));
    act(() => session.fire('play'));
    act(() => session.fire('pause'));

    expect(playback.resume).toHaveBeenCalledTimes(1);
    expect(playback.pause).toHaveBeenCalledTimes(1);
  });

  it('routes previoustrack and nexttrack handlers to skip controls', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture();

    renderHook(() => useMediaSession(playback, session as unknown as MediaSession));
    act(() => session.fire('previoustrack'));
    act(() => session.fire('nexttrack'));

    expect(playback.skipPrevious).toHaveBeenCalledTimes(1);
    expect(playback.skipNext).toHaveBeenCalledTimes(1);
  });

  it('routes seekto to the requested position', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture();

    renderHook(() => useMediaSession(playback, session as unknown as MediaSession));
    act(() => session.fire('seekto', { seekTime: 30 }));

    expect(playback.seekTo).toHaveBeenCalledWith(30_000);
  });

  it('routes seekbackward and seekforward by the default 10 second step', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture({ positionMs: 20_000 });

    renderHook(() => useMediaSession(playback, session as unknown as MediaSession));
    act(() => session.fire('seekbackward', {}));
    act(() => session.fire('seekforward', {}));

    expect(playback.seekTo).toHaveBeenNthCalledWith(1, 10_000);
    expect(playback.seekTo).toHaveBeenNthCalledWith(2, 30_000);
  });

  it('clamps seekbackward to zero and honours a custom seek offset', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture({ positionMs: 5_000 });

    renderHook(() => useMediaSession(playback, session as unknown as MediaSession));
    act(() => session.fire('seekbackward', { seekOffset: 20 }));

    expect(playback.seekTo).toHaveBeenCalledWith(0);
  });

  it('clears its action handlers on unmount', () => {
    const session = new FakeMediaSession();
    const { unmount } = renderHook(() => useMediaSession(playbackFixture(), session as unknown as MediaSession));
    expect(session.handlers.get('play')).not.toBeNull();

    unmount();

    expect([...session.handlers.values()].every((handler) => handler === null)).toBe(true);
  });
});

describe('useMediaSession from the browser media hub', () => {
  const asSession = (s: FakeMediaSession) => s as unknown as MediaSession;

  it('republishes metadata when the track changes', () => {
    const session = new FakeMediaSession();
    const { rerender } = renderHook((p: PlaybackContextValue) => useMediaSession(p, asSession(session)), {
      initialProps: playbackFixture({ track: libraryTrack({ title: 'First', artist: 'One' }) }),
    });

    rerender(
      playbackFixture({ track: libraryTrack({ title: 'Second', artist: 'Two', artworkUrl: 'https://cdn/b.png' }) }),
    );

    expect(session.metadata).toMatchObject({ title: 'Second', artist: 'Two', artwork: [{ src: 'https://cdn/b.png' }] });
  });

  it('seeks forward from the latest position after the position advances', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture({ positionMs: 0 });
    const { rerender } = renderHook((p: PlaybackContextValue) => useMediaSession(p, asSession(session)), {
      initialProps: playback,
    });

    rerender({ ...playback, positionMs: 60_000 });
    act(() => session.fire('seekforward', {}));

    expect(playback.seekTo).toHaveBeenCalledWith(70_000);
  });

  it('routes a media key to the latest playback controls after they change', () => {
    const session = new FakeMediaSession();
    const first = playbackFixture();
    const second = playbackFixture();
    const { rerender } = renderHook((p: PlaybackContextValue) => useMediaSession(p, asSession(session)), {
      initialProps: first,
    });

    rerender(second);
    act(() => session.fire('nexttrack'));

    expect(second.skipNext).toHaveBeenCalledTimes(1);
    expect(first.skipNext).not.toHaveBeenCalled();
  });

  it('seeks by the offset the browser asks for instead of the default step', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture({ positionMs: 20_000 });

    renderHook(() => useMediaSession(playback, asSession(session)));
    act(() => session.fire('seekbackward', { seekOffset: 5 }));
    act(() => session.fire('seekforward', { seekOffset: 30 }));

    expect(playback.seekTo).toHaveBeenNthCalledWith(1, 15_000);
    expect(playback.seekTo).toHaveBeenNthCalledWith(2, 50_000);
  });

  it('ignores a seekto that carries no seek time', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture();

    renderHook(() => useMediaSession(playback, asSession(session)));
    act(() => session.fire('seekto', {}));

    expect(playback.seekTo).not.toHaveBeenCalled();
  });

  it.each([0, Number.NaN, Number.NEGATIVE_INFINITY])('skips setPositionState for a duration of %s', (durationMs) => {
    const session = new FakeMediaSession();

    renderHook(() => useMediaSession(playbackFixture({ durationMs, positionMs: 0 }), asSession(session)));

    expect(session.setPositionState).not.toHaveBeenCalled();
  });

  it('survives a position past the duration, which browsers reject with a TypeError', () => {
    const session = new FakeMediaSession();
    session.setPositionState.mockImplementation((s: MediaPositionState) => {
      if ((s.position ?? 0) > (s.duration ?? 0)) throw new TypeError('position exceeds duration');
    });

    expect(() =>
      renderHook(() =>
        useMediaSession(playbackFixture({ positionMs: 201_000, durationMs: 200_000 }), asSession(session)),
      ),
    ).not.toThrow();
  });

  it('does not report every position tick to the browser', () => {
    const session = new FakeMediaSession();
    const playback = playbackFixture({ positionMs: 0 });
    const { rerender } = renderHook((p: PlaybackContextValue) => useMediaSession(p, asSession(session)), {
      initialProps: playback,
    });

    for (let ms = 50; ms <= 500; ms += 50) rerender({ ...playback, positionMs: ms });

    expect(session.setPositionState.mock.calls.length).toBeLessThan(11);
  });
});
