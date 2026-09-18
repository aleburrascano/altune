// Two writes about one track must land on one library row. The row's own quick-save
// and the "Save all" batch both POST /v1/tracks, and the server only collapses them
// when both carry the same Idempotency-Key — with a per-call random key it never can,
// so the overlap writes the track twice (#1658).

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';

import { useAlbumDetailState } from '../hooks/useAlbumDetailState';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({ playFromList: jest.fn() }),
}));
// A landed save enqueues library_add telemetry, whose own flush/retry schedule has
// nothing to do with the write key under test.
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));
jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracksForAlbum: () => [],
}));
jest.mock('../hooks/useAlbumDiscovery', () => ({
  useAlbumDiscovery: () => ({
    tracks: [],
    isLoading: false,
    isError: false,
    failure: null,
    refetch: jest.fn(),
  }),
}));

const mockUseAlbumTracks = jest.fn();
jest.mock('../hooks/useAlbumTracks', () => ({
  useAlbumTracks: () => mockUseAlbumTracks(),
}));

const albumResult: DiscoveryResult = {
  kind: 'album',
  title: 'In Rainbows',
  subtitle: 'Radiohead',
  image_url: null,
  confidence: 'high',
  sources: [{ provider: 'deezer', external_id: 'alb1', url: 'https://deezer/alb1' }],
  extras: {},
};

function albumTrack(title: string): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Radiohead',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

function savedTrack() {
  return {
    id: 'srv-1',
    title: 'Nude',
    artist: 'Radiohead',
    album: 'In Rainbows',
    duration_seconds: null,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'pending',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

function renderAlbum(tracks: readonly DiscoveryResult[]) {
  mockUseAlbumTracks.mockReturnValue({
    tracks,
    isLoading: false,
    isError: false,
    failure: null,
    refetch: jest.fn(),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return renderHook(() => useAlbumDetailState(albumResult, '/discover/detail'), {
    wrapper: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
}

type SaveWrite = { title: string; key: string };

function saveWrites(): SaveWrite[] {
  return __http.requests
    .filter((r: { method: string; path: string }) => r.method === 'POST' && r.path === '/v1/tracks')
    .map((r: { body: string; headers: Record<string, string> }) => ({
      title: JSON.parse(r.body).title as string,
      key: r.headers['Idempotency-Key'] as string,
    }));
}

function keysFor(title: string): string[] {
  return saveWrites()
    .filter((write) => write.title === title)
    .map((write) => write.key);
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  __http.reply('POST /v1/tracks', { status: 201, json: savedTrack() });
  __http.reply('POST /v1/discovery/events', { status: 202 });
  mockUseAlbumTracks.mockReset();
  useTrackStatusStore.getState().reset();
});

afterEach(() => {
  useTrackStatusStore.getState().reset();
});

describe('a track written by its own row and by "Save all" at once', () => {
  it('presents one idempotency key for both writes, so the server can collapse them', async () => {
    const tracks = [albumTrack('Nude'), albumTrack('Reckoner')];
    const { result } = renderAlbum(tracks);

    // Both dispatches leave before either response lands — the overlap itself.
    await act(async () => {
      result.current.onQuickSave(tracks[0]!);
      result.current.onSaveAll();
    });

    const nudeKeys = keysFor('Nude');
    expect(nudeKeys).toHaveLength(2);
    expect(nudeKeys[1]).toBe(nudeKeys[0]);
  });

  it('leaves another track on its own key, so one save cannot swallow a different track', async () => {
    const tracks = [albumTrack('Nude'), albumTrack('Reckoner')];
    const { result } = renderAlbum(tracks);

    await act(async () => {
      result.current.onSaveAll();
    });

    expect(keysFor('Reckoner')[0]).not.toBe(keysFor('Nude')[0]);
  });
});
