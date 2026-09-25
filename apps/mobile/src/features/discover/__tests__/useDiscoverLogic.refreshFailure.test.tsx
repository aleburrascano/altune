import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import {
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';
import { NetworkError } from '@shared/errors';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { SEARCH_PAGE_SIZE } from '../searchLimits';
import { setSearchState } from '../search-state';
import { resultFixture } from './fixtures';

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn(),
  listSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: jest.fn() }),
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

const mockSearch = searchDiscovery as jest.Mock;
let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

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

describe('useDiscoverLogic refresh failure', () => {
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
