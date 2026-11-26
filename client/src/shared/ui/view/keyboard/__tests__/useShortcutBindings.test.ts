import { renderHook } from '@testing-library/react-native';
import { createElement, type ReactElement, type ReactNode } from 'react';
import { asTrackId } from '@shared/catalog';
import { PlaybackContext } from '@shared/playback';
import type { PlaybackContextValue } from '@shared/playback';
import { makeIdlePlayback } from '@testing/factories';

import { useShortcutBindings } from '@shared/ui';

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
  return makeIdlePlayback({
    status: 'paused',
    track: {
      source: { kind: 'library', trackId: asTrackId('track-1') },
      title: 'Test Track',
      artist: 'Test Artist',
      artworkUrl: null,
    },
    positionMs: 30_000,
    durationMs: 200_000,
    ...overrides,
  });
}

function wrapperFor(controls: PlaybackContextValue) {
  return function Wrapper({ children }: { children: ReactNode }): ReactElement {
    return createElement(PlaybackContext.Provider, { value: controls }, children);
  };
}

const focusSearch = jest.fn();

beforeEach(() => {
  focusSearch.mockClear();
});

function setup(win: FakeWindow, controls: PlaybackContextValue) {
  return renderHook(() => useShortcutBindings(controls, win, focusSearch), {
    wrapper: wrapperFor(controls),
  });
}

describe('useShortcutBindings', () => {
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
});

describe('useShortcutBindings caller edges', () => {
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

    const preventDefault = win.dispatch({ key: '/', target: { tagName: 'INPUT' } });

    expect(preventDefault).not.toHaveBeenCalled();
    expect(focusSearch).not.toHaveBeenCalled();
  });

  it('asks for the search to be focused on a slash outside a field', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());

    win.dispatch({ key: '/' });

    expect(focusSearch).toHaveBeenCalledTimes(1);
  });

  it('asks the latest focus callback after it changes between renders', () => {
    const win = createFakeWindow();
    const first = jest.fn();
    const second = jest.fn();
    let current = first;
    const controls = controlsFixture();
    const { rerender } = renderHook(() => useShortcutBindings(controls, win, current), {
      wrapper: wrapperFor(controls),
    });

    current = second;
    rerender({});
    win.dispatch({ key: '/' });

    expect(second).toHaveBeenCalledTimes(1);
    expect(first).not.toHaveBeenCalled();
    expect(win.addEventListener).toHaveBeenCalledTimes(1);
  });

  it('leaves arrow keys to move the caret inside an input', () => {
    const win = createFakeWindow();
    const controls = controlsFixture();
    setup(win, controls);

    const left = win.dispatch({ key: 'ArrowLeft', target: { tagName: 'INPUT' } });
    const shiftRight = win.dispatch({
      key: 'ArrowRight',
      shiftKey: true,
      target: { tagName: 'INPUT' },
    });

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
    expect(focusSearch).not.toHaveBeenCalled();
  });

  it('prevents the page from scrolling on the arrow and slash keys it handles', () => {
    const win = createFakeWindow();
    setup(win, controlsFixture());

    expect(win.dispatch({ key: 'ArrowLeft' })).toHaveBeenCalledTimes(1);
    expect(win.dispatch({ key: 'ArrowRight', shiftKey: true })).toHaveBeenCalledTimes(1);
    expect(win.dispatch({ key: '/' })).toHaveBeenCalledTimes(1);
  });

  it('toggles with the latest playback status after it changes', () => {
    const win = createFakeWindow();
    let current = controlsFixture({ status: 'paused' });
    const { rerender } = renderHook(() => useShortcutBindings(current, win, focusSearch), {
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
    const { rerender } = renderHook(() => useShortcutBindings(current, win, focusSearch), {
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
    const { rerender } = renderHook(() => useShortcutBindings(current, win, focusSearch), {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(PlaybackContext.Provider, { value: current }, children),
    });
    for (const positionMs of [31_000, 32_000, 33_000]) {
      current = controlsFixture({ positionMs });
      rerender({});
    }

    expect(win.addEventListener.mock.calls.length - win.removeEventListener.mock.calls.length).toBe(
      1,
    );
  });

  it('ignores a target that cannot listen', () => {
    const controls = controlsFixture({ status: 'playing' });
    const removeEventListener = jest.fn();
    const target = { removeEventListener };
    expect(() =>
      renderHook(() => useShortcutBindings(controls, target, focusSearch), {
        wrapper: wrapperFor(controls),
      }).unmount(),
    ).not.toThrow();
    expect(removeEventListener).toHaveBeenCalledTimes(0);
  });

  it('refuses a look-alike target with no addEventListener', () => {
    const controls = controlsFixture({ status: 'playing' });
    const target = {} as Window;
    expect(() =>
      renderHook(() => useShortcutBindings(controls, target, focusSearch), {
        wrapper: wrapperFor(controls),
      }).unmount(),
    ).not.toThrow();
  });

  it('adds one keydown listener on mount and removes one on unmount', () => {
    const win = createFakeWindow();
    const { unmount } = setup(win, controlsFixture());
    expect(win.addEventListener).toHaveBeenCalledTimes(1);
    expect(win.addEventListener).toHaveBeenCalledWith('keydown', expect.any(Function));
    expect(win.removeEventListener).not.toHaveBeenCalled();
    unmount();
    expect(win.removeEventListener).toHaveBeenCalledTimes(1);
  });

  it('rebinds to the new playback when it is swapped between renders', () => {
    const win = createFakeWindow();
    const a = controlsFixture({ status: 'paused' });
    const b = controlsFixture({ status: 'paused' });
    let current = a;
    const { rerender } = renderHook(() => useShortcutBindings(current, win, focusSearch), {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(PlaybackContext.Provider, { value: current }, children),
    });

    current = b;
    rerender({});
    win.dispatch({ key: ' ' });

    expect(win.addEventListener).toHaveBeenCalledTimes(2);
    expect(win.removeEventListener).toHaveBeenCalledTimes(1);
    expect(b.resume).toHaveBeenCalledTimes(1);
    expect(a.resume).not.toHaveBeenCalled();
  });
});
