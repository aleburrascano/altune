// Failure paths of the track mutation hooks (#788): a failed mutation restores the
// cache it optimistically patched, logs a redacted line with track id and endpoint,
// and the bulk delete keeps each item's cause.

import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider, type InfiniteData } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/errors';
import { asPlaylistId, asTrackId, type TrackId } from '@shared/api-client/ids';
import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { RETRY_TAIL } from '@shared/lib/describeError';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import {
  BULK_DELETE_CONCURRENCY,
  BULK_DELETE_DEADLINE_MS,
  useDeleteTracks,
} from '../hooks/useDeleteTracks';
import { useReacquireTrack } from '../hooks/useReacquireTrack';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';

// deleteTrack takes no cancellation today. The mock accepts one anyway and records it,
// so #1701's test can see whether an unmount ever cancels a delete already in flight.
const mockDeleteTrack = jest.fn<Promise<void>, [TrackId, AbortSignal?]>();
const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();
const mockReacquireTrack = jest.fn<Promise<void>, [TrackId]>();
jest.mock('@shared/api-client/tracks', () => ({
  deleteTrack: (id: TrackId, signal?: AbortSignal) => mockDeleteTrack(id, signal),
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
  } as TrackResponse;
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
const LIBRARY_DERIVED = [
  libraryKeys.albumsPrefix,
  libraryKeys.artistsPrefix,
  libraryKeys.summary,
  libraryKeys.lookupPrefix,
];

function invalidatedKeys(spy: jest.SpyInstance): unknown[] {
  return spy.mock.calls.map(([filters]) => (filters as { queryKey: unknown }).queryKey);
}
const PLAYLIST_KEY = playlistKeys.detail(asPlaylistId('pl1'));

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
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
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

  it('clears the failure text while pending and restores all of it on rollback (#933)', async () => {
    const { queryClient, wrapper } = setup();
    const failed = track('t1', {
      acquisition_status: 'failed',
      failure_reason: 'no source',
      failure_message: 'No source found',
    });
    queryClient.setQueryData(PLAYLIST_KEY, playlist([failed]));
    let reject!: (e: Error) => void;
    mockRetryAcquisition.mockReturnValue(new Promise((_, r) => (reject = r)));
    const cached = () => queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!;

    const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(cached().acquisition_status).toBe('pending'));
    expect(cached().failure_reason).toBeNull();
    expect(cached().failure_message).toBeNull();

    await act(async () => reject(new ApiError(503, 'unavailable')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(cached()).toMatchObject({
      acquisition_status: 'failed',
      failure_reason: 'no source',
      failure_message: 'No source found',
    });
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

  it('logs the failure with the track id and endpoint, and alerts with the shared tail', async () => {
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
      failure: 'server',
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
      .patch(asTrackId('t2'), { acquisitionStatus: 'ready', failureMessage: null });
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

  it('logs the failure with the track id and endpoint', async () => {
    const { wrapper } = setup();
    const error = new Error('Network request failed');
    mockDeleteTrack.mockRejectedValue(error);

    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t2')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(warnSpy).toHaveBeenCalledWith('[library] delete track failed', {
      trackId: 't2',
      endpoint: 'DELETE /v1/tracks/t2',
      failure: 'unknown',
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

  it('marks the membership-derived caches stale once the delete lands (#938)', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    mockDeleteTrack.mockResolvedValue(undefined);

    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidatedKeys(spy)).toEqual(LIBRARY_DERIVED);
  });

  it('invalidates nothing when the delete fails and the track is restored', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    mockDeleteTrack.mockRejectedValue(new ApiError(500, 'boom'));

    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(spy).not.toHaveBeenCalled();
  });
});

describe('useDeleteTracks — derived caches', () => {
  it('invalidates the membership-derived caches once when any track was deleted (#938)', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    mockDeleteTrack.mockImplementation((id) =>
      id === 'a' ? Promise.reject(new ApiError(500, 'boom')) : Promise.resolve(),
    );

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate([asTrackId('a'), asTrackId('b'), asTrackId('c')]));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidatedKeys(spy)).toEqual(LIBRARY_DERIVED);
  });

  it('invalidates nothing when no track was deleted', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    mockDeleteTrack.mockRejectedValue(new ApiError(500, 'boom'));

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate([asTrackId('a')]));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(spy).not.toHaveBeenCalled();
  });
});

