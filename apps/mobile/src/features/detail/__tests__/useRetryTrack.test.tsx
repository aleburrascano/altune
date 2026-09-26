// #2852: a detail-side Retry on an already-owned, failed track must hit the
// acquisition endpoint the server actually schedules from, and the status store
// it patches must be the same one the save pill and failure banner already read.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { useRetryTrack } from '../hooks/useRetryTrack';

const mockRetryAcquisition = jest.fn<Promise<void>, [unknown]>();
const mockEnqueueCritical = jest.fn();
jest.mock('@shared/telemetry/outbox', () => ({
  enqueueCritical: (event: unknown) => mockEnqueueCritical(event),
}));
jest.mock('@shared/api-client/tracks', () => ({
  retryAcquisition: (trackId: unknown) => mockRetryAcquisition(trackId),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

const TRACK_ID = asTrackId('server-1');

beforeEach(() => {
  mockRetryAcquisition.mockReset();
  mockEnqueueCritical.mockReset();
  useTrackStatusStore.getState().reset();
});

describe('useRetryTrack', () => {
  it('posts to the retry endpoint with the given trackId, never a create', async () => {
    mockRetryAcquisition.mockResolvedValue(undefined);
    const { result } = renderHook(() => useRetryTrack('detail'), { wrapper });

    await act(async () => {
      result.current.mutate(TRACK_ID);
    });

    expect(mockRetryAcquisition).toHaveBeenCalledWith(TRACK_ID);
  });

  it('marks the track pending immediately, before the retry settles', () => {
    mockRetryAcquisition.mockReturnValue(new Promise(() => undefined));
    const { result } = renderHook(() => useRetryTrack('detail'), { wrapper });

    act(() => {
      result.current.mutate(TRACK_ID);
    });

    expect(useTrackStatusStore.getState().statuses[TRACK_ID]).toEqual({
      acquisitionStatus: 'pending',
      failureMessage: null,
    });
  });

  it('marks the track failed again, with the new reason, when the retry itself fails', async () => {
    mockRetryAcquisition.mockRejectedValue(new Error('502 bad gateway'));
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { result } = renderHook(() => useRetryTrack('detail'), { wrapper });

    await act(async () => {
      result.current.mutate(TRACK_ID);
      await waitFor(() => expect(result.current.isPending).toBe(false));
    });

    expect(useTrackStatusStore.getState().statuses[TRACK_ID]).toEqual({
      acquisitionStatus: 'failed',
      failureMessage: '502 bad gateway',
    });
    warnSpy.mockRestore();
  });

  it('writes nothing to the status store when the retry fails after a sign-out', async () => {
    let rejectRetry!: (e: Error) => void;
    mockRetryAcquisition.mockReturnValue(
      new Promise<void>((_, reject) => {
        rejectRetry = reject;
      }),
    );
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { result } = renderHook(() => useRetryTrack('detail'), { wrapper });

    act(() => {
      result.current.mutate(TRACK_ID);
    });
    await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalled());
    runSignOutCleanups();
    await act(async () => rejectRetry(new Error('502 bad gateway')));
    await waitFor(() => expect(result.current.isPending).toBe(false));

    expect(useTrackStatusStore.getState().statuses[TRACK_ID]).toBeUndefined();
    warnSpy.mockRestore();
  });

  it('records the tap, the sent request and its success, all tagged with the entry point', async () => {
    mockRetryAcquisition.mockResolvedValue(undefined);
    const { result } = renderHook(() => useRetryTrack('detail'), { wrapper });

    await act(async () => {
      result.current.mutate(TRACK_ID);
    });
    await waitFor(() => expect(result.current.isPending).toBe(false));

    const retryEvents = mockEnqueueCritical.mock.calls
      .map(([event]) => event.payload)
      .filter((payload) => payload.action !== 'status_changed');
    expect(retryEvents).toEqual([
      { track_id: TRACK_ID, action: 'retry_tapped', entry_point: 'detail' },
      { track_id: TRACK_ID, action: 'retry_request', entry_point: 'detail', outcome: 'sent' },
      { track_id: TRACK_ID, action: 'retry_request', entry_point: 'detail', outcome: 'succeeded' },
    ]);
  });

  it('records a failed retry_request when the retry itself fails', async () => {
    mockRetryAcquisition.mockRejectedValue(new Error('network down'));
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { result } = renderHook(() => useRetryTrack('album_row'), { wrapper });

    await act(async () => {
      result.current.mutate(TRACK_ID);
    });
    await waitFor(() => expect(result.current.isPending).toBe(false));

    expect(mockEnqueueCritical).toHaveBeenCalledWith({
      type: 'acquisition_ui',
      payload: {
        track_id: TRACK_ID,
        action: 'retry_request',
        entry_point: 'album_row',
        outcome: 'failed',
      },
    });
    warnSpy.mockRestore();
  });
});
