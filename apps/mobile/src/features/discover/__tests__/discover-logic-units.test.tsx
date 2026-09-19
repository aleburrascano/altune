import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import { Keyboard } from 'react-native';

import { clearSearchHistory } from '@shared/api-client/discovery';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useResultTap } from '../hooks/useResultTap';
import { useResultsFilter } from '../hooks/useResultsFilter';
import { MIN_QUERY_LENGTH } from '../hooks/useDiscoverSearch';
import { useSuggestionVisibility } from '../hooks/useSuggestionVisibility';
import { stashHandoffForDetail } from '../handoff';
import { resultFixture } from './fixtures';

import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';

const mockMutate = jest.fn();
const mockPush = jest.fn();
const mockUseFocusEffect = jest.fn();

jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: mockMutate }),
}));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useFocusEffect: (effect: () => void) => mockUseFocusEffect(effect),
}));
jest.mock('@shared/api-client/discovery', () => ({
  clearSearchHistory: jest.fn(),
}));
jest.mock('../handoff', () => ({
  stashHandoffForDetail: jest.fn(() => '/discover/detail'),
}));

const mockClearSearchHistory = clearSearchHistory as jest.Mock;
const mockStash = stashHandoffForDetail as jest.Mock;

function responseFixture(
  overrides: Partial<DiscoverySearchResponse> = {},
): DiscoverySearchResponse {
  return {
    query: 'Radiohead',
    query_norm: 'radiohead',
    search_id: 'search-1',
    results: [resultFixture({ title: 'first' }), resultFixture({ title: 'second' })],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 2,
    offset: 0,
    has_more: false,
    ...overrides,
  };
}

beforeEach(() => {
  jest.clearAllMocks();
});

describe('useResultsFilter resets to "all" only when a new query is committed', () => {
  it('keeps the chosen filter while the query is unchanged and resets it on a new query', () => {
    const { result, rerender } = renderHook(
      ({ query }: { query: string }) => useResultsFilter(query),
      {
        initialProps: { query: 'radiohead' },
      },
    );

    act(() => result.current.setFilter('track'));
    rerender({ query: 'radiohead' });
    expect(result.current.filter).toBe('track');

    rerender({ query: 'bjork' });
    expect(result.current.filter).toBe('all');
  });
});

describe('useClearSearchHistory empties the cache at once and rolls it back on failure', () => {
  let queryClient: QueryClient;

  afterEach(() => {
    queryClient.clear();
  });

  function setup() {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { gcTime: Infinity },
        mutations: { retry: false, gcTime: Infinity },
      },
    });
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'old' }] });
    const invalidate = jest.spyOn(queryClient, 'invalidateQueries');
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useClearSearchHistory(), { wrapper });
    return { queryClient, invalidate, result, clear: result.current.clear };
  }

  it('writes an empty history before the server call and does not refetch on success', async () => {
    mockClearSearchHistory.mockResolvedValue(undefined);
    const { queryClient, invalidate, clear } = setup();

    act(() => clear());

    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(1));
    expect(invalidate).not.toHaveBeenCalled();
  });

  it('rolls the history back, surfaces the error and invalidates when the server call fails', async () => {
    const failure = new Error('offline');
    mockClearSearchHistory.mockRejectedValue(failure);
    const { queryClient, invalidate, result, clear } = setup();

    act(() => clear());

    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    await waitFor(() => expect(result.current.error).toBe(failure));
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [{ query: 'old' }] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: discoveryKeys.history });
  });

  it('leaves the error null on success and clears a previous failure on retry', async () => {
    mockClearSearchHistory.mockRejectedValueOnce(new Error('offline'));
    mockClearSearchHistory.mockResolvedValueOnce(undefined);
    const { queryClient, result } = setup();

    expect(result.current.error).toBeNull();
    act(() => result.current.clear());
    await waitFor(() => expect(result.current.error).not.toBeNull());

    act(() => result.current.clear());
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.error).toBeNull());
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
  });

  it('does not restore or refetch history when the failure settles after sign-out', async () => {
    let reject: (e: Error) => void = () => undefined;
    mockClearSearchHistory.mockReturnValue(
      new Promise<void>((_resolve, rej) => {
        reject = rej;
      }),
    );
    const { queryClient, invalidate, clear } = setup();

    act(() => clear());
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(1));
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'next-user' }] });
    await act(async () => {
      reject(new Error('offline'));
      await Promise.resolve();
    });

    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({
      items: [{ query: 'next-user' }],
    });
    expect(invalidate).not.toHaveBeenCalled();
  });

  it('does not empty the history when the success settles after sign-out', async () => {
    let resolve: () => void = () => undefined;
    mockClearSearchHistory.mockReturnValue(
      new Promise<void>((res) => {
        resolve = res;
      }),
    );
    const { queryClient, clear } = setup();

    act(() => clear());
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(1));
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'next-user' }] });
    await act(async () => {
      resolve();
      await Promise.resolve();
    });

    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({
      items: [{ query: 'next-user' }],
    });
  });
});

