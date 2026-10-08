import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import {
  clearSearchHistory,
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
  type DiscoverySearchResponse,
} from '@shared/api-client/discovery';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';
import { discoveryKeys } from '@shared/lib/query-keys';

import { useAutocompleteSuggestions } from '@features/discover/hooks/useAutocompleteSuggestions';
import { useDiscoverClearSearchHistory } from '@features/discover/hooks/useClearSearchHistory';
import { useDiscoverLogic } from '@features/discover/hooks/useDiscoverLogic';
import { useDiscoverSearch } from '@features/discover/hooks/useDiscoverSearch';
import { useSearchHistory } from '@features/discover/hooks/useSearchHistory';
import { setSearchState } from '@features/discover/searchState';

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
const mockSuggest = suggestDiscovery as jest.Mock;
const mockHistory = listSearchHistory as jest.Mock;
const mockClearHistory = clearSearchHistory as jest.Mock;

const QUERY = 'radiohead';
const HISTORY = { items: [{ query: QUERY, query_norm: QUERY }] };

function searchResponseFixture(): DiscoverySearchResponse {
  return {
    query: QUERY,
    query_norm: QUERY,
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

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function switchDiscoveryFetches(enabled: boolean): void {
  act(() => applyKillSwitches({ discovery_enabled: enabled }));
}

async function settle(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
  });
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false, retryDelay: 0 } },
  });
  setKillSwitchFileStore(createMemoryFileStore());
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  mockSearch.mockReset().mockResolvedValue(searchResponseFixture());
  mockSuggest.mockReset().mockResolvedValue({ suggestions: [] });
  mockHistory.mockReset().mockResolvedValue(HISTORY);
  mockClearHistory.mockReset().mockResolvedValue(undefined);
  setSearchState('', QUERY);
});

afterEach(() => {
  queryClient.clear();
  setSearchState('', '');
  setKillSwitchFileStore();
  jest.restoreAllMocks();
});

const gatedHooks: { hook: string; useGatedHook: () => unknown; request: jest.Mock }[] = [
  { hook: 'useDiscoverSearch', useGatedHook: () => useDiscoverSearch(QUERY), request: mockSearch },
  {
    hook: 'useAutocompleteSuggestions',
    useGatedHook: () => useAutocompleteSuggestions(QUERY),
    request: mockSuggest,
  },
  { hook: 'useSearchHistory', useGatedHook: () => useSearchHistory(), request: mockHistory },
];

type SearchHook = ReturnType<typeof useDiscoverSearch>;

const searchAffordances: { affordance: string; tap: (search: SearchHook) => void }[] = [
  { affordance: 'retry', tap: (search) => search.refetch() },
  {
    affordance: 'next page',
    tap: (search) => {
      void search.fetchNextPage();
    },
  },
];

describe('discover fetches — remote kill switch', () => {
  it.each(gatedHooks)(
    '$hook fires no request while the switch is off',
    async ({ useGatedHook, request }) => {
      switchDiscoveryFetches(false);

      renderHook(useGatedHook, { wrapper });
      await settle();

      expect(request).not.toHaveBeenCalled();
    },
  );

  it.each(searchAffordances)(
    'ignores the $affordance affordance tapped while the switch is off',
    async ({ tap }) => {
      switchDiscoveryFetches(false);
      const { result } = renderHook(() => useDiscoverSearch(QUERY), { wrapper });
      await settle();

      await act(async () => {
        tap(result.current);
        await Promise.resolve();
      });

      expect(mockSearch).not.toHaveBeenCalled();
    },
  );

  it('keeps the cached history when a clear is tapped while the switch is off', async () => {
    switchDiscoveryFetches(false);
    queryClient.setQueryData(discoveryKeys.history, HISTORY);
    const { result } = renderHook(() => useDiscoverClearSearchHistory(), { wrapper });

    act(() => result.current.clear());
    await settle();

    expect(mockClearHistory).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual(HISTORY);
  });

  it('tells the screen search is unavailable instead of searching while the switch is off', async () => {
    switchDiscoveryFetches(false);

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await settle();

    expect(result.current.results.view).toBe('unavailable');
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.history.items).toEqual([]);
  });

  it('searches again when the switch is turned back on, without a remount', async () => {
    switchDiscoveryFetches(false);
    renderHook(() => useDiscoverSearch(QUERY), { wrapper });
    await settle();
    expect(mockSearch).not.toHaveBeenCalled();

    switchDiscoveryFetches(true);

    await waitFor(() => expect(mockSearch).toHaveBeenCalledTimes(1));
  });

  it('leaves discover searching while another loop is switched off', async () => {
    act(() => applyKillSwitches({ sse_enabled: false, offline_downloads_enabled: false }));

    renderHook(() => useDiscoverSearch(QUERY), { wrapper });

    await waitFor(() => expect(mockSearch).toHaveBeenCalledTimes(1));
  });

  it('keeps searching after another loop is switched off with the screen mounted', async () => {
    const { result } = renderHook(() => useDiscoverSearch(QUERY), { wrapper });
    await waitFor(() => expect(mockSearch).toHaveBeenCalledTimes(1));

    act(() => applyKillSwitches({ sse_enabled: false }));
    act(() => result.current.refetch());

    await waitFor(() => expect(mockSearch).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  });

  describe('history retry', () => {
    async function failHistoryLoad() {
      mockHistory.mockRejectedValue(new Error('history down'));
      const hook = renderHook(() => useDiscoverLogic(), { wrapper });
      await waitFor(() => expect(hook.result.current.history.failed).toBe(true));
      return hook;
    }

    it('sends no request when tapped while the switch is off', async () => {
      const { result } = await failHistoryLoad();
      mockHistory.mockClear();
      switchDiscoveryFetches(false);

      await act(async () => {
        result.current.history.onRetry();
        await Promise.resolve();
      });

      expect(mockHistory).not.toHaveBeenCalled();
    });

    it('refetches when tapped while the switch is on', async () => {
      const { result } = await failHistoryLoad();
      mockHistory.mockClear().mockResolvedValue(HISTORY);

      await act(async () => {
        result.current.history.onRetry();
        await Promise.resolve();
      });

      await waitFor(() => expect(mockHistory).toHaveBeenCalledTimes(1));
    });
  });
});
