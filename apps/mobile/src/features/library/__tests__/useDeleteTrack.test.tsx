// Failure paths of useDeleteTrack (#788): a failed delete restores the cache it
// optimistically patched and logs a redacted line with track id and endpoint.

import { Alert } from 'react-native';
import type { InfiniteData } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/errors';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { ListTracksResponse, PlaylistDetailResponse } from '@shared/api-client/types';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { RETRY_TAIL } from '@shared/lib/describeError';
import { libraryKeys } from '@shared/lib/query-keys';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import {
  track,
  page,
  playlist,
  TRACKS_KEY,
  LIBRARY_DERIVED,
  invalidatedKeys,
  PLAYLIST_KEY,
  setup,
  pagedIds,
  deferred,
  READY,
  libraryOf,
  signOutThenLoadUserB,
  userBLibrary,
} from './trackMutationFixtures';

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

// #795: every failure used to roll back and show the same "try again" Alert, so a
// track deleted elsewhere came back as a ghost row and a refused session was told
// to just retry. The hooks now branch on the failure class.
describe('track mutation hooks — respond to the failure class, not one generic path', () => {
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
});
