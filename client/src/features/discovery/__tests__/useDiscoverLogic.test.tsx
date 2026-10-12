import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';

import { ApiError, NetworkError } from '@shared/failure/errors';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { useDiscoverLogic } from '@features/discovery/hooks/useDiscoverLogic';
import {
  MAX_QUERY_LENGTH,
  MIN_QUERY_LENGTH,
  SEARCH_PAGE_SIZE,
} from '@features/discovery/domain/searchPaging';
import { setSearchState } from '@features/discovery/domain/searchState';
import { DiscoverBody } from '@features/discovery/ui/DiscoverBody';
import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { signInTestUser } from '@testing/signInTestUser';
import { DiscoverPortProvider } from '@features/discovery/ports/DiscoverPortProvider';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';

import type { DiscoverySearchResponse } from '@shared/apiClient/discovery';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

const { __http } = require('@testing/doubles/fetch.js');

const CLEAR_HISTORY = 'DELETE /v1/discovery/search-history';
const EVENTS = 'POST /v1/discovery/events';
const EVENTS_PATH = '/v1/discovery/events';
const BAD_REQUEST = 400;
const SERVER_ERROR = 500;
const TWO_SEARCHES = 2;
const FOUR_SEARCHES = 4;
const SHORT_WAIT_MS = 50;

type SearchParams = { q: string };

type LogicResult = { current: ReturnType<typeof useDiscoverLogic> };

interface PageRequest {
  offset: number;
}

const portSearch = jest.fn();
const portSuggest = jest.fn();
const portHistory = jest.fn();
const portRecordHistory = jest.fn();
const port: DiscoverPort = {
  searchDiscovery: portSearch,
  suggestDiscovery: portSuggest,
  listSearchHistory: portHistory,
  recordSearchHistory: portRecordHistory,
};

function searchResponseFixture(query: string): DiscoverySearchResponse {
  return {
    query,
    query_norm: query,
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

let queryClient: QueryClient;

interface WrapperProps {
  children: React.ReactNode;
}

function wrapper({ children }: WrapperProps) {
  return (
    <QueryClientProvider client={queryClient}>
      <DiscoverPortProvider port={port}>{children}</DiscoverPortProvider>
    </QueryClientProvider>
  );
}

function clearRequests(): number {
  return __http.countFor(CLEAR_HISTORY);
}

beforeEach(async () => {
  queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false, retryDelay: 0 },
    },
  });
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
  portSearch.mockReset();
  portSuggest.mockReset();
  portHistory.mockReset();
  portRecordHistory.mockReset().mockResolvedValue(undefined);
  setSearchState('', 'rad');
});

afterEach(() => {
  queryClient.clear();
  setSearchState('', '');
});

describe('useDiscoverLogic opens every query gate at the same minimum length', () => {
  const belowMinimum = 'a'.repeat(MIN_QUERY_LENGTH - 1);
  const atMinimum = 'a'.repeat(MIN_QUERY_LENGTH);

  beforeEach(() => {
    setSearchState('', '');
    portSearch.mockResolvedValue(searchResponseFixture(atMinimum));
    portSuggest.mockResolvedValue({
      suggestions: [{ text: 'radiohead', kind: 'artist', popularity: 1 }],
    });
    portHistory.mockResolvedValue({ items: [] });
  });

  it('keeps the suggest query, the dropdown and the pending state closed below the minimum', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    act(() => {
      result.current.setIsFocused(true);
      result.current.onChangeText(belowMinimum);
    });

    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(portSuggest).not.toHaveBeenCalled();
    expect(result.current.showSuggestions).toBe(false);
    expect(result.current.pending).toBe(false);
  });

  it('opens the pending state, the suggest query, the dropdown and the search at the minimum', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    act(() => {
      result.current.setIsFocused(true);
      result.current.onChangeText(atMinimum);
    });

    expect(result.current.pending).toBe(true);
    await waitFor(() =>
      expect(portSuggest).toHaveBeenCalledWith({ q: atMinimum, limit: 5 }, expect.anything()),
    );
    await waitFor(() => expect(result.current.showSuggestions).toBe(true));
    await waitFor(() =>
      expect(portSearch).toHaveBeenCalledWith(
        expect.objectContaining({ q: atMinimum }),
        expect.anything(),
      ),
    );
  });
});

