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

describe('useKeyboardShortcuts caller edges', () => {
  it.each(['TEXTAREA', 'SELECT'])('ignores Space typed into a %s', (tagName) => {
    const win = createFakeWindow();
    const controls = controlsFixture({ status: 'playing' });
    setup(win, controls);

    const preventDefault = win.dispatch({ key: ' ', target: { tagName } });

    expect(preventDefault).not.toHaveBeenCalled();
    expect(controls.pause).not.toHaveBeenCalled();
    expect(controls.resume).not.toHaveBeenCalled();
  });

  it('types a slash into an input instead of jumping to Discover search', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());
    registerSearchFocus(jest.fn())();
    const focus = jest.fn();
    const unregister = registerSearchFocus(focus);

    const preventDefault = win.dispatch({ key: '/', target: { tagName: 'INPUT' } });

    expect(preventDefault).not.toHaveBeenCalled();
    expect(focus).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
    unregister();
  });

  it('leaves arrow keys to move the caret inside an input', () => {
    const win = createFakeWindow();
    const controls = controlsFixture();
    setup(win, controls);

    const left = win.dispatch({ key: 'ArrowLeft', target: { tagName: 'INPUT' } });
    const shiftRight = win.dispatch({ key: 'ArrowRight', shiftKey: true, target: { tagName: 'INPUT' } });

    expect(left).not.toHaveBeenCalled();
    expect(shiftRight).not.toHaveBeenCalled();
    expect(controls.seekTo).not.toHaveBeenCalled();
    expect(controls.skipNext).not.toHaveBeenCalled();
  });

  it('leaves Alt+ArrowLeft to the browser as history back', () => {
    const win = createFakeWindow();
    const controls = controlsFixture();
    setup(win, controls);

    const preventDefault = win.dispatch({ key: 'ArrowLeft', altKey: true });

    expect(preventDefault).not.toHaveBeenCalled();
    expect(controls.seekTo).not.toHaveBeenCalled();
    expect(controls.skipPrevious).not.toHaveBeenCalled();
  });

  it('leaves Cmd+Shift+ArrowRight and Ctrl+/ to the browser', () => {
    const win = createFakeWindow();
    const controls = controlsFixture();
    setup(win, controls);

    const skip = win.dispatch({ key: 'ArrowRight', shiftKey: true, metaKey: true });
    const slash = win.dispatch({ key: '/', ctrlKey: true });

    expect(skip).not.toHaveBeenCalled();
    expect(slash).not.toHaveBeenCalled();
    expect(controls.skipNext).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it('prevents the page from scrolling on the arrow and slash keys it handles', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());
    const unregister = registerSearchFocus(jest.fn());

    expect(win.dispatch({ key: 'ArrowLeft' })).toHaveBeenCalledTimes(1);
    expect(win.dispatch({ key: 'ArrowRight', shiftKey: true })).toHaveBeenCalledTimes(1);
    expect(win.dispatch({ key: '/' })).toHaveBeenCalledTimes(1);
    unregister();
  });

  it('toggles with the latest playback status after it changes', () => {
    const win = createFakeWindow();
    let current = controlsFixture({ status: 'paused' });
    const { rerender } = renderHook(() => useKeyboardShortcuts(win as unknown as Window), {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(PlaybackContext.Provider, { value: current }, children),
    });

    current = controlsFixture({ status: 'playing' });
    rerender({});
    win.dispatch({ key: ' ' });

    expect(current.pause).toHaveBeenCalledTimes(1);
    expect(current.resume).not.toHaveBeenCalled();
  });

  it('seeks from the latest position after playback advances', () => {
    const win = createFakeWindow();
    let current = controlsFixture({ positionMs: 30_000 });
    const { rerender } = renderHook(() => useKeyboardShortcuts(win as unknown as Window), {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(PlaybackContext.Provider, { value: current }, children),
    });

    current = controlsFixture({ positionMs: 90_000 });
    rerender({});
    win.dispatch({ key: 'ArrowLeft' });

    expect(current.seekTo).toHaveBeenCalledWith(80_000);
  });

  it('keeps a single listener across rerenders', () => {
    const win = createFakeWindow();
    let current = controlsFixture({ positionMs: 30_000 });
    const { rerender } = renderHook(() => useKeyboardShortcuts(win as unknown as Window), {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(PlaybackContext.Provider, { value: current }, children),
    });
    for (const positionMs of [31_000, 32_000, 33_000]) {
      current = controlsFixture({ positionMs });
      rerender({});
    }

    expect(win.addEventListener.mock.calls.length - win.removeEventListener.mock.calls.length).toBe(1);
  });

  it('navigates to Discover again once the Discover search has unregistered', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());
    const focus = jest.fn();
    const unregister = registerSearchFocus(focus);
    unregister();

    win.dispatch({ key: '/' });

    expect(focus).not.toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith('/discover');
    registerSearchFocus(jest.fn())();
  });

  it('focuses the Discover search only once after navigating to it', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());
    win.dispatch({ key: '/' });

    const first = jest.fn();
    registerSearchFocus(first)();
    const second = jest.fn();
    const unregister = registerSearchFocus(second);

    expect(first).toHaveBeenCalledTimes(1);
    expect(second).not.toHaveBeenCalled();
    unregister();
  });

  it('does nothing and does not throw on native with no target', () => {
    const controls = controlsFixture({ status: 'playing' });
    expect(() =>
      renderHook(() => useKeyboardShortcuts(), { wrapper: wrapperFor(controls) }).unmount(),
    ).not.toThrow();
  });
});
