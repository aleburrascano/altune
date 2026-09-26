// #1705: the detail screen answered every failed load with "Playlist not found" and a
// "Go back" button — a false claim for an offline device or a 5xx, and no way back to
// the playlist short of leaving the screen. Only a 404/410 means it is really gone;
// everything else is transient and gets the retry. Nothing was logged either, so a
// "my playlist won't open" report reached triage with no status and no failure class.

// #786: the playlist id arrives from a deep-link route param, so it is untrusted. A value that
// isn't a plausible id shape must never reach a request path; the screen treats it as no id.

import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { PlaylistDetailScreen } from '../ui/PlaylistDetailScreen';

const { __http } = require('../../../../jest/doubles/fetch.js');

let mockParams: { id?: string } = {};
const mockReplace = jest.fn();
jest.mock('expo-router', () => ({
  useLocalSearchParams: () => mockParams,
  useRouter: () => ({
    replace: mockReplace,
    push: jest.fn(),
    back: jest.fn(),
    canGoBack: () => false,
  }),
}));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

// Playback providers are irrelevant to routing; stub them so the screen can mount on its own.
jest.mock('@shared/playback/usePlayback', () => ({
  usePlayback: () => ({ status: 'idle', source: null }),
}));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({}),
}));

function playlistRequests(): string[] {
  return (__http.requests as { path: string }[])
    .map((r) => r.path)
    .filter((path) => path.startsWith('/v1/playlists'));
}

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { gcTime: Infinity },
    },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('PlaylistDetailScreen route param', () => {
  beforeEach(() => {
    mockReplace.mockClear();
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
  });

  it.each(['p1/tracks', '../p1', 'p1?x=1', 'p1#frag', 'p1%2Ftracks'])(
    'redirects to the library and requests nothing for the malformed id %p',
    async (id) => {
      mockParams = { id };

      render(<PlaylistDetailScreen />, { wrapper });
      await Promise.resolve();

      expect(mockReplace).toHaveBeenCalledWith('/library');
      expect(playlistRequests()).toEqual([]);
    },
  );

  it('fetches the playlist for a well-formed id, so the redirect above is not a blanket one', async () => {
    mockParams = { id: 'p1' };
    __http.reply('GET /v1/playlists/p1', { status: 404, json: { message: 'not found' } });

    render(<PlaylistDetailScreen />, { wrapper });

    await waitFor(() => expect(playlistRequests()).toEqual(['/v1/playlists/p1']));
    expect(mockReplace).not.toHaveBeenCalled();
  });
});

const SERVER_MESSAGE = 'playlist shard 7 is down for daft punk homework';
const LOG_LINE = '[library] playlist detail query failed';

const playlistBody = {
  id: 'p1',
  name: 'Road Trip',
  track_count: 0,
  preview_artwork_urls: [],
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
  total_duration_seconds: 0,
  tracks: [],
};

let warnSpy: jest.SpyInstance;

/** What a console would render: `message` and `stack` are non-enumerable (#1704). */
function loggedText(): string {
  return JSON.stringify(warnSpy.mock.calls, (_key, value: unknown) =>
    value instanceof Error ? `${value.name}: ${value.message} ${value.stack ?? ''}` : value,
  );
}

function detailFailureLogs(): unknown[][] {
  return warnSpy.mock.calls.filter((call) => call[0] === LOG_LINE);
}