describe('useDeleteTracks — each failed item keeps its cause', () => {
  it('reports every failure with its own error and removes only the successes', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('a'), track('b'), track('c')])],
      pageParams: [0],
    });
    const conflict = new ApiError(409, 'conflict');
    const unauthorized = new ApiError(401, 'unauthorized');
    mockDeleteTrack.mockImplementation((id) => {
      if (id === 'a') return Promise.reject(conflict);
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
        { trackId: 'a', error: conflict },
        { trackId: 'c', error: unauthorized },
      ],
      skipped: 0,
      cancelled: false,
    });
    expect(pagedIds(queryClient)).toEqual(['a', 'c']);
    expect(warnSpy).toHaveBeenCalledWith('[library] delete track failed', {
      trackId: 'a',
      endpoint: 'DELETE /v1/tracks/a',
      status: 409,
      failure: 'unknown',
    });
    expect(warnSpy).toHaveBeenCalledWith('[library] delete track failed', {
      trackId: 'c',
      endpoint: 'DELETE /v1/tracks/c',
      status: 401,
      failure: 'auth',
    });
    expect(alertSpy).toHaveBeenCalledWith(
      'Delete failed',
      `2 of 3 tracks could not be removed. ${RETRY_TAIL}`,
    );
  });
});

// #789: a bulk delete against a dead dependency paid the full request timeout for
// every selected track, one after another, with no cap and no way to stop it.
describe('useDeleteTracks — bounded concurrency, aggregate deadline, cancel on unmount', () => {
  // Mirrors REQUEST_TIMEOUT_MS; importing the api-client barrel would pull in the auth client.
  const REQUEST_TIMEOUT_MS = 15_000;
  const ids = (n: number): TrackId[] => Array.from({ length: n }, (_, i) => asTrackId(`t${i}`));

  it('sends a bounded batch of deletes in parallel instead of one at a time', async () => {
    const { wrapper } = setup();
    mockDeleteTrack.mockReturnValue(new Promise(() => undefined));

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate(ids(20)));

    await waitFor(() => expect(mockDeleteTrack).toHaveBeenCalledTimes(BULK_DELETE_CONCURRENCY));
    await act(async () => undefined);
    expect(mockDeleteTrack).toHaveBeenCalledTimes(BULK_DELETE_CONCURRENCY);
  });

  it('stops sending once the aggregate deadline passes and reports the rest as not removed', async () => {
    jest.useFakeTimers();
    try {
      const { wrapper } = setup();
      // Every request hangs for the full per-request timeout, as with a dead dependency.
      mockDeleteTrack.mockImplementation(
        () =>
          new Promise((_, reject) =>
            setTimeout(() => reject(new Error('timed out')), REQUEST_TIMEOUT_MS),
          ),
      );

      const { result } = renderHook(() => useDeleteTracks(), { wrapper });
      let run!: Promise<unknown>;
      act(() => {
        run = result.current.mutateAsync(ids(100));
      });
      // Deadline plus one in-flight request timeout bounds the whole run.
      await act(async () => {
        await jest.advanceTimersByTimeAsync(BULK_DELETE_DEADLINE_MS + REQUEST_TIMEOUT_MS);
      });
      const outcome = (await run) as { deleted: number; failures: unknown[]; skipped: number };

      expect(mockDeleteTrack.mock.calls.length).toBeLessThan(100);
      expect(outcome.deleted).toBe(0);
      expect(outcome.failures.length + outcome.skipped).toBe(100);
      expect(outcome.skipped).toBeGreaterThan(0);
      expect(alertSpy).toHaveBeenCalledWith(
        'Delete failed',
        `100 of 100 tracks could not be removed. ${RETRY_TAIL}`,
      );
    } finally {
      jest.useRealTimers();
    }
  });

  it('sends nothing more and shows no alert once the owning screen unmounts', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('t0'), track('t1'), track('t9')])],
      pageParams: [0],
    });
    const pending: (() => void)[] = [];
    mockDeleteTrack.mockImplementation(() => new Promise<void>((resolve) => pending.push(resolve)));

    const { result, unmount } = renderHook(() => useDeleteTracks(), { wrapper });
    let run!: Promise<unknown>;
    act(() => {
      run = result.current.mutateAsync(ids(10));
    });
    await waitFor(() => expect(pending).toHaveLength(BULK_DELETE_CONCURRENCY));

    unmount();
    await act(async () => pending.forEach((resolve) => resolve()));
    const outcome = (await run) as { deleted: number; skipped: number };

    expect(mockDeleteTrack).toHaveBeenCalledTimes(BULK_DELETE_CONCURRENCY);
    // The in-flight deletes did land server-side, so the caches still reflect them.
    expect(outcome.deleted).toBe(BULK_DELETE_CONCURRENCY);
    expect(outcome.skipped).toBe(10 - BULK_DELETE_CONCURRENCY);
    expect(pagedIds(queryClient)).toEqual(['t9']);
    expect(alertSpy).not.toHaveBeenCalled();
  });

  // #1701: unmount stops the run from sending more, and nothing beyond that. A delete
  // already sent is never cancelled — cancelling it would not un-delete the track
  // server-side, and only its result can take that track out of the app-wide caches.
  it('cancels no delete already in flight at unmount, and waits for each to settle', async () => {
    const { wrapper } = setup();
    const pending: (() => void)[] = [];
    mockDeleteTrack.mockImplementation(() => new Promise<void>((resolve) => pending.push(resolve)));
    const settled = jest.fn();

    const { result, unmount } = renderHook(() => useDeleteTracks(), { wrapper });
    let run!: Promise<unknown>;
    act(() => {
      run = result.current.mutateAsync(ids(10));
      void run.then(settled, settled);
    });
    await waitFor(() => expect(pending).toHaveLength(BULK_DELETE_CONCURRENCY));
    unmount();
    await act(async () => undefined);

    const cancelled = mockDeleteTrack.mock.calls.flatMap(([id, signal]) =>
      signal?.aborted ? [id] : [],
    );
    expect(cancelled).toEqual([]);
    expect(settled).not.toHaveBeenCalled();
    await act(async () => pending.forEach((resolve) => resolve()));
    expect(((await run) as { deleted: number }).deleted).toBe(BULK_DELETE_CONCURRENCY);
  });
});

