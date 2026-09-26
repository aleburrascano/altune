import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Text } from 'react-native';
import type { ReactNode } from 'react';
import { act, renderRouter, screen } from 'expo-router/testing-library';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

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
      <PlaybackContext.Provider value={IDLE}>{children}</PlaybackContext.Provider>
    </QueryClientProvider>
  );
}

const ROUTES = {
  'player/_layout': PlayerLayout,
  'player/index': () => <Text>player-screen</Text>,
  'player/queue': () => <Text>queue-screen</Text>,
  'player/lyrics': () => <Text>lyrics-screen</Text>,
};

async function openPlayer(initialUrl: string, { os, width }: { os: string; width: number }) {
  RN.Platform.OS = os;
  mockWindowWidth = width;
  const result = renderRouter(ROUTES, { initialUrl, wrapper });
  await act(async () => {});
  return result;
}

// The sidebar and player bar are now owned by the root-level `AppChrome`
// (see `rootLayout.integration.test.tsx`), mounted once above `(tabs)` and `player`.
// `PlayerLayout` on its own never renders them, on any route, width or platform.
describe('player layout: renders no sidebar or player bar chrome on its own', () => {
  it('renders no sidebar or player bar alongside the queue page on a 1440px web window', async () => {
    await openPlayer('/player/queue', { os: 'web', width: 1440 });

    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });

  it('renders no sidebar or player bar alongside the lyrics page on a 1440px web window', async () => {
    await openPlayer('/player/lyrics', { os: 'web', width: 1440 });

    expect(screen.getByText('lyrics-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });

  it('renders no sidebar or player bar around the queue page on a 999px web window', async () => {
    await openPlayer('/player/queue', { os: 'web', width: 999 });

    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });

  it('renders no sidebar or player bar around the queue page on native', async () => {
    await openPlayer('/player/queue', { os: NATIVE_OS, width: 1440 });

    expect(screen.getByText('queue-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });

  it('renders no sidebar or player bar alongside the player page itself on a 1440px web window', async () => {
    await openPlayer('/player', { os: 'web', width: 1440 });

    expect(screen.getByText('player-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });

  it('never shows the sidebar or player bar around the player on a native tablet 1440px wide', async () => {
    await openPlayer('/player', { os: NATIVE_OS, width: 1440 });

    expect(screen.getByText('player-screen')).toBeTruthy();
    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(screen.queryByTestId('player-bar')).toBeNull();
  });
});

// The web keyboard shortcuts now mount once at the root (`WebPlaybackShortcutsBridge` in
// `src/app/_layout.tsx`; see `rootLayout.integration.test.tsx`), not from `(tabs)` or
// `player`. Neither `TabsLayout` nor `PlayerLayout` mounts the hook on its own, so pressing
// Space around the player, without that root bridge, never touches playback.
describe('player layout: does not mount keyboard shortcuts on its own', () => {
  const { router } = require('expo-router');
  const TabsLayout = require('../src/app/(tabs)/_layout').default;
  const LibraryLayout = require('../src/app/(tabs)/library/_layout').default;
  const { Stack } = require('expo-router');
  const RootStack = () => <Stack screenOptions={{ headerShown: false }} />;

  type KeyListener = { type: string; listener: (event: unknown) => void };
  const host = globalThis as unknown as {
    addEventListener?: unknown;
    removeEventListener?: unknown;
  };
  let listeners: KeyListener[] = [];
  let originalAdd: unknown;
  let originalRemove: unknown;
  let rendered: { unmount: () => void } | null = null;

  beforeEach(() => {
    listeners = [];
    originalAdd = host.addEventListener;
    originalRemove = host.removeEventListener;
    host.addEventListener = (type: string, listener: (event: unknown) => void) => {
      listeners.push({ type, listener });
    };
    host.removeEventListener = (type: string, listener: (event: unknown) => void) => {
      listeners = listeners.filter((l) => !(l.type === type && l.listener === listener));
    };
  });

  afterEach(() => {
    rendered?.unmount();
    rendered = null;
    host.addEventListener = originalAdd;
    host.removeEventListener = originalRemove;
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
      listeners.filter((l) => l.type === 'keydown').forEach((l) => l.listener(event));
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
    rendered = renderRouter(
      {
        _layout: RootStack,
        ...ROUTES,
        '(tabs)/_layout': TabsLayout,
        '(tabs)/library/_layout': LibraryLayout,
        '(tabs)/library/index': () => <Text>library-screen</Text>,
      },
      {
        initialUrl,
        wrapper: ({ children }: { children: ReactNode }) => (
          <QueryClientProvider client={client}>
            <PlaybackContext.Provider value={playback}>{children}</PlaybackContext.Provider>
          </QueryClientProvider>
        ),
      },
    );
    await act(async () => {});
    return playback;
  }

  it.each([
    ['a 1440px', 1440],
    ['a 999px', 999],
  ])('registers no keydown listener and never pauses on Space after opening the player from Library on %s web window', async (_label, width) => {
    const playback = await openApp('/library', { os: 'web', width });

    act(() => router.push('/player'));
    await act(async () => {});

    expect(listeners.filter((l) => l.type === 'keydown')).toHaveLength(0);

    pressSpace();

    expect(screen.getByText('player-screen')).toBeTruthy();
    expect(playback.pause).toHaveBeenCalledTimes(0);
  });
});
