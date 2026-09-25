import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import {
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';
import { recordEvent } from '@shared/telemetry/recordEvent';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { setSearchState } from '../search-state';
import { resultFixture } from './fixtures';

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn(),
  listSearchHistory: jest.fn(),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

const mockSearch = searchDiscovery as jest.Mock;
const mockSuggest = suggestDiscovery as jest.Mock;
const mockHistory = listSearchHistory as jest.Mock;
const mockRecordEvent = recordEvent as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function searchResponse(partial: boolean): DiscoverySearchResponse {
  return {
    query: 'radiohead',
    query_norm: 'radiohead',
    search_id: 'search-1',
    results: [resultFixture()],
    sections: [],
    providers: [
      { provider: 'spotify', status: 'ok', result_count: 1, latency_ms: 80 },
      { provider: 'deezer', status: partial ? 'timeout' : 'ok', result_count: 0, latency_ms: 3000 },
    ],
    partial,
    cache: { hit: false, fetched_at: null },
    total: 1,
    offset: 0,
    has_more: false,
  };
}

function degradedEvents() {
  return mockRecordEvent.mock.calls
    .map((call) => call[0] as { type: string })
    .filter((event) => event.type === 'search_degraded');
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  mockSearch.mockReset();
  mockSuggest.mockReset().mockResolvedValue({ suggestions: [] });
  mockHistory.mockReset().mockResolvedValue({ items: [] });
  mockRecordEvent.mockReset().mockResolvedValue(undefined);
  setSearchState('radiohead', 'radiohead');
});

afterEach(() => {
  queryClient.clear();
  setSearchState('', '');
});

describe('useDiscoverLogic surfaces a partial (degraded) search to the UI and telemetry', () => {
  it('marks a partial response incomplete and records one search_degraded event', async () => {
    mockSearch.mockResolvedValue(searchResponse(true));

    const { result, rerender } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(result.current.view).toBe('results'));
    expect(result.current.resultsIncomplete).toBe(true);
    await waitFor(() => expect(degradedEvents()).toHaveLength(1));
    expect(degradedEvents()[0]).toEqual({
      type: 'search_degraded',
      search_id: 'search-1',
      payload: {
        result_count: 1,
        degraded_providers: [{ provider: 'deezer', status: 'timeout' }],
      },
    });

    rerender({});
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(degradedEvents()).toHaveLength(1);
  });

  it('reports a provider that only degrades on the second page', async () => {
    const healthyFirstPage = { ...searchResponse(false), has_more: true };
    const degradedSecondPage = { ...searchResponse(true), offset: 1 };
    mockSearch.mockImplementation(({ offset }: { offset: number }) =>
      Promise.resolve(offset === 0 ? healthyFirstPage : degradedSecondPage),
    );

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(result.current.view).toBe('results'));
    expect(degradedEvents()).toHaveLength(0);

    act(() => {
      result.current.onEndReached();
    });

    await waitFor(() => expect(result.current.resultsIncomplete).toBe(true));
    await waitFor(() => expect(degradedEvents()).toHaveLength(1));
    expect(degradedEvents()[0]).toEqual({
      type: 'search_degraded',
      search_id: 'search-1',
      payload: {
        result_count: 2,
        degraded_providers: [{ provider: 'deezer', status: 'timeout' }],
      },
    });
  });

  it('leaves a healthy response complete and records no search_degraded event', async () => {
    mockSearch.mockResolvedValue(searchResponse(false));

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(result.current.view).toBe('results'));
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(result.current.resultsIncomplete).toBe(false);
    expect(degradedEvents()).toHaveLength(0);
  });
});
