import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';
import { asTrackId, type TrackId } from '@shared/apiClient/ids';
import type { TrackResponse, ListTracksResponse } from '@shared/apiClient/trackResponse';
import { trackResponseFactory } from '@testing/factories';
import {
  useReacquireTrack,
  useRetryAcquisition,
} from '@features/library/hooks/useRetryAcquisition';
import { TracksList } from '@features/library/ui/TracksList';
import {
  setup,
  track,
  playlist,
  PLAYLIST_KEY,
  deferred,
  libraryOf,
  signOutThenLoadUserB,
  userBLibrary,
  page,
  TRACKS_KEY,
  pagedIds,
} from '@features/library/__tests__/trackMutationFixtures';
import { Alert } from 'react-native';
import { ApiError, NetworkError } from '@shared/failure/errors';
import type { PlaylistDetailResponse } from '@shared/apiClient/playlistResponse';
import { useTrackStatusStore } from '@shared/acquisition/state/trackStatusStore';
import { usePinnedStore } from '@shared/offline/state/pinnedStore';
import { RETRY_TAIL } from '@shared/failure/describeError';
import type { ReactNode } from 'react';
import { QueryClientProvider, type InfiniteData } from '@tanstack/react-query';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import { signInTestUser } from '@testing/signInTestUser';

const { __http } = require('@testing/doubles/fetch.js');

const TELEMETRY_ROUTE = 'POST /v1/discovery/events';
const HTTP_ACCEPTED = 202;

describe('useOneRunPerTrack', () => {
  const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();

  const mockReacquireTrack = jest.fn<Promise<void>, [TrackId]>();

  const ports = {
    library: { reacquireTrack: mockReacquireTrack },
    acquisition: { retryAcquisition: mockRetryAcquisition },
  };

  function failedTrack(id: string): TrackResponse {
    return trackResponseFactory.build({
      id: asTrackId(id),
      title: `Track ${id}`,
      artist: 'An Artist',
      album: null,
      duration_seconds: 180,
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: 'failed',
      artwork_url: null,
      failure_reason: 'no_source',
      failure_message: 'No source found',
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
    });
  }

  function FailedTracksList({ tracks }: { tracks: TrackResponse[] }) {
    const retry = useRetryAcquisition();
    return (
      <TracksList
        tracks={tracks}
        emptyLabel=""
        refresh={{ refreshing: false, onRefresh: jest.fn() }}
        onPlay={jest.fn()}
        onPress={jest.fn()}
        onMore={jest.fn()}
        onRetry={(track) => retry.mutate(track.id)}
        isRetrying={retry.isInFlight}
        isPlaying={() => false}
      />
    );
  }

  const retryButton = (id: string) => screen.queryByTestId(`library-row-retry-${id}`);

  const retriedIds = (): TrackId[] => mockRetryAcquisition.mock.calls.map(([id]) => id);

  function tapRetry(id: string): void {
    const button = retryButton(id);
    if (button === null) return;
    fireEvent.press(button);
  }

  const rowsCatchUp = async (): Promise<void> => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  };

  beforeEach(() => {
    mockRetryAcquisition.mockReset();
    mockReacquireTrack.mockReset();
    mockRetryAcquisition.mockReturnValue(new Promise(() => undefined));
    mockReacquireTrack.mockReturnValue(new Promise(() => undefined));
  });

  describe('retry acquisition — a row in flight does not speak for the others', () => {
    it('sends no second retry for a track whose first retry is still in flight', async () => {
      const { wrapper } = setup(ports);
      render(<FailedTracksList tracks={[failedTrack('a'), failedTrack('b')]} />, { wrapper });

      tapRetry('a');
      tapRetry('b');
      await rowsCatchUp();
      tapRetry('a');
      await rowsCatchUp();

      expect(retriedIds()).toEqual(['a', 'b']);
    });

    it('keeps both tapped rows showing their own retry in progress', async () => {
      const { wrapper } = setup(ports);
      render(<FailedTracksList tracks={[failedTrack('a'), failedTrack('b')]} />, { wrapper });

      tapRetry('a');
      tapRetry('b');

      await waitFor(() => {
        expect(screen.getByTestId('library-row-retrying-a')).toBeTruthy();
        expect(screen.getByTestId('library-row-retrying-b')).toBeTruthy();
      });
    });

    it('offers retry again on the row whose request landed, and only on that row', async () => {
      const { wrapper } = setup(ports);
      let firstRetryLands!: () => void;
      mockRetryAcquisition.mockImplementationOnce(
        () => new Promise((resolve) => (firstRetryLands = () => resolve())),
      );
      render(<FailedTracksList tracks={[failedTrack('a'), failedTrack('b')]} />, { wrapper });

      tapRetry('a');
      tapRetry('b');
      await rowsCatchUp();
      firstRetryLands();

      await waitFor(() => expect(retryButton('a')).not.toBeNull());
      expect(retryButton('b')).toBeNull();
    });
  });

  describe('track mutations — one run per track id, whatever the taps do', () => {
    it('starts one retry per id when several taps land before any row re-renders', async () => {
      const { wrapper } = setup(ports);
      const { result } = renderHook(() => useRetryAcquisition(), { wrapper });

      act(() => {
        result.current.mutate(asTrackId('a'));
        result.current.mutate(asTrackId('a'));
        result.current.mutate(asTrackId('b'));
      });
      await rowsCatchUp();

      expect(retriedIds()).toEqual(['a', 'b']);
    });

    it('reports every re-acquiring track as in flight, not just the last one started', async () => {
      const { wrapper } = setup(ports);
      const { result } = renderHook(() => useReacquireTrack(), { wrapper });

      act(() => {
        result.current.mutate(asTrackId('a'));
        result.current.mutate(asTrackId('b'));
      });

      await waitFor(() => expect(result.current.isInFlight(asTrackId('a'))).toBe(true));
      expect(result.current.isInFlight(asTrackId('b'))).toBe(true);
      expect(result.current.isInFlight(asTrackId('c'))).toBe(false);
    });
  });
});

