// #1697: nothing on the wire promises has_more implies a non-empty slice. A page that
// reports more tracks and serves none advances the offset by zero, so an unguarded cursor
// re-requests the identical page for as long as the list is scrolled.

// #790: "shuffle/play whole library" resolves through loadAll. When the full-library
// fetch fails it falls back to the pages already loaded (playing a subset beats a tap
// that does nothing), but that degradation must leave a trace instead of being silent.

// #792: infinite-scroll pages are fetched by offset. Deleting a track from an earlier,
// already-loaded page renumbers the server list; the next fetchNextPage must follow that
// renumbering instead of trusting the later page's stale offset and skipping a track.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asTrackId } from '@shared/api-client/ids';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { ApiError } from '@shared/errors';
import {
  captureTrackPlacements,
  removeTrackFromCaches,
  restoreTrackPlacements,
} from '@shared/events/trackCachePatch';

import { TRACKS_PAGE_SIZE, useLibraryTracks } from '../hooks/useLibraryTracks';

const mockGetTracks = jest.fn();
const mockGetAllTracks = jest.fn();
jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (params: unknown) => mockGetTracks(params),
  getAllTracks: (params: unknown) => mockGetAllTracks(params),
}));

// Each describe below serves its own fake API; start every test from a blank one.
beforeEach(() => {
  mockGetTracks.mockReset();
  mockGetAllTracks.mockReset();
});

describe('useLibraryTracks on a page that reports more tracks than it serves', () => {
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

describe('useLibraryTracks loadAll', () => {
  function wrapper({ children }: { children: ReactNode }) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  const loaded = [{ id: asTrackId('a') }, { id: asTrackId('b') }];

  beforeEach(() => {
    mockGetTracks.mockReset();
    mockGetAllTracks.mockReset();
    mockGetTracks.mockResolvedValue({
      items: loaded,
      total: 5,
      limit: 200,
      offset: 0,
      has_more: true,
    });
  });

  it('resolves the full library when the fetch succeeds', async () => {
    const all = [...loaded, { id: asTrackId('c') }];
    mockGetAllTracks.mockResolvedValue(all);
    const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(2));

    let resolved: unknown;
    await act(async () => {
      resolved = await result.current.loadAll();
    });

    expect(resolved).toEqual(all);
  });

  it('falls back to the loaded pages on failure and records that the fallback fired', async () => {
    mockGetAllTracks.mockRejectedValue(new Error('boom'));
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(2));

    let resolved: unknown;
    await act(async () => {
      resolved = await result.current.loadAll();
    });

    expect(resolved).toEqual(loaded);
    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining('[library]'),
      expect.objectContaining({ loaded: 2 }),
    );
    warn.mockRestore();
  });
});

describe('useLibraryTracks loadAll failure log', () => {
  function wrapper({ children }: { children: ReactNode }) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  it('logs status, failure class and correlation id when the fetch is rejected', async () => {
    mockGetTracks.mockResolvedValue({
      items: [{ id: asTrackId('a') }],
      total: 5,
      limit: 200,
      offset: 0,
      has_more: true,
    });
    mockGetAllTracks.mockRejectedValue(new ApiError(503, 'unavailable', undefined, 'corr-1'));
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(1));

    await act(async () => {
      await result.current.loadAll();
    });

    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining('[library]'),
      expect.objectContaining({
        loaded: 1,
        status: 503,
        failure: expect.any(String),
        correlationId: 'corr-1',
      }),
    );
    warn.mockRestore();
  });
});

describe('useLibraryTracks loadAll refetch', () => {
  function wrapper({ children }: { children: ReactNode }) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  it('refetches the whole library on every loadAll', async () => {
    const a = { id: asTrackId('a') };
    const b = { id: asTrackId('b') };
    mockGetTracks.mockResolvedValue({
      items: [a],
      total: 1,
      limit: 200,
      offset: 0,
      has_more: false,
    });
    mockGetAllTracks.mockResolvedValueOnce([a, b]).mockResolvedValueOnce([a]);
    const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(1));

    let first: unknown;
    let second: unknown;
    await act(async () => {
      first = await result.current.loadAll();
      second = await result.current.loadAll();
    });

    expect(first).toEqual([a, b]);
    expect(second).toEqual([a]);
    expect(mockGetAllTracks).toHaveBeenCalledTimes(2);
  });
});

describe('useLibraryTracks pagination after a delete', () => {
  // A fake server: an ordered list served by offset/limit, exactly like GET /tracks.
  let server: TrackResponse[] = [];
  const serveLibraryPage = ({ limit, offset }: { limit: number; offset: number }) =>
    Promise.resolve({
      items: server.slice(offset, offset + limit),
      total: server.length,
      limit,
      offset,
      has_more: offset + limit < server.length,
    });

  const track = (i: number) => ({ id: asTrackId(`t${i}`) }) as TrackResponse;

  let client: QueryClient;
  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  // Two full pages plus a partial third, so page 3 is still unfetched after two loads.
  const LIBRARY_SIZE = TRACKS_PAGE_SIZE * 2 + 3;

  async function loadTwoPages() {
    const hook = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(hook.result.current.tracks).toHaveLength(TRACKS_PAGE_SIZE));
    act(() => hook.result.current.onEndReached());
    await waitFor(() => expect(hook.result.current.tracks).toHaveLength(TRACKS_PAGE_SIZE * 2));
    return hook;
  }

  async function loadRest(hook: Awaited<ReturnType<typeof loadTwoPages>>, expected: number) {
    act(() => hook.result.current.onEndReached());
    await waitFor(() => expect(hook.result.current.tracks).toHaveLength(expected));
  }

  beforeEach(() => {
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    server = Array.from({ length: LIBRARY_SIZE }, (_, i) => track(i));
    mockGetTracks.mockImplementation(serveLibraryPage);
  });

  afterEach(() => client.clear());

  it('does not skip a track when a track on an earlier loaded page is deleted', async () => {
    const hook = await loadTwoPages();

    server = server.filter((t) => t.id !== 't5');
    act(() => removeTrackFromCaches(client, asTrackId('t5')));
    await loadRest(hook, LIBRARY_SIZE - 1);

    expect(mockGetTracks).toHaveBeenLastCalledWith(
      expect.objectContaining({ offset: TRACKS_PAGE_SIZE * 2 - 1 }),
    );
    expect(hook.result.current.tracks.map((t) => t.id)).toEqual(server.map((t) => t.id));
  });

  it('keeps the offsets right after a failed delete is rolled back', async () => {
    const hook = await loadTwoPages();

    const placements = captureTrackPlacements(client, asTrackId('t5'));
    act(() => removeTrackFromCaches(client, asTrackId('t5')));
    act(() => restoreTrackPlacements(client, placements));
    await loadRest(hook, LIBRARY_SIZE);

    expect(hook.result.current.tracks.map((t) => t.id)).toEqual(server.map((t) => t.id));
  });
});

describe('useLibraryTracks pending poll', () => {
  const PAGES = 5;
  let status: 'pending' | 'ready' = 'pending';
  const servePollPage = ({ limit, offset }: { limit: number; offset: number }) =>
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
    });

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
    mockGetTracks.mockImplementation(servePollPage);
  });

  afterEach(() => {
    client.clear();
    jest.useRealTimers();
  });

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
