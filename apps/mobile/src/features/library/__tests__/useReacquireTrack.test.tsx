// Failure paths of useReacquireTrack (#788): a failed re-acquire leaves the cache as
// it was and logs a redacted line with track id and endpoint.

import { Alert } from 'react-native';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/errors';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse } from '@shared/api-client/types';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { RETRY_TAIL } from '@shared/lib/describeError';

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

describe('track mutations that settle after sign-out leave the next user untouched (#2729)', () => {
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
