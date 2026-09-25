// Regression for issue #1685: discover's search, suggest, history and clear-history calls must be
// gated by the remote kill switch, so a discovery backend that starts erroring or rate limiting can
// be stopped without an app-store release.

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

import { useAutocompleteSuggestions } from '../hooks/useAutocompleteSuggestions';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { useSearchHistory } from '../hooks/useSearchHistory';
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

// A disabled query never resolves a promise, so nothing waits: let React and react-query settle
// once, then assert that no request was made.
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
  // Restored input long enough to open the suggest query without a committed search.
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

// react-query's own refetch and fetchNextPage fetch whatever `enabled` says, so each affordance the
// screen exposes is its own way past the switch.
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
    const { result } = renderHook(() => useClearSearchHistory(), { wrapper });

    act(() => result.current.clear());
    await settle();

    expect(mockClearHistory).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual(HISTORY);
  });

  it('tells the screen search is unavailable instead of searching while the switch is off', async () => {
    switchDiscoveryFetches(false);

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await settle();

    expect(result.current.view).toBe('unavailable');
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.historyItems).toEqual([]);
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
});
