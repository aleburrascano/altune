import { Alert } from 'react-native';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError, NetworkError } from '@shared/errors';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { RETRY_TAIL } from '@shared/failure/describeError';
import type { ReactNode } from 'react';
import { QueryClientProvider } from '@tanstack/react-query';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';

import { useReacquireTrack } from '../hooks/useReacquireTrack';
import {
  track,
  playlist,
  PLAYLIST_KEY,
  setup,
  deferred,
  libraryOf,
  signOutThenLoadUserB,
  userBLibrary,
} from './trackMutationFixtures';

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

    expect(queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]).toMatchObject(
      { acquisition_status: 'pending', failure_reason: null, failure_message: null },
    );
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
      queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!.acquisition_status,
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
    await waitFor(() => expect(mockReacquireTrack).toHaveBeenCalled());

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
    await waitFor(() => expect(mockReacquireTrack).toHaveBeenCalled());

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
      queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!.acquisition_status,
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
      queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!.acquisition_status,
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
      queryClient.getQueryData<PlaylistDetailResponse>(PLAYLIST_KEY)!.tracks[0]!.acquisition_status,
    ).toBe('ready');
    expect(mockReacquireTrack).not.toHaveBeenCalled();
  });
});
