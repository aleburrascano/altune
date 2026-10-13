import { act, renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';

import { useAlbumDetailState } from '@features/detail/hooks/useAlbumDetailState';
import {
  createTestQueryClient,
  createPlaybackWrapper,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));
const ALBUM_TRACK_COUNT = 2;

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

async function renderAlbum(tracks: readonly DiscoveryResult[]) {
  __http.reply('GET /v1/discovery/albums/deezer/alb1/tracks', {
    status: 200,
    json: { items: tracks, provider_name: 'deezer', status: 'ok' },
  });
  const rendered = renderHook(() => useAlbumDetailState(albumResult, '/discover/detail'), {
    wrapper: createPlaybackWrapper(createTestQueryClient({ mutations: true })),
  });
  await waitFor(() => expect(rendered.result.current.tracks).toHaveLength(tracks.length));
  return rendered;
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

beforeEach(async () => {
  await signInTestUser();
  __http.reply('GET /v1/tracks', {
    status: 200,
    json: { items: [], total: 0, limit: 200, offset: 0, has_more: false },
  });
  __http.reply('POST /v1/tracks', { status: 201, json: savedTrack() });
  __http.reply('POST /v1/discovery/events', { status: 202 });
  useTrackStatusStore.getState().reset();
});

afterEach(() => {
  useTrackStatusStore.getState().reset();
});

describe('a track written by its own row and by "Save all" at once', () => {
  it('presents one idempotency key for both writes, so the server can collapse them', async () => {
    const tracks = [albumTrack('Nude'), albumTrack('Reckoner')];
    const { result } = await renderAlbum(tracks);

    await act(async () => {
      result.current.onQuickSave(tracks[0]!);
      result.current.onSaveAll();
    });

    await waitFor(() => expect(keysFor('Nude')).toHaveLength(ALBUM_TRACK_COUNT));
    const nudeKeys = keysFor('Nude');
    expect(nudeKeys[1]).toBe(nudeKeys[0]);
  });

  it('leaves another track on its own key, so one save cannot swallow a different track', async () => {
    const tracks = [albumTrack('Nude'), albumTrack('Reckoner')];
    const { result } = await renderAlbum(tracks);

    await act(async () => {
      result.current.onSaveAll();
    });

    await waitFor(() => expect(saveWrites()).toHaveLength(ALBUM_TRACK_COUNT));
    expect(keysFor('Reckoner')[0]).not.toBe(keysFor('Nude')[0]);
  });
});
