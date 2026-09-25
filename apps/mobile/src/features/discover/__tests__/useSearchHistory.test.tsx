import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { listSearchHistory, searchDiscovery, suggestDiscovery } from '@shared/api-client/discovery';
import { ApiError, NetworkError } from '@shared/errors';
import { discoveryKeys } from '@shared/lib/query-keys';
import { recordEvent } from '@shared/telemetry/recordEvent';
import { useSearchHistory } from '../hooks/useSearchHistory';

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn(),
  listSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockSearch = searchDiscovery as jest.Mock;
const mockSuggest = suggestDiscovery as jest.Mock;
const mockHistory = listSearchHistory as jest.Mock;
const mockRecordEvent = recordEvent as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function failureEvents() {
  return mockRecordEvent.mock.calls
    .map((call) => call[0] as { type: string; payload?: Record<string, unknown> })
    .filter((event) => event.type === 'search_failed');
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  mockSearch.mockReset();
  mockSuggest.mockReset();
  mockHistory.mockReset();
  mockRecordEvent.mockReset().mockResolvedValue(undefined);
});

afterEach(() => {
  queryClient.clear();
});

describe('discover query failures emit a search_failed telemetry event tagged with its source', () => {
  it('a failed history fetch reports the correlation id of a transport failure', async () => {
    mockHistory.mockRejectedValue(new NetworkError('transport', 'offline', 'f0e1d2c3b4a59687'));
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]?.payload).toEqual({
      source: 'history',
      correlationId: 'f0e1d2c3b4a59687',
    });
  });

  it('a failed history fetch fires search_failed with source history', async () => {
    mockHistory.mockRejectedValue(new ApiError(500, 'boom'));
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]?.payload).toEqual({ source: 'history', status: 500 });
  });

  it('a successful query emits no failure event', async () => {
    mockHistory.mockResolvedValue({ items: [] });
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.data).toEqual({ items: [] }));

    expect(failureEvents()).toHaveLength(0);
  });

  it('one failure is reported once, across re-renders and a second observer of the same query', async () => {
    mockHistory.mockRejectedValue(new ApiError(500, 'boom'));
    const first = renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(first.result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    first.rerender({});
    const second = renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(second.result.current.error).not.toBeNull());

    expect(failureEvents()).toHaveLength(1);
  });

  it('a later, separate failure of the same query is reported again', async () => {
    mockHistory.mockRejectedValue(new ApiError(500, 'boom'));
    renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    mockHistory.mockRejectedValue(new ApiError(502, 'bad gateway'));
    await queryClient.refetchQueries({ queryKey: discoveryKeys.history });

    await waitFor(() => expect(failureEvents()).toHaveLength(2));
    expect(failureEvents()[1]?.payload).toEqual({ source: 'history', status: 502 });
  });
});
