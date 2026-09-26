import { QueryClient } from '@tanstack/react-query';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';
import { PLAYLIST_HANDLERS } from '../playlistEvents';
import type { ServerEvent } from '../sse-client';

// The detail, the list and the library grid's pages: the three caches a removal patches, and
// the ceiling a whole batch must stay under however many tracks it names.
const PATCHED_CACHES = 3;

const BATCH_PLAYLIST = 3000;

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

function cachedDetail(queryClient: QueryClient): PlaylistDetailResponse {
  return queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!;
}

function serverEvent(type: string, data: Record<string, unknown>): ServerEvent {
  return { id: '1', type, data };
}

describe('removing a large batch of tracks from a tracks_removed_from_playlist event', () => {
  it('reads and writes the caches a bounded number of times, not once per track removed', () => {
    const queryClient = new QueryClient();
    const tracks = makeTracks(BATCH_PLAYLIST);
    seedPlaylist(queryClient, tracks);
    const removedIds = tracks.slice(0, BATCH_PLAYLIST / 2).map((t) => t.id);
    const reads = jest.spyOn(queryClient, 'getQueryData');
    const writes = jest.spyOn(queryClient, 'setQueryData');

    PLAYLIST_HANDLERS.tracks_removed_from_playlist(
      queryClient,
      serverEvent('tracks_removed_from_playlist', { playlist_id: 'p1', track_ids: removedIds }),
    );

    expect(writes.mock.calls).toHaveLength(PATCHED_CACHES);
    expect(reads.mock.calls).toHaveLength(1);
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
