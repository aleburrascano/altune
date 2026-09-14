// Failure paths of the track mutation hooks (#788): a failed mutation restores the
// cache it optimistically patched, logs the real error with track id and endpoint,
// and the bulk delete keeps each item's cause.

import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider, type InfiniteData } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client/errors';
import { asPlaylistId, asTrackId, type TrackId } from '@shared/api-client/ids';
import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { RETRY_TAIL } from '@shared/lib/describeError';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

import { useDeleteTrack, useDeleteTracks } from '../hooks/useDeleteTrack';
import { useReacquireTrack } from '../hooks/useReacquireTrack';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';

const mockDeleteTrack = jest.fn<Promise<void>, [TrackId]>();
const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();
const mockReacquireTrack = jest.fn<Promise<void>, [TrackId]>();
jest.mock('@shared/api-client/tracks', () => ({
  deleteTrack: (id: TrackId) => mockDeleteTrack(id),
  retryAcquisition: (id: TrackId) => mockRetryAcquisition(id),
  reacquireTrack: (id: TrackId) => mockReacquireTrack(id),
}));

function track(id: string, overrides: Partial<TrackResponse> = {}): TrackResponse {
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
  };
}

function page(items: TrackResponse[], total = items.length): ListTracksResponse {
  return { items, total, limit: 20, offset: 0, has_more: false };
}

function playlist(tracks: TrackResponse[]): PlaylistDetailResponse {
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

const TRACKS_KEY = libraryKeys.tracks('', 'added');
const PLAYLIST_KEY = playlistKeys.detail('pl1');

function setup() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }
  return { queryClient, wrapper: Wrapper };
}

function pagedIds(queryClient: QueryClient): string[] {
  const data = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(TRACKS_KEY)!;
  return data.pages.flatMap((p) => p.items.map((t) => t.id));
}

let alertSpy: jest.SpyInstance;
let warnSpy: jest.SpyInstance;

beforeEach(() => {
  mockDeleteTrack.mockReset();
  mockRetryAcquisition.mockReset();
  mockReacquireTrack.mockReset();
  useTrackStatusStore.getState().reset();
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  alertSpy.mockRestore();
  warnSpy.mockRestore();
});

describe('useRetryAcquisition — a failed retry does not strand the track in fake pending', () => {
  it('restores the prior failed status and failure reason in every cache', async () => {
    const { queryClient, wrapper } = setup();
    const failed = track('t1', { acquisition_status: 'failed', failure_reason: 'no source' });
    queryClient.setQueryData(TRACKS_KEY, { pages: [page([failed])], pageParams: [0] });
    queryClient.setQueryData(PLAYLIST_KEY, playlist([failed]));
    let reject!: (e: Error) => void;
    mockRetryAcquisition.mockReturnValue(new Promise((_, r) => (reject = r)));

    const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));

    await waitFor(() =>
      expect(
        queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!
          .acquisition_status,
      ).toBe('pending'),
    );
    await act(async () => reject(new ApiError(503, 'unavailable')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!;
    expect(detail.acquisition_status).toBe('failed');
    expect(detail.failure_reason).toBe('no source');
    const paged = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(TRACKS_KEY)!;
    expect(paged.pages[0]!.items[0]!.acquisition_status).toBe('failed');
    expect(paged.pages[0]!.items[0]!.failure_reason).toBe('no source');
  });

  it('does not overwrite a status a server event already moved past pending', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(
      PLAYLIST_KEY,
      playlist([track('t1', { acquisition_status: 'failed' })]),
    );
    let reject!: (e: Error) => void;
    mockRetryAcquisition.mockReturnValue(new Promise((_, r) => (reject = r)));

    const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalled());
    queryClient.setQueryData(
      PLAYLIST_KEY,
      playlist([track('t1', { acquisition_status: 'ready' })]),
    );
    await act(async () => reject(new Error('boom')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!.acquisition_status,
    ).toBe('ready');
  });

  it('logs the real error with the track id and endpoint, and alerts with the shared tail', async () => {
    const { wrapper } = setup();
    const error = new ApiError(500, 'internal');
    mockRetryAcquisition.mockRejectedValue(error);

    const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(warnSpy).toHaveBeenCalledWith('[library] retry acquisition failed', {
      trackId: 't1',
      endpoint: 'POST /v1/tracks/t1/retry',
      status: 500,
      error,
    });
    expect(alertSpy).toHaveBeenCalledWith(
      'Retry failed',
      `Could not restart acquisition. ${RETRY_TAIL}`,
    );
  });
});

