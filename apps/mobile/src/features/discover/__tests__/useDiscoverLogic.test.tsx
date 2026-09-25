import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';

import {
  clearSearchHistory,
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
} from '@shared/api-client/discovery';
import { ApiError, NetworkError } from '@shared/errors';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { MAX_QUERY_LENGTH, MIN_QUERY_LENGTH, SEARCH_PAGE_SIZE } from '../searchLimits';
import { setSearchState } from '../search-state';
import { DiscoverBody } from '../ui/DiscoverBody';
import { resultFixture } from './fixtures';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn(),
  listSearchHistory: jest.fn(),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: mockRecord }),
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

const mockRecord = jest.fn();
const mockSearch = searchDiscovery as jest.Mock;
const mockSuggest = suggestDiscovery as jest.Mock;
const mockHistory = listSearchHistory as jest.Mock;
const mockClearHistory = clearSearchHistory as jest.Mock;

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

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  queryClient = new QueryClient({
    // A mutation carrying its own retry policy (clear-history) ignores the
    // default below, so the delay is pinned to keep its exhaustion instant.
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false, retryDelay: 0 },
    },
  });
  mockSearch.mockReset();
  mockSuggest.mockReset();
  mockHistory.mockReset();
  mockClearHistory.mockReset();
  // Restored input long enough to enable the suggest query without a committed search.
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
    mockSearch.mockResolvedValue(searchResponseFixture(atMinimum));
    mockSuggest.mockResolvedValue({
      suggestions: [{ text: 'radiohead', kind: 'artist', popularity: 1 }],
    });
    mockHistory.mockResolvedValue({ items: [] });
  });

  it('keeps the suggest query, the dropdown and the pending state closed below the minimum', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    act(() => {
      result.current.setIsFocused(true);
      result.current.onChangeText(belowMinimum);
    });

    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(mockSuggest).not.toHaveBeenCalled();
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
      expect(mockSuggest).toHaveBeenCalledWith({ q: atMinimum, limit: 5 }, expect.anything()),
    );
    await waitFor(() => expect(result.current.showSuggestions).toBe(true));
    await waitFor(() =>
      expect(mockSearch).toHaveBeenCalledWith(
        expect.objectContaining({ q: atMinimum }),
        expect.anything(),
      ),
    );
  });
});

describe('useDiscoverLogic leaves the suggestion and history lists empty when there is nothing to show', () => {
  it('falls back to empty lists when the suggest and history queries fail', async () => {
    mockSuggest.mockRejectedValue(new ApiError(500, 'suggest down'));
    mockHistory.mockRejectedValue(new ApiError(500, 'history down'));

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(mockSuggest).toHaveBeenCalled());
    await waitFor(() => expect(mockHistory).toHaveBeenCalled());
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.historyItems).toEqual([]);
  });

  it('exposes empty lists when the queries succeed with nothing to show', async () => {
    mockSuggest.mockResolvedValue({ suggestions: [] });
    mockHistory.mockResolvedValue({ items: [] });

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(mockSuggest).toHaveBeenCalled());
    await waitFor(() => expect(mockHistory).toHaveBeenCalled());
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.historyItems).toEqual([]);
  });
});

describe('useDiscoverLogic keeps the history list when clearing it fails', () => {
  it('restores the history items after a failed clear', async () => {
    mockSuggest.mockResolvedValue({ suggestions: [] });
    mockHistory.mockResolvedValue({ items: [{ query: 'radiohead' }] });
    mockClearHistory.mockRejectedValue(new ApiError(500, 'clear down'));

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(result.current.historyItems).toEqual([{ query: 'radiohead' }]));

    act(() => result.current.onClearHistory());

    await waitFor(() => expect(mockClearHistory).toHaveBeenCalled());
    await waitFor(() => expect(result.current.historyItems).toEqual([{ query: 'radiohead' }]));
  });
});

describe('useDiscoverLogic recovers from failures', () => {
  const mockClear = clearSearchHistory as jest.Mock;

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
    return <DiscoverBody {...logic} onFilterChange={logic.setFilter} />;
  }

  function clearFailureEvents() {
    return mockRecord.mock.calls
      .map((call) => call[0] as { payload?: { source?: string } })
      .filter((event) => event.payload?.source === 'clear_history');
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false, retryDelay: 0 },
      },
    });
    [mockSearch, mockSuggest, mockHistory, mockClear, mockRecord].forEach((mock) =>
      mock.mockReset(),
    );
    mockSuggest.mockResolvedValue({ suggestions: [] });
    setSearchState('', '');
  });

  afterEach(() => {
    queryClient.clear();
    setSearchState('', '');
  });

  it('renders the clear-history error and records a clear_history failure when a clear is rejected', async () => {
    mockHistory.mockResolvedValue({ items: [{ query: 'old', query_norm: 'old' }] });
    mockClear.mockRejectedValue(new ApiError(400, 'bad request'));
    render(<Probe />, { wrapper });

    fireEvent.press(await screen.findByLabelText('Clear search history'));

    expect(await screen.findByTestId('discover-clear-history-error')).toBeTruthy();
    expect(clearFailureEvents()).toHaveLength(1);
  });

  it('retries the next page through fetchNextPage after a failed page 2', async () => {
    setSearchState('radiohead', 'radiohead');
    mockHistory.mockResolvedValue({ items: [] });
    mockSearch
      .mockResolvedValueOnce(page(0))
      .mockRejectedValueOnce(new ApiError(502, 'bad gateway'))
      .mockResolvedValueOnce(page(SEARCH_PAGE_SIZE));
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(result.current.searchData).toBeDefined());
    await act(async () => result.current.onEndReached());
    await waitFor(() => expect(result.current.nextPageFailed).toBe(true));

    await act(async () => result.current.onRetryNextPage());

    await waitFor(() => expect(result.current.nextPageFailed).toBe(false));
    expect(mockSearch).toHaveBeenCalledTimes(3);
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
    mockSearch
      .mockReset()
      .mockImplementation(({ offset }: { offset: number }) => Promise.resolve(page(offset)));
    mockSuggest.mockReset().mockResolvedValue({ suggestions: [] });
    mockHistory.mockReset().mockResolvedValue({ items: [] });
  });

  afterEach(() => {
    queryClient.clear();
  });

  it('refetches history once per search, not once per landed page', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(result.current.searchData).toBeDefined());
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    const historyCalls = mockHistory.mock.calls.length;

    await act(async () => {
      result.current.onEndReached();
    });
    await waitFor(() => expect(mockSearch).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));

    expect(mockHistory).toHaveBeenCalledTimes(historyCalls);
  });
});

