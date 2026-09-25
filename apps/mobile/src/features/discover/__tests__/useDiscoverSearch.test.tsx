import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import {
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';
import { recordEvent } from '@shared/telemetry/recordEvent';
import { MAX_SEARCH_PAGES, SEARCH_PAGE_SIZE } from '../searchLimits';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { resultFixture } from './fixtures';

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn(),
  listSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockSearch = searchDiscovery as jest.Mock;
const mockSuggest = suggestDiscovery as jest.Mock;
const mockHistory = listSearchHistory as jest.Mock;
const mockRecordEvent = recordEvent as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function endlessPage(offset: number): DiscoverySearchResponse {
  return {
    query: 'radiohead',
    query_norm: 'radiohead',
    search_id: 'search-1',
    results: Array.from({ length: SEARCH_PAGE_SIZE }, (_, index) =>
      resultFixture({ title: `result-${offset + index}` }),
    ),
    sections: [],
    providers: [
      { provider: 'spotify', status: 'ok', result_count: SEARCH_PAGE_SIZE, latency_ms: 80 },
    ],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 10_000,
    offset,
    has_more: true,
  };
}

type SearchHook = ReturnType<typeof useDiscoverSearch>;

function resultCount(result: { current: SearchHook }) {
  return result.current.data?.results.length ?? 0;
}

// fetchNextPage resolves as soon as the page is cached, a tick before the hook
// re-renders with it, so each round waits for the merged list itself to grow.
async function fetchWhileOffered(result: { current: SearchHook }, rounds: number) {
  for (let round = 0; round < rounds && result.current.hasNextPage; round += 1) {
    const before = resultCount(result);
    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(resultCount(result)).toBeGreaterThan(before));
  }
}

async function renderWithFirstPage() {
  const rendered = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });
  await waitFor(() => expect(resultCount(rendered.result)).toBe(SEARCH_PAGE_SIZE));
  await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  return rendered;
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  mockSearch
    .mockReset()
    .mockImplementation(({ offset }: { offset: number }) => Promise.resolve(endlessPage(offset)));
});

afterEach(() => {
  queryClient.clear();
});

describe('useDiscoverSearch merges its pages once and retains a bounded number of them', () => {
  it('returns the same merged results on a re-render that fetched nothing', async () => {
    const { result, rerender } = await renderWithFirstPage();
    const merged = result.current.data;

    rerender({});

    expect(result.current.data).toBe(merged);
    expect(result.current.data?.results).toBe(merged?.results);
  });

  it('returns a newly merged list once a page actually arrives', async () => {
    const { result } = await renderWithFirstPage();
    const firstPageOnly = result.current.data?.results;

    await fetchWhileOffered(result, 1);

    expect(result.current.data?.results).not.toBe(firstPageOnly);
    expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE * 2);
  });

  it('stops fetching at the page cap however long the server says results run', async () => {
    const { result } = await renderWithFirstPage();

    await fetchWhileOffered(result, MAX_SEARCH_PAGES + 1);

    expect(result.current.hasNextPage).toBe(false);
    expect(mockSearch).toHaveBeenCalledTimes(MAX_SEARCH_PAGES);
    expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE * MAX_SEARCH_PAGES);
  });

  it('keeps the oldest page at the cap rather than dropping it', async () => {
    const { result } = await renderWithFirstPage();

    await fetchWhileOffered(result, MAX_SEARCH_PAGES + 1);

    expect(result.current.data?.results[0]?.title).toBe('result-0');
    expect(result.current.data?.offset).toBe(0);
  });
});

describe('an explicit submit of a query the debounce just fetched', () => {
  function singlePage(): DiscoverySearchResponse {
    return {
      query: 'radiohead',
      query_norm: 'radiohead',
      search_id: 'search-1',
      results: [],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 0,
      offset: 0,
      has_more: false,
    };
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: 30_000 } },
    });
    mockSearch.mockReset().mockImplementation(() => Promise.resolve(singlePage()));
  });

  afterEach(() => {
    queryClient.clear();
  });

  it('still sends a request with saveHistory true', async () => {
    const { result, rerender } = renderHook(
      ({ saveHistory }: { saveHistory: boolean }) => useDiscoverSearch('radiohead', saveHistory),
      { wrapper, initialProps: { saveHistory: false } },
    );
    await waitFor(() => expect(result.current.data).toBeDefined());

    rerender({ saveHistory: true });

    await waitFor(() =>
      expect(mockSearch).toHaveBeenCalledWith(
        expect.objectContaining({ q: 'radiohead', saveHistory: true }),
        expect.anything(),
      ),
    );
  });
});

