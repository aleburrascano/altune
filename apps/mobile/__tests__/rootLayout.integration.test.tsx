import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Stack } from 'expo-router';
import { Text } from 'react-native';
import type { ReactNode } from 'react';
import { act, fireEvent, renderRouter, screen, waitFor, within } from 'expo-router/testing-library';

import { useWideWebLayout } from '@shared/ui/layout/useWideWebLayout';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';
import { usePlayback } from '@shared/playback/usePlayback';
import { useKeyboardShortcuts } from '@shared/ui/keyboard/useKeyboardShortcuts';

import { AppChrome } from '../src/app-shell/AppChrome';
import TabsLayout from '../src/app/(tabs)/_layout';
import DiscoverLayout from '../src/app/(tabs)/discover/_layout';
import LibraryLayout from '../src/app/(tabs)/library/_layout';
import SettingsLayout from '../src/app/(tabs)/settings/_layout';
import PlayerLayout from '../src/app/player/_layout';

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

let mockWindowWidth = 1440;
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

const PLAYING = {
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

let client: QueryClient;

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
});

afterEach(() => {
  RN.Platform.OS = NATIVE_OS;
  mockWindowWidth = 1440;
  client.clear();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <PlaybackContext.Provider value={PLAYING}>{children}</PlaybackContext.Provider>
    </QueryClientProvider>
  );
}

function idleWrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <PlaybackContext.Provider value={IDLE}>{children}</PlaybackContext.Provider>
    </QueryClientProvider>
  );
}

// A stand-in for the real root layout's web-only shortcuts bridge (`WebPlaybackShortcutsBridge`
// in `src/app/_layout.tsx`), which mounts `useKeyboardShortcuts(usePlayback())` once at the root.
function WebPlaybackShortcutsBridgeStandIn() {
  const playback = usePlayback();
  useKeyboardShortcuts(playback);
  return null;
}

// A stand-in for the real root layout: the web-only shortcuts bridge and the actual
// `AppChrome`, both mounted once around a Stack that holds `(tabs)` and `player`, exactly
// as `src/app/_layout.tsx` composes them.
function RootTestLayout() {
  const isWideWeb = useWideWebLayout();
  return (
    <>
      {RN.Platform.OS === 'web' && <WebPlaybackShortcutsBridgeStandIn />}
      <AppChrome isWideWeb={isWideWeb}>
        <Stack screenOptions={{ headerShown: false }}>
          <Stack.Screen name="(tabs)" />
          <Stack.Screen name="player" options={{ animation: 'none' }} />
        </Stack>
      </AppChrome>
    </>
  );
}

const ROUTES = {
  _layout: RootTestLayout,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/discover/_layout': DiscoverLayout,
  '(tabs)/library/_layout': LibraryLayout,
  '(tabs)/settings/_layout': SettingsLayout,
  '(tabs)/discover/index': () => <Text>discover-screen</Text>,
  '(tabs)/library/index': () => <Text>library-screen</Text>,
  '(tabs)/library/playlist/[id]': () => <Text>playlist-screen</Text>,
  '(tabs)/settings/index': () => <Text>settings-screen</Text>,
  'player/_layout': PlayerLayout,
  'player/index': () => <Text>player-screen</Text>,
  'player/queue': () => <Text>queue-screen</Text>,
  'player/lyrics': () => <Text>lyrics-screen</Text>,
};

function selected(testID: string): boolean {
  return (screen.getByTestId(testID).props.accessibilityState as { selected: boolean }).selected;
}

const LATE_NIGHT = {
  id: 'p7',
  name: 'Late Night',
  track_count: 3,
  preview_artwork_urls: [],
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

// The sidebar's highlighting and its click-to-navigate wiring live in `AppChrome`/`WideChrome`
// at the root (moved from `(tabs)/_layout`; see `tabsLayout.integration.test.tsx`).
describe('app chrome: the sidebar highlights the active route and navigates on press', () => {
  it('highlights Library in the sidebar on a playlist opened under Library', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1440;

    renderRouter(ROUTES, { initialUrl: '/library/playlist/42', wrapper });
    await act(async () => {});

    expect(screen.getByText('playlist-screen')).toBeTruthy();
    expect(selected('sidebar-item-library')).toBe(true);
    expect(selected('sidebar-item-discover')).toBe(false);
    expect(selected('sidebar-item-settings')).toBe(false);
  });

  it('opens Settings and moves the highlight when its sidebar item is pressed', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1440;

    const router = renderRouter(ROUTES, { initialUrl: '/library', wrapper });
    await act(async () => {});

    fireEvent.press(screen.getByTestId('sidebar-item-settings'));
    await act(async () => {});

    expect(router.getPathname()).toBe('/settings');
    expect(screen.getByText('settings-screen')).toBeTruthy();
    expect(selected('sidebar-item-settings')).toBe(true);
    expect(selected('sidebar-item-library')).toBe(false);
  });

  it('returns to the Library screen from a playlist when Library is pressed in the sidebar', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1440;

    const router = renderRouter(ROUTES, { initialUrl: '/library/playlist/42', wrapper });
    await act(async () => {});

    fireEvent.press(screen.getByTestId('sidebar-item-library'));
    await act(async () => {});

    expect(router.getPathname()).toBe('/library');
    expect(screen.getByText('library-screen')).toBeTruthy();
    expect(selected('sidebar-item-library')).toBe(true);
  });

  it('opens a playlist from the sidebar list and moves the highlight to Library', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1440;
    __http.reset();
    __http.reply('GET /v1/playlists', { status: 200, json: { items: [LATE_NIGHT], total: 1 } });

    const router = renderRouter(ROUTES, { initialUrl: '/discover', wrapper });
    await act(async () => {});

    fireEvent.press(await screen.findByText('Late Night'));
    await act(async () => {});

    await waitFor(() => expect(router.getPathname()).toBe('/library/playlist/p7'));
    expect(screen.getByText('playlist-screen')).toBeTruthy();
    expect(selected('sidebar-item-library')).toBe(true);
    expect(selected('sidebar-item-discover')).toBe(false);
  });
});

