import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asTrackId } from '@shared/apiClient/ids';
import type { ListTracksResponse, TrackResponse } from '@shared/apiClient/trackResponse';
import { ApiError } from '@shared/failure/errors';
import { libraryKeys } from '@shared/query/queryKeys';
import {
  removeTrackFromCaches,
  captureTrackPlacements,
  restoreTrackPlacements,
} from '@shared/events/trackCachePatch';

import { useLibraryTracks } from '@features/library/hooks/useLibraryTracks';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { track as seededTrack } from '@features/library/__tests__/trackMutationFixtures';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';

const SERVICE_UNAVAILABLE = 503;
const MAX_PENDING_POLLS = 10;
const TRACKS_PAGE_SIZE = 200;

function track(i: number): TrackResponse {
  return seededTrack(`t${i}`);
}

function library(size: number): TrackResponse[] {
  return Array.from({ length: size }, (_, i) => track(i));
}

function ids(tracks: readonly TrackResponse[]): string[] {
  return tracks.map((t) => t.id);
}

function spiedPort(tracks: TrackResponse[]) {
  const memory = createMemoryLibraryPort({ tracks });
  return {
    ...memory,
    getTracks: jest.fn(memory.getTracks),
    getAllTracks: jest.fn(memory.getAllTracks),
  };
}

let client: QueryClient;
let port: LibraryPort;

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});

afterEach(() => client.clear());

describe('useLibraryTracks on a page that reports more tracks than it serves', () => {
  function page(offset: number, items: TrackResponse[], hasMore: boolean): ListTracksResponse {
    return { items, total: 3, limit: TRACKS_PAGE_SIZE, offset, has_more: hasMore };
  }

  const FIRST_AND_EMPTY_PAGE = 2;
  const firstPage = page(0, [track(0), track(1)], true);
  const emptyPageClaimingMore = page(2, [], true);

  let spied: ReturnType<typeof spiedPort>;

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
    spied = spiedPort(library(firstPage.total));
    spied.getTracks.mockResolvedValueOnce(firstPage);
    port = spied;
  });

  it('stops requesting once a page comes back empty', async () => {
    spied.getTracks.mockResolvedValue(emptyPageClaimingMore);
    const hook = await loadFirstPage();

    act(() => hook.result.current.onEndReached());
    await waitFor(() => expect(spied.getTracks).toHaveBeenCalledTimes(FIRST_AND_EMPTY_PAGE));
    await settle(hook);
    act(() => hook.result.current.onEndReached());
    await settle(hook);

    expect(spied.getTracks).toHaveBeenCalledTimes(FIRST_AND_EMPTY_PAGE);
    expect(ids(hook.result.current.tracks)).toEqual(['t0', 't1']);
  });

  it('keeps requesting the next offset while pages still serve tracks', async () => {
    const hook = await loadFirstPage();

    act(() => hook.result.current.onEndReached());
    await waitFor(() => expect(hook.result.current.tracks).toHaveLength(3));

    expect(ids(hook.result.current.tracks)).toEqual(['t0', 't1', 't2']);
    expect(spied.getTracks).toHaveBeenLastCalledWith(
      expect.objectContaining({ offset: 2 }),
      expect.anything(),
    );
  });
});

describe('useLibraryTracks loadAll', () => {
  const whole = library(TRACKS_PAGE_SIZE + 1);
  const loadedPage = whole.slice(0, TRACKS_PAGE_SIZE);

  async function loadFirstPage() {
    const hook = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(hook.result.current.tracks).toHaveLength(TRACKS_PAGE_SIZE));
    return hook;
  }

  it('resolves the full library when the fetch succeeds', async () => {
    port = createMemoryLibraryPort({ tracks: whole });
    const { result } = await loadFirstPage();

    let resolved: TrackResponse[] = [];
    await act(async () => {
      resolved = await result.current.loadAll();
    });

    expect(ids(resolved)).toEqual(ids(whole));
  });

  it('falls back to the loaded pages on failure and records that the fallback fired', async () => {
    const spied = spiedPort(whole);
    spied.getAllTracks.mockRejectedValue(new Error('boom'));
    port = spied;
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    const { result } = await loadFirstPage();

    let resolved: TrackResponse[] = [];
    await act(async () => {
      resolved = await result.current.loadAll();
    });

    expect(ids(resolved)).toEqual(ids(loadedPage));
    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining('[library]'),
      expect.objectContaining({ loaded: TRACKS_PAGE_SIZE }),
    );
    warn.mockRestore();
  });

  it('logs status, failure class and correlation id when the fetch is rejected', async () => {
    const spied = spiedPort(whole);
    spied.getAllTracks.mockRejectedValue(
      new ApiError(SERVICE_UNAVAILABLE, 'unavailable', undefined, 'corr-1'),
    );
    port = spied;
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    const { result } = await loadFirstPage();

    await act(async () => {
      await result.current.loadAll();
    });

    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining('[library]'),
      expect.objectContaining({
        loaded: TRACKS_PAGE_SIZE,
        status: 503,
        failure: expect.any(String),
        correlationId: 'corr-1',
      }),
    );
    warn.mockRestore();
  });

  it('refetches the whole library on every loadAll', async () => {
    port = createMemoryLibraryPort({ tracks: [seededTrack('a'), seededTrack('b')] });
    const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(2));

    let first: TrackResponse[] = [];
    let second: TrackResponse[] = [];
    await act(async () => {
      first = await result.current.loadAll();
      await port.deleteTrack(asTrackId('b'));
      second = await result.current.loadAll();
    });

    expect(ids(first)).toEqual(['a', 'b']);
    expect(ids(second)).toEqual(['a']);
  });

  it('passes the query and the query signal to the whole-library fetch and unwraps its items', async () => {
    const spied = spiedPort([seededTrack('a')]);
    port = spied;
    const { result } = renderHook(() => useLibraryTracks('q', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(1));

    let resolved: TrackResponse[] = [];
    await act(async () => {
      resolved = await result.current.loadAll();
    });

    expect(ids(resolved)).toEqual(['a']);
    expect(spied.getAllTracks).toHaveBeenCalledWith(
      expect.objectContaining({ q: 'q', signal: expect.any(AbortSignal) }),
    );
  });
});

