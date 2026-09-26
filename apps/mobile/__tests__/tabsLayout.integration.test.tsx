import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Text } from 'react-native';
import type { ReactNode } from 'react';
import { act, renderRouter, screen, within } from 'expo-router/testing-library';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';
import { asTrackId } from '@shared/api-client/ids';

import TabsLayout from '../src/app/(tabs)/_layout';
import DiscoverLayout from '../src/app/(tabs)/discover/_layout';
import LibraryLayout from '../src/app/(tabs)/library/_layout';
import SettingsLayout from '../src/app/(tabs)/settings/_layout';

const { __http } = require('../jest/doubles/fetch.js');

jest.mock('decode-uri-component', () => ({
  __esModule: true,
  default: (s: string) => {
    try {
      return decodeURIComponent(s);
    } catch {
      return s;
    }
  },
}));

let mockWindowWidth = 390;
jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 900, scale: 2, fontScale: 1 }),
}));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest.fn(),
      onAuthStateChange: jest.fn(() => ({ data: { subscription: { unsubscribe: jest.fn() } } })),
    },
  },
}));

const { supabase } = require('@shared/auth/supabaseClient');
const RN = require('react-native');
const NATIVE_OS: string = RN.Platform.OS;

const IDLE = {
  status: 'idle',
  track: null,
  positionMs: 0,
  durationMs: 0,
  errorMessage: null,
  errorKind: null,
} as unknown as PlaybackContextValue;

let client: QueryClient;

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});

afterEach(() => {
  RN.Platform.OS = NATIVE_OS;
  mockWindowWidth = 390;
  client.clear();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <PlaybackContext.Provider value={IDLE}>{children}</PlaybackContext.Provider>
    </QueryClientProvider>
  );
}

const ROUTES = {
  '(tabs)/_layout': TabsLayout,
  '(tabs)/discover/_layout': DiscoverLayout,
  '(tabs)/library/_layout': LibraryLayout,
  '(tabs)/settings/_layout': SettingsLayout,
  '(tabs)/discover/index': () => <Text>discover-screen</Text>,
  '(tabs)/library/index': () => <Text>library-screen</Text>,
  '(tabs)/library/playlist/[id]': () => <Text>playlist-screen</Text>,
  '(tabs)/settings/index': () => <Text>settings-screen</Text>,
};

async function openTabs(
  initialUrl: string,
  { os, width, playlists = [] }: { os: string; width: number; playlists?: unknown[] },
) {
  __http.reply('GET /v1/playlists', {
    status: 200,
    json: { items: playlists, total: playlists.length },
  });
  RN.Platform.OS = os;
  mockWindowWidth = width;
  const result = renderRouter(ROUTES, { initialUrl, wrapper });
  await act(async () => {});
  return result;
}

function bottomBarButtons(label: string) {
  const sidebar = screen.queryByTestId('sidebar');
  const inSidebar =
    sidebar === null ? [] : within(sidebar).queryAllByRole('button', { name: label });
  return screen.queryAllByRole('button', { name: label }).filter((b) => !inSidebar.includes(b));
}