describe('useDiscoverLogic leaves the suggestion and history lists empty when there is nothing to show', () => {
  it('falls back to empty lists when the suggest and history queries fail', async () => {
    portSuggest.mockRejectedValue(new ApiError(SERVER_ERROR, 'suggest down'));
    portHistory.mockRejectedValue(new ApiError(SERVER_ERROR, 'history down'));

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.setIsFocused(true));

    await waitFor(() => expect(portSuggest).toHaveBeenCalled());
    await waitFor(() => expect(portHistory).toHaveBeenCalled());
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.history.items).toEqual([]);
  });

  it('exposes empty lists when the queries succeed with nothing to show', async () => {
    portSuggest.mockResolvedValue({ suggestions: [] });
    portHistory.mockResolvedValue({ items: [] });

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.setIsFocused(true));

    await waitFor(() => expect(portSuggest).toHaveBeenCalled());
    await waitFor(() => expect(portHistory).toHaveBeenCalled());
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.showSuggestions).toBe(false);
    expect(result.current.history.items).toEqual([]);
  });
});

describe('useDiscoverLogic reports a failed history load', () => {
  it('exposes historyFailed when the history request rejects', async () => {
    portSuggest.mockResolvedValue({ suggestions: [] });
    portHistory.mockRejectedValue(new ApiError(SERVER_ERROR, 'history down'));

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(result.current.history.failed).toBe(true));
  });

  it('refetches history and recovers when retry succeeds', async () => {
    portSuggest.mockResolvedValue({ suggestions: [] });
    portHistory.mockRejectedValueOnce(new ApiError(SERVER_ERROR, 'history down'));
    portHistory.mockResolvedValue({ items: [{ query: 'radiohead' }] });

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(result.current.history.failed).toBe(true));

    act(() => result.current.history.onRetry());

    await waitFor(() => expect(result.current.history.failed).toBe(false));
    expect(result.current.history.items).toEqual([{ query: 'radiohead' }]);
  });

  it('does not flag a failure when history loads', async () => {
    portSuggest.mockResolvedValue({ suggestions: [] });
    portHistory.mockResolvedValue({ items: [] });

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(result.current.history.failed).toBe(false);
  });
});

describe('useDiscoverLogic keeps the history list when clearing it fails', () => {
  it('restores the history items after a failed clear', async () => {
    portSuggest.mockResolvedValue({ suggestions: [] });
    portHistory.mockResolvedValue({ items: [{ query: 'radiohead' }] });
    __http.reply(CLEAR_HISTORY, { status: BAD_REQUEST });

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(result.current.history.items).toEqual([{ query: 'radiohead' }]));

    act(() => result.current.history.onClear());

    await waitFor(() => expect(clearRequests()).toBe(1));
    await waitFor(() => expect(result.current.history.items).toEqual([{ query: 'radiohead' }]));
  });
});

