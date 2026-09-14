import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { listSearchHistory, searchDiscovery, suggestDiscovery } from '@shared/api-client/discovery';
import { ApiError } from '@shared/api-client/errors';
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
}));

const mockSearch = searchDiscovery as jest.Mock;
const mockSuggest = suggestDiscovery as jest.Mock;
const mockHistory = listSearchHistory as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  mockSearch.mockReset();
  mockSuggest.mockReset();
  mockHistory.mockReset();
  // Restored input of two+ chars enables the suggest query without a committed search.
  setSearchState('', 'rad');
});

afterEach(() => {
  queryClient.clear();
  setSearchState('', '');
});

describe('useDiscoverLogic surfaces suggestion and history fetch failures', () => {
  it('forwards a failed suggest and history query as typed errors while lists stay empty', async () => {
    const suggestFailure = new ApiError(500, 'suggest down');
    const historyFailure = new ApiError(500, 'history down');
    mockSuggest.mockRejectedValue(suggestFailure);
    mockHistory.mockRejectedValue(historyFailure);

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(result.current.suggestionsError).toBe(suggestFailure));
    await waitFor(() => expect(result.current.historyError).toBe(historyFailure));
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.historyItems).toEqual([]);
  });

  it('leaves both errors null when the queries succeed with nothing to show', async () => {
    mockSuggest.mockResolvedValue({ suggestions: [] });
    mockHistory.mockResolvedValue({ items: [] });

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(mockSuggest).toHaveBeenCalled());
    await waitFor(() => expect(mockHistory).toHaveBeenCalled());
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(result.current.suggestionsError).toBeNull();
    expect(result.current.historyError).toBeNull();
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.historyItems).toEqual([]);
  });
});
