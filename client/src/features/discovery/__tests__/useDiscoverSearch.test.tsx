import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { NetworkError } from '@shared/api-client';
import { CORRELATION_HEADER } from '@shared/api-client/correlationId';
import { supabase } from '@shared/auth/supabaseClient';
import { transientRetryOptions } from '@shared/query/retryDelay';
import type { DiscoverySearchResponse } from '@shared/api-client/discovery';
import { recordEvent } from '@shared/telemetry/recordEvent';
import {
  MAX_SEARCH_OFFSET,
  MAX_SEARCH_PAGES,
  SEARCH_PAGE_SIZE,
} from '@features/discovery/domain/searchPaging';
import { setSearchState } from '@features/discovery/domain/searchState';
import { useDebouncedSearch } from '@features/discovery/hooks/useDebouncedSearch';
import { useDiscoverSearch } from '@features/discovery/hooks/useDiscoverSearch';
import { resultFixture } from '@features/discovery/__tests__/fixtures';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockRecordEvent = recordEvent as jest.Mock;

const SEARCH = 'GET /v1/discovery/search';
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
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
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
    __http.reset();
    __http.reply(SEARCH, { json: singlePage() });
  });

  it('still sends a request with saveHistory true', async () => {
    const { result, rerender } = renderHook(
      ({ saveHistory }: { saveHistory: boolean }) => useDiscoverSearch('radiohead', saveHistory),
      { wrapper, initialProps: { saveHistory: false } },
    );
    await waitFor(() => expect(result.current.data).toBeDefined());

    rerender({ saveHistory: true });

    await waitFor(() => expect(savedFlags()).toEqual([false, true]));
    expect(searchRequests().map((request) => paramsOf(request).get('q'))).toEqual([
      'radiohead',
      'radiohead',
    ]);
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
    await waitFor(() => expect(result.current.data).toBeDefined());

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
    await waitFor(() => expect(result.current.data).toBeDefined());

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

  it('shows none of the old rows and is loading once a new query commits mid-refresh', async () => {
    const { result, rerender } = await startSlowRefresh();

    rerender({ q: 'new' });

    expect(result.current.data).toBeUndefined();
    expect(result.current.isLoading).toBe(true);
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
    __http.reset();
    __http.reply(SEARCH, { status: 503, json: { detail: 'unavailable' } });
    mockRecordEvent.mockReset().mockResolvedValue(undefined);
  });

  it('a failed search fires search_failed with source search and the HTTP status', async () => {
    const { result } = renderHook(() => useDiscoverSearch('radiohead'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]).toEqual({
      type: 'search_failed',
      payload: { source: 'search', status: 503, correlationId: expect.any(String) },
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

  it('saves history on the first request of an explicit submit only, not on a refresh', async () => {
    __http.reply(SEARCH, { json: singlePage() });
    const { result } = renderHook(() => useDiscoverSearch('radiohead', true), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(async () => {
      await result.current.refetch();
    });

    expect(savedFlags()).toEqual([true, false]);
  });

  it('resends saveHistory on the retry of an explicit search whose first attempt failed', async () => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retryDelay: 0 }, mutations: { retry: false } },
    });
    __http.replyOnce(SEARCH, { status: 503, json: { detail: 'unavailable' } });
    __http.reply(SEARCH, { json: singlePage() });
    const { result } = renderHook(() => useDiscoverSearch('radiohead', true), { wrapper });

    await waitFor(() => expect(result.current.data).toBeDefined());

    expect(savedFlags()).toEqual([true, true]);
  });

  it('resends saveHistory when a cancelled first page of an explicit search runs again', async () => {
    __http.hang(SEARCH);
    const { result } = renderHook(() => useDiscoverSearch('radiohead', true), { wrapper });
    await waitFor(() => expect(searchRequests()).toHaveLength(1));

    await act(async () => {
      await queryClient.cancelQueries();
    });
    __http.reset();
    __http.reply(SEARCH, { json: singlePage() });
    await act(async () => {
      await queryClient.refetchQueries();
    });

    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(savedFlags()).toEqual([true]);
  });

  it('saves once after a successful first page: a later refetch does not resave', async () => {
    __http.reply(SEARCH, { json: singlePage() });
    const { result } = renderHook(() => useDiscoverSearch('radiohead', true), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(async () => {
      await queryClient.refetchQueries();
    });

    expect(savedFlags()).toEqual([true, false]);
  });

  it('never saves history for a non-explicit search', async () => {
    __http.reply(SEARCH, { json: singlePage() });
    const { result } = renderHook(() => useDiscoverSearch('radiohead', false), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    expect(savedFlags()).toEqual([false]);
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
    const results = useDiscoverSearch(search.committedQuery, search.isExplicitSubmit);
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

    expect(savedFlags()).toEqual([true]);
    expect(result.current.results.isLoading).toBe(false);
  });

  it('still saves history for an explicit submit of a changed query', async () => {
    const { result } = renderHook(useSearchScreen, { wrapper });
    act(() => result.current.search.setQuery('ab'));
    await flush(0);

    act(() => result.current.search.onChangeText('abc'));
    act(() => result.current.search.onSubmit());
    await flush(DEBOUNCE_MS);

    expect(savedFlags()).toEqual([true, true]);
  });
});
