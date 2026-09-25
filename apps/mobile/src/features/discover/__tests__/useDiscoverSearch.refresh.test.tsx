import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { searchDiscovery, type DiscoverySearchResponse } from '@shared/api-client/discovery';
import { SEARCH_PAGE_SIZE } from '../searchLimits';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { resultFixture } from './fixtures';

jest.mock('@shared/api-client/discovery', () => ({ searchDiscovery: jest.fn() }));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockSearch = searchDiscovery as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

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

describe('useDiscoverSearch refresh', () => {
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