describe('useDeleteTrack — a failed delete puts the track back', () => {
  it('re-inserts the track at its original position in every cache it was removed from', async () => {
    const { queryClient, wrapper } = setup();
    const target = track('t2');
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('t1'), target, track('t3')], 10)],
      pageParams: [0],
    });
    queryClient.setQueryData(libraryKeys.featuring('who'), page([target], 1));
    queryClient.setQueryData(PLAYLIST_KEY, playlist([track('t9'), target]));
    useTrackStatusStore
      .getState()
      .patch('t2', { acquisitionStatus: 'ready', failureMessage: null });
    let reject!: (e: Error) => void;
    mockDeleteTrack.mockReturnValue(new Promise((_, r) => (reject = r)));

    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t2')));
    await waitFor(() => expect(pagedIds(queryClient)).toEqual(['t1', 't3']));

    await act(async () => reject(new ApiError(500, 'internal')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(pagedIds(queryClient)).toEqual(['t1', 't2', 't3']);
    expect(
      queryClient.getQueryData<InfiniteData<ListTracksResponse>>(TRACKS_KEY)!.pages[0]!.total,
    ).toBe(10);
    expect(queryClient.getQueryData<ListTracksResponse>(libraryKeys.featuring('who'))).toEqual(
      page([target], 1),
    );
    const detail = queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t9', 't2']);
    expect(detail.track_count).toBe(2);
    expect(useTrackStatusStore.getState().statuses['t2']).toEqual({
      acquisitionStatus: 'ready',
      failureMessage: null,
    });
  });

  it('logs the real error with the track id and endpoint', async () => {
    const { wrapper } = setup();
    const error = new Error('Network request failed');
    mockDeleteTrack.mockRejectedValue(error);

    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t2')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(warnSpy).toHaveBeenCalledWith('[library] delete track failed', {
      trackId: 't2',
      endpoint: 'DELETE /v1/tracks/t2',
      status: undefined,
      error,
    });
    expect(alertSpy).toHaveBeenCalledWith(
      'Delete failed',
      `Could not remove the track. ${RETRY_TAIL}`,
    );
  });

  it('leaves the track removed when the delete succeeds', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('t1'), track('t2')])],
      pageParams: [0],
    });
    mockDeleteTrack.mockResolvedValue(undefined);

    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t2')));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(pagedIds(queryClient)).toEqual(['t1']);
    expect(warnSpy).not.toHaveBeenCalled();
  });
});

describe('useDeleteTracks — each failed item keeps its cause', () => {
  it('reports every failure with its own error and removes only the successes', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('a'), track('b'), track('c')])],
      pageParams: [0],
    });
    const notFound = new ApiError(404, 'not found');
    const unauthorized = new ApiError(401, 'unauthorized');
    mockDeleteTrack.mockImplementation((id) => {
      if (id === 'a') return Promise.reject(notFound);
      if (id === 'c') return Promise.reject(unauthorized);
      return Promise.resolve();
    });

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate([asTrackId('a'), asTrackId('b'), asTrackId('c')]));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual({
      deleted: 1,
      requested: 3,
      failures: [
        { trackId: 'a', error: notFound },
        { trackId: 'c', error: unauthorized },
      ],
    });
    expect(pagedIds(queryClient)).toEqual(['a', 'c']);
    expect(warnSpy).toHaveBeenCalledWith('[library] delete track failed', {
      trackId: 'a',
      endpoint: 'DELETE /v1/tracks/a',
      status: 404,
      error: notFound,
    });
    expect(warnSpy).toHaveBeenCalledWith('[library] delete track failed', {
      trackId: 'c',
      endpoint: 'DELETE /v1/tracks/c',
      status: 401,
      error: unauthorized,
    });
    expect(alertSpy).toHaveBeenCalledWith(
      'Delete failed',
      `2 of 3 tracks could not be removed. ${RETRY_TAIL}`,
    );
  });
});

describe('useReacquireTrack — failure diagnostics and copy', () => {
  it('logs the real error with the track id and endpoint, and alerts with the shared tail', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(PLAYLIST_KEY, playlist([track('t1')]));
    const error = new ApiError(409, 'conflict');
    mockReacquireTrack.mockRejectedValue(error);

    const { result } = renderHook(() => useReacquireTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(warnSpy).toHaveBeenCalledWith('[library] re-acquire track failed', {
      trackId: 't1',
      endpoint: 'POST /v1/tracks/t1/reacquire',
      status: 409,
      error,
    });
    expect(alertSpy).toHaveBeenCalledWith(
      'Re-acquire failed',
      `Could not start a re-acquisition. Your current audio is unchanged. ${RETRY_TAIL}`,
    );
    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!.acquisition_status,
    ).toBe('ready');
  });
});