describe('useDiscoverLogic recovers from failures', () => {
  function page(offset: number): DiscoverySearchResponse {
    return {
      query: 'radiohead',
      query_norm: 'radiohead',
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

  function Probe() {
    const logic = useDiscoverLogic();
    return (
      <DiscoverBody results={logic.results} pagination={logic.pagination} history={logic.history} />
    );
  }

  function clearFailureEvents() {
    return (__http.requests as { path: string; body: string }[])
      .filter((request) => request.path === EVENTS_PATH)
      .map((request) => JSON.parse(request.body) as { payload?: { source?: string } })
      .filter((event) => event.payload?.source === 'clear_history');
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false, retryDelay: 0 },
      },
    });
    [portSearch, portSuggest, portHistory].forEach((mock) => mock.mockReset());
    portSuggest.mockResolvedValue({ suggestions: [] });
    setSearchState('', '');
  });

  afterEach(() => {
    queryClient.clear();
    setSearchState('', '');
  });

  it('renders the clear-history error and records a clear_history failure when a clear is rejected', async () => {
    portHistory.mockResolvedValue({ items: [{ query: 'old', query_norm: 'old' }] });
    __http.reply(CLEAR_HISTORY, { status: BAD_REQUEST });
    render(<Probe />, { wrapper });

    fireEvent.press(await screen.findByLabelText('Clear search history'));

    expect(await screen.findByTestId('discover-clear-history-error')).toBeTruthy();
    expect(clearFailureEvents()).toHaveLength(1);
    expect(clearRequests()).toBe(1);
  });

  it('retries the next page through fetchNextPage after a failed page 2', async () => {
    setSearchState('radiohead', 'radiohead');
    portHistory.mockResolvedValue({ items: [] });
    portSearch
      .mockResolvedValueOnce(page(0))
      .mockRejectedValueOnce(new ApiError(502, 'bad gateway'))
      .mockRejectedValueOnce(new ApiError(502, 'bad gateway'))
      .mockResolvedValueOnce(page(SEARCH_PAGE_SIZE));
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(result.current.results.searchData).toBeDefined());
    await act(async () => result.current.pagination.onEndReached());
    await waitFor(() => expect(result.current.pagination.nextPageFailed).toBe(true), {
      timeout: 4000,
    });

    await act(async () => result.current.pagination.onRetryNextPage());

    await waitFor(() => expect(result.current.pagination.nextPageFailed).toBe(false));
    expect(portSearch).toHaveBeenCalledTimes(FOUR_SEARCHES);
  });
});

describe('useDiscoverLogic history invalidation', () => {
  function page(offset: number): DiscoverySearchResponse {
    return {
      query: 'radiohead',
      query_norm: 'radiohead',
      search_id: 'search-1',
      results: [],
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
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    setSearchState('radiohead', 'radiohead');
    portSearch
      .mockReset()
      .mockImplementation(({ offset }: PageRequest) => Promise.resolve(page(offset)));
    portSuggest.mockReset().mockResolvedValue({ suggestions: [] });
    portHistory.mockReset().mockResolvedValue({ items: [] });
  });

  afterEach(() => {
    queryClient.clear();
  });

  it('loading another page does not refetch history', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(result.current.results.searchData).toBeDefined());
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    const historyCalls = portHistory.mock.calls.length;

    await act(async () => {
      result.current.pagination.onEndReached();
    });
    await waitFor(() => expect(portSearch).toHaveBeenCalledTimes(TWO_SEARCHES));
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));

    expect(portHistory).toHaveBeenCalledTimes(historyCalls);
  });
});

describe('useDiscoverLogic history tap', () => {
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    setSearchState('', '');
    portSearch.mockReset().mockResolvedValue({
      query: 'ab',
      query_norm: 'ab',
      search_id: 's1',
      results: [],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 0,
      offset: 0,
      has_more: false,
    });
    portSuggest.mockReset().mockResolvedValue({ suggestions: [] });
    portHistory.mockReset().mockResolvedValue({ items: [] });
  });

  afterEach(() => {
    queryClient.clear();
  });

  it('is not pending after tapping a history item with trailing whitespace', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    act(() => {
      result.current.history.onTap({ query: 'ab ' } as never);
    });

    expect(result.current.pending).toBe(false);
  });

  it('never sends a q longer than MAX_QUERY_LENGTH', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    act(() => {
      result.current.onChangeText('a'.repeat(5000));
    });
    act(() => {
      result.current.onSubmit();
    });

    await waitFor(() => expect(portSearch).toHaveBeenCalled());
    for (const [args] of portSearch.mock.calls) {
      expect((args as { q: string }).q.length).toBeLessThanOrEqual(MAX_QUERY_LENGTH);
    }
  });
});

