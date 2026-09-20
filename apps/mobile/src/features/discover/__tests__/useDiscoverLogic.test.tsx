import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import {
  clearSearchHistory,
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
} from '@shared/api-client/discovery';
import { ApiError } from '@shared/errors';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { MIN_QUERY_LENGTH } from '../hooks/useDiscoverSearch';
import { setSearchState } from '../search-state';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn(),
  listSearchHistory: jest.fn(),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: jest.fn() }),
}));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

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