describe('useReacquireTrack — a started re-acquisition is a whole pending state', () => {
  it('drops any failure text instead of patching the status alone (#933)', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(
      PLAYLIST_KEY,
      playlist([
        track('t1', {
          acquisition_status: 'failed',
          failure_reason: 'no source',
          failure_message: 'No source found',
        }),
      ]),
    );
    mockReacquireTrack.mockResolvedValue(undefined);

    const { result } = renderHook(() => useReacquireTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]).toMatchObject(
      { acquisition_status: 'pending', failure_reason: null, failure_message: null },
    );
  });
});

describe('useReacquireTrack — failure diagnostics and copy', () => {
  it('logs the failure with the track id and endpoint, and alerts with the shared tail', async () => {
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
      failure: 'unknown',
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

// #795: every failure used to roll back and show the same "try again" Alert, so a
// track deleted elsewhere came back as a ghost row and a refused session was told
// to just retry. The hooks now branch on the failure class.
describe('track mutation hooks — respond to the failure class, not one generic path', () => {
  const vanished = ['Track not found', 'This track is no longer in your library.'] as const;

  it('a delete answered 404 keeps the track removed and shows no failure', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('t1'), track('t2')])],
      pageParams: [0],
    });
    mockDeleteTrack.mockRejectedValue(new ApiError(404, 'not found'));

    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t2')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(pagedIds(queryClient)).toEqual(['t1']);
    expect(alertSpy).not.toHaveBeenCalled();
    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('a delete refused for auth rolls back and asks to sign in, unlike a server error', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('t1'), track('t2')])],
      pageParams: [0],
    });
    mockDeleteTrack.mockRejectedValueOnce(new ApiError(401, 'unauthorized'));
    mockDeleteTrack.mockRejectedValueOnce(new ApiError(503, 'unavailable'));

    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    for (let i = 0; i < 2; i++) {
      await act(async () => {
        await result.current.mutateAsync(asTrackId('t2')).catch(() => undefined);
      });
    }

    expect(pagedIds(queryClient)).toEqual(['t1', 't2']);
    expect(alertSpy.mock.calls).toEqual([
      ['Delete failed', 'Could not remove the track. Sign in again, then retry.'],
      ['Delete failed', `Could not remove the track. ${RETRY_TAIL}`],
    ]);
  });

  it('a bulk delete counts tracks already gone (404) as deleted, not as failures', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('a'), track('b'), track('c')])],
      pageParams: [0],
    });
    mockDeleteTrack.mockImplementation((id) =>
      id === 'a' ? Promise.reject(new ApiError(404, 'not found')) : Promise.resolve(),
    );

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate([asTrackId('a'), asTrackId('b')]));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toMatchObject({ deleted: 2, requested: 2, failures: [] });
    expect(pagedIds(queryClient)).toEqual(['c']);
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('a retry answered 404 drops the vanished track instead of restoring it as failed', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('t1', { acquisition_status: 'failed' }), track('t2')])],
      pageParams: [0],
    });
    useTrackStatusStore
      .getState()
      .patch(asTrackId('t1'), { acquisitionStatus: 'failed', failureMessage: 'no source' });
    mockRetryAcquisition.mockRejectedValue(new ApiError(404, 'not found'));

    const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(pagedIds(queryClient)).toEqual(['t2']);
    expect(useTrackStatusStore.getState().statuses['t1']).toBeUndefined();
    expect(alertSpy).toHaveBeenCalledWith(...vanished);
  });

  it('a re-acquire refused for auth asks to sign in; one answered 410 drops the track', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(PLAYLIST_KEY, playlist([track('t1'), track('t2')]));
    mockReacquireTrack.mockRejectedValueOnce(new ApiError(403, 'forbidden'));
    mockReacquireTrack.mockRejectedValueOnce(new ApiError(410, 'gone'));

    const { result } = renderHook(() => useReacquireTrack(), { wrapper });
    for (let attempt = 1; attempt <= 2; attempt++) {
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => {
        expect(alertSpy).toHaveBeenCalledTimes(attempt);
        expect(result.current.isInFlight(asTrackId('t1'))).toBe(false);
      });
    }

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t2']);
    expect(alertSpy.mock.calls).toEqual([
      [
        'Re-acquire failed',
        'Could not start a re-acquisition. Your current audio is unchanged. Sign in again, then retry.',
      ],
      vanished,
    ]);
  });
});

