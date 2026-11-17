import React from 'react';
import {
  QueryClient,
  QueryClientProvider,
  type InfiniteData,
  type InvalidateOptions,
  type InvalidateQueryFilters,
  type QueryKey,
} from '@tanstack/react-query';
import { asPlaylistId, asTrackId } from '@shared/catalog';
import type { ListTracksResponse, TrackResponse, PlaylistDetailResponse } from '@shared/catalog';
import {
  useTrackStatusStore,
  AcquisitionPortProvider,
  createMemoryAcquisitionPort,
} from '@shared/acquisition';
import type { TrackStatus, AcquisitionPort } from '@shared/acquisition';
import { libraryKeys, playlistKeys } from '@shared/query';
import { runSignOutCleanups } from '@shared/session';
import { LibraryPortProvider, createMemoryLibraryPort, recordingDialogs } from '@features/library';
import type { LibraryPort } from '@features/library';
import { trackResponseFactory } from '@testing/factories';

export function track(id: string, overrides: Partial<TrackResponse> = {}): TrackResponse {
  return trackResponseFactory.build({
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
  } as TrackResponse);
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

class RecordingQueryClient extends QueryClient {
  readonly invalidated: (QueryKey | undefined)[] = [];

  override invalidateQueries<TTaggedQueryKey extends QueryKey = QueryKey>(
    filters?: InvalidateQueryFilters<TTaggedQueryKey>,
    options?: InvalidateOptions,
  ): Promise<void> {
    this.invalidated.push(filters?.queryKey);
    return super.invalidateQueries(filters, options);
  }
}
export const PLAYLIST_KEY = playlistKeys.detail(asPlaylistId('pl1'));

export interface PortOverrides {
  library?: Partial<LibraryPort>;
  acquisition?: Partial<AcquisitionPort>;
}

export function setup({ library, acquisition }: PortOverrides = {}) {
  const queryClient = new RecordingQueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const dialogs = recordingDialogs();
  const libraryPort: LibraryPort = {
    ...createMemoryLibraryPort({
      showFailure: dialogs.showFailure,
      confirmDestructive: dialogs.confirmDestructive,
    }),
    ...library,
  };
  const acquisitionPort: AcquisitionPort = {
    ...createMemoryAcquisitionPort().port,
    ...acquisition,
  };
  function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <LibraryPortProvider port={libraryPort}>
          <AcquisitionPortProvider port={acquisitionPort}>{children}</AcquisitionPortProvider>
        </LibraryPortProvider>
      </QueryClientProvider>
    );
  }
  return { queryClient, wrapper: Wrapper, dialogs };
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
