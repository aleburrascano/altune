import { Alert } from 'react-native';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { QueryClient } from '@tanstack/react-query';

import { ApiError } from '@shared/errors';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { RETRY_TAIL } from '@shared/lib/describeError';
import { TRACK_CACHE_FAMILIES } from '@shared/events/trackCachePatch';
import type * as ForgetTrackModule from '@shared/events/forgetTrack';

import {
  BULK_DELETE_CONCURRENCY,
  BULK_DELETE_DEADLINE_MS,
  deleteOne,
  useDeleteTracks,
} from '../hooks/useDeleteTracks';
import {
  track,
  page,
  TRACKS_KEY,
  LIBRARY_DERIVED,
  invalidatedKeys,
  setup,
  pagedIds,
  deferred,
  libraryOf,
  signOutThenLoadUserB,
  userBLibrary,
} from './trackMutationFixtures';

const mockDeleteTrack = jest.fn<Promise<void>, [TrackId, AbortSignal?]>();
const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();
const mockReacquireTrack = jest.fn<Promise<void>, [TrackId]>();
jest.mock('@shared/api-client/tracks', () => ({
  deleteTrack: (id: TrackId, signal?: AbortSignal) => mockDeleteTrack(id, signal),
  retryAcquisition: (id: TrackId) => mockRetryAcquisition(id),
  reacquireTrack: (id: TrackId) => mockReacquireTrack(id),
}));

const actualForgetTrack = jest.requireActual<typeof ForgetTrackModule>(
  '@shared/events/forgetTrack',
).forgetTrack;
const mockForgetTrack = jest.fn<void, [QueryClient, TrackId]>(actualForgetTrack);
jest.mock('@shared/events/forgetTrack', () => ({
  forgetTrack: (queryClient: QueryClient, trackId: TrackId) =>
    mockForgetTrack(queryClient, trackId),
}));

let alertSpy: jest.SpyInstance;
let warnSpy: jest.SpyInstance;

beforeEach(() => {
  mockDeleteTrack.mockReset();
  mockRetryAcquisition.mockReset();
  mockReacquireTrack.mockReset();
  mockForgetTrack.mockReset();
  mockForgetTrack.mockImplementation(actualForgetTrack);
  useTrackStatusStore.getState().reset();
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  alertSpy.mockRestore();
  warnSpy.mockRestore();
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
      '2 of 3 tracks could not be removed. Sign in again, then retry.',
    );
  });
});

describe('useDeleteTracks — bounded concurrency, aggregate deadline, cancel on unmount', () => {
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
    expect(outcome.deleted).toBe(BULK_DELETE_CONCURRENCY);
    expect(outcome.skipped).toBe(10 - BULK_DELETE_CONCURRENCY);
    expect(pagedIds(queryClient)).toEqual(['t9']);
    expect(alertSpy).not.toHaveBeenCalled();
  });

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

describe('track mutation hooks — respond to the failure class, not one generic path', () => {
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
});

describe('deleting a pinned track removes its download entry', () => {
  const seedPinned = (id: string) =>
    usePinnedStore.setState({
      entries: { [id]: { trackId: asTrackId(id), status: 'ready', uri: `file:///${id}.m4a` } },
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

describe('track mutations that settle after sign-out leave the next user untouched (#2729)', () => {
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
});

describe('useDeleteTracks — a cleanup throw is not silent (#2763)', () => {
  it('logs the cleanup failure and re-syncs instead of rejecting when onDeleted throws', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    mockDeleteTrack.mockResolvedValue(undefined);
    mockForgetTrack.mockImplementation((queryClient, trackId) => {
      if (trackId === 'b') throw new Error('cache patch failed');
      actualForgetTrack(queryClient, trackId);
    });

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate([asTrackId('a'), asTrackId('b')]));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toMatchObject({ deleted: 2, requested: 2, failures: [] });
    expect(invalidatedKeys(spy)).toEqual(
      expect.arrayContaining([
        ...LIBRARY_DERIVED,
        ...Object.values(TRACK_CACHE_FAMILIES).map((f) => f.prefix),
      ]),
    );
    expect(warnSpy).toHaveBeenCalledWith(
      '[library] delete cleanup failed',
      expect.objectContaining({ trackId: 'b' }),
    );
    expect(warnSpy).not.toHaveBeenCalledWith('[library] bulk delete failed', expect.anything());
    expect(alertSpy).not.toHaveBeenCalled();
  });
});

describe('useDeleteTracks — local cleanup failure never loses a deleted track (#218)', () => {
  const throwForB = () =>
    mockForgetTrack.mockImplementation((client, trackId) => {
      if (trackId === 'b') throw new Error('cache patch failed');
      actualForgetTrack(client, trackId);
    });
  const seedPinned = (id: string) =>
    usePinnedStore.setState((s) => ({
      entries: {
        ...s.entries,
        [id]: { trackId: asTrackId(id), status: 'ready', uri: `file:///${id}.m4a` },
      },
    }));

  it('succeeds, cleans the other tracks, and re-syncs every track cache family', async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(TRACKS_KEY, {
      pages: [page([track('a'), track('b'), track('c')])],
      pageParams: [0],
    });
    ['a', 'c'].forEach(seedPinned);
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    mockDeleteTrack.mockResolvedValue(undefined);
    throwForB();

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate(['a', 'b', 'c'].map(asTrackId)));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toMatchObject({ deleted: 3, failures: [] });
    expect(alertSpy).not.toHaveBeenCalled();
    expect(pagedIds(queryClient)).toEqual(['b']);
    expect(Object.keys(usePinnedStore.getState().entries)).toEqual([]);
    expect(mockForgetTrack.mock.calls.filter(([, id]) => id === 'b')).toHaveLength(1);
    expect(invalidatedKeys(spy)).toEqual(
      expect.arrayContaining(Object.values(TRACK_CACHE_FAMILIES).map((f) => f.prefix)),
    );
    expect(warnSpy).toHaveBeenCalledWith(
      '[library] delete cleanup failed',
      expect.objectContaining({ trackId: 'b' }),
    );
  });

  it('invalidates only the derived keys when no cleanup threw', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    mockDeleteTrack.mockResolvedValue(undefined);

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate(['a', 'b'].map(asTrackId)));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidatedKeys(spy)).toEqual(LIBRARY_DERIVED);
  });

  it('skips cleanup and re-sync for a run whose user signed out before the delete returned', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    const request = deferred();
    mockDeleteTrack.mockImplementation(() => request.promise);
    mockForgetTrack.mockImplementation(() => {
      throw new Error('cache patch failed');
    });
    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate([asTrackId('a1')]));
    await waitFor(() => expect(mockDeleteTrack).toHaveBeenCalledTimes(1));

    signOutThenLoadUserB(queryClient, [track('a1'), track('b1')]);
    spy.mockClear();
    await act(async () => request.resolve());
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(mockForgetTrack).not.toHaveBeenCalled();
    expect(invalidatedKeys(spy)).toEqual([]);
    expect(userBLibrary(queryClient)).toEqual(libraryOf([track('a1'), track('b1')]));
  });
});

