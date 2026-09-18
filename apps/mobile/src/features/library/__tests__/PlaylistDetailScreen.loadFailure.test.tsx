// #1705: the detail screen answered every failed load with "Playlist not found" and a
// "Go back" button — a false claim for an offline device or a 5xx, and no way back to
// the playlist short of leaving the screen. Only a 404/410 means it is really gone;
// everything else is transient and gets the retry. Nothing was logged either, so a
// "my playlist won't open" report reached triage with no status and no failure class.

import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { PlaylistDetailScreen } from '../ui/PlaylistDetailScreen';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ id: 'p1' }),
  useRouter: () => ({
    replace: jest.fn(),
    push: jest.fn(),
    back: jest.fn(),
    canGoBack: () => false,
  }),
}));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

jest.mock('@shared/playback/usePlayback', () => ({
  usePlayback: () => ({ status: 'idle', source: null }),
}));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({}),
}));

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

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { gcTime: Infinity },
    },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

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

describe('PlaylistDetailScreen — a failed load', () => {
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
