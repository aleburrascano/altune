import { act, fireEvent, render, renderHook, screen } from '@testing-library/react-native';
import { createElement, type ReactElement, type ReactNode } from 'react';
import { Keyboard, Platform } from 'react-native';

import { asTrackId } from '@shared/api-client/ids';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';
import { useKeyboardShortcuts } from '@shared/ui/keyboard/useKeyboardShortcuts';
import type { DiscoverLogic } from '../hooks/useDiscoverLogic';
import { DiscoverScreen } from '../ui/DiscoverScreen';

const mockPush = jest.fn();
const focusEffects: Array<() => (() => void) | void> = [];

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useFocusEffect: (effect: () => (() => void) | void) => {
    focusEffects.push(effect);
  },
}));

const mockFocus = jest.fn();
jest.mock('@shared/ui/primitives/SearchBar', () => {
  const { forwardRef: fr, useImperativeHandle: useImp } = jest.requireActual('react');
  return {
    SearchBar: fr((_props: unknown, ref: unknown) => {
      useImp(ref, () => ({ focus: mockFocus }));
      return null;
    }),
  };
});

jest.mock('../ui/DiscoverBody', () => ({ DiscoverBody: () => null }));
jest.mock('../ui/SuggestionsList', () => ({ SuggestionsList: () => null }));

function mockDiscoverLogic(): DiscoverLogic {
  return {
    inputValue: '',
    pending: false,
    onChangeText: jest.fn(),
    onSubmit: jest.fn(),
    onClear: jest.fn(),
    isFocused: false,
    setIsFocused: jest.fn(),
    showSuggestions: false,
    suggestionItems: [],
    onSuggestionSelect: jest.fn(),
    view: 'empty-no-query',
    resultsIncomplete: false,
    searchData: undefined,
    historyItems: [],
    filter: 'all',
    setFilter: jest.fn(),
    onHistoryTap: jest.fn(),
    onResultTap: jest.fn(),
    impression: {
      onImpression: jest.fn(),
      onImpressionEnd: jest.fn(),
    } as unknown as DiscoverLogic['impression'],
    onRetry: jest.fn(),
    searchError: null,
    onEndReached: jest.fn(),
    isFetchingNextPage: false,
    onRefresh: jest.fn(),
    isRefreshing: false,
    correction: null,
    onSearchOriginal: jest.fn(),
    onClearHistory: jest.fn(),
    nextPageFailed: false,
    onRetryNextPage: jest.fn(),
    clearHistoryFailed: false,
    refreshFailed: false,
    historyFailed: false,
    onRetryHistory: jest.fn(),
  };
}

jest.mock('../hooks/useDiscoverLogic', () => ({
  useDiscoverLogic: () => mockDiscoverLogic(),
}));

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
  return {
    status: 'paused',
    track: {
      source: { kind: 'library', trackId: asTrackId('track-1') },
      title: 'Test Track',
      artist: 'Test Artist',
      artworkUrl: null,
    },
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
    skipNext: jest.fn().mockResolvedValue(undefined),
    skipPrevious: jest.fn().mockResolvedValue(undefined),
    removeQueueIndex: jest.fn(),
    pause: jest.fn(),
    resume: jest.fn(),
    seekTo: jest.fn(),
    setRate: jest.fn(),
    stop: jest.fn(),
    retry: jest.fn(),
  };
}

function renderShortcuts(win: FakeWindow) {
  const controls = playbackFixture();
  return renderHook(() => useKeyboardShortcuts(controls, win as unknown as Window), {
    wrapper: ({ children }: { children: ReactNode }) =>
      createElement(PlaybackContext.Provider, { value: controls }, children),
  });
}

beforeEach(() => {
  mockPush.mockClear();
  mockFocus.mockClear();
  focusEffects.length = 0;
});

function renderDiscover(): ReactElement {
  return <DiscoverScreen />;
}

describe('DiscoverScreen registers its search focus only while visible', () => {
  it('unregisters on blur (not only on unmount), so a "/" pressed elsewhere after a visit navigates again and focuses the freshly-focused Discover, not the stale hidden one', () => {
    render(renderDiscover());
    expect(focusEffects).toHaveLength(1);
    const focusEffect = focusEffects[0]!;

    let blur: (() => void) | void;
    act(() => {
      blur = focusEffect();
    });

    act(() => {
      blur?.();
    });

    const win = createFakeWindow();
    renderShortcuts(win);

    win.dispatch('/');

    expect(mockFocus).not.toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith('/discover');

    act(() => {
      focusEffect();
    });
    expect(mockFocus).toHaveBeenCalledTimes(1);
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
    render(<DiscoverScreen />);
    fireEvent.press(screen.getByTestId('discover-screen-body'));
    expect(dismiss).toHaveBeenCalledTimes(1);
  });
});