describe('PlaylistDetailScreen — a failed load', () => {
  beforeEach(() => {
    mockParams = { id: 'p1' };
  });

  beforeEach(() => {
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  it('offers a retry instead of claiming the playlist is gone when the API fails', async () => {
    __http.reply('GET /v1/playlists/p1', { status: 500, json: { message: SERVER_MESSAGE } });

    const screen = render(<PlaylistDetailScreen />, { wrapper });

    expect(await screen.findByTestId('playlist-detail-retry')).toBeTruthy();
    expect(screen.queryByText('Playlist not found')).toBeNull();
    expect(screen.getByText('Something went wrong on our end. Please try again.')).toBeTruthy();
  });

  it('offers a retry and the offline copy when the device cannot reach the API', async () => {
    __http.fail('GET /v1/playlists/p1');

    const screen = render(<PlaylistDetailScreen />, { wrapper });

    expect(await screen.findByTestId('playlist-detail-retry')).toBeTruthy();
    expect(screen.queryByText('Playlist not found')).toBeNull();
    expect(screen.getByText('No connection')).toBeTruthy();
    expect(detailFailureLogs()).toEqual([
      [LOG_LINE, expect.objectContaining({ playlistId: 'p1', failure: 'network' })],
    ]);
  });

  it('logs the playlist, the status and the failure class without the server message', async () => {
    __http.reply('GET /v1/playlists/p1', {
      status: 500,
      json: { message: SERVER_MESSAGE, code: 'playlist_unavailable' },
    });

    const screen = render(<PlaylistDetailScreen />, { wrapper });

    await screen.findByTestId('playlist-detail-retry');
    expect(detailFailureLogs()).toEqual([
      [
        LOG_LINE,
        expect.objectContaining({
          playlistId: 'p1',
          status: 500,
          code: 'playlist_unavailable',
          failure: 'server',
        }),
      ],
    ]);
    expect(loggedText()).not.toContain(SERVER_MESSAGE);
  });

  it('shows the playlist once the retry succeeds', async () => {
    __http.replyOnce('GET /v1/playlists/p1', { status: 500, json: { message: SERVER_MESSAGE } });
    __http.reply('GET /v1/playlists/p1', { status: 200, json: playlistBody });

    const screen = render(<PlaylistDetailScreen />, { wrapper });

    fireEvent.press(await screen.findByTestId('playlist-detail-retry'));

    expect(await screen.findByText('Road Trip')).toBeTruthy();
    expect(screen.queryByTestId('playlist-detail-retry')).toBeNull();
  });

  it('keeps the "Playlist not found" copy and no retry for a playlist that is really gone', async () => {
    __http.reply('GET /v1/playlists/p1', { status: 404, json: { message: 'not found' } });

    const screen = render(<PlaylistDetailScreen />, { wrapper });

    expect(await screen.findByText('Playlist not found')).toBeTruthy();
    expect(screen.getByText('Go back')).toBeTruthy();
    expect(screen.queryByTestId('playlist-detail-retry')).toBeNull();
    await waitFor(() =>
      expect(detailFailureLogs()).toEqual([
        [
          LOG_LINE,
          expect.objectContaining({ playlistId: 'p1', status: 404, failure: 'not-found' }),
        ],
      ]),
    );
  });
});

let mockOfflineDownloadsSupported = true;
jest.mock('@shared/offline/offlineSupport', () => ({
  get offlineDownloadsSupported() {
    return mockOfflineDownloadsSupported;
  },
}));

const oneReadyTrackPlaylist = {
  ...playlistBody,
  track_count: 1,
  total_duration_seconds: 212,
  tracks: [
    {
      id: 't1',
      title: 'Aerodynamic',
      artist: 'Daft Punk',
      album: 'Discovery',
      duration_seconds: 212,
      added_at: '2026-01-01T00:00:00Z',
      artwork_url: null,
      year: 2001,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: 'ref-1',
      acquisition_status: 'ready',
      failure_reason: null,
    },
  ],
};

describe('PlaylistDetailScreen — offline download controls follow platform support', () => {
  beforeEach(() => {
    mockParams = { id: 'p1' };
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    __http.reply('GET /v1/playlists/p1', { status: 200, json: oneReadyTrackPlaylist });
  });

  afterEach(() => {
    mockOfflineDownloadsSupported = true;
  });

  async function renderLoaded(offlineDownloadsSupported: boolean) {
    mockOfflineDownloadsSupported = offlineDownloadsSupported;
    const screen = render(<PlaylistDetailScreen />, { wrapper });
    await screen.findByText('Road Trip');
    return screen;
  }

  it('offers no playlist download action in the playlist options on web', async () => {
    const screen = await renderLoaded(false);
    fireEvent.press(screen.getByLabelText('Playlist options'));
    expect(screen.getByLabelText('Delete Playlist')).toBeTruthy();
    expect(screen.queryByLabelText(/download/i)).toBeNull();
  }, 60000);

  it('still offers Download all in the playlist options on native', async () => {
    const screen = await renderLoaded(true);
    fireEvent.press(screen.getByLabelText('Playlist options'));
    expect(screen.getByLabelText('Download all (1)')).toBeTruthy();
  }, 60000);

  it('offers no Download action in the selection bar on web', async () => {
    const screen = await renderLoaded(false);
    fireEvent(screen.getByTestId('library-row-t1'), 'longPress');
    expect(screen.getByText('Add to Queue')).toBeTruthy();
    expect(screen.queryByText(/download/i)).toBeNull();
  }, 60000);

  it('still offers Download in the selection bar on native', async () => {
    const screen = await renderLoaded(true);
    fireEvent(screen.getByTestId('library-row-t1'), 'longPress');
    expect(screen.getByText('Download')).toBeTruthy();
  }, 60000);
});

const { Platform: RNPlatform } = require('react-native');

let mockDetailWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockDetailWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

describe('PlaylistDetailScreen — wide web layout', () => {
  const originalOS = RNPlatform.OS;

  beforeEach(() => {
    mockParams = { id: 'p1' };
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    __http.reply('GET /v1/playlists/p1', { status: 200, json: oneReadyTrackPlaylist });
    RNPlatform.OS = 'web';
    mockDetailWindowWidth = 1440;
  });

  afterEach(() => {
    RNPlatform.OS = originalOS;
    mockDetailWindowWidth = 390;
  });

  it('puts the hero to the left and the tracks to the right at 1440px', async () => {
    const screen = render(<PlaylistDetailScreen />, { wrapper });
    await screen.findByText('Road Trip');

    expect(screen.getByTestId('playlist-wide-hero')).toBeTruthy();
    expect(screen.getByTestId('playlist-wide-tracks')).toBeTruthy();
    const layout = screen.getByTestId('playlist-wide-layout');
    expect(layout.props.style).toEqual(expect.objectContaining({ flexDirection: 'row' }));
  }, 60000);

  it('keeps the single-column layout at a compact width even on web', async () => {
    mockDetailWindowWidth = 390;
    const screen = render(<PlaylistDetailScreen />, { wrapper });
    await screen.findByText('Road Trip');

    expect(screen.queryByTestId('playlist-wide-layout')).toBeNull();
  }, 60000);
});
