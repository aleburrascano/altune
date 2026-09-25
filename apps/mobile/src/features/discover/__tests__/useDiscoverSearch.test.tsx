import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { searchDiscovery, type DiscoverySearchResponse } from '@shared/api-client/discovery';
import { MAX_SEARCH_PAGES, SEARCH_PAGE_SIZE } from '../searchLimits';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { resultFixture } from './fixtures';

jest.mock('@shared/api-client/discovery', () => ({ searchDiscovery: jest.fn() }));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockSearch = searchDiscovery as jest.Mock;

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

// fetchNextPage resolves as soon as the page is cached, a tick before the hook
// re-renders with it, so each round waits for the merged list itself to grow.
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
  mockSearch
    .mockReset()
    .mockImplementation(({ offset }: { offset: number }) => Promise.resolve(endlessPage(offset)));
});

afterEach(() => {
  queryClient.clear();
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

  it('stops fetching at the page cap however long the server says results run', async () => {
    const { result } = await renderWithFirstPage();

    await fetchWhileOffered(result, MAX_SEARCH_PAGES + 1);

    expect(result.current.hasNextPage).toBe(false);
    expect(mockSearch).toHaveBeenCalledTimes(MAX_SEARCH_PAGES);
    expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE * MAX_SEARCH_PAGES);
  });

  it('keeps the oldest page at the cap rather than dropping it', async () => {
    const { result } = await renderWithFirstPage();

    await fetchWhileOffered(result, MAX_SEARCH_PAGES + 1);

    expect(result.current.data?.results[0]?.title).toBe('result-0');
    expect(result.current.data?.offset).toBe(0);
  });
});
