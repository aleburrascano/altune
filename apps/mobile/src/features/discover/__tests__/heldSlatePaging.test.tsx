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

describe('held-slate paging', () => {
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