describe('useDiscoverLogic history tap', () => {
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    setSearchState('', '');
    mockSearch.mockReset().mockResolvedValue({
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
    (suggestDiscovery as jest.Mock).mockReset().mockResolvedValue({ suggestions: [] });
    (listSearchHistory as jest.Mock).mockReset().mockResolvedValue({ items: [] });
  });

  afterEach(() => {
    queryClient.clear();
  });

  it('is not pending after tapping a history item with trailing whitespace', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    act(() => {
      result.current.onHistoryTap({ query: 'ab ' } as never);
    });

    await waitFor(() => expect(result.current.committedQuery).toBe('ab'));
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

    await waitFor(() => expect(mockSearch).toHaveBeenCalled());
    for (const [args] of mockSearch.mock.calls) {
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
    mockSearch
      .mockReset()
      .mockImplementation(({ offset }: { offset: number }) => Promise.resolve(page(offset)));
    const hook = renderHook(() => useDiscoverLogic(), { wrapper });
    await waitFor(() => expect(hook.result.current.searchData).toBeDefined());
    await act(async () => hook.result.current.onEndReached());
    await waitFor(() =>
      expect(hook.result.current.searchData?.results).toHaveLength(SEARCH_PAGE_SIZE * 2),
    );
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    return hook;
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    (suggestDiscovery as jest.Mock).mockReset().mockResolvedValue({ suggestions: [] });
    (listSearchHistory as jest.Mock).mockReset().mockResolvedValue({ items: [] });
    setSearchState('radiohead', 'radiohead');
  });

  afterEach(() => {
    queryClient.clear();
    setSearchState('', '');
  });

  it('keeps loaded pages and flags refreshFailed when a refresh is rejected', async () => {
    const { result } = await loadTwoPages();
    mockSearch.mockRejectedValue(new NetworkError('transport', 'offline'));

    await act(async () => result.current.onRefresh());

    await waitFor(() => expect(result.current.refreshFailed).toBe(true));
    expect(result.current.searchData?.results).toHaveLength(SEARCH_PAGE_SIZE * 2);
    expect(result.current.view).toBe('results');
  });

  it('replaces the list with a fresh first page and clears the flag on a good refresh', async () => {
    const { result } = await loadTwoPages();
    mockSearch.mockRejectedValueOnce(new NetworkError('transport', 'offline'));
    await act(async () => result.current.onRefresh());
    await waitFor(() => expect(result.current.refreshFailed).toBe(true));
    mockSearch.mockImplementation(({ offset }: { offset: number }) =>
      Promise.resolve(page(offset, 'fresh')),
    );

    await act(async () => result.current.onRefresh());

    await waitFor(() => expect(result.current.refreshFailed).toBe(false));
    expect(result.current.searchData?.results).toHaveLength(SEARCH_PAGE_SIZE);
    expect(result.current.searchData?.results[0]?.title).toBe('fresh');
  });

  it('does not cancel a refresh when the list end is reached during it', async () => {
    const { result } = await loadTwoPages();
    let release: () => void = () => undefined;
    mockSearch.mockReset().mockImplementation(
      ({ offset }: { offset: number }) =>
        new Promise((resolve) => {
          release = () => resolve(page(offset, 'fresh'));
        }),
    );
    act(() => result.current.onRefresh());
    await waitFor(() => expect(result.current.isRefreshing).toBe(true));

    act(() => result.current.onEndReached());
    act(() => result.current.onRefresh());
    await act(async () => release());

    await waitFor(() => expect(result.current.isRefreshing).toBe(false));
    expect(mockSearch).toHaveBeenCalledTimes(1);
    expect(result.current.searchData?.results[0]?.title).toBe('fresh');
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
    mockSearch.mockReset().mockResolvedValue(page());
    (suggestDiscovery as jest.Mock).mockReset().mockResolvedValue({ suggestions: [] });
    (listSearchHistory as jest.Mock).mockReset().mockResolvedValue({ items: [] });
  });

  afterEach(() => {
    queryClient.clear();
  });

  async function submitRadiohead() {
    const hook = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => hook.result.current.onChangeText('radiohead'));
    act(() => hook.result.current.onSubmit());
    await waitFor(() => expect(mockSearch).toHaveBeenCalled());
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

  it('does not re-run the previous search with saveHistory', async () => {
    await submitRadiohead();
    mockSearch.mockClear();

    switchAccount();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });

    const saved = mockSearch.mock.calls.filter(
      ([params]) => (params as { saveHistory?: boolean } | undefined)?.saveHistory === true,
    );
    expect(saved).toHaveLength(0);
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

    expect(mockSearch).not.toHaveBeenCalled();
  });
});
