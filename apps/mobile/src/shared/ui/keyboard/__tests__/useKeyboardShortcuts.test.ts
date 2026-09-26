import { renderHook } from '@testing-library/react-native';
import { createElement, type ReactElement, type ReactNode } from 'react';

import { asTrackId } from '@shared/api-client/ids';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

import { registerSearchFocus, useKeyboardShortcuts } from '../useKeyboardShortcuts';

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
}));

interface FakeWindow {
  readonly addEventListener: jest.Mock;
  readonly removeEventListener: jest.Mock;
  dispatch(event: FakeKeyboardEvent): jest.Mock;
  hasListener(): boolean;
}

interface FakeKeyboardEvent {
  key: string;
  shiftKey?: boolean;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
  target?: unknown;
}

function createFakeWindow(): FakeWindow {
  let handler: ((event: unknown) => void) | null = null;
  const addEventListener = jest.fn((_type: string, listener: (event: unknown) => void) => {
    handler = listener;
  });
  const removeEventListener = jest.fn(() => {
    handler = null;
  });
  return {
    addEventListener,
    removeEventListener,
    dispatch(event) {
      const preventDefault = jest.fn();
      handler?.({
        shiftKey: false,
        ctrlKey: false,
        metaKey: false,
        altKey: false,
        target: null,
        ...event,
        preventDefault,
      });
      return preventDefault;
    },
    hasListener: () => handler !== null,
  };
}

function controlsFixture(overrides: Partial<PlaybackContextValue> = {}): PlaybackContextValue {
  return {
    status: 'paused',
    track: {
      source: { kind: 'library', trackId: asTrackId('track-1') },
      title: 'Test Track',
      artist: 'Test Artist',
      artworkUrl: null,
    },
    positionMs: 30_000,
    durationMs: 200_000,
    errorMessage: null,
    errorKind: null,
    play: jest.fn(),
    startQueue: jest.fn(),
    skipToQueueIndex: jest.fn(),
    reorderUpcoming: jest.fn(),
    appendToQueue: jest.fn(),
    insertNext: jest.fn(),
    skipNext: jest.fn().mockResolvedValue(undefined),
    skipPrevious: jest.fn().mockResolvedValue(undefined),
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

function wrapperFor(controls: PlaybackContextValue) {
  return function Wrapper({ children }: { children: ReactNode }): ReactElement {
    return createElement(PlaybackContext.Provider, { value: controls }, children);
  };
}

function setup(win: FakeWindow, controls: PlaybackContextValue) {
  return renderHook(() => useKeyboardShortcuts(win as unknown as Window), {
    wrapper: wrapperFor(controls),
  });
}

beforeEach(() => {
  mockPush.mockClear();
});

describe('useKeyboardShortcuts', () => {
  it('attaches a single keydown listener and removes it on unmount', () => {
    const win = createFakeWindow();
    const { unmount } = setup(win, controlsFixture());

    expect(win.addEventListener).toHaveBeenCalledTimes(1);
    expect(win.addEventListener).toHaveBeenCalledWith('keydown', expect.any(Function));
    expect(win.hasListener()).toBe(true);

    unmount();

    expect(win.removeEventListener).toHaveBeenCalledTimes(1);
    expect(win.hasListener()).toBe(false);
  });

  it('toggles play/pause on Space depending on status', () => {
    const win = createFakeWindow();
    const playing = controlsFixture({ status: 'playing' });
    setup(win, playing);

    win.dispatch({ key: ' ' });

    expect(playing.pause).toHaveBeenCalledTimes(1);
    expect(playing.resume).not.toHaveBeenCalled();
  });

  it('resumes on Space when not playing', () => {
    const win = createFakeWindow();
    const paused = controlsFixture({ status: 'paused' });
    setup(win, paused);

    win.dispatch({ key: ' ' });

    expect(paused.resume).toHaveBeenCalledTimes(1);
    expect(paused.pause).not.toHaveBeenCalled();
  });

  it('seeks back 10s on ArrowLeft, clamped at zero', () => {
    const win = createFakeWindow();
    const controls = controlsFixture({ positionMs: 4_000 });
    setup(win, controls);

    win.dispatch({ key: 'ArrowLeft' });

    expect(controls.seekTo).toHaveBeenCalledWith(0);
  });

  it('seeks forward 10s on ArrowRight', () => {
    const win = createFakeWindow();
    const controls = controlsFixture({ positionMs: 30_000 });
    setup(win, controls);

    win.dispatch({ key: 'ArrowRight' });

    expect(controls.seekTo).toHaveBeenCalledWith(40_000);
  });

  it('skips to the previous track on Shift+ArrowLeft', () => {
    const win = createFakeWindow();
    const controls = controlsFixture();
    setup(win, controls);

    win.dispatch({ key: 'ArrowLeft', shiftKey: true });

    expect(controls.skipPrevious).toHaveBeenCalledTimes(1);
    expect(controls.seekTo).not.toHaveBeenCalled();
  });

  it('skips to the next track on Shift+ArrowRight', () => {
    const win = createFakeWindow();
    const controls = controlsFixture();
    setup(win, controls);

    win.dispatch({ key: 'ArrowRight', shiftKey: true });

    expect(controls.skipNext).toHaveBeenCalledTimes(1);
    expect(controls.seekTo).not.toHaveBeenCalled();
  });

  it('calls preventDefault only for keys it handles', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());

    const handled = win.dispatch({ key: ' ' });
    expect(handled).toHaveBeenCalledTimes(1);

    const unhandled = win.dispatch({ key: 'a' });
    expect(unhandled).not.toHaveBeenCalled();
  });

  it('ignores every binding when the event target is typing into a form field', () => {
    const win = createFakeWindow();
    const controls = controlsFixture();
    setup(win, controls);

    win.dispatch({ key: ' ', target: { tagName: 'INPUT' } });

    expect(controls.pause).not.toHaveBeenCalled();
    expect(controls.resume).not.toHaveBeenCalled();
  });

  it('lets a space typed into a contenteditable field through untouched', () => {
    const win = createFakeWindow();
    const controls = controlsFixture();
    setup(win, controls);

    const event = win.dispatch({ key: ' ', target: { isContentEditable: true } });

    expect(event).not.toHaveBeenCalled();
    expect(controls.pause).not.toHaveBeenCalled();
    expect(controls.resume).not.toHaveBeenCalled();
  });

  it('ignores bindings held with a browser modifier', () => {
    const win = createFakeWindow();
    const controls = controlsFixture({ status: 'playing' });
    setup(win, controls);

    win.dispatch({ key: ' ', ctrlKey: true });
    win.dispatch({ key: ' ', metaKey: true });
    win.dispatch({ key: ' ', altKey: true });

    expect(controls.pause).not.toHaveBeenCalled();
  });

  it('focuses the Discover search immediately when it is already registered', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());
    const focus = jest.fn();
    const unregister = registerSearchFocus(focus);

    win.dispatch({ key: '/' });

    expect(focus).toHaveBeenCalledTimes(1);
    expect(mockPush).not.toHaveBeenCalled();
    unregister();
  });

  it('navigates to Discover then focuses once it registers, when pressed elsewhere', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());

    win.dispatch({ key: '/' });

    expect(mockPush).toHaveBeenCalledWith('/discover');

    const focus = jest.fn();
    const unregister = registerSearchFocus(focus);

    expect(focus).toHaveBeenCalledTimes(1);
    unregister();
  });
});