describe('useReacquireTrack', () => {
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

  describe('useReacquireTrack — a started re-acquisition is a whole pending state', () => {
    it('drops any failure text instead of patching the status alone (#933)', async () => {
      const { queryClient, wrapper } = setup(ports);
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

      expect(
        queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0],
      ).toMatchObject({
        acquisition_status: 'pending',
        failure_reason: null,
        failure_message: null,
      });
    });
  });

  describe('useReacquireTrack — failure diagnostics and copy', () => {
    it('logs the failure with the track id and endpoint, and alerts with the shared tail', async () => {
      const { queryClient, wrapper } = setup(ports);
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
        queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!
          .acquisition_status,
      ).toBe('ready');
    });
  });

  describe('track mutation hooks — respond to the failure class, not one generic path', () => {
    const vanished = ['Track not found', 'This track is no longer in your library.'] as const;

    it('a re-acquire refused for auth asks to sign in; one answered 410 drops the track', async () => {
      const { queryClient, wrapper } = setup(ports);
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

  describe('track mutations that settle after sign-out leave the next user untouched (#2729)', () => {
    it("useReacquireTrack: a re-acquire answering after sign-out does not mark user B's track pending", async () => {
      const { queryClient, wrapper } = setup(ports);
      const request = deferred();
      mockReacquireTrack.mockReturnValue(request.promise);
      const { result } = renderHook(() => useReacquireTrack(), { wrapper });
      act(() => result.current.mutate(asTrackId('a1')));
      await waitFor(() => expect(mockReacquireTrack).toHaveBeenCalledWith(asTrackId('a1')));

      signOutThenLoadUserB(queryClient, [track('a1'), track('b1')]);
      await act(async () => request.resolve());
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(userBLibrary(queryClient)).toEqual(libraryOf([track('a1'), track('b1')]));
    });

    it("useReacquireTrack: a not-found answer after sign-out drops nothing from user B's library and does not alert", async () => {
      const { queryClient, wrapper } = setup(ports);
      const request = deferred();
      mockReacquireTrack.mockReturnValue(request.promise);
      const { result } = renderHook(() => useReacquireTrack(), { wrapper });
      act(() => result.current.mutate(asTrackId('a1')));
      await waitFor(() => expect(mockReacquireTrack).toHaveBeenCalledWith(asTrackId('a1')));

      signOutThenLoadUserB(queryClient, [track('a1'), track('b1')]);
      await act(async () => request.reject(new ApiError(404, 'not found')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(userBLibrary(queryClient)).toEqual(libraryOf([track('a1'), track('b1')]));
      expect(alertSpy).not.toHaveBeenCalled();
    });
  });

  describe('useReacquireTrack — a re-acquire that never reaches the API (#3027)', () => {
    it('asks to try again, not to sign in, and leaves the track as it was', async () => {
      const { queryClient, wrapper } = setup(ports);
      queryClient.setQueryData(PLAYLIST_KEY, playlist([track('t1')]));
      mockReacquireTrack.mockRejectedValue(new NetworkError('transport', 'offline'));

      const { result } = renderHook(() => useReacquireTrack(), { wrapper });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(alertSpy.mock.calls).toEqual([
        [
          'Re-acquire failed',
          'Could not start a re-acquisition. Your current audio is unchanged. Please try again.',
        ],
      ]);
      expect(
        queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!
          .acquisition_status,
      ).toBe('ready');
    });
  });

  describe('useReacquireTrack — re-acquires through the library port it is given (#1132)', () => {
    function portWrapper(queryClient: ReturnType<typeof setup>['queryClient'], port: LibraryPort) {
      return function Wrapper({ children }: { children: ReactNode }) {
        return (
          <QueryClientProvider client={queryClient}>
            <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
          </QueryClientProvider>
        );
      };
    }

    it('asks the provided port to re-acquire, marks the track pending and never calls the HTTP client', async () => {
      const { queryClient } = setup(ports);
      const memory = createMemoryLibraryPort({ tracks: [track('t1')] });
      const asked: string[] = [];
      const port: LibraryPort = {
        ...memory,
        reacquireTrack: (id) => {
          asked.push(id);
          return memory.reacquireTrack(id);
        },
      };
      queryClient.setQueryData(
        PLAYLIST_KEY,
        playlist([track('t1', { acquisition_status: 'failed' })]),
      );

      const { result } = renderHook(() => useReacquireTrack(), {
        wrapper: portWrapper(queryClient, port),
      });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(asked).toEqual(['t1']);
      expect(
        queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!
          .acquisition_status,
      ).toBe('pending');
      expect(mockReacquireTrack).not.toHaveBeenCalled();
    });

    it('alerts and leaves the track as it was when the port refuses the re-acquire', async () => {
      const { queryClient } = setup(ports);
      const port: LibraryPort = {
        ...createMemoryLibraryPort({ tracks: [track('t1')] }),
        reacquireTrack: () => Promise.reject(new ApiError(409, 'conflict')),
      };
      queryClient.setQueryData(PLAYLIST_KEY, playlist([track('t1')]));

      const { result } = renderHook(() => useReacquireTrack(), {
        wrapper: portWrapper(queryClient, port),
      });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(alertSpy).toHaveBeenCalledWith(
        'Re-acquire failed',
        `Could not start a re-acquisition. Your current audio is unchanged. ${RETRY_TAIL}`,
      );
      expect(
        queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!
          .acquisition_status,
      ).toBe('ready');
      expect(mockReacquireTrack).not.toHaveBeenCalled();
    });
  });
});

describe('useRetryAcquisition', () => {
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

  function payloadsFor(action: string): Record<string, unknown>[] {
    return (__http.requests as { path: string; body: string }[])
      .filter((request) => request.path === '/v1/discovery/events')
      .map((request) => {
        const { payload } = JSON.parse(request.body) as { payload: Record<string, unknown> };
        delete payload['session_id'];
        return payload;
      })
      .filter((payload) => payload['action'] === action);
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
      const { queryClient, wrapper } = setup(ports);
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
      const { queryClient, wrapper } = setup(ports);
      const failed = track('t1', {
        acquisition_status: 'failed',
        failure_reason: 'no source',
        failure_message: 'No source found',
      });
      queryClient.setQueryData(PLAYLIST_KEY, playlist([failed]));
      let reject!: (e: Error) => void;
      mockRetryAcquisition.mockReturnValue(new Promise((_, r) => (reject = r)));
      function cached() {
        return queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!;
      }

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
      const { queryClient, wrapper } = setup(ports);
      queryClient.setQueryData(
        PLAYLIST_KEY,
        playlist([track('t1', { acquisition_status: 'failed' })]),
      );
      let reject!: (e: Error) => void;
      mockRetryAcquisition.mockReturnValue(new Promise((_, r) => (reject = r)));

      const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalledTimes(1));
      queryClient.setQueryData(
        PLAYLIST_KEY,
        playlist([track('t1', { acquisition_status: 'ready' })]),
      );
      await act(async () => reject(new Error('boom')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(
        queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!
          .acquisition_status,
      ).toBe('ready');
    });

    it('logs the failure with the track id and endpoint, and alerts with the shared tail', async () => {
      const { wrapper } = setup(ports);
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

  describe('track mutation hooks — respond to the failure class, not one generic path', () => {
    const vanished = ['Track not found', 'This track is no longer in your library.'] as const;

    it('a retry answered 404 drops the vanished track instead of restoring it as failed', async () => {
      const { queryClient, wrapper } = setup(ports);
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

    it('a retry refused for auth asks to sign in instead of a plain retry', async () => {
      const { wrapper } = setup(ports);
      mockRetryAcquisition.mockRejectedValue(new ApiError(401, 'unauthorized'));

      const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
      act(() => result.current.mutate(asTrackId('t1')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(alertSpy).toHaveBeenCalledWith(
        'Retry failed',
        'Could not restart acquisition. Sign in again, then retry.',
      );
    });
  });

  describe('track mutations that settle after sign-out leave the next user untouched (#2729)', () => {
    it("useRetryAcquisition: a retry failing after sign-out rolls nothing back into user B's cache and does not alert", async () => {
      const { queryClient, wrapper } = setup(ports);
      queryClient.setQueryData(
        TRACKS_KEY,
        libraryOf([track('a1', { acquisition_status: 'failed' })]),
      );
      const request = deferred();
      mockRetryAcquisition.mockReturnValue(request.promise);
      const { result } = renderHook(() => useRetryAcquisition(), { wrapper });
      act(() => result.current.mutate(asTrackId('a1')));
      await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalledTimes(1));

      const userBTracks = [track('a1', { acquisition_status: 'pending' }), track('b1')];
      signOutThenLoadUserB(queryClient, userBTracks);
      await act(async () => request.reject(new ApiError(500, 'internal')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(userBLibrary(queryClient)).toEqual(libraryOf(userBTracks));
      expect(alertSpy).not.toHaveBeenCalled();
    });
  });

  describe('useRetryAcquisition — retry_tapped and retry_request telemetry', () => {
    beforeEach(async () => {
      await signInTestUser();
      __http.reply(TELEMETRY_ROUTE, { status: HTTP_ACCEPTED });
    });

    it('records retry_tapped with the given entry point, then a sent and succeeded retry_request', async () => {
      const { wrapper } = setup(ports);
      mockRetryAcquisition.mockResolvedValue(undefined);
      const { result } = renderHook(() => useRetryAcquisition('album_row'), { wrapper });

      act(() => result.current.mutate(asTrackId('a')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      await waitFor(() =>
        expect(payloadsFor('retry_tapped')).toEqual([
          { track_id: 'a', action: 'retry_tapped', entry_point: 'album_row' },
        ]),
      );
      await waitFor(() =>
        expect(payloadsFor('retry_request')).toEqual([
          { track_id: 'a', action: 'retry_request', entry_point: 'album_row', outcome: 'sent' },
          {
            track_id: 'a',
            action: 'retry_request',
            entry_point: 'album_row',
            outcome: 'succeeded',
          },
        ]),
      );
    });

    it('has no entry point in the payload when none is given', async () => {
      const { wrapper } = setup(ports);
      mockRetryAcquisition.mockResolvedValue(undefined);
      const { result } = renderHook(() => useRetryAcquisition(), { wrapper });

      act(() => result.current.mutate(asTrackId('a')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      await waitFor(() =>
        expect(payloadsFor('retry_tapped')).toEqual([{ track_id: 'a', action: 'retry_tapped' }]),
      );
    });

    it('records a failed retry_request with the status once the request rejects', async () => {
      const { wrapper } = setup(ports);
      const { ApiError } = jest.requireActual('@shared/failure/errors');
      mockRetryAcquisition.mockRejectedValue(new ApiError(500, 'internal'));
      const { result } = renderHook(() => useRetryAcquisition('detail'), { wrapper });

      act(() => result.current.mutate(asTrackId('a')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      await waitFor(() =>
        expect(payloadsFor('retry_request')).toEqual([
          { track_id: 'a', action: 'retry_request', entry_point: 'detail', outcome: 'sent' },
          {
            track_id: 'a',
            action: 'retry_request',
            entry_point: 'detail',
            outcome: 'failed',
            status: 500,
          },
        ]),
      );
    });

    it('records a skipped retry_request for a second tap while the first is still in flight', async () => {
      const { wrapper } = setup(ports);
      mockRetryAcquisition.mockReturnValue(new Promise(() => undefined));
      const { result } = renderHook(() => useRetryAcquisition('library_row'), { wrapper });

      act(() => result.current.mutate(asTrackId('a')));
      await waitFor(() => expect(result.current.isInFlight(asTrackId('a'))).toBe(true));
      act(() => result.current.mutate(asTrackId('a')));

      expect(mockRetryAcquisition).toHaveBeenCalledTimes(1);
      await waitFor(() =>
        expect(payloadsFor('retry_request')).toEqual([
          {
            track_id: 'a',
            action: 'retry_request',
            entry_point: 'library_row',
            outcome: 'sent',
          },
          {
            track_id: 'a',
            action: 'retry_request',
            entry_point: 'library_row',
            outcome: 'skipped',
            reason: 'already_running',
          },
        ]),
      );
    });

    it('records a skipped retry_request for an unsafe id without ever sending the request', async () => {
      const { wrapper } = setup(ports);
      const { result } = renderHook(() => useRetryAcquisition('playlist'), { wrapper });
      const unsafeId = '../etc/passwd' as unknown as TrackId;

      act(() => result.current.mutate(unsafeId));

      expect(mockRetryAcquisition).not.toHaveBeenCalled();
      await waitFor(() =>
        expect(payloadsFor('retry_request')).toEqual([
          {
            track_id: unsafeId,
            action: 'retry_request',
            entry_point: 'playlist',
            outcome: 'skipped',
            reason: 'unsafe_id',
          },
        ]),
      );
    });
  });
});
