import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { listSearchHistory, searchDiscovery, suggestDiscovery } from '@shared/api-client/discovery';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
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
  useFocusEffect: jest.fn(),
}));

const mockSearch = searchDiscovery as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function page() {
  return {
    query: 'radiohead',
    query_norm: 'radiohead',
    search_id: 's1',
    results: [],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 0,
    offset: 0,
    has_more: false,
  };
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  setSearchState('', '');
  mockSearch.mockReset().mockResolvedValue(page());
  (suggestDiscovery as jest.Mock).mockReset().mockResolvedValue({ suggestions: [] });
  (listSearchHistory as jest.Mock).mockReset().mockResolvedValue({ items: [] });
});

afterEach(() => {
  queryClient.clear();
});

async function submitRadiohead() {
  const hook = renderHook(() => useDiscoverLogic(), { wrapper });
  act(() => hook.result.current.onChangeText('radiohead'));
  act(() => hook.result.current.onSubmit());
  await waitFor(() => expect(mockSearch).toHaveBeenCalled());
  return hook;
}

function switchAccount() {
  act(() => {
    runSignOutCleanups();
    queryClient.clear();
  });
}

describe('a direct account switch with Discover still mounted', () => {
  it('clears the input', async () => {
    const { result } = await submitRadiohead();

    switchAccount();

    expect(result.current.inputValue).toBe('');
  });

  it('does not re-run the previous search with saveHistory', async () => {
    await submitRadiohead();
    mockSearch.mockClear();

    switchAccount();
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });

    const saved = mockSearch.mock.calls.filter(
      ([params]) => (params as { saveHistory?: boolean } | undefined)?.saveHistory === true,
    );
    expect(saved).toHaveLength(0);
  });

  it('drops a pending debounced commit', () => {
    jest.useFakeTimers();
    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    act(() => result.current.onChangeText('radiohead'));

    switchAccount();
    act(() => {
      jest.advanceTimersByTime(5000);
    });
    jest.useRealTimers();

    expect(mockSearch).not.toHaveBeenCalled();
  });
});