describe('deleting a pinned track removes its download entry', () => {
  const seedPinned = (id: string) =>
    usePinnedStore.setState({
      entries: { [id]: { trackId: asTrackId(id), status: 'ready', uri: `file:///${id}.m4a` } },
    });

  it('drops the entry after a single delete succeeds', async () => {
    const { wrapper } = setup();
    seedPinned('t1');
    mockDeleteTrack.mockResolvedValue(undefined);
    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
  });

  it('drops the entry when the server says the track is already gone', async () => {
    const { wrapper } = setup();
    seedPinned('t1');
    mockDeleteTrack.mockRejectedValue(new ApiError(404, 'not found'));
    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
  });

  it('leaves the entry as it was when the delete fails', async () => {
    const { wrapper } = setup();
    seedPinned('t1');
    mockDeleteTrack.mockRejectedValue(new ApiError(500, 'boom'));
    const { result } = renderHook(() => useDeleteTrack(), { wrapper });
    act(() => result.current.mutate(asTrackId('t1')));
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(usePinnedStore.getState().entries['t1']?.status).toBe('ready');
  });

  it('drops the entries of bulk-deleted tracks only', async () => {
    const { wrapper } = setup();
    seedPinned('a');
    usePinnedStore.setState((s) => ({
      entries: {
        ...s.entries,
        b: { trackId: asTrackId('b'), status: 'ready', uri: 'file:///b.m4a' },
      },
    }));
    mockDeleteTrack.mockImplementation((id) =>
      id === 'b' ? Promise.reject(new ApiError(500, 'boom')) : Promise.resolve(),
    );
    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate([asTrackId('a'), asTrackId('b')]));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(Object.keys(usePinnedStore.getState().entries)).toEqual(['b']);
  });
});
