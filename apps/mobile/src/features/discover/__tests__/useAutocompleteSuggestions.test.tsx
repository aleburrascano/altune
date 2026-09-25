import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { listSearchHistory, searchDiscovery, suggestDiscovery } from '@shared/api-client/discovery';
import { recordEvent } from '@shared/telemetry/recordEvent';
import {
  SUGGEST_DEBOUNCE_MS,
  useAutocompleteSuggestions,
} from '../hooks/useAutocompleteSuggestions';

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

function renderSuggestions() {
  return renderHook(({ text }: { text: string }) => useAutocompleteSuggestions(text), {
    wrapper,
    initialProps: { text: '' },
  });
}

type Rendered = ReturnType<typeof renderSuggestions>;

// Each keystroke lands well inside the debounce window, so the burst is the
// ticket's scenario: a pasted or fast-typed query, not a pause between letters.
function typeBurst(rendered: Rendered, text: string): void {
  for (let length = 1; length <= text.length; length += 1) {
    act(() => {
      rendered.rerender({ text: text.slice(0, length) });
      jest.advanceTimersByTime(SUGGEST_DEBOUNCE_MS - 1);
    });
  }
}

function settle(): void {
  act(() => {
    jest.advanceTimersByTime(SUGGEST_DEBOUNCE_MS);
  });
}

function signalOfCall(index: number): AbortSignal | undefined {
  return mockSuggest.mock.calls[index]?.[1] as AbortSignal | undefined;
}

beforeEach(() => {
  jest.useFakeTimers();
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  mockSuggest.mockReset().mockResolvedValue({ suggestions: [] });
});

afterEach(() => {
  queryClient.clear();
  jest.runOnlyPendingTimers();
  jest.useRealTimers();
});

describe('useAutocompleteSuggestions asks for suggestions once the typing settles', () => {
  it('sends one request for a burst of keystrokes typed inside the debounce window', () => {
    const rendered = renderSuggestions();

    typeBurst(rendered, 'radiohead');
    settle();

    expect(mockSuggest).toHaveBeenCalledTimes(1);
    expect(mockSuggest).toHaveBeenCalledWith({ q: 'radiohead', limit: 5 }, expect.anything());
  });

  it('holds the request back until the typing has paused for the debounce window', () => {
    const rendered = renderSuggestions();

    typeBurst(rendered, 'rad');

    expect(mockSuggest).not.toHaveBeenCalled();
  });
});

describe('useAutocompleteSuggestions cancels a suggest request its successor supersedes', () => {
  it('aborts the in-flight request when a later query starts', () => {
    mockSuggest.mockImplementation(() => new Promise(() => {}));
    const rendered = renderSuggestions();

    typeBurst(rendered, 'rad');
    settle();
    typeBurst(rendered, 'radiohead');
    settle();

    expect(mockSuggest).toHaveBeenCalledTimes(2);
    expect(signalOfCall(0)?.aborted).toBe(true);
    expect(signalOfCall(1)?.aborted).toBe(false);
  });
});

describe('discover query failures emit a search_failed telemetry event tagged with its source', () => {
  function failureEvents() {
    return mockRecordEvent.mock.calls
      .map((call) => call[0] as { type: string; payload?: Record<string, unknown> })
      .filter((event) => event.type === 'search_failed');
  }

  // This file runs on fake timers; the failure-reporting tests wait on real ones.
  beforeEach(() => {
    jest.useRealTimers();
  });

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

  afterEach(() => {
    jest.useFakeTimers();
  });

  it('a failed suggest fires search_failed with source suggest, omitting status for non-HTTP errors', async () => {
    mockSuggest.mockRejectedValue(new TypeError('Network request failed'));
    const { result } = renderHook(() => useAutocompleteSuggestions('rad'), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]).toEqual({ type: 'search_failed', payload: { source: 'suggest' } });
  });
});
