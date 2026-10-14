import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { NetworkError } from '@shared/apiClient';
import { CORRELATION_HEADER } from '@shared/apiClient/correlationId';
import { transientRetryOptions } from '@shared/query/retryDelay';
import type { DiscoverySearchResponse } from '@shared/apiClient/discovery';
import {
  MAX_SEARCH_OFFSET,
  MAX_SEARCH_PAGES,
  SEARCH_PAGE_SIZE,
} from '@features/discovery/domain/searchPaging';
import { setSearchState } from '@features/discovery/domain/searchState';
import { useDebouncedSearch } from '@features/discovery/hooks/useDebouncedSearch';
import { useDiscoverSearch } from '@features/discovery/hooks/useDiscoverSearch';
import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { signInTestUser } from '@testing/signInTestUser';
import { FeaturePorts } from '@/wiring/FeaturePorts';
import { httpFeaturePorts } from '@/wiring/httpFeaturePorts';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

const SEARCH = 'GET /v1/discovery/search';
const EVENTS = 'POST /v1/discovery/events';
const EVENTS_PATH = '/v1/discovery/events';
const TWO_REQUESTS = 2;
const THIRD_PAGE_OFFSET = SEARCH_PAGE_SIZE + SEARCH_PAGE_SIZE;

type SentRequest = { path: string; query: string; headers: Record<string, string> };

function searchRequests(): SentRequest[] {
  return (__http.requests as SentRequest[]).filter(
    (request) => request.path === '/v1/discovery/search',
  );
}

function paramsOf(request: SentRequest | undefined): URLSearchParams {
  return new URLSearchParams(request?.query);
}

function offsetOf(request: SentRequest | undefined): number {
  return Number(paramsOf(request).get('offset') ?? 0);
}

function savedFlags(): boolean[] {
  return searchRequests().map((request) => paramsOf(request).get('save_history') !== 'false');
}

function servePages(...pages: DiscoverySearchResponse[]): void {
  for (const page of pages) __http.replyOnce(SEARCH, { json: page });
}

function serveEndlessPages(count: number): void {
  servePages(...Array.from({ length: count }, (_, index) => endlessPage(index * SEARCH_PAGE_SIZE)));
}

function holdSearchesUntilReleased(): () => void {
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  global.fetch = ((...args: Parameters<typeof fetch>) =>
    gate.then(() => fakeFetch(...args))) as typeof fetch;
  return release;
}

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
    </QueryClientProvider>
  );
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

beforeEach(async () => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
  serveEndlessPages(MAX_SEARCH_PAGES + 1);
});