describe('useDeleteTracks — re-sync gate after a live cleanup throw then sign-out', () => {
  it('does not re-sync track caches when a cleanup threw and the user then signed out', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    const held = deferred();
    mockDeleteTrack.mockImplementation((id) => (id === 'a1' ? Promise.resolve() : held.promise));
    mockForgetTrack.mockImplementation(() => {
      throw new Error('cache patch failed');
    });
    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate(['a1', 'a2'].map(asTrackId)));
    await waitFor(() =>
      expect(warnSpy).toHaveBeenCalledWith(
        '[library] delete cleanup failed',
        expect.objectContaining({ trackId: 'a1' }),
      ),
    );

    signOutThenLoadUserB(queryClient, [track('a1'), track('b1')]);
    spy.mockClear();
    await act(async () => held.resolve());
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidatedKeys(spy)).toEqual([]);
    expect(userBLibrary(queryClient)).toEqual(libraryOf([track('a1'), track('b1')]));
  });
});

describe('useDeleteTracks — partial failure alert uses the classified tail', () => {
  const runWithFailures = async (errors: Record<string, ApiError>) => {
    const { wrapper } = setup();
    mockDeleteTrack.mockImplementation((id) =>
      errors[id] ? Promise.reject(errors[id]) : Promise.resolve(),
    );
    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate(['a', 'b', 'c'].map(asTrackId)));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
  };

  it.each([
    ['auth last', [409, 401]],
    ['auth first', [401, 409]],
  ])('ends with the sign-in tail when a failure is auth (%s)', async (_name, [first, second]) => {
    await runWithFailures({ a: new ApiError(first!, 'x'), c: new ApiError(second!, 'y') });
    expect(alertSpy).toHaveBeenCalledWith(
      'Delete failed',
      '2 of 3 tracks could not be removed. Sign in again, then retry.',
    );
  });

  it('ends with the retry tail when no failure is auth', async () => {
    await runWithFailures({ a: new ApiError(409, 'x'), c: new ApiError(500, 'y') });
    expect(alertSpy).toHaveBeenCalledWith(
      'Delete failed',
      `2 of 3 tracks could not be removed. ${RETRY_TAIL}`,
    );
  });
});

describe('useDeleteTracks — a failed run names the affected tracks', () => {
  it('logs the first 20 ids of 25 and invalidates the tracks list', async () => {
    const { queryClient, wrapper } = setup();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    mockDeleteTrack.mockResolvedValue(undefined);
    alertSpy.mockImplementationOnce(() => {
      throw new Error('alert failed');
    });
    mockDeleteTrack.mockImplementation((id) =>
      id === 't0' ? Promise.reject(new ApiError(500, 'boom')) : Promise.resolve(),
    );
    const ids = Array.from({ length: 25 }, (_, i) => asTrackId(`t${i}`));

    const { result } = renderHook(() => useDeleteTracks(), { wrapper });
    act(() => result.current.mutate(ids));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(warnSpy).toHaveBeenCalledWith(
      '[library] bulk delete failed',
      expect.objectContaining({ requested: 25, trackIds: ids.slice(0, 20) }),
    );
    expect(invalidatedKeys(spy)).toContainEqual(['library', 'tracks']);
  });

  it('deleteOne sends one delete request for the track', async () => {
    mockDeleteTrack.mockResolvedValue(undefined);

    await deleteOne(asTrackId('t9'));

    expect(mockDeleteTrack).toHaveBeenCalledWith('t9', undefined);
  });
});
