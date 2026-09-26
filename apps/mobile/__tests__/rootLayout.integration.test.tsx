import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Stack } from 'expo-router';
import { Text } from 'react-native';
import type { ReactNode } from 'react';
import { act, fireEvent, renderRouter, screen } from 'expo-router/testing-library';

import { useWideWebLayout } from '@shared/ui/layout/useWideWebLayout';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

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

// A stand-in for the real root layout: the actual `AppChrome` mounted once around a
// Stack that holds `(tabs)` and `player`, exactly as `src/app/_layout.tsx` composes them.
function RootTestLayout() {
  const isWideWeb = useWideWebLayout();
  return (
    <AppChrome isWideWeb={isWideWeb}>
      <Stack screenOptions={{ headerShown: false }}>
        <Stack.Screen name="(tabs)" />
        <Stack.Screen name="player" options={{ animation: 'none' }} />
      </Stack>
    </AppChrome>
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
  '(tabs)/settings/index': () => <Text>settings-screen</Text>,
  'player/_layout': PlayerLayout,
  'player/index': () => <Text>player-screen</Text>,
  'player/queue': () => <Text>queue-screen</Text>,
  'player/lyrics': () => <Text>lyrics-screen</Text>,
};

describe('app chrome: one sidebar and one player bar across a tab-to-player navigation', () => {
  it('keeps a single sidebar and a single player bar across a tab, then the player, then the queue', async () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1440;

    const router = renderRouter(ROUTES, { initialUrl: '/library', wrapper });
    await act(async () => {});

    expect(screen.getByText('library-screen')).toBeTruthy();
    expect(screen.getAllByTestId('sidebar')).toHaveLength(1);
    expect(screen.getAllByTestId('player-bar')).toHaveLength(1);

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
});