afterEach(() => {
  queryClient.clear();
  global.fetch = fakeFetch;
  jest.restoreAllMocks();
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

  it('stops fetching at the server offset ceiling however long the server says results run', async () => {
    const { result } = await renderWithFirstPage();
    const lastAllowedPage = MAX_SEARCH_OFFSET / SEARCH_PAGE_SIZE + 1;

    await fetchWhileOffered(result, MAX_SEARCH_PAGES + 1);

    expect(result.current.hasNextPage).toBe(false);
    expect(result.current.isFetchNextPageError).toBe(false);
    expect(searchRequests()).toHaveLength(lastAllowedPage);
    const offsets = searchRequests().map(offsetOf);
    expect(Math.max(...offsets)).toBe(MAX_SEARCH_OFFSET);
    expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE * lastAllowedPage);
  });

  it('keeps the oldest page at the cap rather than dropping it', async () => {
    const { result } = await renderWithFirstPage();

    await fetchWhileOffered(result, MAX_SEARCH_PAGES + 1);

    expect(result.current.data?.results[0]?.title).toBe('result-0');
    expect(result.current.data?.offset).toBe(0);
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
    __http.reset();
    servePages(page(0), page(SEARCH_PAGE_SIZE), page(THIRD_PAGE_OFFSET));
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

    const sentBefore = searchRequests().length;
    __http.reply(SEARCH, { json: page(0) });
    const release = holdSearchesUntilReleased();
    act(() => {
      result.current.refetch();
    });
    await waitFor(() => expect(result.current.isRefreshing).toBe(true));
    await act(async () => {
      release();
    });
    await waitFor(() => expect(result.current.isRefreshing).toBe(false));

    const refreshRequests = searchRequests().slice(sentBefore);
    expect(refreshRequests).toHaveLength(1);
    expect(offsetOf(refreshRequests[0])).toBe(0);
    expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE);
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
    __http.reset();
  });

  it('sends page 1 search_id on the page 2 request, not a fresh search', async () => {
    servePages(page(0, 'search-1'), page(SEARCH_PAGE_SIZE, 'search-1'));
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data?.search_id).toEqual(expect.any(String)));

    await act(async () => {
      await result.current.fetchNextPage();
    });

    await waitFor(() => expect(searchRequests()).toHaveLength(TWO_REQUESTS));
    expect(offsetOf(searchRequests()[1])).toBe(SEARCH_PAGE_SIZE);
    expect(paramsOf(searchRequests()[1]).get('search_id')).toBe('search-1');
    await waitFor(() =>
      expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE + SEARCH_PAGE_SIZE),
    );
  });

  it('sends no search_id on the first page', async () => {
    servePages(page(0, 'search-1'));
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data?.search_id).toEqual(expect.any(String)));

    expect(searchRequests()).toHaveLength(1);
    expect(paramsOf(searchRequests()[0]).has('search_id')).toBe(false);
  });
});

describe('held slate belongs to its query', () => {
  function pageFor(title: string, searchId: string): DiscoverySearchResponse {
    return {
      ...endlessPage(0),
      search_id: searchId,
      results: [resultFixture({ title })],
      has_more: false,
    };
  }

  async function startSlowRefresh() {
    __http.reset();
    servePages(pageFor('old-row', 'old-search'));
    const rendered = renderHook(({ q }: { q: string }) => useDiscoverSearch(q), {
      wrapper,
      initialProps: { q: 'old' },
    });
    await waitFor(() => expect(resultCount(rendered.result)).toBe(1));
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    __http.hang(SEARCH);
    act(() => {
      rendered.result.current.refetch();
    });
    await waitFor(() => expect(rendered.result.current.isRefreshing).toBe(true));
    return rendered;
  }

  it('shows the old rows only as marked previous results once a new query commits mid-refresh', async () => {
    const { result, rerender } = await startSlowRefresh();

    rerender({ q: 'new' });

    expect(result.current.isShowingPrevious).toBe(true);
    expect(result.current.hasNextPage).toBe(false);
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
    __http.reset();
  });

  it('flags a rejected page 2 while keeping page 1', async () => {
    servePages(page(0));
    __http.reply(SEARCH, { status: 502, json: { detail: 'bad gateway' } });
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data?.search_id).toEqual(expect.any(String)));
    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.isFetchNextPageError).toBe(true));
    expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE);
  });
});

describe('discover query failures emit a search_failed telemetry event tagged with its source', () => {
  function failureEvents() {
    return (__http.requests as { path: string; body: string }[])
      .filter((request) => request.path === EVENTS_PATH)
      .map(
        (request) =>
          JSON.parse(request.body) as { type: string; payload?: Record<string, unknown> },
      )
      .filter((event) => event.type === 'search_failed');
  }

  beforeEach(() => {
    __http.reset();
    __http.reply(SEARCH, { status: 503, json: { detail: 'unavailable' } });
    __http.reply(EVENTS, { status: 204 });
  });

  it('a failed search fires search_failed with source search and the HTTP status', async () => {
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]).toEqual({
      type: 'search_failed',
      payload: {
        source: 'search',
        status: 503,
        correlationId: expect.any(String),
        session_id: expect.any(String),
      },
    });
  });

  it("a failed search reports the request's correlation id, so the event matches the server log", async () => {
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    const failedRequest = searchRequests().at(-1);
    expect(failedRequest?.headers[CORRELATION_HEADER]).toEqual(expect.any(String));
    expect(failureEvents()[0]?.payload).toEqual({
      source: 'search',
      status: 503,
      correlationId: failedRequest?.headers[CORRELATION_HEADER],
      session_id: expect.any(String),
    });
  });
});

