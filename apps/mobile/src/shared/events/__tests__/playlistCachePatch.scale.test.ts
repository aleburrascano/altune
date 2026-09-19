import { QueryClient } from '@tanstack/react-query';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';

import { reorderPlaylistCache } from '../playlistCachePatch';
import { PLAYLIST_HANDLERS } from '../playlistEvents';
import type { ServerEvent } from '../sse-client';

// The two playlist sizes a reorder is timed at: a short one and an "everything" playlist,
// sixteen times longer. A patch that scans the named ids once per cached track measures five
// to ten times more per track on the long one; one that looks them up measures about the same.
const SHORT_PLAYLIST = 1000;
const LONG_PLAYLIST = 16000;

// Allocation and the runner's scheduler add noise on top of the per-track work; three times
// leaves room for both while still failing anything that walks the id list per track.
const TOLERATED_GROWTH = 3;

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

// jsdom rounds performance.now() to whole milliseconds, which is coarser than a reorder of a
// short playlist takes; the process clock is what can tell one from sixteen.
function nowMs(): number {
  return Number(process.hrtime.bigint()) / 1e6;
}

// Milliseconds per track to reverse a playlist of `size` tracks, the reorder a drag from the
// top to the bottom of the list produces.
function msPerTrack(size: number): number {
  const queryClient = new QueryClient();
  const tracks = makeTracks(size);
  seedPlaylist(queryClient, tracks);
  const reversedIds = tracks.map((t) => t.id).reverse();

  const startedAt = nowMs();
  reorderPlaylistCache(queryClient, asPlaylistId('p1'), reversedIds);

  return (nowMs() - startedAt) / size;
}

// The fastest of three runs: a slow run can only come from noise, never from the patch being
// cheaper than it is, so the minimum is the measurement least able to flake. The discarded
// first run is what stops a cold JIT from inflating whichever size is measured first.
function fastestMsPerTrack(size: number): number {
  msPerTrack(size);
  return Math.min(msPerTrack(size), msPerTrack(size), msPerTrack(size));
}

describe('reordering a whole playlist from a playlist_reordered event', () => {
  it('costs the same per track on a long playlist as on a short one, so the patch stays linear in its length', () => {
    const onShort = fastestMsPerTrack(SHORT_PLAYLIST);

    const onLong = fastestMsPerTrack(LONG_PLAYLIST);

    expect(onLong).toBeLessThanOrEqual(onShort * TOLERATED_GROWTH);
  });

  it('lands every track in the order the event named, so the cheap patch still reorders', () => {
    const queryClient = new QueryClient();
    const tracks = makeTracks(LONG_PLAYLIST);
    seedPlaylist(queryClient, tracks);

    reorderPlaylistCache(queryClient, asPlaylistId('p1'), tracks.map((t) => t.id).reverse());

    const reordered = cachedDetail(queryClient).tracks;
    expect(reordered).toHaveLength(LONG_PLAYLIST);
    expect(reordered[0]!.id).toBe(`t${LONG_PLAYLIST - 1}`);
    expect(reordered[LONG_PLAYLIST - 1]!.id).toBe('t0');
  });
});

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
