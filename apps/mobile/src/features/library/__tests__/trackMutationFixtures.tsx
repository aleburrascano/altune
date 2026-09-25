// Shared setup for the track mutation hook tests (useDeleteTrack, useDeleteTracks,
// useRetryAcquisition, useReacquireTrack): the track and cache shapes they seed, and
// the sign-out-then-next-user sequence of #2729. Each test file still mocks
// @shared/api-client/tracks itself, since jest.mock only applies to the file that calls it.

import React from 'react';
import { QueryClient, QueryClientProvider, type InfiniteData } from '@tanstack/react-query';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import type { TrackStatus } from '@shared/acquisition/trackStatusStore';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

export function track(id: string, overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: asTrackId(id),
    title: `Track ${id}`,
    artist: 'An Artist',
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

export function page(items: TrackResponse[], total = items.length): ListTracksResponse {
  return { items, total, limit: 20, offset: 0, has_more: false };
}

export function playlist(tracks: TrackResponse[]): PlaylistDetailResponse {
  return {
    id: asPlaylistId('pl1'),
    name: 'Road Trip',
    track_count: tracks.length,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks,
  };
}

export const TRACKS_KEY = libraryKeys.tracks('', 'added');
export const LIBRARY_DERIVED = [
  libraryKeys.albumsPrefix,
  libraryKeys.artistsPrefix,
  libraryKeys.summary,
  libraryKeys.lookupPrefix,
];

export function invalidatedKeys(spy: jest.SpyInstance): unknown[] {
  return spy.mock.calls.map(([filters]) => (filters as { queryKey: unknown }).queryKey);
}
export const PLAYLIST_KEY = playlistKeys.detail(asPlaylistId('pl1'));

export function setup() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }
  return { queryClient, wrapper: Wrapper };
}

export function pagedIds(queryClient: QueryClient): string[] {
  const data = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(TRACKS_KEY)!;
  return data.pages.flatMap((p) => p.items.map((t) => t.id));
}

type Deferred = { promise: Promise<void>; resolve: () => void; reject: (e: Error) => void };

export function deferred(): Deferred {
  let resolve!: () => void;
  let reject!: (e: Error) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

export const READY: TrackStatus = { acquisitionStatus: 'ready', failureMessage: null };

export function libraryOf(tracks: TrackResponse[]): InfiniteData<ListTracksResponse> {
  return {
    pages: [{ items: tracks, total: tracks.length, limit: 20, offset: 0, has_more: false }],
    pageParams: [0],
  };
}

export function signOutThenLoadUserB(queryClient: QueryClient, userBTracks: TrackResponse[]): void {
  runSignOutCleanups();
  queryClient.clear();
  useTrackStatusStore.getState().reset();
  queryClient.setQueryData(TRACKS_KEY, libraryOf(userBTracks));
  useTrackStatusStore.getState().patch(asTrackId('b1'), READY);
}

export function userBLibrary(
  queryClient: QueryClient,
): InfiniteData<ListTracksResponse> | undefined {
  return queryClient.getQueryData<InfiniteData<ListTracksResponse>>(TRACKS_KEY);
}