describe('app chrome: one sidebar and one player bar across a tab-to-player navigation', () => {
  it('keeps a single sidebar and a single player bar across a tab, then the player, then the queue', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1440;

    const router = renderRouter(ROUTES, { initialUrl: '/library', wrapper });
    await act(async () => {});

    expect(screen.getByText('library-screen')).toBeTruthy();
    expect(screen.getAllByTestId('sidebar')).toHaveLength(1);
    expect(screen.getAllByTestId('player-bar')).toHaveLength(1);
    expect(screen.queryByTestId('mini-player')).toBeNull();

    fireEvent.press(screen.getByLabelText('Open player: A Title by An Artist'));
    await act(async () => {});

    expect(router.getPathname()).toBe('/player');
    expect(screen.getAllByTestId('sidebar')).toHaveLength(1);
    expect(screen.getAllByTestId('player-bar')).toHaveLength(1);

    fireEvent.press(screen.getByLabelText('View queue'));
    await act(async () => {});

    expect(router.getPathname()).toBe('/player/queue');
    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.getAllByTestId('sidebar')).toHaveLength(1);
    expect(screen.getAllByTestId('player-bar')).toHaveLength(1);
  });

  it('shows no sidebar or player bar chrome around the tab on a native app', async () => {
    RN.Platform.OS = NATIVE_OS;
    mockWindowWidth = 1440;

    renderRouter(ROUTES, { initialUrl: '/library', wrapper });
    await act(async () => {});

    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });

  it('shows the sidebar and player bar beside the player, queue and lyrics pages on a 1440px web window', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1440;

    const { router } = require('expo-router');
    renderRouter(ROUTES, { initialUrl: '/player/queue', wrapper });
    await act(async () => {});

    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.getByTestId('sidebar')).toBeTruthy();
    expect(screen.getByTestId('player-bar')).toBeTruthy();

    act(() => router.push('/player/lyrics'));
    await act(async () => {});

    expect(screen.getByText('lyrics-screen')).toBeTruthy();
    expect(screen.getByTestId('sidebar')).toBeTruthy();
    expect(screen.getByTestId('player-bar')).toBeTruthy();

    act(() => router.push('/player'));
    await act(async () => {});

    expect(screen.getByText('player-screen')).toBeTruthy();
    expect(screen.getByTestId('sidebar')).toBeTruthy();
    expect(screen.getByTestId('player-bar')).toBeTruthy();
  });

  it('shows neither sidebar nor player bar around the queue page on a 999px web window', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 999;

    renderRouter(ROUTES, { initialUrl: '/player/queue', wrapper });
    await act(async () => {});

    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });

  it('shows no sidebar or player bar chrome around the player on a native tablet 1440px wide', async () => {
    RN.Platform.OS = NATIVE_OS;
    mockWindowWidth = 1440;

    renderRouter(ROUTES, { initialUrl: '/player', wrapper });
    await act(async () => {});

    expect(screen.getByText('player-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });
});

// `PlayerBar.test.tsx` covers the next-track control against the real queue store directly;
// this confirms the same wiring holds through the root chrome (moved from `(tabs)/_layout`;
// see `tabsLayout.integration.test.tsx`).
describe('app chrome: the player bar reflects the live queue', () => {
  it('offers a next-track control once a second track is queued', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1440;
    const { useQueueStore } = require('@shared/playback/queueStore');
    useQueueStore.getState().loadQueue(
      [
        {
          source: { kind: 'library', trackId: 'trk-1' as never },
          title: 'A Title',
          artist: 'An Artist',
          artworkUrl: null,
        },
        {
          source: { kind: 'library', trackId: 'trk-2' as never },
          title: 'B Title',
          artist: 'B Artist',
          artworkUrl: null,
        },
      ],
      0,
      null,
    );

    renderRouter(ROUTES, { initialUrl: '/library', wrapper });
    await act(async () => {});

    expect(within(screen.getByTestId('player-bar')).getByLabelText('Next track')).toBeTruthy();

    useQueueStore.getState().clearQueue();
  });
});

describe('app chrome: the idle player bar stays on every screen in wide web layout', () => {
  it.each(['/discover', '/settings', '/library/playlist/42'])(
    'shows the idle player bar on %s on a 1440px web window',
    async (url) => {
      RN.Platform.OS = 'web';
      mockWindowWidth = 1440;

      renderRouter(ROUTES, { initialUrl: url, wrapper: idleWrapper });
      await act(async () => {});

      expect(within(screen.getByTestId('player-bar')).getByText('Pick something to play')).toBeTruthy();
    },
  );

  it('shows neither player bar nor idle placeholder on a 390px phone', async () => {
    RN.Platform.OS = NATIVE_OS;
    mockWindowWidth = 390;

    renderRouter(ROUTES, { initialUrl: '/library', wrapper: idleWrapper });
    await act(async () => {});

    expect(screen.queryByTestId('player-bar')).toBeNull();
    expect(screen.queryByText('Pick something to play')).toBeNull();
  });
});


type KeyListener = { type: string; listener: (event: unknown) => void };

// The test environment has no native `addEventListener`/`removeEventListener` (jest's
// react-native preset runs under plain Node, not jsdom), so this stub stands in for the
// one the browser provides in production, letting `useKeyboardShortcuts`'s real
// `window.addEventListener('keydown', ...)` wiring be exercised end to end.
let keyListeners: KeyListener[] = [];
(globalThis as { addEventListener?: unknown }).addEventListener = (
  type: string,
  listener: (event: unknown) => void,
) => {
  keyListeners.push({ type, listener });
};
(globalThis as { removeEventListener?: unknown }).removeEventListener = (
  type: string,
  listener: (event: unknown) => void,
) => {
  keyListeners = keyListeners.filter((l) => !(l.type === type && l.listener === listener));
};

describe('app chrome: the web keyboard shortcuts bridge mounts once at the root', () => {
  beforeEach(() => {
    keyListeners = [];
  });

  function pressSpace() {
    const event = {
      key: ' ',
      shiftKey: false,
      ctrlKey: false,
      metaKey: false,
      altKey: false,
      target: null,
      preventDefault: () => {},
    };
    act(() => {
      keyListeners.filter((l) => l.type === 'keydown').forEach((l) => l.listener(event));
    });
  }

  function playingControls(): PlaybackContextValue {
    return {
      ...IDLE,
      status: 'playing',
      track: {
        source: { kind: 'library', trackId: 'trk-1' as never },
        title: 'A Title',
        artist: 'An Artist',
        artworkUrl: null,
      },
      positionMs: 30000,
      durationMs: 200000,
      pause: jest.fn(),
      resume: jest.fn(),
      seekTo: jest.fn(),
      skipNext: jest.fn(),
      skipPrevious: jest.fn(),
    } as unknown as PlaybackContextValue;
  }

  async function openApp(initialUrl: string, { os, width }: { os: string; width: number }) {
    const playback = playingControls();
    RN.Platform.OS = os;
    mockWindowWidth = width;
    const rendered = renderRouter(ROUTES, {
      initialUrl,
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>
          <PlaybackContext.Provider value={playback}>{children}</PlaybackContext.Provider>
        </QueryClientProvider>
      ),
    });
    await act(async () => {});
    return { playback, rendered };
  }

  it.each([
    ['a 1440px', 1440],
    ['a 999px', 999],
  ])('registers exactly one keydown listener and pauses once on Space after opening the player from Library on %s web window', async (_label, width) => {
    const { playback } = await openApp('/library', { os: 'web', width });

    act(() => require('expo-router').router.push('/player'));
    await act(async () => {});

    expect(keyListeners.filter((l) => l.type === 'keydown')).toHaveLength(1);

    pressSpace();

    expect(screen.getByText('player-screen')).toBeTruthy();
    expect(playback.pause).toHaveBeenCalledTimes(1);
  });

  it('registers exactly one keydown listener and pauses once on Space on a directly loaded /player route', async () => {
    const { playback } = await openApp('/player', { os: 'web', width: 1440 });

    expect(keyListeners.filter((l) => l.type === 'keydown')).toHaveLength(1);

    pressSpace();

    expect(screen.getByText('player-screen')).toBeTruthy();
    expect(playback.pause).toHaveBeenCalledTimes(1);
  });
});
