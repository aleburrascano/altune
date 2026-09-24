import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { searchDiscovery, type DiscoverySearchResponse } from '@shared/api-client/discovery';
import { SEARCH_PAGE_SIZE, useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { ResultsList, type ResultsCommonProps } from '../ui/ResultsList';
import { DiscoverBody } from '../ui/DiscoverBody';
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
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockSearch.mockReset();
});

afterEach(() => queryClient.clear());

const common = (over: Partial<ResultsCommonProps>): ResultsCommonProps => ({
  onResultTap: jest.fn(),
  impression: {
    viewabilityConfig: { itemVisiblePercentThreshold: 50 },
    onViewableItemsChanged: jest.fn(),
  },
  onRefresh: jest.fn(),
  isRefreshing: false,
  onEndReached: jest.fn(),
  isFetchingNextPage: false,
  correction: null,
  onSearchOriginal: jest.fn(),
  ...over,
});

describe('next page failure', () => {
  it('flags a rejected page 2 while keeping page 1', async () => {
    mockSearch
      .mockResolvedValueOnce(page(0))
      .mockRejectedValueOnce(new ApiError(502, 'bad gateway'));
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());
    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.isFetchNextPageError).toBe(true));
    expect(result.current.data?.results).toHaveLength(SEARCH_PAGE_SIZE);
  });

  it('shows a retry footer that calls onRetryNextPage', () => {
    const onRetryNextPage = jest.fn();
    render(
      <ResultsList
        data={[]}
        keyExtractor={(_, i) => String(i)}
        renderItem={() => null}
        common={common({ nextPageFailed: true, onRetryNextPage })}
      />,
    );
    fireEvent.press(screen.getByTestId('discover-load-more-error'));
    expect(onRetryNextPage).toHaveBeenCalledTimes(1);
  });

  it('shows no retry footer when the next page did not fail', () => {
    render(
      <ResultsList
        data={[]}
        keyExtractor={(_, i) => String(i)}
        renderItem={() => null}
        common={common({})}
      />,
    );
    expect(screen.queryByTestId('discover-load-more-error')).toBeNull();
  });
});

describe('clear history failure', () => {
  function renderEmpty(clearHistoryFailed: boolean) {
    render(
      <DiscoverBody
        view="empty-no-query"
        searchData={undefined}
        historyItems={[{ query: 'old', query_norm: 'old' } as never]}
        filter="all"
        onFilterChange={jest.fn()}
        onHistoryTap={jest.fn()}
        onResultTap={jest.fn()}
        impression={{
          viewabilityConfig: { itemVisiblePercentThreshold: 50 },
          onViewableItemsChanged: jest.fn(),
        }}
        onRetry={jest.fn()}
        onEndReached={jest.fn()}
        isFetchingNextPage={false}
        onRefresh={jest.fn()}
        isRefreshing={false}
        correction={null}
        onSearchOriginal={jest.fn()}
        onClearHistory={jest.fn()}
        clearHistoryFailed={clearHistoryFailed}
      />,
    );
  }

  it('shows a message when the clear failed', () => {
    renderEmpty(true);
    expect(screen.getByTestId('discover-clear-history-error')).toBeTruthy();
  });

  it('shows no message otherwise', () => {
    renderEmpty(false);
    expect(screen.queryByTestId('discover-clear-history-error')).toBeNull();
  });
});