describe('useDiscoverSearch refresh', () => {
  function page(offset: number): DiscoverySearchResponse {
    return {
      query: 'radiohead',
      query_norm: 'radiohead',
      search_id: 'search-1',
      results: Array.from({ length: SEARCH_PAGE_SIZE }, (_, index) =>
        resultFixture({ title: `result-${offset + index}` }),
      ),
      sections: [],
      providers: [
        { provider: 'spotify', status: 'ok', result_count: SEARCH_PAGE_SIZE, latency_ms: 80 },
      ],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 10_000,
      offset,
      has_more: true,
    };
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    mockSearch
      .mockReset()
      .mockImplementation(({ offset }: { offset: number }) => Promise.resolve(page(offset)));
  });

  afterEach(() => {
    queryClient.clear();
  });

  it('reports isRefreshing during a refetch of loaded data and refetches only page 1', async () => {
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });
    for (let loaded = 1; loaded <= 3; loaded += 1) {
      await waitFor(() =>
        expect(result.current.data?.results.length).toBe(SEARCH_PAGE_SIZE * loaded),
      );
      await waitFor(() => expect(queryClient.isFetching()).toBe(0));
      if (loaded < 3) {
        await act(async () => {
          await result.current.fetchNextPage();
        });
      }
    }
    expect(result.current.isRefreshing).toBe(false);

    mockSearch.mockClear();
    let release: () => void = () => undefined;
    mockSearch.mockImplementation(
      ({ offset }: { offset: number }) =>
        new Promise((resolve) => {
          release = () => resolve(page(offset));
        }),
    );
    act(() => {
      result.current.refetch();
    });
    await waitFor(() => expect(result.current.isRefreshing).toBe(true));
    await act(async () => {
      release();
    });
    await waitFor(() => expect(result.current.isRefreshing).toBe(false));

    expect(mockSearch).toHaveBeenCalledTimes(1);
    expect(mockSearch.mock.calls[0][0].offset).toBe(0);
  });
});

describe('held-slate paging', () => {
  function page(offset: number, searchId: string): DiscoverySearchResponse {
    return {
      query: 'q',
      query_norm: 'q',
      search_id: searchId,
      results: Array.from({ length: SEARCH_PAGE_SIZE }, () => resultFixture()),
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 100,
      offset,
      has_more: true,
    };
  }

  beforeEach(() => {
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    mockSearch.mockReset();
  });

  afterEach(() => queryClient.clear());

  it('sends page 1 search_id on the page 2 request, not a fresh search', async () => {
    mockSearch
      .mockResolvedValueOnce(page(0, 'search-1'))
      .mockResolvedValueOnce(page(SEARCH_PAGE_SIZE, 'search-1'));
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(async () => {
      await result.current.fetchNextPage();
    });

    await waitFor(() => expect(mockSearch).toHaveBeenCalledTimes(2));
    expect(mockSearch.mock.calls[1]?.[0]).toMatchObject({
      offset: SEARCH_PAGE_SIZE,
      searchId: 'search-1',
    });
  });

  it('sends no search_id on the first page', async () => {
    mockSearch.mockResolvedValueOnce(page(0, 'search-1'));
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    expect(mockSearch.mock.calls[0]?.[0]).not.toHaveProperty('searchId');
  });
});

describe('next page failure', () => {
  function page(offset: number): DiscoverySearchResponse {
    return {
      query: 'q',
      query_norm: 'q',
      search_id: 's',
      results: Array.from({ length: SEARCH_PAGE_SIZE }, () => resultFixture()),
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 100,
      offset,
      has_more: true,
    };
  }

  beforeEach(() => {
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    mockSearch.mockReset();
  });

  afterEach(() => queryClient.clear());

  it('flags a rejected page 2 while keeping page 1', async () => {
    mockSearch
      .mockResolvedValueOnce(page(0))
      .mockRejectedValueOnce(new ApiError(502, 'bad gateway'));
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());
    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.isFetchNextPageError).toBe(true));
    expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE);
  });
});

describe('discover query failures emit a search_failed telemetry event tagged with its source', () => {
  function failureEvents() {
    return mockRecordEvent.mock.calls
      .map((call) => call[0] as { type: string; payload?: Record<string, unknown> })
      .filter((event) => event.type === 'search_failed');
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    mockSearch.mockReset();
    mockSuggest.mockReset();
    mockHistory.mockReset();
    mockRecordEvent.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    queryClient.clear();
  });

  it('a failed search fires search_failed with source search and the HTTP status', async () => {
    mockSearch.mockRejectedValue(new ApiError(503, 'unavailable'));
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]).toEqual({
      type: 'search_failed',
      payload: { source: 'search', status: 503 },
    });
  });

  it("a failed search reports the request's correlation id, so the event matches the server log", async () => {
    mockSearch.mockRejectedValue(
      new ApiError(503, 'unavailable', 'unavailable', 'a1b2c3d4e5f60718'),
    );
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]?.payload).toEqual({
      source: 'search',
      status: 503,
      correlationId: 'a1b2c3d4e5f60718',
    });
  });
});
