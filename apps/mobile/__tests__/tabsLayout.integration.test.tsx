import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Text } from 'react-native';
import type { ReactNode } from 'react';
import { act, fireEvent, renderRouter, screen, waitFor, within } from 'expo-router/testing-library';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

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

const LATE_NIGHT = {
  id: 'p7',
  name: 'Late Night',
  track_count: 3,
  preview_artwork_urls: [],
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

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

function selected(testID: string): boolean {
  return (screen.getByTestId(testID).props.accessibilityState as { selected: boolean }).selected;
}

function bottomBarButtons(label: string) {
  const sidebar = screen.queryByTestId('sidebar');
  const inSidebar =
    sidebar === null ? [] : within(sidebar).queryAllByRole('button', { name: label });
  return screen.queryAllByRole('button', { name: label }).filter((b) => !inSidebar.includes(b));
}

describe('tabs layout: sidebar in a wide web window, bottom tab bar otherwise', () => {
  it('shows the sidebar and no bottom tab bar on a 1440px web window', async () => {
    await openTabs('/library', { os: 'web', width: 1440 });

    expect(screen.getByTestId('sidebar')).toBeTruthy();
    expect(bottomBarButtons('Discover')).toHaveLength(0);
    expect(bottomBarButtons('Library')).toHaveLength(0);
    expect(bottomBarButtons('Settings')).toHaveLength(0);
  });

  it('shows the sidebar and no bottom tab bar at exactly 1000px on web', async () => {
    await openTabs('/library', { os: 'web', width: 1000 });

    expect(screen.getByTestId('sidebar')).toBeTruthy();
    expect(bottomBarButtons('Library')).toHaveLength(0);
  });

  it('keeps the bottom tab bar and no sidebar on a 999px web window', async () => {
    await openTabs('/library', { os: 'web', width: 999 });

    expect(screen.queryByTestId('sidebar')).toBeNull();
    expect(bottomBarButtons('Discover')).toHaveLength(1);
    expect(bottomBarButtons('Library')).toHaveLength(1);
    expect(bottomBarButtons('Settings')).toHaveLength(1);
  });

  it('keeps the bottom tab bar and no sidebar on a 390px phone', async () => {
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

  it('highlights Library in the sidebar on a playlist opened under Library', async () => {
    await openTabs('/library/playlist/42', { os: 'web', width: 1440 });

    expect(screen.getByText('playlist-screen')).toBeTruthy();
    expect(selected('sidebar-item-library')).toBe(true);
    expect(selected('sidebar-item-discover')).toBe(false);
    expect(selected('sidebar-item-settings')).toBe(false);
  });

  it('opens Settings and moves the highlight when its sidebar item is pressed', async () => {
    const router = await openTabs('/library', { os: 'web', width: 1440 });

    fireEvent.press(screen.getByTestId('sidebar-item-settings'));
    await act(async () => {});

    expect(router.getPathname()).toBe('/settings');
    expect(screen.getByText('settings-screen')).toBeTruthy();
    expect(selected('sidebar-item-settings')).toBe(true);
    expect(selected('sidebar-item-library')).toBe(false);
  });

  it('returns to the Library screen from a playlist when Library is pressed in the sidebar', async () => {
    const router = await openTabs('/library/playlist/42', { os: 'web', width: 1440 });

    fireEvent.press(screen.getByTestId('sidebar-item-library'));
    await act(async () => {});

    expect(router.getPathname()).toBe('/library');
    expect(screen.getByText('library-screen')).toBeTruthy();
    expect(selected('sidebar-item-library')).toBe(true);
  });

  it('opens a playlist from the sidebar list and moves the highlight to Library', async () => {
    const router = await openTabs('/discover', {
      os: 'web',
      width: 1440,
      playlists: [LATE_NIGHT],
    });

    fireEvent.press(await screen.findByText('Late Night'));
    await act(async () => {});

    await waitFor(() => expect(router.getPathname()).toBe('/library/playlist/p7'));
    expect(screen.getByText('playlist-screen')).toBeTruthy();
    expect(selected('sidebar-item-library')).toBe(true);
    expect(selected('sidebar-item-discover')).toBe(false);
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
