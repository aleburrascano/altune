import { QueryClient, type InfiniteData } from '@tanstack/react-query';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/trackResponse';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import { libraryKeys } from '@shared/query/queryKeys';

export function makeTrack(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: asTrackId('t1'),
    title: 'Track One',
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
    ...overrides,
  } as TrackResponse;
}

export function makePage(
  items: TrackResponse[],
  overrides: Partial<ListTracksResponse> = {},
): ListTracksResponse {
  return { items, total: items.length, limit: 20, offset: 0, has_more: false, ...overrides };
}

export function makeInfinite(pages: ListTracksResponse[]): InfiniteData<ListTracksResponse> {
  return { pages, pageParams: pages.map((_, i) => i) };
}

export function makePlaylistDetail(
  id: string,
  tracks: TrackResponse[],
  overrides: Partial<PlaylistDetailResponse> = {},
): PlaylistDetailResponse {
  return {
    id: asPlaylistId(id),
    name: 'My Playlist',
    track_count: tracks.length,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks,
    ...overrides,
  };
}

export function newClient(): QueryClient {
  return new QueryClient();
}

export function registerUnfetchedQuery(client: QueryClient, queryKey: readonly unknown[]): void {
  client.getQueryCache().build(client, { queryKey });
}

export function seedTracksPrefix(client: QueryClient, pages: ListTracksResponse[]): void {
  client.setQueryData(libraryKeys.tracks('q', 'sort'), makeInfinite(pages));
}
