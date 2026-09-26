// Failure paths of useRetryAcquisition (#788): a failed retry restores the cache it
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
import { enqueueCritical } from '@shared/telemetry/outbox';

import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import {
  track,
  page,
  playlist,
  TRACKS_KEY,
  PLAYLIST_KEY,
  setup,
  pagedIds,
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
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

const enqueueCriticalMock = enqueueCritical as jest.MockedFunction<typeof enqueueCritical>;

function payloadsFor(action: string): Record<string, unknown>[] {
  return enqueueCriticalMock.mock.calls
    .map(([event]) => event.payload as Record<string, unknown>)
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

// #795: every failure used to roll back and show the same "try again" Alert, so a
// track deleted elsewhere came back as a ghost row and a refused session was told
// to just retry. The hooks now branch on the failure class.
describe('track mutation hooks — respond to the failure class, not one generic path', () => {
  const vanished = ['Track not found', 'This track is no longer in your library.'] as const;

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
});

describe('track mutations that settle after sign-out leave the next user untouched (#2729)', () => {
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
});

describe('useRetryAcquisition — retry_tapped and retry_request telemetry', () => {
  beforeEach(() => {
    enqueueCriticalMock.mockReset().mockResolvedValue(undefined);
  });

  it('records retry_tapped with the given entry point, then a sent and succeeded retry_request', async () => {
    const { wrapper } = setup();
    mockRetryAcquisition.mockResolvedValue(undefined);
    const { result } = renderHook(() => useRetryAcquisition('album_row'), { wrapper });

    act(() => result.current.mutate(asTrackId('a')));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(payloadsFor('retry_tapped')).toEqual([
      { track_id: 'a', action: 'retry_tapped', entry_point: 'album_row' },
    ]);
    expect(payloadsFor('retry_request')).toEqual([
      { track_id: 'a', action: 'retry_request', entry_point: 'album_row', outcome: 'sent' },
      { track_id: 'a', action: 'retry_request', entry_point: 'album_row', outcome: 'succeeded' },
    ]);
  });

  it('has no entry point in the payload when none is given', async () => {
    const { wrapper } = setup();
    mockRetryAcquisition.mockResolvedValue(undefined);
    const { result } = renderHook(() => useRetryAcquisition(), { wrapper });

    act(() => result.current.mutate(asTrackId('a')));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(payloadsFor('retry_tapped')).toEqual([{ track_id: 'a', action: 'retry_tapped' }]);
  });

  it('records a failed retry_request with the status once the request rejects', async () => {
    const { wrapper } = setup();
    const { ApiError } = jest.requireActual('@shared/errors');
    mockRetryAcquisition.mockRejectedValue(new ApiError(500, 'internal'));
    const { result } = renderHook(() => useRetryAcquisition('detail'), { wrapper });

    act(() => result.current.mutate(asTrackId('a')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(payloadsFor('retry_request')).toEqual([
      { track_id: 'a', action: 'retry_request', entry_point: 'detail', outcome: 'sent' },
      {
        track_id: 'a',
        action: 'retry_request',
        entry_point: 'detail',
        outcome: 'failed',
        status: 500,
      },
    ]);
  });

  it('records a skipped retry_request for a second tap while the first is still in flight', async () => {
    const { wrapper } = setup();
    mockRetryAcquisition.mockReturnValue(new Promise(() => undefined));
    const { result } = renderHook(() => useRetryAcquisition('library_row'), { wrapper });

    act(() => result.current.mutate(asTrackId('a')));
    await waitFor(() => expect(result.current.isInFlight(asTrackId('a'))).toBe(true));
    act(() => result.current.mutate(asTrackId('a')));

    expect(mockRetryAcquisition).toHaveBeenCalledTimes(1);
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
    ]);
  });

  it('records a skipped retry_request for an unsafe id without ever sending the request', () => {
    const { wrapper } = setup();
    const { result } = renderHook(() => useRetryAcquisition('playlist'), { wrapper });
    const unsafeId = '../etc/passwd' as unknown as TrackId;

    act(() => result.current.mutate(unsafeId));

    expect(mockRetryAcquisition).not.toHaveBeenCalled();
    expect(payloadsFor('retry_request')).toEqual([
      {
        track_id: unsafeId,
        action: 'retry_request',
        entry_point: 'playlist',
        outcome: 'skipped',
        reason: 'unsafe_id',
      },
    ]);
  });
});
