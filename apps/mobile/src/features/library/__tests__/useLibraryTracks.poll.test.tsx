// #2506: the pending poll refetches only the page holding a pending row, and gives up on a stuck one.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import { TRACKS_PAGE_SIZE, useLibraryTracks } from '../hooks/useLibraryTracks';

const PAGES = 5;
let status: 'pending' | 'ready' = 'pending';
const mockGetTracks = jest.fn(({ limit, offset }: { limit: number; offset: number }) =>
  Promise.resolve({
    items: Array.from({ length: limit }, (_, i) => {
      const n = offset + i;
      return {
        id: asTrackId(`t${n}`),
        acquisition_status: n === 3 ? status : 'ready',
      } as TrackResponse;
    }),
    total: TRACKS_PAGE_SIZE * PAGES,
    limit,
    offset,
    has_more: offset + limit < TRACKS_PAGE_SIZE * PAGES,
  }),
);
jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (params: { limit: number; offset: number }) => mockGetTracks(params),
  getAllTracks: jest.fn(),
}));

let client: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

async function loadAllPages() {
  const hook = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
  for (let i = 1; i <= PAGES; i++) {
    await waitFor(() => expect(hook.result.current.tracks).toHaveLength(TRACKS_PAGE_SIZE * i));
    act(() => hook.result.current.onEndReached());
  }
  return hook;
}

beforeEach(() => {
  jest.useFakeTimers();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  status = 'pending';
  mockGetTracks.mockClear();
});

afterEach(() => {
  client.clear();
  jest.useRealTimers();
});

describe('useLibraryTracks pending poll', () => {
  it('refetches one page a minute, not every loaded page, and stops on a stuck row', async () => {
    await loadAllPages();
    mockGetTracks.mockClear();

    await act(() => jest.advanceTimersByTimeAsync(60_000));
    expect(mockGetTracks).toHaveBeenCalledTimes(1);

    await act(() => jest.advanceTimersByTimeAsync(60 * 60_000));
    expect(mockGetTracks.mock.calls.length).toBeLessThanOrEqual(10);
  });

  it('shows a track that goes ready', async () => {
    const hook = await loadAllPages();
    status = 'ready';
    await act(() => jest.advanceTimersByTimeAsync(60_000));
    expect(hook.result.current.tracks[3]?.acquisition_status).toBe('ready');
  });
});
