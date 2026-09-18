// #1697: nothing on the wire promises has_more implies a non-empty slice. A page that
// reports more tracks and serves none advances the offset by zero, so an unguarded cursor
// re-requests the identical page for as long as the list is scrolled.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asTrackId } from '@shared/api-client/ids';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';

import { TRACKS_PAGE_SIZE, useLibraryTracks } from '../hooks/useLibraryTracks';

const mockGetTracks = jest.fn();
jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (params: { limit: number; offset: number }) => mockGetTracks(params),
  getAllTracks: jest.fn(),
}));

const track = (i: number) => ({ id: asTrackId(`t${i}`) }) as TrackResponse;

function page(offset: number, items: TrackResponse[], hasMore: boolean): ListTracksResponse {
  return { items, total: 3, limit: TRACKS_PAGE_SIZE, offset, has_more: hasMore };
}

const firstPage = page(0, [track(0), track(1)], true);
const emptyPageClaimingMore = page(2, [], true);
const lastPage = page(2, [track(2)], false);

let client: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

async function loadFirstPage() {
  const hook = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
  await waitFor(() => expect(hook.result.current.tracks).toHaveLength(2));
  return hook;
}

async function settle(hook: Awaited<ReturnType<typeof loadFirstPage>>) {
  await act(async () => {
    await Promise.resolve();
  });
  await waitFor(() => expect(hook.result.current.isFetchingNextPage).toBe(false));
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockGetTracks.mockReset();
  mockGetTracks.mockResolvedValueOnce(firstPage);
});

afterEach(() => client.clear());

describe('useLibraryTracks on a page that reports more tracks than it serves', () => {
  it('stops requesting once a page comes back empty', async () => {
    mockGetTracks.mockResolvedValue(emptyPageClaimingMore);
    const hook = await loadFirstPage();

    act(() => hook.result.current.onEndReached());
    await waitFor(() => expect(mockGetTracks).toHaveBeenCalledTimes(2));
    await settle(hook);
    act(() => hook.result.current.onEndReached());
    await settle(hook);

    expect(mockGetTracks).toHaveBeenCalledTimes(2);
    expect(hook.result.current.tracks.map((t) => t.id)).toEqual(['t0', 't1']);
  });

  it('keeps requesting the next offset while pages still serve tracks', async () => {
    mockGetTracks.mockResolvedValue(lastPage);
    const hook = await loadFirstPage();

    act(() => hook.result.current.onEndReached());
    await waitFor(() => expect(hook.result.current.tracks).toHaveLength(3));

    expect(mockGetTracks).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 2 }));
  });
});
