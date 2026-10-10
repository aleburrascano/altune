import { Alert } from 'react-native';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import { QueryClientProvider, type InfiniteData } from '@tanstack/react-query';
import { ApiError } from '@shared/failure/errors';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { RETRY_TAIL } from '@shared/failure/describeError';
import { TRACK_CACHE_FAMILIES } from '@shared/events/trackCacheShapes';
import type { ReactNode } from 'react';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import {
  BULK_DELETE_CONCURRENCY,
  BULK_DELETE_DEADLINE_MS,
  deleteOne,
  useDeleteTracks,
  useDeleteTrack,
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
  playlist,
  PLAYLIST_KEY,
  READY,
} from './trackMutationFixtures';
import type { ListTracksResponse } from '@shared/api-client/trackResponse';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import { libraryKeys } from '@shared/query/queryKeys';
import { recordUserAction } from '@shared/telemetry/userTelemetry';

const realUnpin = usePinnedStore.getState().unpin;

function failCleanupFor(failing: (trackId: TrackId) => boolean): void {
  function unpin(trackId: TrackId): ReturnType<typeof realUnpin> {
    if (failing(trackId)) throw new Error('download cleanup failed');
    return realUnpin(trackId);
  }
  usePinnedStore.setState({ unpin });
}

function failEveryCleanup(): boolean {
  return true;
}

function failCleanupOfB(trackId: TrackId): boolean {
  return trackId === 'b';
}

jest.mock('@shared/telemetry/userTelemetry', () => ({
  recordUserAction: jest.fn(),
  recordFailureShown: jest.fn(),
}));

