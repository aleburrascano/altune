import { QueryClient } from '@tanstack/react-query';

import { asPlaylistId, asTrackId } from '@shared/apiClient';
import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/apiClient';
import { playlistKeys } from '@shared/query';
import { PLAYLIST_HANDLERS } from '@shared/events';
import type { JsonObject } from '@shared/files';
import type { ServerEvent } from '@shared/events';

import { recordingCache } from '@shared/events/__tests__/recordingCache';

const PATCHED_CACHES = 3;

const BATCH_PLAYLIST = 3000;
const LISTED_COUNT = 9;
const NUMERIC_ID = 7;

function makeTrack(id: string): TrackResponse {
  return {
    id: asTrackId(id),
    title: `Track ${id}`,
    artist: 'Artist One',
    album: null,
    duration_seconds: 180,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  } as TrackResponse;
}

function makeTracks(count: number): TrackResponse[] {
  return Array.from({ length: count }, (_unused, i) => makeTrack(`t${i}`));
}

function seedPlaylist(queryClient: QueryClient, tracks: TrackResponse[]): void {
  queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')), {
    id: asPlaylistId('p1'),
    name: 'Everything',
    track_count: tracks.length,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks,
  });
  queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, {
    items: [
      {
        id: asPlaylistId('p1'),
        name: 'Everything',
        track_count: tracks.length,
        preview_artwork_urls: [],
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      },
    ],
    total: 1,
  });
}

function listedCount(queryClient: QueryClient): number | undefined {
  return queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)?.items[0]?.track_count;
}

function reviseListedCount(queryClient: QueryClient, trackCount: number): void {
  queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, (prev) =>
    prev ? { ...prev, items: prev.items.map((p) => ({ ...p, track_count: trackCount })) } : prev,
  );
}

function cachedDetail(queryClient: QueryClient): PlaylistDetailResponse {
  return queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!;
}

function serverEvent(type: string, data: JsonObject): ServerEvent {
  return { id: '1', type, data };
}

describe('removing a large batch of tracks from a tracks_removed_from_playlist event', () => {
  it('reads and writes the caches a bounded number of times, not once per track removed', () => {
    const queryClient = new QueryClient();
    const tracks = makeTracks(BATCH_PLAYLIST);
    seedPlaylist(queryClient, tracks);
    const removedIds = tracks.slice(0, BATCH_PLAYLIST / 2).map((t) => t.id);
    const cache = recordingCache(queryClient);

    PLAYLIST_HANDLERS.tracks_removed_from_playlist(
      cache,
      serverEvent('tracks_removed_from_playlist', { playlist_id: 'p1', track_ids: removedIds }),
    );

    expect(cache.writes).toHaveLength(PATCHED_CACHES);
    expect(cache.reads).toHaveLength(1);
  });

  it('leaves exactly the tracks the batch did not name, with both track counts agreeing', () => {
    const queryClient = new QueryClient();
    const tracks = makeTracks(BATCH_PLAYLIST);
    seedPlaylist(queryClient, tracks);
    const removedIds = tracks.slice(0, BATCH_PLAYLIST / 2).map((t) => t.id);

    PLAYLIST_HANDLERS.tracks_removed_from_playlist(
      queryClient,
      serverEvent('tracks_removed_from_playlist', { playlist_id: 'p1', track_ids: removedIds }),
    );

    const detail = cachedDetail(queryClient);
    expect(detail.tracks).toHaveLength(BATCH_PLAYLIST / 2);
    expect(detail.tracks[0]!.id).toBe(`t${BATCH_PLAYLIST / 2}`);
    expect(detail.track_count).toBe(BATCH_PLAYLIST / 2);
    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(BATCH_PLAYLIST / 2);
  });
});

describe('playlist events that name no usable target', () => {
  it.each<[keyof typeof PLAYLIST_HANDLERS, JsonObject]>([
    ['track_removed_from_playlist', { track_id: 't1' }],
    ['track_removed_from_playlist', { playlist_id: 'p1' }],
    ['tracks_removed_from_playlist', { track_ids: ['t1'] }],
    ['tracks_removed_from_playlist', { playlist_id: 'p1' }],
    ['tracks_removed_from_playlist', { playlist_id: 'p1', track_ids: [] }],
    ['playlist_reordered', { track_ids: ['t1'] }],
    ['playlist_reordered', { playlist_id: 'p1' }],
  ])('writes no cache for %s with %j', (type, data) => {
    const queryClient = new QueryClient();
    seedPlaylist(queryClient, makeTracks(PATCHED_CACHES));
    const cache = recordingCache(queryClient);

    PLAYLIST_HANDLERS[type](cache, serverEvent(type, data));

    expect(cache.writes).toEqual([]);
  });
});

describe('a removal the cached playlist detail does not hold', () => {
  it('keeps the listed track count when no named track was in the detail', () => {
    const queryClient = new QueryClient();
    seedPlaylist(queryClient, makeTracks(PATCHED_CACHES));
    reviseListedCount(queryClient, LISTED_COUNT);

    PLAYLIST_HANDLERS.tracks_removed_from_playlist(
      queryClient,
      serverEvent('tracks_removed_from_playlist', { playlist_id: 'p1', track_ids: ['elsewhere'] }),
    );

    expect(listedCount(queryClient)).toBe(LISTED_COUNT);
  });

  it('counts only the string ids when no detail is cached', () => {
    const queryClient = new QueryClient();
    seedPlaylist(queryClient, makeTracks(PATCHED_CACHES));
    queryClient.removeQueries({ queryKey: playlistKeys.detail(asPlaylistId('p1')) });

    PLAYLIST_HANDLERS.tracks_removed_from_playlist(
      queryClient,
      serverEvent('tracks_removed_from_playlist', {
        playlist_id: 'p1',
        track_ids: ['t0', NUMERIC_ID],
      }),
    );

    expect(listedCount(queryClient)).toBe(PATCHED_CACHES - 1);
  });
});
