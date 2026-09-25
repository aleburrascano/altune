import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { asTrackId, type TrackId } from '@shared/api-client/ids';
import { enqueueCritical } from '@shared/telemetry/outbox';

import { useRetryAcquisition } from '../useRetryAcquisition';

const mockRetryAcquisition = jest.fn<Promise<void>, [TrackId]>();
jest.mock('@shared/api-client/tracks', () => ({
  retryAcquisition: (id: TrackId) => mockRetryAcquisition(id),
}));
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

const enqueueCriticalMock = enqueueCritical as jest.MockedFunction<typeof enqueueCritical>;

function payloadsFor(action: string): Record<string, unknown>[] {
  return enqueueCriticalMock.mock.calls
    .map(([event]) => event.payload as Record<string, unknown>)
    .filter((payload) => payload['action'] === action);
}

function setup() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }
  return { wrapper: Wrapper };
}

beforeEach(() => {
  mockRetryAcquisition.mockReset();
  enqueueCriticalMock.mockReset().mockResolvedValue(undefined);
});

describe('useRetryAcquisition — retry_tapped and retry_request telemetry', () => {
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