describe('useDeleteTracks', () => {
  const mockDeleteTrack = jest.fn<Promise<void>, [TrackId, AbortSignal?]>();

  const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();

  const mockReacquireTrack = jest.fn<Promise<void>, [TrackId]>();

  const ports = {
    library: {
      deleteTrack: mockDeleteTrack,
      reacquireTrack: mockReacquireTrack,
    },
    acquisition: { retryAcquisition: mockRetryAcquisition },
  };

  let alertSpy: jest.SpyInstance;

  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    mockDeleteTrack.mockReset();
    mockRetryAcquisition.mockReset();
    mockReacquireTrack.mockReset();
    useTrackStatusStore.getState().reset();
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, unpin: realUnpin });
    alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    alertSpy.mockRestore();
    warnSpy.mockRestore();
  });

  describe('useDeleteTracks — derived caches', () => {
    it('invalidates the membership-derived caches once when any track was deleted (#938)', async () => {
      const { queryClient, wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
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
      const { wrapper } = setup(ports);
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
        const { wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
      queryClient.setQueryData(TRACKS_KEY, {
        pages: [page([track('t0'), track('t1'), track('t9')])],
        pageParams: [0],
      });
      const pending: (() => void)[] = [];
      mockDeleteTrack.mockImplementation(
        () => new Promise<void>((resolve) => pending.push(resolve)),
      );

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
      const { wrapper } = setup(ports);
      const pending: (() => void)[] = [];
      mockDeleteTrack.mockImplementation(
        () => new Promise<void>((resolve) => pending.push(resolve)),
      );
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
      const { queryClient, wrapper } = setup(ports);
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
      const { wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
      const spy = jest.spyOn(queryClient, 'invalidateQueries');
      mockDeleteTrack.mockResolvedValue(undefined);
      failCleanupFor(failCleanupOfB);

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
    const seedPinned = (id: string) =>
      usePinnedStore.setState((s) => ({
        entries: {
          ...s.entries,
          [id]: { trackId: asTrackId(id), status: 'ready', uri: `file:///${id}.m4a` },
        },
      }));

    it('succeeds, cleans the other tracks, and re-syncs every track cache family', async () => {
      const { queryClient, wrapper } = setup(ports);
      queryClient.setQueryData(TRACKS_KEY, {
        pages: [page([track('a'), track('b'), track('c')])],
        pageParams: [0],
      });
      ['a', 'c'].forEach(seedPinned);
      const spy = jest.spyOn(queryClient, 'invalidateQueries');
      mockDeleteTrack.mockResolvedValue(undefined);
      failCleanupFor(failCleanupOfB);

      const { result } = renderHook(() => useDeleteTracks(), { wrapper });
      act(() => result.current.mutate(['a', 'b', 'c'].map(asTrackId)));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(result.current.data).toMatchObject({ deleted: 3, failures: [] });
      expect(alertSpy).not.toHaveBeenCalled();
      expect(pagedIds(queryClient)).toEqual([]);
      expect(Object.keys(usePinnedStore.getState().entries)).toEqual([]);
      expect(invalidatedKeys(spy)).toEqual(
        expect.arrayContaining(Object.values(TRACK_CACHE_FAMILIES).map((f) => f.prefix)),
      );
      expect(warnSpy).toHaveBeenCalledWith(
        '[library] delete cleanup failed',
        expect.objectContaining({ trackId: 'b' }),
      );
    });

    it('invalidates only the derived keys when no cleanup threw', async () => {
      const { queryClient, wrapper } = setup(ports);
      const spy = jest.spyOn(queryClient, 'invalidateQueries');
      mockDeleteTrack.mockResolvedValue(undefined);

      const { result } = renderHook(() => useDeleteTracks(), { wrapper });
      act(() => result.current.mutate(['a', 'b'].map(asTrackId)));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(invalidatedKeys(spy)).toEqual(LIBRARY_DERIVED);
    });

    it('skips cleanup and re-sync for a run whose user signed out before the delete returned', async () => {
      const { queryClient, wrapper } = setup(ports);
      const spy = jest.spyOn(queryClient, 'invalidateQueries');
      const request = deferred();
      mockDeleteTrack.mockImplementation(() => request.promise);
      failCleanupFor(failEveryCleanup);
      const { result } = renderHook(() => useDeleteTracks(), { wrapper });
      act(() => result.current.mutate([asTrackId('a1')]));
      await waitFor(() => expect(mockDeleteTrack).toHaveBeenCalledTimes(1));

      signOutThenLoadUserB(queryClient, [track('a1'), track('b1')]);
      spy.mockClear();
      await act(async () => request.resolve());
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(warnSpy).not.toHaveBeenCalledWith(
        '[library] delete cleanup failed',
        expect.anything(),
      );
      expect(invalidatedKeys(spy)).toEqual([]);
      expect(userBLibrary(queryClient)).toEqual(libraryOf([track('a1'), track('b1')]));
    });
  });

  describe('useDeleteTracks — re-sync gate after a live cleanup throw then sign-out', () => {
    it('does not re-sync track caches when a cleanup threw and the user then signed out', async () => {
      const { queryClient, wrapper } = setup(ports);
      const spy = jest.spyOn(queryClient, 'invalidateQueries');
      const held = deferred();
      mockDeleteTrack.mockImplementation((id) => (id === 'a1' ? Promise.resolve() : held.promise));
      failCleanupFor(failEveryCleanup);
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
      const { wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
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

      await deleteOne({ ...createMemoryLibraryPort(), ...ports.library }, asTrackId('t9'));

      expect(mockDeleteTrack).toHaveBeenCalledWith('t9');
    });
  });

  describe('useDeleteTracks — deletes through the library port it is given (#1132)', () => {
    function portWrapper(queryClient: ReturnType<typeof setup>['queryClient'], port: LibraryPort) {
      return function Wrapper({ children }: { children: ReactNode }) {
        return (
          <QueryClientProvider client={queryClient}>
            <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
          </QueryClientProvider>
        );
      };
    }

    async function idsLeftIn(port: LibraryPort): Promise<string[]> {
      const { items } = await port.getAllTracks({});
      return items.map((t) => t.id);
    }

    it('removes the selected tracks from the provided port and never calls the HTTP client', async () => {
      const { queryClient } = setup(ports);
      const port = createMemoryLibraryPort({ tracks: [track('a'), track('b'), track('c')] });
      queryClient.setQueryData(TRACKS_KEY, libraryOf([track('a'), track('b'), track('c')]));

      const { result } = renderHook(() => useDeleteTracks(), {
        wrapper: portWrapper(queryClient, port),
      });
      act(() => result.current.mutate([asTrackId('a'), asTrackId('c')]));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(result.current.data).toMatchObject({ deleted: 2, requested: 2, failures: [] });
      expect(await idsLeftIn(port)).toEqual(['b']);
      expect(pagedIds(queryClient)).toEqual(['b']);
      expect(mockDeleteTrack).not.toHaveBeenCalled();
    });

    it('counts a track the port no longer has (404) as deleted, not as a failure', async () => {
      const { queryClient } = setup(ports);
      const port = createMemoryLibraryPort({ tracks: [track('a'), track('b')] });
      queryClient.setQueryData(TRACKS_KEY, libraryOf([track('a'), track('b'), track('ghost')]));

      const { result } = renderHook(() => useDeleteTracks(), {
        wrapper: portWrapper(queryClient, port),
      });
      act(() => result.current.mutate([asTrackId('ghost'), asTrackId('a')]));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(result.current.data).toMatchObject({ deleted: 2, requested: 2, failures: [] });
      expect(await idsLeftIn(port)).toEqual(['b']);
      expect(pagedIds(queryClient)).toEqual(['b']);
      expect(alertSpy).not.toHaveBeenCalled();
    });

    it('reports a delete the port refuses as a failure with its own error and keeps that track', async () => {
      const { queryClient } = setup(ports);
      const memory = createMemoryLibraryPort({ tracks: [track('a'), track('b')] });
      const refusal = new ApiError(500, 'boom');
      const port: LibraryPort = {
        ...memory,
        deleteTrack: (id) => (id === 'a' ? Promise.reject(refusal) : memory.deleteTrack(id)),
      };
      queryClient.setQueryData(TRACKS_KEY, libraryOf([track('a'), track('b')]));

      const { result } = renderHook(() => useDeleteTracks(), {
        wrapper: portWrapper(queryClient, port),
      });
      act(() => result.current.mutate([asTrackId('a'), asTrackId('b')]));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(result.current.data).toMatchObject({
        deleted: 1,
        requested: 2,
        failures: [{ trackId: 'a', error: refusal }],
      });
      expect(await idsLeftIn(port)).toEqual(['a']);
      expect(pagedIds(queryClient)).toEqual(['a']);
      expect(mockDeleteTrack).not.toHaveBeenCalled();
    });

    it('deleteOne deletes the track through the port passed to it', async () => {
      const port = createMemoryLibraryPort({ tracks: [track('t1'), track('t2')] });

      await deleteOne(port, asTrackId('t1'));

      expect(await idsLeftIn(port)).toEqual(['t2']);
      expect(mockDeleteTrack).not.toHaveBeenCalled();
    });

    it('deleteOne rejects with the error the port gives, without falling back to HTTP', async () => {
      const refusal = new ApiError(500, 'boom');
      const port: LibraryPort = {
        ...createMemoryLibraryPort(),
        deleteTrack: () => Promise.reject(refusal),
      };

      await expect(deleteOne(port, asTrackId('t1'))).rejects.toBe(refusal);
      expect(mockDeleteTrack).not.toHaveBeenCalled();
    });
  });
});

describe('useDeleteTrack', () => {
  const mockDeleteTrack = jest.fn<Promise<void>, [TrackId, AbortSignal?]>();

  const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();

  const mockReacquireTrack = jest.fn<Promise<void>, [TrackId]>();

  const ports = {
    library: {
      deleteTrack: mockDeleteTrack,
      reacquireTrack: mockReacquireTrack,
    },
    acquisition: { retryAcquisition: mockRetryAcquisition },
  };

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
      const { queryClient, wrapper } = setup(ports);
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
      const { wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
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

    it('records the delete as a library.delete_track user action with the track id', async () => {
      const { wrapper } = setup(ports);
      mockDeleteTrack.mockResolvedValue(undefined);

      const { result } = renderHook(() => useDeleteTrack(), { wrapper });
      act(() => result.current.mutate(asTrackId('t2')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(recordUserAction).toHaveBeenCalledWith({
        action: 'library.delete_track',
        outcome: 'succeeded',
        track_id: 't2',
      });
    });

    it('marks the membership-derived caches stale once the delete lands (#938)', async () => {
      const { queryClient, wrapper } = setup(ports);
      const spy = jest.spyOn(queryClient, 'invalidateQueries');
      mockDeleteTrack.mockResolvedValue(undefined);

      const { result } = renderHook(() => useDeleteTrack(), { wrapper });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(invalidatedKeys(spy)).toEqual(LIBRARY_DERIVED);
    });

    it('invalidates nothing when the delete fails and the track is restored', async () => {
      const { queryClient, wrapper } = setup(ports);
      const spy = jest.spyOn(queryClient, 'invalidateQueries');
      mockDeleteTrack.mockRejectedValue(new ApiError(500, 'boom'));

      const { result } = renderHook(() => useDeleteTrack(), { wrapper });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(spy).not.toHaveBeenCalled();
    });
  });

  describe('track mutation hooks — respond to the failure class, not one generic path', () => {
    it('a delete answered 404 keeps the track removed and shows no failure', async () => {
      const { queryClient, wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
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
      const { wrapper } = setup(ports);
      seedPinned('t1');
      mockDeleteTrack.mockResolvedValue(undefined);
      const { result } = renderHook(() => useDeleteTrack(), { wrapper });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));
      expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
    });

    it('drops the entry when the server says the track is already gone', async () => {
      const { wrapper } = setup(ports);
      seedPinned('t1');
      mockDeleteTrack.mockRejectedValue(new ApiError(404, 'not found'));
      const { result } = renderHook(() => useDeleteTrack(), { wrapper });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(result.current.isError).toBe(true));
      expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
    });

    it('leaves the entry as it was when the delete fails', async () => {
      const { wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
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

  describe('useDeleteTrack — deletes through the library port it is given (#1132)', () => {
    function portWrapper(queryClient: ReturnType<typeof setup>['queryClient'], port: LibraryPort) {
      return function Wrapper({ children }: { children: ReactNode }) {
        return (
          <QueryClientProvider client={queryClient}>
            <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
          </QueryClientProvider>
        );
      };
    }

    async function idsLeftIn(port: LibraryPort): Promise<string[]> {
      const { items } = await port.getAllTracks({});
      return items.map((t) => t.id);
    }

    it('removes the track from the provided port and never calls the HTTP client', async () => {
      const { queryClient } = setup(ports);
      const port = createMemoryLibraryPort({ tracks: [track('t1'), track('t2')] });
      queryClient.setQueryData(TRACKS_KEY, libraryOf([track('t1'), track('t2')]));

      const { result } = renderHook(() => useDeleteTrack(), {
        wrapper: portWrapper(queryClient, port),
      });
      act(() => result.current.mutate(asTrackId('t2')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(await idsLeftIn(port)).toEqual(['t1']);
      expect(pagedIds(queryClient)).toEqual(['t1']);
      expect(mockDeleteTrack).not.toHaveBeenCalled();
    });

    it('keeps a track the port no longer has (404) removed and shows no failure', async () => {
      const { queryClient } = setup(ports);
      const port = createMemoryLibraryPort({ tracks: [track('t1')] });
      queryClient.setQueryData(TRACKS_KEY, libraryOf([track('t1'), track('t2')]));

      const { result } = renderHook(() => useDeleteTrack(), {
        wrapper: portWrapper(queryClient, port),
      });
      act(() => result.current.mutate(asTrackId('t2')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(pagedIds(queryClient)).toEqual(['t1']);
      expect(alertSpy).not.toHaveBeenCalled();
      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('puts the track back and alerts when the port refuses the delete', async () => {
      const { queryClient } = setup(ports);
      const memory = createMemoryLibraryPort({ tracks: [track('t1'), track('t2')] });
      const port: LibraryPort = {
        ...memory,
        deleteTrack: () => Promise.reject(new ApiError(500, 'internal')),
      };
      queryClient.setQueryData(TRACKS_KEY, libraryOf([track('t1'), track('t2')]));

      const { result } = renderHook(() => useDeleteTrack(), {
        wrapper: portWrapper(queryClient, port),
      });
      act(() => result.current.mutate(asTrackId('t2')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(pagedIds(queryClient)).toEqual(['t1', 't2']);
      expect(await idsLeftIn(port)).toEqual(['t1', 't2']);
      expect(alertSpy).toHaveBeenCalledWith(
        'Delete failed',
        `Could not remove the track. ${RETRY_TAIL}`,
      );
      expect(mockDeleteTrack).not.toHaveBeenCalled();
    });
  });

  describe('useDeleteTracks — bulk edge cases', () => {
    const CONFLICT = 409;
    const TWICE = 2;
    const THRICE = 3;
    it('resolves an empty batch without calling the port or alerting', async () => {
      const { wrapper } = setup(ports);
      const { result } = renderHook(() => useDeleteTracks(), { wrapper });

      await expect(result.current.mutateAsync([])).resolves.toEqual({
        deleted: 0,
        requested: 0,
        failures: [],
        skipped: 0,
        cancelled: false,
      });

      expect(mockDeleteTrack).not.toHaveBeenCalled();
      expect(alertSpy).not.toHaveBeenCalled();
    });

    it('lists failures in input order even when a later track fails first', async () => {
      const { wrapper } = setup(ports);
      const slowA = deferred();
      const errorA = new ApiError(CONFLICT, 'conflict a');
      const errorC = new ApiError(CONFLICT, 'conflict c');
      mockDeleteTrack.mockImplementation((id) => {
        if (id === 'a') return slowA.promise;
        if (id === 'c') return Promise.reject(errorC);
        return Promise.resolve();
      });

      const { result } = renderHook(() => useDeleteTracks(), { wrapper });
      let settled: ReturnType<typeof result.current.mutateAsync> | undefined;
      act(() => {
        settled = result.current.mutateAsync([asTrackId('a'), asTrackId('b'), asTrackId('c')]);
      });
      await waitFor(() => expect(mockDeleteTrack).toHaveBeenCalledTimes(THRICE));
      await act(async () => {
        slowA.reject(errorA);
        await Promise.resolve();
      });

      await expect(settled).resolves.toEqual({
        deleted: 1,
        requested: THRICE,
        failures: [
          { trackId: 'a', error: errorA },
          { trackId: 'c', error: errorC },
        ],
        skipped: 0,
        cancelled: false,
      });
    });

    it('counts a track id given twice once per occurrence', async () => {
      const { wrapper } = setup(ports);
      mockDeleteTrack.mockResolvedValue(undefined);
      const { result } = renderHook(() => useDeleteTracks(), { wrapper });

      await expect(
        result.current.mutateAsync([asTrackId('a'), asTrackId('a')]),
      ).resolves.toMatchObject({ requested: TWICE, deleted: TWICE, skipped: 0 });
      expect(mockDeleteTrack).toHaveBeenCalledTimes(TWICE);
    });
  });
});