// The sidebar itself, its highlighting and its click-to-navigate wiring now live in the
// root-level `AppChrome`/`WideChrome` (see `rootLayout.integration.test.tsx`), not in
// `TabsLayout`. `TabsLayout` still owns hiding its own bottom tab bar in wide web layout.
describe('tabs layout: hides its own bottom tab bar in a wide web window, shows it otherwise', () => {
  it('hides its own bottom tab bar on a 1440px web window', async () => {
    await openTabs('/library', { os: 'web', width: 1440 });

    expect(bottomBarButtons('Discover')).toHaveLength(0);
    expect(bottomBarButtons('Library')).toHaveLength(0);
    expect(bottomBarButtons('Settings')).toHaveLength(0);
  });

  it('hides its own bottom tab bar at exactly 1000px on web', async () => {
    await openTabs('/library', { os: 'web', width: 1000 });

    expect(bottomBarButtons('Library')).toHaveLength(0);
  });

  it('keeps the bottom tab bar and renders no sidebar chrome of its own on a 999px web window', async () => {
    await openTabs('/library', { os: 'web', width: 999 });

    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(bottomBarButtons('Discover')).toHaveLength(1);
    expect(bottomBarButtons('Library')).toHaveLength(1);
    expect(bottomBarButtons('Settings')).toHaveLength(1);
  });

  it('keeps the bottom tab bar and renders no sidebar chrome of its own on a 390px phone', async () => {
    await openTabs('/library', { os: NATIVE_OS, width: 390 });

    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(bottomBarButtons('Library')).toHaveLength(1);
  });

  it('highlights the Library tab on a playlist opened under Library on a phone', async () => {
    await openTabs('/library/playlist/42', { os: NATIVE_OS, width: 390 });

    expect(screen.getByText('playlist-screen')).toBeTruthy();
    expect(bottomBarButtons('Library')[0].props.accessibilityState).toEqual({ selected: true });
    expect(bottomBarButtons('Discover')[0].props.accessibilityState).toEqual({ selected: false });
  });

  it('renders no sidebar chrome of its own on a playlist route on a 1440px web window', async () => {
    await openTabs('/library/playlist/42', { os: 'web', width: 1440 });

    expect(screen.getByText('playlist-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
  });
});

describe('tabs layout: the sidebar is web-only, whatever the native width', () => {
  it('keeps the bottom tab bar and no sidebar on a native tablet 1440px wide', async () => {
    await openTabs('/library', { os: NATIVE_OS, width: 1440 });

    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(bottomBarButtons('Discover')).toHaveLength(1);
    expect(bottomBarButtons('Library')).toHaveLength(1);
    expect(bottomBarButtons('Settings')).toHaveLength(1);
  });
});

function playingFixture(): PlaybackContextValue {
  return {
    status: 'playing',
    track: {
      source: { kind: 'library', trackId: 'trk-1' as never },
      title: 'A Title',
      artist: 'An Artist',
      artworkUrl: null,
    },
    positionMs: 0,
    durationMs: 0,
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
  } as unknown as PlaybackContextValue;
}

async function openTabsPlaying(
  initialUrl: string,
  { os, width, playback }: { os: string; width: number; playback: PlaybackContextValue },
) {
  __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
  RN.Platform.OS = os;
  mockWindowWidth = width;
  const result = renderRouter(ROUTES, {
    initialUrl,
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        <PlaybackContext.Provider value={playback}>{children}</PlaybackContext.Provider>
      </QueryClientProvider>
    ),
  });
  await act(async () => {});
  return result;
}

// `MiniPlayer` hides itself in wide web layout on its own (`useWideWebLayout()`), unaffected
// by where the sidebar/player bar chrome now mounts. The player bar itself, alongside the
// hidden mini player, is asserted in `rootLayout.integration.test.tsx`.
describe('tabs layout: hides its own mini player in wide web layout, shows it otherwise', () => {
  it('hides the mini player on a 1440px web window', async () => {
    await openTabsPlaying('/library', { os: 'web', width: 1440, playback: playingFixture() });

    expect(screen.queryByTestId('mini-player')).toBeNull();
  });

  it('shows the mini player and no player bar of its own on a 999px web window', async () => {
    await openTabsPlaying('/library', { os: 'web', width: 999, playback: playingFixture() });

    expect(screen.queryByTestId('player-bar')).toBeNull();
    expect(screen.getByTestId('mini-player')).toBeTruthy();
  });

  it('shows the mini player and no player bar of its own on native, whatever the width', async () => {
    await openTabsPlaying('/library', { os: NATIVE_OS, width: 1440, playback: playingFixture() });

    expect(screen.queryByTestId('player-bar')).toBeNull();
    expect(screen.getByTestId('mini-player')).toBeTruthy();
  });
});

const mockUseKeyboardShortcuts = jest.fn();

jest.mock('../src/shared/ui/keyboard/useKeyboardShortcuts', () => ({
  useKeyboardShortcuts: (...args: unknown[]) => mockUseKeyboardShortcuts(...args),
}));

// The web keyboard shortcuts now mount once at the root (`WebPlaybackShortcutsBridge` in
// `src/app/_layout.tsx`; see `rootLayout.integration.test.tsx`), not from `TabsLayout`.
describe('tabs layout: does not mount the web keyboard shortcuts on its own', () => {
  beforeEach(() => {
    mockUseKeyboardShortcuts.mockClear();
  });

  it('never calls the shortcut hook on a wide web window', async () => {
    const playback = playingFixture();
    await openTabsPlaying('/library', { os: 'web', width: 1440, playback });

    expect(mockUseKeyboardShortcuts).not.toHaveBeenCalled();
  });

  it('never calls the shortcut hook on a narrow web window either', async () => {
    const playback = playingFixture();
    await openTabsPlaying('/library', { os: 'web', width: 999, playback });

    expect(mockUseKeyboardShortcuts).not.toHaveBeenCalled();
  });
});

// The player bar itself, and its live-queue wiring, now render only from the root chrome
// (see `rootLayout.integration.test.tsx`); `PlayerBar.test.tsx` covers the next-track
// control against the real queue store directly. `TabsLayout` alone never renders it.
describe('tabs layout: renders no player bar of its own even with a queued second track', () => {
  it('renders no player bar on a 1440px web window', async () => {
    const { useQueueStore } = require('@shared/playback/queueStore');
    useQueueStore.getState().loadQueue(
      [
        {
          source: { kind: 'library', trackId: asTrackId('trk-1') },
          title: 'A Title',
          artist: 'An Artist',
          artworkUrl: null,
        },
        {
          source: { kind: 'library', trackId: asTrackId('trk-2') },
          title: 'B Title',
          artist: 'B Artist',
          artworkUrl: null,
        },
      ],
      0,
      null,
    );

    await openTabsPlaying('/library', { os: 'web', width: 1440, playback: playingFixture() });

    expect(screen.queryByTestId('player-bar')).toBeNull();

    useQueueStore.getState().clearQueue();
  });
});

// The idle player bar itself now renders only from the root chrome (see
// `rootLayout.integration.test.tsx`); `TabsLayout` alone never renders it.
describe('tabs layout: renders no idle player bar of its own', () => {
  it.each(['/discover', '/settings', '/library/playlist/42'])(
    'renders no player bar on %s on a 1440px web window',
    async (url) => {
      await openTabs(url, { os: 'web', width: 1440 });

      expect(screen.queryByTestId('player-bar')).toBeNull();
    },
  );

  it('shows neither player bar nor idle placeholder on a 390px phone', async () => {
    await openTabs('/library', { os: NATIVE_OS, width: 390 });

    expect(screen.queryByTestId('player-bar')).toBeNull();
    expect(screen.queryByText('Pick something to play')).toBeNull();
  });
});