describe('useResultTap records result_clicked and hands off to the detail screen', () => {
  it('uses the global index, search identity and signature from the response', () => {
    const dismiss = jest.spyOn(Keyboard, 'dismiss');
    const data = responseFixture();
    const tapped = data.results[1]!;
    const { result } = renderHook(() => useResultTap(data));

    result.current(tapped, 0);

    expect(dismiss).toHaveBeenCalled();
    expect(mockMutate).toHaveBeenCalledWith({
      type: 'result_clicked',
      search_id: 'search-1',
      payload: {
        kind: 'track',
        title: 'second',
        subtitle: null,
        position: 1,
        confidence: 'high',
        provider: 'spotify',
        result_signature: 'sig',
      },
    });
    expect(mockStash).toHaveBeenCalledWith(tapped, 'search-1');
    expect(mockPush).toHaveBeenCalledWith('/discover/detail');
  });

  it('logs the global rank for a blended-view tap whose object is a parsed copy, not a results[] reference', () => {
    const results = [
      resultFixture({
        kind: 'artist',
        title: 'Radiohead',
        result_signature: 'artist|radiohead|',
        sources: [{ provider: 'spotify', external_id: 'art-1', url: 'https://x' }],
      }),
      resultFixture({
        title: 'Creep',
        result_signature: 'track|creep|radiohead',
        sources: [{ provider: 'spotify', external_id: 'trk-1', url: 'https://x' }],
      }),
      resultFixture({
        title: 'Karma Police',
        result_signature: 'track|karma police|radiohead',
        sources: [{ provider: 'deezer', external_id: 'trk-2', url: 'https://x' }],
      }),
    ];
    // top_result / sections[].items are parsed independently from results[], so the same
    // logical entry arrives as a structurally equal but distinct object.
    const copy = (r: DiscoveryResult): DiscoveryResult => structuredClone(r);
    const data = responseFixture({
      results,
      top_result: copy(results[0]!),
      sections: [{ kind: 'track', items: [copy(results[1]!), copy(results[2]!)], has_more: false }],
    });
    const { result } = renderHook(() => useResultTap(data));
    const onFocus = () => act(() => (mockUseFocusEffect.mock.calls.at(-1)![0] as () => void)());

    result.current(data.sections[0]!.items[1]!, 1);
    onFocus();
    result.current(data.top_result!, 0);

    expect(
      mockMutate.mock.calls.map(([e]) => (e as { payload: { position: number } }).payload.position),
    ).toEqual([2, 0]);
  });

  it('matches by result signature when the tapped copy carries no source identity', () => {
    const results = [
      resultFixture({ title: 'a', result_signature: 'track|a|', sources: [] }),
      resultFixture({ title: 'b', result_signature: 'track|b|', sources: [] }),
    ];
    const data = responseFixture({ results });
    const { result } = renderHook(() => useResultTap(data));

    result.current(structuredClone(results[1]!), 0);

    expect(mockMutate.mock.calls[0]![0].payload.position).toBe(1);
  });

  it('falls back to the passed position, and omits a missing signature', () => {
    const orphan = resultFixture({ result_signature: undefined, sources: [] });
    const { result } = renderHook(() => useResultTap(undefined));

    result.current(orphan, 4);

    expect(mockMutate).toHaveBeenCalledWith({
      type: 'result_clicked',
      search_id: undefined,
      payload: {
        kind: 'track',
        title: 'The Title',
        subtitle: null,
        position: 4,
        confidence: 'high',
        provider: null,
      },
    });
  });

  it('ignores a second tap while the first navigation is pending, until the screen refocuses', () => {
    const data = responseFixture();
    const [first, second] = data.results;
    const { result } = renderHook(() => useResultTap(data));

    result.current(first!, 0);
    result.current(second!, 1);

    expect(mockPush).toHaveBeenCalledTimes(1);
    expect(mockMutate).toHaveBeenCalledTimes(1);
    expect(mockStash).toHaveBeenCalledTimes(1);
    expect(mockStash).toHaveBeenCalledWith(first, 'search-1');

    const onFocus = mockUseFocusEffect.mock.calls.at(-1)![0] as () => void;
    act(() => onFocus());
    result.current(second!, 1);

    expect(mockPush).toHaveBeenCalledTimes(2);
    expect(mockStash).toHaveBeenLastCalledWith(second, 'search-1');
  });
});

describe('useSuggestionVisibility shows suggestions only while the user is typing', () => {
  function setup(inputValue: string, count: number) {
    const search = {
      inputValue,
      onChangeText: jest.fn(),
      onSubmit: jest.fn(),
      setQuery: jest.fn(),
    };
    const hook = renderHook(({ n }: { n: number }) => useSuggestionVisibility(search, n), {
      initialProps: { n: count },
    });
    return { search, ...hook };
  }

  it('requires focus, a minimum-length query and at least one suggestion', () => {
    const { result, rerender } = setup('a'.repeat(MIN_QUERY_LENGTH), 1);
    expect(result.current.showSuggestions).toBe(false);

    act(() => result.current.setIsFocused(true));
    expect(result.current.showSuggestions).toBe(true);

    rerender({ n: 0 });
    expect(result.current.showSuggestions).toBe(false);
  });

  it('stays closed for a focused query left below the minimum length by trimming', () => {
    const { result } = setup(` ${'a'.repeat(MIN_QUERY_LENGTH - 1)} `, 1);

    act(() => result.current.setIsFocused(true));

    expect(result.current.showSuggestions).toBe(false);
  });

  it('hides on submit and on suggestion select, and reopens on typing', () => {
    const { result, search } = setup('radio', 3);
    act(() => result.current.setIsFocused(true));

    act(() => result.current.onSubmit());
    expect(search.onSubmit).toHaveBeenCalled();
    expect(result.current.showSuggestions).toBe(false);

    act(() => result.current.onChangeText('radioh'));
    expect(search.onChangeText).toHaveBeenCalledWith('radioh');
    expect(result.current.showSuggestions).toBe(true);

    act(() => result.current.onSuggestionSelect('radiohead'));
    expect(search.setQuery).toHaveBeenCalledWith('radiohead');
    expect(result.current.showSuggestions).toBe(false);
  });
});