describe('useDiscoverLogic refresh failure', () => {
  function page(offset: number, title = 'old'): DiscoverySearchResponse {
    return {
      query: 'radiohead',
      query_norm: 'radiohead',
      search_id: 's',
      results: Array.from({ length: SEARCH_PAGE_SIZE }, () => resultFixture({ title })),
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 100,
      offset,
      has_more: true,
    };
  }

  async function loadTwoPages() {
    portSearch
      .mockReset()
      .mockImplementation(({ offset }: PageRequest) => Promise.resolve(page(offset)));
    const hook = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(hook.result.current.results.searchData).toBeDefined());
    await act(async () => hook.result.current.pagination.onEndReached());
    await waitFor(() =>
      expect(hook.result.current.results.searchData?.results).toHaveLength(SEARCH_PAGE_SIZE * 2),
    );
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    return hook;
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    portSuggest.mockReset().mockResolvedValue({ suggestions: [] });
    portHistory.mockReset().mockResolvedValue({ items: [] });
    setSearchState('radiohead', 'radiohead');
  });

  afterEach(() => {
    queryClient.clear();
    setSearchState('', '');
  });

  it('keeps loaded pages and flags refreshFailed when a refresh is rejected', async () => {
    const { result } = await loadTwoPages();
    portSearch.mockRejectedValue(new NetworkError('transport', 'offline'));

    await act(async () => result.current.results.onRefresh());

    await waitFor(() => expect(result.current.results.refreshFailed).toBe(true));
    expect(result.current.results.searchData?.results).toHaveLength(SEARCH_PAGE_SIZE * 2);
    expect(result.current.results.view).toBe('results');
  });

  it('replaces the list with a fresh first page and clears the flag on a good refresh', async () => {
    const { result } = await loadTwoPages();
    portSearch
      .mockRejectedValueOnce(new NetworkError('transport', 'offline'))
      .mockRejectedValueOnce(new NetworkError('transport', 'offline'));
    await act(async () => result.current.results.onRefresh());
    await waitFor(() => expect(result.current.results.refreshFailed).toBe(true), { timeout: 4000 });
    portSearch.mockImplementation(({ offset }: PageRequest) =>
      Promise.resolve(page(offset, 'fresh')),
    );

    await act(async () => result.current.results.onRefresh());

    await waitFor(() => expect(result.current.results.refreshFailed).toBe(false));
    expect(result.current.results.searchData?.results).toHaveLength(SEARCH_PAGE_SIZE);
    expect(result.current.results.searchData?.results[0]?.title).toBe('fresh');
  });

  it('does not cancel a refresh when the list end is reached during it', async () => {
    const { result } = await loadTwoPages();
    let release: () => void = () => undefined;
    portSearch.mockReset().mockImplementation(
      ({ offset }: PageRequest) =>
        new Promise((resolve) => {
          release = () => resolve(page(offset, 'fresh'));
        }),
    );
    act(() => result.current.results.onRefresh());
    await waitFor(() => expect(result.current.results.isRefreshing).toBe(true));

    act(() => result.current.pagination.onEndReached());
    act(() => result.current.results.onRefresh());
    await act(async () => release());

    await waitFor(() => expect(result.current.results.isRefreshing).toBe(false));
    expect(portSearch).toHaveBeenCalledTimes(1);
    expect(result.current.results.searchData?.results[0]?.title).toBe('fresh');
  });
});

