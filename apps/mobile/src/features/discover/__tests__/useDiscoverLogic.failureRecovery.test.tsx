import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';

import {
  clearSearchHistory,
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';
import { ApiError } from '@shared/errors';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { SEARCH_PAGE_SIZE } from '../searchLimits';
import { setSearchState } from '../search-state';
import { DiscoverBody } from '../ui/DiscoverBody';
import { resultFixture } from './fixtures';

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn(),
  listSearchHistory: jest.fn(),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: mockRecord }),
}));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

const mockRecord = jest.fn();
const mockSearch = searchDiscovery as jest.Mock;
const mockSuggest = suggestDiscovery as jest.Mock;
const mockHistory = listSearchHistory as jest.Mock;
const mockClear = clearSearchHistory as jest.Mock;
let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

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
  [mockSearch, mockSuggest, mockHistory, mockClear, mockRecord].forEach((mock) => mock.mockReset());
  mockSuggest.mockResolvedValue({ suggestions: [] });
  setSearchState('', '');
});

afterEach(() => {
  queryClient.clear();
  setSearchState('', '');
});

describe('useDiscoverLogic recovers from failures', () => {
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
