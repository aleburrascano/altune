import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import {
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { setSearchState } from '../search-state';

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

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

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

describe('useDiscoverLogic history invalidation', () => {
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