describe('a direct account switch with Discover still mounted', () => {
  function page() {
    return {
      query: 'radiohead',
      query_norm: 'radiohead',
      search_id: 's1',
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
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    setSearchState('', '');
    portSearch.mockReset().mockResolvedValue(page());
    portSuggest.mockReset().mockResolvedValue({ suggestions: [] });
    portHistory.mockReset().mockResolvedValue({ items: [] });
  });

  afterEach(() => {
    queryClient.clear();
  });

  async function submitRadiohead() {
    const hook = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => hook.result.current.onChangeText('radiohead'));
    act(() => hook.result.current.onSubmit());
    await waitFor(() => expect(portSearch).toHaveBeenCalled());
    return hook;
  }

  function switchAccount() {
    act(() => {
      runSignOutCleanups();
      queryClient.clear();
    });
  }

  it('clears the input', async () => {
    const { result } = await submitRadiohead();

    switchAccount();

    expect(result.current.inputValue).toBe('');
  });

  it('does not save the previous query to history again', async () => {
    await submitRadiohead();
    await waitFor(() => expect(portRecordHistory).toHaveBeenCalledTimes(1));
    portRecordHistory.mockClear();

    switchAccount();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });

    expect(portRecordHistory).not.toHaveBeenCalled();
  });

  it('drops a pending debounced commit', () => {
    jest.useFakeTimers();
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));

    switchAccount();
    act(() => {
      jest.advanceTimersByTime(5000);
    });
    jest.useRealTimers();

    expect(portSearch).not.toHaveBeenCalled();
  });
});

const DEBOUNCE_SETTLED_MS = 400;

function submitPage(query: string): DiscoverySearchResponse {
  return {
    query,
    query_norm: query,
    search_id: `search-${query}`,
    results: [resultFixture({ title: query })],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 1,
    offset: 0,
    has_more: false,
  };
}

async function settle(ms: number = DEBOUNCE_SETTLED_MS) {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
}
function searchedQueries(): string[] {
  return portSearch.mock.calls.map(([params]) => (params as { q: string }).q);
}

describe('useDiscoverLogic submit and history', () => {
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    setSearchState('', '');
    portSearch
      .mockReset()
      .mockImplementation(({ q }: SearchParams) => Promise.resolve(submitPage(q)));
    portSuggest.mockReset().mockResolvedValue({ suggestions: [] });
    portHistory.mockReset().mockResolvedValue({ items: [] });
    portRecordHistory.mockReset().mockResolvedValue(undefined);
  });

  it('pressing Enter after the debounced search does not search again', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));
    await waitFor(() => expect(result.current.results.searchData).toBeDefined());

    act(() => result.current.onSubmit());
    await settle();

    expect(searchedQueries()).toEqual(['radiohead']);
    expect(portRecordHistory).toHaveBeenCalledTimes(1);
    expect(portRecordHistory).toHaveBeenCalledWith('radiohead');
  });

  it('pressing Enter before the debounce fires searches once and saves once', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));
    act(() => result.current.onSubmit());
    await settle();

    expect(searchedQueries()).toEqual(['radiohead']);
    expect(portRecordHistory).toHaveBeenCalledTimes(1);
  });

  it('pressing Enter twice on the same query saves it once', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));
    act(() => result.current.onSubmit());
    await settle();
    act(() => result.current.onSubmit());
    await settle();

    expect(portRecordHistory).toHaveBeenCalledTimes(1);
  });

  it('a query searched again after another one is saved again', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    for (const q of ['radiohead', 'portishead', 'radiohead']) {
      act(() => result.current.onChangeText(q));
      act(() => result.current.onSubmit());
      await settle();
    }

    expect(portRecordHistory.mock.calls.map(([q]) => q)).toEqual([
      'radiohead',
      'portishead',
      'radiohead',
    ]);
  });

  it('picking a suggestion searches once and saves once', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onSuggestionSelect('radiohead'));
    await settle();

    expect(searchedQueries()).toEqual(['radiohead']);
    expect(portRecordHistory).toHaveBeenCalledWith('radiohead');
  });

  it('typing without submitting never saves history', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));
    await waitFor(() => expect(result.current.results.searchData).toBeDefined());
    await settle();

    expect(portRecordHistory).not.toHaveBeenCalled();
  });

  it('never asks the search endpoint to save history', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));
    act(() => result.current.onSubmit());
    await settle();

    for (const [params] of portSearch.mock.calls) {
      expect((params as { saveHistory?: boolean }).saveHistory).toBe(false);
    }
  });

  it('a failed history save leaves the results on screen', async () => {
    portRecordHistory.mockRejectedValue(new ApiError(SERVER_ERROR, 'history down'));
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));
    act(() => result.current.onSubmit());
    await settle();

    expect(result.current.results.searchData?.results).toHaveLength(1);
    expect(result.current.results.searchError).toBeNull();
  });

  it('refreshes the history list after a save, not after each search', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(portHistory).toHaveBeenCalled());
    portHistory.mockClear();

    act(() => result.current.onChangeText('radiohead'));
    await waitFor(() => expect(result.current.results.searchData).toBeDefined());
    await settle();
    expect(portHistory).not.toHaveBeenCalled();

    act(() => result.current.onSubmit());
    await waitFor(() => expect(portHistory).toHaveBeenCalledTimes(1));
  });
});

