import { act, renderHook, waitFor } from '@testing-library/react-native';

import { asTrackId } from '@shared/apiClient/ids';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { useRetryTrack } from '../hooks/useRetryTrack';
import { createMemoryDetailPort } from './memoryDetailPort';
import { createPortWrapper, createTestQueryClient, signInTestUser } from '@testing/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

interface PostedEvent {
  type: string;
  payload: Record<string, unknown>;
}

function postedEvents(): PostedEvent[] {
  return (__http.requests as { method: string; path: string; body: string }[])
    .filter((request) => request.method === 'POST' && request.path === '/v1/discovery/events')
    .map((request) => JSON.parse(request.body) as PostedEvent);
}

const mockRetryAcquisition = jest.fn<Promise<void>, [unknown]>();

let wrapper: ReturnType<typeof createPortWrapper>;

beforeEach(() => {
  wrapper = createPortWrapper(createTestQueryClient({ mutations: true }), {
    detail: createMemoryDetailPort(),
    acquisition: { retryAcquisition: (trackId) => mockRetryAcquisition(trackId) },
  });
});

const RETRY_EVENT_COUNT = 3;
const TRACK_ID = asTrackId('server-1');

beforeEach(async () => {
  await signInTestUser();
  __http.replyAll({ status: 204 });
  mockRetryAcquisition.mockReset();
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
      retryable: false,
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

    await waitFor(() => expect(postedEvents().length).toBeGreaterThanOrEqual(RETRY_EVENT_COUNT));
    const retryEvents = postedEvents()
      .map(({ payload: { session_id: _sessionId, ...payload } }) => payload)
      .filter(
        (payload) => payload.action !== 'status_changed' && payload.entry_point !== undefined,
      );
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

    await waitFor(() =>
      expect(postedEvents()).toContainEqual({
        type: 'acquisition_ui',
        event_id: expect.any(String),
        client_occurred_at: expect.any(String),
        payload: {
          track_id: TRACK_ID,
          action: 'retry_request',
          entry_point: 'album_row',
          outcome: 'failed',
          session_id: expect.any(String),
        },
      }),
    );
    warnSpy.mockRestore();
  });
});
