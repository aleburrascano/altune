import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider, type InfiniteData } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/errors';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import type { TrackStatus } from '@shared/acquisition/trackStatusStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { libraryKeys } from '@shared/lib/query-keys';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import { BULK_DELETE_CONCURRENCY, useDeleteTracks } from '../hooks/useDeleteTracks';
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

type Deferred = { promise: Promise<void>; resolve: () => void; reject: (e: Error) => void };

function deferred(): Deferred {
  let resolve!: () => void;
  let reject!: (e: Error) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

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
  } as TrackResponse;
}

const TRACKS_KEY = libraryKeys.tracks('', 'added');
const READY: TrackStatus = { acquisitionStatus: 'ready', failureMessage: null };

function libraryOf(tracks: TrackResponse[]): InfiniteData<ListTracksResponse> {
  return {
    pages: [{ items: tracks, total: tracks.length, limit: 20, offset: 0, has_more: false }],
    pageParams: [0],
  };
}

function setup() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }
  return { queryClient, wrapper: Wrapper };
}

function signOutThenLoadUserB(queryClient: QueryClient, userBTracks: TrackResponse[]): void {
  runSignOutCleanups();
  queryClient.clear();
  useTrackStatusStore.getState().reset();
  queryClient.setQueryData(TRACKS_KEY, libraryOf(userBTracks));
  useTrackStatusStore.getState().patch(asTrackId('b1'), READY);
}

function userBLibrary(queryClient: QueryClient): InfiniteData<ListTracksResponse> | undefined {
  return queryClient.getQueryData<InfiniteData<ListTracksResponse>>(TRACKS_KEY);
}

let alertSpy: jest.SpyInstance;
let warnSpy: jest.SpyInstance;

beforeEach(() => {
  mockDeleteTrack.mockReset();
  mockRetryAcquisition.mockReset();
  mockReacquireTrack.mockReset();
  useTrackStatusStore.getState().reset();
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  alertSpy.mockRestore();
  warnSpy.mockRestore();
});

describe('track mutations that settle after sign-out leave the next user untouched (#2729)', () => {
  it("useDeleteTrack: a delete failing after sign-out restores nothing into user B's library, status store or alerts", async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, libraryOf([track('a1'), track('a2')]));
    useTrackStatusStore.getState().patch(asTrackId('a1'), READY);
    const request = deferred();
    mockDeleteTrack.mockReturnValue(request.promise);
    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('a1')));
    await waitFor(() => expect(mockDeleteTrack).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient, [track('b1')]);
    await act(async () => request.reject(new ApiError(500, 'internal')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(userBLibrary(queryClient)).toEqual(libraryOf([track('b1')]));
    expect(useTrackStatusStore.getState().statuses).toEqual({ b1: READY });
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('useDeleteTracks: a bulk delete stops sending once its user signs out and reports nothing to the next', async () => {
    const { queryClient, wrapper } = setup();
    const ids = ['a1', 'a2', 'a3', 'a4', 'a5', 'a6'].map(asTrackId);
    const requests = ids.map(() => deferred());
    mockDeleteTrack.mockImplementation((id) => requests[ids.indexOf(id)]!.promise);
    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate(ids));
    await waitFor(() => expect(mockDeleteTrack).toHaveBeenCalledTimes(BULK_DELETE_CONCURRENCY));

    signOutThenLoadUserB(queryClient, [track('a1'), track('b1')]);
    await act(async () => {
      requests[0]!.resolve();
      requests
        .slice(1, BULK_DELETE_CONCURRENCY)
        .forEach((r) => r.reject(new ApiError(500, 'internal')));
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(mockDeleteTrack).toHaveBeenCalledTimes(BULK_DELETE_CONCURRENCY);
    expect(userBLibrary(queryClient)).toEqual(libraryOf([track('a1'), track('b1')]));
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it("useRetryAcquisition: a retry failing after sign-out rolls nothing back into user B's cache and does not alert", async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(
      TRACKS_KEY,
      libraryOf([track('a1', { acquisition_status: 'failed' })]),
    );
    const request = deferred();
    mockRetryAcquisition.mockReturnValue(request.promise);
    const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
    act(() => result.current.mutate(asTrackId('a1')));
    await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalled());

    const userBTracks = [track('a1', { acquisition_status: 'pending' }), track('b1')];
    signOutThenLoadUserB(queryClient, userBTracks);
    await act(async () => request.reject(new ApiError(500, 'internal')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(userBLibrary(queryClient)).toEqual(libraryOf(userBTracks));
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it("useReacquireTrack: a re-acquire answering after sign-out does not mark user B's track pending", async () => {
    const { queryClient, wrapper } = setup();
    const request = deferred();
    mockReacquireTrack.mockReturnValue(request.promise);
    const { result } = renderHook(() => useReacquireTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('a1')));
    await waitFor(() => expect(mockReacquireTrack).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient, [track('a1'), track('b1')]);
    await act(async () => request.resolve());
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(userBLibrary(queryClient)).toEqual(libraryOf([track('a1'), track('b1')]));
  });

  it("useReacquireTrack: a not-found answer after sign-out drops nothing from user B's library and does not alert", async () => {
    const { queryClient, wrapper } = setup();
    const request = deferred();
    mockReacquireTrack.mockReturnValue(request.promise);
    const { result } = renderHook(() => useReacquireTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('a1')));
    await waitFor(() => expect(mockReacquireTrack).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient, [track('a1'), track('b1')]);
    await act(async () => request.reject(new ApiError(404, 'not found')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(userBLibrary(queryClient)).toEqual(libraryOf([track('a1'), track('b1')]));
    expect(alertSpy).not.toHaveBeenCalled();
  });
});