describe('useLibraryTracks pagination after a delete', () => {
  const LIBRARY_SIZE = TRACKS_PAGE_SIZE * 2 + 3;
  let spied: ReturnType<typeof spiedPort>;

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
    spied = spiedPort(library(LIBRARY_SIZE));
    port = spied;
  });

  it('does not skip a track when a track on an earlier loaded page is deleted', async () => {
    const hook = await loadTwoPages();

    await act(async () => {
      await port.deleteTrack(asTrackId('t5'));
      removeTrackFromCaches(client, asTrackId('t5'));
    });
    await loadRest(hook, LIBRARY_SIZE - 1);

    expect(spied.getTracks).toHaveBeenLastCalledWith(
      expect.objectContaining({ offset: TRACKS_PAGE_SIZE * 2 - 1 }),
      expect.anything(),
    );
    expect(ids(hook.result.current.tracks)).toEqual(
      ids(library(LIBRARY_SIZE)).filter((id) => id !== 't5'),
    );
  });

  it('keeps the offsets right after a failed delete is rolled back', async () => {
    const hook = await loadTwoPages();

    const placements = captureTrackPlacements(client, asTrackId('t5'));
    act(() => removeTrackFromCaches(client, asTrackId('t5')));
    act(() => restoreTrackPlacements(client, placements));
    await loadRest(hook, LIBRARY_SIZE);

    expect(ids(hook.result.current.tracks)).toEqual(ids(library(LIBRARY_SIZE)));
  });
});

describe('useLibraryTracks pending poll', () => {
  const PAGES = 5;
  const PENDING_ID = 't3';
  let status: 'pending' | 'ready' = 'pending';
  let spied: ReturnType<typeof spiedPort>;

  function servePollPage(memory: LibraryPort): LibraryPort['getTracks'] {
    return async (params, signal) => {
      const served = await memory.getTracks(params, signal);
      const items = served.items.map((t) =>
        t.id === PENDING_ID ? ({ ...t, acquisition_status: status } as TrackResponse) : t,
      );
      return { ...served, items };
    };
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
    status = 'pending';
    const memory = createMemoryLibraryPort({ tracks: library(TRACKS_PAGE_SIZE * PAGES) });
    spied = {
      ...memory,
      getTracks: jest.fn(servePollPage(memory)),
      getAllTracks: jest.fn(memory.getAllTracks),
    };
    port = spied;
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('refetches one page a minute, not every loaded page, and stops on a stuck row', async () => {
    await loadAllPages();
    spied.getTracks.mockClear();

    await act(() => jest.advanceTimersByTimeAsync(60_000));
    expect(spied.getTracks).toHaveBeenCalledTimes(1);

    await act(() => jest.advanceTimersByTimeAsync(60 * 60_000));
    expect(spied.getTracks.mock.calls.length).toBeLessThanOrEqual(MAX_PENDING_POLLS);
  });

  it('shows a track that goes ready', async () => {
    const hook = await loadAllPages();
    status = 'ready';
    await act(() => jest.advanceTimersByTimeAsync(60_000));
    expect(hook.result.current.tracks[3]?.acquisition_status).toBe('ready');
  });

  function holdNextPoll() {
    let land: () => void = () => undefined;
    const serve = spied.getTracks.getMockImplementation()!;
    spied.getTracks.mockImplementationOnce(
      (params, signal) =>
        new Promise((resolve) => {
          land = () => resolve(serve(params, signal));
        }),
    );
    return () => land();
  }

  it('does not re-insert a track deleted while a poll response was in flight', async () => {
    const hook = await loadAllPages();
    const land = holdNextPoll();
    await act(() => jest.advanceTimersByTimeAsync(60_000));
    await act(async () => {
      removeTrackFromCaches(client, asTrackId('t1'));
    });
    await act(async () => {
      land();
    });
    await act(() => jest.advanceTimersByTimeAsync(0));
    expect(ids(hook.result.current.tracks)).not.toContain('t1');
  });

  it('sends no poll request while the previous one is unsettled', async () => {
    await loadAllPages();
    spied.getTracks.mockClear();
    holdNextPoll();
    await act(() => jest.advanceTimersByTimeAsync(3 * 60_000));
    expect(spied.getTracks).toHaveBeenCalledTimes(1);
  });

  it('writes nothing when the consumer unmounts with a poll in flight', async () => {
    const hook = await loadAllPages();
    const land = holdNextPoll();
    await act(() => jest.advanceTimersByTimeAsync(60_000));
    status = 'ready';
    hook.unmount();
    await act(async () => {
      land();
    });
    const cached = client.getQueryData<{ pages: ListTracksResponse[] }>(
      libraryKeys.tracks('', 'recent'),
    );
    expect(cached).toBeDefined();
    expect(cached?.pages.flatMap((p) => p.items)[3]?.acquisition_status).toBe('pending');
  });
});