describe('a search backend that never answers reaches the error state quickly', () => {
  const HUNG_BACKEND_BUDGET_MS = 40_000;

  beforeEach(() => {
    jest.useFakeTimers();
    queryClient = new QueryClient({
      defaultOptions: { queries: { ...transientRetryOptions }, mutations: { retry: false } },
    });
    __http.reset();
    __http.hang(SEARCH);
  });

  afterEach(() => {
    queryClient.clear();
    jest.useRealTimers();
  });

  it('sets error once a single retry of a timed-out request has failed', async () => {
    const { result } = renderHook(() => useDiscoverSearch('ab'), { wrapper });

    await act(async () => {
      await jest.advanceTimersByTimeAsync(HUNG_BACKEND_BUDGET_MS);
    });

    expect(result.current.error).toBeInstanceOf(NetworkError);
    expect((result.current.error as NetworkError).failure).toBe('timeout');
    expect(searchRequests()).toHaveLength(TWO_REQUESTS);
  });
});

describe('useDiscoverSearch history saving', () => {
  function singlePage(): DiscoverySearchResponse {
    return {
      query: 'radiohead',
      query_norm: 'radiohead',
      search_id: 'search-1',
      results: [resultFixture()],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      offset: 0,
      has_more: false,
    };
  }

  beforeEach(() => {
    __http.reset();
  });

  it('never asks the search endpoint to save history, on load, refresh or retry', async () => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retryDelay: 0 }, mutations: { retry: false } },
    });
    __http.replyOnce(SEARCH, { status: 503, json: { detail: 'unavailable' } });
    __http.reply(SEARCH, { json: singlePage() });
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });
    await waitFor(() => expect(result.current.data?.search_id).toEqual(expect.any(String)));

    await act(async () => {
      await result.current.refetch();
    });

    expect(savedFlags()).toEqual([false, false, false]);
  });
});

describe('useDiscoverSearch retries only what the shared classifier calls transient', () => {
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retryDelay: 0 }, mutations: { retry: false } },
    });
    __http.reset();
  });

  it('does not re-send a search the server rejected with a 400', async () => {
    __http.reply(SEARCH, { status: 400, json: { detail: 'bad query' } });
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));

    expect(searchRequests()).toHaveLength(1);
  });

  it('retries a 503 once, then surfaces the error', async () => {
    __http.reply(SEARCH, { status: 503, json: { detail: 'unavailable' } });
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());

    expect(searchRequests()).toHaveLength(TWO_REQUESTS);
  });
});

describe('re-typing the submitted query does not re-send the search', () => {
  const DEBOUNCE_MS = 300;

  function useSearchScreen() {
    const search = useDebouncedSearch({ debounceMs: DEBOUNCE_MS });
    const results = useDiscoverSearch(search.committedQuery);
    return { search, results };
  }

  function page(): DiscoverySearchResponse {
    return { ...endlessPage(0), results: [resultFixture()], has_more: false };
  }

  async function flush(ms: number): Promise<void> {
    await act(async () => {
      await jest.advanceTimersByTimeAsync(ms);
    });
  }

  beforeEach(() => {
    jest.useFakeTimers();
    setSearchState('', '');
    __http.reset();
    __http.reply(SEARCH, { json: page() });
  });

  afterEach(() => {
    queryClient.clear();
    jest.useRealTimers();
  });

  it('sends one search when "ab" is submitted and then typed again', async () => {
    const { result } = renderHook(useSearchScreen, { wrapper });
    act(() => result.current.search.onChangeText('ab'));
    act(() => result.current.search.onSubmit());
    await flush(0);

    act(() => result.current.search.onChangeText('ab'));
    await flush(DEBOUNCE_MS);

    expect(savedFlags()).toEqual([false]);
    expect(result.current.results.isLoading).toBe(false);
  });
});