describe('useDiscoverLogic keeps the previous results while the next search loads', () => {
  type Deferred = { resolve: (page: DiscoverySearchResponse) => void; reject: (e: Error) => void };
  const pending = new Map<string, Deferred>();

  function holdSearches() {
    portSearch.mockImplementation(
      ({ q }: SearchParams) =>
        new Promise<DiscoverySearchResponse>((resolve, reject) => {
          pending.set(q, { resolve, reject });
        }),
    );
  }

  async function answer(q: string, response: DiscoverySearchResponse | Error) {
    await waitFor(() => expect(pending.has(q)).toBe(true));
    const deferred = pending.get(q)!;
    pending.delete(q);
    await act(async () => {
      if (response instanceof Error) deferred.reject(response);
      else deferred.resolve(response);
    });
  }

  function emptyPage(query: string): DiscoverySearchResponse {
    return { ...submitPage(query), results: [], total: 0 };
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, retryDelay: 0 }, mutations: { retry: false } },
    });
    setSearchState('', '');
    portSearch
      .mockReset()
      .mockImplementation(({ q }: SearchParams) => Promise.resolve(submitPage(q)));
    portSuggest.mockReset().mockResolvedValue({ suggestions: [] });
    portHistory.mockReset().mockResolvedValue({ items: [] });
    portRecordHistory.mockReset().mockResolvedValue(undefined);
    pending.clear();
  });

  async function showRadiohead() {
    holdSearches();
    const hook = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => hook.result.current.onChangeText('radiohead'));
    await answer('radiohead', { ...submitPage('radiohead'), has_more: true, total: 40 });
    await waitFor(() => expect(hook.result.current.results.view).toBe('results'));
    return hook;
  }

  function shownTitles(result: LogicResult) {
    return result.current.results.searchData?.results.map((r) => r.title);
  }

  it('the first search of a session still shows the loading state', async () => {
    holdSearches();
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));
    await waitFor(() => expect(pending.has('radiohead')).toBe(true));

    expect(result.current.results.view).toBe('loading');
  });

  it('a new query keeps the old results on screen and marks them as updating', async () => {
    const { result } = await showRadiohead();

    act(() => result.current.onChangeText('portishead'));
    await waitFor(() => expect(pending.has('portishead')).toBe(true));

    expect(result.current.results.view).toBe('results');
    expect(shownTitles(result)).toEqual(['radiohead']);
    expect(result.current.pending).toBe(true);
  });

  it('the new results replace the old ones when they arrive', async () => {
    const { result } = await showRadiohead();
    act(() => result.current.onChangeText('portishead'));

    await answer('portishead', submitPage('portishead'));

    await waitFor(() => expect(shownTitles(result)).toEqual(['portishead']));
    expect(result.current.pending).toBe(false);
  });

  it('a failed new search shows the error, not the old results', async () => {
    const { result } = await showRadiohead();
    act(() => result.current.onChangeText('portishead'));

    await answer('portishead', new ApiError(SERVER_ERROR, 'search down'));
    await answer('portishead', new ApiError(SERVER_ERROR, 'search down on retry'));

    await waitFor(() => expect(result.current.results.view).toBe('full-error'));
  });

  it('a new search with no matches shows zero results, not the old results', async () => {
    const { result } = await showRadiohead();
    act(() => result.current.onChangeText('zzqx'));

    await answer('zzqx', emptyPage('zzqx'));

    await waitFor(() => expect(result.current.results.view).toBe('zero-results'));
  });

  it('clearing the box removes the old results', async () => {
    const { result } = await showRadiohead();

    act(() => result.current.onClear());

    expect(result.current.results.view).toBe('empty-no-query');
    expect(result.current.results.searchData).toBeUndefined();
  });

  it('scrolling to the end of the old results does not load more of them', async () => {
    const { result } = await showRadiohead();
    act(() => result.current.onChangeText('portishead'));
    await waitFor(() => expect(pending.has('portishead')).toBe(true));
    portSearch.mockClear();

    act(() => result.current.pagination.onEndReached());
    await settle(SHORT_WAIT_MS);

    expect(portSearch).not.toHaveBeenCalled();
  });
});

