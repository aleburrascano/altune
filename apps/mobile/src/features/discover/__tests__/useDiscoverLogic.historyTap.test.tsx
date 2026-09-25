import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import {
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
} from '@shared/api-client/discovery';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { setSearchState } from '../search-state';
import { MAX_QUERY_LENGTH } from '../searchLimits';

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

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  setSearchState('', '');
  mockSearch.mockReset().mockResolvedValue({
    query: 'ab',
    query_norm: 'ab',
    search_id: 's1',
    results: [],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 0,
    offset: 0,
    has_more: false,
  });
  (suggestDiscovery as jest.Mock).mockReset().mockResolvedValue({ suggestions: [] });
  (listSearchHistory as jest.Mock).mockReset().mockResolvedValue({ items: [] });
});

afterEach(() => {
  queryClient.clear();
});

describe('useDiscoverLogic history tap', () => {
  it('is not pending after tapping a history item with trailing whitespace', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    act(() => {
      result.current.onHistoryTap({ query: 'ab ' } as never);
    });

    await waitFor(() => expect(result.current.committedQuery).toBe('ab'));
    expect(result.current.pending).toBe(false);
  });

  it('never sends a q longer than MAX_QUERY_LENGTH', async () => {
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    act(() => {
      result.current.onChangeText('a'.repeat(5000));
    });
    act(() => {
      result.current.onSubmit();
    });

    await waitFor(() => expect(mockSearch).toHaveBeenCalled());
    for (const [args] of mockSearch.mock.calls) {
      expect((args as { q: string }).q.length).toBeLessThanOrEqual(MAX_QUERY_LENGTH);
    }
  });
});
