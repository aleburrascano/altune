import { act, fireEvent, render, renderHook, screen } from '@testing-library/react-native';
import { createElement, type ReactElement, type ReactNode } from 'react';
import { Keyboard, Platform, TextInput } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { asTrackId } from '@shared/apiClient/ids';
import { PlaybackContext } from '@shared/playback/usePlayback';
import type { PlaybackContextValue } from '@shared/playback/playbackContextValue';
import { makeIdlePlayback } from '@testing/factories';
import { useKeyboardShortcuts } from '@shared/ui/keyboard/useKeyboardShortcuts';
import { DiscoverPortProvider } from '@features/discovery/ports/DiscoverPortProvider';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';
import { DiscoverScreen } from '@features/discovery/ui/DiscoverScreen';

jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

const mockPush = jest.fn();
const focusEffects: (() => (() => void) | void)[] = [];

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useFocusEffect: (effect: () => (() => void) | void) => {
    focusEffects.push(effect);
  },
}));

const port: DiscoverPort = {
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn().mockResolvedValue({ suggestions: [] }),
  listSearchHistory: jest.fn().mockResolvedValue({ items: [] }),
  recordSearchHistory: jest.fn().mockResolvedValue(undefined),
};

interface FakeWindow {
  readonly addEventListener: jest.Mock;
  readonly removeEventListener: jest.Mock;
  dispatch(key: string): void;
}

function createFakeWindow(): FakeWindow {
  let handler: ((event: unknown) => void) | null = null;
  return {
    addEventListener: jest.fn((_type: string, listener: (event: unknown) => void) => {
      handler = listener;
    }),
    removeEventListener: jest.fn(() => {
      handler = null;
    }),
    dispatch(key) {
      handler?.({
        key,
        shiftKey: false,
        ctrlKey: false,
        metaKey: false,
        altKey: false,
        target: null,
        preventDefault: jest.fn(),
      });
    },
  };
}

function playbackFixture(): PlaybackContextValue {
  return makeIdlePlayback({
    status: 'paused',
    track: {
      source: { kind: 'library', trackId: asTrackId('track-1') },
      title: 'Test Track',
      artist: 'Test Artist',
      artworkUrl: null,
    },
    durationMs: 200_000,
  });
}

function renderShortcuts(win: FakeWindow) {
  const controls = playbackFixture();
  return renderHook(() => useKeyboardShortcuts(controls, win as unknown as Window), {
    wrapper: ({ children }: { children: ReactNode }) =>
      createElement(PlaybackContext.Provider, { value: controls }, children),
  });
}

let focusSpy: jest.SpyInstance;

beforeEach(() => {
  focusSpy = jest.spyOn(TextInput.prototype, 'focus').mockImplementation(() => undefined);
  mockPush.mockClear();
  focusEffects.length = 0;
});

afterEach(() => {
  focusSpy.mockRestore();
});

function renderDiscover(): ReactElement {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={queryClient}>
      <PlaybackContext.Provider value={playbackFixture()}>
        <DiscoverPortProvider port={port}>
          <DiscoverScreen />
        </DiscoverPortProvider>
      </PlaybackContext.Provider>
    </QueryClientProvider>
  );
}

describe('DiscoverScreen registers its search focus only while visible', () => {
  it('unregisters on blur (not only on unmount), so a "/" pressed elsewhere after a visit navigates again and focuses the freshly-focused Discover, not the stale hidden one', () => {
    render(renderDiscover());
    const visibleEffects = [...focusEffects];

    const blurs: ((() => void) | void)[] = [];
    act(() => {
      for (const effect of visibleEffects) blurs.push(effect());
    });

    act(() => {
      for (const blur of blurs) blur?.();
    });

    const win = createFakeWindow();
    renderShortcuts(win);

    win.dispatch('/');

    expect(focusSpy).not.toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith('/discover');

    act(() => {
      for (const effect of visibleEffects) effect();
    });
    expect(focusSpy).toHaveBeenCalledTimes(1);
  });
});

describe('DiscoverScreen tap-to-dismiss-keyboard area', () => {
  const originalOS = Platform.OS;
  let dismiss: jest.SpyInstance;

  beforeEach(() => {
    dismiss = jest.spyOn(Keyboard, 'dismiss').mockImplementation(() => undefined);
  });

  afterEach(() => {
    dismiss.mockRestore();
    Platform.OS = originalOS;
  });

  it('dismisses the keyboard when the screen body is tapped on native', () => {
    Platform.OS = 'ios';
    render(renderDiscover());
    fireEvent.press(screen.getByTestId('discover-screen-body'));
    expect(dismiss).toHaveBeenCalledTimes(1);
  });
});

describe('@features/discovery public surface', () => {
  it('exports exactly DiscoverScreen and the search-state sign-out registration', () => {
    expect(Object.keys(require('@features/discovery')).sort()).toEqual([
      'DiscoverScreen',
      'registerSearchStateSignOutReset',
    ]);
  });
});
