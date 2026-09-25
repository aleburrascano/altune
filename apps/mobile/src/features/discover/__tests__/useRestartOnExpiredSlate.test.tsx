import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { searchDiscovery, type DiscoverySearchResponse } from '@shared/api-client/discovery';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { resultFixture } from './fixtures';

jest.mock('@shared/api-client/discovery', () => ({ searchDiscovery: jest.fn() }));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockSearch = searchDiscovery as jest.Mock;
let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function pageOf(offset: number, searchId: string, titles: string[]): DiscoverySearchResponse {
  return {
    query: 'q',
    query_norm: 'q',
    search_id: searchId,
    results: titles.map((title) => resultFixture({ title })),
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 9,
    offset,
    has_more: true,
  };
}

beforeEach(() => {
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockSearch.mockReset();
});

afterEach(() => queryClient.clear());

describe('held-slate expiry', () => {
  it('restarts from page 1 instead of stitching a fresh ranking onto old pages', async () => {
    mockSearch
      .mockResolvedValueOnce(pageOf(0, 'search-1', ['A1', 'A2', 'A3']))
      .mockResolvedValueOnce(pageOf(3, 'search-2', ['B1', 'B2', 'B3']))
      .mockResolvedValueOnce(pageOf(0, 'search-3', ['C1', 'C2', 'C3']));
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(async () => {
      await result.current.fetchNextPage();
    });

    await waitFor(() => expect(mockSearch).toHaveBeenCalledTimes(3));
    await waitFor(() =>
      expect(result.current.data?.results.map((r) => r.title)).toEqual(['C1', 'C2', 'C3']),
    );

    const titles = result.current.data?.results.map((r) => r.title) ?? [];
    expect(new Set(titles).size).toBe(titles.length);
    expect(titles).not.toEqual(expect.arrayContaining(['A1', 'B1']));
    expect(mockSearch.mock.calls[2]?.[0]).not.toHaveProperty('searchId');
  });
});

describe('held-slate expiry when the restart fails', () => {
  it('restarts once and keeps showing only the pages from the original search', async () => {
    mockSearch
      .mockResolvedValueOnce(pageOf(0, 'search-1', ['A1', 'A2', 'A3']))
      .mockResolvedValueOnce(pageOf(3, 'search-2', ['B1', 'B2', 'B3']))
      .mockRejectedValue(new Error('search unavailable'));
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.refreshFailed).toBe(true));
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200));
    });

    expect(mockSearch).toHaveBeenCalledTimes(3);
    expect(result.current.data?.results.map((r) => r.title)).toEqual(['A1', 'A2', 'A3']);
  });
});