describe('useDiscoverLogic asks for suggestions only while they can be shown', () => {
  const SUGGEST_SETTLED_MS = 250;
  function suggestedQueries(): string[] {
    return portSuggest.mock.calls.map(([p]) => p.q);
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    setSearchState('', '');
    portSearch
      .mockReset()
      .mockImplementation(({ q }: SearchParams) => Promise.resolve(submitPage(q)));
    portSuggest.mockReset().mockResolvedValue({
      suggestions: [{ text: 'radiohead', kind: 'artist', popularity: 1 }],
    });
    portHistory.mockReset().mockResolvedValue({ items: [] });
    portRecordHistory.mockReset().mockResolvedValue(undefined);
  });

  it('typing in the focused box fetches and shows suggestions', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.setIsFocused(true));
    act(() => result.current.onChangeText('radio'));

    await waitFor(() => expect(result.current.showSuggestions).toBe(true));
    expect(suggestedQueries()).toEqual(['radio']);
  });

  it('text set while the box is not focused does not fetch suggestions', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radio'));
    await settle(SUGGEST_SETTLED_MS);

    expect(suggestedQueries()).toEqual([]);
  });

  it('pressing Enter before suggestions settle does not fetch them', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.setIsFocused(true));
    act(() => result.current.onChangeText('radio'));
    act(() => result.current.onSubmit());
    await settle(SUGGEST_SETTLED_MS);

    expect(suggestedQueries()).toEqual([]);
  });

  it('picking a suggestion does not fetch suggestions for the picked text', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.setIsFocused(true));
    act(() => result.current.onChangeText('radio'));
    await waitFor(() => expect(result.current.showSuggestions).toBe(true));

    act(() => result.current.onSuggestionSelect('radiohead'));
    await settle(SUGGEST_SETTLED_MS);

    expect(suggestedQueries()).toEqual(['radio']);
  });

  it('tapping a history item does not fetch suggestions', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.setIsFocused(true));
    act(() => result.current.history.onTap({ query: 'radiohead' } as never));
    await settle(SUGGEST_SETTLED_MS);

    expect(suggestedQueries()).toEqual([]);
  });

  it('typing again after a submit fetches suggestions again', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.setIsFocused(true));
    act(() => result.current.onChangeText('radio'));
    act(() => result.current.onSubmit());
    await settle(SUGGEST_SETTLED_MS);

    act(() => result.current.onChangeText('radioh'));

    await waitFor(() => expect(suggestedQueries()).toEqual(['radioh']));
  });
});
